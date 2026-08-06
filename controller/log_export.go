package controller

import (
	"encoding/csv"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// logExportColumn describes one exportable bill column. Frontend "列设置" keys
// are the source of truth, but the bill export filters the user-selected set
// through a strict allow-list defined here. Channel-related and privacy-
// sensitive keys (channel/retry/ip/details) are intentionally absent.
type logExportColumn struct {
	key    string
	header string
	width  float64
}

// logExportAllowedColumns is the closed set of bill columns. Order matters and
// matches what we want the xlsx to look like by default. The frontend may pass
// a `columns` query param to reorder/subset; unknown keys are silently dropped.
var logExportAllowedColumns = []logExportColumn{
	{key: "time", header: "时间", width: 20},
	{key: "username", header: "账户", width: 16},
	{key: "token", header: "令牌", width: 16},
	{key: "group", header: "分组", width: 12},
	{key: "type", header: "类型", width: 10},
	{key: "model", header: "模型", width: 22},
	{key: "use_time", header: "用时(ms)", width: 10},
	{key: "prompt", header: "输入 tokens", width: 12},
	{key: "completion", header: "输出 tokens", width: 12},
	{key: "cost", header: "费用", width: 14},
}

// logExportColumnMap is the indexed view of logExportAllowedColumns.
var logExportColumnMap = func() map[string]logExportColumn {
	m := make(map[string]logExportColumn, len(logExportAllowedColumns))
	for _, c := range logExportAllowedColumns {
		m[c.key] = c
	}
	return m
}()

// logExportDefaultColumnKeys is the fallback when no `columns` param is given.
var logExportDefaultColumnKeys = []string{"time", "username", "token", "group", "type", "model", "prompt", "completion", "cost"}

// Forced columns appended to every export regardless of the user's selection.
var cacheReadColumn = logExportColumn{key: "cache_read", header: "缓存读取 tokens", width: 16}
var cacheCreationColumn = logExportColumn{key: "cache_creation", header: "缓存创建 tokens", width: 16}
var billingColumn = logExportColumn{key: "billing", header: "计费过程", width: 70}
var requestIdColumn = logExportColumn{key: "request_id", header: "请求 ID", width: 36}

// logPricingInfo mirrors the subset of Log.Other we need to rebuild the
// billing breakdown. Field names match the JS source in
// web/src/helpers/render.jsx so the two stay easy to keep in sync.
type logPricingInfo struct {
	ModelRatio            float64 `json:"model_ratio"`
	ModelPrice            float64 `json:"model_price"`
	CompletionRatio       float64 `json:"completion_ratio"`
	GroupRatio            float64 `json:"group_ratio"`
	UserGroupRatio        float64 `json:"user_group_ratio"`
	CacheTokens           int     `json:"cache_tokens"`
	CacheRatio            float64 `json:"cache_ratio"`
	CacheCreationTokens   int     `json:"cache_creation_tokens"`
	CacheCreationRatio    float64 `json:"cache_creation_ratio"`
	CacheCreationTokens5m int     `json:"cache_creation_tokens_5m"`
	CacheCreationRatio5m  float64 `json:"cache_creation_ratio_5m"`
	CacheCreationTokens1h int     `json:"cache_creation_tokens_1h"`
	CacheCreationRatio1h  float64 `json:"cache_creation_ratio_1h"`
	CacheWriteTokens      int     `json:"cache_write_tokens"`
	Frt                   float64 `json:"frt"` // 首字延迟毫秒；<=0 视为未记录

	// 上游 usage 语义，决定 prompt_tokens 是否已含缓存 tokens：
	// "anthropic" → 不含（Claude 原厂 input_tokens 与 cache 互斥）；
	// 其余（含缺失的旧日志）→ 含。Claude 为最终上游格式时 claude=true，
	// 老日志可能只有后者，故两者取或。写入点见 service/text_quota.go。
	UsageSemantic string `json:"usage_semantic"`
	Claude        bool   `json:"claude"`

	// Tiered-expression billing keys written by InjectTieredBillingInfo.
	// expr_b64 is the base64 expression; matched_tier names the tier that
	// actually billed this request (see pkg/billingexpr/expr.md).
	BillingMode string `json:"billing_mode"`
	ExprB64     string `json:"expr_b64"`
	MatchedTier string `json:"matched_tier"`

	// Async-task billing keys (seedance-style pre-consume → settle/refund),
	// written by LogTaskConsumption / RecalculateTaskQuota / RefundTaskQuota.
	IsTask           bool    `json:"is_task"`
	BillingStage     string  `json:"billing_stage"` // pre_consume | settle | refund
	TaskId           string  `json:"task_id"`
	PreConsumedQuota float64 `json:"pre_consumed_quota"`
	ActualQuota      float64 `json:"actual_quota"`
	Reason           string  `json:"reason"`
	VideoUnitPrice   float64 `json:"video_unit_price"`
	VideoTokens      float64 `json:"video_tokens"`
	VideoInput       float64 `json:"video_input"` // video-input discount ratio, shown only when present
	VideoResolution  string  `json:"video_resolution_tier"`
	VideoHasInput    bool    `json:"video_has_input"`
}

const billingDisclaimer = "仅供参考，以实际扣费为准"
const billingMissingPlaceholder = "（无计费明细）" + "\n" + billingDisclaimer

// buildBillingText reconstructs the same three-line "price display" billing
// breakdown the frontend shows on the cost column tooltip, plus a disclaimer
// line. The actual total is read straight from Log.Quota to stay aligned with
// what was charged — we only rebuild the per-token math.
func buildBillingText(log *model.Log) string {
	if log == nil {
		return billingMissingPlaceholder
	}
	// seedance 单行化导出：预扣行携带 TaskInfo 时输出任务全过程（状态/
	// 预扣→实扣/失败原因/任务 ID），settle/refund 行已在导出批次中剔除。
	if log.TaskInfo != nil {
		return buildTaskInfoBillingText(log)
	}
	if strings.TrimSpace(log.Other) == "" {
		return billingMissingPlaceholder
	}
	var info logPricingInfo
	if err := common.UnmarshalJsonStr(log.Other, &info); err != nil {
		return billingMissingPlaceholder
	}

	// Async-task rows (pre-consume / settle / refund) carry their own stage
	// formulas — the token-ratio reconstruction below would be meaningless.
	if text, ok := buildTaskBillingText(log, &info); ok {
		return text
	}

	totalUSD := float64(log.Quota) / common.QuotaPerUnit
	ratioLabel := "分组倍率"
	effectiveRatio := info.GroupRatio
	if isValidGroupRatio(info.UserGroupRatio) {
		ratioLabel = "专属倍率"
		effectiveRatio = info.UserGroupRatio
	}

	// Per-call pricing branch: model_price > 0 means flat-rate per request,
	// not a ratio-based bill. Mirror render.jsx's short-form output.
	if info.ModelPrice > 0 {
		line := fmt.Sprintf("按次计费：$%s * %s %s = $%s",
			formatPrice(info.ModelPrice),
			ratioLabel,
			formatRatio(effectiveRatio),
			formatPrice(totalUSD),
		)
		return line + "\n" + billingDisclaimer
	}

	// Ratio pricing branch needs at least a model_ratio to be meaningful. If
	// it's missing, fall back to the placeholder so we don't emit a bogus
	// "$0.000000 / 1M tokens" line.
	if info.ModelRatio == 0 {
		return billingMissingPlaceholder
	}

	inputUnitPrice := info.ModelRatio * 2.0
	completionUnitPrice := info.ModelRatio * 2.0 * info.CompletionRatio
	cacheUnitPrice := info.ModelRatio * 2.0 * info.CacheRatio
	cacheCreationUnitPrice := info.ModelRatio * 2.0 * info.CacheCreationRatio
	cacheCreationUnitPrice5m := info.ModelRatio * 2.0 * info.CacheCreationRatio5m
	cacheCreationUnitPrice1h := info.ModelRatio * 2.0 * info.CacheCreationRatio1h

	hasSplitCacheCreation := info.CacheCreationTokens5m > 0 || info.CacheCreationTokens1h > 0
	showLegacyCacheCreation := !hasSplitCacheCreation && info.CacheCreationTokens > 0

	segments := []string{
		fmt.Sprintf("提示 %d tokens / 1M tokens * $%s", log.PromptTokens, formatPrice(inputUnitPrice)),
	}
	if info.CacheTokens > 0 {
		segments = append(segments,
			fmt.Sprintf("缓存 %d tokens / 1M tokens * $%s", info.CacheTokens, formatPrice(cacheUnitPrice)),
		)
	}
	if showLegacyCacheCreation {
		segments = append(segments,
			fmt.Sprintf("缓存创建 %d tokens / 1M tokens * $%s", info.CacheCreationTokens, formatPrice(cacheCreationUnitPrice)),
		)
	}
	if hasSplitCacheCreation && info.CacheCreationTokens5m > 0 {
		segments = append(segments,
			fmt.Sprintf("5m缓存创建 %d tokens / 1M tokens * $%s", info.CacheCreationTokens5m, formatPrice(cacheCreationUnitPrice5m)),
		)
	}
	if hasSplitCacheCreation && info.CacheCreationTokens1h > 0 {
		segments = append(segments,
			fmt.Sprintf("1h缓存创建 %d tokens / 1M tokens * $%s", info.CacheCreationTokens1h, formatPrice(cacheCreationUnitPrice1h)),
		)
	}
	segments = append(segments,
		fmt.Sprintf("补全 %d tokens / 1M tokens * $%s", log.CompletionTokens, formatPrice(completionUnitPrice)),
	)

	breakdown := strings.Join(segments, " + ")
	line1 := fmt.Sprintf("输入价格：$%s / 1M tokens", formatPrice(inputUnitPrice))
	line2 := fmt.Sprintf("输出价格：$%s / 1M tokens", formatPrice(completionUnitPrice))
	line3 := fmt.Sprintf("%s * %s %s = $%s", breakdown, ratioLabel, formatRatio(effectiveRatio), formatPrice(totalUSD))

	return line1 + "\n" + line2 + "\n" + line3 + "\n" + billingDisclaimer
}

// isValidGroupRatio mirrors the JS counterpart: a ratio is valid only if it's
// finite and not the sentinel -1.
func isValidGroupRatio(r float64) bool {
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return false
	}
	return r != -1
}

