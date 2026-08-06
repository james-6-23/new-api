package sora

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const miniModel = "doubao-seedance-2-0-mini-260615"

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func newTaskCtx(model string, metadata map[string]interface{}) (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	// 真实请求总带 body;这里给个空 JSON 体,避免 peekRawBody 解引用 nil Request。
	// 视频/分辨率信号由 metadata 承载,raw body 仅作兜底。
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("{}"))
	req := relaycommon.TaskSubmitReq{
		Model:    model,
		Prompt:   "a cat",
		Metadata: metadata,
	}
	c.Set("task_request", req)
	return c, &relaycommon.RelayInfo{OriginModelName: model}
}

func videoContentMeta() map[string]interface{} {
	return map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type":      "video_url",
				"video_url": map[string]interface{}{"url": "http://example.com/in.mp4"},
			},
		},
	}
}

// mini 必须被识别为 seedance2 模型,否则 sora 适配器的 EstimateBilling 不会对它计折扣。
func TestIsSeedance2Model_IncludesMini(t *testing.T) {
	if !IsSeedance2Model(miniModel) {
		t.Fatalf("expected %s to be a seedance2 model", miniModel)
	}
	// 回归:两个哥哥仍在集合内
	for _, m := range []string{"doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128"} {
		if !IsSeedance2Model(m) {
			t.Fatalf("regression: expected %s to remain a seedance2 model", m)
		}
	}
	// 非 seedance2 模型不应命中
	for _, m := range []string{"sora-2", "sora-2-pro", ""} {
		if IsSeedance2Model(m) {
			t.Fatalf("expected %s NOT to be a seedance2 model", m)
		}
	}
}

// mini 含视频输入 → video_pricing 折扣 = 14/23(人民币两价之比,含视频÷不含视频)。
func TestEstimateSeedance2Ratios_MiniWithVideoInput(t *testing.T) {
	c, info := newTaskCtx(miniModel, videoContentMeta())
	ratios := estimateSeedance2Ratios(c, info)
	got, ok := ratios["video_pricing"]
	if !ok {
		t.Fatalf("expected video_pricing ratio for mini with video input, got ratios=%v", ratios)
	}
	if !approx(got, 14.0/23.0) {
		t.Fatalf("video_pricing=%v want %v (=14/23)", got, 14.0/23.0)
	}
}

// mini 无视频输入 → 不产生任何 ratio(返回 nil)。
func TestEstimateSeedance2Ratios_MiniNoVideo(t *testing.T) {
	c, info := newTaskCtx(miniModel, map[string]interface{}{})
	if ratios := estimateSeedance2Ratios(c, info); ratios != nil {
		t.Fatalf("expected nil ratios for mini without video input, got %v", ratios)
	}
}

// mini + 请求 1080p(且含视频)→ 倍率仍为含视频折扣,mini 不支持 1080p 故不额外加价。
func TestEstimateSeedance2Ratios_Mini1080pNoResolutionRatio(t *testing.T) {
	meta := videoContentMeta()
	meta["resolution"] = "1080p"
	c, info := newTaskCtx(miniModel, meta)
	ratios := estimateSeedance2Ratios(c, info)
	if got, ok := ratios["video_pricing"]; !ok || !approx(got, 14.0/23.0) {
		t.Fatalf("expected video_pricing=14/23 for mini with video at 1080p, got %v ok=%v", got, ok)
	}
}
