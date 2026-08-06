package seedance3rd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func TestBuildRequestURL(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://model.service-inference.ai", ApiKey: "sk-x"}})
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
	a.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://x", ApiKey: "sk-abc"}})
	req, _ := http.NewRequest(http.MethodPost, "https://x", nil)
	if err := a.BuildRequestHeader(nil, req, nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-abc" {
		t.Fatalf("auth = %q, want %q", got, "Bearer sk-abc")
	}
}

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

// TestConvertToRequestPayload_NoClobberImagesAndMetadataContent 验证 req.Images
// 与 req.Metadata["content"] 同时存在时互不覆盖:两者的 image_url 都要出现在结果里,
// 证明"先抽 metadata.content 再反序列化"逻辑生效(否则 UnmarshalMetadata 整体替换
// []ContentItem slice 会冲掉 req.Images 追加的条目)。
func TestConvertToRequestPayload_NoClobberImagesAndMetadataContent(t *testing.T) {
	a := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{
		Model:  "dreamina-seedance-2-0-260128",
		Prompt: "a girl dancing",
		Images: []string{"https://x/a.png"},
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "image_url",
					"image_url": map[string]interface{}{
						"url": "https://x/b.png",
					},
					"role": "reference_image",
				},
			},
		},
	}
	got, err := a.convertToRequestPayload(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// 期望:req.Images 的 a.png + metadata.content 的 b.png + 末尾 text,共 3 项。
	if len(got.Content) != 3 {
		t.Fatalf("content len = %d, want 3 (%+v)", len(got.Content), got.Content)
	}
	var sawA, sawB bool
	for _, c := range got.Content {
		if c.Type == "image_url" && c.ImageURL != nil {
			switch c.ImageURL.URL {
			case "https://x/a.png":
				sawA = true
			case "https://x/b.png":
				sawB = true
			}
		}
	}
	if !sawA {
		t.Fatalf("req.Images entry (a.png) missing from content: %+v", got.Content)
	}
	if !sawB {
		t.Fatalf("metadata.content entry (b.png) missing from content: %+v", got.Content)
	}
	last := got.Content[len(got.Content)-1]
	if last.Type != "text" || last.Text != "a girl dancing" {
		t.Fatalf("last item not expected text: %+v", last)
	}
}

// TestConvertToRequestPayload_DurationPrecedence_SecondsWinsOverAll 验证优先级最高的
// req.Seconds 覆盖 req.Duration 与 metadata.duration。
func TestConvertToRequestPayload_DurationPrecedence_SecondsWinsOverAll(t *testing.T) {
	a := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{
		Model:    "dreamina-seedance-2-0-260128",
		Prompt:   "blink",
		Seconds:  "7",
		Duration: 5,
		Metadata: map[string]interface{}{
			"duration": 3,
		},
	}
	got, err := a.convertToRequestPayload(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Duration == nil || int(*got.Duration) != 7 {
		t.Fatalf("duration = %v, want 7 (req.Seconds must win)", got.Duration)
	}
}

// TestConvertToRequestPayload_DurationPrecedence_MetadataBeatsReqDuration 验证
// metadata.duration 优先于 req.Duration(当 req.Seconds 为空时)。
func TestConvertToRequestPayload_DurationPrecedence_MetadataBeatsReqDuration(t *testing.T) {
	a := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{
		Model:    "dreamina-seedance-2-0-260128",
		Prompt:   "blink",
		Duration: 5,
		Metadata: map[string]interface{}{
			"duration": 4,
		},
	}
	got, err := a.convertToRequestPayload(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Duration == nil || int(*got.Duration) != 4 {
		t.Fatalf("duration = %v, want 4 (metadata.duration must beat req.Duration)", got.Duration)
	}
}

// TestConvertToRequestPayload_StripsStaleTextFromMetadataContent 验证 metadata.content
// 中预置的 text 项会被剔除,最终只保留由 req.Prompt 重建的唯一 text 项。
func TestConvertToRequestPayload_StripsStaleTextFromMetadataContent(t *testing.T) {
	a := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{
		Model:  "dreamina-seedance-2-0-260128",
		Prompt: "fresh prompt",
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "stale",
				},
			},
		},
	}
	got, err := a.convertToRequestPayload(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	textCount := 0
	for _, c := range got.Content {
		if c.Type == "text" {
			textCount++
			if c.Text != "fresh prompt" {
				t.Fatalf("text item = %q, want %q (stale text should be stripped)", c.Text, "fresh prompt")
			}
		}
	}
	if textCount != 1 {
		t.Fatalf("text item count = %d, want 1 (%+v)", textCount, got.Content)
	}
}

