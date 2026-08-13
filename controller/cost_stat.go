package controller

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
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

	// CostCny 按版本逐条定价后累加：每条日志用 log.CreatedAt 当时生效的倍率换算，
	// 因此改价不会追溯改写历史成本。UnpricedListQuota 为无版本可用的刊例敞口
	// （用于 Priced 判定）——"查不到版本"不等于"上游免费"，必须单独暴露。
	CostCny           float64
	UnpricedListQuota float64

	// 用户折扣信号（从日志 other 取历史值，替代 QuotaByGroup 配置查询链路）
	DiscountListBasis  float64 // 有有效折扣信息的 listQ 之和（退款同步冲减）
	DiscountSpecialSum float64 // 命中专属倍率（user_group_ratio 有效）的 listQ 之和
	DiscountFirstRatio float64 // 第一个出现的折扣值（避免 float64 map key 精度问题）
	DiscountMixed      bool    // 区间内出现过 >1 个不同折扣值
	// DiscountTotalBasis 覆盖率分母：消费行 listQ 之和（无论有无折扣信息），减去
	// 携带折扣信息的退款行 listQ。不能直接用 ListQuota 当分母——两者加减规则不同。
	// 退款只在「有折扣信息」时才冲减本字段，与 DiscountListBasis 共用同一个门槛：
	// 退款日志的 other 往往比对应消费行贫瘠（见 addBatch 退款分支注释），若分母无条件
	// 冲减而分子有条件冲减，比值会越过 1。
	DiscountTotalBasis float64
	// RatioVersionSeen 记录本格子命中过的版本 EffectiveFrom 集合；len > 1 即区间内改过价。
	// 用 int64 作 map key 安全（EffectiveFrom 是精确整数，无浮点精度问题）。
	// 折叠时需把各格子的集合并起来再判定，不能只看单格——日粒度下改价前后会落在
	// 不同的时间桶里，每格各自只见一个版本。
	RatioVersionSeen map[int64]struct{}
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

func newCostCube() *costCube {
	return newCostCubeWithGranularity(costGranularityDay)
}

func newCostCubeWithGranularity(granularity string) *costCube {
	return &costCube{rows: make(map[costCubeKey]*costCubeRow), granularity: granularity}
}

