package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestBuildBillingText_TaskPreConsumeVideo(t *testing.T) {
	log := &model.Log{
		Type:  model.LogTypeConsume,
		Quota: 73000,
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"task-1","model_ratio":10,"group_ratio":1,"video_unit_price":40,"video_resolution_tier":"720p","video_has_input":true}`,
	}
	text := buildBillingText(log)
	for _, want := range []string{
		"任务预扣费（估算，任务完成后按实际用量结算，多退少补）",
		"预扣金额 $0.146000",
		"单价 $40.000000 / 1M tokens（720p，含视频输入） × 分组倍率 1",
		"任务 task-1",
		billingDisclaimer,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("pre-consume text missing %q:\n%s", want, text)
		}
	}
}

func TestBuildBillingText_TaskSettle(t *testing.T) {
	log := &model.Log{
		Type:  model.LogTypeConsume,
		Quota: 7000,
		Other: `{"billing_stage":"settle","task_id":"task-1","pre_consumed_quota":73000,"actual_quota":80000,"video_tokens":2000,"video_unit_price":40,"group_ratio":1}`,
	}
	text := buildBillingText(log)
	for _, want := range []string{
		"实际结算 = 2000 tokens × 单价 $40.000000 / 1M tokens × 分组倍率 1 = 应扣 $0.160000",
		"预扣 $0.146000 → 实扣 $0.160000，补扣 $0.014000",
		"任务 task-1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("settle text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, billingDisclaimer) {
		t.Fatalf("settle is final, must not carry the reference disclaimer:\n%s", text)
	}
}

func TestBuildBillingText_TaskRefundDelta(t *testing.T) {
	log := &model.Log{
		Type:  model.LogTypeRefund,
		Quota: 23000,
		Other: `{"billing_stage":"refund","task_id":"task-1","pre_consumed_quota":73000,"actual_quota":50000,"group_ratio":1}`,
	}
	text := buildBillingText(log)
	if !strings.Contains(text, "预扣 $0.146000 → 实扣 $0.100000，退款 $0.046000") {
		t.Fatalf("refund delta line wrong:\n%s", text)
	}
}

func TestBuildBillingText_TaskFullRefund(t *testing.T) {
	log := &model.Log{
		Type:  model.LogTypeRefund,
		Quota: 73000,
		Other: `{"billing_stage":"refund","task_id":"task-2","reason":"上游任务失败"}`,
	}
	text := buildBillingText(log)
	for _, want := range []string{
		"任务退款：退还预扣 $0.146000",
		"原因 上游任务失败",
		"任务 task-2",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("full-refund text missing %q:\n%s", want, text)
		}
	}
}

func TestBuildBillingText_NonTaskRatioUnchanged(t *testing.T) {
	log := &model.Log{
		Type:             model.LogTypeConsume,
		Quota:            1500,
		PromptTokens:     100,
		CompletionTokens: 50,
		Other:            `{"model_ratio":10,"group_ratio":1,"completion_ratio":2,"user_group_ratio":-1}`,
	}
	text := buildBillingText(log)
	if !strings.Contains(text, "输入价格：$20.000000 / 1M tokens") {
		t.Fatalf("ratio branch regressed:\n%s", text)
	}
	if strings.Contains(text, "任务") {
		t.Fatalf("non-task row must not use the task template:\n%s", text)
	}
}
