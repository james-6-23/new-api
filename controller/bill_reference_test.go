package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestBillRefAgg_DimensionsAndTotals(t *testing.T) {
	agg := newBillRefAgg()
	agg.addBatch([]*model.Log{
		// day2 tk1 m1：消费 1000（ratio 0.5 → list 2000）
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-02", 12), Username: "u1", TokenName: "tk1", ModelName: "m1",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5, Other: `{"model_ratio":10,"group_ratio":0.5}`},
		// day2 tk1 m1：任务 settle 补扣 200（请求数不计）
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-02", 11), Username: "u1", TokenName: "tk1", ModelName: "m1",
			Quota: 200, Other: `{"billing_stage":"settle","group_ratio":0.5}`},
		// day1 tk2 m2：消费 500 + 退款 100
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "u1", TokenName: "tk2", ModelName: "m2",
			Quota: 500, PromptTokens: 2, Other: `{"group_ratio":1}`},
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 9), Username: "u1", TokenName: "tk2", ModelName: "m2",
			Quota: 100, Other: `{"group_ratio":1}`},
		// 非账单类型跳过
		{Type: model.LogTypeTopup, CreatedAt: tsOn("2026-06-01", 8), Username: "u1", TokenName: "tk2", ModelName: "m2", Quota: 9999},
	})

	tot := agg.totals()
	if tot.BillingRecords != 4 || tot.RequestCount != 2 {
		t.Fatalf("totals records/requests = %d/%d, want 4/2", tot.BillingRecords, tot.RequestCount)
	}
	if tot.Quota != 1000+200+500-100 {
		t.Fatalf("totals quota = %d, want 1600", tot.Quota)
	}
	if tot.ListQuota != 2000+400+500-100 {
		t.Fatalf("totals listQuota = %v, want 2800", tot.ListQuota)
	}
	if tot.FirstTs != tsOn("2026-06-01", 9) || tot.LastTs != tsOn("2026-06-02", 12) {
		t.Fatalf("totals ts range = %d..%d", tot.FirstTs, tot.LastTs)
	}

	days, byDay := agg.byDay()
	if len(days) != 2 || days[0] != "2026-06-02" || days[1] != "2026-06-01" {
		t.Fatalf("days = %v, want [2026-06-02 2026-06-01] (DESC)", days)
	}
	if byDay["2026-06-02"].Quota != 1200 || byDay["2026-06-01"].Quota != 400 {
		t.Fatalf("byDay quota = %d/%d", byDay["2026-06-02"].Quota, byDay["2026-06-01"].Quota)
	}

	// Quota DESC 排序：tk1(1200) 在 tk2(400) 前；withUser=false 合并用户
	tokens, byTok := agg.byToken(false)
	if len(tokens) != 2 || tokens[0].Name != "tk1" || tokens[1].Name != "tk2" {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[0].Username != "" {
		t.Fatalf("withUser=false must blank username, got %q", tokens[0].Username)
	}
	if byTok[tokens[0]].RequestCount != 1 {
		t.Fatalf("tk1 requests = %d, want 1 (settle row not counted)", byTok[tokens[0]].RequestCount)
	}

	models, _ := agg.byModel(true)
	if len(models) != 2 || models[0].Name != "m1" || models[0].Username != "u1" {
		t.Fatalf("models = %v", models)
	}
}
