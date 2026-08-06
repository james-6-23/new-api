package controller

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// costCubeKey 成本立方体分桶键：用户 × 模型 × 渠道 × 时间桶。
// 三个维度报表与总览趋势都从同一立方体折叠得出，保证口径一致。
type costCubeKey struct {
	UserId    int
	Username  string
	ModelName string
	ChannelId int
	// Bucket 时间桶标签，格式随粒度而变（见 costBucketLabel）：
	// 日粒度 "2006-01-02"、小时粒度 "2006-01-02 15"。服务器本地时区，与账单汇总一致。
	Bucket string
}

type costCubeRow struct {
	// Quota 净实付（含分组折扣，退款已冲减）；ListQuota 净刊例价金额（上游计费基数）。
	// 均为 quota 单位，除以 QuotaPerUnit 得 USD。
	Quota       float64
	ListQuota   float64
	RefundQuota float64 // 退款行 quota 正数累计（仅展示用，净额已含在 Quota/ListQuota）
	// PromptTokens 为「非缓存输入」（已按 usage 语义扣除缓存读取），与
	// CacheReadTokens/CacheCreationTokens 互不重叠，四项可直接相加得总 tokens。
	PromptTokens     int
	CompletionTokens int
	RequestCount     int // 消费且非 settle 补扣行（任务多行只按 pre_consume 计 1 次）

	ErrorCount          int     // LogTypeError 行计数（同一用户/模型/渠道/日桶）
	CacheReadTokens     int     // 累计缓存读取 tokens（消费非退款行）
	CacheCreationTokens int     // 累计缓存创建 tokens（归一化后的总量，非各变体相加）
	FrtSumMs            float64 // 首字延迟毫秒累加（仅 info.Frt > 0 的行）
	FrtCount            int     // 参与 FrtSumMs 累加的行数

	// QuotaByGroup 按「使用分组」（Log.Group，即令牌所属分组）累计的净实付额度，
	// 供 attachUserGroupRatios 精确查二维专属倍率 groupGroupRatio[用户分组][使用分组]
	// 并在跨多个使用分组时按额度加权。
	//
	// 刻意不进 costCubeKey：使用分组只被折扣展示消费，纳入键会让立方体行数按
	// 使用分组数翻倍，而三个维度报表都不按它折叠。
	QuotaByGroup map[string]float64
}

const (
	costGranularityHour = "hour"
	costGranularityDay  = "day"
	// costHourlyMaxRangeSeconds 自适应小时粒度的跨度上限：≤2 天走小时桶。
	// 单日查询（页面默认筛选就是「今天」）因此能出 24 点趋势，而不是一个孤点。
	costHourlyMaxRangeSeconds = int64(2 * 24 * 3600)
)

// normalizeCostGranularity 归一化粒度参数：显式 hour/day 直接采用，其余（含
// 空值与 "auto"）按时间跨度自适应。对齐 bill_summary 的 normalizeBillGranularity。
func normalizeCostGranularity(raw string, start, end int64) string {
	switch raw {
	case costGranularityHour, costGranularityDay:
		return raw
	}
	if end-start <= costHourlyMaxRangeSeconds {
		return costGranularityHour
	}
	return costGranularityDay
}

// costBucketLabel 返回时间戳所属的桶标签。分桶全在应用层做（time.Format），
// 不依赖任何 SQL 日期函数，天然兼容 SQLite/MySQL/PostgreSQL 三库。
func costBucketLabel(t time.Time, granularity string) string {
	if granularity == costGranularityHour {
		return t.Format("2006-01-02 15")
	}
	return t.Format("2006-01-02")
}

// costBucketStep 单个桶的时长，供补零时步进。
func costBucketStep(granularity string) time.Duration {
	if granularity == costGranularityHour {
		return time.Hour
	}
	return 24 * time.Hour
}

