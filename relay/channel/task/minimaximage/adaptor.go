package minimaximage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// ============================
// Request / Response structures
// ============================

// imageRequest mirrors the MiniMax generate/edit body sent to upstream.
// Optional scalar fields use pointer + omitempty per Rule 6.
type imageRequest struct {
	Model             string          `json:"model"`
	Prompt            string          `json:"prompt"`
	N                 *uint           `json:"n,omitempty"`
	Size              string          `json:"size,omitempty"`
	Quality           string          `json:"quality,omitempty"`
	OutputFormat      json.RawMessage `json:"output_format,omitempty"`
	OutputCompression json.RawMessage `json:"output_compression,omitempty"`
	Images            json.RawMessage `json:"images,omitempty"`
	Mask              json.RawMessage `json:"mask,omitempty"`
}

type baseResp struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

// asyncSubmitResponse is returned when X-Async: true is set.
// A non-empty Status means the task is still pending.
type asyncSubmitResponse struct {
	TaskID   string   `json:"task_id"`
	Status   string   `json:"status"`
	BaseResp baseResp `json:"base_resp"`
}

// fetchedTaskResponse is returned by the task-query endpoint.
// Completed tasks omit the Status field and carry a Data array.
type fetchedTaskResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Data   []struct {
		B64Json       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	BaseResp baseResp `json:"base_resp"`
}

// ============================
// Adaptor
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
	proxy   string
	// model is cached in BuildRequestBody for use in BuildRequestURL.
	modelName string
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = info.ChannelBaseUrl
	a.proxy = info.ChannelSetting.Proxy
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	m := resolveModel(info.UpstreamModelName)
	if m == "" {
		return "", errors.New("minimax image: model name is required")
	}
	if info.RelayMode == relayconstant.RelayModeImagesEdits {
		return fmt.Sprintf("%s/v1/content/models/%s/edits", a.baseURL, m), nil
	}
	return fmt.Sprintf("%s/v1/content/models/%s/generations", a.baseURL, m), nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("X-Async", "true")
	req.Header.Set("X-Timeout", "600")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	body := imageRequest{
		Model:  taskReq.Model,
		Prompt: taskReq.GetPrompt(),
	}

	// UnmarshalMetadata fills optional fields (N, Size, Quality, etc.) from
	// the metadata map the gateway parsed out of the incoming request.
	if err := taskReq.UnmarshalMetadata(&body); err != nil {
		return nil, fmt.Errorf("minimax image: unmarshal metadata: %w", err)
	}

	// Model mapping: upstream receives the mapped name; billing stays on origin.
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		// Apply internal alias mapping (gpt-image-* -> MiniMax upstream names).
		info.UpstreamModelName = resolveModel(body.Model)
		body.Model = info.UpstreamModelName
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	info.UpstreamRequestBody = data
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, _ *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, string(types.ErrorCodeBadResponse), http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, service.TaskErrorWrapper(
			errors.New(string(responseBody)),
			string(types.ErrorCodeBadResponseStatusCode),
			resp.StatusCode,
		)
	}

	var sResp asyncSubmitResponse
	if err := common.Unmarshal(responseBody, &sResp); err != nil {
		return "", nil, service.TaskErrorWrapper(err, string(types.ErrorCodeBadResponseBody), http.StatusInternalServerError)
	}
	if sResp.BaseResp.StatusCode != 0 {
		return "", nil, service.TaskErrorWrapper(
			errors.New(sResp.BaseResp.StatusMsg),
			string(types.ErrorCodeBadResponse),
			http.StatusBadGateway,
		)
	}

	c.JSON(http.StatusOK, sResp)
	return sResp.TaskID, responseBody, nil
}

// FetchTask polls GET /v1/content/images/tasks/{task_id}.
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, errors.New("minimax image: invalid task_id")
	}
	url := fmt.Sprintf("%s/v1/content/images/tasks/%s", baseUrl, taskID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("minimax image: http client: %w", err)
	}
	return client.Do(req)
}

// ParseTaskResult converts a fetched task body into a normalised TaskInfo.
//
// MiniMax signals completion by omitting the "status" field and including a
// "data" array; a non-empty "status" means still pending.
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var r fetchedTaskResponse
	if err := common.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("minimax image: parse task result: %w", err)
	}

	info := &relaycommon.TaskInfo{}

	if r.BaseResp.StatusCode != 0 {
		info.Status = model.TaskStatusFailure
		info.Reason = r.BaseResp.StatusMsg
		return info, nil
	}

	// Non-empty Status means the task is still queued or processing.
	if r.Status != "" {
		info.Status = model.TaskStatusQueued
		return info, nil
	}

	// No Status field + data present = completed.
	info.Status = model.TaskStatusSuccess
	info.CompletionTokens = r.Usage.OutputTokens
	info.TotalTokens = r.Usage.TotalTokens
	return info, nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }
func (a *TaskAdaptor) GetChannelName() string { return ChannelName }
