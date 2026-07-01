package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/xuri/excelize/v2"
)

func TestWriteBillSummarySheet_ValuesAndTotals(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.rows[billSummaryKey{Day: "2026-06-01", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o"}] =
		&billSummaryRow{Quota: 1500, PromptTokens: 12, CompletionTokens: 6, CacheReadTokens: 5, CacheCreationTokens: 3}

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheet(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	// header
	h, _ := f.GetCellValue(billSummarySheetPrefix, "A1")
	if h != "日期" {
		t.Fatalf("A1 = %q, want 日期", h)
	}
	// data row
	usd, _ := f.GetCellValue(billSummarySheetPrefix, "F2")
	wantUSD := formatPrice(1500 / common.QuotaPerUnit)
	if usd != wantUSD {
		t.Fatalf("USD cell = %q, want %q", usd, wantUSD)
	}
	rate, _ := f.GetCellValue(billSummarySheetPrefix, "G2")
	if rate != "7.3" {
		t.Fatalf("rate cell = %q, want 7.3", rate)
	}
	prompt, _ := f.GetCellValue(billSummarySheetPrefix, "I2")
	if prompt != "12" {
		t.Fatalf("prompt cell = %q, want 12", prompt)
	}
}