// seedanceTaskStatusLabel 把任务状态映射为导出文本用的中文标签。
func seedanceTaskStatusLabel(status string) string {
	switch status {
	case string(model.TaskStatusSuccess):
		return "成功"
	case string(model.TaskStatusFailure):
		return "失败"
	case string(model.TaskStatusInProgress):
		return "生成中"
	case string(model.TaskStatusSubmitted), string(model.TaskStatusQueued):
		return "排队中"
	default:
		return "未知"
	}
}

// buildTaskInfoBillingText 渲染 seedance 单行化导出的计费过程：任务状态、
// 预扣→实扣净额、输出 tokens、失败原因与任务 ID（上游 ID 仅 admin 路径有值）。
func buildTaskInfoBillingText(log *model.Log) string {
	ti := log.TaskInfo
	quotaUSD := func(q int) string { return "$" + formatPrice(float64(q)/common.QuotaPerUnit) }
	lines := []string{
		fmt.Sprintf("视频任务：%s（进度 %s）", seedanceTaskStatusLabel(ti.Status), ti.Progress),
	}
	settleLine := fmt.Sprintf("预扣 %s → 实扣 %s", quotaUSD(ti.PreQuota), quotaUSD(ti.FinalQuota))
	if ti.Status == string(model.TaskStatusFailure) && ti.FinalQuota == 0 {
		settleLine += "（已退款）"
	}
	lines = append(lines, settleLine)
	if ti.OutputTokens > 0 {
		lines = append(lines, fmt.Sprintf("输出 %d tokens", ti.OutputTokens))
	}
	if ti.FailReason != "" {
		lines = append(lines, "失败原因 "+ti.FailReason)
	}
	if ti.TaskId != "" {
		lines = append(lines, "任务 "+ti.TaskId)
	}
	if ti.UpstreamTaskId != "" {
		lines = append(lines, "上游任务 "+ti.UpstreamTaskId)
	}
	return strings.Join(lines, "\n")
}

