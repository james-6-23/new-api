# 第三方 Seedance 视频渠道接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增独立渠道类型 `ChannelTypeSeedance3rd = 59`(UI 名「seedance(第三方)」)+ 新适配器包 `relay/channel/task/seedance3rd/`,接入第三方上游 `https://model.service-inference.ai`,与现有 doubao(45/54)、sora(55/1)两条 Seedance 链路在 wire 协议层完全隔离,计费复用共享 `seedance` 包。

**Architecture:** 新适配器实现全套 `TaskAdaptor` + `OpenAIVideoConverter`,speak 第三方协议(`POST /v1/video/generate`、`GET /v1/video/tasks/{id}`、嵌套 `{task}` 结构、`outputs[]`)。客户端用原模型名,上游 `-hc`/`-ep` 后缀由内置 `model_mapping` 产生(零后缀代码)。计费按原名走共享 `seedance.EstimateBilling`。素材预上传按上游名后缀选流程(`-hc` → `/v1/sd/assets`;其余 → 组模式,组 id 缓存+失效重建)。轮询零改动(通用 `default` 分支)。

**Tech Stack:** Go 1.22+、Gin、GORM;`relay/channel/task/*` 适配器体系;`pkg/cachex.HybridCache`;前端 web/default(React19/Rsbuild)+ web/classic(React18/Semi)。

## Global Constraints

- **CLAUDE.md Rule 1**:JSON 一律用 `common.Marshal` / `common.Unmarshal` / `common.UnmarshalJsonStr`,禁止直接 `encoding/json`(类型引用除外)。
- **CLAUDE.md Rule 2**:DB 兼容 SQLite/MySQL/PostgreSQL(本计划不新增表/迁移,不涉及)。
- **CLAUDE.md Rule 5**:`new-api` / `QuantumNous` 品牌标识受保护,不得改动/删除。
- **CLAUDE.md Rule 6**:上游请求 DTO 的可选标量用指针 + `omitempty`(`*dto.IntValue` / `*dto.BoolValue`),保留显式零值。
- **计费公式(不变)**:`quota = completion_tokens × modelRatio × groupRatio × video_pricing`,`video_pricing` 由共享 `seedance` 包按**原模型名**得出。
- **隔离原则**:seedance3rd 包与 doubao/sora 包**零共享 wire 代码**;需要的辅助(pickMedia、缓存等)在本包内独立实现。
- **上游基址**:`https://model.service-inference.ai`;鉴权 `Authorization: Bearer <channel key>`(素材库复用同一 key,无 AK/SK 签名)。
- 参考实现(只读参照,勿改):`relay/channel/task/doubao/adaptor.go`、`relay/channel/task/doubao/byteplus_asset.go`、`relay/channel/task/vidu/adaptor.go`、`relay/channel/task/taskcommon/helpers.go`。

---

## File Structure

- **Create** `relay/channel/task/seedance3rd/constants.go` — `ModelList`(三个原名)、`ChannelName`。
- **Create** `relay/channel/task/seedance3rd/adaptor.go` — DTO 结构 + `TaskAdaptor`(全套接口方法)+ `convertToRequestPayload`。
- **Create** `relay/channel/task/seedance3rd/asset.go` — 素材预上传:`assetClient` 接口、`sdAssetClient`、`groupAssetClient`、`preuploadAssets`、`pickMedia`、缓存。
- **Create** `relay/channel/task/seedance3rd/adaptor_test.go` — 适配器单测。
- **Create** `relay/channel/task/seedance3rd/asset_test.go` — 素材单测(httptest)。
- **Modify** `dto/channel_settings.go` — `ChannelOtherSettings` 加 `Seedance3rdAssetEnabled`。
- **Modify** `constant/channel.go` — 常量 59 + `ChannelBaseURLs[59]` + `ChannelTypeNames[59]`。
- **Modify** `relay/relay_adaptor.go` — import + `GetTaskAdaptor` case。
- **Modify** `common/endpoint_type.go` — `GetEndpointTypesByChannelType` case。
- **Modify** `controller/channel-test.go` — `unsupportedTestChannelTypes` 加 59。
- **Modify** 前端 web/default(`constants.ts`、`channel-utils.ts`、`channel-form.ts`、`channel-mutate-drawer.tsx`、i18n)。
- **Modify** 前端 web/classic(`channel.constants.js`、`EditChannelModal.jsx`、i18n)。

---

### Task 1: 适配器骨架 — 包、常量、DTO、基础方法

**Files:**
- Create: `relay/channel/task/seedance3rd/constants.go`
- Create: `relay/channel/task/seedance3rd/adaptor.go`
- Test: `relay/channel/task/seedance3rd/adaptor_test.go`

**Interfaces:**
- Produces: `TaskAdaptor` struct(嵌 `taskcommon.BaseBilling`,字段 `ChannelType int; apiKey, baseURL string; channelId int; proxy string; otherSettings dto.ChannelOtherSettings; endpointOverride string`);`requestPayload`、`ContentItem`、`MediaURL`、`submitResponse`、`fetchResponse` DTO;`ModelList []string`、`ChannelName string`;方法 `Init`、`ValidateRequestAndSetAction`、`BuildRequestURL`、`BuildRequestHeader`、`GetModelList`、`GetChannelName`。

- [ ] **Step 1: 写常量文件**

`relay/channel/task/seedance3rd/constants.go`:
```go
package seedance3rd

// 客户端可见模型 = 原名(无后缀)。-hc/-ep 是上游 wire 名,不放这里,
// 由管理员用渠道 model_mapping 映射产生。
var ModelList = []string{
	"dreamina-seedance-2-0-260128",
	"dreamina-seedance-2-0-fast-260128",
	"dreamina-seedance-2-0-mini-260615",
}

var ChannelName = "seedance-3rd"
```

- [ ] **Step 2: 写失败测试(URL/Header/基础方法)**

`relay/channel/task/seedance3rd/adaptor_test.go`:
```go
package seedance3rd

import (
	"net/http"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func TestBuildRequestURL(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(&relaycommon.RelayInfo{ChannelBaseUrl: "https://model.service-inference.ai", ApiKey: "sk-x"})
	got, err := a.BuildRequestURL(nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := "https://model.service-inference.ai/v1/video/generate"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestBuildRequestHeader(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(&relaycommon.RelayInfo{ChannelBaseUrl: "https://x", ApiKey: "sk-abc"})
	req, _ := http.NewRequest(http.MethodPost, "https://x", nil)
	if err := a.BuildRequestHeader(nil, req, nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-abc" {
		t.Fatalf("auth = %q, want %q", got, "Bearer sk-abc")
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestBuildRequest' -v`
Expected: 编译失败(`undefined: TaskAdaptor`)。

