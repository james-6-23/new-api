package controller

import (
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

func assertCellNotText(t *testing.T, f *excelize.File, sheet, cell string) {
	t.Helper()
	typ, err := f.GetCellType(sheet, cell)
	if err != nil {
		t.Fatalf("GetCellType(%s!%s): %v", sheet, cell, err)
	}
	if typ == excelize.CellTypeInlineString || typ == excelize.CellTypeSharedString {
		t.Fatalf("%s!%s written as text, want numeric", sheet, cell)
	}
}

func TestWriteBillSummarySheets_InternalValues(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.rows[billSummaryKey{Day: "2026-06-01", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o"}] =
		&billSummaryRow{Quota: 1500, PromptTokens: 12, CompletionTokens: 6, CacheReadTokens: 5, CacheCreationTokens: 3}
	agg.rows[billSummaryKey{Day: "2026-06-02", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o"}] =
		&billSummaryRow{Quota: 500, PromptTokens: 2, CompletionTokens: 1, CacheReadTokens: 1, CacheCreationTokens: 0}

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	// ---- 明细对账单 (daily) ----
	daily := billDailySheetPrefix
	h, _ := f.GetCellValue(daily, "A1")
	if h != "日期" {
		t.Fatalf("daily A1 = %q, want 日期", h)
	}
	ch, _ := f.GetCellValue(daily, "C1")
	if ch != "渠道ID" {
		t.Fatalf("daily C1 = %q, want 渠道ID", ch)
	}
	// Day DESC: row2 = 06-02, row3 = 06-01
	d2, _ := f.GetCellValue(daily, "A2")
	if d2 != "2026-06-02" {
		t.Fatalf("daily A2 = %q, want 2026-06-02", d2)
	}
	// new pricing columns sit between 模型名称 and 汇总金额(美元)
	lp, _ := f.GetCellValue(daily, "F1")
	er, _ := f.GetCellValue(daily, "G1")
	if lp != "刊例价" || er != "专属倍率" {
		t.Fatalf("daily F1/G1 = %q/%q, want 刊例价/专属倍率", lp, er)
	}
	// rows filled directly (no Other) → pricing cells stay empty, not 0
	lpv, _ := f.GetCellValue(daily, "F3")
	if lpv != "" {
		t.Fatalf("daily F3 (list price, no Other) = %q, want empty", lpv)
	}
	// 新增三列表头：计费记录/请求数/刊例价金额(美元)
	br, _ := f.GetCellValue(daily, "H1")
	rc, _ := f.GetCellValue(daily, "I1")
	la, _ := f.GetCellValue(daily, "J1")
	if br != "计费记录" || rc != "请求数" || la != "刊例价金额(美元)" {
		t.Fatalf("daily H1/I1/J1 = %q/%q/%q", br, rc, la)
	}
	// quota 1500 => $0.003, six-decimal number format
	usd, _ := f.GetCellValue(daily, "K3")
	if usd != "0.003000" {
		t.Fatalf("daily K3 (USD) = %q, want 0.003000", usd)
	}
	assertCellNotText(t, f, daily, "K3")
	rate, _ := f.GetCellValue(daily, "L3")
	if rate != "7.3" {
		t.Fatalf("daily L3 (rate) = %q, want 7.3", rate)
	}
	assertCellNotText(t, f, daily, "L3")
	cny, _ := f.GetCellValue(daily, "M3")
	if cny != "0.021900" {
		t.Fatalf("daily M3 (CNY) = %q, want 0.021900", cny)
	}
	prompt, _ := f.GetCellValue(daily, "N3")
	if prompt != "12" {
		t.Fatalf("daily N3 (prompt) = %q, want 12", prompt)
	}
	assertCellNotText(t, f, daily, "N3")
	// no totals row after the last data row
	after, _ := f.GetCellValue(daily, "A4")
	if after != "" {
		t.Fatalf("daily A4 = %q, want empty (totals row removed)", after)
	}

	// 总对账单已在 v2 精简中删除，区间合计见封面（账单汇总）。
	if idx, _ := f.GetSheetIndex("总对账单"); idx != -1 {
		t.Fatalf("总对账单 sheet should no longer be generated")
	}
}

func TestWriteBillSummarySheets_ExternalMergesChannelsAndTokens(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.external = true
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk1", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), Username: "alice", ChannelId: 7, TokenName: "tk2", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1},
	})
	if got := len(agg.sortedKeys()); got != 1 {
		t.Fatalf("external groups = %d, want 1 (channel/token merged)", got)
	}

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	daily := billDailySheetPrefix
	// external layout: no 渠道ID / 令牌名称 columns
	c1, _ := f.GetCellValue(daily, "C1")
	if c1 != "模型名称" {
		t.Fatalf("external daily C1 = %q, want 模型名称", c1)
	}
	model2, _ := f.GetCellValue(daily, "C2")
	if model2 != "gpt-4o" {
		t.Fatalf("external daily C2 = %q, want gpt-4o", model2)
	}
	// merged quota 1500 => $0.003 (USD moved to I with the pricing & count columns)
	usd, _ := f.GetCellValue(daily, "I2")
	if usd != "0.003000" {
		t.Fatalf("external daily I2 (USD) = %q, want 0.003000", usd)
	}
	// 计数列：2 条消费 → 计费记录 2 / 请求数 2；无 group_ratio → 刊例价金额兜底实付
	brv, _ := f.GetCellValue(daily, "F2")
	rcv, _ := f.GetCellValue(daily, "G2")
	if brv != "2" || rcv != "2" {
		t.Fatalf("external daily F2/G2 = %q/%q, want 2/2", brv, rcv)
	}
	lav, _ := f.GetCellValue(daily, "H2")
	if lav != "0.003000" {
		t.Fatalf("external daily H2 (list amount) = %q, want 0.003000", lav)
	}
	// only one merged data row
	a3, _ := f.GetCellValue(daily, "A3")
	if a3 != "" {
		t.Fatalf("external daily A3 = %q, want empty (rows merged)", a3)
	}

}

func TestBillDetailWriter_CostIsNumeric(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	w, err := newBillDetailWriter(f, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1500, PromptTokens: 10, CompletionTokens: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}
	// billDetailColumns: cost is the 11th column (K)
	cost, _ := f.GetCellValue("2026-06-01", "K2")
	if cost != "$0.003000" {
		t.Fatalf("detail K2 (cost) = %q, want $0.003000", cost)
	}
	assertCellNotText(t, f, "2026-06-01", "K2")
}

func TestWriteBillSummarySheets_MonthGranularity(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.granularity = "month"
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-03", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-07", 11), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1},
	})

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	// daily sheet buckets by month
	day, _ := f.GetCellValue(billDailySheetPrefix, "A2")
	if day != "2026-06" {
		t.Fatalf("month-bucket daily A2 = %q, want 2026-06", day)
	}
	usd, _ := f.GetCellValue(billDailySheetPrefix, "K2")
	if usd != "0.003000" {
		t.Fatalf("month-bucket daily K2 = %q, want 0.003000", usd)
	}
}

