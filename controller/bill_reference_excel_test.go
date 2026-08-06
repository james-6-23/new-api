package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

func TestWriteBillReferenceSheets(t *testing.T) {
	ref := newBillRefAgg()
	ref.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-02", 12), Username: "u1", TokenName: "tk1", ModelName: "m1",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5, Other: `{"model_ratio":10,"group_ratio":0.5}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "u1", TokenName: "tk2", ModelName: "m2",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1, Other: `{"group_ratio":1}`},
	})

	f := excelize.NewFile()
	defer f.Close()
	styles, err := newBillExcelStyles(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBillReferenceSheets(f, styles, ref, false, 7.3); err != nil {
		t.Fatal(err)
	}

	get := func(sheet, cell string) string {
		v, _ := f.GetCellValue(sheet, cell)
		return v
	}
	// 精简版：仅输出 按模型汇总；按日/按令牌汇总不再生成
	if idx, _ := f.GetSheetIndex(billByDaySheetPrefix); idx != -1 {
		t.Fatalf("按日汇总 should no longer be generated")
	}
	if idx, _ := f.GetSheetIndex(billByTokenSheetPrefix); idx != -1 {
		t.Fatalf("按令牌汇总 should no longer be generated")
	}
	// 按模型汇总：Quota DESC → m1 在前；首/末笔时间列在 K/L
	if get(billByModelSheetPrefix, "A1") != "模型名称" || get(billByModelSheetPrefix, "A2") != "m1" || get(billByModelSheetPrefix, "A3") != "m2" {
		t.Fatalf("byModel rows: %q/%q/%q", get(billByModelSheetPrefix, "A1"), get(billByModelSheetPrefix, "A2"), get(billByModelSheetPrefix, "A3"))
	}
	if get(billByModelSheetPrefix, "B2") != "1" || get(billByModelSheetPrefix, "C2") != "1" {
		t.Fatalf("byModel counts = %q/%q", get(billByModelSheetPrefix, "B2"), get(billByModelSheetPrefix, "C2"))
	}
	if get(billByModelSheetPrefix, "H2") != "1000" {
		t.Fatalf("byModel quota units = %q, want 1000", get(billByModelSheetPrefix, "H2"))
	}
	if get(billByModelSheetPrefix, "I2") != "0.004000" {
		t.Fatalf("byModel list amount = %q, want 0.004000", get(billByModelSheetPrefix, "I2"))
	}
	if get(billByModelSheetPrefix, "J2") != "0.002000" {
		t.Fatalf("byModel amount = %q, want 0.002000", get(billByModelSheetPrefix, "J2"))
	}
	assertCellNotText(t, f, billByModelSheetPrefix, "I2")
	if get(billByModelSheetPrefix, "K1") != "首笔计费时间" || get(billByModelSheetPrefix, "L1") != "末笔计费时间" {
		t.Fatalf("byModel ts headers: %q/%q", get(billByModelSheetPrefix, "K1"), get(billByModelSheetPrefix, "L1"))
	}
	if get(billByModelSheetPrefix, "K2") != "2026-06-02 12:00:00" {
		t.Fatalf("byModel first ts = %q", get(billByModelSheetPrefix, "K2"))
	}
}

func TestWriteBillReferenceSheets_WithUserColumn(t *testing.T) {
	ref := newBillRefAgg()
	ref.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "u1", TokenName: "tk1", ModelName: "m1",
			Quota: 100, Other: `{"group_ratio":1}`},
	})
	f := excelize.NewFile()
	defer f.Close()
	styles, _ := newBillExcelStyles(f)
	if err := writeBillReferenceSheets(f, styles, ref, true, 7.3); err != nil {
		t.Fatal(err)
	}
	u, _ := f.GetCellValue(billByModelSheetPrefix, "A1")
	n, _ := f.GetCellValue(billByModelSheetPrefix, "B1")
	if u != "用户名" || n != "模型名称" {
		t.Fatalf("byModel withUser headers = %q/%q", u, n)
	}
}