// applySeedanceExportMerge 把原始日志导出批次单行化：seedance 的 settle/refund
// 行剔除；带 TaskInfo 的预扣行费用改写为实扣净额、输出 tokens 落列（导出行是
// 一次性流式对象，就地改写安全）。其余行原样透传。
func applySeedanceExportMerge(batch []*model.Log) []*model.Log {
	out := batch[:0]
	for _, log := range batch {
		if strings.Contains(log.ModelName, "seedance") &&
			(strings.Contains(log.Other, `"billing_stage":"settle"`) || strings.Contains(log.Other, `"billing_stage":"refund"`)) {
			continue
		}
		if ti := log.TaskInfo; ti != nil {
			log.Quota = ti.FinalQuota
			if log.CompletionTokens == 0 && ti.OutputTokens > 0 {
				log.CompletionTokens = ti.OutputTokens
			}
		}
		out = append(out, log)
	}
	return out
}

// buildTaskBillingText renders the stage-specific billing breakdown for async
// task rows (pre-consume → settle/refund, e.g. seedance video tasks). Wording
// mirrors the frontend renderTaskBillingProcess so exports match the log page.
// ok=false means the row is not a task row and the ratio-based path applies.
func buildTaskBillingText(log *model.Log, info *logPricingInfo) (string, bool) {
	isPre := info.BillingStage == "pre_consume" || (info.BillingStage == "" && info.IsTask)
	isSettle := !isPre && (info.BillingStage == "settle" || info.BillingStage == "refund" || info.TaskId != "")
	if !isPre && !isSettle {
		return "", false
	}

	ratioLabel := "分组倍率"
	groupRatio := info.GroupRatio
	if isValidGroupRatio(info.UserGroupRatio) && info.UserGroupRatio > 0 {
		ratioLabel = "专属倍率"
		groupRatio = info.UserGroupRatio
	}
	unitUSD := info.VideoUnitPrice
	if unitUSD == 0 {
		unitUSD = info.ModelRatio * 2.0
	}
	vpart := ""
	if info.VideoInput > 0 && info.VideoInput != 1 {
		vpart = " × 视频折扣 " + formatRatio(info.VideoInput)
	}
	quotaUSD := func(q float64) string { return "$" + formatPrice(q/common.QuotaPerUnit) }

	var lines []string
	if isSettle {
		pre, actual := info.PreConsumedQuota, info.ActualQuota
		if pre == 0 && actual == 0 {
			// 失败全额退款(RefundTaskQuota)：无差额字段，退还整行金额。
			lines = append(lines, "任务退款：退还预扣 "+quotaUSD(float64(log.Quota)))
			reason := info.Reason
			if reason == "" {
				reason = log.Content
			}
			if reason != "" {
				lines = append(lines, "原因 "+reason)
			}
		} else {
			if info.VideoTokens > 0 && unitUSD > 0 {
				lines = append(lines, fmt.Sprintf("实际结算 = %d tokens × 单价 $%s / 1M tokens × %s %s%s = 应扣 %s",
					int(info.VideoTokens), formatPrice(unitUSD), ratioLabel, formatRatio(groupRatio), vpart, quotaUSD(actual)))
			}
			delta, label := actual-pre, "补扣"
			if delta < 0 {
				label, delta = "退款", -delta
			}
			lines = append(lines, fmt.Sprintf("预扣 %s → 实扣 %s，%s %s",
				quotaUSD(pre), quotaUSD(actual), label, quotaUSD(delta)))
		}
		if info.TaskId != "" {
			lines = append(lines, "任务 "+info.TaskId)
		}
		return strings.Join(lines, "\n"), true
	}

	// 预扣阶段：估算金额 + 单价与倍率，结算前仅供参考。
	lines = append(lines, "任务预扣费（估算，任务完成后按实际用量结算，多退少补）")
	lines = append(lines, "预扣金额 "+quotaUSD(float64(log.Quota)))
	if info.VideoUnitPrice > 0 {
		unitLine := fmt.Sprintf("单价 $%s / 1M tokens", formatPrice(unitUSD))
		if info.VideoResolution != "" {
			hasInput := "不含视频输入"
			if info.VideoHasInput {
				hasInput = "含视频输入"
			}
			unitLine += fmt.Sprintf("（%s，%s）", info.VideoResolution, hasInput)
		}
		lines = append(lines, fmt.Sprintf("%s × %s %s%s", unitLine, ratioLabel, formatRatio(groupRatio), vpart))
	} else if info.ModelPrice > 0 {
		lines = append(lines, fmt.Sprintf("按次价格 $%s × %s %s", formatPrice(info.ModelPrice), ratioLabel, formatRatio(groupRatio)))
	} else if info.ModelRatio > 0 {
		lines = append(lines, fmt.Sprintf("单价 $%s / 1M tokens × %s %s%s", formatPrice(unitUSD), ratioLabel, formatRatio(groupRatio), vpart))
	}
	if info.TaskId != "" {
		lines = append(lines, "任务 "+info.TaskId)
	}
	lines = append(lines, billingDisclaimer)
	return strings.Join(lines, "\n"), true
}

