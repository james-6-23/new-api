package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// testVersions 与 testChannels 一一对应的计价版本映射（VersionMap 要求按
// EffectiveFrom 升序）。全部取 effective_from=0（"自古以来"，等同迁移回填出来的
// 初始版本），因此任何 end>=0 的折叠都能解析到它们，测试无需关心时间点。
// ch3 的 CostMode 留空：空值等同 ratio，与 testChannels 里 ch3 的配置一致。
// ch9 的 ExchangeRate 冻结为 6.8——discount 模式的成本自此只跟版本汇率走，
// 与查询汇率无关。ch7 刻意不建版本：它是"未定价渠道"用例的主角。
func testVersions() model.VersionMap {
	return model.VersionMap{
		3: {{ChannelId: 3, EffectiveFrom: 0, CostRatio: 2.5}},
		9: {{ChannelId: 9, EffectiveFrom: 0, CostMode: "discount", CostDiscount: 0.8, ExchangeRate: 6.8}},
	}
}

// testFoldEnd 折叠时的"区间末尾"时间戳，用于取展示用的末尾生效版本。
// 测试数据都落在 2026-06-01，取当天 23 点即可覆盖全部日志。
func testFoldEnd() int64 { return tsOn("2026-06-01", 23) }

// 分组折扣 0.8：实付 800，刊例应还原为 1000。
func TestCostCube_ListVsActualWithGroupDiscount(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 800, PromptTokens: 10, CompletionTokens: 5,
			Other: `{"model_ratio":2,"group_ratio":0.8}`},
	}, testVersions())
	if len(c.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(c.rows))
	}
	for _, r := range c.rows {
		if r.Quota != 800 {
			t.Fatalf("actual = %v, want 800", r.Quota)
		}
		if r.ListQuota != 1000 {
			t.Fatalf("list = %v, want 1000", r.ListQuota)
		}
		if r.RequestCount != 1 {
			t.Fatalf("requests = %d, want 1", r.RequestCount)
		}
	}
}

// seedance 三场景：全额退款净 0；结算补扣净 120；结算部分退净 70。
// settle 行不计请求数；退款行冲减实付与刊例并累计 RefundQuota。
func TestCostCube_SeedanceRefundScenarios(t *testing.T) {
	mk := func(typ int, quota int, stage string, day string, hour int) *model.Log {
		return &model.Log{Type: typ, CreatedAt: tsOn(day, hour), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "seedance-1-5-pro",
			Quota: quota, Other: `{"group_ratio":1,"billing_stage":"` + stage + `","is_task":true}`}
	}
	c := newCostCube()
	// 场景1: 预扣100 全退 → 净0
	c.addBatch([]*model.Log{
		mk(model.LogTypeConsume, 100, "pre_consume", "2026-06-01", 9),
		mk(model.LogTypeRefund, 100, "refund", "2026-06-01", 10),
	}, testVersions())
	// 场景2: 预扣100 补扣20 → 净120（settle 不计请求）
	c.addBatch([]*model.Log{
		mk(model.LogTypeConsume, 100, "pre_consume", "2026-06-02", 9),
		mk(model.LogTypeConsume, 20, "settle", "2026-06-02", 10),
	}, testVersions())
	// 场景3: 预扣100 退30 → 净70
	c.addBatch([]*model.Log{
		mk(model.LogTypeConsume, 100, "pre_consume", "2026-06-03", 9),
		mk(model.LogTypeRefund, 30, "refund", "2026-06-03", 10),
	}, testVersions())
	var totalActual, totalList, totalRefund float64
	var totalReq int
	for _, r := range c.rows {
		totalActual += r.Quota
		totalList += r.ListQuota
		totalRefund += r.RefundQuota
		totalReq += r.RequestCount
	}
	if totalActual != 190 { // 0 + 120 + 70
		t.Fatalf("net actual = %v, want 190", totalActual)
	}
	if totalList != 190 { // ratio 1 → list == actual
		t.Fatalf("net list = %v, want 190", totalList)
	}
	if totalRefund != 130 { // 100 + 30
		t.Fatalf("refund = %v, want 130", totalRefund)
	}
	if totalReq != 3 { // settle 行不计
		t.Fatalf("requests = %d, want 3", totalReq)
	}
}

