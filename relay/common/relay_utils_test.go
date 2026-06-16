package common

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这些测试覆盖 promoteUnknownFieldsToMetadata 的契约:
// 1) JSON 路径下,顶层未知字段被合并进 req.Metadata
// 2) metadata 中已有的同名字段优先,顶层只用于补全
// 3) isKnownTaskField 白名单内的字段不会被升级到 metadata
//
// 这是 Doubao Seedance 2.0 文档承诺的"顶层 + metadata 兼容"行为,
// 文档:接口文档/提克seedance2视频文档/SeeDance2系列视频生成说明.md

func newJSONTaskContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

// 用例 1:Doubao Seedance 2.0 文档方式 1.1 ——纯顶层 ratio/resolution/duration/generate_audio/watermark
func TestPromoteUnknownFields_TextToVideoTopLevel(t *testing.T) {
	body := `{
		"model": "doubao-seedance-2-0-fast-260128",
		"prompt": "一个女孩跳舞",
		"resolution": "480p",
		"ratio": "9:16",
		"duration": 5,
		"generate_audio": true,
		"watermark": false
	}`
	c := newJSONTaskContext(t, body)
	req := &TaskSubmitReq{}

	promoteUnknownFieldsToMetadata(c, req)

	require.NotNil(t, req.Metadata)
	// 未知字段应被合并
	assert.Equal(t, "480p", req.Metadata["resolution"])
	assert.Equal(t, "9:16", req.Metadata["ratio"])
	assert.Equal(t, true, req.Metadata["generate_audio"])
	assert.Equal(t, false, req.Metadata["watermark"])
	// 已知字段不进 metadata(由 TaskSubmitReq 直接字段处理)
	_, hasModel := req.Metadata["model"]
	assert.False(t, hasModel, "model 是已知字段,不应进 metadata")
	_, hasPrompt := req.Metadata["prompt"]
	assert.False(t, hasPrompt, "prompt 是已知字段,不应进 metadata")
	_, hasDuration := req.Metadata["duration"]
	assert.False(t, hasDuration, "duration 在 isKnownTaskField 白名单,不应进 metadata(由 req.Duration 处理)")
}

// 用例 2:metadata 中已有同名字段优先于顶层(文档明文契约)
func TestPromoteUnknownFields_MetadataTakesPrecedence(t *testing.T) {
	body := `{
		"model": "doubao-seedance-2-0-fast-260128",
		"prompt": "test",
		"ratio": "16:9",
		"metadata": {
			"ratio": "9:16"
		}
	}`
	c := newJSONTaskContext(t, body)
	req := &TaskSubmitReq{
		Metadata: map[string]interface{}{
			"ratio": "9:16", // 模拟已被 UnmarshalBodyReusable 解码出来
		},
	}

	promoteUnknownFieldsToMetadata(c, req)

	assert.Equal(t, "9:16", req.Metadata["ratio"], "metadata 中已有的 ratio 优先,顶层 16:9 不应覆盖")
}

// 用例 3:顶层 content[] 也被升级到 metadata
func TestPromoteUnknownFields_TopLevelContent(t *testing.T) {
	body := `{
		"model": "doubao-seedance-2-0-fast-260128",
		"prompt": "test",
		"content": [
			{"type": "image_url", "role": "reference_image", "image_url": {"url": "https://example.com/r.png"}}
		]
	}`
	c := newJSONTaskContext(t, body)
	req := &TaskSubmitReq{}

	promoteUnknownFieldsToMetadata(c, req)

	require.NotNil(t, req.Metadata)
	contentRaw, ok := req.Metadata["content"]
	require.True(t, ok, "content 应被升级到 metadata")
	contentSlice, ok := contentRaw.([]interface{})
	require.True(t, ok)
	require.Len(t, contentSlice, 1)
}

// 用例 4:Doubao Seedance 完整顶层字段全部升级
func TestPromoteUnknownFields_AllDoubaoFields(t *testing.T) {
	body := `{
		"model": "doubao-seedance-2-0-260128",
		"prompt": "test",
		"resolution": "720p",
		"ratio": "16:9",
		"duration": 7,
		"generate_audio": false,
		"watermark": true,
		"seed": 12345,
		"camera_fixed": true,
		"safety_identifier": "abc",
		"return_last_frame": true,
		"service_tier": "default",
		"execution_expires_after": 3600
	}`
	c := newJSONTaskContext(t, body)
	req := &TaskSubmitReq{}

	promoteUnknownFieldsToMetadata(c, req)

	require.NotNil(t, req.Metadata)
	assert.Equal(t, "720p", req.Metadata["resolution"])
	assert.Equal(t, "16:9", req.Metadata["ratio"])
	assert.Equal(t, false, req.Metadata["generate_audio"])
	assert.Equal(t, true, req.Metadata["watermark"])
	// JSON 数字会解码成 float64(map[string]interface{} 默认),不是 int
	assert.Equal(t, float64(12345), req.Metadata["seed"])
	assert.Equal(t, true, req.Metadata["camera_fixed"])
	assert.Equal(t, "abc", req.Metadata["safety_identifier"])
	assert.Equal(t, true, req.Metadata["return_last_frame"])
	assert.Equal(t, "default", req.Metadata["service_tier"])
	assert.Equal(t, float64(3600), req.Metadata["execution_expires_after"])
	// duration 是已知字段
	_, hasDuration := req.Metadata["duration"]
	assert.False(t, hasDuration)
}

// 用例 5:空请求体不 panic,req.Metadata 不被错误地写入未知值
func TestPromoteUnknownFields_EmptyBody(t *testing.T) {
	c := newJSONTaskContext(t, `{}`)
	req := &TaskSubmitReq{}

	promoteUnknownFieldsToMetadata(c, req)

	// 没有字段可合并;metadata 可能被初始化为空 map(可接受)
	if req.Metadata != nil {
		assert.Empty(t, req.Metadata)
	}
}

// 用例 6:metadata + 顶层 混合,顶层补全 metadata 缺失项
func TestPromoteUnknownFields_TopLevelFillsMissing(t *testing.T) {
	body := `{
		"model": "doubao-seedance-2-0-fast-260128",
		"prompt": "test",
		"ratio": "16:9",
		"resolution": "1080p",
		"metadata": {
			"ratio": "9:16"
		}
	}`
	c := newJSONTaskContext(t, body)
	req := &TaskSubmitReq{
		Metadata: map[string]interface{}{
			"ratio": "9:16",
		},
	}

	promoteUnknownFieldsToMetadata(c, req)

	assert.Equal(t, "9:16", req.Metadata["ratio"], "metadata.ratio 不被顶层覆盖")
	assert.Equal(t, "1080p", req.Metadata["resolution"], "顶层 resolution 补全到 metadata")
}
