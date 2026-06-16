package doubao

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 注意:这些用例对 convertToRequestPayload 进行单元测试。
// 它假定 promoteUnknownFieldsToMetadata 已经在 ValidateBasicTaskRequest 里把
// 顶层未知字段(ratio/resolution/generate_audio/watermark/content/seed/camera_fixed/
// safety_identifier/return_last_frame/service_tier/execution_expires_after)合并到 req.Metadata,
// 而已知顶层字段(duration / images / image_reference)留在 req.Duration / req.Images / req.ImageReference。
// 顶层→metadata 合并的契约本身在 relay/common/relay_utils_test.go 里测。

func newAdaptor() *TaskAdaptor {
	return &TaskAdaptor{}
}

// 用例 1:文档说明.md 方式 1.1 ——文生视频,纯顶层 ratio/resolution/duration/generate_audio/watermark
func TestConvert_TopLevel_TextToVideo(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "一个女孩跳舞",
		Duration: 5, // 顶层 duration 进 req.Duration,不进 metadata
		Metadata: map[string]interface{}{
			"resolution":     "480p",
			"ratio":          "9:16",
			"generate_audio": true,
			"watermark":      false,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	require.NotNil(t, r)

	assert.Equal(t, "doubao-seedance-2-0-fast-260128", r.Model)
	assert.Equal(t, "480p", r.Resolution)
	assert.Equal(t, "9:16", r.Ratio)
	require.NotNil(t, r.Duration, "Duration should be set via req.Duration fallback")
	assert.Equal(t, 5, int(*r.Duration))
	require.NotNil(t, r.GenerateAudio)
	assert.Equal(t, true, bool(*r.GenerateAudio))
	require.NotNil(t, r.Watermark, "watermark:false (explicit zero) must survive omitempty pointer round-trip")
	assert.Equal(t, false, bool(*r.Watermark))

	// content 只剩一个 text 项(prompt)
	require.Len(t, r.Content, 1)
	assert.Equal(t, "text", r.Content[0].Type)
	assert.Equal(t, "一个女孩跳舞", r.Content[0].Text)
}

// 用例 2:文档说明.md 方式 1.2 ——首帧模式,顶层 content 包含 first_frame
func TestConvert_TopLevel_FirstFrame(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "让人物自然眨眼并轻微转头",
		Duration: 5,
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "image_url",
					"role": "first_frame",
					"image_url": map[string]interface{}{
						"url": "https://example.com/start.png",
					},
				},
			},
			"resolution": "720p",
			"ratio":      "16:9",
			"watermark":  false,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	require.NotNil(t, r)

	assert.Equal(t, "720p", r.Resolution)
	assert.Equal(t, "16:9", r.Ratio)
	require.NotNil(t, r.Duration)
	assert.Equal(t, 5, int(*r.Duration))

	require.Len(t, r.Content, 2, "first_frame image + text prompt")
	assert.Equal(t, "image_url", r.Content[0].Type)
	assert.Equal(t, "first_frame", r.Content[0].Role)
	require.NotNil(t, r.Content[0].ImageURL)
	assert.Equal(t, "https://example.com/start.png", r.Content[0].ImageURL.URL)
	assert.Equal(t, "text", r.Content[1].Type)
	assert.Equal(t, "让人物自然眨眼并轻微转头", r.Content[1].Text)
}

// 用例 3:文档说明.md 方式 1.3 ——首尾帧模式 + return_last_frame:true
func TestConvert_TopLevel_FirstLastFrame(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-260128",
		Prompt:   "镜头平稳推进,人物表情自然变化",
		Duration: 5,
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type":      "image_url",
					"role":      "first_frame",
					"image_url": map[string]interface{}{"url": "https://example.com/start.png"},
				},
				map[string]interface{}{
					"type":      "image_url",
					"role":      "last_frame",
					"image_url": map[string]interface{}{"url": "https://example.com/end.png"},
				},
			},
			"return_last_frame": true,
			"resolution":        "720p",
			"ratio":             "16:9",
			"watermark":         false,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)

	require.NotNil(t, r.ReturnLastFrame)
	assert.Equal(t, true, bool(*r.ReturnLastFrame))
	assert.Equal(t, "720p", r.Resolution)
	assert.Equal(t, "16:9", r.Ratio)

	require.Len(t, r.Content, 3)
	assert.Equal(t, "first_frame", r.Content[0].Role)
	assert.Equal(t, "last_frame", r.Content[1].Role)
	assert.Equal(t, "text", r.Content[2].Type)
}

