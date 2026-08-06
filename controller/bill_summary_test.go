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
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5,
			Other: `{"cache_tokens":4,"cache_creation_tokens":2,"cache_creation_tokens_5m":1}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 13), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1,
			Other: `{"cache_tokens":1}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-02", 9), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
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
	// cache_creation_tokens 本身即总量，_5m/_1h 是其拆分项，不能相加（会翻倍）。
	if r.CacheCreationTokens != 2 { // max(2, 1(5m)) = 2
		t.Fatalf("cache creation = %d, want 2", r.CacheCreationTokens)
	}
}

func TestBillSummaryAgg_RefundNetsConsumption(t *testing.T) {
	agg := newBillSummaryAgg()
	// 同一分组：消费 1000 + 退款 300 => 净 700；tokens 只来自消费
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5, Other: `{"cache_tokens":4}`},
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 11), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 300, PromptTokens: 0, CompletionTokens: 0, Other: ``},
		// 充值日志必须被忽略
		{Type: model.LogTypeTopup, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 99999, Other: ``},
	})

	keys := agg.sortedKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 group (topup ignored), got %d: %+v", len(keys), keys)
	}
	r := agg.rows[keys[0]]
	if r.Quota != 700 {
		t.Fatalf("net quota = %d, want 700 (1000 consume - 300 refund)", r.Quota)
	}
	if r.PromptTokens != 10 || r.CompletionTokens != 5 || r.CacheReadTokens != 4 {
		t.Fatalf("tokens must come from consume only: %+v", r)
	}
}

func TestBillSummaryAgg_ExternalMergesChannelAndToken(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.external = true
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk1", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), Username: "alice", ChannelId: 7, TokenName: "tk2", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1},
		// 不同模型仍单独成组
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 7, TokenName: "tk2", ModelName: "gpt-4o-mini",
			Quota: 100, PromptTokens: 1, CompletionTokens: 1},
	})
	keys := agg.sortedKeys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 groups (channels/tokens merged, models split), got %d: %+v", len(keys), keys)
	}
	for _, k := range keys {
		if k.ChannelId != 0 || k.TokenName != "" {
			t.Fatalf("external key must zero channel/token, got %+v", k)
		}
	}
	merged := agg.rows[billSummaryKey{Day: "2026-06-01", Username: "alice", ModelName: "gpt-4o"}]
	if merged == nil || merged.Quota != 1500 || merged.PromptTokens != 12 {
		t.Fatalf("merged row wrong: %+v", merged)
	}
}

func TestBillSummaryAgg_RefundOnlyGroupIsNegative(t *testing.T) {
	agg := newBillSummaryAgg()
	// 退款独立 key（模型不同于任何消费）=> 单独成行，净额为负
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 9), Username: "bob", ChannelId: 1, TokenName: "t2", ModelName: "refunded-model",
			Quota: 500, Other: ``},
	})
	keys := agg.sortedKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 group, got %d", len(keys))
	}
	if agg.rows[keys[0]].Quota != -500 {
		t.Fatalf("refund-only quota = %d, want -500", agg.rows[keys[0]].Quota)
	}
}

func TestBillBucketDayAndLabel(t *testing.T) {
	at := func(day string) time.Time {
		tm, err := time.ParseInLocation("2006-01-02 15", day+" 12", time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	// 2026-06-01 is a Monday; 06-03 Wed and 06-07 Sun share its week, 06-08 starts the next.
	if got := billBucketDay(at("2026-06-03"), "week"); got != "2026-06-01" {
		t.Fatalf("week bucket(2026-06-03) = %q, want 2026-06-01", got)
	}
	if got := billBucketDay(at("2026-06-07"), "week"); got != "2026-06-01" {
		t.Fatalf("week bucket(2026-06-07) = %q, want 2026-06-01", got)
	}
	if got := billBucketDay(at("2026-06-08"), "week"); got != "2026-06-08" {
		t.Fatalf("week bucket(2026-06-08) = %q, want 2026-06-08", got)
	}
	if got := billBucketDay(at("2026-06-15"), "month"); got != "2026-06" {
		t.Fatalf("month bucket = %q, want 2026-06", got)
	}
	if got := billBucketDay(at("2026-06-15"), "day"); got != "2026-06-15" {
		t.Fatalf("day bucket = %q, want 2026-06-15", got)
	}
	if got := billPeriodLabel("2026-06-01", "week"); got != "2026-06-01 ~ 2026-06-07" {
		t.Fatalf("week label = %q", got)
	}
	if got := billPeriodLabel("2026-06", "month"); got != "2026-06" {
		t.Fatalf("month label = %q", got)
	}
}

func TestBillSummaryAgg_WeekGranularityBucketsAndRealRange(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.granularity = "week"
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-03", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-07", 11), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1},
	})
	keys := agg.sortedKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 weekly group, got %d: %+v", len(keys), keys)
	}
	if keys[0].Day != "2026-06-01" {
		t.Fatalf("weekly bucket = %q, want 2026-06-01 (Monday)", keys[0].Day)
	}
	if agg.rows[keys[0]].Quota != 1500 {
		t.Fatalf("weekly quota = %d, want 1500", agg.rows[keys[0]].Quota)
	}
	// real calendar range is tracked independently of the bucket
	if agg.minDay != "2026-06-03" || agg.maxDay != "2026-06-07" {
		t.Fatalf("real range = %q..%q, want 2026-06-03..2026-06-07", agg.minDay, agg.maxDay)
	}
}

