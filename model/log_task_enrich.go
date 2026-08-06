package model

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// LogTaskInfo 是 seedance 视频任务预扣行的查询时增强载荷（gorm:"-"，不落库）。
// 前端据此渲染完成状态列、三态费用与详情区块；字段命名与两个前端的
// task_info 消费方保持一致。
type LogTaskInfo struct {
	Status         string  `json:"status"`
	Progress       string  `json:"progress,omitempty"`
	FailReason     string  `json:"fail_reason,omitempty"`
	TaskId         string  `json:"task_id,omitempty"`
	UpstreamTaskId string  `json:"upstream_task_id,omitempty"`
	PreQuota       int     `json:"pre_quota"`
	FinalQuota     int     `json:"final_quota"`
	OutputTokens   int     `json:"output_tokens,omitempty"`
	ResolutionTier string  `json:"resolution_tier,omitempty"`
	// Resolution/Ratio/DurationS 为用户请求的原始参数（"720p"/"16:9"/5），
	// 从 task.Properties.Input 解析（请求值优先，task.Data 兜底）。
	Resolution     string  `json:"resolution,omitempty"`
	Ratio          string  `json:"ratio,omitempty"`
	DurationS      int     `json:"duration_s,omitempty"`
	HasInput       bool    `json:"has_input"`
	EffectiveRatio float64 `json:"effective_ratio,omitempty"`
	IsUserRatio    bool    `json:"is_user_ratio,omitempty"`
}

// EnrichSeedanceTaskLogs 是 enrichSeedanceTaskLogs 的导出入口，供原始日志导出
// （controller）在流式批次上复用同一套单行化增强。
func EnrichSeedanceTaskLogs(logs []*Log, includeUpstreamId bool) {
	enrichSeedanceTaskLogs(logs, includeUpstreamId)
}

// enrichSeedanceTaskLogs 对页内 seedance 预扣行挂 task_info：
// 两次批量查询（tasks by task_id、兄弟日志 by request_id），无逐行 N+1。
// includeUpstreamId=false（self 路径）不暴露上游任务 ID。
func enrichSeedanceTaskLogs(logs []*Log, includeUpstreamId bool) {
	type candidate struct {
		log   *Log
		other map[string]interface{}
	}
	var cands []candidate
	taskIds := make([]string, 0)
	reqIds := make([]string, 0)
	for _, l := range logs {
		if l.Type != LogTypeConsume || !strings.Contains(l.ModelName, "seedance") {
			continue
		}
		if !strings.Contains(l.Other, `"billing_stage":"pre_consume"`) {
			continue
		}
		m, err := common.StrToMap(l.Other)
		if err != nil || m == nil {
			continue
		}
		cands = append(cands, candidate{log: l, other: m})
		if tid, _ := m["task_id"].(string); tid != "" {
			taskIds = append(taskIds, tid)
		}
		if l.RequestId != "" {
			reqIds = append(reqIds, l.RequestId)
		}
	}
	if len(cands) == 0 {
		return
	}

	taskById := make(map[string]*Task)
	if len(taskIds) > 0 {
		var tasks []*Task
		if err := DB.Where("task_id IN ?", taskIds).Find(&tasks).Error; err == nil {
			for _, tk := range tasks {
				taskById[tk.TaskID] = tk
			}
		}
	}

	// 兄弟结算/退款行（分页列表里被隐藏，这里按 request_id 补查取金额与原因）
	type sibling struct {
		typ     int
		quota   int
		content string
		other   map[string]interface{}
	}
	sibByReq := make(map[string][]sibling)
	if len(reqIds) > 0 {
		var sibs []*Log
		if err := LOG_DB.Where("request_id IN ? AND type IN ?", reqIds, []int{LogTypeConsume, LogTypeRefund}).Find(&sibs).Error; err == nil {
			for _, s := range sibs {
				if !strings.Contains(s.ModelName, "seedance") {
					continue
				}
				if !strings.Contains(s.Other, `"billing_stage":"settle"`) && !strings.Contains(s.Other, `"billing_stage":"refund"`) {
					continue
				}
				m, err := common.StrToMap(s.Other)
				if err != nil {
					m = nil
				}
				sibByReq[s.RequestId] = append(sibByReq[s.RequestId], sibling{typ: s.Type, quota: s.Quota, content: s.Content, other: m})
			}
		}
	}

	num := func(m map[string]interface{}, key string) float64 {
		if m == nil {
			return 0
		}
		if v, ok := m[key].(float64); ok {
			return v
		}
		return 0
	}

	for _, c := range cands {
		ti := &LogTaskInfo{Status: string(TaskStatusUnknown), PreQuota: c.log.Quota, FinalQuota: c.log.Quota}
		ti.TaskId, _ = c.other["task_id"].(string)
		ti.ResolutionTier, _ = c.other["video_resolution_tier"].(string)
		ti.HasInput, _ = c.other["video_has_input"].(bool)
		if ug := num(c.other, "user_group_ratio"); ug > 0 && ug != -1 {
			ti.EffectiveRatio, ti.IsUserRatio = ug, true
		} else if gr := num(c.other, "group_ratio"); gr > 0 {
			ti.EffectiveRatio = gr
		}

		hasSettle, hasRefund := false, false
		for _, s := range sibByReq[c.log.RequestId] {
			// 带 actual_quota(>0) 的退款是多退少补的结算调整（任务实为成功），
			// 其 Content 是"token重算：..."结算说明，不是失败原因。
			isSettleAdjust := num(s.other, "actual_quota") > 0
			if s.typ == LogTypeRefund {
				if isSettleAdjust {
					hasSettle = true
				} else {
					hasRefund = true
				}
				ti.FinalQuota -= s.quota
			} else {
				hasSettle = true
				ti.FinalQuota += s.quota
			}
			if vt := int(num(s.other, "video_tokens")); vt > 0 {
				ti.OutputTokens = vt
			}
			if ti.FailReason == "" {
				ti.FailReason, _ = s.other["reason"].(string)
			}
			// 全额失败退款的原因写在 Content 而非 other.reason，兜底补取。
			if ti.FailReason == "" && s.typ == LogTypeRefund && !isSettleAdjust {
				ti.FailReason = s.content
			}
		}
		if ti.FinalQuota < 0 {
			ti.FinalQuota = 0
		}

		if tk := taskById[ti.TaskId]; tk != nil {
			ti.Status = string(tk.Status)
			ti.Progress = tk.Progress
			if tk.FailReason != "" && tk.Status == TaskStatusFailure {
				ti.FailReason = tk.FailReason
			}
			if includeUpstreamId {
				ti.UpstreamTaskId = tk.GetUpstreamTaskID()
			}
			// 请求参数真实值优先（Properties.Input = 用户原始请求 JSON）
			res, ratio, dur := parseTaskRequestParams(tk.Properties.Input)
			ti.Resolution, ti.Ratio = res, ratio
			if dur > 0 {
				ti.DurationS = dur
			}
			metaDur, outTok := parseTaskVideoMeta(tk.Data)
			if ti.DurationS == 0 && metaDur > 0 {
				ti.DurationS = metaDur
			}
			if ti.OutputTokens == 0 && outTok > 0 {
				ti.OutputTokens = outTok
			}
		} else {
			// task 已被清理：由兄弟行推断终态
			switch {
			case hasRefund:
				ti.Status = string(TaskStatusFailure)
			case hasSettle:
				ti.Status = string(TaskStatusSuccess)
			}
		}
		// 成功任务绝不携带失败原因（结算说明不是失败）。
		if ti.Status == string(TaskStatusSuccess) {
			ti.FailReason = ""
		}
		c.log.TaskInfo = ti
	}
}

