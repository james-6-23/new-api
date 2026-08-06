package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", common.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}

func TestTaskErrorFromUpstreamBody_OpenAIErrorObject(t *testing.T) {
	body := []byte(`{"error":{"message":"The prompt violates content policy","type":"invalid_request_error","code":"content_policy_violation"}}`)
	taskErr := TaskErrorFromUpstreamBody(http.StatusBadRequest, body)

	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.Equal(t, "content_policy_violation", taskErr.Code)
	require.Equal(t, "The prompt violates content policy", taskErr.Message)
	require.False(t, taskErr.LocalError)
}

func TestTaskErrorFromUpstreamBody_MessageOnlyBody(t *testing.T) {
	body := []byte(`{"message":"duration must be between 1 and 12 seconds"}`)
	taskErr := TaskErrorFromUpstreamBody(http.StatusUnprocessableEntity, body)

	require.Equal(t, http.StatusUnprocessableEntity, taskErr.StatusCode)
	require.Equal(t, "upstream_error", taskErr.Code)
	require.Equal(t, "duration must be between 1 and 12 seconds", taskErr.Message)
}

func TestTaskErrorFromUpstreamBody_MasksURLs(t *testing.T) {
	body := []byte(`{"error":{"message":"failed to download image from https://internal.vendor-cdn.com/assets/img.png"}}`)
	taskErr := TaskErrorFromUpstreamBody(http.StatusBadRequest, body)

	require.NotContains(t, taskErr.Message, "internal.vendor-cdn.com")
	require.Contains(t, taskErr.Message, "failed to download image")
	require.Contains(t, taskErr.Message, "***")
}

func TestTaskErrorFromUpstreamBody_AuthNotPassedThrough(t *testing.T) {
	body := []byte(`{"error":{"message":"Incorrect API key provided: sk-abc123"}}`)
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		taskErr := TaskErrorFromUpstreamBody(status, body)
		require.Equal(t, http.StatusServiceUnavailable, taskErr.StatusCode)
		require.Equal(t, "upstream_auth_failed", taskErr.Code)
		require.NotContains(t, taskErr.Message, "sk-abc123")
		require.Contains(t, taskErr.Message, "上游渠道认证失败")
	}
}

func TestTaskErrorFromUpstreamBody_NonJSONTruncated(t *testing.T) {
	body := []byte("<html>" + strings.Repeat("x", 1024) + "</html>")
	taskErr := TaskErrorFromUpstreamBody(http.StatusBadGateway, body)

	require.Equal(t, http.StatusBadGateway, taskErr.StatusCode)
	require.LessOrEqual(t, len(taskErr.Message), taskUpstreamBodyPreviewLimit+8)
	require.Contains(t, taskErr.Message, "...")
}

func TestTaskErrorFromUpstreamBody_NumericCodeKeepsDefault(t *testing.T) {
	// OpenAI error with numeric code — stringified into the code field
	body := []byte(`{"error":{"message":"quota exceeded","code":429}}`)
	taskErr := TaskErrorFromUpstreamBody(http.StatusTooManyRequests, body)

	require.Equal(t, http.StatusTooManyRequests, taskErr.StatusCode)
	require.Equal(t, "429", taskErr.Code)
	require.Equal(t, "quota exceeded", taskErr.Message)
}

// 真实上游样例回归：火山引擎 seedance 内容审核 400。
// message/code 必须原样透传给客户（无 URL/IP，脱敏不改动内容），400 状态码保留（不重试）。
func TestTaskErrorFromUpstreamBody_VolcengineSensitiveContent(t *testing.T) {
	body := []byte(`{"error":{"code":"InputTextSensitiveContentDetected","message":"The request failed because the input text 'content[6]' may contain sensitive information. Request id: 021785493621261e63fcd4b145954693"}}`)
	taskErr := TaskErrorFromUpstreamBody(http.StatusBadRequest, body)

	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.Equal(t, "InputTextSensitiveContentDetected", taskErr.Code)
	require.Equal(t,
		"The request failed because the input text 'content[6]' may contain sensitive information. Request id: 021785493621261e63fcd4b145954693",
		taskErr.Message)
	require.False(t, taskErr.LocalError)
}
