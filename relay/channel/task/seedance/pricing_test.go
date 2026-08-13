package seedance

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestIsSeedance2(t *testing.T) {
	for _, m := range []string{
		"dreamina-seedance-2-0-260128",
		"dreamina-seedance-2-0-fast-260128",
		"dreamina-seedance-2-0-mini-260615",
		"doubao-seedance-2-0-260128",
		"doubao-seedance-2-0-fast-260128",
		"doubao-seedance-2-0-mini-260615",
		"dreamina-seedance-2-5-260628",
		"doubao-seedance-2-5-260628",
	} {
		if !IsSeedance2(m) {
			t.Fatalf("expected %s to be seedance2", m)
		}
	}
	for _, m := range []string{"sora-2", "sora-2-pro", "doubao-seedance-1-0-pro-250528", ""} {
		if IsSeedance2(m) {
			t.Fatalf("expected %s NOT to be seedance2", m)
		}
	}
}

func TestClassifyResTier(t *testing.T) {
	cases := map[string]string{
		"480p": "base", "720p": "base", "1280x720": "base", "": "base",
		"1080p": "1080p", "1080P": "1080p", "1920x1080": "1080p",
		"4k": "4k", "4K": "4k", "2160p": "4k", "3840x2160": "4k",
	}
	for in, want := range cases {
		if got := ClassifyResTier(in); got != want {
			t.Fatalf("ClassifyResTier(%q)=%q want %q", in, got, want)
		}
	}
}

// TestDreaminaCellUnit 覆盖海外 USD 矩阵(移植自原 doubao 包测试)。
func TestDreaminaCellUnit(t *testing.T) {
	type c struct {
		model    string
		tier     string
		hasVideo bool
		want     float64
	}
	cases := []c{
		{"dreamina-seedance-2-0-260128", "base", false, 7.0},
		{"dreamina-seedance-2-0-260128", "base", true, 4.3},
		{"dreamina-seedance-2-0-260128", "1080p", false, 7.7},
		{"dreamina-seedance-2-0-260128", "1080p", true, 4.7},
		{"dreamina-seedance-2-0-260128", "4k", false, 4.0},
		{"dreamina-seedance-2-0-260128", "4k", true, 2.4},
		// fast/mini 不支持 1080p/4k，回退到 base
		{"dreamina-seedance-2-0-fast-260128", "base", false, 5.6},
		{"dreamina-seedance-2-0-fast-260128", "1080p", false, 5.6},
		{"dreamina-seedance-2-0-mini-260615", "base", true, 2.1},
		{"dreamina-seedance-2-0-mini-260615", "1080p", true, 2.1},
		// 2.5 仅 480p/720p:1080p/4k 均回退 base
		{"dreamina-seedance-2-5-260628", "base", false, 10.7},
		{"dreamina-seedance-2-5-260628", "base", true, 6.4},
		{"dreamina-seedance-2-5-260628", "1080p", true, 6.4},
		{"dreamina-seedance-2-5-260628", "4k", false, 10.7},
	}
	for _, tc := range cases {
		got, _, ok := CellUnit(tc.model, tc.tier, tc.hasVideo)
		if !ok {
			t.Fatalf("unexpected miss for %+v", tc)
		}
		if !approx(got, tc.want) {
			t.Fatalf("unit(%+v)=%v want %v", tc, got, tc.want)
		}
	}
	if _, _, ok := CellUnit("sora-2", "base", false); ok {
		t.Fatalf("expected miss for non-seedance2 model")
	}
}

// TestDoubaoRatioEquivalence 是本次对齐的核心回归:验证国内 doubao-* 从
// 「video_input × resolution 两倍率相乘」切换到「单一 video_pricing」后,
// 每个(档位, 含视频)组合的有效倍率与旧实现完全一致。
func TestDoubaoRatioEquivalence(t *testing.T) {
	type c struct {
		model    string
		tier     string
		hasVideo bool
		want     float64 // 旧 sora 实现的等效最终倍率
	}
	cases := []c{
		// 260128: 旧 video_input=28/46, resolution(no-video)=51/46, resolution(video)=31/28
		{"doubao-seedance-2-0-260128", "base", false, 1.0},        // 无倍率
		{"doubao-seedance-2-0-260128", "base", true, 28.0 / 46.0}, // video_input
		{"doubao-seedance-2-0-260128", "1080p", false, 51.0 / 46.0},
		{"doubao-seedance-2-0-260128", "1080p", true, (28.0 / 46.0) * (31.0 / 28.0)}, // = 31/46
		// fast: 旧 video_input=22/37, 无 1080p 表(1080p 回退 base)
		{"doubao-seedance-2-0-fast-260128", "base", false, 1.0},
		{"doubao-seedance-2-0-fast-260128", "base", true, 22.0 / 37.0},
		{"doubao-seedance-2-0-fast-260128", "1080p", false, 1.0},
		{"doubao-seedance-2-0-fast-260128", "1080p", true, 22.0 / 37.0},
		// mini: 官方价 不含视频 23 / 含视频 14, 不支持 1080p(回退 base)
		{"doubao-seedance-2-0-mini-260615", "base", false, 1.0},
		{"doubao-seedance-2-0-mini-260615", "base", true, 14.0 / 23.0},
		{"doubao-seedance-2-0-mini-260615", "1080p", false, 1.0},
		{"doubao-seedance-2-0-mini-260615", "1080p", true, 14.0 / 23.0},
		// 2.5: 官方价 不含视频 70 / 含视频 42,不支持 1080p/4k(回退 base)
		{"doubao-seedance-2-5-260628", "base", false, 1.0},
		{"doubao-seedance-2-5-260628", "base", true, 42.0 / 70.0},
		{"doubao-seedance-2-5-260628", "1080p", false, 1.0},
		{"doubao-seedance-2-5-260628", "1080p", true, 42.0 / 70.0},
		{"doubao-seedance-2-5-260628", "4k", true, 42.0 / 70.0},
	}
	for _, tc := range cases {
		ratio, _, ok := PricingRatio(tc.model, tc.tier, tc.hasVideo)
		if !ok {
			t.Fatalf("unexpected miss for %+v", tc)
		}
		if !approx(ratio, tc.want) {
			t.Fatalf("PricingRatio(%+v)=%v want %v", tc, ratio, tc.want)
		}
	}
}

// TestBaseRatioIsOne 每个模型 base 档不含视频的相对倍率必须为 1.0。
func TestBaseRatioIsOne(t *testing.T) {
	for m := range unitPrice {
		r, _, ok := PricingRatio(m, "base", false)
		if !ok || !approx(r, 1.0) {
			t.Fatalf("model %s base/no-video ratio=%v ok=%v want 1.0", m, r, ok)
		}
	}
}

// TestDreaminaPricingRatioSample 抽查海外矩阵单一倍率与基准。
func TestDreaminaPricingRatioSample(t *testing.T) {
	r, base, ok := PricingRatio("dreamina-seedance-2-0-260128", "1080p", true)
	if !ok || !approx(base, 7.0) || !approx(r, 4.7/7.0) {
		t.Fatalf("ratio=%v base=%v ok=%v", r, base, ok)
	}
}
