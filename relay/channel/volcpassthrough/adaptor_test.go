package volcpassthrough

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{},
	}
}

func newTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/volc/api/v3/contents/generations/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c
}

// 官方「创建视频生成任务」请求体没有顶层 prompt,文本在 content[] 里。
// 共享校验器 ValidateBasicTaskRequest 会对空 prompt 直接 400,因此透传适配器
// 必须自己解析 —— 这条用例锁住这个契约。
func TestValidateAcceptsOfficialBodyWithoutPrompt(t *testing.T) {
	const body = `{
		"model": "doubao-seedance-1-0-pro-250528",
		"content": [
			{"type": "text", "text": "一只猫在跳舞"},
			{"type": "image_url", "image_url": {"url": "https://example.com/a.png"}}
		],
		"resolution": "1080p",
		"ratio": "16:9",
		"duration": 5
	}`
	c := newTestContext(t, body)
	a := &TaskAdaptor{}
	info := newRelayInfo()

	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)

	submit, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err, "task_request must be set so seedance.ResolveVideoBilling can price the request")
	assert.Equal(t, "doubao-seedance-1-0-pro-250528", submit.Model)
	assert.Equal(t, 5, submit.Duration)
	// 顶层扩展字段升级进 metadata,供档位判定使用。
	assert.Equal(t, "1080p", submit.Metadata["resolution"])
	assert.Equal(t, "16:9", submit.Metadata["ratio"])
	assert.NotNil(t, submit.Metadata["content"])
	// 已声明字段不重复升级。
	assert.Nil(t, submit.Metadata["duration"])
	assert.Nil(t, submit.Metadata["model"])
}

func TestValidateRejectsMissingModel(t *testing.T) {
	c := newTestContext(t, `{"content":[{"type":"text","text":"hi"}]}`)
	a := &TaskAdaptor{}
	taskErr := a.ValidateRequestAndSetAction(c, newRelayInfo())
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}

func TestValidateRejectsInvalidJSON(t *testing.T) {
	c := newTestContext(t, `{not json`)
	a := &TaskAdaptor{}
	taskErr := a.ValidateRequestAndSetAction(c, newRelayInfo())
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}

func TestBuildRequestURLFollowsChannelBaseURL(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://ark.ap-southeast.bytepluses.com/",
			ApiKey:         "sk-test",
		},
	})
	url, err := a.BuildRequestURL(&relaycommon.RelayInfo{})
	require.NoError(t, err)
	assert.Equal(t, "https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks", url)
}

func TestBuildRequestHeaderUsesChannelBearerKey(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://ark.cn-beijing.volces.com",
			ApiKey:         "sk-channel",
		},
	})
	req, _ := http.NewRequest(http.MethodPost, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks", nil)
	require.NoError(t, a.BuildRequestHeader(nil, req, nil))
	// 上游拿到的必须是渠道密钥,不是客户的站点令牌。
	assert.Equal(t, "Bearer sk-channel", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestBuildRequestHeaderFailsWithoutKey(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	assert.Error(t, a.BuildRequestHeader(nil, req, nil))
}

func TestBuildRequestBodyPassesThroughByteForByte(t *testing.T) {
	const body = `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"x"}],"unknown_future_field":true}`
	c := newTestContext(t, body)
	a := &TaskAdaptor{}
	info := newRelayInfo()

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	out := new(bytes.Buffer)
	_, _ = out.ReadFrom(reader)
	// 未配模型映射时字节完全不变,官方新增字段零改造直通。
	assert.JSONEq(t, body, out.String())
	assert.Equal(t, body, string(info.UpstreamRequestBody))
}

func TestBuildRequestBodyAppliesModelMapping(t *testing.T) {
	c := newTestContext(t, `{"model":"client-alias","content":[{"type":"text","text":"x"}]}`)
	a := &TaskAdaptor{}
	info := newRelayInfo()
	info.IsModelMapped = true
	info.UpstreamModelName = "doubao-seedance-1-0-pro-250528"

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	out := new(bytes.Buffer)
	_, _ = out.ReadFrom(reader)
	assert.Contains(t, out.String(), `"model":"doubao-seedance-1-0-pro-250528"`)
	assert.NotContains(t, out.String(), "client-alias")
}

func TestParseTaskResultStatusMapping(t *testing.T) {
	a := &TaskAdaptor{}
	cases := []struct {
		body     string
		status   string
		progress string
	}{
		{`{"status":"queued"}`, string(model.TaskStatusQueued), taskcommon.ProgressQueued},
		{`{"status":"running"}`, string(model.TaskStatusInProgress), taskcommon.ProgressInProgress},
		{`{"status":"processing"}`, string(model.TaskStatusInProgress), taskcommon.ProgressInProgress},
		{`{"status":"cancelled"}`, string(model.TaskStatusFailure), taskcommon.ProgressComplete},
		{`{"status":"brand_new_status"}`, string(model.TaskStatusInProgress), taskcommon.ProgressInProgress},
	}
	for _, tc := range cases {
		info, err := a.ParseTaskResult([]byte(tc.body))
		require.NoError(t, err)
		assert.Equal(t, tc.status, info.Status, tc.body)
		assert.Equal(t, tc.progress, info.Progress, tc.body)
	}
}

// 结算依赖上游 usage.total_tokens 重算实际额度
// (service.settleTaskBillingOnComplete 的 token 分支)。丢了它就只按预扣价收费。
func TestParseTaskResultSuccessCarriesUsage(t *testing.T) {
	a := &TaskAdaptor{}
	info, err := a.ParseTaskResult([]byte(`{
		"status": "succeeded",
		"content": {"video_url": "https://cdn.example.com/v.mp4"},
		"usage": {"completion_tokens": 35100, "total_tokens": 35100}
	}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), info.Status)
	assert.Equal(t, taskcommon.ProgressComplete, info.Progress)
	assert.Equal(t, "https://cdn.example.com/v.mp4", info.Url)
	assert.Equal(t, 35100, info.TotalTokens)
	assert.Equal(t, 35100, info.CompletionTokens)
}

func TestParseTaskResultFailureCarriesReason(t *testing.T) {
	a := &TaskAdaptor{}
	info, err := a.ParseTaskResult([]byte(`{"status":"failed","error":{"message":"content filter"}}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.Equal(t, "content filter", info.Reason)

	info, err = a.ParseTaskResult([]byte(`{"status":"expired"}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.NotEmpty(t, info.Reason, "reason must never be empty, users need a cause")
}

func TestFetchTaskRejectsEmptyTaskID(t *testing.T) {
	a := &TaskAdaptor{}
	_, err := a.FetchTask("https://ark.cn-beijing.volces.com", "sk", map[string]any{}, "")
	assert.Error(t, err)
	_, err = a.FetchTask("https://ark.cn-beijing.volces.com", "sk", map[string]any{"task_id": ""}, "")
	assert.Error(t, err)
}

func TestChannelMetadata(t *testing.T) {
	a := &TaskAdaptor{}
	assert.Equal(t, ChannelName, a.GetChannelName())
	assert.Empty(t, a.GetModelList(), "透传渠道不预置模型清单")
}
