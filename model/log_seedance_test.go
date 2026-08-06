package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearLogsAndTasks(t *testing.T) {
	t.Helper()
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	t.Cleanup(func() {
		LOG_DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM tasks")
	})
}

// seedance 的 settle/refund 行从分页列表隐藏(count 同步)；预扣行保留；
// 非 seedance 的任务行不受影响。
func TestGetLogs_HidesSeedanceSettleRefundRows(t *testing.T) {
	clearLogsAndTasks(t)
	mk := func(typ int, model, other string) {
		require.NoError(t, LOG_DB.Create(&Log{
			UserId: 1, Username: "u", Type: typ, ModelName: model,
			CreatedAt: 1000, Quota: 100, Other: other,
		}).Error)
	}
	mk(LogTypeConsume, "doubao-seedance-1-0", `{"is_task":true,"billing_stage":"pre_consume","task_id":"t1"}`)
	mk(LogTypeConsume, "doubao-seedance-1-0", `{"billing_stage":"settle","task_id":"t1","pre_consumed_quota":100,"actual_quota":150}`)
	mk(LogTypeRefund, "doubao-seedance-1-0", `{"billing_stage":"refund","task_id":"t1"}`)
	// 非 seedance 任务的 settle 行必须保留
	mk(LogTypeConsume, "grok-video-1", `{"billing_stage":"settle","task_id":"g1"}`)
	// 普通聊天日志保留
	mk(LogTypeConsume, "gpt-4o", ``)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, total, "count 必须同步排除")
	models := map[string]int{}
	for _, l := range logs {
		models[l.ModelName]++
	}
	assert.Equal(t, 1, models["doubao-seedance-1-0"], "只剩预扣行")
	assert.Equal(t, 1, models["grok-video-1"])
	assert.Equal(t, 1, models["gpt-4o"])

	// self 路径同规则
	ulogs, utotal, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 100, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, utotal)
	assert.Len(t, ulogs, 3)
}

