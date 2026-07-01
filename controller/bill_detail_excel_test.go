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
