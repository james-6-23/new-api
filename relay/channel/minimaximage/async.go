package minimaximage

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	// asyncPollInterval is how often the task endpoint is polled.
	asyncPollInterval = 5 * time.Second
	// asyncPollTimeout bounds the total wait before we give up on a task.
	asyncPollTimeout = 10 * time.Minute
)

// asyncSubmitResp is the reply to a submission carrying X-Async: true.
type asyncSubmitResp struct {
	TaskID   string          `json:"task_id"`
	Status   string          `json:"status"`
	TraceID  string          `json:"trace_id"`
	BaseResp minimaxBaseResp `json:"base_resp"`
}

// asyncTaskResp is the reply from the task-query endpoint. MiniMax signals
// completion by dropping "status" and returning a populated "data" array.
type asyncTaskResp struct {
	TaskID   string             `json:"task_id"`
	Status   string             `json:"status"`
	TraceID  string             `json:"trace_id"`
	Created  int64              `json:"created"`
	Data     []minimaxImageData `json:"data"`
	Usage    minimaxUsage       `json:"usage"`
	BaseResp minimaxBaseResp    `json:"base_resp"`
}

// submitAsyncTask reads the async submission reply and returns the task id.
func submitAsyncTask(c *gin.Context, body []byte) (string, string, error) {
	var sResp asyncSubmitResp
	if err := common.Unmarshal(body, &sResp); err != nil {
		return "", "", fmt.Errorf("parse async submit response: %w", err)
	}
	if sResp.BaseResp.StatusCode != 0 {
		return "", sResp.TraceID, &taskFailedError{
			Code: sResp.BaseResp.StatusCode,
			Msg:  sResp.BaseResp.StatusMsg,
		}
	}
	if sResp.TaskID == "" {
		return "", sResp.TraceID, errors.New("upstream returned no task_id")
	}
	logger.LogInfo(c, fmt.Sprintf("minimax image: async task submitted, task_id=%s trace_id=%s",
		sResp.TaskID, sResp.TraceID))
	return sResp.TaskID, sResp.TraceID, nil
}

// pollAsyncTask polls until the task completes, fails, or the deadline passes.
//
// A timeout here is billing-critical: MiniMax has already started (and will
// charge for) generation, but the client gets nothing back. Those cases are
// logged at error level with the task_id and trace_id so the upstream charge
// can be reconciled against the failed downstream request.
func pollAsyncTask(c *gin.Context, info *relaycommon.RelayInfo, taskID, traceID string) (*asyncTaskResp, error) {
	client, err := service.GetHttpClientWithProxy(info.ChannelSetting.Proxy)
	if err != nil {
		return nil, fmt.Errorf("http client: %w", err)
	}

	url := fmt.Sprintf("%s/v1/content/images/tasks/%s", info.ChannelBaseUrl, taskID)
	deadline := time.Now().Add(asyncPollTimeout)
	started := time.Now()

	for attempt := 1; ; attempt++ {
		if time.Now().After(deadline) {
			// Upstream is still generating and will bill for it; the client is
			// about to receive an error. Record loudly for reconciliation.
			logger.LogError(c, fmt.Sprintf(
				"minimax image: BILLING RISK - async task timed out after %s, upstream may still be billed. "+
					"task_id=%s trace_id=%s model=%s channel_id=%d user_id=%d",
				time.Since(started).Round(time.Second), taskID, traceID,
				info.OriginModelName, info.ChannelId, info.UserId))
			return nil, fmt.Errorf("image task %s did not complete within %s", taskID, asyncPollTimeout)
		}

		select {
		case <-c.Request.Context().Done():
			// Client hung up mid-generation: same billing exposure as a timeout.
			logger.LogError(c, fmt.Sprintf(
				"minimax image: BILLING RISK - client disconnected while task in flight, upstream may still be billed. "+
					"task_id=%s trace_id=%s model=%s channel_id=%d user_id=%d",
				taskID, traceID, info.OriginModelName, info.ChannelId, info.UserId))
			return nil, c.Request.Context().Err()
		case <-time.After(asyncPollInterval):
		}

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+info.ApiKey)

		resp, err := client.Do(req)
		if err != nil {
			// Transient transport failures must not abort a task that is still
			// running upstream; keep polling until the deadline.
			logger.LogWarn(c, fmt.Sprintf("minimax image: poll #%d failed for task %s: %v", attempt, taskID, err))
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			logger.LogWarn(c, fmt.Sprintf("minimax image: read poll #%d for task %s: %v", attempt, taskID, readErr))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			logger.LogWarn(c, fmt.Sprintf("minimax image: poll #%d for task %s returned HTTP %d: %s",
				attempt, taskID, resp.StatusCode, truncate(respBody, 300)))
			continue
		}

		var tResp asyncTaskResp
		if err := common.Unmarshal(respBody, &tResp); err != nil {
			logger.LogWarn(c, fmt.Sprintf("minimax image: parse poll #%d for task %s: %v", attempt, taskID, err))
			continue
		}

		if tResp.BaseResp.StatusCode != 0 {
			logger.LogError(c, fmt.Sprintf(
				"minimax image: task failed upstream. task_id=%s trace_id=%s code=%d msg=%s model=%s channel_id=%d",
				taskID, traceID, tResp.BaseResp.StatusCode, tResp.BaseResp.StatusMsg,
				info.OriginModelName, info.ChannelId))
			return nil, &taskFailedError{
				Code: tResp.BaseResp.StatusCode,
				Msg:  tResp.BaseResp.StatusMsg,
			}
		}

		// A non-empty status means queued/in_progress; completion drops it.
		if tResp.Status != "" {
			continue
		}
		if len(tResp.Data) == 0 {
			logger.LogError(c, fmt.Sprintf(
				"minimax image: task reported complete but returned no images. task_id=%s trace_id=%s",
				taskID, traceID))
			return nil, errors.New("image task completed without returning any image")
		}

		logger.LogInfo(c, fmt.Sprintf("minimax image: task %s completed in %s",
			taskID, time.Since(started).Round(time.Second)))
		return &tResp, nil
	}
}

