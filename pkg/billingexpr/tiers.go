package billingexpr

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedTier is one tier("label", body) segment of a billing expression with
// its per-variable $/1M prices. Coefficients are real published prices
// (expr.md principle 3) — callers must NOT apply the legacy ×2 convention.
//
// The parsing grammar mirrors parseTiersFromExpr in
// web/default/src/features/pricing/lib/billing-expr.ts — keep both in sync.
type ParsedTier struct {
	Label  string
	Prices map[string]float64 // keys: p c cr cc cc1h img img_o ai ao
}

var (
	exprVersionRe = regexp.MustCompile(`^v(\d+):`)
	// Optional "cond ? " prefix followed by tier("label", body).
	tierRe = regexp.MustCompile(`(?:((?:(?:p|c|len)\s*(?:<=|>=|<|>)\s*[\d.eE+]+)(?:\s*&&\s*(?:p|c|len)\s*(?:<=|>=|<|>)\s*[\d.eE+]+)*)\s*\?\s*)?tier\("([^"]*)",\s*([^)]+)\)`)
	// var * coefficient pairs inside a tier body.
	tierCoefRe = regexp.MustCompile(`\b(p|c|cr|cc1h|cc|img_o|img|ai|ao)\s*\*\s*([\d.eE+-]+)`)
)

// ParseTiers extracts the tier segments of a billing expression. The request
// rule tail (after "|||") and the version prefix ("v1:") are ignored. Returns
// nil when the expression contains no tier() call.
func ParseTiers(exprStr string) []ParsedTier {
	body := strings.TrimSpace(exprStr)
	if body == "" {
		return nil
	}
	body = exprVersionRe.ReplaceAllString(body, "")
	if idx := strings.Index(body, "|||"); idx >= 0 {
		body = body[:idx]
	}
	var tiers []ParsedTier
	for _, m := range tierRe.FindAllStringSubmatch(body, -1) {
		tier := ParsedTier{Label: m[2], Prices: make(map[string]float64)}
		for _, cm := range tierCoefRe.FindAllStringSubmatch(m[3], -1) {
			if _, seen := tier.Prices[cm[1]]; seen {
				continue // first coefficient wins, matching the frontend parser
			}
			if v, err := strconv.ParseFloat(cm[2], 64); err == nil {
				tier.Prices[cm[1]] = v
			}
		}
		tiers = append(tiers, tier)
	}
	return tiers
}

// MatchTier returns the tier whose label equals the recorded matched_tier,
// falling back to the first tier when the label is empty or unknown.
func MatchTier(tiers []ParsedTier, label string) *ParsedTier {
	if len(tiers) == 0 {
		return nil
	}
	if label != "" {
		for i := range tiers {
			if tiers[i].Label == label {
				return &tiers[i]
			}
		}
	}
	return &tiers[0]
}