// 用例 4:文档说明.md 方式 1.4 ——多图参考模式(多个 reference_image)
func TestConvert_TopLevel_MultiReference(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-260128",
		Prompt:   "融合多张参考图",
		Duration: 5,
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type":      "image_url",
					"role":      "reference_image",
					"image_url": map[string]interface{}{"url": "https://example.com/ref1.png"},
				},
				map[string]interface{}{
					"type":      "image_url",
					"role":      "reference_image",
					"image_url": map[string]interface{}{"url": "https://example.com/ref2.png"},
				},
				map[string]interface{}{
					"type":      "image_url",
					"role":      "reference_image",
					"image_url": map[string]interface{}{"url": "https://example.com/ref3.png"},
				},
			},
			"resolution": "720p",
			"ratio":      "16:9",
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)

	require.Len(t, r.Content, 4, "3 reference_image + text prompt")
	for i := 0; i < 3; i++ {
		assert.Equal(t, "image_url", r.Content[i].Type)
		assert.Equal(t, "reference_image", r.Content[i].Role)
		require.NotNil(t, r.Content[i].ImageURL)
	}
	assert.Equal(t, "text", r.Content[3].Type)
}

// 用例 5:文档说明.md 方式 1.5 ——多模态(reference_image + reference_video + reference_audio)
func TestConvert_TopLevel_Multimodal(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-260128",
		Prompt:   "保持人物主体一致",
		Duration: 5,
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type":      "image_url",
					"role":      "reference_image",
					"image_url": map[string]interface{}{"url": "https://example.com/portrait.png"},
				},
				map[string]interface{}{
					"type":      "video_url",
					"role":      "reference_video",
					"video_url": map[string]interface{}{"url": "https://example.com/reference.mp4"},
				},
				map[string]interface{}{
					"type":      "audio_url",
					"role":      "reference_audio",
					"audio_url": map[string]interface{}{"url": "https://example.com/reference.mp3"},
				},
			},
			"resolution":     "720p",
			"ratio":          "16:9",
			"generate_audio": false,
			"watermark":      false,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)

	require.NotNil(t, r.GenerateAudio)
	assert.Equal(t, false, bool(*r.GenerateAudio), "generate_audio:false (explicit zero) must survive")

	require.Len(t, r.Content, 4, "image + video + audio + text")
	assert.Equal(t, "image_url", r.Content[0].Type)
	assert.Equal(t, "reference_image", r.Content[0].Role)
	assert.Equal(t, "video_url", r.Content[1].Type)
	assert.Equal(t, "reference_video", r.Content[1].Role)
	require.NotNil(t, r.Content[1].VideoURL)
	assert.Equal(t, "https://example.com/reference.mp4", r.Content[1].VideoURL.URL)
	assert.Equal(t, "audio_url", r.Content[2].Type)
	assert.Equal(t, "reference_audio", r.Content[2].Role)
	require.NotNil(t, r.Content[2].AudioURL)
	assert.Equal(t, "https://example.com/reference.mp3", r.Content[2].AudioURL.URL)
	assert.Equal(t, "text", r.Content[3].Type)
}

// 用例 6:文档说明.md 方式 2 ——所有字段都在 metadata 里(向后兼容)
// 行为应当与方式 1 等价。
func TestConvert_MetadataWrapped(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-fast-260128",
		Prompt: "一个女孩跳舞",
		// 注意:这里 duration 在 metadata 里,不在 req.Duration
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type":      "image_url",
					"role":      "reference_image",
					"image_url": map[string]interface{}{"url": "https://example.com/ref.png"},
				},
			},
			"resolution":     "480p",
			"generate_audio": true,
			"ratio":          "9:16",
			"duration":       5,
			"watermark":      false,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)

	assert.Equal(t, "480p", r.Resolution)
	assert.Equal(t, "9:16", r.Ratio)
	require.NotNil(t, r.Duration)
	assert.Equal(t, 5, int(*r.Duration))
	require.NotNil(t, r.GenerateAudio)
	assert.Equal(t, true, bool(*r.GenerateAudio))
	require.NotNil(t, r.Watermark)
	assert.Equal(t, false, bool(*r.Watermark))

	require.Len(t, r.Content, 2)
	assert.Equal(t, "image_url", r.Content[0].Type)
	assert.Equal(t, "reference_image", r.Content[0].Role)
	assert.Equal(t, "text", r.Content[1].Type)
}