func TestWriteBillSummarySheets_WeekLabel(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.granularity = "week"
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-03", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5},
	})

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}
	day, _ := f.GetCellValue(billDailySheetPrefix, "A2")
	if day != "2026-06-01 ~ 2026-06-07" {
		t.Fatalf("week label A2 = %q, want 2026-06-01 ~ 2026-06-07", day)
	}
}

// TestWriteBillSummarySheets_ListPriceAndRatioColumns covers the 刊例价/专属倍率
// columns: token-ratio pricing, per-call pricing, video unit price, the
// user_group_ratio-over-group_ratio priority, and the -1 sentinel fallback.
func TestWriteBillSummarySheets_ListPriceAndRatioColumns(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		// token 模型：刊例价 = model_ratio×2 = 20；user_group_ratio=-1 → 回退 group_ratio 0.8
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "a-model",
			Quota: 1000, PromptTokens: 10,
			Other: `{"model_ratio":10,"group_ratio":0.8,"user_group_ratio":-1}`},
		// 专属倍率优先：user_group_ratio=0.5 覆盖 group_ratio=1
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "b-model",
			Quota: 1000, PromptTokens: 10,
			Other: `{"model_ratio":5,"group_ratio":1,"user_group_ratio":0.5}`},
		// 视频任务：刊例价 = video_unit_price
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "c-video",
			Quota: 1000,
			Other: `{"model_ratio":10,"group_ratio":1,"video_unit_price":40}`},
		// 按次计费：刊例价 = model_price
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "d-percall",
			Quota: 1000,
			Other: `{"model_price":0.1,"group_ratio":1}`},
		// 旧日志无 Other：两列留空
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "e-legacy",
			Quota: 1000, Other: ``},
	})

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	daily := billDailySheetPrefix
	// rows sorted by model name ASC within the day: a,b,c,d,e → rows 2..6
	get := func(cell string) string {
		v, _ := f.GetCellValue(daily, cell)
		return v
	}
	if get("F2") != "20" || get("G2") != "0.8" {
		t.Fatalf("a-model price/ratio = %q/%q, want 20/0.8 (sentinel -1 falls back)", get("F2"), get("G2"))
	}
	if get("F3") != "10" || get("G3") != "0.5" {
		t.Fatalf("b-model price/ratio = %q/%q, want 10/0.5 (user_group_ratio wins)", get("F3"), get("G3"))
	}
	if get("F4") != "40" {
		t.Fatalf("c-video list price = %q, want 40 (video_unit_price)", get("F4"))
	}
	if get("F5") != "0.1" {
		t.Fatalf("d-percall list price = %q, want 0.1 (model_price)", get("F5"))
	}
	if get("F6") != "" || get("G6") != "" {
		t.Fatalf("e-legacy price/ratio = %q/%q, want empty (no Other)", get("F6"), get("G6"))
	}
	assertCellNotText(t, f, daily, "F2")
	assertCellNotText(t, f, daily, "G2")
	// 新三列：a-model 1 条消费、group_ratio 0.8 → listQuota 1250 → $0.002500
	if get("H2") != "1" || get("I2") != "1" {
		t.Fatalf("a-model records/requests = %q/%q, want 1/1", get("H2"), get("I2"))
	}
	if get("J2") != "0.002500" {
		t.Fatalf("a-model list amount = %q, want 0.002500 (1000/0.8/500000)", get("J2"))
	}
}

