package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 回填：旧日志按 task.private_data.request_id 反向注入 task_id/billing_stage；
// stage 推断：type=6→refund；type=2 有 pre_consumed_quota→settle；否则 pre_consume。
func TestSeedanceLogBackfill(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "old-t1", UserId: 1, Status: TaskStatusSuccess, Quota: 150,
		Properties:  Properties{OriginModelName: "doubao-seedance-1-0"},
		PrivateData: TaskPrivateData{RequestId: "old-req-1"},
	}).Error)
	// 旧三行：无 task_id / billing_stage
	require.NoError(t, LOG_DB.Create(&Log{UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0",
		CreatedAt: 1, Quota: 100, RequestId: "old-req-1", Other: `{"is_task":true,"model_ratio":10}`}).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0",
		CreatedAt: 2, Quota: 50, RequestId: "old-req-1", Other: `{"pre_consumed_quota":100,"actual_quota":150}`}).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: 1, Type: LogTypeRefund, ModelName: "doubao-seedance-1-0",
		CreatedAt: 3, Quota: 10, RequestId: "old-req-1", Other: `{"reason":"partial"}`}).Error)
	// request_id 缺失的任务 → 跳过不炸
	require.NoError(t, DB.Create(&Task{
		TaskID: "old-t2", UserId: 1, Status: TaskStatusSuccess,
		Properties: Properties{OriginModelName: "doubao-seedance-1-0"},
	}).Error)
	// 非 seedance 任务 → 不动
	require.NoError(t, DB.Create(&Task{
		TaskID: "old-g1", UserId: 1, Status: TaskStatusSuccess,
		Properties:  Properties{OriginModelName: "grok-video-1"},
		PrivateData: TaskPrivateData{RequestId: "old-req-g"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: 1, Type: LogTypeConsume, ModelName: "grok-video-1",
		CreatedAt: 4, Quota: 10, RequestId: "old-req-g", Other: `{}`}).Error)

	updated, err := runSeedanceLogBackfill()
	require.NoError(t, err)
	assert.Equal(t, 3, updated)

	var rows []*Log
	require.NoError(t, LOG_DB.Where("request_id = ?", "old-req-1").Order("created_at asc").Find(&rows).Error)
	require.Len(t, rows, 3)
	assert.Contains(t, rows[0].Other, `"billing_stage":"pre_consume"`)
	assert.Contains(t, rows[0].Other, `"task_id":"old-t1"`)
	assert.Contains(t, rows[1].Other, `"billing_stage":"settle"`)
	assert.Contains(t, rows[2].Other, `"billing_stage":"refund"`)

	var grok Log
	require.NoError(t, LOG_DB.Where("request_id = ?", "old-req-g").First(&grok).Error)
	assert.False(t, strings.Contains(grok.Other, "billing_stage"), "非 seedance 不回填")

	// 幂等：二跑无变更
	updated2, err := runSeedanceLogBackfill()
	require.NoError(t, err)
	assert.Equal(t, 0, updated2)
}
