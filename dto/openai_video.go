package dto

import (
	"strconv"
	"strings"
)

const (
	// VideoStatusUnknown 仅供内部日志/兜底判断使用，禁止作为对外响应 status 值返回客户端。
	// 对外响应 status 必须是 queued / in_progress / completed / failed 四个标准枚举之一，
	// 详见 model.TaskStatus.ToVideoStatus()。
	VideoStatusUnknown    = "unknown"
	VideoStatusQueued     = "queued"
	VideoStatusInProgress = "in_progress"
	VideoStatusCompleted  = "completed"
	VideoStatusFailed     = "failed"
)

type OpenAIVideo struct {
	ID                 string            `json:"id"`
	TaskID             string            `json:"task_id,omitempty"` //兼容旧接口 待废弃
	Object             string            `json:"object"`
	Model              string            `json:"model"`
	Status             string            `json:"status"` // Should use VideoStatus constants: VideoStatusQueued, VideoStatusInProgress, VideoStatusCompleted, VideoStatusFailed
	Progress           int               `json:"progress"`
	CreatedAt          int64             `json:"created_at"`
	CompletedAt        int64             `json:"completed_at,omitempty"`
	ExpiresAt          int64             `json:"expires_at,omitempty"`
	Seconds            string            `json:"seconds,omitempty"`
	Size               string            `json:"size,omitempty"`
	Quality            string            `json:"quality,omitempty"`  // 分辨率质量 480p/720p
	Prompt             string            `json:"prompt,omitempty"`   // 原始提示词
	RemixedFromVideoID string            `json:"remixed_from_video_id,omitempty"`
	Error              *OpenAIVideoError `json:"error,omitempty"`
	Metadata           map[string]any    `json:"metadata,omitempty"`
}

// IsTerminal 报告响应状态是否处于终态（completed 或 failed）。
// 用于在适配器/序列化层判断是否应当写入 CompletedAt、视频 URL 等仅终态有效的字段，
// 避免任务刚创建（NOT_START / QUEUED）时把零值时间戳错误地暴露为 completed_at。
func (m *OpenAIVideo) IsTerminal() bool {
	return m.Status == VideoStatusCompleted || m.Status == VideoStatusFailed
}

func (m *OpenAIVideo) SetProgressStr(progress string) {
	progress = strings.TrimSuffix(progress, "%")
	m.Progress, _ = strconv.Atoi(progress)
}
func (m *OpenAIVideo) SetMetadata(k string, v any) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]any)
	}
	m.Metadata[k] = v
}
func NewOpenAIVideo() *OpenAIVideo {
	return &OpenAIVideo{
		Object: "video",
		Status: VideoStatusQueued,
	}
}

type OpenAIVideoError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}
