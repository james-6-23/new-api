package model

import (
	"testing"
)

func mkVersionMap(versions []ChannelCostVersion) VersionMap {
	m := make(VersionMap)
	for _, v := range versions {
		m[v.ChannelId] = append(m[v.ChannelId], v)
	}
	return m
}

func TestVersionMap_NoVersions(t *testing.T) {
	if _, ok := mkVersionMap(nil).RatioAt(1, 1000); ok {
		t.Fatal("want false")
	}
}

func TestVersionMap_SingleRatio(t *testing.T) {
	vm := mkVersionMap([]ChannelCostVersion{
		{ChannelId: 1, EffectiveFrom: 0, CostMode: "ratio", CostRatio: 2.5},
	})
	r, ok := vm.RatioAt(1, 9999999999)
	if !ok || r != 2.5 {
		t.Fatalf("want (2.5,true), got (%v,%v)", r, ok)
	}
}

func TestVersionMap_BeforeFirstVersion(t *testing.T) {
	vm := mkVersionMap([]ChannelCostVersion{
		{ChannelId: 1, EffectiveFrom: 1000, CostMode: "ratio", CostRatio: 2.5},
	})
	if _, ok := vm.RatioAt(1, 500); ok {
		t.Fatal("before first version must return false")
	}
}

func TestVersionMap_CrossVersionBoundary(t *testing.T) {
	vm := mkVersionMap([]ChannelCostVersion{
		{ChannelId: 3, EffectiveFrom: 0, CostMode: "ratio", CostRatio: 2.5},
		{ChannelId: 3, EffectiveFrom: 2000, CostMode: "ratio", CostRatio: 2.3},
	})
	r, ok := vm.RatioAt(3, 1999)
	if !ok || r != 2.5 {
		t.Fatalf("before boundary (ts=1999): want (2.5,true), got (%v,%v)", r, ok)
	}
	r, ok = vm.RatioAt(3, 2000)
	if !ok || r != 2.3 {
		t.Fatalf("at boundary (ts=2000): want (2.3,true), got (%v,%v)", r, ok)
	}
}

func TestVersionMap_DiscountFrozenRate(t *testing.T) {
	vm := mkVersionMap([]ChannelCostVersion{
		{ChannelId: 5, EffectiveFrom: 0, CostMode: "discount", CostDiscount: 0.8, ExchangeRate: 6.8},
	})
	r, ok := vm.RatioAt(5, 9999)
	if !ok {
		t.Fatal("want ok=true")
	}
	want := 0.8 * 6.8
	if d := r - want; d > 1e-9 || d < -1e-9 {
		t.Fatalf("want %v, got %v", want, r)
	}
}

func TestVersionMap_ZeroRatioUnpriced(t *testing.T) {
	if _, ok := mkVersionMap([]ChannelCostVersion{
		{ChannelId: 7, EffectiveFrom: 0, CostMode: "ratio", CostRatio: 0},
	}).RatioAt(7, 9999); ok {
		t.Fatal("zero ratio must return ok=false")
	}
}

// Channel key present but with empty version slice.
// Different from unknown channel ID (which is also absent from map) because it takes
// a different code path: the channel exists as a map key but has no versions to search.
func TestVersionMap_EmptyVersionSlice(t *testing.T) {
	vm := VersionMap{
		9: {}, // channel 9 exists but has no versions
	}
	if _, ok := vm.RatioAt(9, 5000); ok {
		t.Fatal("empty version slice must return ok=false")
	}
}

// VersionAt 返回版本本体，供 addBatch 记录"命中了哪个版本"以判定跨版本。
// 与 RatioAt 必须选中同一个版本——两者共用查找逻辑，此测试锁死这一点。
func TestVersionMap_VersionAt_IdentifiesVersion(t *testing.T) {
	vm := mkVersionMap([]ChannelCostVersion{
		{Id: 1, ChannelId: 3, EffectiveFrom: 0, CostMode: "ratio", CostRatio: 2.5},
		{Id: 2, ChannelId: 3, EffectiveFrom: 2000, CostMode: "ratio", CostRatio: 2.3},
	})
	v1, ok := vm.VersionAt(3, 1999)
	if !ok || v1.Id != 1 {
		t.Fatalf("at 1999: want version id 1, got id=%d ok=%v", v1.Id, ok)
	}
	v2, ok := vm.VersionAt(3, 2000)
	if !ok || v2.Id != 2 {
		t.Fatalf("at 2000: want version id 2, got id=%d ok=%v", v2.Id, ok)
	}
	if _, ok := vm.VersionAt(3, -1); ok {
		t.Fatal("before first version must return ok=false")
	}
}

// EffectiveRatio 是 RatioAt 的底层换算；两条路径必须给出同一个数。
func TestChannelCostVersion_EffectiveRatio(t *testing.T) {
	ratioVer := ChannelCostVersion{CostMode: "ratio", CostRatio: 2.5}
	if r, ok := ratioVer.EffectiveRatio(); !ok || r != 2.5 {
		t.Fatalf("ratio mode: want (2.5,true), got (%v,%v)", r, ok)
	}
	discVer := ChannelCostVersion{CostMode: "discount", CostDiscount: 0.8, ExchangeRate: 6.8}
	r, ok := discVer.EffectiveRatio()
	if !ok {
		t.Fatal("discount mode: want ok=true")
	}
	if d := r - 0.8*6.8; d > 1e-9 || d < -1e-9 {
		t.Fatalf("discount mode: want %v, got %v", 0.8*6.8, r)
	}
	// discount 模式但汇率为 0 → 无法定价
	if _, ok := (ChannelCostVersion{
		CostMode: "discount", CostDiscount: 0.8, ExchangeRate: 0,
	}).EffectiveRatio(); ok {
		t.Fatal("discount with zero exchange rate must return ok=false")
	}
}