func TestWriteBillCoverSheet(t *testing.T) {
	ref := newBillRefAgg()
	ref.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "u1", TokenName: "tk", ModelName: "m",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5, Other: `{"model_ratio":10,"group_ratio":0.5}`},
	})
	f := excelize.NewFile()
	defer f.Close()
	styles, _ := newBillExcelStyles(f)
	meta := billCoverMeta{
		Customer: "u1", StartTs: tsOn("2026-06-01", 0), EndTs: tsOn("2026-06-02", 0),
		ExchangeRate: 7.3, Filters: []string{"模型=m"}, Truncated: true, MaxRows: 100,
	}
	if err := writeBillCoverSheet(f, styles, meta, ref.totals()); err != nil {
		t.Fatal(err)
	}
	get := func(cell string) string {
		v, _ := f.GetCellValue(billCoverSheetName, cell)
		return v
	}
	if get("A1") != "账单详情" {
		t.Fatalf("A1 = %q", get("A1"))
	}
	if get("A4") != "客户" || get("B4") != "u1" {
		t.Fatalf("customer = %q/%q", get("A4"), get("B4"))
	}
	if get("B5") != "2026-06-01 00:00:00" || get("D5") != "2026-06-02 00:00:00" {
		t.Fatalf("range = %q..%q", get("B5"), get("D5"))
	}
	if get("A9") != "计费记录" || get("B9") != "1" || get("C9") != "请求数" || get("D9") != "1" {
		t.Fatalf("counts row: %q=%q %q=%q", get("A9"), get("B9"), get("C9"), get("D9"))
	}
	if get("B12") != "1000" {
		t.Fatalf("quota units = %q, want 1000", get("B12"))
	}
	if get("B15") != "USD = quota_units / 500000" {
		t.Fatalf("金额口径 = %q", get("B15"))
	}
	if get("B16") != "0.004000" || get("D16") != "0.002000" {
		t.Fatalf("list/actual USD = %q/%q", get("B16"), get("D16"))
	}
	if get("B17") != "7.3" || get("D17") != "0.014600" {
		t.Fatalf("rate/CNY = %q/%q", get("B17"), get("D17"))
	}
	// 说明区：筛选回显 + 截断提示
	if get("A20") != "筛选：模型=m" {
		t.Fatalf("filter echo = %q", get("A20"))
	}
	if get("A21") != "数据已按上限 100 行截断，金额仅覆盖已导出行" {
		t.Fatalf("truncation note = %q", get("A21"))
	}
}

// 精简版 sheet 顺序：账单汇总, 账单明细, 按模型汇总, 逐日明细；激活 sheet 为账单汇总。
func TestFinalizeBillWorkbook_ReferenceSheetOrder(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	f.NewSheet("2026-06-01") // 逐日明细在流式阶段先创建

	agg := newBillSummaryAgg()
	ref := newBillRefAgg()
	logs := []*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "u1", TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"group_ratio":1}`},
	}
	agg.addBatch(logs)
	ref.addBatch(logs)
	styles, _ := newBillExcelStyles(f)
	if err := writeBillCoverSheet(f, styles, billCoverMeta{ExchangeRate: 7.3}, ref.totals()); err != nil {
		t.Fatal(err)
	}
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}
	if err := writeBillReferenceSheets(f, styles, ref, true, 7.3); err != nil {
		t.Fatal(err)
	}

	finalizeBillWorkbook(f)

	list := f.GetSheetList()
	want := []string{billCoverSheetName, billDailySheetPrefix, billByModelSheetPrefix, "2026-06-01"}
	if len(list) != len(want) {
		t.Fatalf("sheet list = %v, want %v", list, want)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("sheet[%d] = %q, want %q (full %v)", i, list[i], want[i], list)
		}
	}
	if got := f.GetSheetName(f.GetActiveSheetIndex()); got != billCoverSheetName {
		t.Fatalf("active sheet = %q, want %q", got, billCoverSheetName)
	}
}
