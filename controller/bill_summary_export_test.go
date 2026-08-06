package controller

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestFinalizeBillWorkbook_OrdersAndActivatesSummaries(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	// Simulate detail sheets created before the summaries (as runBillExport does)
	f.NewSheet("2026-06-02") // fake detail sheet
	f.NewSheet("2026-06-01") // another fake detail sheet

	// Create the real summary sheet via the actual writer
	agg := newBillSummaryAgg()
	agg.rows[billSummaryKey{Day: "2026-06-01", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o"}] =
		&billSummaryRow{Quota: 1500}
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	finalizeBillWorkbook(f)

	// Summary first, then detail sheets in their original order
	list := f.GetSheetList()
	want := []string{billDailySheetPrefix, "2026-06-02", "2026-06-01"}
	if len(list) != len(want) {
		t.Fatalf("sheet list = %v, want %v", list, want)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("sheet[%d] = %q, want %q (full list %v)", i, list[i], want[i], list)
		}
	}

	// Without a cover, 账单明细 must be the active sheet
	activeName := f.GetSheetName(f.GetActiveSheetIndex())
	if activeName != billDailySheetPrefix {
		t.Fatalf("active sheet = %q, want %q", activeName, billDailySheetPrefix)
	}

	// Sheet1 must be removed
	if idx, _ := f.GetSheetIndex("Sheet1"); idx != -1 {
		t.Fatalf("Sheet1 still present at index %d, want -1", idx)
	}
}

func TestFinalizeBillWorkbook_NoDetailSheets(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	agg := newBillSummaryAgg()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	finalizeBillWorkbook(f)

	list := f.GetSheetList()
	want := []string{billDailySheetPrefix}
	if len(list) != len(want) || list[0] != want[0] {
		t.Fatalf("sheet list = %v, want %v", list, want)
	}
	activeName := f.GetSheetName(f.GetActiveSheetIndex())
	if activeName != billDailySheetPrefix {
		t.Fatalf("active sheet = %q, want %q", activeName, billDailySheetPrefix)
	}
}
