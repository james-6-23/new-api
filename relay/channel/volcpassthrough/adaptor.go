package volcpassthrough

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	"github.com/tidwall/sjson"
)

// generationsTaskPath 官方视频生成任务路径。提交与查询共用同一常量,
// 杜绝两处各自拼接造成漂移(同源约束见 doubao 适配器的既有教训:
// 提交走 A、查询走 B 会让任务永远停在 in_progress 且额度一直被预扣)。
const generationsTaskPath = "/api/v3/contents/generations/tasks"

// TaskAdaptor 是视频生成透传的任务适配器。
//
// 它只服务一条被计费的路径:POST <站点>/volc/api/v3/contents/generations/tasks。
// 其余接口不经过任务链路,由 controller 直接字节流转发、不计费。
//
// 与 doubao 适配器的关键差别:请求体**逐字节原样上送**,不做字段转换。
// 客户按官方文档构造 body,站点不得改写,否则「与官方完全一致」的契约就破了。
type TaskAdaptor struct {
	taskcommon.BaseBilling
	cred ChannelCredentials
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.cred = ChannelCredentials{
		BaseURL:       info.ChannelBaseUrl,
		APIKey:        info.ApiKey,
		Proxy:         info.ChannelSetting.Proxy,
		OtherSettings: info.ChannelOtherSettings,
	}
}

// ValidateRequestAndSetAction 校验官方原生请求体并写入 task_request 上下文。
//
// 刻意不复用 relaycommon.ValidateBasicTaskRequest:官方视频请求体**没有顶层 prompt**,
// 文本在 content[] 里(见官方文档「创建视频生成任务」),而共享校验器会对空 prompt
// 直接返回 400。透传渠道必须原样接受官方 body,故在此自行解析。
//
// 但 task_request 仍必须写入:seedance.ResolveVideoBilling 通过
// relaycommon.GetTaskRequest(c) 取请求,取不到就直接返回 ok=false —— 那样整条
// 视频计费(分辨率档位 + 限时折扣)会静默失效,只按 base 价预扣。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	raw, err := readRequestBody(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "read_request_body_failed", http.StatusBadRequest)
	}

	var req relaycommon.TaskSubmitReq
	if err := common.Unmarshal(raw, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model is required"), "invalid_request", http.StatusBadRequest)
	}

	// 把官方顶层扩展字段(resolution / ratio / content 等)补进 metadata,
	// 让 seedance 的档位与视频输入探测走 metadata 快路径,与既有渠道行为一致。
	promoteTopLevelFields(raw, &req)

	info.Action = constant.TaskActionGenerate
	c.Set("task_request", req)
	return nil
}

// EstimateBilling 复用 seedance 计价矩阵。只有 Seedance 2.0 系列返回倍率,
// 其余模型走基础模型价格,与既有 doubao 渠道逐字一致。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if seedance.IsSeedance2(info.OriginModelName) {
		return seedance.EstimateBilling(c, info)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	target, err := ResolveTarget(a.cred, http.MethodPost, generationsTaskPath, "")
	if err != nil {
		return "", err
	}
	return target.URL, nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	if a.cred.APIKey == "" {
		return fmt.Errorf("data plane request requires channel api key")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cred.APIKey)
	return nil
}

// BuildRequestBody 原样返回客户请求体。
//
// 唯一的例外是渠道模型映射:管理员在渠道里配了 model_mapping 时必须把 body 里的
// model 换成上游名,否则映射对透传渠道形同虚设。未配映射时字节完全不变。
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	raw, err := readRequestBody(c)
	if err != nil {
		return nil, err
	}
	if info.IsModelMapped && info.UpstreamModelName != "" {
		mapped, err := sjson.SetBytes(raw, "model", info.UpstreamModelName)
		if err != nil {
			return nil, fmt.Errorf("apply model mapping failed: %w", err)
		}
		raw = mapped
	}
	info.UpstreamRequestBody = raw
	return strings.NewReader(string(raw)), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse 原样回写官方响应体。
//
// 不构造 OpenAIVideo 外壳:客户按官方文档解析 {"id": "cgt-..."},站点若换成
// task_xxxx 就与官方不一致了。因此上游 id 同时作为公开 id 落库,后续查询按同一
// id 回源官方 —— 这也是同源约束的一部分。
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var submitted struct {
		ID string `json:"id"`
	}
	if err := common.Unmarshal(responseBody, &submitted); err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("unmarshal submit response failed: %w, body: %s", err, responseBody),
			"unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if submitted.ID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream returned empty task id"),
			"invalid_response", http.StatusInternalServerError)
	}

	// 公开 ID 对齐上游 ID:客户拿到的 id 必须能直接用于官方格式的查询路径。
	info.PublicTaskID = submitted.ID

	CopyDownstreamResponseHeaders(c.Writer.Header(), resp.Header)
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(responseBody)

	return submitted.ID, responseBody, nil
}

// FetchTask 供轮询链路查询任务状态,与提交同源(同一渠道 BaseURL + 同一密钥)。
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := strings.TrimRight(baseUrl, "/") + generationsTaskPath + "/" + taskID
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

// ParseTaskResult 把官方任务状态映射为站点内部状态。
// 成功时带回 usage.total_tokens,轮询结算据此重算实际额度
// (见 service.settleTaskBillingOnComplete 的 token 重算分支)。
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var upstream struct {
		Status  string `json:"status"`
		Content struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := common.Unmarshal(respBody, &upstream); err != nil {
		return nil, fmt.Errorf("unmarshal task result failed: %w", err)
	}

	result := &relaycommon.TaskInfo{Code: 0}
	switch upstream.Status {
	case "queued", "pending":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "running", "processing":
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	case "succeeded":
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		result.Url = upstream.Content.VideoURL
		result.CompletionTokens = upstream.Usage.CompletionTokens
		result.TotalTokens = upstream.Usage.TotalTokens
	case "failed", "cancelled", "expired":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = upstream.Error.Message
		if result.Reason == "" {
			result.Reason = "task " + upstream.Status
		}
	default:
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	}
	return result, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// readRequestBody 读取已缓存的请求体,不消费原始 body(重试链路要反复读)。
func readRequestBody(c *gin.Context) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

// promoteTopLevelFields 把 TaskSubmitReq 没有声明的官方顶层字段补进 metadata。
//
// 等价于 relaycommon.promoteUnknownFieldsToMetadata,但只吃已读出的字节,
// 不再碰 gin 的 body。metadata 中已有的同名字段优先,顶层只用于补全 ——
// 这是官方文档承诺的契约。
func promoteTopLevelFields(raw []byte, req *relaycommon.TaskSubmitReq) {
	var top map[string]json.RawMessage
	if err := common.Unmarshal(raw, &top); err != nil {
		return
	}
	if req.Metadata == nil {
		req.Metadata = make(map[string]interface{})
	}
	for k, v := range top {
		if k == "metadata" || isDeclaredTaskField(k) {
			continue
		}
		if _, exists := req.Metadata[k]; exists {
			continue
		}
		var val interface{}
		if err := common.Unmarshal(v, &val); err == nil {
			req.Metadata[k] = val
		}
	}
}

// isDeclaredTaskField 列出 TaskSubmitReq 已直接解出的顶层字段,
// 这些字段不重复升级到 metadata(适配器应读结构体字段)。
func isDeclaredTaskField(field string) bool {
	switch field {
	case "prompt", "model", "mode", "image", "images", "size", "duration", "seconds",
		"input_reference", "image_reference", "image_references":
		return true
	}
	return false
}