// 非消费/退款/错误类型忽略（如充值）；无 other 的旧日志刊例=实付兜底。
// LogTypeError 的计数行为见 TestCostCube_ErrorAndMetrics（v2 起错误行不再被忽略）。
func TestCostCube_IgnoresOtherTypesAndLegacyLogs(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeTopup, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "a", Quota: 999},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "a",
			ChannelId: 7, ModelName: "m", Quota: 50, Other: ``},
	}, testVersions())
	if len(c.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(c.rows))
	}
	for _, r := range c.rows {
		if r.Quota != 50 || r.ListQuota != 50 {
			t.Fatalf("legacy fallback broken: %+v", r)
		}
	}
}

func testChannels() map[int]*model.ChannelCostInfo {
	return map[int]*model.ChannelCostInfo{
		3: {Id: 3, Name: "openai-a", CostRatio: 2.5},
		7: {Id: 7, Name: "nopriced", CostRatio: 0},
		9: {Id: 9, Name: "disc", CostMode: "discount", CostDiscount: 0.8, IsAggregator: true,
			SubSuppliers: []dto.ChannelSubSupplier{{Name: "sub-a", CostRatio: 6.0}}},
	}
}

// discount 模式渠道折叠：quota 500 / group_ratio 1 → list=500；
// cost_cny = 500/500000 * 0.8 * 6.8 = 0.00544（6.8 为版本冻结汇率，非查询汇率）；
// Priced=true。
func TestFoldCostCube_DiscountModeChannel(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 9, ModelName: "gpt-4o", Quota: 500, PromptTokens: 1, CompletionTokens: 1,
			Other: `{"model_ratio":1,"group_ratio":1}`},
	}, testVersions())
	rows := foldCostCube(c, costDimChannel, testChannels(), testVersions(), 6.8, testFoldEnd())
	var ch9 *costDimensionRow
	for i := range rows {
		if rows[i].ChannelId == 9 {
			ch9 = &rows[i]
		}
	}
	if ch9 == nil {
		t.Fatal("channel 9 missing")
	}
	want := roundTo6(500 / 500000.0 * 0.8 * 6.8)
	if ch9.CostCny != want {
		t.Fatalf("cost_cny = %v, want %v", ch9.CostCny, want)
	}
	if !ch9.Priced {
		t.Fatalf("priced = false, want true")
	}
}

// TestFoldCostCube_ChannelDimSupplierExtras 渠道维度行需要透传计价模式相关的展示
// 信息：cost_ratio 取区间末尾生效版本的原始配置值（折扣渠道为 0），effective_ratio
// 为区间内真实付出的加权倍率（CostCny/ListUsd，此处只有一个版本故等于 0.8×6.8），
// 另附 cost_mode/cost_discount/is_aggregator/sub_suppliers，供前端按计价模式渲染
// （channel 9 为 discount 模式 + 聚合商 + 一个子供应商）。
func TestFoldCostCube_ChannelDimSupplierExtras(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 9, ModelName: "gpt-4o", Quota: 500, PromptTokens: 1, CompletionTokens: 1,
			Other: `{"model_ratio":1,"group_ratio":1}`},
	}, testVersions())
	chs := testChannels()
	rows := foldCostCube(c, costDimChannel, chs, testVersions(), 6.8, testFoldEnd())
	var ch9 *costDimensionRow
	for i := range rows {
		if rows[i].ChannelId == 9 {
			ch9 = &rows[i]
		}
	}
	if ch9 == nil {
		t.Fatal("channel 9 missing")
	}
	if ch9.CostRatio != 0 {
		t.Fatalf("cost_ratio = %v, want 0 (raw configured value for discount-mode channel)", ch9.CostRatio)
	}
	wantEffective := 0.8 * 6.8
	if diff := ch9.EffectiveRatio - wantEffective; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("effective_ratio = %v, want %v", ch9.EffectiveRatio, wantEffective)
	}
	if ch9.CostMode != "discount" {
		t.Fatalf("cost_mode = %q, want %q", ch9.CostMode, "discount")
	}
	if ch9.CostDiscount != 0.8 {
		t.Fatalf("cost_discount = %v, want 0.8", ch9.CostDiscount)
	}
	if !ch9.IsAggregator {
		t.Fatal("is_aggregator = false, want true")
	}
	if len(ch9.SubSuppliers) != 1 || ch9.SubSuppliers[0].Name != "sub-a" || ch9.SubSuppliers[0].CostRatio != 6.0 {
		t.Fatalf("sub_suppliers = %+v", ch9.SubSuppliers)
	}
	if !ch9.Priced {
		t.Fatal("priced = false, want true (effective ratio > 0)")
	}
}

