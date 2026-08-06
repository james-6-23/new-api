package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// 分组折扣 0.8：实付 800，刊例应还原为 1000。
func TestCostCube_ListVsActualWithGroupDiscount(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 800, PromptTokens: 10, CompletionTokens: 5,
			Other: `{"model_ratio":2,"group_ratio":0.8}`},
	})
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
	})
	// 场景2: 预扣100 补扣20 → 净120（settle 不计请求）
	c.addBatch([]*model.Log{
		mk(model.LogTypeConsume, 100, "pre_consume", "2026-06-02", 9),
		mk(model.LogTypeConsume, 20, "settle", "2026-06-02", 10),
	})
	// 场景3: 预扣100 退30 → 净70
	c.addBatch([]*model.Log{
		mk(model.LogTypeConsume, 100, "pre_consume", "2026-06-03", 9),
		mk(model.LogTypeRefund, 30, "refund", "2026-06-03", 10),
	})
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
	})
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

// effectiveChannelRatio：ratio 模式取 CostRatio；discount 模式取 CostDiscount×汇率；
// 未知渠道/未填 → 0。
func TestEffectiveChannelRatio(t *testing.T) {
	chs := testChannels()
	cases := []struct {
		name      string
		id        int
		rate      float64
		wantRatio float64
		wantName  string
	}{
		{"ratio-mode", 3, 7.0, 2.5, "openai-a"},
		{"discount-mode", 9, 6.8, 0.8 * 6.8, "disc"},
		{"unknown-channel", 999, 6.8, 0, ""},
		{"discount-mode-zero-discount", 10, 6.8, 0, "zero-disc"},
	}
	chs[10] = &model.ChannelCostInfo{Id: 10, Name: "zero-disc", CostMode: "discount", CostDiscount: 0}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ratio, name := effectiveChannelRatio(chs, tc.id, tc.rate)
			if diff := ratio - tc.wantRatio; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("ratio = %v, want %v", ratio, tc.wantRatio)
			}
			if name != tc.wantName {
				t.Fatalf("name = %q, want %q", name, tc.wantName)
			}
		})
	}
}

