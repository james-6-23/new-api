package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

func TestBillDetailWriter_SplitByModelWithinDay(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	w, err := newBillDetailWriter(f, true)
	if err != nil {
		t.Fatal(err)
	}
	// 同一天两模型，DESC 顺序到达
	if err := w.addBatch([]*model.Log{
		{CreatedAt: tsOn("2026-06-01", 15), Username: "a", ModelName: "gpt-4o", Type: model.LogTypeConsume},
		{CreatedAt: tsOn("2026-06-01", 14), Username: "a", ModelName: "claude-3", Type: model.LogTypeConsume},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}

	if _, err := f.GetSheetIndex("2026-06-01"); err != nil {
		t.Fatalf("day sheet missing: %v", err)
	}
	// 该 sheet 内应含两处「模型：」标题行
	rows, _ := f.GetRows("2026-06-01")
	count := 0
	for _, r := range rows {
		if len(r) > 0 && strings.HasPrefix(r[0], "模型：") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("model header rows = %d, want 2", count)
	}
}

// TestBillDetailWriter_ModelHeaderReEmittedOnRoll verifies that when a sheet
// rolls mid-model-block (including when the model header row itself is the
// write that hits the soft cap), the new sheet still begins with the
// "模型：<name>" header before the first data row.
func TestBillDetailWriter_ModelHeaderReEmittedOnRoll(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	w, err := newBillDetailWriter(f, true)
	if err != nil {
		t.Fatal(err)
	}
	// Tiny cap: 1 column-header row + 1 content row before rolling.
	// softCap=2 means after writing row index 2 (rowIn==2 >= 2) we roll.
	w.softCap = 2

	// 5 rows of the same model on 2026-06-01. With softCap=2:
	//   Sheet 1: row1=column-header, row2=model-header "模型：gpt-4o"
	//            → rowIn==2 >= 2, roll before writing data row 1
	//   Sheet 2: row1=column-header, row2=model-header "模型：gpt-4o" (re-emitted)
	//            rowIn==2 >= 2, roll before writing data row 2  ... and so on.
	logs := []*model.Log{
		{CreatedAt: tsOn("2026-06-01", 10), Username: "u", ModelName: "gpt-4o", Type: model.LogTypeConsume},
		{CreatedAt: tsOn("2026-06-01", 11), Username: "u", ModelName: "gpt-4o", Type: model.LogTypeConsume},
		{CreatedAt: tsOn("2026-06-01", 12), Username: "u", ModelName: "gpt-4o", Type: model.LogTypeConsume},
		{CreatedAt: tsOn("2026-06-01", 13), Username: "u", ModelName: "gpt-4o", Type: model.LogTypeConsume},
		{CreatedAt: tsOn("2026-06-01", 14), Username: "u", ModelName: "gpt-4o", Type: model.LogTypeConsume},
	}
	if err := w.addBatch(logs); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}

	// A rolled sheet must exist.
	rolledName := "2026-06-01 (2)"
	if _, err := f.GetSheetIndex(rolledName); err != nil {
		t.Fatalf("rolled sheet %q missing: %v", rolledName, err)
	}

	// On the rolled sheet, row 2 (index 1) must be the model-header, NOT a data row.
	rows, err := f.GetRows(rolledName)
	if err != nil {
		t.Fatalf("GetRows(%q): %v", rolledName, err)
	}
	// rows[0] = column header (row 1); rows[1] = first content row (row 2)
	if len(rows) < 2 {
		t.Fatalf("rolled sheet has only %d rows, want ≥ 2", len(rows))
	}
	firstContent := rows[1]
	if len(firstContent) == 0 || !strings.HasPrefix(firstContent[0], "模型：") {
		t.Fatalf("rolled sheet first content row = %v; want model header \"模型：gpt-4o\"", firstContent)
	}
	if firstContent[0] != "模型：gpt-4o" {
		t.Fatalf("rolled sheet model header = %q, want \"模型：gpt-4o\"", firstContent[0])
	}
}

func TestBillDetailWriter_IncludesRefundRowWithTypeColumn(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	w, err := newBillDetailWriter(f, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "a", ModelName: "gpt-4o", Quota: 1000},
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 11), Username: "a", ModelName: "gpt-4o", Quota: 300},
		{Type: model.LogTypeTopup, CreatedAt: tsOn("2026-06-01", 12), Username: "a", ModelName: "gpt-4o", Quota: 99999},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}

	rows, err := f.GetRows("2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	// 表头行 + 2 数据行(消费/退款)，充值被过滤 => 共 3 行
	if len(rows) != 3 {
		t.Fatalf("expected header + 2 data rows (topup filtered), got %d: %v", len(rows), rows)
	}
	header := rows[0]
	typeIdx := -1
	for i, h := range header {
		if h == "类型" {
			typeIdx = i
		}
	}
	if typeIdx == -1 {
		t.Fatalf("header missing 类型 column: %v", header)
	}
	foundRefund := false
	for _, r := range rows[1:] {
		if typeIdx < len(r) && r[typeIdx] == "退款" {
			foundRefund = true
		}
	}
	if !foundRefund {
		t.Fatalf("no 退款 row found in detail: %v", rows)
	}
}