// TestFoldCostCube_BreakdownCarriesChannelPricing 用户维度的展开明细行需要带上
// 所属渠道的计价配置，前端才能在明细行直接展示"这笔成本按哪个倍率/折扣算的"。
// ch3 为 ratio 模式（cost_ratio 2.5，区间内只有这一个版本，故加权实付倍率同值）；
// ch9 为 discount 模式（cost_discount 0.8，实付倍率 0.8×版本冻结汇率）。
func TestFoldCostCube_BreakdownCarriesChannelPricing(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 800, Other: `{"group_ratio":0.8}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), UserId: 1, Username: "alice",
			ChannelId: 9, ModelName: "gpt-4o", Quota: 500, Other: `{"group_ratio":1}`},
	}, testVersions())
	rows := foldCostCube(c, costDimUser, testChannels(), testVersions(), 6.8, testFoldEnd())
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	byChannel := map[int]costBreakdownRow{}
	for _, b := range rows[0].Breakdown {
		byChannel[b.ChannelId] = b
	}
	ch3, ok := byChannel[3]
	if !ok {
		t.Fatal("breakdown for channel 3 missing")
	}
	if ch3.CostRatio != 2.5 || ch3.EffectiveRatio != 2.5 {
		t.Fatalf("ch3 ratio = %v / effective %v, want 2.5 / 2.5", ch3.CostRatio, ch3.EffectiveRatio)
	}
	if ch3.CostMode != "" {
		t.Fatalf("ch3 cost_mode = %q, want empty (ratio mode)", ch3.CostMode)
	}
	ch9, ok := byChannel[9]
	if !ok {
		t.Fatal("breakdown for channel 9 missing")
	}
	if ch9.CostMode != "discount" || ch9.CostDiscount != 0.8 {
		t.Fatalf("ch9 mode = %q discount = %v, want discount / 0.8", ch9.CostMode, ch9.CostDiscount)
	}
	if want := 0.8 * 6.8; ch9.EffectiveRatio != want {
		t.Fatalf("ch9 effective_ratio = %v, want %v", ch9.EffectiveRatio, want)
	}
}

// TestEffectiveDiscountDerivation 实际加权折扣 = 收入$ ÷ 刊例$：
// 该商本身即区间内按额度加权的真实生效折扣，专属倍率/跨分组/倍率变更全自动正确。
// 刊例为 0（免费或未定价模型）时商无意义，置 Known=false 由前端显示 "-"。
func TestEffectiveDiscountDerivation(t *testing.T) {
	// 8 折：实付 800 / 刊例 1000
	m := costMoney{RevenueUsd: 8, ListUsd: 10}
	m.deriveRates()
	if !m.EffectiveDiscountKnown || m.EffectiveDiscount != 0.8 {
		t.Fatalf("effective discount = %v (known=%v), want 0.8", m.EffectiveDiscount, m.EffectiveDiscountKnown)
	}

	// 刊例为 0 → 无意义
	zero := costMoney{RevenueUsd: 0, ListUsd: 0}
	zero.deriveRates()
	if zero.EffectiveDiscountKnown {
		t.Fatalf("zero list price must leave discount unknown: %+v", zero)
	}

	// add() 汇总两行不同折扣后按总额重算：(8+5) / (10+10) = 0.65
	sum := costMoney{RevenueUsd: 8, ListUsd: 10}
	sum.deriveRates()
	sum.add(costMoney{RevenueUsd: 5, ListUsd: 10})
	if got := sum.EffectiveDiscount; got < 0.6499 || got > 0.6501 {
		t.Fatalf("aggregated discount = %v, want 0.65 (total revenue ÷ total list)", got)
	}
}

func seedCube() *costCube {
	c := newCostCube()
	c.addBatch([]*model.Log{
		// alice, gpt-4o, ch3: 实付800 刊例1000（8折）
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 800, PromptTokens: 10, CompletionTokens: 5,
			Other: `{"model_ratio":2,"group_ratio":0.8}`},
		// bob, gpt-4o, ch7: 实付=刊例 500
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), UserId: 2, Username: "bob",
			ChannelId: 7, ModelName: "gpt-4o", Quota: 500, PromptTokens: 3, CompletionTokens: 2,
			Other: `{"model_ratio":2,"group_ratio":1}`},
	}, testVersions())
	return c
}

// 金额换算：QuotaPerUnit=500000, 汇率 7.0, ch3 版本倍率 2.5
// alice: revenue_usd=800/5e5=0.0016, revenue_cny=0.0112, list_usd=0.002, cost=0.005, profit=0.0062
// 覆盖率跨格子合并必须按各自的基数相加后再求商，不能拿 ListUsd 当权重。
//
// 两者的口径本就不同：ListQuota 对**每一笔**退款都冲减，而 DiscountTotalBasis 只在
// 退款自带折扣信息时才冲减（见 addBatch 里那段门槛注释）。于是单个格子的 ListUsd
// 可以是 0 甚至负数，用它加权就把一个「份额」乘到了另一个分母上，结果不再落在
// [0,1]：下面这组数据在旧实现下得出 1.666667，而真实覆盖率是 1.0——刊例与有信息
// 的基数是同一笔钱，净额之后仍然完全覆盖。
//
// 前端把 <0.99 当作「部分覆盖」告警阈值（cost-user-cells.tsx），所以 >1 的值会让
// 告警在真正部分覆盖时消失；负值则会渲染成「-200% 的消费有折扣信息」。
func TestFoldCostCube_CoverageFoldsByBasisNotListUsd(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		// 06-01 消费：刊例 500000（$1），带 group_ratio 故有折扣信息
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 500000, Other: `{"group_ratio":1}`},
		// 06-02 部分退款：落在另一个日桶，于是与上面分属两个格子，合并时走 add()
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-02", 9), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 200000, Other: `{"group_ratio":1}`},
	}, testVersions())

	rows := foldCostCube(c, costDimUser, testChannels(), testVersions(), 7.0, testFoldEnd())
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0].DiscountCoverage
	if !nearly(got, 1) {
		t.Fatalf("discount_coverage = %v, want 1 (basis-weighted); "+
			"ListUsd-weighted folding yields 1.666667 here", got)
	}
}

// 覆盖率必须与格子的遍历顺序无关，且恒落在 [0,1]。
//
// foldCostCube 遍历的是 map，Go 的 map 迭代顺序每次都不同。旧实现拿 ListUsd 当权重，
// 而每格的覆盖率分母是 DiscountTotalBasis——两者的退款口径本就不同（ListQuota 每笔
// 退款都减，DiscountTotalBasis 只在退款带折扣信息时才减），于是权重可以为 0 甚至为负。
// 一旦某格 ListUsd 为负，`totalList > 0` 这道门槛会保住上一轮的旧值，让结果取决于
// 「负的那格在第几步被访问」，同一份数据反复折叠就会跳。
//
// 两组数据分工：
//   - 普通退款：权重恒正，旧实现结果稳定但偏大（0.434783 vs 0.347826），钉住数值正确性。
//   - 超额退款：把该格 ListUsd 压成负数，正是让旧实现在 ±2 之间跳的形状，钉住顺序无关
//     与取值范围。它同时也是分子为负的唯一入口，验证 deriveRates 的门槛没有放行负覆盖率
//     （负值传到前端会渲染成「-200% 的消费有折扣信息」）。
//
// 两组都混入一条不带 group_ratio 的消费行（MJ/任务日志就是这样，见 addBatch 的门槛
// 注释）：它进分母不进分子，是覆盖率真正要描述的那部分缺口。
func TestFoldCostCube_CoverageIsOrderIndependentAndBounded(t *testing.T) {
	cases := []struct {
		name        string
		refundQuota int
		want        float64
	}{
		// 分子 400000（500000 消费 − 100000 退款，均带信息）／分母 1150000（再加 750000 无信息消费）
		{"partial refund", 100000, 400000.0 / 1150000.0},
		// 退款超过带信息的消费 → 分子 −500000，属异常数据，覆盖率取 0 而非负数
		{"over refund", 1000000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			build := func() *costCube {
				c := newCostCube()
				c.addBatch([]*model.Log{
					{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
						ChannelId: 3, ModelName: "gpt-4o", Quota: 500000, Other: `{"group_ratio":1}`},
					{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-02", 9), UserId: 1, Username: "alice",
						ChannelId: 3, ModelName: "gpt-4o", Quota: tc.refundQuota, Other: `{"group_ratio":1}`},
					{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-03", 9), UserId: 1, Username: "alice",
						ChannelId: 3, ModelName: "gpt-4o", Quota: 750000, Other: `{"task_id":"x"}`},
				}, testVersions())
				return c
			}
			coverage := func() float64 {
				return foldCostCube(build(), costDimUser, testChannels(), testVersions(), 7.0, testFoldEnd())[0].DiscountCoverage
			}

			first := coverage()
			if !nearly(first, tc.want) {
				t.Fatalf("coverage = %v, want %v", first, tc.want)
			}
			for i := 0; i < 200; i++ {
				got := coverage()
				if got != first {
					t.Fatalf("iteration %d: coverage = %v, first = %v (map iteration order must not matter)", i, got, first)
				}
				if got < 0 || got > 1 {
					t.Fatalf("iteration %d: coverage = %v, must stay within [0,1]", i, got)
				}
			}
		})
	}
}

func TestFoldCostCube_UserDimensionMoney(t *testing.T) {
	rows := foldCostCube(seedCube(), costDimUser, testChannels(), testVersions(), 7.0, testFoldEnd())
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	var alice *costDimensionRow
	for i := range rows {
		if rows[i].Username == "alice" {
			alice = &rows[i]
		}
	}
	if alice == nil {
		t.Fatal("alice missing")
	}
	if alice.RevenueUsd != 0.0016 || alice.RevenueCny != 0.0112 {
		t.Fatalf("revenue: %+v", alice.costMoney)
	}
	if alice.ListUsd != 0.002 || alice.CostCny != 0.005 {
		t.Fatalf("cost: %+v", alice.costMoney)
	}
	if alice.ProfitCny != 0.0062 {
		t.Fatalf("profit = %v, want 0.0062", alice.ProfitCny)
	}
	if len(alice.Breakdown) != 1 || alice.Breakdown[0].ChannelName != "openai-a" || alice.Breakdown[0].ModelName != "gpt-4o" {
		t.Fatalf("breakdown: %+v", alice.Breakdown)
	}
}

// 未建版本的渠道：成本 0、Priced=false（"查不到版本" ≠ 上游免费）；
// 渠道维度带 user_count。
func TestFoldCostCube_ChannelDimensionUnpriced(t *testing.T) {
	rows := foldCostCube(seedCube(), costDimChannel, testChannels(), testVersions(), 7.0, testFoldEnd())
	var ch7 *costDimensionRow
	for i := range rows {
		if rows[i].ChannelId == 7 {
			ch7 = &rows[i]
		}
	}
	if ch7 == nil {
		t.Fatal("channel 7 missing")
	}
	if ch7.Priced || ch7.CostCny != 0 {
		t.Fatalf("unpriced channel must cost 0: %+v", ch7)
	}
	if ch7.UserCount != 1 {
		t.Fatalf("user_count = %d, want 1", ch7.UserCount)
	}
	if ch7.ChannelName != "nopriced" {
		t.Fatalf("name = %q", ch7.ChannelName)
	}
}

// 三维度总计一致：Σ用户 == Σ模型 == Σ渠道（收入与成本都相等）。
func TestFoldCostCube_DimensionTotalsAgree(t *testing.T) {
	cube := seedCube()
	chs := testChannels()
	sum := func(dim string) (rev, cost float64) {
		for _, r := range foldCostCube(cube, dim, chs, testVersions(), 7.0, testFoldEnd()) {
			rev += r.RevenueCny
			cost += r.CostCny
		}
		return
	}
	r1, c1 := sum(costDimUser)
	r2, c2 := sum(costDimModel)
	r3, c3 := sum(costDimChannel)
	if r1 != r2 || r2 != r3 || c1 != c2 || c2 != c3 {
		t.Fatalf("totals disagree: rev(%v,%v,%v) cost(%v,%v,%v)", r1, r2, r3, c1, c2, c3)
	}
}

// 收入为 0 时利润率必须为 0（不得 NaN/Inf）。
func TestCostMoney_ZeroRevenueRate(t *testing.T) {
	m := costMoneyFromRow(&costCubeRow{}, 7.0)
	if m.ProfitRate != 0 {
		t.Fatalf("rate = %v, want 0", m.ProfitRate)
	}
}

// TestCostCube_ErrorAndMetrics 覆盖 v2 立方体新增指标：错误计数、缓存 tokens、
// 首字延迟（TTFT）。同一用户/模型/渠道/日桶下：2 条消费 + 1 条错误 + 1 条退款。
// 两条消费均为 OpenAI 语义（无 usage_semantic/claude 标记），prompt_tokens 已含
// 缓存读取，故归一化后的非缓存输入为 (100-40) + 50 = 110。
// 折叠后 SuccessRate = 2/(2+1)；CacheRate = 40/总输入；AvgTtftMs 只取 frt>0 的行
// （第二条消费 frt=-1000 视为未记录，不计入 FrtCount/FrtSumMs）。
func TestCostCube_ErrorAndMetrics(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 200, PromptTokens: 100, CompletionTokens: 20,
			Other: `{"group_ratio":1,"cache_tokens":40,"cache_creation_tokens":5,"cache_creation_tokens_5m":3,"frt":120.5}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 100, PromptTokens: 50, CompletionTokens: 10,
			Other: `{"group_ratio":1,"frt":-1000}`},
		{Type: model.LogTypeError, CreatedAt: tsOn("2026-06-01", 11), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o"},
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 12), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 50, Other: `{"group_ratio":1}`},
	}, testVersions())

	rows := foldCostCube(c, costDimUser, testChannels(), testVersions(), 7.0, testFoldEnd())
	var alice *costDimensionRow
	for i := range rows {
		if rows[i].Username == "alice" {
			alice = &rows[i]
		}
	}
	if alice == nil {
		t.Fatal("alice missing")
	}
	if alice.ErrorCount != 1 {
		t.Fatalf("error_count = %d, want 1", alice.ErrorCount)
	}
	if alice.CacheReadTokens != 40 {
		t.Fatalf("cache_read_tokens = %d, want 40", alice.CacheReadTokens)
	}
	// max(5, 3(5m)) = 5：cache_creation_tokens 已是总量，不与拆分项相加。
	if alice.CacheCreationTokens != 5 {
		t.Fatalf("cache_creation_tokens = %d, want 5", alice.CacheCreationTokens)
	}
	// OpenAI 语义：(100-40) + (50-0) = 110 非缓存输入。
	if alice.PromptTokens != 110 {
		t.Fatalf("prompt_tokens = %d, want 110", alice.PromptTokens)
	}
	if alice.FrtCount != 1 {
		t.Fatalf("frt_count = %d, want 1", alice.FrtCount)
	}
	if alice.FrtSumMs != 120.5 {
		t.Fatalf("frt_sum_ms = %v, want 120.5", alice.FrtSumMs)
	}
	wantSuccessRate := roundTo6(2.0 / 3.0)
	if alice.SuccessRate != wantSuccessRate {
		t.Fatalf("success_rate = %v, want %v", alice.SuccessRate, wantSuccessRate)
	}
	// 总输入 = 非缓存输入 110 + 缓存读取 40 + 缓存创建 5 = 155。
	wantCacheRate := roundTo6(40.0 / 155.0)
	if alice.CacheRate != wantCacheRate {
		t.Fatalf("cache_rate = %v, want %v", alice.CacheRate, wantCacheRate)
	}
	if alice.AvgTtftMs != 120.5 {
		t.Fatalf("avg_ttft_ms = %v, want 120.5", alice.AvgTtftMs)
	}
	// 155 总输入 + 30 输出。
	if alice.TotalTokens != 185 {
		t.Fatalf("total_tokens = %d, want 185", alice.TotalTokens)
	}
}