// 成功任务：状态/进度/net 金额/tokens/档位/秒数/倍率齐备；admin 可见上游 ID。
func TestEnrichSeedance_SuccessWithSettle(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "vt1", UserId: 1, Status: TaskStatusSuccess, Progress: "100%",
		Quota: 150, PrivateData: TaskPrivateData{UpstreamTaskID: "up-999", RequestId: "req-1"},
		Data: []byte(`{"model":"doubao-seedance-1-0","duration":5,"usage":{"completion_tokens":123}}`),
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1000,
		Quota: 100, RequestId: "req-1",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt1","video_resolution_tier":"720p","video_has_input":true,"group_ratio":1,"user_group_ratio":0.5}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{ // settle 补扣 50
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1100,
		Quota: 50, RequestId: "req-1",
		Other: `{"billing_stage":"settle","task_id":"vt1","pre_consumed_quota":100,"actual_quota":150,"video_tokens":123}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1, "settle 行已隐藏")
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	assert.Equal(t, "SUCCESS", ti.Status)
	assert.Equal(t, "100%", ti.Progress)
	assert.Equal(t, 100, ti.PreQuota)
	assert.Equal(t, 150, ti.FinalQuota, "预扣100+补扣50")
	assert.Equal(t, 123, ti.OutputTokens)
	assert.Equal(t, "720p", ti.ResolutionTier)
	assert.Equal(t, 5, ti.DurationS)
	assert.True(t, ti.HasInput)
	assert.Equal(t, 0.5, ti.EffectiveRatio)
	assert.True(t, ti.IsUserRatio)
	assert.Equal(t, "vt1", ti.TaskId)
	assert.Equal(t, "up-999", ti.UpstreamTaskId, "admin 路径可见上游 ID")
}

// 失败全额退款：FinalQuota=0；self 路径不给上游 ID；失败原因来自 task。
func TestEnrichSeedance_FailureRefundAndSelfPath(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "vt2", UserId: 1, Status: TaskStatusFailure, Progress: "100%",
		FailReason: "content policy", Quota: 100,
		PrivateData: TaskPrivateData{UpstreamTaskID: "up-2", RequestId: "req-2"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "dreamina-seedance-2-0", CreatedAt: 1000,
		Quota: 100, RequestId: "req-2",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt2","group_ratio":0.8}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeRefund, ModelName: "dreamina-seedance-2-0", CreatedAt: 1100,
		Quota: 100, RequestId: "req-2",
		Other: `{"billing_stage":"refund","task_id":"vt2","reason":"content policy"}`,
	}).Error)

	logs, _, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 100, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	assert.Equal(t, "FAILURE", ti.Status)
	assert.Equal(t, 0, ti.FinalQuota)
	assert.Equal(t, "content policy", ti.FailReason)
	assert.Empty(t, ti.UpstreamTaskId, "self 路径不暴露上游 ID")
	assert.Equal(t, 0.8, ti.EffectiveRatio)
	assert.False(t, ti.IsUserRatio)
}

// 进行中（无兄弟行）与 task 被清理（兄弟推断）两种兜底。
func TestEnrichSeedance_InProgressAndOrphan(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "vt3", UserId: 1, Status: TaskStatusInProgress, Progress: "30%", Quota: 100,
		PrivateData: TaskPrivateData{RequestId: "req-3"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1000,
		Quota: 100, RequestId: "req-3",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt3","group_ratio":1}`,
	}).Error)
	// orphan: 无 task 行，但有 settle 兄弟 → SUCCESS
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 2000,
		Quota: 100, RequestId: "req-4",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"gone","group_ratio":1}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 2100,
		Quota: 20, RequestId: "req-4",
		Other: `{"billing_stage":"settle","task_id":"gone","pre_consumed_quota":100,"actual_quota":120}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 2)
	byReq := map[string]*Log{}
	for _, l := range logs {
		byReq[l.RequestId] = l
	}
	require.NotNil(t, byReq["req-3"].TaskInfo)
	assert.Equal(t, "IN_PROGRESS", byReq["req-3"].TaskInfo.Status)
	assert.Equal(t, "30%", byReq["req-3"].TaskInfo.Progress)
	require.NotNil(t, byReq["req-4"].TaskInfo)
	assert.Equal(t, "SUCCESS", byReq["req-4"].TaskInfo.Status, "task 缺失时由 settle 兄弟推断")
	assert.Equal(t, 120, byReq["req-4"].TaskInfo.FinalQuota)
}

// 请求参数真实值：优先从 Properties.Input（用户原始请求）取
// resolution/ratio/duration；顶层与 metadata 嵌套均支持；sora 的 size/seconds 推导。
func TestEnrichSeedance_RequestParamsFromInput(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "vp1", UserId: 1, Status: TaskStatusSuccess, Progress: "100%", Quota: 100,
		Properties: Properties{
			Input:           `{"model":"doubao-seedance-1-0","prompt":"cat","resolution":"720p","ratio":"16:9","duration":5}`,
			OriginModelName: "doubao-seedance-1-0",
		},
		PrivateData: TaskPrivateData{RequestId: "preq-1"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1000,
		Quota: 100, RequestId: "preq-1",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vp1","group_ratio":1}`,
	}).Error)
	// metadata 嵌套形态
	require.NoError(t, DB.Create(&Task{
		TaskID: "vp2", UserId: 1, Status: TaskStatusSuccess, Progress: "100%", Quota: 100,
		Properties: Properties{
			Input:           `{"model":"doubao-seedance-1-0","prompt":"dog","metadata":{"resolution":"1080p","ratio":"9:16"},"seconds":10}`,
			OriginModelName: "doubao-seedance-1-0",
		},
		PrivateData: TaskPrivateData{RequestId: "preq-2"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 2000,
		Quota: 100, RequestId: "preq-2",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vp2","group_ratio":1}`,
	}).Error)
	// sora 形态：size + seconds
	require.NoError(t, DB.Create(&Task{
		TaskID: "vp3", UserId: 1, Status: TaskStatusSuccess, Progress: "100%", Quota: 100,
		Properties: Properties{
			Input:           `{"model":"doubao-seedance-2-0","prompt":"bird","size":"1280x720","seconds":"8"}`,
			OriginModelName: "doubao-seedance-2-0",
		},
		PrivateData: TaskPrivateData{RequestId: "preq-3"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-2-0", CreatedAt: 3000,
		Quota: 100, RequestId: "preq-3",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vp3","group_ratio":1}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 3)
	byReq := map[string]*Log{}
	for _, l := range logs {
		byReq[l.RequestId] = l
	}

	t1 := byReq["preq-1"].TaskInfo
	require.NotNil(t, t1)
	assert.Equal(t, "720p", t1.Resolution)
	assert.Equal(t, "16:9", t1.Ratio)
	assert.Equal(t, 5, t1.DurationS)

	t2 := byReq["preq-2"].TaskInfo
	require.NotNil(t, t2)
	assert.Equal(t, "1080p", t2.Resolution)
	assert.Equal(t, "9:16", t2.Ratio)
	assert.Equal(t, 10, t2.DurationS)

	t3 := byReq["preq-3"].TaskInfo
	require.NotNil(t, t3)
	assert.Equal(t, "720p", t3.Resolution, "size 1280x720 → 短边 720p")
	assert.Equal(t, "16:9", t3.Ratio, "size 1280x720 → 16:9")
	assert.Equal(t, 8, t3.DurationS)
}

// 成功任务绝不显示失败原因：差额退款（settle 方向、带 actual_quota）的
// Content 是"token重算：..."结算说明，不是失败原因，不得捕获；
// 任务状态为 SUCCESS 时失败原因必须为空。
func TestEnrichSeedance_SuccessNeverHasFailReason(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "ok-1", UserId: 1, Status: TaskStatusSuccess, Progress: "100%", Quota: 1252,
		PrivateData: TaskPrivateData{RequestId: "ok-req-1"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-2-0-mini-260615", CreatedAt: 1000,
		Quota: 2875, RequestId: "ok-req-1",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"ok-1","group_ratio":1}`,
	}).Error)
	// 多退少补的退款行：reason 在 Content，带 actual_quota（结算调整，非失败）
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeRefund, ModelName: "doubao-seedance-2-0-mini-260615", CreatedAt: 1100,
		Quota: 1623, RequestId: "ok-req-1",
		Content: "token重算：tokens=108900, modelRatio=11.50, groupRatio=1.00, otherMultiplier=1.0000",
		Other:   `{"billing_stage":"refund","task_id":"ok-1","pre_consumed_quota":2875,"actual_quota":1252}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	assert.Equal(t, "SUCCESS", ti.Status)
	assert.Empty(t, ti.FailReason, "成功任务不得携带失败原因（token重算是结算说明）")
	assert.Equal(t, 1252, ti.FinalQuota)
}

// orphan（task 已清理）+ 结算调整退款：同样不得把"token重算"当失败原因，
// 状态由 settle 语义推断为成功。
func TestEnrichSeedance_OrphanSettleRefundNotFailure(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1000,
		Quota: 2875, RequestId: "orph-1",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"gone-s","group_ratio":1}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeRefund, ModelName: "doubao-seedance-1-0", CreatedAt: 1100,
		Quota: 1623, RequestId: "orph-1",
		Content: "token重算：tokens=108900",
		Other:   `{"billing_stage":"refund","task_id":"gone-s","pre_consumed_quota":2875,"actual_quota":1252}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	assert.Equal(t, "SUCCESS", ti.Status, "带 actual_quota 的退款是多退少补，任务实为成功")
	assert.Empty(t, ti.FailReason)
	assert.Equal(t, 1252, ti.FinalQuota)
}

// task 已被清理时，全额失败退款（无 actual_quota）的原因从退款兄弟行兜底：
// other.reason 优先，缺失时取 Content。
func TestEnrichSeedance_OrphanFailReasonFromSiblingContent(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1000,
		Quota: 100, RequestId: "oreq-1",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"gone-1","group_ratio":1}`,
	}).Error)
	// 退款兄弟行：other 无 reason，原因在 Content（负差额结算路径的写法）
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeRefund, ModelName: "doubao-seedance-1-0", CreatedAt: 1100,
		Quota: 100, RequestId: "oreq-1", Content: "upstream error: output_video_censored",
		Other: `{"billing_stage":"refund","task_id":"gone-1","pre_consumed_quota":100,"actual_quota":0}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	assert.Equal(t, "FAILURE", ti.Status)
	assert.Equal(t, "upstream error: output_video_censored", ti.FailReason)
}