// TestBillDetailWriter_AlignsTaskRowsAndNegativeRefundCost verifies that rows
// sharing a request_id (pre-consume + refund of one async task) are emitted
// adjacently in chronological order, and that refund cost cells are negative.
func TestBillDetailWriter_AlignsTaskRowsAndNegativeRefundCost(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	w, err := newBillDetailWriter(f, false)
	if err != nil {
		t.Fatal(err)
	}
	// Arrival order is created_at DESC (streaming order). 非 seedance 任务：
	// 保持逐行展示（相邻对齐 + 退款负数），不参与合并。
	if err := w.addBatch([]*model.Log{
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 11), Username: "a", ModelName: "grok-video-1", Quota: 300, RequestId: "req-A",
			Other: `{"billing_stage":"refund","task_id":"t1","pre_consumed_quota":1000,"actual_quota":700}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10) + 1800, Username: "a", ModelName: "gpt-4o", Quota: 100, RequestId: "req-B"},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "a", ModelName: "grok-video-1", Quota: 1000, RequestId: "req-A",
			Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"t1"}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}

	rows, err := f.GetRows("2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected header + 3 data rows, got %d: %v", len(rows), rows)
	}
	typeIdx := -1
	for i, h := range rows[0] {
		if h == "类型" {
			typeIdx = i
		}
	}
	if typeIdx == -1 {
		t.Fatalf("header missing 类型 column: %v", rows[0])
	}
	// req-A's pair anchors at the refund's slot, chronological inside:
	// row2 = pre-consume(10:00), row3 = refund(11:00), row4 = req-B(10:30).
	if rows[1][typeIdx] != "消费" || rows[2][typeIdx] != "退款" || rows[3][typeIdx] != "消费" {
		t.Fatalf("aligned type order wrong: %v / %v / %v", rows[1][typeIdx], rows[2][typeIdx], rows[3][typeIdx])
	}
	if !strings.Contains(rows[1][0], "10:00") || !strings.Contains(rows[2][0], "11:00") || !strings.Contains(rows[3][0], "10:30") {
		t.Fatalf("aligned time order wrong: %v / %v / %v", rows[1][0], rows[2][0], rows[3][0])
	}

	// cost column: consume positive, refund negative (net = sum of the column)
	cost2, _ := f.GetCellValue("2026-06-01", "K2")
	cost3, _ := f.GetCellValue("2026-06-01", "K3")
	if cost2 != "$0.002000" {
		t.Fatalf("K2 (pre-consume cost) = %q, want $0.002000", cost2)
	}
	if cost3 != "-$0.000600" {
		t.Fatalf("K3 (refund cost) = %q, want -$0.000600", cost3)
	}
}

// seedance 同 request_id 的 预扣+退款 多行合并为一行：净额 + 全过程文字。
func TestBillDetailWriter_MergesSeedanceTaskRows(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	w, err := newBillDetailWriter(f, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.addBatch([]*model.Log{
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 12), Username: "u", TokenName: "tk", ModelName: "doubao-seedance-1-0",
			Quota: 30, RequestId: "vreq-1", Other: `{"billing_stage":"refund","task_id":"vt1","pre_consumed_quota":100,"actual_quota":70,"group_ratio":1}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "u", TokenName: "tk", ModelName: "doubao-seedance-1-0",
			Quota: 100, RequestId: "vreq-1", Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt1","model_ratio":10,"group_ratio":1,"video_unit_price":40,"video_resolution_tier":"720p"}`},
		// 非 seedance 普通行不受影响
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), Username: "u", TokenName: "tk", ModelName: "gpt-4o",
			Quota: 500, RequestId: "chat-1", Other: ``},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}
	sheet := "2026-06-01"
	// 合并后：seedance 1 行 + gpt-4o 1 行 = 2 条数据行（header 第 1 行）
	a3, _ := f.GetCellValue(sheet, "A3")
	if a3 == "" {
		t.Fatalf("expected 2 data rows, got fewer")
	}
	a4, _ := f.GetCellValue(sheet, "A4")
	if a4 != "" {
		t.Fatalf("expected exactly 2 data rows, got a 3rd: %q", a4)
	}
	found := false
	for _, row := range []string{"2", "3"} {
		mv, _ := f.GetCellValue(sheet, "E"+row)
		if mv != "doubao-seedance-1-0" {
			continue
		}
		found = true
		// 费用列 = 净额 (100-30)/500000 = $0.000140
		cost, _ := f.GetCellValue(sheet, "K"+row)
		if cost != "$0.000140" {
			t.Fatalf("merged cost = %q, want $0.000140 (net 70 quota)", cost)
		}
		billing, _ := f.GetCellValue(sheet, "L"+row)
		if !strings.Contains(billing, "预扣") || !strings.Contains(billing, "退款") {
			t.Fatalf("merged billing text must contain both stages, got %q", billing)
		}
		typ, _ := f.GetCellValue(sheet, "F"+row)
		if typ != "消费" {
			t.Fatalf("merged type = %q, want 消费", typ)
		}
	}
	if !found {
		t.Fatal("merged seedance row not found")
	}
}
