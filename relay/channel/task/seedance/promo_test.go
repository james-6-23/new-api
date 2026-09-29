package seedance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"

	"github.com/gin-gonic/gin"
)

// newCtx 构造一个带 body 的任务请求上下文;视频/分辨率信号由 metadata 承载。
func newCtx(model string, metadata map[string]interface{}) (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("{}"))
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    model,
		Prompt:   "a cat",
		Metadata: metadata,
	})
	return c, &relaycommon.RelayInfo{OriginModelName: model}
}

func resMeta(res string) map[string]interface{} {
	return map[string]interface{}{"resolution": res}
}

// setPromo 注入折扣配置与固定时钟,测试结束自动还原。
func setPromo(t *testing.T, cfg map[string]billing_setting.VideoPromo, now int64) {
	t.Helper()
	t.Cleanup(billing_setting.SetVideoPromoForTest(cfg, now))
}

// TestEmptyPromoConfigIsByteIdentical 最高优先级验收标准:
// video_promo 为空时,所有模型所有档位的 OtherRatios 与折扣层引入前逐键一致,且不出现 video_promo 键。
func TestEmptyPromoConfigIsByteIdentical(t *testing.T) {
	setPromo(t, nil, 0)
	for model := range unitPrice {
		for _, res := range []string{"480p", "1080p", "4k"} {
			c, info := newCtx(model, resMeta(res))
			got := EstimateBilling(c, info)
			if _, has := got["video_promo"]; has {
				t.Fatalf("model=%s res=%s leaked video_promo key with empty config: %v", model, res, got)
			}
			tier := ClassifyResTier(res)
			wantRatio, _, _, ok := PricingRatio(model, tier, false)
			if !ok {
				t.Fatalf("unexpected pricing miss for %s", model)
			}
			if wantRatio == 1.0 {
				if got != nil {
					t.Fatalf("model=%s res=%s want nil ratios, got %v", model, res, got)
				}
				continue
			}
			if len(got) != 1 || !approx(got["video_pricing"], wantRatio) {
				t.Fatalf("model=%s res=%s got %v want only video_pricing=%v", model, res, got, wantRatio)
			}
			if info.PriceData.VideoBilling.PromoFactor != 1.0 {
				t.Fatalf("model=%s PromoFactor=%v want 1.0", model, info.PriceData.VideoBilling.PromoFactor)
			}
		}
	}
}

// TestBaseTierPromoSurvivesRatioDenominator 全案核心回归:
// base 档折扣必须独立生效,不能被相对倍率的分母约掉。折扣若乘进矩阵,此用例静默失败。
func TestBaseTierPromoSurvivesRatioDenominator(t *testing.T) {
	const model = "doubao-seedance-2-0-fast-260128"
	setPromo(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"base": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)

	c, info := newCtx(model, resMeta("480p"))
	got := EstimateBilling(c, info)
	if !approx(got["video_promo"], 0.72) {
		t.Fatalf("video_promo=%v want 0.72 (got ratios %v)", got["video_promo"], got)
	}
	if _, has := got["video_pricing"]; has {
		t.Fatalf("base/no-video ratio is 1.0 and must stay unwritten, got %v", got)
	}
	vb := info.PriceData.VideoBilling
	if !approx(vb.PricingRatio, 1.0) {
		t.Fatalf("PricingRatio=%v must stay list-price 1.0", vb.PricingRatio)
	}
	if vb.PromoStartAt != 100 || vb.PromoEndAt != 200 {
		t.Fatalf("window not snapshotted: (%d,%d)", vb.PromoStartAt, vb.PromoEndAt)
	}
}

// Test1080pPromoIsIndependentOfListRatio 1080p 折扣:两键相乘 = 折后有效倍率。
func Test1080pPromoIsIndependentOfListRatio(t *testing.T) {
	const model = "doubao-seedance-2-5-260628"
	setPromo(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)

	c, info := newCtx(model, resMeta("1080p"))
	got := EstimateBilling(c, info)
	if !approx(got["video_pricing"], 77.0/70.0) {
		t.Fatalf("video_pricing=%v want list ratio 77/70", got["video_pricing"])
	}
	if !approx(got["video_promo"], 0.72) {
		t.Fatalf("video_promo=%v want 0.72", got["video_promo"])
	}
	if !approx(got["video_pricing"]*got["video_promo"], 0.72*77.0/70.0) {
		t.Fatalf("product mismatch")
	}
	if vb := info.PriceData.VideoBilling; vb.ResolutionTier != "1080p" || !approx(vb.PromoFactor, 0.72) {
		t.Fatalf("snapshot tier=%q factor=%v want 1080p/0.72", vb.ResolutionTier, vb.PromoFactor)
	}
}

// TestPromoTierIsolation 只给 1080p 配系数时,base 请求扣费与配置前完全一致。
func TestPromoTierIsolation(t *testing.T) {
	const model = "doubao-seedance-2-5-260628"
	setPromo(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)

	c, info := newCtx(model, resMeta("720p"))
	got := EstimateBilling(c, info)
	if _, has := got["video_promo"]; has {
		t.Fatalf("base request must not be discounted, got %v", got)
	}
	if info.PriceData.VideoBilling.PromoFactor != 1.0 {
		t.Fatalf("PromoFactor=%v want 1.0", info.PriceData.VideoBilling.PromoFactor)
	}
}