// costBucketTruncate 把时间戳对齐到所属桶的起点，供补零时生成连续序列。
func costBucketTruncate(t time.Time, granularity string) time.Time {
	if granularity == costGranularityHour {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

type costCube struct {
	rows        map[costCubeKey]*costCubeRow
	granularity string
}

// addGroupQuota 累计某使用分组下的净实付额度（退款传负数）。空分组名（旧日志未
// 记录 Group）跳过：无法归属到任何使用分组，计入会污染加权。
func (r *costCubeRow) addGroupQuota(group string, quota float64) {
	if group == "" {
		return
	}
	if r.QuotaByGroup == nil {
		r.QuotaByGroup = make(map[string]float64, 1)
	}
	r.QuotaByGroup[group] += quota
}

func newCostCube() *costCube {
	return newCostCubeWithGranularity(costGranularityDay)
}

func newCostCubeWithGranularity(granularity string) *costCube {
	return &costCube{rows: make(map[costCubeKey]*costCubeRow), granularity: granularity}
}

func (c *costCube) addBatch(logs []*model.Log) {
	for _, log := range logs {
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund && log.Type != model.LogTypeError {
			continue
		}
		key := costCubeKey{
			UserId:    log.UserId,
			Username:  log.Username,
			ModelName: log.ModelName,
			ChannelId: log.ChannelId,
			Bucket:    costBucketLabel(time.Unix(log.CreatedAt, 0), c.granularity),
		}
		row := c.rows[key]
		if row == nil {
			row = &costCubeRow{}
			c.rows[key] = row
		}
		if log.Type == model.LogTypeError {
			row.ErrorCount++
			continue
		}
		info := parseLogPricingInfo(log)
		listQ := logListQuota(log, info)
		if log.Type == model.LogTypeRefund {
			row.Quota -= float64(log.Quota)
			row.ListQuota -= listQ
			row.RefundQuota += float64(log.Quota)
			row.addGroupQuota(log.Group, -float64(log.Quota))
			continue
		}
		if !isSettleStageLog(info) {
			row.RequestCount++
		}
		row.Quota += float64(log.Quota)
		row.ListQuota += listQ
		row.addGroupQuota(log.Group, float64(log.Quota))
		cacheRead := 0
		if info != nil {
			cacheRead = info.CacheTokens
		}
		// PromptTokens 归一化为「非缓存输入」，四项 token 才能相加得总数且跨渠道可加。
		row.PromptTokens += promptTokensExcludingCache(log.PromptTokens, cacheRead, info)
		row.CompletionTokens += log.CompletionTokens
		row.CacheReadTokens += cacheRead
		row.CacheCreationTokens += cacheCreationTokensOf(info)
		if info != nil && info.Frt > 0 {
			row.FrtSumMs += info.Frt
			row.FrtCount++
		}
	}
}

const (
	costDimUser      = "user"
	costDimModel     = "model"
	costDimChannel   = "channel"
	costBreakdownCap = 100
)

// costMoney 金额与用量的汇总单元：USD 原始金额 + 按汇率/渠道倍率换算后的 CNY 金额。
type costMoney struct {
	RevenueUsd float64 `json:"revenue_usd"`
	RevenueCny float64 `json:"revenue_cny"`
	ListUsd    float64 `json:"list_usd"`
	CostCny    float64 `json:"cost_cny"`
	ProfitCny  float64 `json:"profit_cny"`
	ProfitRate float64 `json:"profit_rate"`
	RefundUsd  float64 `json:"refund_usd"`

	// PromptTokens 为「非缓存输入」tokens：与 CacheReadTokens/CacheCreationTokens
	// 互不重叠，四项相加恒等于 TotalTokens（前端悬浮明细依赖该恒等式）。
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	RequestCount     int `json:"request_count"`

	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	ErrorCount          int     `json:"error_count"`
	FrtSumMs            float64 `json:"frt_sum_ms"`
	FrtCount            int     `json:"frt_count"`
	SuccessRate         float64 `json:"success_rate"`
	CacheRate           float64 `json:"cache_rate"`
	AvgTtftMs           float64 `json:"avg_ttft_ms"`

	// EffectiveDiscount 实际生效折扣 = 收入$ ÷ 刊例$：该商本身就是本行区间内按
	// 额度加权的真实折扣，专属倍率、跨分组混用、区间内倍率变更全部自动正确，
	// 无需查任何配置。刊例为 0（免费或未定价模型）时商无意义，Known=false。
	EffectiveDiscount      float64 `json:"effective_discount"`
	EffectiveDiscountKnown bool    `json:"effective_discount_known"`
}

// costBreakdownRow 维度折叠行下的子明细（折叠掉本维度与 Day，保留其余两个维度）。
// CostMode/CostRatio/CostDiscount/EffectiveRatio 为该明细行所属渠道的计价配置，
// 供前端在明细行直接展示"这一笔成本是按哪个倍率/折扣算出来的"；渠道身份被合并
// （按模型汇总）时这些字段无意义，由前端按 channel_id 是否存在决定是否渲染。
// UserGroup/GroupRatio/GroupRatioKnown/GroupRatioSpecial 为该明细行所属用户的
// 当前分组折扣（专属优先），由 attachUserGroupRatios 统一填充。
type costBreakdownRow struct {
	Username    string `json:"username,omitempty"`
	ModelName   string `json:"model_name,omitempty"`
	ChannelId   int    `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`

	CostMode       string  `json:"cost_mode,omitempty"`
	CostRatio      float64 `json:"cost_ratio,omitempty"`
	CostDiscount   float64 `json:"cost_discount,omitempty"`
	EffectiveRatio float64 `json:"effective_ratio,omitempty"`

	UserGroup         string  `json:"user_group,omitempty"`
	GroupRatio        float64 `json:"group_ratio,omitempty"`
	GroupRatioKnown   bool    `json:"group_ratio_known,omitempty"`
	GroupRatioSpecial bool    `json:"group_ratio_special,omitempty"`
	GroupRatioMixed   bool    `json:"group_ratio_mixed,omitempty"`
	// UsingGroupQuota 仅服务端内部使用（按使用分组拆分的净额度，用于解析配置
	// 折扣），不下发前端。
	UsingGroupQuota map[string]float64 `json:"-"`
	costMoney
}

// costDimensionRow 单个维度（user/model/channel）折叠后的一行；同一时刻只有对应
// 维度的身份字段被填充。
type costDimensionRow struct {
	UserId      int    `json:"user_id,omitempty"`
	Username    string `json:"username,omitempty"`
	ModelName   string `json:"model_name,omitempty"`
	ChannelId   int    `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
	// CostRatio 为渠道原始配置倍率（ratio 模式即生效倍率；discount 模式恒为 0，
	// 生效倍率见 EffectiveRatio）。
	CostRatio float64 `json:"cost_ratio,omitempty"`
	Priced    bool    `json:"priced"`
	UserCount int     `json:"user_count,omitempty"`
	costMoney
	Breakdown          []costBreakdownRow `json:"breakdown,omitempty"`
	BreakdownTruncated int                `json:"breakdown_truncated,omitempty"`

	// 以下字段仅渠道维度（costDimChannel）填充，供前端按计价模式渲染。
	CostMode       string                   `json:"cost_mode,omitempty"`
	CostDiscount   float64                  `json:"cost_discount,omitempty"`
	EffectiveRatio float64                  `json:"effective_ratio,omitempty"`
	IsAggregator   bool                     `json:"is_aggregator,omitempty"`
	SubSuppliers   []dto.ChannelSubSupplier `json:"sub_suppliers,omitempty"`

	// 以下字段仅用户维度（costDimUser）填充：用户"当前"所属分组及该分组的配置
	// 折扣。注意这是查询时刻的**配置快照**，实际生效折扣见 costMoney.EffectiveDiscount
	// （收入÷刊例，按额度加权）——用户期间换过组或倍率被调过时两者会不一致，
	// 前端悬浮说明里已注明口径并在不一致时给出提示。
	// GroupRatioSpecial 表示命中了「专属倍率」（GroupGroupRatio 二维配置）；
	// GroupRatioMixed 表示区间内跨多个使用分组，GroupRatio 为按额度加权值。
	UserGroup         string  `json:"user_group,omitempty"`
	GroupRatio        float64 `json:"group_ratio,omitempty"`
	GroupRatioKnown   bool    `json:"group_ratio_known,omitempty"`
	GroupRatioSpecial bool    `json:"group_ratio_special,omitempty"`
	GroupRatioMixed   bool    `json:"group_ratio_mixed,omitempty"`
	// UsingGroupQuota 仅服务端内部使用，不下发前端（见 costBreakdownRow 同名字段）。
	UsingGroupQuota map[string]float64 `json:"-"`
}

// costMoneyFromRow 统一金额换算：
// revenue_usd = 实付/QuotaPerUnit；cost_cny = 刊例USD × 渠道倍率；
// profit = revenue_cny − cost_cny；收入为 0 时利润率置 0。
// 同时派生 v2 指标：total_tokens、success_rate、cache_rate、avg_ttft_ms
// （公式与零分母兜底规则见 deriveRates）。
func costMoneyFromRow(r *costCubeRow, ratio, exchangeRate float64) costMoney {
	m := costMoney{
		RevenueUsd:          roundTo6(r.Quota / common.QuotaPerUnit),
		ListUsd:             roundTo6(r.ListQuota / common.QuotaPerUnit),
		RefundUsd:           roundTo6(r.RefundQuota / common.QuotaPerUnit),
		PromptTokens:        r.PromptTokens,
		CompletionTokens:    r.CompletionTokens,
		RequestCount:        r.RequestCount,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		ErrorCount:          r.ErrorCount,
		FrtSumMs:            r.FrtSumMs,
		FrtCount:            r.FrtCount,
	}
	m.RevenueCny = roundTo6(m.RevenueUsd * exchangeRate)
	m.CostCny = roundTo6(m.ListUsd * ratio)
	m.ProfitCny = roundTo6(m.RevenueCny - m.CostCny)
	if m.RevenueCny != 0 {
		m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
	}
	m.deriveRates()
	return m
}

// deriveRates 重新计算 v2 派生指标。add() 汇总原始字段后必须重新调用本方法，
// 不能直接对派生字段做加法。
//
//	total_tokens = 非缓存输入 + 输出 + 缓存读取 + 缓存创建
//	cache_rate   = 缓存读取 / 总输入（非缓存输入 + 缓存读取 + 缓存创建）
//	effective_discount = 收入$ / 刊例$（实际生效折扣，见字段注释）
//
// PromptTokens 在采集时已归一化为「非缓存输入」（见 promptTokensExcludingCache），
// 故四项互不重叠、直接相加即为总数，且 Claude 与 OpenAI 语义的行可混合累加。
// 命中率分母取总输入而非总 tokens——输出 tokens 永远不可能命中缓存，计入分母会
// 系统性压低命中率。零分母兜底：success_rate → 1（无请求视为无失败），其余 → 0。
func (m *costMoney) deriveRates() {
	inputTokens := m.PromptTokens + m.CacheReadTokens + m.CacheCreationTokens
	m.TotalTokens = inputTokens + m.CompletionTokens
	if m.RequestCount+m.ErrorCount == 0 {
		m.SuccessRate = 1
	} else {
		m.SuccessRate = roundTo6(float64(m.RequestCount) / float64(m.RequestCount+m.ErrorCount))
	}
	if inputTokens == 0 {
		m.CacheRate = 0
	} else {
		m.CacheRate = roundTo6(float64(m.CacheReadTokens) / float64(inputTokens))
	}
	if m.FrtCount == 0 {
		m.AvgTtftMs = 0
	} else {
		m.AvgTtftMs = roundTo6(m.FrtSumMs / float64(m.FrtCount))
	}
	// 汇总后按「总收入 ÷ 总刊例」重算，而非对各行折扣取平均——加权口径才是本行
	// 真实付出的折扣。
	if m.ListUsd == 0 {
		m.EffectiveDiscount, m.EffectiveDiscountKnown = 0, false
	} else {
		m.EffectiveDiscount = roundTo6(m.RevenueUsd / m.ListUsd)
		m.EffectiveDiscountKnown = true
	}
}

func (m *costMoney) add(o costMoney) {
	m.RevenueUsd = roundTo6(m.RevenueUsd + o.RevenueUsd)
	m.RevenueCny = roundTo6(m.RevenueCny + o.RevenueCny)
	m.ListUsd = roundTo6(m.ListUsd + o.ListUsd)
	m.CostCny = roundTo6(m.CostCny + o.CostCny)
	m.ProfitCny = roundTo6(m.ProfitCny + o.ProfitCny)
	m.RefundUsd = roundTo6(m.RefundUsd + o.RefundUsd)
	m.PromptTokens += o.PromptTokens
	m.CompletionTokens += o.CompletionTokens
	m.RequestCount += o.RequestCount
	m.CacheReadTokens += o.CacheReadTokens
	m.CacheCreationTokens += o.CacheCreationTokens
	m.ErrorCount += o.ErrorCount
	m.FrtSumMs = roundTo6(m.FrtSumMs + o.FrtSumMs)
	m.FrtCount += o.FrtCount
	if m.RevenueCny != 0 {
		m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
	} else {
		m.ProfitRate = 0
	}
	m.deriveRates()
}

// effectiveChannelRatio 依据计价方式换算有效倍率（CNY:USD）：
// ratio 模式取 CostRatio；discount 模式取 CostDiscount×exchangeRate；未知渠道/未填 → 0。
func effectiveChannelRatio(channels map[int]*model.ChannelCostInfo, id int, exchangeRate float64) (ratio float64, name string) {
	ci := channels[id]
	if ci == nil {
		return 0, ""
	}
	if ci.CostMode == "discount" {
		return ci.CostDiscount * exchangeRate, ci.Name
	}
	return ci.CostRatio, ci.Name
}

// userDiscount 单个用户的当前折扣快照：分组名 + 生效倍率 + 是否命中专属倍率。
type userDiscount struct {
	group   string
	ratio   float64
	known   bool
	special bool
	mixed   bool // 区间内跨多个使用分组，ratio 为按额度加权值
}

// resolveUserDiscount 按与计费链路 HandleGroupRatio（relay/helper/price.go:53）
// 一致的优先级取用户当前配置折扣：二维专属倍率 groupGroupRatio[用户分组][使用分组]
// 优先，其次一维分组倍率，都没配置时 known=false。
//
// usingGroupQuota 为该用户区间内按「使用分组」拆分的净实付额度（来自
// costCubeRow.QuotaByGroup）：
//   - 单一使用分组 → 精确查该组合的专属倍率；
//   - 多个使用分组 → 逐组解析后按额度加权，mixed=true 供前端标注；
//   - 为空（旧日志无 Group）→ 退化为「用户使用自身分组」的口径。
//
// 与计费不同的是这里不兜底 1——报表上"未配置"和"倍率恰好是 1"必须可区分。
func resolveUserDiscount(group string, usingGroupQuota map[string]float64, groupRatios map[string]float64) userDiscount {
	d := userDiscount{group: group}

	// 单组配置解析：专属倍率优先，其次一维分组倍率。
	lookupOne := func(usingGroup string) (ratio float64, known, special bool) {
		if special, ok := ratio_setting.GetGroupGroupRatio(group, usingGroup); ok && special >= 0 {
			return special, true, true
		}
		if r, ok := groupRatios[usingGroup]; ok {
			return r, true, false
		}
		return 0, false, false
	}

	// 只保留正额度分组：净额为 0 或负（全额退款）不应参与加权。
	positive := make(map[string]float64, len(usingGroupQuota))
	var totalQuota float64
	for g, q := range usingGroupQuota {
		if q > 0 {
			positive[g] = q
			totalQuota += q
		}
	}

	if totalQuota <= 0 {
		// 无可用使用分组信息：退回「用户使用自身分组」口径。
		d.ratio, d.known, d.special = lookupOne(group)
		return d
	}

	var weighted float64
	var knownQuota float64
	for g, q := range positive {
		ratio, known, special := lookupOne(g)
		if !known {
			continue
		}
		weighted += ratio * q
		knownQuota += q
		d.known = true
		if special {
			d.special = true
		}
	}
	if !d.known {
		return d
	}
	// 加权分母只算「已配置倍率」的那部分额度，避免未配置分组把折扣稀释成偏低值。
	d.ratio = roundTo6(weighted / knownQuota)
	d.mixed = len(positive) > 1
	return d
}

// attachUserGroupRatios 给维度行与 breakdown 明细行补上"当前分组 + 配置折扣"。
// groups 为 username -> 当前分组。
//   - 父行：仅用户维度有单一用户（Username 非空），models/channels 父行留空；
//   - 明细行：models/channels 维度的明细行自带 Username；users 维度的明细行
//     Username 已被折叠，回退用父行的用户名（同一用户）。
//
// 折扣按各行自己的 UsingGroupQuota（区间内按使用分组拆分的额度）解析，因此同一
// 用户在不同渠道/模型行上若使用了不同分组的令牌，各行折扣可以不同。
//
// 用户已删除（groups 里查不到）或分组未配置倍率时 GroupRatioKnown 置 false，
// 由前端显示"—"而不是误显示 1（未配置 ≠ 不打折）。
func attachUserGroupRatios(rows []costDimensionRow, groups map[string]string) {
	if len(rows) == 0 {
		return
	}
	groupRatios := ratio_setting.GetGroupRatioCopy()
	resolve := func(username string, usingGroupQuota map[string]float64) (userDiscount, bool) {
		if username == "" {
			return userDiscount{}, false
		}
		group, ok := groups[username]
		if !ok {
			return userDiscount{}, false
		}
		return resolveUserDiscount(group, usingGroupQuota, groupRatios), true
	}
	apply := func(target *costBreakdownRow, d userDiscount) {
		target.UserGroup = d.group
		target.GroupRatio = d.ratio
		target.GroupRatioKnown = d.known
		target.GroupRatioSpecial = d.special
		target.GroupRatioMixed = d.mixed
	}
	for i := range rows {
		if d, ok := resolve(rows[i].Username, rows[i].UsingGroupQuota); ok {
			rows[i].UserGroup = d.group
			rows[i].GroupRatio = d.ratio
			rows[i].GroupRatioKnown = d.known
			rows[i].GroupRatioSpecial = d.special
			rows[i].GroupRatioMixed = d.mixed
		}
		for j := range rows[i].Breakdown {
			b := &rows[i].Breakdown[j]
			username := b.Username
			usingGroups := b.UsingGroupQuota
			if username == "" {
				// users 维度：明细行的用户身份被折叠，归属父行用户。
				username = rows[i].Username
			}
			if len(usingGroups) == 0 {
				usingGroups = rows[i].UsingGroupQuota
			}
			if d, ok := resolve(username, usingGroups); ok {
				apply(b, d)
			}
		}
	}
}

// foldCostCube 沿 dim 折叠立方体。每行金额先在 (渠道) 粒度换算再累加，
// 保证不同倍率渠道混在同一用户/模型行时成本正确。
func foldCostCube(cube *costCube, dim string, channels map[int]*model.ChannelCostInfo, exchangeRate float64) []costDimensionRow {
	type groupKey struct {
		UserId    int
		Username  string
		ModelName string
		ChannelId int
	}
	// costSubAgg 明细子桶：金额 + 按使用分组拆分的额度（后者供折扣解析）。
	type costSubAgg struct {
		money      costMoney
		groupQuota map[string]float64
	}
	// mergeGroupQuota 把一条立方体行的使用分组额度并入目标累加器。
	mergeGroupQuota := func(dst *map[string]float64, src map[string]float64) {
		if len(src) == 0 {
			return
		}
		if *dst == nil {
			*dst = make(map[string]float64, len(src))
		}
		for g, q := range src {
			(*dst)[g] += q
		}
	}
	groups := make(map[groupKey]*costDimensionRow)
	breakdowns := make(map[groupKey]map[costCubeKey]*costSubAgg) // sub-agg per group
	userSets := make(map[groupKey]map[int]bool)

	for k, r := range cube.rows {
		ratio, chName := effectiveChannelRatio(channels, k.ChannelId, exchangeRate)
		m := costMoneyFromRow(r, ratio, exchangeRate)

		var gk groupKey
		switch dim {
		case costDimUser:
			gk = groupKey{UserId: k.UserId, Username: k.Username}
		case costDimModel:
			gk = groupKey{ModelName: k.ModelName}
		default:
			gk = groupKey{ChannelId: k.ChannelId}
		}
		row := groups[gk]
		if row == nil {
			row = &costDimensionRow{UserId: gk.UserId, Username: gk.Username, ModelName: gk.ModelName, ChannelId: gk.ChannelId}
			if dim == costDimChannel {
				row.ChannelName = chName
				row.EffectiveRatio = ratio
				row.Priced = ratio > 0
				if ci := channels[gk.ChannelId]; ci != nil {
					row.CostRatio = ci.CostRatio
					row.CostMode = ci.CostMode
					row.CostDiscount = ci.CostDiscount
					row.IsAggregator = ci.IsAggregator
					row.SubSuppliers = ci.SubSuppliers
				}
				userSets[gk] = make(map[int]bool)
			}
			groups[gk] = row
		}
		row.costMoney.add(m)
		mergeGroupQuota(&row.UsingGroupQuota, r.QuotaByGroup)
		if dim == costDimChannel {
			userSets[gk][k.UserId] = true
		}

		// breakdown 子键：折叠掉本维度与 Bucket，保留其余两个维度
		bk := k
		bk.Bucket = ""
		switch dim {
		case costDimUser:
			bk.UserId, bk.Username = 0, ""
		case costDimModel:
			bk.ModelName = ""
		default:
			bk.ChannelId = 0
		}
		if breakdowns[gk] == nil {
			breakdowns[gk] = make(map[costCubeKey]*costSubAgg)
		}
		sub := breakdowns[gk][bk]
		if sub == nil {
			sub = &costSubAgg{}
			breakdowns[gk][bk] = sub
		}
		sub.money.add(m)
		mergeGroupQuota(&sub.groupQuota, r.QuotaByGroup)
	}

	rows := make([]costDimensionRow, 0, len(groups))
	for gk, row := range groups {
		if dim == costDimChannel {
			row.UserCount = len(userSets[gk])
		}
		// breakdown 排序取前 costBreakdownCap
		type bd struct {
			key costCubeKey
			sub *costSubAgg
		}
		bds := make([]bd, 0, len(breakdowns[gk]))
		for bk, sub := range breakdowns[gk] {
			bds = append(bds, bd{bk, sub})
		}
		sort.Slice(bds, func(i, j int) bool {
			a, b := bds[i], bds[j]
			if a.sub.money.RevenueCny != b.sub.money.RevenueCny {
				return a.sub.money.RevenueCny > b.sub.money.RevenueCny
			}
			if a.key.Username != b.key.Username {
				return a.key.Username < b.key.Username
			}
			if a.key.ModelName != b.key.ModelName {
				return a.key.ModelName < b.key.ModelName
			}
			return a.key.ChannelId < b.key.ChannelId
		})
		if len(bds) > costBreakdownCap {
			row.BreakdownTruncated = len(bds) - costBreakdownCap
			bds = bds[:costBreakdownCap]
		}
		for _, b := range bds {
			effRatio, chName := effectiveChannelRatio(channels, b.key.ChannelId, exchangeRate)
			br := costBreakdownRow{
				Username: b.key.Username, ModelName: b.key.ModelName,
				ChannelId: b.key.ChannelId, ChannelName: chName,
				UsingGroupQuota: b.sub.groupQuota,
				costMoney:       b.sub.money,
			}
			// 渠道身份未被折叠（ChannelId != 0）时带上该渠道的计价配置，供前端在
			// 明细行展示成本倍率/折扣。按模型汇总的明细行 ChannelId 为 0，此时
			// 跨渠道混合，不存在单一倍率，字段留空由前端显示"—"。
			if b.key.ChannelId != 0 {
				br.EffectiveRatio = effRatio
				if ci := channels[b.key.ChannelId]; ci != nil {
					br.CostMode = ci.CostMode
					br.CostRatio = ci.CostRatio
					br.CostDiscount = ci.CostDiscount
				}
			}
			row.Breakdown = append(row.Breakdown, br)
		}
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.RevenueCny != b.RevenueCny {
			return a.RevenueCny > b.RevenueCny
		}
		if a.Username != b.Username {
			return a.Username < b.Username
		}
		if a.ModelName != b.ModelName {
			return a.ModelName < b.ModelName
		}
		return a.ChannelId < b.ChannelId
	})
	return rows
}