- [ ] **Step 4: 写 adaptor.go 骨架**

`relay/channel/task/seedance3rd/adaptor.go`:
```go
package seedance3rd

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
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

func (a *TaskAdaptor) GetModelList() []string  { return ModelList }
func (a *TaskAdaptor) GetChannelName() string  { return ChannelName }
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestBuildRequest' -v`
Expected: PASS(两个测试)。

- [ ] **Step 6: 提交**

```bash
git add relay/channel/task/seedance3rd/constants.go relay/channel/task/seedance3rd/adaptor.go relay/channel/task/seedance3rd/adaptor_test.go
git commit -m "feat(seedance3rd): adaptor scaffold, constants, DTOs, base methods"
```

---

### Task 2: `convertToRequestPayload`

**Files:**
- Modify: `relay/channel/task/seedance3rd/adaptor.go`
- Test: `relay/channel/task/seedance3rd/adaptor_test.go`

**Interfaces:**
- Consumes: `requestPayload`、`ContentItem`、`MediaURL`(Task 1)。
- Produces: `func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error)` — 把网关统一 `TaskSubmitReq` 转成第三方请求体;剔除 content 中 text 项后用 `req.Prompt` 重建末尾 text;duration 优先级 `req.Seconds > metadata.duration > req.Duration`;`req.Images` 转 `image_url` 条目;先抽 `metadata.content` 再反序列化避免覆盖。

- [ ] **Step 1: 写失败测试**

追加到 `adaptor_test.go`:
```go
import (
	"github.com/QuantumNous/new-api/dto"
	// 复用已有 import
)

func TestConvertToRequestPayload_TextOnly(t *testing.T) {
	a := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{Model: "dreamina-seedance-2-0-260128", Prompt: "a girl dancing"}
	got, err := a.convertToRequestPayload(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Model != "dreamina-seedance-2-0-260128" {
		t.Fatalf("model = %q", got.Model)
	}
	if len(got.Content) != 1 || got.Content[0].Type != "text" || got.Content[0].Text != "a girl dancing" {
		t.Fatalf("content = %+v, want single text item", got.Content)
	}
}

func TestConvertToRequestPayload_ImagesAndDuration(t *testing.T) {
	a := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{
		Model:    "dreamina-seedance-2-0-260128",
		Prompt:   "blink",
		Images:   []string{"https://x/a.png"},
		Duration: 5,
	}
	got, err := a.convertToRequestPayload(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// 期望:1 个 image_url + 1 个末尾 text
	if len(got.Content) != 2 {
		t.Fatalf("content len = %d, want 2 (%+v)", len(got.Content), got.Content)
	}
	if got.Content[0].Type != "image_url" || got.Content[0].ImageURL == nil || got.Content[0].ImageURL.URL != "https://x/a.png" {
		t.Fatalf("first item not image_url: %+v", got.Content[0])
	}
	if got.Content[1].Type != "text" {
		t.Fatalf("last item not text: %+v", got.Content[1])
	}
	if got.Duration == nil || int(*got.Duration) != 5 {
		t.Fatalf("duration = %v, want 5", got.Duration)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestConvertToRequestPayload' -v`
Expected: 编译失败(`a.convertToRequestPayload undefined`)。

- [ ] **Step 3: 实现 `convertToRequestPayload`**

在 `adaptor.go` 追加(并补 import `strconv`、`github.com/QuantumNous/new-api/common`、`github.com/pkg/errors`、`github.com/samber/lo`):
```go
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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestConvertToRequestPayload' -v`
Expected: PASS(两个测试)。

- [ ] **Step 5: 提交**

```bash
git add relay/channel/task/seedance3rd/adaptor.go relay/channel/task/seedance3rd/adaptor_test.go
git commit -m "feat(seedance3rd): convertToRequestPayload (content/duration/images)"
```

---

### Task 3: `ParseTaskResult`(状态映射)

**Files:**
- Modify: `relay/channel/task/seedance3rd/adaptor.go`
- Test: `relay/channel/task/seedance3rd/adaptor_test.go`

**Interfaces:**
- Consumes: `fetchResponse`(Task 1)。
- Produces: `func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error)` — 把第三方查询响应映射为内部 `TaskInfo`(状态/进度/URL/tokens)。

- [ ] **Step 1: 写失败测试**

追加到 `adaptor_test.go`(补 import `github.com/QuantumNous/new-api/model`):
```go
func TestParseTaskResult(t *testing.T) {
	a := &TaskAdaptor{}
	cases := []struct {
		name       string
		body       string
		wantStatus model.TaskStatus
		wantURL    string
		wantTokens int
	}{
		{"pending", `{"task":{"status":"pending"}}`, model.TaskStatusQueued, "", 0},
		{"processing", `{"task":{"status":"processing"}}`, model.TaskStatusInProgress, "", 0},
		{"completed", `{"task":{"status":"completed","outputs":["https://v/1.mp4"],"usage":{"completion_tokens":40594,"total_tokens":40594}}}`, model.TaskStatusSuccess, "https://v/1.mp4", 40594},
		{"failed", `{"task":{"status":"failed","error":{"message":"boom"}}}`, model.TaskStatusFailure, "", 0},
		{"unknown", `{"task":{"status":"weird"}}`, model.TaskStatusInProgress, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := a.ParseTaskResult([]byte(tc.body))
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("status = %v, want %v", got.Status, tc.wantStatus)
			}
			if got.Url != tc.wantURL {
				t.Fatalf("url = %q, want %q", got.Url, tc.wantURL)
			}
			if got.CompletionTokens != tc.wantTokens {
				t.Fatalf("tokens = %d, want %d", got.CompletionTokens, tc.wantTokens)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestParseTaskResult' -v`
Expected: 编译失败(`a.ParseTaskResult undefined`)。

- [ ] **Step 3: 实现 `ParseTaskResult`**

在 `adaptor.go` 追加:
```go
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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestParseTaskResult' -v`
Expected: PASS(5 个子测试)。

- [ ] **Step 5: 提交**

```bash
git add relay/channel/task/seedance3rd/adaptor.go relay/channel/task/seedance3rd/adaptor_test.go
git commit -m "feat(seedance3rd): ParseTaskResult status mapping"
```

---

### Task 4: `DoRequest` + `DoResponse`(嵌套 `{task:{id}}`)

**Files:**
- Modify: `relay/channel/task/seedance3rd/adaptor.go`
- Test: `relay/channel/task/seedance3rd/adaptor_test.go`