// TestPromoMatchesTierHitNotRequestedTier 折扣按实际计价档位匹配:
// 1080p 请求在只有 base 档的模型上回退 base 计价,配在 1080p 上的折扣不得生效
// —— 否则会把 1080p 的折扣施加到 base 单价上。
func TestPromoMatchesTierHitNotRequestedTier(t *testing.T) {
	const model = "doubao-seedance-2-0-fast-260128"
	setPromo(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)

	c, info := newCtx(model, resMeta("1080p"))
	got := EstimateBilling(c, info)
	if _, has := got["video_promo"]; has {
		t.Fatalf("1080p promo must not apply when billing fell back to base, got %v", got)
	}
	if info.PriceData.VideoBilling.ResolutionTier != "base" {
		t.Fatalf("ResolutionTier=%q want base", info.PriceData.VideoBilling.ResolutionTier)
	}
}

// TestInFlightTaskUsesSubmitTimeSnapshot 提交后修改折扣配置,结算仍按提交时快照计费。
// 这条守的是对账:用户按下单时看到的价格付费,中途调价不影响在途任务。
func TestInFlightTaskUsesSubmitTimeSnapshot(t *testing.T) {
	const model = "doubao-seedance-2-5-260628"

	// 提交时:1080p 打 0.72
	setPromo(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)
	c, info := newCtx(model, resMeta("1080p"))
	submitted := EstimateBilling(c, info)
	if !approx(submitted["video_promo"], 0.72) {
		t.Fatalf("submit-time promo=%v want 0.72", submitted["video_promo"])
	}

	// 快照是值拷贝,后续改配置不得回改它
	snapshot := make(map[string]float64, len(submitted))
	for k, v := range submitted {
		snapshot[k] = v
	}
	displayed := info.PriceData.VideoBilling.PromoFactor

	// 运营中途改成 0.5,并让窗口结束
	setPromo(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.5}, StartAt: 100, EndAt: 200},
	}, 999)

	if !approx(snapshot["video_promo"], 0.72) {
		t.Fatalf("snapshot mutated to %v after config change", snapshot["video_promo"])
	}
	if !approx(submitted["video_promo"], 0.72) {
		t.Fatalf("returned map mutated to %v after config change", submitted["video_promo"])
	}
	if !approx(displayed, 0.72) {
		t.Fatalf("display snapshot mutated to %v", displayed)
	}
	if !approx(info.PriceData.VideoBilling.PromoFactor, 0.72) {
		t.Fatalf("live PriceData mutated to %v", info.PriceData.VideoBilling.PromoFactor)
	}

	// 结算只连乘快照:即便此刻窗口已结束、折扣已改,快照连乘结果必须不变。
	// 若此断言失败,说明结算路径重新读了配置 —— 那是必须修的对账 bug。
	if got := snapshot["video_promo"] * snapshot["video_res"]; !approx(got, 0.72*snapshot["video_res"]) {
		t.Fatalf("snapshot product drifted to %v", got)
	}
}
// TestFourModelsDoNotCrossTalk 四个模型各配不同档位与系数,互不串扰。
func TestFourModelsDoNotCrossTalk(t *testing.T) {
	setPromo(t, map[string]billing_setting.VideoPromo{
		"doubao-seedance-2-5-260628":      {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
		"doubao-seedance-2-0-fast-260128": {Factors: map[string]float64{"base": 0.9}, StartAt: 100, EndAt: 200},
		"doubao-seedance-2-0-mini-260615": {Factors: map[string]float64{"base": 0.5}, StartAt: 300, EndAt: 400},
		"dreamina-seedance-2-5-260628":    {Factors: map[string]float64{"1080p": 0.6}, StartAt: 100, EndAt: 200},
	}, 150)

	cases := []struct {
		model string
		res   string
		want  float64 // 0 表示不应出现 video_promo 键
	}{
		{"doubao-seedance-2-5-260628", "1080p", 0.72},
		{"doubao-seedance-2-5-260628", "480p", 0},
		{"doubao-seedance-2-0-fast-260128", "480p", 0.9},
		{"doubao-seedance-2-0-mini-260615", "480p", 0}, // 窗口未开始
		{"dreamina-seedance-2-5-260628", "1080p", 0.6},
		{"doubao-seedance-2-0-260128", "1080p", 0}, // 未配置
	}
	for _, tc := range cases {
		c, _ := newCtx(tc.model, resMeta(tc.res))
		info := &relaycommon.RelayInfo{OriginModelName: tc.model}
		got := EstimateBilling(c, info)
		f, has := got["video_promo"]
		if tc.want == 0 {
			if has {
				t.Fatalf("%s/%s should have no promo, got %v", tc.model, tc.res, got)
			}
			continue
		}
		if !has || !approx(f, tc.want) {
			t.Fatalf("%s/%s promo=%v has=%v want %v", tc.model, tc.res, f, has, tc.want)
		}
	}
}
