package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func runRespondTaskError(t *testing.T, path string, taskErr *dto.TaskError) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest(http.MethodPost, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Request = req
	respondTaskError(c, taskErr)
	return w
}

func TestRespondTaskError_OpenAIVideoEndpointUsesOpenAIFormat(t *testing.T) {
	w := runRespondTaskError(t, "/v1/videos", &dto.TaskError{
		Code:       "content_policy_violation",
		Message:    "The prompt violates content policy",
		StatusCode: http.StatusBadRequest,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`"error"`, `"message":"The prompt violates content policy"`, `"type":"invalid_request_error"`, `"code":"content_policy_violation"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"data"`) {
		t.Fatalf("OpenAI-format body must not contain TaskError data field: %s", body)
	}
}

func TestRespondTaskError_OpenAIVideoFetchPathAlsoOpenAIFormat(t *testing.T) {
	w := runRespondTaskError(t, "/v1/videos/task_abc123", &dto.TaskError{
		Code:       "upstream_error",
		Message:    "boom",
		StatusCode: http.StatusBadGateway,
	})
	body := w.Body.String()
	if !strings.Contains(body, `"type":"upstream_error"`) {
		t.Fatalf("5xx must map to upstream_error type: %s", body)
	}
}

func TestRespondTaskError_LegacyEndpointKeepsTaskErrorShape(t *testing.T) {
	w := runRespondTaskError(t, "/v1/video/generations", &dto.TaskError{
		Code:       "upstream_error",
		Message:    "boom",
		StatusCode: http.StatusBadRequest,
	})
	body := w.Body.String()
	if !strings.Contains(body, `"code":"upstream_error"`) || !strings.Contains(body, `"data"`) {
		t.Fatalf("legacy endpoint should keep TaskError shape: %s", body)
	}
	if strings.Contains(body, `invalid_request_error`) {
		t.Fatalf("legacy endpoint must not use OpenAI type mapping: %s", body)
	}
}

func TestRespondTaskError_429RewritesMessageBothShapes(t *testing.T) {
	w := runRespondTaskError(t, "/v1/videos", &dto.TaskError{
		Code: "rate_limited", Message: "upstream raw", StatusCode: http.StatusTooManyRequests,
	})
	if !strings.Contains(w.Body.String(), "当前分组上游负载已饱和") {
		t.Fatalf("429 message not rewritten: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"rate_limit_error"`) {
		t.Fatalf("429 type wrong: %s", w.Body.String())
	}
}

// 端到端形状回归：上游火山引擎内容审核 400 → 客户在 /v1/videos 收到的最终响应。
func TestRespondTaskError_VolcengineSensitiveContentEndToEnd(t *testing.T) {
	upstreamBody := []byte(`{"error":{"code":"InputTextSensitiveContentDetected","message":"The request failed because the input text 'content[6]' may contain sensitive information. Request id: 021785493621261e63fcd4b145954693"}}`)
	taskErr := service.TaskErrorFromUpstreamBody(http.StatusBadRequest, upstreamBody)
	w := runRespondTaskError(t, "/v1/videos", taskErr)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"code":"InputTextSensitiveContentDetected"`,
		`may contain sensitive information`,
		`Request id: 021785493621261e63fcd4b145954693`,
		`"type":"invalid_request_error"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("client body missing %s:\n%s", want, body)
		}
	}
}

