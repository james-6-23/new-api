package doubao

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
// Request / Response structures
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

type requestPayload struct {
	Model                 string         `json:"model"`
	Content               []ContentItem  `json:"content,omitempty"`
	CallbackURL           string         `json:"callback_url,omitempty"`
	SafetyIdentifier      string         `json:"safety_identifier,omitempty"`
	ReturnLastFrame       *dto.BoolValue `json:"return_last_frame,omitempty"`
	ServiceTier           string         `json:"service_tier,omitempty"`
	ExecutionExpiresAfter *dto.IntValue  `json:"execution_expires_after,omitempty"`
	GenerateAudio         *dto.BoolValue `json:"generate_audio,omitempty"`
	Draft                 *dto.BoolValue `json:"draft,omitempty"`
	Tools                 []struct {
		Type string `json:"type,omitempty"`
	} `json:"tools,omitempty"`
	Resolution  string         `json:"resolution,omitempty"`
	Ratio       string         `json:"ratio,omitempty"`
	Duration    *dto.IntValue  `json:"duration,omitempty"`
	Frames      *dto.IntValue  `json:"frames,omitempty"`
	Seed        *dto.IntValue  `json:"seed,omitempty"`
	CameraFixed *dto.BoolValue `json:"camera_fixed,omitempty"`
	Watermark   *dto.BoolValue `json:"watermark,omitempty"`
}

type responsePayload struct {
	ID string `json:"id"` // task_id
}

