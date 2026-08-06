package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// 原始日志导出单行化：settle/refund 行剔除；预扣行费用改写为实扣净额、
// 输出 tokens 落列；非 seedance 行与聊天行原样透传。
func TestApplySeedanceExportMerge(t *testing.T) {
	pre := &model.Log{
		Type: model.LogTypeConsume, ModelName: "doubao-seedance-1-0", Quota: 100, RequestId: "vreq-1",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt1"}`,
		TaskInfo: &model.LogTaskInfo{
			Status: "SUCCESS", Progress: "100%", PreQuota: 100, FinalQuota: 150,
			OutputTokens: 123, TaskId: "vt1",
		},
	}
	settle := &model.Log{
		Type: model.LogTypeConsume, ModelName: "doubao-seedance-1-0", Quota: 50, RequestId: "vreq-1",
		Other: `{"billing_stage":"settle","task_id":"vt1","pre_consumed_quota":100,"actual_quota":150}`,
	}
	refund := &model.Log{
		Type: model.LogTypeRefund, ModelName: "doubao-seedance-1-0", Quota: 10, RequestId: "vreq-2",
		Other: `{"billing_stage":"refund","task_id":"vt2"}`,
	}
	otherTaskSettle := &model.Log{
		Type: model.LogTypeConsume, ModelName: "grok-video-1", Quota: 20, RequestId: "greq-1",
		Other: `{"billing_stage":"settle","task_id":"g1"}`,
	}
	chat := &model.Log{Type: model.LogTypeConsume, ModelName: "gpt-4o", Quota: 500}

	out := applySeedanceExportMerge([]*model.Log{settle, refund, pre, otherTaskSettle, chat})
	if len(out) != 3 {
		t.Fatalf("rows = %d, want 3 (seedance settle/refund dropped)", len(out))
	}
	if out[0] != pre || out[1] != otherTaskSettle || out[2] != chat {
		t.Fatalf("unexpected row set/order: %+v", out)
	}
	if pre.Quota != 150 {
		t.Fatalf("pre quota = %d, want 150 (net)", pre.Quota)
	}
	if pre.CompletionTokens != 123 {
		t.Fatalf("pre completion tokens = %d, want 123 (output tokens surfaced)", pre.CompletionTokens)
	}
}

// TaskInfo 行的计费过程文字：单行任务全过程（状态/预扣→实扣/任务 ID）。
func TestBuildBillingText_TaskInfoSingleRow(t *testing.T) {
	log := &model.Log{
		Type: model.LogTypeConsume, ModelName: "doubao-seedance-1-0", Quota: 150,
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt1"}`,
		TaskInfo: &model.LogTaskInfo{
			Status: "SUCCESS", Progress: "100%", PreQuota: 100, FinalQuota: 150,
			OutputTokens: 123, TaskId: "vt1", UpstreamTaskId: "up-9",
		},
	}
	text := buildBillingText(log)
	for _, want := range []string{"成功", "预扣", "实扣", "vt1", "up-9", "123"} {
		if !strings.Contains(text, want) {
			t.Fatalf("task billing text missing %q:\n%s", want, text)
		}
	}

	failed := &model.Log{
		Type: model.LogTypeConsume, ModelName: "doubao-seedance-1-0", Quota: 100,
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt2"}`,
		TaskInfo: &model.LogTaskInfo{
			Status: "FAILURE", PreQuota: 100, FinalQuota: 0,
			FailReason: "content policy", TaskId: "vt2",
		},
	}
	ftext := buildBillingText(failed)
	for _, want := range []string{"失败", "content policy", "已退款"} {
		if !strings.Contains(ftext, want) {
			t.Fatalf("failed task billing text missing %q:\n%s", want, ftext)
		}
	}
}