// formatPrice renders a USD amount the way the frontend does — six fractional
// digits — so the export and the page match character-for-character.
func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

// formatRatio drops trailing zeros so common values like 4.27 stay short
// instead of becoming 4.270000.
func formatRatio(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// logTypeLabel maps the integer Log.Type enum to the short Chinese labels
// that match the dropdown on the log page filter.
func logTypeLabel(t int) string {
	switch t {
	case model.LogTypeTopup:
		return "充值"
	case model.LogTypeConsume:
		return "消费"
	case model.LogTypeManage:
		return "管理"
	case model.LogTypeSystem:
		return "系统"
	case model.LogTypeError:
		return "错误"
	case model.LogTypeRefund:
		return "退款"
	default:
		return "未知"
	}
}

// resolveExportColumns picks the column set for one export. It accepts the
// raw `columns` query param (comma-separated frontend keys), drops anything
// not on the allow-list, dedupes, and always appends billing + request_id at
// the end. An empty/all-filtered input falls back to logExportDefaultColumnKeys.
func resolveExportColumns(raw string) []logExportColumn {
	keys := strings.Split(raw, ",")
	seen := make(map[string]bool, len(keys))
	result := make([]logExportColumn, 0, len(keys)+2)

	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		col, ok := logExportColumnMap[k]
		if !ok {
			continue
		}
		seen[k] = true
		result = append(result, col)
	}

	if len(result) == 0 {
		for _, k := range logExportDefaultColumnKeys {
			if !seen[k] {
				seen[k] = true
				result = append(result, logExportColumnMap[k])
			}
		}
	}

	// Insert cache columns after "completion" and before "cost".
	// Find the insertion point: right after "completion", or right before "cost",
	// or at the end of user-selected columns if neither is present.
	insertIdx := -1
	for i, col := range result {
		if col.key == "completion" {
			insertIdx = i + 1
			break
		}
	}
	if insertIdx == -1 {
		for i, col := range result {
			if col.key == "cost" {
				insertIdx = i
				break
			}
		}
	}
	if insertIdx == -1 {
		insertIdx = len(result)
	}
	// Insert cacheReadColumn and cacheCreationColumn at insertIdx
	forced := []logExportColumn{cacheReadColumn, cacheCreationColumn}
	result = append(result[:insertIdx], append(forced, result[insertIdx:]...)...)

	// Append billing and request_id at the very end
	result = append(result, billingColumn, requestIdColumn)
	return result
}