// addBatch 处理一批日志，按 versions 逐条定价并累加到立方体。
// 定价下沉到逐条采集（而非折叠时统一乘当前倍率），是"改价不追溯改写历史"的关键。
// versions 为 nil 时退化为成本按 0 计，但正常调用应始终传入。
func (c *costCube) addBatch(logs []*model.Log, versions model.VersionMap) {
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

		// 版本定价：逐条按 log.CreatedAt 查当时生效版本。
		// 用 VersionAt 而非 RatioAt，以便记录命中的版本身份（跨版本判定）。
		ratio, priced := float64(0), false
		if versions != nil {
			if ver, ok := versions.VersionAt(log.ChannelId, log.CreatedAt); ok {
				ratio, priced = ver.EffectiveRatio()
				if priced {
					if row.RatioVersionSeen == nil {
						row.RatioVersionSeen = make(map[int64]struct{}, 1)
					}
					row.RatioVersionSeen[ver.EffectiveFrom] = struct{}{}
				}
			}
		}

		// 折扣覆盖率的分子与分母共用「本行是否带折扣信息」这一个门槛，消费加、退款减
		// 都不例外，否则退款会把比值推过 1（推导见退款分支注释）。
		histDiscount := historicalDiscount(info)
		isSpecial := info != nil && isValidGroupRatio(info.UserGroupRatio) && info.UserGroupRatio > 0

		if log.Type == model.LogTypeRefund {
			row.Quota -= float64(log.Quota)
			row.ListQuota -= listQ
			row.RefundQuota += float64(log.Quota)
			if priced && ratio > 0 {
				row.CostCny -= listQ / common.QuotaPerUnit * ratio
			} else {
				row.UnpricedListQuota -= listQ
			}
			// 折扣三元组要么整体冲减，要么整体不动：退款日志能知道的信息比消费行少——
			// MJ 构图失败退款（controller/midjourney.go）与任务退款在 BillingContext
			// 为 nil 时（service/task_billing.go）写的 other 里都没有 group_ratio，
			// 此时 histDiscount 为 0，logListQuota 也退化成实付额而非刊例额。分母若在
			// 这种行上照样冲减，就会用一个更小的、口径不同的数去减，把覆盖率推过 1。
			// 门槛统一后：分母 − 分子 ≡ Σ(无折扣信息的消费行 listQ) ≥ 0，覆盖率 ≤ 1
			// 由算式本身保证，与退款日志究竟携带多少信息无关。
			// 代价是这类退款不参与折扣口径：折扣信号描述的是「日志真正记下过的折扣事实」，
			// 而不是净额，所以全额退款后 discount_special 仍为 true——它与同样未被冲减的
			// 覆盖率分子分母彼此自洽，都在描述同一笔有信息的基数。
			if histDiscount > 0 {
				row.DiscountTotalBasis -= listQ
				row.DiscountListBasis -= listQ
				if isSpecial {
					row.DiscountSpecialSum -= listQ
				}
			}
			continue
		}

		if !isSettleStageLog(info) {
			row.RequestCount++
		}
		row.Quota += float64(log.Quota)
		row.ListQuota += listQ
		if priced && ratio > 0 {
			row.CostCny += listQ / common.QuotaPerUnit * ratio
		} else {
			row.UnpricedListQuota += listQ
		}

		// 用户折扣：从 other 取请求当时的历史值（UserGroupRatio 有效则取，否则 GroupRatio）
		// 分母在消费行上无条件累加：正是「有信息 / 全部」这个比值让覆盖率有意义，
		// 若这里也套上门槛，覆盖率会恒等于 1。
		row.DiscountTotalBasis += listQ
		if histDiscount > 0 {
			row.DiscountListBasis += listQ
			if isSpecial {
				row.DiscountSpecialSum += listQ
			}
			if row.DiscountFirstRatio == 0 {
				row.DiscountFirstRatio = histDiscount
			} else if !row.DiscountMixed {
				d := row.DiscountFirstRatio - histDiscount
				if d > 0.001 || d < -0.001 {
					row.DiscountMixed = true
				}
			}
		}

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
	// costUnpricedEpsilon 未定价敞口的判定阈值（quota 单位）。unpricedListQuota 是
	// 带符号累加值，不能用 == 0 判断：消费与退款的 listQ 不完全相等时相消会留下
	// ~1e-13 的浮点残差；退款未定价而对应消费已定价时（EffectiveRatio 对
	// CostRatio <= 0 返回 false，建版本只校验 effective_from）还会转负。这两种假象
	// 都会把已全部定价的区间误判成有敞口。1e-6 quota（≈2e-12 美元）远高于残差量级，
	// 又远低于任何真实敞口，故只有真敞口能越过它。
	costUnpricedEpsilon = 1e-6
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

	// EffectiveRatio 加权真实成本倍率 = CostCny / ListUsd。
	// 跨版本时为加权均值；RatioMixed=true 表示区间内改过价。
	EffectiveRatio      float64 `json:"effective_ratio,omitempty"`
	EffectiveRatioKnown bool    `json:"effective_ratio_known,omitempty"`
	RatioMixed          bool    `json:"ratio_mixed,omitempty"`
	// 用户折扣信号（从日志历史值派生，替代查配置）
	DiscountMixed   bool `json:"discount_mixed,omitempty"`
	DiscountSpecial bool `json:"discount_special,omitempty"`

	// DiscountListBasis/DiscountTotalBasis 覆盖率的分子与分母，单位 $，随行向上累加。
	// 之所以把两个基数一路带上来而不是只带比值：比值不可加权合并。ListQuota 对每一笔
	// 退款都冲减，DiscountTotalBasis 只在退款带折扣信息时才冲减（见 addBatch），两者
	// 口径不同，单个格子的 ListUsd 可以是 0 或负数，拿它当权重会把结果推出 [0,1]。
	// 与 EffectiveRatio/EffectiveDiscount 同一套做法：累加绝对量，最后除一次。
	DiscountListBasis  float64 `json:"discount_list_basis,omitempty"`
	DiscountTotalBasis float64 `json:"discount_total_basis,omitempty"`
	// DiscountCoverage = DiscountListBasis / DiscountTotalBasis，由 deriveRates 派生。
	DiscountCoverage float64 `json:"discount_coverage,omitempty"`
}