// TestCostMoneyDerivedRates 覆盖零分母兜底规则：请求+错误数为 0 → SuccessRate=1；
// 总输入为 0 → CacheRate=0；FrtCount=0 → AvgTtftMs=0。并附一组非零分母的正常路径
// 校验公式本身正确。
func TestCostMoneyDerivedRates(t *testing.T) {
	m1 := costMoneyFromRow(&costCubeRow{}, 7.0)
	if m1.SuccessRate != 1 {
		t.Fatalf("success_rate = %v, want 1", m1.SuccessRate)
	}
	if m1.CacheRate != 0 {
		t.Fatalf("cache_rate = %v, want 0", m1.CacheRate)
	}
	if m1.AvgTtftMs != 0 {
		t.Fatalf("avg_ttft_ms = %v, want 0", m1.AvgTtftMs)
	}

	// 全部输入都来自缓存读取 → 命中率 100%（分母是总输入，不是非缓存输入）。
	m2 := costMoneyFromRow(&costCubeRow{PromptTokens: 0, CacheReadTokens: 10}, 7.0)
	if m2.CacheRate != 1 {
		t.Fatalf("cache_rate = %v, want 1", m2.CacheRate)
	}
	if m2.TotalTokens != 10 {
		t.Fatalf("total_tokens = %d, want 10", m2.TotalTokens)
	}

	m3 := costMoneyFromRow(&costCubeRow{FrtCount: 0, FrtSumMs: 999}, 7.0)
	if m3.AvgTtftMs != 0 {
		t.Fatalf("avg_ttft_ms = %v, want 0", m3.AvgTtftMs)
	}

	m4 := costMoneyFromRow(&costCubeRow{RequestCount: 3, ErrorCount: 1, PromptTokens: 100, CompletionTokens: 20, CacheReadTokens: 25, CacheCreationTokens: 15, FrtCount: 2, FrtSumMs: 200}, 7.0)
	if want := roundTo6(3.0 / 4.0); m4.SuccessRate != want {
		t.Fatalf("success_rate = %v, want %v", m4.SuccessRate, want)
	}
	// 总输入 = 100 + 25 + 15 = 140。
	if want := roundTo6(25.0 / 140.0); m4.CacheRate != want {
		t.Fatalf("cache_rate = %v, want %v", m4.CacheRate, want)
	}
	if m4.TotalTokens != 160 {
		t.Fatalf("total_tokens = %d, want 160", m4.TotalTokens)
	}
	if want := roundTo6(200.0 / 2.0); m4.AvgTtftMs != want {
		t.Fatalf("avg_ttft_ms = %v, want %v", m4.AvgTtftMs, want)
	}
}