// TestBillSummaryAgg_PricingFirstSeenWins: 流式 DESC 顺序下组内最新一条日志的
// 定价胜出，旧值不覆盖。
func TestBillSummaryAgg_PricingFirstSeenWins(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		// 最新一条（DESC 首见）：ratio 0.5
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"model_ratio":10,"group_ratio":0.5}`},
		// 更早一条：ratio 1（不应覆盖）
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"model_ratio":8,"group_ratio":1}`},
	})
	keys := agg.sortedKeys()
	if len(keys) != 1 {
		t.Fatalf("groups = %d, want 1", len(keys))
	}
	r := agg.rows[keys[0]]
	if !r.hasPrice || r.ListPriceUSD != 20 {
		t.Fatalf("list price = %v (has=%v), want 20 from newest log", r.ListPriceUSD, r.hasPrice)
	}
	if !r.hasRatio || r.EffectiveRatio != 0.5 {
		t.Fatalf("ratio = %v (has=%v), want 0.5 from newest log", r.EffectiveRatio, r.hasRatio)
	}
}

// tiered_expr 日志：刊例价取匹配档 p 系数（真实 $/1M，不乘 2），不回退 model_ratio。
func TestBillSummaryAgg_TieredExprListPrice(t *testing.T) {
	expr := `len <= 200000 ? tier("standard", p * 3 + c * 15) : tier("long_context", p * 6 + c * 22.5)`
	b64 := base64.StdEncoding.EncodeToString([]byte(expr))
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "m-tier",
			Quota: 100, Other: `{"billing_mode":"tiered_expr","expr_b64":"` + b64 + `","matched_tier":"long_context","model_ratio":0,"group_ratio":1}`},
	})
	r := agg.rows[agg.sortedKeys()[0]]
	if !r.hasPrice || r.ListPriceUSD != 6 {
		t.Fatalf("tiered list price = %v (has=%v), want 6 (matched tier p coefficient)", r.ListPriceUSD, r.hasPrice)
	}
}

// tiered 行解析失败（expr_b64 损坏）不得回退 model_ratio×2，也不得阻止组内
// 其他正常日志（更早的倍率计费日志）提供价格。
func TestBillSummaryAgg_TieredExprBrokenNoFallbackWithinRow(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"billing_mode":"tiered_expr","expr_b64":"!!!not-base64!!!","model_ratio":10,"group_ratio":1}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"model_ratio":8,"group_ratio":1}`},
	})
	r := agg.rows[agg.sortedKeys()[0]]
	if !r.hasPrice || r.ListPriceUSD != 16 {
		t.Fatalf("list price = %v (has=%v), want 16 from the older ratio log (tiered row must not contribute model_ratio×2)", r.ListPriceUSD, r.hasPrice)
	}
}

// matched_tier 缺失 → 取第一档。
func TestBillSummaryAgg_TieredExprDefaultsToFirstTier(t *testing.T) {
	expr := `tier("only", p * 1.25 + c * 5)`
	b64 := base64.StdEncoding.EncodeToString([]byte(expr))
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"billing_mode":"tiered_expr","expr_b64":"` + b64 + `","group_ratio":1}`},
	})
	r := agg.rows[agg.sortedKeys()[0]]
	if !r.hasPrice || r.ListPriceUSD != 1.25 {
		t.Fatalf("list price = %v, want 1.25 (first tier fallback)", r.ListPriceUSD)
	}
}
