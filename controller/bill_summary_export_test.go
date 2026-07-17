package controller

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestFinalizeBillWorkbook_ActivatesSummary(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	// Simulate detail sheets created before the summary (as runBillExport does)
	f.NewSheet("2026-06-02") // fake detail sheet
	f.NewSheet("2026-06-01") // another fake detail sheet

	// Create the real summary sheet via the actual writer
	agg := newBillSummaryAgg()
	if err := writeBillSummarySheet(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	// Apply the fix under test
	finalizeBillWorkbook(f)

	// Summary sheet must be active
	activeName := f.GetSheetName(f.GetActiveSheetIndex())
	if activeName != billSummarySheetPrefix {
		t.Fatalf("active sheet = %q, want %q", activeName, billSummarySheetPrefix)
	}

	// Sheet1 must be removed
	if idx, _ := f.GetSheetIndex("Sheet1"); idx != -1 {
		t.Fatalf("Sheet1 still present at index %d, want -1", idx)
	}
}
