package billingexpr

import "testing"

func TestParseTiers_SingleTier(t *testing.T) {
	tiers := ParseTiers(`tier("base", p * 2.5 + c * 15 + cr * 0.25)`)
	if len(tiers) != 1 {
		t.Fatalf("tiers = %d, want 1", len(tiers))
	}
	tt := tiers[0]
	if tt.Label != "base" || tt.Prices["p"] != 2.5 || tt.Prices["c"] != 15 || tt.Prices["cr"] != 0.25 {
		t.Fatalf("unexpected tier: %+v", tt)
	}
}

func TestParseTiers_MultiTierWithConditionsVersionAndRules(t *testing.T) {
	expr := `v1:len <= 200000
  ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)
  : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)|||when(header("x-b") has "fast") * 6`
	tiers := ParseTiers(expr)
	if len(tiers) != 2 {
		t.Fatalf("tiers = %d, want 2", len(tiers))
	}
	if tiers[0].Label != "standard" || tiers[0].Prices["p"] != 3 || tiers[0].Prices["cc1h"] != 6 {
		t.Fatalf("tier0: %+v", tiers[0])
	}
	if tiers[1].Label != "long_context" || tiers[1].Prices["p"] != 6 || tiers[1].Prices["cc"] != 7.5 {
		t.Fatalf("tier1: %+v", tiers[1])
	}
}

func TestParseTiers_MediaVarsAndFirstCoefficientWins(t *testing.T) {
	tiers := ParseTiers(`tier("base", p * 0.43 + c * 3.06 + img * 0.78 + img_o * 1.5 + ai * 3.81 + ao * 15.11 + p * 99)`)
	if len(tiers) != 1 {
		t.Fatalf("tiers = %d, want 1", len(tiers))
	}
	pr := tiers[0].Prices
	if pr["p"] != 0.43 || pr["img"] != 0.78 || pr["img_o"] != 1.5 || pr["ai"] != 3.81 || pr["ao"] != 15.11 {
		t.Fatalf("prices: %+v (first coefficient must win, img/img_o distinct)", pr)
	}
}

func TestParseTiers_InvalidOrEmpty(t *testing.T) {
	if got := ParseTiers(""); len(got) != 0 {
		t.Fatalf("empty expr → %d tiers, want 0", len(got))
	}
	if got := ParseTiers("p * 3 + c * 15"); len(got) != 0 {
		t.Fatalf("no tier() call → %d tiers, want 0", len(got))
	}
}

func TestMatchTier(t *testing.T) {
	tiers := ParseTiers(`len <= 100 ? tier("a", p * 1 + c * 2) : tier("b", p * 3 + c * 4)`)
	if m := MatchTier(tiers, "b"); m == nil || m.Label != "b" {
		t.Fatalf("MatchTier b → %+v", m)
	}
	if m := MatchTier(tiers, ""); m == nil || m.Label != "a" {
		t.Fatalf("MatchTier empty → %+v, want first tier", m)
	}
	if m := MatchTier(tiers, "nope"); m == nil || m.Label != "a" {
		t.Fatalf("MatchTier miss → %+v, want first tier", m)
	}
	if m := MatchTier(nil, "a"); m != nil {
		t.Fatalf("MatchTier(nil) → %+v, want nil", m)
	}
}