// 任务三行（pre_consume 消费 + settle 补扣消费 + refund 退款）：
// 计费记录 3、请求数 1；刊例价金额按 quota/倍率 逐条累加、退款冲抵。
func TestBillSummaryAgg_CountsAndListQuota(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		// 普通消费：group_ratio 0.5 → listQuota 2000
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), Username: "a", ChannelId: 1, TokenName: "tk", ModelName: "m",
			Quota: 1000, PromptTokens: 10, Other: `{"model_ratio":10,"group_ratio":0.5,"user_group_ratio":-1}`},
		// 任务 settle 补扣：请求数不计；user_group_ratio 0.5 优先 → listQuota 400
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), Username: "a", ChannelId: 1, TokenName: "tk", ModelName: "m",
			Quota: 200, Other: `{"billing_stage":"settle","group_ratio":1,"user_group_ratio":0.5}`},
		// 任务 pre_consume：请求数计 1 → listQuota 600
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "a", ChannelId: 1, TokenName: "tk", ModelName: "m",
			Quota: 300, Other: `{"billing_stage":"pre_consume","group_ratio":0.5}`},
		// 退款：计费记录 +1、请求数不计、刊例价金额冲抵 -200
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 9), Username: "a", ChannelId: 1, TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"billing_stage":"refund","group_ratio":0.5}`},
	})
	r := agg.rows[agg.sortedKeys()[0]]
	if r.BillingRecords != 4 {
		t.Fatalf("BillingRecords = %d, want 4", r.BillingRecords)
	}
	if r.RequestCount != 2 { // 普通消费 1 + pre_consume 1
		t.Fatalf("RequestCount = %d, want 2", r.RequestCount)
	}
	if r.ListQuota != 2000+400+600-200 {
		t.Fatalf("ListQuota = %v, want 2800", r.ListQuota)
	}
	if r.Quota != 1000+200+300-100 {
		t.Fatalf("Quota = %d, want 1400", r.Quota)
	}
}

// 边界：旧日志无 Other → listQuota 兜底为 quota；免费组 quota=0 → 0；
// 无倍率键但 quota>0 → 兜底 quota。
func TestLogListQuota_Fallbacks(t *testing.T) {
	if got := logListQuota(&model.Log{Quota: 500}, nil); got != 500 {
		t.Fatalf("nil info → %v, want 500", got)
	}
	if got := logListQuota(&model.Log{Quota: 0}, &logPricingInfo{GroupRatio: 0}); got != 0 {
		t.Fatalf("free group → %v, want 0", got)
	}
	if got := logListQuota(&model.Log{Quota: 700}, &logPricingInfo{}); got != 700 {
		t.Fatalf("missing ratio keys → %v, want 700 (fallback to actual)", got)
	}
	if got := logListQuota(&model.Log{Quota: 1000}, &logPricingInfo{GroupRatio: 0.8, UserGroupRatio: -1}); got != 1250 {
		t.Fatalf("sentinel -1 → %v, want 1250 (group_ratio)", got)
	}
	if got := logListQuota(&model.Log{Quota: 1000, PromptTokens: 1}, &logPricingInfo{GroupRatio: 1, UserGroupRatio: 0.5}); got != 2000 {
		t.Fatalf("user_group_ratio wins → %v, want 2000", got)
	}
}
