package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOpenAIVideoIsTerminal 验证 IsTerminal 仅在 completed / failed 时返回 true。
// 该 helper 被各 task adaptor 的 ConvertToOpenAIVideo 用来决定是否写入 CompletedAt。
func TestOpenAIVideoIsTerminal(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{VideoStatusQueued, false},
		{VideoStatusInProgress, false},
		{VideoStatusCompleted, true},
		{VideoStatusFailed, true},
		{VideoStatusUnknown, false}, // 兜底：未识别状态不应被当作终态
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			v := &OpenAIVideo{Status: tc.status}
			assert.Equalf(t, tc.want, v.IsTerminal(),
				"OpenAIVideo{Status: %q}.IsTerminal() = %v, want %v", tc.status, v.IsTerminal(), tc.want)
		})
	}
}