// TestCostCube_UsageSemanticNormalization 覆盖 prompt_tokens 的语义分叉：
// Claude 的 input_tokens 与缓存互斥（原样保留），OpenAI 的 prompt_tokens 已含
// 缓存读取（需减去）。两条日志的 tokens 完全相同，归一化后非缓存输入不同，
// 但同一桶内可直接相加——这正是归一化要保证的跨渠道可加性。
func TestCostCube_UsageSemanticNormalization(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		// Claude 语义：input_tokens=100 不含缓存 → 非缓存输入 100。
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "claude-opus", Quota: 100, PromptTokens: 100, CompletionTokens: 10,
			Other: `{"group_ratio":1,"usage_semantic":"anthropic","cache_tokens":30,"cache_creation_tokens":20}`},
		// OpenAI 语义：prompt_tokens=100 已含 cache_read 30 → 非缓存输入 70。
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "claude-opus", Quota: 100, PromptTokens: 100, CompletionTokens: 10,
			Other: `{"group_ratio":1,"cache_tokens":30}`},
	}, testVersions())

	rows := foldCostCube(c, costDimUser, testChannels(), testVersions(), 7.0, testFoldEnd())
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.PromptTokens != 170 { // 100 (claude) + 70 (openai)
		t.Fatalf("prompt_tokens = %d, want 170", r.PromptTokens)
	}
	if r.CacheReadTokens != 60 || r.CacheCreationTokens != 20 {
		t.Fatalf("cache tokens = %d/%d, want 60/20", r.CacheReadTokens, r.CacheCreationTokens)
	}
	// 四项互不重叠，相加恒等于总数：170 + 60 + 20 + 20 = 270。
	if r.TotalTokens != 270 {
		t.Fatalf("total_tokens = %d, want 270", r.TotalTokens)
	}
	if want := roundTo6(60.0 / 250.0); r.CacheRate != want {
		t.Fatalf("cache_rate = %v, want %v", r.CacheRate, want)
	}
}

