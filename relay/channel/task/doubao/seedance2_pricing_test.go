package doubao

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestIsDreaminaSeedance2(t *testing.T) {
	for _, m := range []string{
		"dreamina-seedance-2-0-260128",
		"dreamina-seedance-2-0-fast-260128",
		"dreamina-seedance-2-0-mini-260615",
	} {
		if !IsDreaminaSeedance2(m) {
			t.Fatalf("expected %s to be dreamina seedance2", m)
		}
	}
	for _, m := range []string{"doubao-seedance-2-0-260128", "sora-2", ""} {
		if IsDreaminaSeedance2(m) {
			t.Fatalf("expected %s NOT to be dreamina seedance2", m)
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

func TestDreaminaCellUnitUSD(t *testing.T) {
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
		{"dreamina-seedance-2-0-fast-260128", "base", true, 3.3},
		{"dreamina-seedance-2-0-fast-260128", "1080p", false, 5.6},
		{"dreamina-seedance-2-0-fast-260128", "4k", true, 3.3},
		{"dreamina-seedance-2-0-mini-260615", "base", false, 3.5},
		{"dreamina-seedance-2-0-mini-260615", "base", true, 2.1},
		{"dreamina-seedance-2-0-mini-260615", "1080p", true, 2.1},
	}
	for _, tc := range cases {
		got, _, ok := DreaminaCellUnitUSD(tc.model, tc.tier, tc.hasVideo)
		if !ok {
			t.Fatalf("unexpected miss for %+v", tc)
		}
		if !approx(got, tc.want) {
			t.Fatalf("unit(%+v)=%v want %v", tc, got, tc.want)
		}
	}
	if _, _, ok := DreaminaCellUnitUSD("sora-2", "base", false); ok {
		t.Fatalf("expected miss for non-dreamina model")
	}
}

func TestDreaminaPricingRatio(t *testing.T) {
	r, base, ok := DreaminaPricingRatio("dreamina-seedance-2-0-260128", "1080p", true)
	if !ok || !approx(base, 7.0) || !approx(r, 4.7/7.0) {
		t.Fatalf("ratio=%v base=%v ok=%v", r, base, ok)
	}
	// 基准格 ratio == 1
	r0, _, _ := DreaminaPricingRatio("dreamina-seedance-2-0-mini-260615", "base", false)
	if !approx(r0, 1.0) {
		t.Fatalf("base ratio=%v want 1", r0)
	}
}