**Interfaces:**
- Consumes: `submitResponse`(Task 1)。
- Produces: `func (a *TaskAdaptor) DoRequest(c, info, requestBody) (*http.Response, error)`;`func (a *TaskAdaptor) DoResponse(c, resp, info) (taskID string, taskData []byte, taskErr *dto.TaskError)` — 解析嵌套 `{task:{id}}`,取 `task.id` 作上游 id,回写 OpenAI video 响应。

- [ ] **Step 1: 写失败测试(用 httptest 构造上游响应)**

追加到 `adaptor_test.go`(补 import `httptest`、`strings`、`bytes`、`io`、`net/http/httptest`、`github.com/gin-gonic/gin`):
```go
func newTestGinCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	return c, w
}

func TestDoResponse_ExtractsTaskID(t *testing.T) {
	a := &TaskAdaptor{}
	c, _ := newTestGinCtx()
	info := &relaycommon.RelayInfo{PublicTaskID: "task_public_123", OriginModelName: "dreamina-seedance-2-0-260128"}
	body := `{"task":{"id":"mvt-179197ccca01401a","status":"pending"}}`
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}

	taskID, _, taskErr := a.DoResponse(c, resp, info)
	if taskErr != nil {
		t.Fatalf("taskErr: %+v", taskErr)
	}
	if taskID != "mvt-179197ccca01401a" {
		t.Fatalf("taskID = %q, want upstream id", taskID)
	}
}

func TestDoResponse_EmptyIDErrors(t *testing.T) {
	a := &TaskAdaptor{}
	c, _ := newTestGinCtx()
	info := &relaycommon.RelayInfo{PublicTaskID: "task_public_123"}
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"task":{"id":""}}`))}
	_, _, taskErr := a.DoResponse(c, resp, info)
	if taskErr == nil {
		t.Fatalf("expected taskErr for empty id")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestDoResponse' -v`
Expected: 编译失败(`a.DoResponse undefined`)。

- [ ] **Step 3: 实现 `DoRequest` / `DoResponse`**

在 `adaptor.go` 追加(补 import `io`、`time`、`github.com/QuantumNous/new-api/relay/channel`、`github.com/QuantumNous/new-api/service`):
```go
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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestDoResponse' -v`
Expected: PASS(两个测试)。

- [ ] **Step 5: 提交**

```bash
git add relay/channel/task/seedance3rd/adaptor.go relay/channel/task/seedance3rd/adaptor_test.go
git commit -m "feat(seedance3rd): DoRequest/DoResponse (nested task envelope)"
```

---

### Task 5: `FetchTask` + `EstimateBilling` + `ConvertToOpenAIVideo`

**Files:**
- Modify: `relay/channel/task/seedance3rd/adaptor.go`
- Test: `relay/channel/task/seedance3rd/adaptor_test.go`

**Interfaces:**
- Consumes: `fetchResponse`(Task 1);共享包 `github.com/QuantumNous/new-api/relay/channel/task/seedance`(`IsSeedance2`、`EstimateBilling`)。
- Produces: `FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error)`;`EstimateBilling(c, info) map[string]float64`;`ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error)`。

- [ ] **Step 1: 写失败测试(ConvertToOpenAIVideo 回显 URL + tokens)**

追加到 `adaptor_test.go`:
```go
func TestConvertToOpenAIVideo(t *testing.T) {
	a := &TaskAdaptor{}
	task := &model.Task{
		TaskID:    "task_public_123",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 1000,
		UpdatedAt: 2000,
	}
	task.Properties.OriginModelName = "dreamina-seedance-2-0-260128"
	task.Data = []byte(`{"task":{"status":"completed","outputs":["https://v/1.mp4"],"usage":{"completion_tokens":40594,"total_tokens":40594}}}`)

	out, err := a.ConvertToOpenAIVideo(task)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "https://v/1.mp4") {
		t.Fatalf("output missing video url: %s", s)
	}
	if !strings.Contains(s, "completion_tokens") {
		t.Fatalf("output missing usage: %s", s)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestConvertToOpenAIVideo' -v`
Expected: 编译失败(`a.ConvertToOpenAIVideo undefined`)。

- [ ] **Step 3: 实现三个方法**

在 `adaptor.go` 追加(补 import `github.com/QuantumNous/new-api/model`、`github.com/QuantumNous/new-api/relay/channel/task/seedance`):
```go
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
		ov.Error = &dto.OpenAIVideoError{Message: tk.Error.Message, Code: tk.Error.Code}
	}
	return common.Marshal(ov)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestConvertToOpenAIVideo' -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add relay/channel/task/seedance3rd/adaptor.go relay/channel/task/seedance3rd/adaptor_test.go
git commit -m "feat(seedance3rd): FetchTask, EstimateBilling (shared seedance pkg), ConvertToOpenAIVideo"
```

---

### Task 6: 渠道设置字段 `Seedance3rdAssetEnabled`

**Files:**
- Modify: `dto/channel_settings.go:54`(在 `BytePlusModerationSkip` 之后)
- Test: `dto/channel_settings_test.go`(若不存在则创建)

**Interfaces:**
- Produces: `ChannelOtherSettings.Seedance3rdAssetEnabled bool`(JSON key `seedance3rd_asset_enabled`)。

- [ ] **Step 1: 写失败测试**

`dto/channel_settings_test.go`:
```go
package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestSeedance3rdAssetEnabledRoundTrip(t *testing.T) {
	s := ChannelOtherSettings{Seedance3rdAssetEnabled: true}
	b, err := common.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back ChannelOtherSettings
	if err := common.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.Seedance3rdAssetEnabled {
		t.Fatalf("Seedance3rdAssetEnabled lost in round-trip: %s", string(b))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./dto/ -run 'TestSeedance3rdAssetEnabled' -v`
Expected: 编译失败(`unknown field Seedance3rdAssetEnabled`)。

- [ ] **Step 3: 加字段**

在 `dto/channel_settings.go` 的 `ChannelOtherSettings` 结构体末尾(`BytePlusModerationSkip` 行之后)追加:
```go
	// 第三方 Seedance 渠道(model.service-inference.ai)素材库预上传总开关。
	// 开启后提交视频生成前把参考媒体(公网 URL)预上传到素材库,换 asset://<id> 再提交。
	// 鉴权复用渠道 Bearer key;素材组按渠道自动创建/复用。
	Seedance3rdAssetEnabled bool `json:"seedance3rd_asset_enabled,omitempty"`
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./dto/ -run 'TestSeedance3rdAssetEnabled' -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add dto/channel_settings.go dto/channel_settings_test.go
git commit -m "feat(seedance3rd): add Seedance3rdAssetEnabled channel setting"
```

---

### Task 7: 素材上传 — `sdAssetClient`(`/v1/sd/assets`,hc 流程)

**Files:**
- Create: `relay/channel/task/seedance3rd/asset.go`
- Test: `relay/channel/task/seedance3rd/asset_test.go`

**Interfaces:**
- Produces: `assetClient` 接口 `CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error)`;`sdAssetClient` 实现(字段 `baseURL, apiKey string; httpClient *http.Client; pollInterval, pollTimeout time.Duration`),`POST {baseURL}/v1/sd/assets` 创建、轮询 `GET {baseURL}/v1/sd/assets/{id}` 到 `Status=="Active"`。

- [ ] **Step 1: 写失败测试(httptest,create → poll Active)**

`relay/channel/task/seedance3rd/asset_test.go`:
```go
package seedance3rd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSDAssetClient_CreateAndWait(t *testing.T) {
	var createHits, getHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sd/assets":
			createHits++
			w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","base_resp":{"status_code":0}}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/sd/assets/"):
			getHits++
			// 第一次 Processing,第二次 Active,验证轮询逻辑
			if getHits == 1 {
				w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","Status":"Processing"}}`))
			} else {
				w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","Status":"Active"}}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cl := &sdAssetClient{
		baseURL:      srv.URL,
		apiKey:       "sk-x",
		httpClient:   srv.Client(),
		pollInterval: time.Millisecond,
		pollTimeout:  time.Second,
	}
	id, err := cl.CreateAndWait(context.Background(), "https://x/a.png", "Image")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != "asset-abc" {
		t.Fatalf("id = %q, want asset-abc", id)
	}
	if createHits != 1 || getHits != 2 {
		t.Fatalf("createHits=%d getHits=%d, want 1/2", createHits, getHits)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestSDAssetClient' -v`
Expected: 编译失败(`undefined: sdAssetClient`)。

- [ ] **Step 3: 实现 `assetClient` + `sdAssetClient`**

`relay/channel/task/seedance3rd/asset.go`:
```go
package seedance3rd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/pkg/errors"
)

const (
	assetPollInterval = 2 * time.Second
	assetPollTimeout  = 60 * time.Second
)

// assetClient:上传单个媒体 URL → 轮询到就绪 → 返回 assetId。
type assetClient interface {
	CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error)
}

// ---- sdAssetClient: /v1/sd/assets(hc 变体) ----

type sdAssetClient struct {
	baseURL      string
	apiKey       string
	httpClient   *http.Client
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func (cl *sdAssetClient) interval() time.Duration {
	if cl.pollInterval > 0 {
		return cl.pollInterval
	}
	return assetPollInterval
}

func (cl *sdAssetClient) timeout() time.Duration {
	if cl.pollTimeout > 0 {
		return cl.pollTimeout
	}
	return assetPollTimeout
}

type sdCreateResp struct {
	Data struct {
		Id     string `json:"Id"`
		Status string `json:"Status"`
	} `json:"data"`
}

func (cl *sdAssetClient) CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error) {
	body, _ := common.Marshal(map[string]string{"URL": mediaURL, "Name": "newapi", "AssetType": assetType})
	var cr sdCreateResp
	if err := cl.do(ctx, http.MethodPost, "/v1/sd/assets", body, &cr); err != nil {
		return "", errors.Wrap(err, "sd create asset failed")
	}
	if cr.Data.Id == "" {
		return "", errors.New("sd create asset returned empty id")
	}
	deadline := time.Now().Add(cl.timeout())
	for {
		var gr sdCreateResp
		if err := cl.do(ctx, http.MethodGet, "/v1/sd/assets/"+cr.Data.Id, nil, &gr); err != nil {
			return "", errors.Wrap(err, "sd get asset failed")
		}
		switch gr.Data.Status {
		case "Active":
			return cr.Data.Id, nil
		case "Failed":
			return "", errors.Errorf("sd asset %s failed", cr.Data.Id)
		}
		if time.Now().After(deadline) {
			return "", errors.Errorf("sd asset %s not active within %s (last: %s)", cr.Data.Id, cl.timeout(), gr.Data.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.interval()):
		}
	}
}

// do 发起一次 Bearer 鉴权的 JSON 请求并解析到 out。
func (cl *sdAssetClient) do(ctx context.Context, method, path string, body []byte, out any) error {
	return doJSON(ctx, cl.httpClient, method, cl.baseURL+path, cl.apiKey, body, out)
}

// doJSON 是包内共用的 Bearer JSON 请求辅助。
func doJSON(ctx context.Context, client *http.Client, method, url, apiKey string, body []byte, out any) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return errors.Errorf("upstream status %d: %s", resp.StatusCode, truncate(respBody, 512))
	}
	if out != nil {
		if err := common.Unmarshal(respBody, out); err != nil {
			return errors.Wrapf(err, "unmarshal failed (body: %s)", truncate(respBody, 512))
		}
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// pickMedia 返回 content item 的媒体引用与 AssetType(本包独立实现,保隔离)。
func pickMedia(item *ContentItem) (*MediaURL, string) {
	switch {
	case item.ImageURL != nil:
		return item.ImageURL, "Image"
	case item.VideoURL != nil:
		return item.VideoURL, "Video"
	case item.AudioURL != nil:
		return item.AudioURL, "Audio"
	default:
		return nil, ""
	}
}

var _ = fmt.Sprintf // 占位:后续 group client 使用 fmt
var _ = strings.HasSuffix
```

> 注:末尾两个 `var _ =` 仅为让 Task 7 单独编译时不出现「imported and not used」。Task 8/9/10 会实际使用 `fmt`/`strings` 后删除这两行。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestSDAssetClient' -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add relay/channel/task/seedance3rd/asset.go relay/channel/task/seedance3rd/asset_test.go
git commit -m "feat(seedance3rd): sdAssetClient (/v1/sd/assets, hc flow)"
```

---

### Task 8: 素材上传 — `groupAssetClient`(组模式 + 失效重建)

**Files:**
- Modify: `relay/channel/task/seedance3rd/asset.go`
- Test: `relay/channel/task/seedance3rd/asset_test.go`

**Interfaces:**
- Consumes: `assetClient`、`doJSON`、`truncate`(Task 7)。
- Produces: `groupAssetClient`(字段 `baseURL, apiKey, groupName string; httpClient *http.Client; pollInterval, pollTimeout time.Duration; groupID string`),实现 `CreateAndWait`:确保组存在(`POST /v1/asset-groups`)→ `POST /v1/assets` → 轮询 `POST /v1/assets/get` 到 `completed`;创建素材因组失效报错时清空 `groupID` 重建一次。

- [ ] **Step 1: 写失败测试(含组失效重建路径)**

追加到 `asset_test.go`:
```go
func TestGroupAssetClient_CreateAndWait(t *testing.T) {
	var groupCreates, assetCreates, getHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/asset-groups":
			groupCreates++
			w.Write([]byte(`{"id":"group-abc"}`))
		case "/v1/assets":
			assetCreates++
			w.Write([]byte(`{"id":"asset-xyz","task_id":"t-1","status":"processing"}`))
		case "/v1/assets/get":
			getHits++
			if getHits == 1 {
				w.Write([]byte(`{"id":"asset-xyz","status":"processing"}`))
			} else {
				w.Write([]byte(`{"id":"asset-xyz","status":"completed"}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cl := &groupAssetClient{
		baseURL:      srv.URL,
		apiKey:       "sk-x",
		groupName:    "newapi-ch7",
		httpClient:   srv.Client(),
		pollInterval: time.Millisecond,
		pollTimeout:  time.Second,
	}
	id, err := cl.CreateAndWait(context.Background(), "https://x/a.png", "Image")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != "asset-xyz" {
		t.Fatalf("id = %q", id)
	}
	if groupCreates != 1 || assetCreates != 1 || getHits != 2 {
		t.Fatalf("groupCreates=%d assetCreates=%d getHits=%d", groupCreates, assetCreates, getHits)
	}
}

func TestGroupAssetClient_RecreatesGroupOnFailure(t *testing.T) {
	var groupCreates, assetCreates int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/asset-groups":
			groupCreates++
			w.Write([]byte(`{"id":"group-new"}`))
		case "/v1/assets":
			assetCreates++
			if assetCreates == 1 {
				// 首次:组失效
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"asset group not found"}`))
				return
			}
			w.Write([]byte(`{"id":"asset-ok","task_id":"t","status":"processing"}`))
		case "/v1/assets/get":
			w.Write([]byte(`{"id":"asset-ok","status":"completed"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cl := &groupAssetClient{
		baseURL: srv.URL, apiKey: "sk-x", groupName: "newapi-ch7",
		httpClient: srv.Client(), pollInterval: time.Millisecond, pollTimeout: time.Second,
		groupID: "group-stale", // 预置一个失效组,验证清空重建
	}
	id, err := cl.CreateAndWait(context.Background(), "https://x/a.png", "Image")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != "asset-ok" {
		t.Fatalf("id = %q", id)
	}
	if groupCreates != 1 || assetCreates != 2 {
		t.Fatalf("groupCreates=%d assetCreates=%d, want 1/2 (recreate+retry)", groupCreates, assetCreates)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestGroupAssetClient' -v`
Expected: 编译失败(`undefined: groupAssetClient`)。

- [ ] **Step 3: 实现 `groupAssetClient`**

在 `asset.go` 追加(此时 `fmt`/`strings` 仍未被使用,保留 Task 7 末尾的两行 `var _ =` 占位,勿删 —— Task 9 才删):
```go
// ---- groupAssetClient: /v1/asset-groups + /v1/assets(260128 等变体) ----

type groupAssetClient struct {
	baseURL      string
	apiKey       string
	groupName    string
	httpClient   *http.Client
	pollInterval time.Duration
	pollTimeout  time.Duration
	groupID      string // 缓存的组 id;失效时清空重建
}

func (cl *groupAssetClient) interval() time.Duration {
	if cl.pollInterval > 0 {
		return cl.pollInterval
	}
	return assetPollInterval
}

func (cl *groupAssetClient) timeoutDur() time.Duration {
	if cl.pollTimeout > 0 {
		return cl.pollTimeout
	}
	return assetPollTimeout
}

func (cl *groupAssetClient) ensureGroup(ctx context.Context) (string, error) {
	if cl.groupID != "" {
		return cl.groupID, nil
	}
	body, _ := common.Marshal(map[string]string{"name": cl.groupName, "description": "new-api auto group"})
	var gr struct {
		ID string `json:"id"`
	}
	if err := doJSON(ctx, cl.httpClient, http.MethodPost, cl.baseURL+"/v1/asset-groups", cl.apiKey, body, &gr); err != nil {
		return "", errors.Wrap(err, "create asset group failed")
	}
	if gr.ID == "" {
		return "", errors.New("create asset group returned empty id")
	}
	cl.groupID = gr.ID
	return cl.groupID, nil
}

type groupCreateAssetResp struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

func (cl *groupAssetClient) CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error) {
	created, err := cl.createAssetWithGroup(ctx, mediaURL, assetType)
	if err != nil {
		// 组可能被上游轮转失效:清空缓存组、重建、重试一次。
		cl.groupID = ""
		created, err = cl.createAssetWithGroup(ctx, mediaURL, assetType)
		if err != nil {
			return "", err
		}
	}
	if created.ID == "" {
		return "", errors.New("create asset returned empty id")
	}

	deadline := time.Now().Add(cl.timeoutDur())
	for {
		var gr groupCreateAssetResp
		getBody, _ := common.Marshal(map[string]string{"asset_id": created.ID, "task_id": created.TaskID})
		if err := doJSON(ctx, cl.httpClient, http.MethodPost, cl.baseURL+"/v1/assets/get", cl.apiKey, getBody, &gr); err != nil {
			return "", errors.Wrap(err, "get asset failed")
		}
		switch gr.Status {
		case "completed":
			return created.ID, nil
		case "failed":
			return "", errors.Errorf("group asset %s failed", created.ID)
		}
		if time.Now().After(deadline) {
			return "", errors.Errorf("group asset %s not completed within %s (last: %s)", created.ID, cl.timeoutDur(), gr.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.interval()):
		}
	}
}

func (cl *groupAssetClient) createAssetWithGroup(ctx context.Context, mediaURL, assetType string) (groupCreateAssetResp, error) {
	groupID, err := cl.ensureGroup(ctx)
	if err != nil {
		return groupCreateAssetResp{}, err
	}
	body, _ := common.Marshal(map[string]string{
		"group_id": groupID, "url": mediaURL, "asset_type": assetType, "name": "newapi",
	})
	var cr groupCreateAssetResp
	if err := doJSON(ctx, cl.httpClient, http.MethodPost, cl.baseURL+"/v1/assets", cl.apiKey, body, &cr); err != nil {
		return groupCreateAssetResp{}, errors.Wrap(err, "create asset failed")
	}
	return cr, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestGroupAssetClient' -v`
Expected: PASS(两个测试)。

- [ ] **Step 5: 提交**

```bash
git add relay/channel/task/seedance3rd/asset.go relay/channel/task/seedance3rd/asset_test.go
git commit -m "feat(seedance3rd): groupAssetClient with group cache + recreate-on-invalid"
```

---

### Task 9: 素材预上传编排 `preuploadAssets`(流程选择 + 缓存)

**Files:**
- Modify: `relay/channel/task/seedance3rd/asset.go`
- Test: `relay/channel/task/seedance3rd/asset_test.go`

**Interfaces:**
- Consumes: `sdAssetClient`、`groupAssetClient`、`assetClient`、`pickMedia`(Task 7/8);`TaskAdaptor`(Task 1)。
- Produces: `func (a *TaskAdaptor) newAssetClient(model string, httpClient *http.Client) assetClient`(按 `-hc` 后缀选流程);`func (a *TaskAdaptor) preuploadAssets(c *gin.Context, payload *requestPayload) error`;URL→assetId 缓存(`assetCacheKey`、`getCachedAssetID`、`setCachedAssetID`)。

- [ ] **Step 1: 写失败测试(开关关闭零行为 + 流程选择)**

追加到 `asset_test.go`(补 import `github.com/QuantumNous/new-api/dto`):
```go
func TestPreuploadAssets_DisabledIsNoop(t *testing.T) {
	a := &TaskAdaptor{}
	a.otherSettings = dto.ChannelOtherSettings{Seedance3rdAssetEnabled: false}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://x/a.png"}},
	}}
	if err := a.preuploadAssets(nil, payload); err != nil {
		t.Fatalf("err: %v", err)
	}
	if payload.Content[0].ImageURL.URL != "https://x/a.png" {
		t.Fatalf("url mutated while disabled: %q", payload.Content[0].ImageURL.URL)
	}
}

func TestNewAssetClient_SelectsByHCSuffix(t *testing.T) {
	a := &TaskAdaptor{baseURL: "https://x", apiKey: "sk"}
	if _, ok := a.newAssetClient("dreamina-seedance-2-0-260128-hc", nil).(*sdAssetClient); !ok {
		t.Fatalf("-hc should select sdAssetClient")
	}
	if _, ok := a.newAssetClient("dreamina-seedance-2-0-260128", nil).(*groupAssetClient); !ok {
		t.Fatalf("non-hc should select groupAssetClient")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestPreuploadAssets|TestNewAssetClient' -v`
Expected: 编译失败(`a.preuploadAssets undefined` / `a.newAssetClient undefined`)。

- [ ] **Step 3: 实现编排 + 缓存;删除 Task 7 的两行 `var _ =` 占位**

在 `asset.go` 删除文件末尾 `var _ = fmt.Sprintf` / `var _ = strings.HasSuffix` 两行(现在 `fmt`/`strings` 被 `preuploadAssets`/`assetCacheKey`/`newAssetClient` 真正使用),并追加(补 import `crypto/sha256`、`encoding/hex`、`sync`、`github.com/QuantumNous/new-api/logger`、`github.com/QuantumNous/new-api/pkg/cachex`、`github.com/QuantumNous/new-api/service`、`github.com/samber/hot`、`github.com/gin-gonic/gin`):
```go
const assetCacheTTL = 6 * time.Hour

// newAssetClient 按上游(带后缀)模型名选流程:-hc → sd/assets;其余 → 组模式。
func (a *TaskAdaptor) newAssetClient(model string, httpClient *http.Client) assetClient {
	base := a.baseURL
	if a.endpointOverride != "" {
		base = a.endpointOverride
	}
	if strings.HasSuffix(model, "-hc") {
		return &sdAssetClient{baseURL: base, apiKey: a.apiKey, httpClient: httpClient}
	}
	return &groupAssetClient{
		baseURL:    base,
		apiKey:     a.apiKey,
		groupName:  fmt.Sprintf("newapi-ch%d", a.channelId),
		httpClient: httpClient,
	}
}

// preuploadAssets 开关开启时,把 payload.Content 中每个公网媒体 URL 预上传素材库,
// 就地替换为 asset://<id>。开关关闭时零行为。
func (a *TaskAdaptor) preuploadAssets(c *gin.Context, payload *requestPayload) error {
	if !a.otherSettings.Seedance3rdAssetEnabled {
		return nil
	}
	httpClient, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return errors.Wrap(err, "create http client for seedance3rd asset upload failed")
	}
	cl := a.newAssetClient(payload.Model, httpClient)

	ctx := context.Background()
	if c != nil {
		ctx = c.Request.Context()
	}
	for i := range payload.Content {
		item := &payload.Content[i]
		media, assetType := pickMedia(item)
		if media == nil {
			continue
		}
		url := strings.TrimSpace(media.URL)
		if url == "" || strings.HasPrefix(url, "asset://") {
			continue
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return errors.Errorf("seedance3rd asset upload requires a public http(s) URL for %s (base64/data URIs unsupported)", item.Type)
		}
		key := assetCacheKey(a.channelId, payload.Model, url)
		if id, ok := getCachedAssetID(key); ok {
			media.URL = "asset://" + id
			continue
		}
		id, err := cl.CreateAndWait(ctx, url, assetType)
		if err != nil {
			return errors.Wrapf(err, "preupload %s to seedance3rd asset library failed", item.Type)
		}
		setCachedAssetID(key, id)
		media.URL = "asset://" + id
	}
	return nil
}

func assetCacheKey(channelId int, model, url string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", channelId, model, url)))
	return hex.EncodeToString(h[:])
}

var (
	assetIDCache     *cachex.HybridCache[string]
	assetIDCacheOnce sync.Once
)

func getAssetIDCache() *cachex.HybridCache[string] {
	assetIDCacheOnce.Do(func() {
		assetIDCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace:    cachex.Namespace("seedance3rd_asset"),
			Redis:        common.RDB,
			RedisEnabled: func() bool { return common.RedisEnabled && common.RDB != nil },
			RedisCodec:   cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, 10_000).
					WithTTL(assetCacheTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return assetIDCache
}

func getCachedAssetID(key string) (string, bool) {
	v, found, err := getAssetIDCache().Get(key)
	if err != nil || !found {
		return "", false
	}
	return v, true
}

func setCachedAssetID(key, id string) {
	if err := getAssetIDCache().SetWithTTL(key, id, assetCacheTTL); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("cache seedance3rd asset id failed: %s", err.Error()))
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestPreuploadAssets|TestNewAssetClient' -v`
Expected: PASS。

- [ ] **Step 5: 全包测试 + 提交**

Run: `go test ./relay/channel/task/seedance3rd/ -v`
Expected: 全部 PASS。
```bash
git add relay/channel/task/seedance3rd/asset.go relay/channel/task/seedance3rd/asset_test.go
git commit -m "feat(seedance3rd): preuploadAssets orchestration (flow select by -hc, url cache)"
```

---

### Task 10: `BuildRequestBody`(接线 convert + 模型映射 + 素材预上传)

**Files:**
- Modify: `relay/channel/task/seedance3rd/adaptor.go`
- Test: `relay/channel/task/seedance3rd/adaptor_test.go`

**Interfaces:**
- Consumes: `convertToRequestPayload`(Task 2)、`preuploadAssets`(Task 9)。
- Produces: `func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error)` — 转请求体 → 若 `IsModelMapped` 用 `UpstreamModelName`(带后缀)否则回填 → 预上传素材 → marshal → 存 `info.UpstreamRequestBody`。

- [ ] **Step 1: 写失败测试(模型映射:body.Model = UpstreamModelName)**

追加到 `adaptor_test.go`(需要构造带 body 的 gin 上下文;补 import `bytes`):
```go
func TestBuildRequestBody_UsesMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	reqBody := `{"model":"dreamina-seedance-2-0-260128","prompt":"hi"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	a := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		IsModelMapped:     true,
		UpstreamModelName: "dreamina-seedance-2-0-260128-hc",
		ChannelOtherSettings: dto.ChannelOtherSettings{Seedance3rdAssetEnabled: false},
	}
	// 先让网关把 body 缓存进上下文(ValidateBasicTaskRequest 会 storeTaskRequest)
	if taskErr := a.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("validate: %+v", taskErr)
	}

	r, err := a.BuildRequestBody(c, info)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), `"dreamina-seedance-2-0-260128-hc"`) {
		t.Fatalf("upstream body missing mapped model: %s", buf.String())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestBuildRequestBody' -v`
Expected: 编译失败(`a.BuildRequestBody undefined`)。

- [ ] **Step 3: 实现 `BuildRequestBody`**

在 `adaptor.go` 追加(补 import `bytes`):
```go
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
	if info != nil {
		info.UpstreamRequestBody = data
	}
	return bytes.NewReader(data), nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./relay/channel/task/seedance3rd/ -run 'TestBuildRequestBody' -v`
Expected: PASS。

- [ ] **Step 5: 全包测试 + 提交**

Run: `go test ./relay/channel/task/seedance3rd/ -v`
Expected: 全部 PASS。
```bash
git add relay/channel/task/seedance3rd/adaptor.go relay/channel/task/seedance3rd/adaptor_test.go
git commit -m "feat(seedance3rd): BuildRequestBody (convert + model_mapping + asset preupload)"
```

---

### Task 11: 注册渠道类型 59(常量 + 路由 + 端点 + 测试豁免)

**Files:**
- Modify: `constant/channel.go:58`(常量)、`:122`(BaseURLs)、`:180`(Names)
- Modify: `relay/relay_adaptor.go`(import + `GetTaskAdaptor`)
- Modify: `common/endpoint_type.go:34`(`GetEndpointTypesByChannelType`)
- Modify: `controller/channel-test.go`(`unsupportedTestChannelTypes`)

**Interfaces:**
- Consumes: `seedance3rd.TaskAdaptor`(Task 1–10 完整实现的接口)。
- Produces: `constant.ChannelTypeSeedance3rd = 59`;`GetTaskAdaptor(59)` 返回 `*seedance3rd.TaskAdaptor`。

- [ ] **Step 1: 加常量(在 `ChannelTypeAIGCVideo = 58` 之后、`ChannelTypeDummy` 之前)**

`constant/channel.go`:
```go
	ChannelTypeAIGCVideo      = 58
	ChannelTypeSeedance3rd    = 59
	ChannelTypeDummy          // this one is only for count, do not add any channel after this
```

- [ ] **Step 2: 加 BaseURL(在 index 58 之后)**

`constant/channel.go` `ChannelBaseURLs` 末尾:
```go
	"",                                          //58 - OpenAI Video
	"https://model.service-inference.ai",        //59 - Seedance (3rd party)
```

- [ ] **Step 3: 加显示名(在 `ChannelTypeAIGCVideo` 之后)**

`constant/channel.go` `ChannelTypeNames`:
```go
	ChannelTypeAIGCVideo:      "OpenAI Video",
	ChannelTypeSeedance3rd:    "seedance(第三方)",
```

- [ ] **Step 4: 注册适配器**

`relay/relay_adaptor.go` import 区(与其它 task 适配器并列):
```go
	taskSeedance3rd "github.com/QuantumNous/new-api/relay/channel/task/seedance3rd"
```
`GetTaskAdaptor` 的 `switch channelType` 内追加:
```go
		case constant.ChannelTypeSeedance3rd:
			return &taskSeedance3rd.TaskAdaptor{}
```

- [ ] **Step 5: 加端点类型(与 Sora/AIGCVideo 对齐)**

`common/endpoint_type.go` `GetEndpointTypesByChannelType` 的 switch 内追加:
```go
	case constant.ChannelTypeSeedance3rd:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
```

- [ ] **Step 6: 加入渠道测试豁免**

在 `controller/channel-test.go` 的 `unsupportedTestChannelTypes` 列表中加入 `constant.ChannelTypeSeedance3rd`(与 `ChannelTypeDoubaoVideo` 并列)。

- [ ] **Step 7: 构建 + vet(验证接口完整实现)**

Run: `go build ./... && go vet ./relay/channel/task/seedance3rd/... ./relay/... ./constant/... ./common/... ./controller/...`
Expected: 无错误。若报 `*seedance3rd.TaskAdaptor` 未实现 `channel.TaskAdaptor`,对照缺失方法补齐(应已在 Task 1–10 完成)。

- [ ] **Step 8: 提交**

```bash
git add constant/channel.go relay/relay_adaptor.go common/endpoint_type.go controller/channel-test.go
git commit -m "feat(seedance3rd): register channel type 59 (const/route/endpoint/test)"
```

---

### Task 12: 前端 web/default(React19)

**Files:**
- Modify: `web/default/src/features/channels/constants.ts`
- Modify: `web/default/src/features/channels/lib/channel-utils.ts`
- Modify: `web/default/src/features/channels/lib/channel-form.ts`
- Modify: `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
- Modify: `web/default/src/i18n/locales/en.json`(+ `bun run i18n:sync`)

**Interfaces:**
- Consumes: 后端 `channelType 59` + `seedance3rd_asset_enabled` 设置字段。

- [ ] **Step 1: `CHANNEL_TYPES` 加 59(顺带补漏的 58)**

在 `web/default/src/features/channels/constants.ts` 的 `CHANNEL_TYPES` 对象内加:
```ts
  58: 'OpenAI Video',
  59: 'seedance(第三方)',
```

- [ ] **Step 2: vendor-icon map 加 59**

在 `web/default/src/features/channels/lib/channel-utils.ts` 的图标映射中加(复用 Doubao 图标):
```ts
  59: 'Doubao',
```

- [ ] **Step 3: 素材开关字段(schema/defaults/parse/build,gate [59])**

在 `web/default/src/features/channels/lib/channel-form.ts` 中,仿现有 `byteplus_asset_enabled` 的四处(zod schema 字段、defaults、解析 settings、构建 settings)加 `seedance3rd_asset_enabled: z.boolean().optional()`(默认 false),gate 条件用 `data.type === 59`(或 `[59].includes(data.type)`)。

- [ ] **Step 4: 抽屉 UI 开关(gate [59])**

在 `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx` 仿 BytePlus 区块加一个 Switch,条件 `[59].includes(currentType)`,label 走 i18n key `'Seedance 3rd-party asset preupload'`。

- [ ] **Step 5: i18n 同步**

在 `web/default/src/i18n/locales/en.json` 加英文 key,然后:
Run: `cd web/default && bun run i18n:sync`

- [ ] **Step 6: 构建验证**

Run: `cd web/default && bun run build`
Expected: 构建通过。

- [ ] **Step 7: 提交**

```bash
git add web/default/src/features/channels web/default/src/i18n/locales
git commit -m "feat(seedance3rd): web/default channel type 59 + asset toggle"
```

---

### Task 13: 前端 web/classic(React18/Semi)

**Files:**
- Modify: `web/classic/src/constants/channel.constants.js`
- Modify: `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`
- Modify: `web/classic/src/i18n/locales/en.json`

**Interfaces:**
- Consumes: 后端 `channelType 59` + `seedance3rd_asset_enabled`。

- [ ] **Step 1: `CHANNEL_OPTIONS` 加 59**

在 `web/classic/src/constants/channel.constants.js` 的 `CHANNEL_OPTIONS` 数组加(仿 58):
```js
  { value: 59, color: 'purple', label: 'seedance(第三方)' },
```

- [ ] **Step 2: `localModels` 加 case 59(默认模型列表)**

在 `EditChannelModal.jsx` 的 `localModels` switch 加:
```js
      case 59:
        localModels = [
          'dreamina-seedance-2-0-260128',
          'dreamina-seedance-2-0-fast-260128',
          'dreamina-seedance-2-0-mini-260615',
        ];
        break;
```

- [ ] **Step 3: 素材开关 UI + 加载/保存(gate [59])**

在 `EditChannelModal.jsx` 仿 BytePlus 素材开关:default state 加 `seedance3rd_asset_enabled`;load 时从 `other_settings` 解析(try/catch/else 三处);save 时 build 进 settings + cleanup delete;UI 用 `Form.Switch`,gate `[59].includes(inputs.type)`,通过 `handleChannelOtherSettingsChange` 写入。label 用中文 key `'Seedance第三方素材预上传'`。

- [ ] **Step 4: i18n 英文映射**

在 `web/classic/src/i18n/locales/en.json` 加中文 key → 英文映射:`"Seedance第三方素材预上传": "Seedance 3rd-party asset preupload"`。

- [ ] **Step 5: 构建验证**

Run: `cd web/classic && bun install && bun run build`(若无 bun,`npm run build`)
Expected: 构建通过。

- [ ] **Step 6: 提交**

```bash
git add web/classic/src/constants/channel.constants.js web/classic/src/components/table/channels/modals/EditChannelModal.jsx web/classic/src/i18n/locales/en.json
git commit -m "feat(seedance3rd): web/classic channel type 59 + asset toggle"
```

---

### Task 14: 端到端回归验证

**Files:** 无(纯验证)

- [ ] **Step 1: 全量后端测试**

Run: `go test ./relay/channel/task/seedance3rd/... ./relay/channel/task/seedance/... ./dto/...`
Expected: 全部 PASS(含共享 seedance 包回归)。

- [ ] **Step 2: 全量构建 + vet**

Run: `go build ./... && go vet ./...`
Expected: 无错误、无新增告警。

- [ ] **Step 3: 手动冒烟(需真实第三方 key,可选)**

建渠道类型 59、填 Bearer key,配三个原模型 modelRatio,`model_mapping` 配 `{"dreamina-seedance-2-0-260128":"dreamina-seedance-2-0-260128-hc"}`;开启素材开关。提交文生 / 图生 / 首尾帧 / 含视频输入任务,轮询完成后核对:
  - 上游实际收到带后缀模型名与 `asset://<id>`;
  - 扣费按原名 `quota ≈ completion_tokens × modelRatio × groupRatio × video_pricing ÷ 500000`;
  - 使用记录「视频计费」区块单价/token/分组倍率/公式/实际扣费一致;
  - doubao(45/54)、sora(55/1)旧渠道行为不变。

- [ ] **Step 4: 提交(如手动验证有微调)**

```bash
git add -A && git commit -m "test(seedance3rd): e2e regression pass"
```

---

## Self-Review

**1. Spec coverage:**
- 新渠道类型 59 + BaseURL + 名称 → Task 11 ✓
- 新适配器包全套方法 → Task 1–5, 10 ✓
- 计费复用共享 seedance 包(零改动)→ Task 5(EstimateBilling)✓;`pricing.go` 不动 ✓
- 模型名后缀走内置 model_mapping → Task 10(BuildRequestBody 尊重 IsModelMapped)✓
- 素材预上传两流程 + 按上游后缀选 + 组失效重建 + 缓存 → Task 7/8/9 ✓
- 渠道设置字段 → Task 6 ✓
- 端点/测试豁免 → Task 11 ✓
- 前端两主题 → Task 12/13 ✓
- 轮询零改动 → 计划不含轮询改动 ✓(通用 default 分支)
- 回归验证 → Task 14 ✓

**2. Placeholder scan:** 每个代码步骤含完整可编译代码;测试含真实断言;无 TBD/TODO。Task 7 的 `var _ =` 占位在 Task 8 Step 3 明确删除。

**3. Type consistency:**
- `assetClient.CreateAndWait(ctx, mediaURL, assetType string) (string, error)` — sdAssetClient(Task 7)、groupAssetClient(Task 8)、preuploadAssets 调用(Task 9)一致 ✓
- `newAssetClient(model string, httpClient *http.Client) assetClient` — Task 9 定义与测试一致 ✓
- `convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error)` — Task 2 定义、Task 10 调用一致 ✓
- `ParseTaskResult` 返回 `*relaycommon.TaskInfo` 字段 `Status/Progress/Url/CompletionTokens/TotalTokens` — 与 doubao 参考一致 ✓
- `fetchResponse`/`submitResponse` 结构在 Task 1 定义,Task 3/4/5 复用 ✓
- `Seedance3rdAssetEnabled`(Task 6)在 Task 9/10/12/13 引用一致 ✓