// 老日志只有 claude=true 而无 usage_semantic 时也必须按 Claude 语义处理，
// 否则 prompt_tokens 会被误减一次缓存读取。
func TestCostCube_LegacyClaudeFlagNormalization(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "claude-opus", Quota: 100, PromptTokens: 80, CompletionTokens: 5,
			Other: `{"group_ratio":1,"claude":true,"cache_tokens":25}`},
	}, testVersions())
	rows := foldCostCube(c, costDimUser, testChannels(), testVersions(), 7.0, testFoldEnd())
	if rows[0].PromptTokens != 80 {
		t.Fatalf("prompt_tokens = %d, want 80 (claude semantics: no subtraction)", rows[0].PromptTokens)
	}
	if rows[0].TotalTokens != 110 { // 80 + 25 + 0 + 5
		t.Fatalf("total_tokens = %d, want 110", rows[0].TotalTokens)
	}
}

// 多个 0 收入行 RevenueCny 相同（并列），map 迭代顺序本身是随机的；
// foldCostCube 必须用身份字段（Username/ModelName/ChannelId）兜底排序，
// 保证同一份数据多次调用结果顺序一致（分页/缓存依赖稳定顺序）。
func TestFoldCostCube_DeterministicOrderOnTies(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 10, Username: "zed",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 0},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 11, Username: "amy",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 0},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 12, Username: "mike",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 0},
	}, testVersions())
	chs := testChannels()

	var first []string
	for i := 0; i < 5; i++ {
		rows := foldCostCube(c, costDimUser, chs, testVersions(), 7.0, testFoldEnd())
		if len(rows) != 3 {
			t.Fatalf("rows = %d, want 3", len(rows))
		}
		names := make([]string, len(rows))
		for j, r := range rows {
			names[j] = r.Username
		}
		if i == 0 {
			first = names
			if names[0] != "amy" || names[1] != "mike" || names[2] != "zed" {
				t.Fatalf("expected alphabetical tiebreak, got %v", names)
			}
			continue
		}
		for j := range names {
			if names[j] != first[j] {
				t.Fatalf("order changed across calls: run0=%v run%d=%v", first, i, names)
			}
		}
	}
}