// discount 模式渠道折叠：quota 500 / group_ratio 1 → list=500；
// cost_cny = 500/500000 * 0.8 * 6.8 = 0.00544；Priced=true。
func TestFoldCostCube_DiscountModeChannel(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 9, ModelName: "gpt-4o", Quota: 500, PromptTokens: 1, CompletionTokens: 1,
			Other: `{"model_ratio":1,"group_ratio":1}`},
	})
	rows := foldCostCube(c, costDimChannel, testChannels(), 6.8)
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
// 信息：cost_ratio 保持原始配置值（折扣渠道为 0），effective_ratio 携带折扣渠道的
// 实际生效倍率，另附 cost_mode/cost_discount/is_aggregator/sub_suppliers，供前端
// 按计价模式渲染（channel 9 为 discount 模式 + 聚合商 + 一个子供应商）。
func TestFoldCostCube_ChannelDimSupplierExtras(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 9, ModelName: "gpt-4o", Quota: 500, PromptTokens: 1, CompletionTokens: 1,
			Other: `{"model_ratio":1,"group_ratio":1}`},
	})
	chs := testChannels()
	rows := foldCostCube(c, costDimChannel, chs, 6.8)
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
// ch3 为 ratio 模式（cost_ratio 2.5，生效倍率同值）；ch9 为 discount 模式
// （cost_discount 0.8，生效倍率 0.8×汇率）。
func TestFoldCostCube_BreakdownCarriesChannelPricing(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 800, Other: `{"group_ratio":0.8}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), UserId: 1, Username: "alice",
			ChannelId: 9, ModelName: "gpt-4o", Quota: 500, Other: `{"group_ratio":1}`},
	})
	rows := foldCostCube(c, costDimUser, testChannels(), 6.8)
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

// TestAttachUserGroupRatios 用户折扣补齐：
// 已知分组且配置了倍率 → 填充且 GroupRatioKnown=true；
// 用户已删除（映射里没有）→ 字段留空；
// 分组存在但未配置倍率 → 只填分组名，Known=false（未配置 ≠ 不打折）；
// 配置了专属倍率（GroupGroupRatio[G][G]）→ 专属优先，GroupRatioSpecial=true；
// breakdown 明细行：models/channels 维度按自身 Username 补齐，users 维度回退父行用户名。
func TestAttachUserGroupRatios(t *testing.T) {
	// 注入专属倍率：sp_vip 用户使用自身分组令牌时 0.7（优先于一维配置的 0.9）。
	if err := ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1,"svip":1,"sp_vip":0.9}`); err != nil {
		t.Fatal(err)
	}
	if err := ratio_setting.UpdateGroupGroupRatioByJSONString(`{"sp_vip":{"sp_vip":0.7}}`); err != nil {
		t.Fatal(err)
	}

	rows := []costDimensionRow{
		// users 维度父行 + 折叠掉用户名的明细行（应回退父行用户名）
		{Username: "alice", Breakdown: []costBreakdownRow{
			{ChannelId: 3, ModelName: "gpt-4o"},
		}},
		{Username: "ghost"},
		{Username: "carol"},
		{Username: "sam"},
		// models 维度父行（无单一用户）+ 携带用户名的明细行
		{ModelName: "gpt-4o", Breakdown: []costBreakdownRow{
			{Username: "alice", ChannelId: 3},
			{Username: "sam", ChannelId: 3},
			{Username: "ghost", ChannelId: 3},
		}},
	}
	attachUserGroupRatios(rows, map[string]string{
		"alice": "default",
		"carol": "no_ratio_group",
		"sam":   "sp_vip",
	})

	if rows[0].UserGroup != "default" || !rows[0].GroupRatioKnown || rows[0].GroupRatio != 1 || rows[0].GroupRatioSpecial {
		t.Fatalf("alice: %+v", rows[0])
	}
	if b := rows[0].Breakdown[0]; b.UserGroup != "default" || !b.GroupRatioKnown || b.GroupRatio != 1 {
		t.Fatalf("alice breakdown must inherit parent username: %+v", b)
	}
	if rows[1].UserGroup != "" || rows[1].GroupRatioKnown {
		t.Fatalf("deleted user must stay empty: %+v", rows[1])
	}
	if rows[2].UserGroup != "no_ratio_group" || rows[2].GroupRatioKnown || rows[2].GroupRatio != 0 {
		t.Fatalf("unconfigured group: %+v", rows[2])
	}
	if rows[3].UserGroup != "sp_vip" || !rows[3].GroupRatioKnown || rows[3].GroupRatio != 0.7 || !rows[3].GroupRatioSpecial {
		t.Fatalf("dedicated ratio must win over group ratio: %+v", rows[3])
	}
	// models 维度：父行不补（无单一用户），明细行按各自 Username 补
	if rows[4].UserGroup != "" || rows[4].GroupRatioKnown {
		t.Fatalf("models-dim parent must stay empty: %+v", rows[4])
	}
	if b := rows[4].Breakdown[0]; b.UserGroup != "default" || !b.GroupRatioKnown {
		t.Fatalf("models-dim breakdown alice: %+v", b)
	}
	if b := rows[4].Breakdown[1]; b.GroupRatio != 0.7 || !b.GroupRatioSpecial {
		t.Fatalf("models-dim breakdown sam must use dedicated ratio: %+v", b)
	}
	if b := rows[4].Breakdown[2]; b.UserGroup != "" || b.GroupRatioKnown {
		t.Fatalf("models-dim breakdown ghost must stay empty: %+v", b)
	}
}

