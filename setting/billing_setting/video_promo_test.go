package billing_setting

import "testing"

// withPromo 临时替换折扣配置与时钟,返回还原函数。
func withPromo(t *testing.T, cfg map[string]VideoPromo, now int64) {
	t.Helper()
	oldCfg, oldNow := billingSetting.VideoPromo, nowFn
	billingSetting.VideoPromo = cfg
	nowFn = func() int64 { return now }
	t.Cleanup(func() {
		billingSetting.VideoPromo = oldCfg
		nowFn = oldNow
	})
}

const (
	winStart = int64(1_800_000_000)
	winEnd   = int64(1_800_003_600)
)

func onePromo(factors map[string]float64, start, end int64) map[string]VideoPromo {
	return map[string]VideoPromo{"m": {Factors: factors, StartAt: start, EndAt: end}}
}

// TestWindowIsClosedInterval 闭区间五点:start-1 不折、start 折、区间中折、end 折、end+1 不折。
func TestWindowIsClosedInterval(t *testing.T) {
	cases := []struct {
		now  int64
		want float64
	}{
		{winStart - 1, 1.0},
		{winStart, 0.5},
		{winStart + 1800, 0.5},
		{winEnd, 0.5},
		{winEnd + 1, 1.0},
	}
	for _, tc := range cases {
		withPromo(t, onePromo(map[string]float64{"base": 0.5}, winStart, winEnd), tc.now)
		got, _, _ := ResolveVideoPromo("m", "base")
		if got != tc.want {
			t.Fatalf("now=%d factor=%v want %v", tc.now, got, tc.want)
		}
	}
}

// TestSecondPrecision 窗口 10:00:00~10:00:59,10:00:30 命中、10:01:00 不命中。
func TestSecondPrecision(t *testing.T) {
	s, e := int64(1_800_000_000), int64(1_800_000_059)
	withPromo(t, onePromo(map[string]float64{"base": 0.72}, s, e), s+30)
	if f, _, _ := ResolveVideoPromo("m", "base"); f != 0.72 {
		t.Fatalf("mid-window factor=%v want 0.72", f)
	}
	withPromo(t, onePromo(map[string]float64{"base": 0.72}, s, e), s+60)
	if f, _, _ := ResolveVideoPromo("m", "base"); f != 1.0 {
		t.Fatalf("after-window factor=%v want 1.0", f)
	}
}

// TestBadConfigFallsBackToListPrice 坏值一律降级到原价,且不 panic。
func TestBadConfigFallsBackToListPrice(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]VideoPromo
	}{
		{"factor zero", onePromo(map[string]float64{"base": 0}, winStart, winEnd)},
		{"factor negative", onePromo(map[string]float64{"base": -0.5}, winStart, winEnd)},
		{"factor above one", onePromo(map[string]float64{"base": 1.5}, winStart, winEnd)},
		{"nil factors", onePromo(nil, winStart, winEnd)},
		{"empty factors", onePromo(map[string]float64{}, winStart, winEnd)},
		{"missing start", onePromo(map[string]float64{"base": 0.5}, 0, winEnd)},
		{"missing end", onePromo(map[string]float64{"base": 0.5}, winStart, 0)},
		{"start equals end", onePromo(map[string]float64{"base": 0.5}, winStart, winStart)},
		{"start after end", onePromo(map[string]float64{"base": 0.5}, winEnd, winStart)},
	}
	for _, tc := range cases {
		withPromo(t, tc.cfg, winStart+10)
		f, s, e := ResolveVideoPromo("m", "base")
		if f != 1.0 || s != 0 || e != 0 {
			t.Fatalf("%s: got (%v,%d,%d) want (1,0,0)", tc.name, f, s, e)
		}
	}
}

// TestDegradationGranularity 坏系数只废该档位;好档位不受牵连。
func TestDegradationGranularity(t *testing.T) {
	withPromo(t, onePromo(map[string]float64{"base": 5.0, "1080p": 0.72}, winStart, winEnd), winStart+10)
	if f, _, _ := ResolveVideoPromo("m", "base"); f != 1.0 {
		t.Fatalf("bad base factor should degrade, got %v", f)
	}
	if f, _, _ := ResolveVideoPromo("m", "1080p"); f != 0.72 {
		t.Fatalf("good 1080p factor should survive, got %v", f)
	}
}