// costBreakdownRow 维度折叠行下的子明细（折叠掉本维度与 Day，保留其余两个维度）。
// CostMode/CostRatio/CostDiscount 为该明细行所属渠道在查询区间末尾生效版本的计价
// 配置，供前端在明细行对照"现在是按哪个倍率/折扣算的"；渠道身份被合并（按模型
// 汇总）时这些字段无意义，由前端按 channel_id 是否存在决定是否渲染。
// 区间内真实付出的倍率见内嵌 costMoney.EffectiveRatio（CostCny/ListUsd 加权值）。
type costBreakdownRow struct {
	Username    string `json:"username,omitempty"`
	ModelName   string `json:"model_name,omitempty"`
	ChannelId   int    `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`

	CostMode     string  `json:"cost_mode,omitempty"`
	CostRatio    float64 `json:"cost_ratio,omitempty"`
	CostDiscount float64 `json:"cost_discount,omitempty"`

	// Priced 与父行 costDimensionRow.Priced 同义同标签（不带 omitempty，false 也要
	// 下发——恰恰是 false 才需要提示）。逐条定价后父行 priced=false 已不能代表整组都
	// 被低估：渠道可能只在中途某个版本空档未定价，其余区间都有价。少了这个字段，
	// 落在空档里的明细行会输出 cost_cny=0、profit_rate=1 且不带 effective_ratio，
	// 与真正免费的上游逐字节一致，在表里反而显示为最赚钱的一行。
	Priced bool `json:"priced"`

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
	// CostRatio 为区间末尾生效版本的配置倍率（ratio 模式；discount 模式恒为 0，
	// 折扣见 CostDiscount）。它是"现在按什么价"的对照值，与区间内实际付出的加权
	// 倍率 costMoney.EffectiveRatio 不同——改过价时两者本就应该不一致。
	CostRatio float64 `json:"cost_ratio,omitempty"`
	// Priced 本行全部刊例金额都找到了生效版本。false 表示存在算不出成本的敞口，
	// 由前端提示"成本被低估"，而不是当成 0 成本。
	Priced    bool `json:"priced"`
	UserCount int  `json:"user_count,omitempty"`
	costMoney
	Breakdown          []costBreakdownRow `json:"breakdown,omitempty"`
	BreakdownTruncated int                `json:"breakdown_truncated,omitempty"`

	// 以下字段仅渠道维度（costDimChannel）填充，供前端按计价模式渲染。
	CostMode     string                   `json:"cost_mode,omitempty"`
	CostDiscount float64                  `json:"cost_discount,omitempty"`
	IsAggregator bool                     `json:"is_aggregator,omitempty"`
	SubSuppliers []dto.ChannelSubSupplier `json:"sub_suppliers,omitempty"`

	// 内部使用，不下发：累计无版本可用的刊例敞口，最终派生 Priced
	unpricedListQuota float64 `json:"-"`
	// ratioVersionSeen 本行所有立方体格子命中过的版本身份并集，最终派生
	// costMoney.RatioMixed。必须在折叠层做并集：改价前后的日志落在不同时间桶，
	// 单个格子各自只见一个版本，逐格判定后取或永远得不到 true。
	ratioVersionSeen map[int64]struct{} `json:"-"`
}

