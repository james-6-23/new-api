package seedance3rd

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/seedance"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/lo"
)

// ============================
// Request / Response structures (第三方 model.service-inference.ai)
// ============================

type ContentItem struct {
	Type     string    `json:"type,omitempty"`
	Text     string    `json:"text,omitempty"`
	ImageURL *MediaURL `json:"image_url,omitempty"`
	VideoURL *MediaURL `json:"video_url,omitempty"`
	AudioURL *MediaURL `json:"audio_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}

type MediaURL struct {
	URL string `json:"url,omitempty"`
}

// requestPayload 对齐第三方 POST /v1/video/generate 顶层参数。
type requestPayload struct {
	Model           string         `json:"model"`
	Content         []ContentItem  `json:"content,omitempty"`
	Duration        *dto.IntValue  `json:"duration,omitempty"`
	Resolution      string         `json:"resolution,omitempty"`
	Ratio           string         `json:"ratio,omitempty"`
	GenerateAudio   *dto.BoolValue `json:"generate_audio,omitempty"`
	Watermark       *dto.BoolValue `json:"watermark,omitempty"`
	ReturnLastFrame *dto.BoolValue `json:"return_last_frame,omitempty"`
}

// submitResponse: { "task": { "id": ... } }
type submitResponse struct {
	Task struct {
		ID string `json:"id"`
	} `json:"task"`
}

// fetchResponse: { "task": {...} }
type fetchResponse struct {
	Task struct {
		ID           string   `json:"id"`
		Status       string   `json:"status"`
		Model        string   `json:"model"`
		Outputs      []string `json:"outputs"`
		LastFrameURL string   `json:"last_frame_url"`
		Error        *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		DurationSeconds int `json:"duration_seconds"`
	} `json:"task"`
}

// ============================
// Adaptor
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType   int
	apiKey        string
	baseURL       string
	channelId     int
	proxy         string
	otherSettings dto.ChannelOtherSettings
	// endpointOverride 仅测试用,指向 httptest.Server;生产为空。
	endpointOverride string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
	a.channelId = info.ChannelId
	a.proxy = info.ChannelSetting.Proxy
	a.otherSettings = info.ChannelOtherSettings
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/video/generate", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }
func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

// BuildRequestBody 从网关缓存的任务请求构造上游请求体:转换 payload →
// 应用模型映射(上游收到带 -hc/-ep 后缀名;计费仍按 OriginModelName)→
// 素材预上传(开关关闭时零行为)→ marshal 并缓存到 info.UpstreamRequestBody。
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	// 模型映射:上游收到带 -hc/-ep 后缀名;计费仍按 OriginModelName。
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		info.UpstreamModelName = body.Model
	}
	// 素材预上传(按上游名后缀选流程),开关关闭时零行为。
	if err := a.preuploadAssets(c, body); err != nil {
		return nil, err
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	info.UpstreamRequestBody = data
	return bytes.NewReader(data), nil
}

// FetchTask 查询第三方任务状态:GET /v1/video/tasks/{task_id}。
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s/v1/video/tasks/%s", baseUrl, taskID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

// EstimateBilling 复用共享 seedance 计费包(按 OriginModelName 查同一矩阵)。
// 非 Seedance 2.0 模型返回 nil(按基础 modelRatio 计费)。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if seedance.IsSeedance2(info.OriginModelName) {
		return seedance.EstimateBilling(c, info)
	}
	return nil
}

// ConvertToOpenAIVideo 把落库的第三方任务查询响应({"task":{...}})转换为
// OpenAI Video API 风格的对外响应(状态/进度/URL/last_frame_url/duration/usage/error)。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var resp fetchResponse
	if err := common.Unmarshal(originTask.Data, &resp); err != nil {
		return nil, errors.Wrap(err, "unmarshal seedance3rd task data failed")
	}
	tk := resp.Task

	ov := dto.NewOpenAIVideo()
	ov.ID = originTask.TaskID
	ov.TaskID = originTask.TaskID
	ov.Status = originTask.Status.ToVideoStatus()
	ov.SetProgressStr(originTask.Progress)
	if len(tk.Outputs) > 0 && tk.Outputs[0] != "" {
		ov.SetMetadata("url", tk.Outputs[0])
	}
	if tk.LastFrameURL != "" {
		ov.SetMetadata("last_frame_url", tk.LastFrameURL)
	}
	ov.CreatedAt = originTask.CreatedAt
	if ov.IsTerminal() {
		ov.CompletedAt = originTask.UpdatedAt
	}
	ov.Model = originTask.Properties.OriginModelName
	if tk.DurationSeconds > 0 {
		ov.SetMetadata("duration", tk.DurationSeconds)
	}
	if tk.Usage.CompletionTokens > 0 || tk.Usage.TotalTokens > 0 {
		ov.SetMetadata("usage", map[string]int{
			"completion_tokens": tk.Usage.CompletionTokens,
			"total_tokens":      tk.Usage.TotalTokens,
		})
	}
	if (tk.Status == "failed" || tk.Status == "expired") && tk.Error != nil {
		// 上游错误脱敏后透传（URL/IP/域名 mask）。
		ov.Error = &dto.OpenAIVideoError{Message: common.MaskSensitiveInfo(tk.Error.Message), Code: tk.Error.Code}
	}
	return common.Marshal(ov)
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	// 先抽 metadata.content 再反序列化,避免 Go slice 整体替换冲掉 req.Images。
	var metaContent []ContentItem
	metadata := req.Metadata
	if metadata != nil {
		if raw, ok := metadata["content"]; ok {
			if b, err := common.Marshal(raw); err == nil {
				_ = common.Unmarshal(b, &metaContent)
			}
			delete(metadata, "content")
		}
	}

	if req.HasImage() {
		for _, imgURL := range req.Images {
			r.Content = append(r.Content, ContentItem{
				Type:     "image_url",
				ImageURL: &MediaURL{URL: imgURL},
			})
		}
	}

	if err := taskcommon.UnmarshalMetadata(metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	if len(metaContent) > 0 {
		r.Content = append(r.Content, metaContent...)
	}

	// duration 优先级:req.Seconds > metadata.duration(已入 r.Duration) > req.Duration
	if sec, _ := strconv.Atoi(req.Seconds); sec > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(sec))
	} else if r.Duration == nil && req.Duration > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(req.Duration))
	}

	// 第三方只用顶层 prompt 作文本输入:剔除 content 中 text 项,用 req.Prompt 重建末尾 text。
	r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
	r.Content = append(r.Content, ContentItem{Type: "text", Text: req.Prompt})

	return &r, nil
}

// ParseTaskResult 把第三方查询响应 { "task": {...} } 映射为内部 TaskInfo(状态/进度/URL/tokens)。
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var resp fetchResponse
	if err := common.Unmarshal(respBody, &resp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	tk := resp.Task
	info := relaycommon.TaskInfo{Code: 0}
	switch tk.Status {
	case "pending", "queued":
		info.Status = model.TaskStatusQueued
		info.Progress = "10%"
	case "processing", "running":
		info.Status = model.TaskStatusInProgress
		info.Progress = "50%"
	case "completed", "succeeded":
		info.Status = model.TaskStatusSuccess
		info.Progress = "100%"
		if len(tk.Outputs) > 0 {
			info.Url = tk.Outputs[0]
		}
		info.CompletionTokens = tk.Usage.CompletionTokens
		info.TotalTokens = tk.Usage.TotalTokens
	case "failed", "expired":
		info.Status = model.TaskStatusFailure
		info.Progress = "100%"
		if tk.Error != nil {
			info.Reason = tk.Error.Message
		}
	default:
		info.Status = model.TaskStatusInProgress
		info.Progress = "30%"
	}
	return &info, nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var sResp submitResponse
	if err := common.Unmarshal(responseBody, &sResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}
	if sResp.Task.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return sResp.Task.ID, responseBody, nil
}