// cellValue returns the value to write for a given column on a given log row.
// Numeric tokens are returned as int (so Excel treats them as numbers); the
// rest are strings. The billing column is the only one that contains \n —
// rendering callers must apply a WrapText style to it.
// cacheCreationTokensOf 归一化单条日志的缓存创建 tokens。
//
// `cache_creation_tokens` 本身就是总量（Anthropic 的 cache_creation_input_tokens
// 恒等于 ephemeral_5m + ephemeral_1h，见 dto/claude.go），`_5m`/`_1h` 是它的拆分
// 项——三者相加会在存在拆分时翻倍。计费侧 service/text_quota.go 的
// cacheWriteTokensTotal 用的是 max(总量, 5m+1h)，并把结果写进 `cache_write_tokens`；
// 这里优先读该字段，缺失的旧日志退回同样的 max 语义。
func cacheCreationTokensOf(info *logPricingInfo) int {
	if info == nil {
		return 0
	}
	if info.CacheWriteTokens > 0 {
		return info.CacheWriteTokens
	}
	if split := info.CacheCreationTokens5m + info.CacheCreationTokens1h; split > info.CacheCreationTokens {
		return split
	}
	return info.CacheCreationTokens
}

// promptTokensExcludingCache 把 Log.PromptTokens 归一化为「非缓存输入 tokens」，
// 使 输入+输出+缓存读取+缓存创建 恒等于总 tokens，且跨渠道可加。
//
// Claude 语义（usage_semantic=anthropic / claude=true）的 prompt_tokens 与缓存
// 互斥，原样返回；OpenAI/Gemini/DeepSeek 等的 prompt_tokens 已含缓存读取，需减去
// （与计费侧 service/text_quota.go 的 baseTokens 分叉口径一致）。缓存创建对后者
// 不计入 prompt_tokens，故不减。裁剪到 0 以防脏日志导致负数。
func promptTokensExcludingCache(promptTokens int, cacheReadTokens int, info *logPricingInfo) int {
	if info != nil && (info.UsageSemantic == "anthropic" || info.Claude) {
		return promptTokens
	}
	if n := promptTokens - cacheReadTokens; n > 0 {
		return n
	}
	return 0
}