// costMoneyFromRow 统一金额换算：
// revenue_usd = 实付/QuotaPerUnit；cost_cny 直接取采集阶段按版本逐条累加的结果
// （不再在这里乘倍率——那样会用今天的价重算历史）；
// profit = revenue_cny − cost_cny；收入为 0 时利润率置 0。
// 同时派生 v2 指标：total_tokens、success_rate、cache_rate、avg_ttft_ms
// （公式与零分母兜底规则见 deriveRates）。
func costMoneyFromRow(r *costCubeRow, exchangeRate float64) costMoney {
	m := costMoney{
		RevenueUsd:          roundTo6(r.Quota / common.QuotaPerUnit),
		ListUsd:             roundTo6(r.ListQuota / common.QuotaPerUnit),
		CostCny:             roundTo6(r.CostCny),
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
	m.ProfitCny = roundTo6(m.RevenueCny - m.CostCny)
	if m.RevenueCny != 0 {
		m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
	}
	// 折扣信号。两个基数原样带上（换算成 $），比值交给 deriveRates 统一派生——
	// 单格子和折叠后的父行走同一条公式，避免两处口径漂移。
	m.DiscountListBasis = roundTo6(r.DiscountListBasis / common.QuotaPerUnit)
	m.DiscountTotalBasis = roundTo6(r.DiscountTotalBasis / common.QuotaPerUnit)
	// mixed/special 是采集到的事实而非金额的函数，仍在这里定，且只在本格子确有
	// 折扣信息时才置位：没有信息就不该声称"混用"或"专属"。
	if r.DiscountTotalBasis > 0 && r.DiscountListBasis > 0 {
		m.DiscountMixed = r.DiscountMixed
		m.DiscountSpecial = r.DiscountSpecialSum > 0
	}
	// RatioMixed 不在这里判定：单个格子只覆盖一个时间桶，改价前后的日志分属不同
	// 格子，逐格判定恒为 false。版本身份的并集由 foldCostCube 跨格子汇总后派生。
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
	// 真实成本倍率同理按「总成本 ÷ 总刊例」重算，跨版本时天然是加权均值。
	// RatioMixed 不在此派生——它是采集到的版本身份事实，不是金额的函数，
	// 在这里赋值会把 add() 合并上来的 mixed 状态覆盖掉。
	if m.ListUsd == 0 {
		m.EffectiveRatio, m.EffectiveRatioKnown = 0, false
	} else {
		m.EffectiveRatio = roundTo6(m.CostCny / m.ListUsd)
		m.EffectiveRatioKnown = true
	}
	// 覆盖率同样是「两个累加后的基数相除」，不是各行比值的平均。分子分母在采集侧
	// 共用同一个门槛（见 addBatch），故分母 ≥ 分子恒成立，min 只是兜底。
	// 两者都要求为正：净额退成 0 或负时比值无意义，此时不给覆盖率（omitempty 后
	// 前端按 0 处理，会如实显示"缺折扣信息"而不是编一个数）。
	if m.DiscountTotalBasis > 0 && m.DiscountListBasis > 0 {
		m.DiscountCoverage = roundTo6(min(m.DiscountListBasis/m.DiscountTotalBasis, 1))
	} else {
		m.DiscountCoverage = 0
	}
}

func (m *costMoney) add(o costMoney) {
	// 布尔信号取或：任一子行跨版本/跨折扣，父行即为 mixed。
	// RatioMixed 的兄弟格子场景由 foldCostCube 的版本并集覆盖，这里的或只保证
	// 已经确定 mixed 的子行在向上合并时不丢失该状态。
	m.RatioMixed = m.RatioMixed || o.RatioMixed
	m.DiscountMixed = m.DiscountMixed || o.DiscountMixed
	m.DiscountSpecial = m.DiscountSpecial || o.DiscountSpecial

	m.RevenueUsd = roundTo6(m.RevenueUsd + o.RevenueUsd)
	m.RevenueCny = roundTo6(m.RevenueCny + o.RevenueCny)
	m.ListUsd = roundTo6(m.ListUsd + o.ListUsd)
	m.CostCny = roundTo6(m.CostCny + o.CostCny)
	m.DiscountListBasis = roundTo6(m.DiscountListBasis + o.DiscountListBasis)
	m.DiscountTotalBasis = roundTo6(m.DiscountTotalBasis + o.DiscountTotalBasis)
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

// historicalDiscount 从日志 other 取请求当时的生效折扣：
// user_group_ratio 有效（专属倍率）优先，其次 group_ratio。
// 返回 0 表示无法确定（旧日志无 other 或值无效）。
// 取历史值而不是查当前配置：用户换过组、倍率被调过之后，配置快照会把过去的账
// 也按今天的折扣重述一遍。
func historicalDiscount(info *logPricingInfo) float64 {
	if info == nil {
		return 0
	}
	if isValidGroupRatio(info.UserGroupRatio) && info.UserGroupRatio > 0 {
		return info.UserGroupRatio
	}
	if info.GroupRatio > 0 {
		return info.GroupRatio
	}
	return 0
}

// foldCostCube 沿 dim 折叠立方体。成本已在采集阶段按各条日志当时生效的版本算好，
// 这里只做累加；end 用于取"区间末尾生效版本"作为配置展示的对照值。
func foldCostCube(cube *costCube, dim string, channels map[int]*model.ChannelCostInfo,
	versions model.VersionMap, exchangeRate float64, end int64) []costDimensionRow {
	type groupKey struct {
		UserId    int
		Username  string
		ModelName string
		ChannelId int
	}
	// costSubAgg 明细子桶：金额 + 未定价敞口 + 版本身份并集（后两者供 Priced/跨版本
	// 判定）。敞口必须与版本身份一样在子桶上单独累计，不能只靠父行——父行 Priced 已
	// 无法定位敞口落在哪条明细上。
	type costSubAgg struct {
		money             costMoney
		unpricedListQuota float64
		ratioVersionSeen  map[int64]struct{}
	}
	// mergeVersionSeen 把一条立方体行命中的版本身份并入目标集合。
	mergeVersionSeen := func(dst *map[int64]struct{}, src map[int64]struct{}) {
		if len(src) == 0 {
			return
		}
		if *dst == nil {
			*dst = make(map[int64]struct{}, len(src))
		}
		for v := range src {
			(*dst)[v] = struct{}{}
		}
	}
	groups := make(map[groupKey]*costDimensionRow)
	breakdowns := make(map[groupKey]map[costCubeKey]*costSubAgg) // sub-agg per group
	userSets := make(map[groupKey]map[int]bool)

	for k, r := range cube.rows {
		chName := ""
		if ci := channels[k.ChannelId]; ci != nil {
			chName = ci.Name
		}
		m := costMoneyFromRow(r, exchangeRate)

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
				if ci := channels[gk.ChannelId]; ci != nil {
					row.IsAggregator = ci.IsAggregator
					row.SubSuppliers = ci.SubSuppliers
				}
				// 配置展示优先取区间末尾版本（供 hover 对照"现在是什么价"）；
				// 渠道从未建过版本时回退到 ChannelSettings 的当前配置。
				if ver, ok := versions.VersionAt(gk.ChannelId, end); ok {
					row.CostMode = ver.CostMode
					row.CostRatio = ver.CostRatio
					row.CostDiscount = ver.CostDiscount
				} else if ci := channels[gk.ChannelId]; ci != nil {
					row.CostMode = ci.CostMode
					row.CostRatio = ci.CostRatio
					row.CostDiscount = ci.CostDiscount
				}
				userSets[gk] = make(map[int]bool)
			}
			groups[gk] = row
		}
		row.costMoney.add(m)
		row.unpricedListQuota += r.UnpricedListQuota
		mergeVersionSeen(&row.ratioVersionSeen, r.RatioVersionSeen)
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
		sub.unpricedListQuota += r.UnpricedListQuota
		mergeVersionSeen(&sub.ratioVersionSeen, r.RatioVersionSeen)
	}

	rows := make([]costDimensionRow, 0, len(groups))
	for gk, row := range groups {
		if dim == costDimChannel {
			row.UserCount = len(userSets[gk])
		}
		// 全部刊例都定到了价才算 Priced；跨版本以并集大小判定，两者都必须等折叠
		// 完成、所有格子归并之后才能得出结论。
		row.Priced = row.unpricedListQuota <= costUnpricedEpsilon
		row.RatioMixed = len(row.ratioVersionSeen) > 1
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
			chName := ""
			if ci := channels[b.key.ChannelId]; ci != nil {
				chName = ci.Name
			}
			br := costBreakdownRow{
				Username: b.key.Username, ModelName: b.key.ModelName,
				ChannelId: b.key.ChannelId, ChannelName: chName,
				costMoney: b.sub.money,
			}
			br.Priced = b.sub.unpricedListQuota <= costUnpricedEpsilon
			br.RatioMixed = len(b.sub.ratioVersionSeen) > 1
			// 渠道身份未被折叠（ChannelId != 0）时带上该渠道区间末尾生效版本的计价
			// 配置，供前端在明细行展示成本倍率/折扣。按模型汇总的明细行 ChannelId
			// 为 0，此时跨渠道混合，不存在单一倍率，字段留空由前端显示"—"。
			if b.key.ChannelId != 0 {
				if ver, ok := versions.VersionAt(b.key.ChannelId, end); ok {
					br.CostMode = ver.CostMode
					br.CostRatio = ver.CostRatio
					br.CostDiscount = ver.CostDiscount
				} else if ci := channels[b.key.ChannelId]; ci != nil {
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