// TestUnconfiguredModelAndTier 未配置的模型/档位无折扣。
func TestUnconfiguredModelAndTier(t *testing.T) {
	withPromo(t, onePromo(map[string]float64{"1080p": 0.72}, winStart, winEnd), winStart+10)
	if f, _, _ := ResolveVideoPromo("other-model", "1080p"); f != 1.0 {
		t.Fatalf("unconfigured model should not discount")
	}
	if f, _, _ := ResolveVideoPromo("m", "base"); f != 1.0 {
		t.Fatalf("unconfigured tier should not discount")
	}
}

// TestEmptyConfigIsNoOp 空配置下任何查询都返回不打折 —— 对应最高优先级验收标准。
func TestEmptyConfigIsNoOp(t *testing.T) {
	withPromo(t, map[string]VideoPromo{}, winStart+10)
	for _, tier := range []string{"base", "1080p", "4k", ""} {
		if f, s, e := ResolveVideoPromo("m", tier); f != 1.0 || s != 0 || e != 0 {
			t.Fatalf("empty config tier=%q got (%v,%d,%d) want (1,0,0)", tier, f, s, e)
		}
	}
}

// TestWindowsAreIndependentPerModel A 进行中、B 未开始、C 已结束同时成立时只有 A 打折。
func TestWindowsAreIndependentPerModel(t *testing.T) {
	now := winStart + 10
	withPromo(t, map[string]VideoPromo{
		"a": {Factors: map[string]float64{"base": 0.8}, StartAt: winStart, EndAt: winEnd},
		"b": {Factors: map[string]float64{"base": 0.8}, StartAt: now + 100, EndAt: now + 200},
		"c": {Factors: map[string]float64{"base": 0.8}, StartAt: now - 200, EndAt: now - 100},
	}, now)
	if f, _, _ := ResolveVideoPromo("a", "base"); f != 0.8 {
		t.Fatalf("a should be active, got %v", f)
	}
	if f, _, _ := ResolveVideoPromo("b", "base"); f != 1.0 {
		t.Fatalf("b not started yet, got %v", f)
	}
	if f, _, _ := ResolveVideoPromo("c", "base"); f != 1.0 {
		t.Fatalf("c already ended, got %v", f)
	}
}

// TestResolveReturnsWindowForAudit 命中时必须回传窗口,供账单自证。
func TestResolveReturnsWindowForAudit(t *testing.T) {
	withPromo(t, onePromo(map[string]float64{"base": 0.72}, winStart, winEnd), winStart+10)
	f, s, e := ResolveVideoPromo("m", "base")
	if f != 0.72 || s != winStart || e != winEnd {
		t.Fatalf("got (%v,%d,%d) want (0.72,%d,%d)", f, s, e, winStart, winEnd)
	}
}

func TestValidateVideoPromo(t *testing.T) {
	ok := []string{
		``, `{}`,
		`{"m":{"factors":{"1080p":0.72},"start_at":1800000000,"end_at":1800003600}}`,
		`{"m":{"factors":{"base":1},"start_at":1,"end_at":2}}`,
		`{"m":{"factors":{},"start_at":0,"end_at":0}}`,
	}
	for _, s := range ok {
		if err := ValidateVideoPromo(s); err != nil {
			t.Fatalf("ValidateVideoPromo(%s) unexpected err: %v", s, err)
		}
	}
	bad := []string{
		`not json`,
		`{"m":{"factors":{"base":0},"start_at":1,"end_at":2}}`,
		`{"m":{"factors":{"base":1.5},"start_at":1,"end_at":2}}`,
		`{"m":{"factors":{"base":-1},"start_at":1,"end_at":2}}`,
		`{"m":{"factors":{"base":0.5},"start_at":0,"end_at":2}}`,
		`{"m":{"factors":{"base":0.5},"start_at":1,"end_at":0}}`,
		`{"m":{"factors":{"base":0.5},"start_at":2,"end_at":1}}`,
		`{"m":{"factors":{"base":0.5},"start_at":1,"end_at":1}}`,
	}
	for _, s := range bad {
		if err := ValidateVideoPromo(s); err == nil {
			t.Fatalf("ValidateVideoPromo(%s) expected error, got nil", s)
		}
	}
}