// TestAttachUserGroupRatios_CrossGroupDedicatedRatio 跨分组专属倍率：
// groupGroupRatio 的语义是 [用户分组][使用分组]，用户在 vip 组、拿 default 分组的
// 令牌发请求时应命中 {"vip":{"default":0.7}}。旧实现只查对角线
// GetGroupGroupRatio(g, g)，这类配置查不到、会静默回退一维分组倍率。
func TestAttachUserGroupRatios_CrossGroupDedicatedRatio(t *testing.T) {
	if err := ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":0.9}`); err != nil {
		t.Fatal(err)
	}
	if err := ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"default":0.7}}`); err != nil {
		t.Fatal(err)
	}

	// dave 属于 vip 组，本区间内的消费全部走 default 使用分组。
	rows := []costDimensionRow{{Username: "dave", UsingGroupQuota: map[string]float64{"default": 1000}}}
	attachUserGroupRatios(rows, map[string]string{"dave": "vip"})

	if !rows[0].GroupRatioKnown || !rows[0].GroupRatioSpecial {
		t.Fatalf("cross-group dedicated ratio must be found: %+v", rows[0])
	}
	if rows[0].GroupRatio != 0.7 {
		t.Fatalf("group_ratio = %v, want 0.7 (dedicated), not 0.9 (plain vip ratio)", rows[0].GroupRatio)
	}
}

// TestAttachUserGroupRatios_MixedUsingGroups 用户在区间内跨多个使用分组消费时，
// 配置折扣按各分组额度加权，并置 GroupRatioMixed 供前端标注。
func TestAttachUserGroupRatios_MixedUsingGroups(t *testing.T) {
	if err := ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":0.9}`); err != nil {
		t.Fatal(err)
	}
	if err := ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"default":0.7,"vip":0.5}}`); err != nil {
		t.Fatal(err)
	}

	// 3/4 额度走 default(0.7)、1/4 走 vip(0.5) → 0.7*0.75 + 0.5*0.25 = 0.65
	rows := []costDimensionRow{{Username: "erin",
		UsingGroupQuota: map[string]float64{"default": 750, "vip": 250}}}
	attachUserGroupRatios(rows, map[string]string{"erin": "vip"})

	if !rows[0].GroupRatioMixed {
		t.Fatalf("multiple using-groups must set GroupRatioMixed: %+v", rows[0])
	}
	if got := rows[0].GroupRatio; got < 0.6499 || got > 0.6501 {
		t.Fatalf("weighted group_ratio = %v, want 0.65", got)
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
	})
	return c
}

// 金额换算：QuotaPerUnit=500000, 汇率 7.0, ch3 倍率 2.5
// alice: revenue_usd=800/5e5=0.0016, revenue_cny=0.0112, list_usd=0.002, cost=0.005, profit=0.0062
func TestFoldCostCube_UserDimensionMoney(t *testing.T) {
	rows := foldCostCube(seedCube(), costDimUser, testChannels(), 7.0)
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

// 未填倍率渠道：成本 0、Priced=false；渠道维度带 user_count。
func TestFoldCostCube_ChannelDimensionUnpriced(t *testing.T) {
	rows := foldCostCube(seedCube(), costDimChannel, testChannels(), 7.0)
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
		for _, r := range foldCostCube(cube, dim, chs, 7.0) {
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
	m := costMoneyFromRow(&costCubeRow{}, 2.5, 7.0)
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
	})

	rows := foldCostCube(c, costDimUser, testChannels(), 7.0)
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
	m1 := costMoneyFromRow(&costCubeRow{}, 2.5, 7.0)
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
	m2 := costMoneyFromRow(&costCubeRow{PromptTokens: 0, CacheReadTokens: 10}, 2.5, 7.0)
	if m2.CacheRate != 1 {
		t.Fatalf("cache_rate = %v, want 1", m2.CacheRate)
	}
	if m2.TotalTokens != 10 {
		t.Fatalf("total_tokens = %d, want 10", m2.TotalTokens)
	}

	m3 := costMoneyFromRow(&costCubeRow{FrtCount: 0, FrtSumMs: 999}, 2.5, 7.0)
	if m3.AvgTtftMs != 0 {
		t.Fatalf("avg_ttft_ms = %v, want 0", m3.AvgTtftMs)
	}

	m4 := costMoneyFromRow(&costCubeRow{RequestCount: 3, ErrorCount: 1, PromptTokens: 100, CompletionTokens: 20, CacheReadTokens: 25, CacheCreationTokens: 15, FrtCount: 2, FrtSumMs: 200}, 2.5, 7.0)
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
	})

	rows := foldCostCube(c, costDimUser, testChannels(), 7.0)
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
	})
	rows := foldCostCube(c, costDimUser, testChannels(), 7.0)
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
	})
	chs := testChannels()

	var first []string
	for i := 0; i < 5; i++ {
		rows := foldCostCube(c, costDimUser, chs, 7.0)
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