// taskFailedError marks an upstream rejection (content safety, bad params, ...)
// as opposed to a timeout, so the client sees the real status code.
type taskFailedError struct {
	Code int
	Msg  string
}

func (e *taskFailedError) Error() string {
	return fmt.Sprintf("image task failed: [%d] %s", e.Code, e.Msg)
}

// httpStatus maps MiniMax's base_resp.status_code onto an HTTP status.
// MiniMax reuses HTTP-like codes, so pass those through and fall back to 502.
func (e *taskFailedError) httpStatus() int {
	if e.Code >= 400 && e.Code <= 599 {
		return e.Code
	}
	return http.StatusBadGateway
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// doAsyncImageResponse turns an async submission into a normal OpenAI image
// response by polling the task to completion before replying to the client.
func doAsyncImageResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, types.NewOpenAIError(errors.New(string(body)),
			types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	}

	taskID, traceID, err := submitAsyncTask(c, body)
	if err != nil {
		status := http.StatusBadGateway
		var failed *taskFailedError
		if errors.As(err, &failed) {
			status = failed.httpStatus()
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, status)
	}

	task, err := pollAsyncTask(c, info, taskID, traceID)
	if err != nil {
		// Never retry: the upstream task is already submitted and billable, so a
		// retry would generate (and pay for) the same image a second time.
		status := http.StatusGatewayTimeout
		var failed *taskFailedError
		if errors.As(err, &failed) {
			status = failed.httpStatus()
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse,
			status, types.ErrOptionWithSkipRetry())
	}

	openAIResp := dto.ImageResponse{Created: task.Created}
	for _, d := range task.Data {
		openAIResp.Data = append(openAIResp.Data, dto.ImageData{
			B64Json:       d.B64Json,
			RevisedPrompt: d.RevisedPrompt,
		})
	}
	c.JSON(http.StatusOK, openAIResp)

	return &dto.Usage{
		PromptTokens:     task.Usage.InputTokens,
		CompletionTokens: task.Usage.OutputTokens,
		TotalTokens:      task.Usage.TotalTokens,
	}, nil
}