// 跨版本折叠：改价前后的日志各自按当时生效的版本定价，成本是两段的和而非
// 任一单价 × 总量；同时 RatioMixed 必须为 true。
//
// 这条用例针对的是一个已经踩过的坑：RatioMixed 若在单个立方体格子里判定
// （len(RatioVersionSeen) > 1），日粒度下改价前后的日志分属不同时间桶，每格
// 都只看到一个版本，恒为 false——标记永远不会亮，而它正是为这个场景存在的。
// 所以判定必须在折叠层对版本身份做并集。
func TestFoldCostCube_CrossVersionPricing(t *testing.T) {
	boundary := tsOn("2026-06-02", 0)
	versions := model.VersionMap{
		3: {
			{ChannelId: 3, EffectiveFrom: 0, CostRatio: 2.5},
			{ChannelId: 3, EffectiveFrom: boundary, CostRatio: 2.0},
		},
	}
	c := newCostCubeWithGranularity(costGranularityDay)
	// 两条日志刊例各 $1（quota 500000 / group_ratio 1），分落改价前后。
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 500000, Other: `{"group_ratio":1}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-02", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 500000, Other: `{"group_ratio":1}`},
	}, versions)

	rows := foldCostCube(c, costDimChannel, testChannels(), versions, 7.0, tsOn("2026-06-02", 23))
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	// $1×2.5 + $1×2.0 = ¥4.5。若成本仍在折叠末尾统一乘一个倍率，这里会得到
	// ¥5.0（全按 2.5）或 ¥4.0（全按 2.0）。
	if diff := row.CostCny - 4.5; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("cost = %v, want 4.5 (1×2.5 + 1×2.0)", row.CostCny)
	}
	if !row.RatioMixed {
		t.Fatal("RatioMixed = false, want true (price changed mid-range)")
	}
	// 加权真实倍率 = 4.5 / 2 = 2.25，落在两个版本之间。
	if diff := row.EffectiveRatio - 2.25; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("effective ratio = %v, want 2.25 (weighted across versions)", row.EffectiveRatio)
	}
	// 区间内未改价的渠道不应被标记
	if row.Priced != true {
		t.Fatalf("priced = %v, want true (both versions price successfully)", row.Priced)
	}
}

// 单一版本覆盖整个区间时 RatioMixed 必须为 false——否则"改过价"的标记会对
// 所有渠道常亮，等于没有标记。
func TestFoldCostCube_SingleVersionNotMixed(t *testing.T) {
	c := newCostCubeWithGranularity(costGranularityDay)
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 500000, Other: `{"group_ratio":1}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-02", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 500000, Other: `{"group_ratio":1}`},
	}, testVersions())
	rows := foldCostCube(c, costDimChannel, testChannels(), testVersions(), 7.0, tsOn("2026-06-02", 23))
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].RatioMixed {
		t.Fatal("RatioMixed = true, want false (one version covers the whole range)")
	}
}
