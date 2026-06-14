package model

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToVideoStatusNeverLeaksUnknown 验证 ToVideoStatus 永远不会向客户端返回
// "unknown" 状态——这是字节火山方舟/豆包视频任务在 NOT_START 时间窗（任务创建
// 到第一次轮询的 15 秒）内对外暴露非标准 status 的核心修复点。
func TestToVideoStatusNeverLeaksUnknown(t *testing.T) {
	cases := []struct {
		name string
		in   TaskStatus
		want string
	}{
		{"NotStart 视为排队中", TaskStatusNotStart, dto.VideoStatusQueued},
		{"Submitted 视为排队中", TaskStatus(TaskStatusSubmitted), dto.VideoStatusQueued},
		{"Queued 视为排队中", TaskStatus(TaskStatusQueued), dto.VideoStatusQueued},
		{"InProgress 视为进行中", TaskStatus(TaskStatusInProgress), dto.VideoStatusInProgress},
		{"Unknown 视为进行中（继续轮询）", TaskStatus(TaskStatusUnknown), dto.VideoStatusInProgress},
		{"Success 视为已完成", TaskStatus(TaskStatusSuccess), dto.VideoStatusCompleted},
		{"Failure 视为失败", TaskStatus(TaskStatusFailure), dto.VideoStatusFailed},
		{"未来未识别枚举走防御性兜底", TaskStatus("FUTURE_UNRECOGNIZED_VALUE"), dto.VideoStatusQueued},
		{"空字符串走防御性兜底", TaskStatus(""), dto.VideoStatusQueued},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.ToVideoStatus()
			assert.Equalf(t, tc.want, got, "ToVideoStatus(%q) 应为 %q，实际 %q", tc.in, tc.want, got)
			assert.NotEqualf(t, dto.VideoStatusUnknown, got,
				"ToVideoStatus(%q) 不允许向客户端泄露 %q", tc.in, dto.VideoStatusUnknown)
		})
	}
}

// TestTaskToOpenAIVideoCompletedAtOnlyTerminal 验证只有任务真正终态时才写入 CompletedAt，
// 否则 OpenAIVideo.CompletedAt 应为 0（由 omitempty 在 JSON 中省略），
// 避免暴露 created_at == completed_at 这种误导客户端"任务已完成"的脏数据。
func TestTaskToOpenAIVideoCompletedAtOnlyTerminal(t *testing.T) {
	now := int64(1780972915)
	build := func(status TaskStatus) *Task {
		return &Task{
			TaskID:    "task_test",
			Status:    status,
			Progress:  "0%",
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	t.Run("NotStart 不写 CompletedAt", func(t *testing.T) {
		v := build(TaskStatusNotStart).ToOpenAIVideo()
		require.Equal(t, dto.VideoStatusQueued, v.Status)
		require.Zerof(t, v.CompletedAt, "未完成任务 CompletedAt 应为 0，实际 %d", v.CompletedAt)
	})

	t.Run("InProgress 不写 CompletedAt", func(t *testing.T) {
		v := build(TaskStatus(TaskStatusInProgress)).ToOpenAIVideo()
		require.Equal(t, dto.VideoStatusInProgress, v.Status)
		require.Zero(t, v.CompletedAt)
	})

	t.Run("Success 写入 CompletedAt", func(t *testing.T) {
		v := build(TaskStatus(TaskStatusSuccess)).ToOpenAIVideo()
		require.Equal(t, dto.VideoStatusCompleted, v.Status)
		require.Equal(t, now, v.CompletedAt)
	})

	t.Run("Failure 写入 CompletedAt", func(t *testing.T) {
		v := build(TaskStatus(TaskStatusFailure)).ToOpenAIVideo()
		require.Equal(t, dto.VideoStatusFailed, v.Status)
		require.Equal(t, now, v.CompletedAt)
	})
}