// 上游报错里的 Request ID 替换为系统 request id：客户拿到的排查凭据应是
// 我们系统的 ID，而非上游内部 ID。
func TestReplaceUpstreamRequestId(t *testing.T) {
	msg := "The request failed because the input image may contain sensitive information. Request ID: 20260802133303AC60C953E95B6AEDD755_asset-20260802133311-ck4kk"
	got := replaceUpstreamRequestId(msg, "sys-req-123")
	want := "The request failed because the input image may contain sensitive information. Request ID: sys-req-123"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// 无 Request ID 时原样返回
	if got := replaceUpstreamRequestId("plain error", "sys-req-123"); got != "plain error" {
		t.Fatalf("plain passthrough failed: %q", got)
	}
	// 系统 request id 为空时不替换（保留上游原文，别删信息）
	if got := replaceUpstreamRequestId(msg, ""); got != msg {
		t.Fatalf("empty rid must keep original, got %q", got)
	}
	// 小写/变体形式也替换，且保留原前缀写法
	got2 := replaceUpstreamRequestId("failed. request id: abc_def-123", "sys-req-123")
	if got2 != "failed. request id: sys-req-123" {
		t.Fatalf("lowercase variant failed: %q", got2)
	}
}

// 任务错误日志内容：与聊天错误日志同格式（status_code=N, 完整上游文字），
// Request ID 已换成系统 ID；消息为空才落回 error_code 兜底。
func TestBuildTaskErrorLogContent(t *testing.T) {
	taskErr := &dto.TaskError{
		Code:       "build_request_failed",
		Message:    "preupload image to byteplus asset library failed: byteplus asset asset-1 processing failed: The request failed because the input image may contain sensitive information. Request ID: 2026_upstream-id",
		StatusCode: http.StatusInternalServerError,
	}
	got := buildTaskErrorLogContent(taskErr, "sys-req-9")
	for _, want := range []string{
		"status_code=500",
		"sensitive information",
		"Request ID: sys-req-9",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("content missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "2026_upstream-id") {
		t.Fatalf("upstream request id must be replaced:\n%s", got)
	}
	// 空消息兜底
	empty := buildTaskErrorLogContent(&dto.TaskError{Code: "x_failed", StatusCode: 500}, "rid")
	if empty != "status_code=500, error_code=x_failed" {
		t.Fatalf("empty-message fallback = %q", empty)
	}
}

// 上游本身也是网关时，其 message 可能已带 "status_code=N, " 前缀——不得重复叠加。
func TestBuildTaskErrorLogContent_NoDoublePrefix(t *testing.T) {
	taskErr := &dto.TaskError{
		Code:       "build_request_failed",
		Message:    "status_code=500, error_code=build_request_failed",
		StatusCode: http.StatusInternalServerError,
	}
	got := buildTaskErrorLogContent(taskErr, "rid")
	if got != "status_code=500, error_code=build_request_failed" {
		t.Fatalf("prefix doubled: %q", got)
	}
}

// 端到端：素材预上传被审核拒绝 → TaskErrorWrapper(build_request_failed) →
// 客户端响应与错误日志都必须携带上游完整拒绝原因（不得只剩 error_code）。
func TestBuildRequestFailed_CarriesAssetRejectionDetail(t *testing.T) {
	assetErr := fmt.Errorf("preupload image to byteplus asset library failed: byteplus asset a-1 processing failed: {\"Id\":\"a-1\",\"Status\":\"Failed\",\"StatusMessage\":\"The request failed because the input image may contain sensitive information. Request ID: up-999\"}")
	taskErr := service.TaskErrorWrapper(assetErr, "build_request_failed", http.StatusInternalServerError)

	if !strings.Contains(taskErr.Message, "sensitive information") {
		t.Fatalf("TaskError message lost upstream detail: %q", taskErr.Message)
	}

	// 客户端响应
	w := runRespondTaskError(t, "/v1/videos", taskErr)
	if !strings.Contains(w.Body.String(), "sensitive information") {
		t.Fatalf("client response lost upstream detail: %s", w.Body.String())
	}

	// 错误日志内容
	content := buildTaskErrorLogContent(taskErr, "sys-rid")
	if !strings.Contains(content, "sensitive information") || !strings.Contains(content, "status_code=500") {
		t.Fatalf("error log content wrong: %q", content)
	}
	if strings.Contains(content, "up-999") {
		t.Fatalf("upstream request id must be replaced: %q", content)
	}
}