// getCacheTokensFromOther extracts a single integer field from Log.Other JSON.
func getCacheTokensFromOther(log *model.Log, field string) int {
	if log == nil || strings.TrimSpace(log.Other) == "" {
		return 0
	}
	var m map[string]any
	if err := common.UnmarshalJsonStr(log.Other, &m); err != nil {
		return 0
	}
	v, ok := m[field]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	default:
		return 0
	}
}

// getCacheCreationTokensFromOther returns a single normalized "total cache
// creation tokens" number for a log. See cacheCreationTokensOf for why the
// variants must not simply be summed.
func getCacheCreationTokensFromOther(log *model.Log) int {
	return cacheCreationTokensOf(parseLogPricingInfo(log))
}

func cellValue(col logExportColumn, log *model.Log) any {
	switch col.key {
	case "time":
		return time.Unix(log.CreatedAt, 0).Format("2006-01-02 15:04:05")
	case "username":
		return log.Username
	case "token":
		return log.TokenName
	case "group":
		return log.Group
	case "type":
		return logTypeLabel(log.Type)
	case "model":
		return log.ModelName
	case "use_time":
		return log.UseTime
	case "prompt":
		return log.PromptTokens
	case "completion":
		return log.CompletionTokens
	case "cache_read":
		return getCacheTokensFromOther(log, "cache_tokens")
	case "cache_creation":
		return getCacheCreationTokensFromOther(log)
	case "cost":
		return "$" + formatPrice(float64(log.Quota)/common.QuotaPerUnit)
	case "billing":
		return buildBillingText(log)
	case "request_id":
		return log.RequestId
	default:
		return ""
	}
}

// excelSingleSheetSoftCap caps rows-per-sheet below Excel's 1,048,576 hard
// limit. When same-day data exceeds this, the sheet rolls to "<day> (2)",
// "<day> (3)", and so on. Lower than the hard cap so a small accounting buffer
// remains and total-row counters never trip the platform limit.
const excelSingleSheetSoftCap = 1000000

// excelSheetWriter accumulates log rows into an in-progress xlsx, rolling to
// a new sheet whenever the calendar day of the next log differs from the
// current sheet, or when the current sheet hits excelSingleSheetSoftCap.
// Sheets are named by the date ("2026-06-01") with a "(2)", "(3)", ... suffix
// for same-day overflow. Rows MUST arrive ordered by created_at DESC, id DESC
// (the order streamLogsForExport yields) — otherwise a day's sheets will be
// split into discontiguous fragments. At any moment only ONE StreamWriter is
// active: previous sheets are Flush()ed before the next NewStreamWriter call,
// which is what keeps excelize memory flat across many sheets.
type excelSheetWriter struct {
	f             *excelize.File
	columns       []logExportColumn
	headerRow     []any // pre-built header cells reused across sheets
	billingColIdx int
	wrapStyleID   int

	sw          *excelize.StreamWriter
	curDay      string
	daySuffix   int
	rowInSheet  int // 1-indexed; header occupies row 1, data starts at row 2
	totalSheets int
	totalRows   int
}

func newExcelSheetWriter(f *excelize.File, columns []logExportColumn, headerStyleID, wrapStyleID int) *excelSheetWriter {
	billingIdx := -1
	headerRow := make([]any, len(columns))
	for i, col := range columns {
		headerRow[i] = excelize.Cell{Value: col.header, StyleID: headerStyleID}
		if col.key == "billing" {
			billingIdx = i
		}
	}
	return &excelSheetWriter{
		f:             f,
		columns:       columns,
		headerRow:     headerRow,
		billingColIdx: billingIdx,
		wrapStyleID:   wrapStyleID,
	}
}

func (w *excelSheetWriter) writeBatch(logs []*model.Log) error {
	for _, log := range logs {
		day := time.Unix(log.CreatedAt, 0).Format("2006-01-02")
		needRoll := w.sw == nil || day != w.curDay || w.rowInSheet >= excelSingleSheetSoftCap
		if needRoll {
			if err := w.rollSheet(day); err != nil {
				return err
			}
		}
		row := make([]any, len(w.columns))
		for i, col := range w.columns {
			val := cellValue(col, log)
			if i == w.billingColIdx {
				row[i] = excelize.Cell{Value: val, StyleID: w.wrapStyleID}
			} else {
				row[i] = val
			}
		}
		cellName, err := excelize.CoordinatesToCellName(1, w.rowInSheet+1)
		if err != nil {
			return err
		}
		if err = w.sw.SetRow(cellName, row); err != nil {
			return err
		}
		w.rowInSheet++
		w.totalRows++
	}
	return nil
}