type responseTask struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Seed            int    `json:"seed"`
	Resolution      string `json:"resolution"`
	Duration        int    `json:"duration"`
	Ratio           string `json:"ratio"`
	FramesPerSecond int    `json:"framespersecond"`
	ServiceTier     string `json:"service_tier"`
	GenerateAudio   bool   `json:"generate_audio"`
	Draft           bool   `json:"draft"`
	Tools           []struct {
		Type string `json:"type"`
	} `json:"tools"`
	Usage struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		ToolUsage        struct {
			WebSearch int `json:"web_search"`
		} `json:"tool_usage"`
	} `json:"usage"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

// ============================
// Endpoint routing
// ============================

// seedanceEndpoints 是一组成对的视频任务路径。
// 提交与查询必须同源:提交走 A 网关、查询走 B 网关会让任务永远停在
// in_progress 且用户配额一直被预扣。因此两条路径只在这里成对定义,
// BuildRequestURL 与 FetchTask 都从同一个来源取值,杜绝两处各自拼接导致的漂移。
type seedanceEndpoints struct {
	// generate 提交生成任务的路径。
	generate string
	// queryPrefix 查询任务的路径前缀,末尾直接拼接 task_id。
	// 刻意不用 fmt.Sprintf 的格式串:非常量格式串会触发 go vet 的
	// non-constant format string 检查。
	queryPrefix string
}

var (
	// modelArkEndpoints 火山方舟(ModelArk)官方路径。
	// 存量渠道(other_settings 里没有 asset_provider 这个 key)必须继续走这里,
	// 与本次改动前逐字节一致,实现零迁移。
	modelArkEndpoints = seedanceEndpoints{
		generate:    "/api/v3/contents/generations/tasks",
		queryPrefix: "/api/v3/contents/generations/tasks/",
	}
	// cloudwiseEndpoints 第三方 cloudwise 网关路径。
	// 该网关在不同路径上提供完全相同的请求体/响应体/状态词(succeeded 等),
	// 因此只有路径需要分流,payload 与解析逻辑全部复用。
	cloudwiseEndpoints = seedanceEndpoints{
		generate:    "/api/v1/aiproducts/video/seedance",
		queryPrefix: "/api/v1/aiproducts/video/seedance/tasks/",
	}
)

// resolveEndpoints 按渠道 other_settings.asset_provider 选择路径组。
// 空值与未知值由 ResolveAssetProvider 归一到 byteplus,所以存量渠道行为不变。
//
// 注意:这里读的是 a.otherSettings,由 Init 填充。请求链路经 InitChannelMeta 天然带上
// 该字段;轮询链路是自己手搓 RelayInfo 的,必须由构造方把 ChannelOtherSettings 一起带进来
// (见 service.BuildVideoPollingRelayInfo),否则查询会退回官方路径而 404。
func (a *TaskAdaptor) resolveEndpoints() seedanceEndpoints {
	if a.otherSettings.ResolveAssetProvider() == dto.AssetProviderCloudwise {
		return cloudwiseEndpoints
	}
	return modelArkEndpoints
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType   int
	apiKey        string
	baseURL       string
	channelId     int
	proxy         string
	otherSettings dto.ChannelOtherSettings
	// endpointOverride 仅用于测试，指向 httptest.Server；生产为空时按 region 推导。
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

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// Accept only POST /v1/video/generations as "generate" action.
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + a.resolveEndpoints().generate, nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// EstimateBilling 计算 Seedance 2.0 视频计费的 OtherRatios 并写入展示快照。
// 模型识别、视频/分辨率检测与定价统一走共享包 seedance(与 sora 适配器同一套精确矩阵),
// 覆盖海外 dreamina-* 与国内 doubao-* 两种命名。非 Seedance 2.0 模型返回 nil。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if seedance.IsSeedance2(info.OriginModelName) {
		return seedance.EstimateBilling(c, info)
	}
	return nil
}

// BuildRequestBody converts request into Doubao specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		info.UpstreamModelName = body.Model
	}

	// 海外 BytePlus 素材库预上传：开关开启时，把 content 中的公网媒体 URL 先上传素材库，
	// 替换为 asset://<id> 再提交生成。开关关闭时此调用为零行为。
	if err := a.preuploadAssets(c, body); err != nil {
		return nil, err
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	// 保存上游请求体,供 controller 写入 task.Properties.Input 用作审计与回显。
	if info != nil {
		info.UpstreamRequestBody = data
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// Parse Doubao response
	var dResp responsePayload
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return dResp.ID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := baseUrl + a.resolveEndpoints().queryPrefix + taskID

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

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	// 关键:在 JSON 反序列化前先把 metadata.content 抽出来,避免覆盖 req.Images。
	// Go encoding/json 解码 JSON 数组到已有 slice 字段是整体替换而非追加,
	// 若不先抽出来,req.Images 填充的 image_url 条目会被冲掉。
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

	// Add images if present
	if req.HasImage() {
		for _, imgURL := range req.Images {
			r.Content = append(r.Content, ContentItem{
				Type: "image_url",
				ImageURL: &MediaURL{
					URL: imgURL,
				},
			})
		}
	}

	if err := taskcommon.UnmarshalMetadata(metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	if len(metaContent) > 0 {
		r.Content = append(r.Content, metaContent...)
	}

	// duration 优先级:req.Seconds > metadata.duration (已由 UnmarshalMetadata 写入 r.Duration) > req.Duration
	// req.Duration 回退是必需的:即便 promoteUnknownFieldsToMetadata 让顶层未知字段进 metadata,
	// duration 在 isKnownTaskField 白名单里,会被解到 req.Duration 而不会进 req.Metadata,
	// 所以 Doubao 必须显式读 req.Duration。
	if sec, _ := strconv.Atoi(req.Seconds); sec > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(sec))
	} else if r.Duration == nil && req.Duration > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(req.Duration))
	}

	// Seedance 实际只用顶层 prompt 作为文本输入(文档示例中 content 数组没有 text 项)。
	// 剔除任何 text 项,用 req.Prompt 重建为最后一项 text。
	r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
	r.Content = append(r.Content, ContentItem{
		Type: "text",
		Text: req.Prompt,
	})

	return &r, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// Map Doubao status to internal status
	switch resTask.Status {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "processing", "running":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = resTask.Content.VideoURL
		// 解析 usage 信息用于按倍率计费
		taskResult.CompletionTokens = resTask.Usage.CompletionTokens
		taskResult.TotalTokens = resTask.Usage.TotalTokens
	case "failed":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = resTask.Error.Message
	case "expired":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = "task expired"
	default:
		// Unknown status, treat as processing
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	// url 仅在上游真实返回非空视频地址时写入，避免任务未完成时
	// 在 metadata 中输出 "url":"" 这种容易让客户端误判已完成的脏字段。
	if dResp.Content.VideoURL != "" {
		openAIVideo.SetMetadata("url", dResp.Content.VideoURL)
	}
	openAIVideo.CreatedAt = originTask.CreatedAt
	// CompletedAt 只在终态写入；未完成任务的 UpdatedAt 接近 CreatedAt，
	// 直接赋值会让客户端误以为任务已完成。
	if openAIVideo.IsTerminal() {
		openAIVideo.CompletedAt = originTask.UpdatedAt
	}
	openAIVideo.Model = originTask.Properties.OriginModelName

	if dResp.Seed != 0 {
		openAIVideo.SetMetadata("seed", dResp.Seed)
	}
	if dResp.Duration > 0 {
		openAIVideo.SetMetadata("duration", dResp.Duration)
	}
	if dResp.Ratio != "" {
		openAIVideo.SetMetadata("ratio", dResp.Ratio)
	}
	if dResp.Resolution != "" {
		openAIVideo.SetMetadata("resolution", dResp.Resolution)
	}
	if dResp.FramesPerSecond > 0 {
		openAIVideo.SetMetadata("framespersecond", dResp.FramesPerSecond)
	}
	if dResp.ServiceTier != "" {
		openAIVideo.SetMetadata("service_tier", dResp.ServiceTier)
	}
	openAIVideo.SetMetadata("generate_audio", dResp.GenerateAudio)
	openAIVideo.SetMetadata("draft", dResp.Draft)
	if dResp.Usage.CompletionTokens > 0 || dResp.Usage.TotalTokens > 0 {
		openAIVideo.SetMetadata("usage", map[string]int{
			"completion_tokens": dResp.Usage.CompletionTokens,
			"total_tokens":      dResp.Usage.TotalTokens,
		})
	}

	if dResp.Status == "failed" || dResp.Status == "expired" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: dResp.Error.Message,
			Code:    dResp.Error.Code,
		}
	}

	// 从 Properties.Input 反推用户原始请求,在异步 GET 时回显 reference 计数与
	// "requested_*" 字段。上游响应不带 content 输入,这里补齐审计信息。
	if originTask.Properties.Input != "" {
		var origReq requestPayload
		if err := common.Unmarshal([]byte(originTask.Properties.Input), &origReq); err == nil {
			var refImg, refAudio, refVideo, firstFrame, lastFrame int
			for _, c := range origReq.Content {
				switch c.Type {
				case "image_url":
					switch c.Role {
					case "reference_image":
						refImg++
					case "first_frame":
						firstFrame++
					case "last_frame":
						lastFrame++
					}
				case "video_url":
					if c.Role == "reference_video" {
						refVideo++
					}
				case "audio_url":
					if c.Role == "reference_audio" {
						refAudio++
					}
				}
			}
			if refImg > 0 {
				openAIVideo.SetMetadata("reference_image_count", refImg)
			}
			if refVideo > 0 {
				openAIVideo.SetMetadata("reference_video_count", refVideo)
			}
			if refAudio > 0 {
				openAIVideo.SetMetadata("reference_audio_count", refAudio)
			}
			if firstFrame > 0 {
				openAIVideo.SetMetadata("first_frame_count", firstFrame)
			}
			if lastFrame > 0 {
				openAIVideo.SetMetadata("last_frame_count", lastFrame)
			}
			// 上游回带的字段保持原样;若上游没回带,暴露用户提交值便于排查
			if dResp.Ratio == "" && origReq.Ratio != "" {
				openAIVideo.SetMetadata("requested_ratio", origReq.Ratio)
			}
			if dResp.Duration == 0 && origReq.Duration != nil {
				openAIVideo.SetMetadata("requested_duration", int(*origReq.Duration))
			}
			if dResp.Resolution == "" && origReq.Resolution != "" {
				openAIVideo.SetMetadata("requested_resolution", origReq.Resolution)
			}
		}
	}

	return common.Marshal(openAIVideo)
}