// parseTaskRequestParams 从 task.Properties.Input（用户原始请求 JSON）解析
// 请求的分辨率/宽高比/时长。支持顶层 resolution/ratio/duration/seconds、
// metadata.resolution/metadata.ratio 嵌套，以及 sora 形态的 size("1280x720")
// 推导（短边→"720p"、约分→"16:9"）。截断标记或解析失败返回零值。
func parseTaskRequestParams(input string) (resolution, ratio string, durationS int) {
	if strings.TrimSpace(input) == "" || strings.Contains(input, `"_truncated"`) {
		return "", "", 0
	}
	var m map[string]interface{}
	if err := common.UnmarshalJsonStr(input, &m); err != nil {
		return "", "", 0
	}
	str := func(src map[string]interface{}, key string) string {
		if src == nil {
			return ""
		}
		s, _ := src[key].(string)
		return strings.TrimSpace(s)
	}
	toInt := func(v interface{}) int {
		switch n := v.(type) {
		case float64:
			if n > 0 {
				return int(n)
			}
		case string:
			if p, err := strconv.Atoi(strings.TrimSpace(n)); err == nil && p > 0 {
				return p
			}
		}
		return 0
	}
	meta, _ := m["metadata"].(map[string]interface{})

	resolution = str(m, "resolution")
	if resolution == "" {
		resolution = str(meta, "resolution")
	}
	ratio = str(m, "ratio")
	if ratio == "" {
		ratio = str(meta, "ratio")
	}
	durationS = toInt(m["duration"])
	if durationS == 0 {
		durationS = toInt(m["seconds"])
	}
	if durationS == 0 && meta != nil {
		durationS = toInt(meta["duration"])
	}

	// sora 形态：size "1280x720" → 分辨率短边 + 约分宽高比
	if resolution == "" || ratio == "" {
		if size := str(m, "size"); size != "" {
			if w, h, ok := parseVideoSize(size); ok {
				if resolution == "" {
					short := h
					if w < h {
						short = w
					}
					resolution = strconv.Itoa(short) + "p"
				}
				if ratio == "" {
					g := gcdInt(w, h)
					ratio = fmt.Sprintf("%d:%d", w/g, h/g)
				}
			}
		}
	}
	return resolution, ratio, durationS
}

func parseVideoSize(size string) (w, h int, ok bool) {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(size)), "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func gcdInt(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

// parseTaskVideoMeta 从 task.Data(脱敏后的上游最终响应 JSON)尽力解析生成秒数与
// 输出 tokens。键名因 adaptor 而异，扫描顶层与一层嵌套的常见键；解析不到返回 0。
func parseTaskVideoMeta(data []byte) (durationS int, outputTokens int) {
	if len(data) == 0 {
		return 0, 0
	}
	var m map[string]interface{}
	if err := common.Unmarshal(data, &m); err != nil {
		return 0, 0
	}
	pickInt := func(m map[string]interface{}, keys ...string) int {
		for _, k := range keys {
			switch v := m[k].(type) {
			case float64:
				if v > 0 {
					return int(v)
				}
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
					return n
				}
			}
		}
		return 0
	}
	scan := func(m map[string]interface{}) {
		if durationS == 0 {
			durationS = pickInt(m, "duration", "seconds", "duration_seconds", "video_duration")
		}
		if outputTokens == 0 {
			outputTokens = pickInt(m, "completion_tokens", "total_tokens")
		}
	}
	scan(m)
	for _, v := range m {
		if sub, ok := v.(map[string]interface{}); ok {
			scan(sub)
		}
	}
	return durationS, outputTokens
}