// 用例 7:文档说明.md 优先级规则 ——metadata 中已有的同名字段优先于顶层。
// 假设 promoteUnknownFieldsToMetadata 已经按规则把"顶层 ratio:16:9"
// 在 metadata.ratio 已有时跳过了,所以 req.Metadata 里只有 metadata 版本。
func TestConvert_MetadataTakesPrecedence(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-fast-260128",
		Prompt: "test",
		Metadata: map[string]interface{}{
			"ratio": "9:16", // 此字段假定已经由 metadata 提供,promote 跳过了顶层的 "16:9"
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	assert.Equal(t, "9:16", r.Ratio)
}

// 用例 8:`req.Images` 与 `metadata.content[]` 同时存在 ——两组都进上游 content
func TestConvert_ImagesPlusMetadataContent(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-260128",
		Prompt:   "test",
		Duration: 5,
		Images:   []string{"https://example.com/legacy.png"},
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type":      "image_url",
					"role":      "reference_image",
					"image_url": map[string]interface{}{"url": "https://example.com/meta.png"},
				},
			},
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)

	require.Len(t, r.Content, 3, "legacy images + metadata.content + text")
	// req.Images 项在前
	assert.Equal(t, "image_url", r.Content[0].Type)
	require.NotNil(t, r.Content[0].ImageURL)
	assert.Equal(t, "https://example.com/legacy.png", r.Content[0].ImageURL.URL)
	assert.Equal(t, "", r.Content[0].Role, "legacy images 没有 role")
	// metadata.content 项在后
	assert.Equal(t, "image_url", r.Content[1].Type)
	assert.Equal(t, "reference_image", r.Content[1].Role)
	require.NotNil(t, r.Content[1].ImageURL)
	assert.Equal(t, "https://example.com/meta.png", r.Content[1].ImageURL.URL)
	// text prompt 最后
	assert.Equal(t, "text", r.Content[2].Type)
}

// 用例 9:顶层 duration 回退 ——`req.Seconds=""` 且 metadata 中无 duration 时,
// 顶层 duration:7 必须被 Doubao 适配器读出,否则上游回落默认 5。
func TestConvert_TopLevelDurationFallback(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "test",
		Duration: 7,
		Seconds:  "",
		Metadata: map[string]interface{}{
			"ratio": "1:1",
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	require.NotNil(t, r.Duration, "req.Duration=7 must populate r.Duration when seconds & metadata.duration both absent")
	assert.Equal(t, 7, int(*r.Duration))
	assert.Equal(t, "1:1", r.Ratio)
}

// 用例 10:序列化后的上游 JSON 真的包含所有期望字段(端到端检查)
func TestConvert_MarshalsAllFields(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "最佳年度公益广告: ...",
		Duration: 7,
		Metadata: map[string]interface{}{
			"resolution":     "480p",
			"ratio":          "1:1",
			"generate_audio": true,
			"watermark":      false,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)

	data, err := common.Marshal(r)
	require.NoError(t, err)
	body := string(data)
	t.Logf("upstream payload: %s", body)

	assert.Contains(t, body, `"model":"doubao-seedance-2-0-fast-260128"`)
	assert.Contains(t, body, `"resolution":"480p"`)
	assert.Contains(t, body, `"ratio":"1:1"`)
	assert.Contains(t, body, `"duration":7`)
	assert.Contains(t, body, `"generate_audio":true`)
	assert.Contains(t, body, `"watermark":false`, "explicit watermark:false must reach upstream (Rule 6)")
}

// 用例 11:确认 dto.IntValue / dto.BoolValue 行为符合预期 ——JSON 数字直接解码
func TestConvert_IntValueAcceptsJSONNumber(t *testing.T) {
	a := newAdaptor()
	// metadata.duration 是 JSON 数字 10,模拟前端不带顶层 duration、只在 metadata 里给
	req := &relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-fast-260128",
		Prompt: "test",
		Metadata: map[string]interface{}{
			"duration": 10,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	require.NotNil(t, r.Duration)
	assert.Equal(t, 10, int(*r.Duration))
}

// 用例 12:safety_identifier 字段被透传(新加的字段)
func TestConvert_SafetyIdentifierPassthrough(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "test",
		Duration: 5,
		Metadata: map[string]interface{}{
			"safety_identifier": "abuse-detect-2024",
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	assert.Equal(t, "abuse-detect-2024", r.SafetyIdentifier)
}

// 用例 13:seed / camera_fixed 字段透传
func TestConvert_SeedAndCameraFixed(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "test",
		Duration: 5,
		Metadata: map[string]interface{}{
			"seed":         12345,
			"camera_fixed": true,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	require.NotNil(t, r.Seed)
	assert.Equal(t, 12345, int(*r.Seed))
	require.NotNil(t, r.CameraFixed)
	assert.Equal(t, true, bool(*r.CameraFixed))
}

// 用例 14:req.Seconds 优先于 req.Duration 与 metadata.duration
func TestConvert_SecondsTakesPrecedence(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "test",
		Duration: 7,
		Seconds:  "9",
		Metadata: map[string]interface{}{
			"duration": 5,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	require.NotNil(t, r.Duration)
	assert.Equal(t, 9, int(*r.Duration), "req.Seconds=9 should win")
}

// 用例 15:确认 BoolValue 类型与 dto.BoolValue 一致
func TestConvert_BoolValueType(t *testing.T) {
	a := newAdaptor()
	req := &relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "test",
		Duration: 5,
		Metadata: map[string]interface{}{
			"generate_audio": false,
			"watermark":      false,
		},
	}

	r, err := a.convertToRequestPayload(req)
	require.NoError(t, err)
	require.NotNil(t, r.GenerateAudio)
	assert.IsType(t, (*dto.BoolValue)(nil), r.GenerateAudio)
	assert.Equal(t, false, bool(*r.GenerateAudio))
}