func (w *excelSheetWriter) rollSheet(day string) error {
	if w.sw != nil {
		if err := w.sw.Flush(); err != nil {
			return err
		}
	}
	suffix := 1
	if day == w.curDay {
		suffix = w.daySuffix + 1
	}
	name := day
	if suffix > 1 {
		name = fmt.Sprintf("%s (%d)", day, suffix)
	}
	idx, err := w.f.NewSheet(name)
	if err != nil {
		return err
	}
	if w.totalSheets == 0 {
		w.f.SetActiveSheet(idx)
	}
	sw, err := w.f.NewStreamWriter(name)
	if err != nil {
		return err
	}
	for i, col := range w.columns {
		if err = sw.SetColWidth(i+1, i+1, col.width); err != nil {
			return err
		}
	}
	if err = sw.SetRow("A1", w.headerRow); err != nil {
		return err
	}
	w.sw = sw
	w.curDay = day
	w.daySuffix = suffix
	w.rowInSheet = 1 // header occupies row 1
	w.totalSheets++
	return nil
}

func (w *excelSheetWriter) finish() error {
	if w.sw == nil {
		return nil
	}
	return w.sw.Flush()
}

// writeBillExcelStream consumes log rows from run() via a callback, writing
// them into a multi-sheet xlsx (one sheet per calendar day). truncated == true
// triggers an X-Export-Truncated header so the frontend can warn the user.
// Empty result sets still produce a valid xlsx (single "账单" sheet, headers
// only) to avoid Excel "no sheet" errors.
func writeBillExcelStream(c *gin.Context, columns []logExportColumn, run func(consume func([]*model.Log) error) (bool, error)) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "left", WrapText: true},
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	wrapStyle, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "top", Horizontal: "left", WrapText: true},
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	writer := newExcelSheetWriter(f, columns, headerStyle, wrapStyle)

	truncated, err := run(func(batch []*model.Log) error {
		return writer.writeBatch(batch)
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err = writer.finish(); err != nil {
		common.ApiError(c, err)
		return
	}

	// Empty result — emit a single placeholder sheet with just headers so the
	// downloaded file isn't an empty xlsx (which Excel rejects on open).
	if writer.totalSheets == 0 {
		idx, err := f.NewSheet("账单")
		if err != nil {
			common.ApiError(c, err)
			return
		}
		f.SetActiveSheet(idx)
		sw, err := f.NewStreamWriter("账单")
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for i, col := range columns {
			if err = sw.SetColWidth(i+1, i+1, col.width); err != nil {
				common.ApiError(c, err)
				return
			}
		}
		if err = sw.SetRow("A1", writer.headerRow); err != nil {
			common.ApiError(c, err)
			return
		}
		if err = sw.Flush(); err != nil {
			common.ApiError(c, err)
			return
		}
	}

	_ = f.DeleteSheet("Sheet1")

	if truncated {
		c.Writer.Header().Set("X-Export-Truncated", "1")
		c.Writer.Header().Set("X-Export-Max-Rows", strconv.Itoa(model.LogExportMaxRows("xlsx")))
	}
	filename := "bill-" + time.Now().Format("20060102-150405") + ".xlsx"
	c.Writer.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.WriteHeader(http.StatusOK)
	if err = f.Write(c.Writer); err != nil {
		// Headers already sent — best effort log; we can't switch to JSON now.
		common.SysError("failed to write export xlsx: " + err.Error())
	}
}

// writeBillCSVStream streams log rows as a single UTF-8 CSV with BOM. Unlike
// the Excel writer, this path commits the HTTP response headers BEFORE row
// data is known, so X-Export-Truncated can't be set after the fact. Plan B:
// always send X-Export-Max-Rows up front (so the frontend always knows the
// cap), and append a trailer line "# truncated_at=N" at file end when the cap
// is hit. The frontend reads both: header for the cap, trailer for the flag.
func writeBillCSVStream(c *gin.Context, columns []logExportColumn, run func(consume func([]*model.Log) error) (bool, error)) {
	maxRows := model.LogExportMaxRows("csv")
	filename := "bill-" + time.Now().Format("20060102-150405") + ".csv"
	c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Export-Max-Rows", strconv.Itoa(maxRows))
	c.Writer.WriteHeader(http.StatusOK)

	// UTF-8 BOM so Excel auto-detects encoding when double-clicking the CSV.
	if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		common.SysError("failed to write csv BOM: " + err.Error())
		return
	}

	w := csv.NewWriter(c.Writer)
	flusher, _ := c.Writer.(http.Flusher)

	headerRow := make([]string, len(columns))
	for i, col := range columns {
		headerRow[i] = col.header
	}
	if err := w.Write(headerRow); err != nil {
		common.SysError("failed to write csv header: " + err.Error())
		return
	}

	truncated, err := run(func(batch []*model.Log) error {
		for _, log := range batch {
			row := make([]string, len(columns))
			for i, col := range columns {
				row[i] = csvCellString(cellValue(col, log))
			}
			if werr := w.Write(row); werr != nil {
				return werr
			}
		}
		w.Flush()
		if werr := w.Error(); werr != nil {
			return werr
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})

	w.Flush()

	if err != nil {
		// Headers already sent — best-effort trailer. Frontend treats lines
		// starting with "# " as control comments rather than data rows.
		common.SysError("failed to write export csv: " + err.Error())
		_, _ = c.Writer.Write([]byte("# error=" + sanitizeTrailerValue(err.Error()) + "\n"))
		return
	}

	if truncated {
		// Trailer comment lets clients that can't read response headers (e.g.
		// reopened from disk) still discover that the file was truncated.
		_, _ = c.Writer.Write([]byte(fmt.Sprintf("# truncated_at=%d\n", maxRows)))
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// csvCellString renders an exported cell value as the string the CSV writer
// should encode. cellValue() returns either string or int (and Excel writes
// ints as numbers); CSV doesn't care about typing, so we stringify uniformly.
func csvCellString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return ""
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// sanitizeTrailerValue scrubs newlines from an error message so it can be
// embedded in a single trailer line without confusing CSV parsers.
func sanitizeTrailerValue(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// resolveExportFormat normalizes the ?format= query param to either "csv" or
// "xlsx" (default). Unknown values fall back to xlsx for backward compat with
// older frontends that don't send the param.
func resolveExportFormat(c *gin.Context) string {
	format := strings.ToLower(strings.TrimSpace(c.Query("format")))
	if format == "csv" {
		return "csv"
	}
	return "xlsx"
}

// dispatchExport picks the writer for the requested format and invokes run()
// with the per-format row cap. run() is a closure that the caller wraps around
// the model-layer streaming function (GetAllLogsForExport / GetUserLogsForExport).
func dispatchExport(c *gin.Context, columns []logExportColumn, format string, run func(maxRows int, consume func([]*model.Log) error) (bool, error)) {
	maxRows := model.LogExportMaxRows(format)
	wrappedRun := func(consume func([]*model.Log) error) (bool, error) {
		return run(maxRows, consume)
	}
	if format == "csv" {
		writeBillCSVStream(c, columns, wrappedRun)
	} else {
		writeBillExcelStream(c, columns, wrappedRun)
	}
}

// ExportAllLogs handles the admin bill export. Filter query params match
// /api/log/ exactly so the frontend can reuse its existing form-values
// serializer without translation. format=xlsx (default) or format=csv.
func ExportAllLogs(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	format := resolveExportFormat(c)
	columns := resolveExportColumns(c.Query("columns"))

	dispatchExport(c, columns, format, func(maxRows int, consume func([]*model.Log) error) (bool, error) {
		return model.GetAllLogsForExport(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, requestId, maxRows, func(batch []*model.Log) error {
			// seedance 单行化：结算/退款行剔除、预扣行净额化（与使用日志页一致）。
			model.EnrichSeedanceTaskLogs(batch, true)
			return consume(applySeedanceExportMerge(batch))
		})
	})
}

// ExportUserLogs handles the self-service bill export. It is gated by the
// admin-controlled LogExportEnabled toggle even when the user is authenticated.
func ExportUserLogs(c *gin.Context) {
	if !common.LogExportEnabled {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "导出功能已关闭"})
		return
	}
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	format := resolveExportFormat(c)
	columns := resolveExportColumns(c.Query("columns"))

	dispatchExport(c, columns, format, func(maxRows int, consume func([]*model.Log) error) (bool, error) {
		return model.GetUserLogsForExport(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, group, requestId, maxRows, func(batch []*model.Log) error {
			// seedance 单行化：self 路径不暴露上游任务 ID。
			model.EnrichSeedanceTaskLogs(batch, false)
			return consume(applySeedanceExportMerge(batch))
		})
	})
}
