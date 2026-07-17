package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestBuildBillSummaryPage_PagingAndTotals(t *testing.T) {
	agg := newBillSummaryAgg()
	// 3 distinct groups (same day, different model) so ordering is deterministic.
	mk := func(model string, quota, p, c, cr, cc int) {
		agg.rows[billSummaryKey{Day: "2026-06-01", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: model}] =
			&billSummaryRow{Quota: quota, PromptTokens: p, CompletionTokens: c, CacheReadTokens: cr, CacheCreationTokens: cc}
	}
	mk("a-model", 1000, 10, 5, 4, 2)
	mk("b-model", 500, 2, 1, 1, 0)
	mk("c-model", 250, 1, 1, 0, 0)

	// page 1, size 2 -> first two by sort (Day, user, channel, model ASC): a-model, b-model
	page := buildBillSummaryPage(agg, 7.3, 1, 2)
	if page.Total != 3 {
		t.Fatalf("total = %d, want 3", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	if page.Items[0].ModelName != "a-model" || page.Items[1].ModelName != "b-model" {
		t.Fatalf("unexpected page-1 order: %+v", page.Items)
	}
	// per-row money
	wantUSD := roundTo6(1000 / common.QuotaPerUnit)
	if page.Items[0].AmountUSD != wantUSD {
		t.Fatalf("row USD = %v, want %v", page.Items[0].AmountUSD, wantUSD)
	}
	if page.Items[0].AmountCNY != roundTo6(wantUSD*7.3) {
		t.Fatalf("row CNY = %v", page.Items[0].AmountCNY)
	}
	if page.Items[0].ExchangeRate != 7.3 {
		t.Fatalf("rate = %v, want 7.3", page.Items[0].ExchangeRate)
	}
	// full totals over ALL 3 groups, independent of paging
	wantTotUSD := roundTo6((1000 + 500 + 250) / common.QuotaPerUnit)
	if page.Summary.TotalAmountUSD != wantTotUSD {
		t.Fatalf("total USD = %v, want %v", page.Summary.TotalAmountUSD, wantTotUSD)
	}
	if page.Summary.TotalPromptTokens != 13 || page.Summary.TotalCompletionTokens != 7 {
		t.Fatalf("token totals wrong: %+v", page.Summary)
	}
	if page.Summary.TotalCacheReadTokens != 5 || page.Summary.TotalCacheCreationTokens != 2 {
		t.Fatalf("cache totals wrong: %+v", page.Summary)
	}

	// page 2, size 2 -> only c-model
	page2 := buildBillSummaryPage(agg, 7.3, 2, 2)
	if len(page2.Items) != 1 || page2.Items[0].ModelName != "c-model" {
		t.Fatalf("page 2 wrong: %+v", page2.Items)
	}

	// page beyond range -> empty items, total still 3
	page3 := buildBillSummaryPage(agg, 7.3, 9, 2)
	if len(page3.Items) != 0 || page3.Total != 3 {
		t.Fatalf("page 3 wrong: len=%d total=%d", len(page3.Items), page3.Total)
	}
}