// TestParseTaskResult 验证第三方查询响应 { "task": {...} } 到内部 TaskInfo 的状态/进度/URL/tokens 映射。
//
// wantStatus 声明为 string(而非 model.TaskStatus): relaycommon.TaskInfo.Status 字段本身是
// string 类型,model.TaskStatusQueued 等常量是未显式声明类型的字符串常量(仅 TaskStatusNotStart
// 显式标了 model.TaskStatus 类型),直接拿 model.TaskStatus 类型变量与 got.Status(string)比较会
// 编译失败("mismatched types string and model.TaskStatus")。用 string 字段类型规避该问题,
// 常量赋值本身不受影响。
func TestParseTaskResult(t *testing.T) {
	a := &TaskAdaptor{}
	cases := []struct {
		name       string
		body       string
		wantStatus string
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

func newTestGinCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	return c, w
}

func TestDoResponse_ExtractsTaskID(t *testing.T) {
	a := &TaskAdaptor{}
	c, w := newTestGinCtx()
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_123"}, OriginModelName: "dreamina-seedance-2-0-260128"}
	body := `{"task":{"id":"mvt-179197ccca01401a","status":"pending"}}`
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}

	taskID, _, taskErr := a.DoResponse(c, resp, info)
	if taskErr != nil {
		t.Fatalf("taskErr: %+v", taskErr)
	}
	if taskID != "mvt-179197ccca01401a" {
		t.Fatalf("taskID = %q, want upstream id", taskID)
	}
	// 客户端响应体只能看到公开任务 id,绝不能泄露上游 id(内部存储才用上游 id)。
	if !strings.Contains(w.Body.String(), info.PublicTaskID) {
		t.Fatalf("client response missing public task id: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "mvt-179197ccca01401a") {
		t.Fatalf("client response leaked upstream task id: %s", w.Body.String())
	}
}

func TestDoResponse_EmptyIDErrors(t *testing.T) {
	a := &TaskAdaptor{}
	c, _ := newTestGinCtx()
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_123"}}
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"task":{"id":""}}`))}
	_, _, taskErr := a.DoResponse(c, resp, info)
	if taskErr == nil {
		t.Fatalf("expected taskErr for empty id")
	}
}

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

// TestBuildRequestBody_UsesMappedModel 验证模型映射生效时,上游请求体里的 model
// 字段用 info.UpstreamModelName(带 -hc/-ep 后缀),而非客户端原始传入的模型名。
//
// 注意:brief 原始测试字面量把 IsModelMapped/UpstreamModelName/ChannelOtherSettings
// 直接写在 &relaycommon.RelayInfo{} 顶层——这三个字段实际定义在 RelayInfo 匿名嵌入的
// *ChannelMeta 里(指针嵌入的字段不能在外层结构体字面量里直接赋值,会编译失败:
// "unknown field ... in struct literal"),因此这里改为嵌套在
// ChannelMeta: &relaycommon.ChannelMeta{...} 下,语义与 brief 描述一致(字段值不变)。
func TestBuildRequestBody_UsesMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	reqBody := `{"model":"dreamina-seedance-2-0-260128","prompt":"hi"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	a := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			IsModelMapped:        true,
			UpstreamModelName:    "dreamina-seedance-2-0-260128-hc",
			ChannelOtherSettings: dto.ChannelOtherSettings{Seedance3rdAssetEnabled: false},
		},
		// TaskRelayInfo 必须非 nil:ValidateBasicTaskRequest -> storeTaskRequest 会写
		// info.Action(定义在嵌入的 *TaskRelayInfo 里),否则运行时对 nil 指针写字段会 panic
		// (RED 阶段实测复现,非编译错误)。
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
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

func TestConvertToOpenAIVideo_MasksUpstreamErrorMessage(t *testing.T) {
	a := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_pub9",
		Status: model.TaskStatusFailure,
		Data:   []byte(`{"task":{"id":"up9","status":"failed","error":{"code":"download_failed","message":"cannot fetch http://10.0.0.8/material.mp4"}}}`),
	}
	out, err := a.ConvertToOpenAIVideo(task)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "10.0.0.8") {
		t.Fatalf("upstream IP leaked: %s", s)
	}
	if !strings.Contains(s, "download_failed") || !strings.Contains(s, "cannot fetch") {
		t.Fatalf("error info lost: %s", s)
	}
}
