package controller

import (
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
)

func timeParseLocal(day string, hour int) (int64, error) {
	t, err := time.ParseInLocation("2006-01-02 15", day+" "+itoa2(hour), time.Local)
	if err != nil {
		return 0, err
	}
	return t.Unix(), nil
}

func itoa2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// 2026-06-01 12:00:00 与 13:00:00 同一天；2026-06-02 另一天（服务器本地时区）。
func tsOn(day string, hour int) int64 {
	t, _ := timeParseLocal(day, hour)
	return t
}

func TestBillSummaryAgg_AggregatesByDayModel(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		{CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5,
			Other: `{"cache_tokens":4,"cache_creation_tokens":2,"cache_creation_tokens_5m":1}`},
		{CreatedAt: tsOn("2026-06-01", 13), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1,
			Other: `{"cache_tokens":1}`},
		{CreatedAt: tsOn("2026-06-02", 9), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 200, PromptTokens: 1, CompletionTokens: 1, Other: ``},
	})

	keys := agg.sortedKeys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(keys))
	}
	// Day DESC: 06-02 first
	if keys[0].Day != "2026-06-02" || keys[1].Day != "2026-06-01" {
		t.Fatalf("unexpected key order: %+v", keys)
	}
	r := agg.rows[keys[1]] // 2026-06-01 group
	if r.Quota != 1500 || r.PromptTokens != 12 || r.CompletionTokens != 6 {
		t.Fatalf("bad sums: %+v", r)
	}
	if r.CacheReadTokens != 5 { // 4 + 1
		t.Fatalf("cache read = %d, want 5", r.CacheReadTokens)
	}
	if r.CacheCreationTokens != 3 { // 2 + 1(5m)
		t.Fatalf("cache creation = %d, want 3", r.CacheCreationTokens)
	}
}
