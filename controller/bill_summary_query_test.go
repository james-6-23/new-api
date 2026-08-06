package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

// 计费记录/请求数/刊例价金额透出到查询 DTO（settle 补扣行不计请求数）。
func TestBuildBillSummaryPage_CountsAndListAmount(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "a", ChannelId: 1, TokenName: "tk", ModelName: "m",
			Quota: 1000, PromptTokens: 10, Other: `{"model_ratio":10,"group_ratio":0.5}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), Username: "a", ChannelId: 1, TokenName: "tk", ModelName: "m",
			Quota: 200, Other: `{"billing_stage":"settle","group_ratio":0.5}`},
	})
	page := buildBillSummaryPage(agg, 7.3, 1, 20)
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	it := page.Items[0]
	if it.BillingRecords != 2 || it.RequestCount != 1 {
		t.Fatalf("item counts = %d/%d, want 2/1", it.BillingRecords, it.RequestCount)
	}
	if it.ListAmountUSD != 0.0048 { // (2000+400)/500000
		t.Fatalf("item list amount = %v, want 0.0048", it.ListAmountUSD)
	}
	if page.Summary.TotalBillingRecords != 2 || page.Summary.TotalRequestCount != 1 || page.Summary.TotalListAmountUSD != 0.0048 {
		t.Fatalf("summary = %+v", page.Summary)
	}
}
