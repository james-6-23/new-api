package common

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetRandomStringWithCharset_LengthZeroReturnsEmpty(t *testing.T) {
	require.Equal(t, "", GetRandomStringWithCharset(0, "alphanumeric"))
	require.Equal(t, "", GetRandomStringWithCharset(-1, "digits"))
}

func TestGetRandomStringWithCharset_Charsets(t *testing.T) {
	alphanumericRe := regexp.MustCompile(`^[A-Za-z0-9]+$`)
	digitsRe := regexp.MustCompile(`^[0-9]+$`)
	lettersRe := regexp.MustCompile(`^[A-Za-z]+$`)

	tests := []struct {
		name    string
		charset string
		matcher *regexp.Regexp
	}{
		{name: "alphanumeric", charset: "alphanumeric", matcher: alphanumericRe},
		{name: "digits", charset: "digits", matcher: digitsRe},
		{name: "letters", charset: "letters", matcher: lettersRe},
		{name: "unknown falls back to alphanumeric", charset: "weirdo", matcher: alphanumericRe},
		{name: "empty string falls back to alphanumeric", charset: "", matcher: alphanumericRe},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Drive enough iterations to make the charset constraint observable in practice
			// (16 chars × 500 iters = 8000 chars; with the digits-only case, any letter would fail).
			for i := 0; i < 500; i++ {
				got := GetRandomStringWithCharset(16, tt.charset)
				require.Len(t, got, 16, "iter=%d charset=%q", i, tt.charset)
				require.True(t,
					tt.matcher.MatchString(got),
					"iter=%d charset=%q got=%q (expected to match %s)",
					i, tt.charset, got, tt.matcher.String(),
				)
			}
		})
	}
}

func TestGetRandomStringWithCharset_DigitsExcludeLetters(t *testing.T) {
	// Concrete refutation: collect all chars seen across many iterations of "digits"
	// and assert no letters slipped through. This guards against a future refactor
	// that accidentally widens the digits charset.
	seen := make(map[rune]struct{})
	for i := 0; i < 200; i++ {
		for _, r := range GetRandomStringWithCharset(8, "digits") {
			seen[r] = struct{}{}
		}
	}
	for r := range seen {
		require.True(t, r >= '0' && r <= '9',
			"digits charset produced non-digit rune %q", string(r))
	}
}

func TestGetRandomStringWithCharset_LettersExcludeDigits(t *testing.T) {
	seen := make(map[rune]struct{})
	for i := 0; i < 200; i++ {
		for _, r := range GetRandomStringWithCharset(8, "letters") {
			seen[r] = struct{}{}
		}
	}
	for r := range seen {
		require.True(t,
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'),
			"letters charset produced non-letter rune %q", string(r))
	}
}

func TestGetRandomString_StillAlphanumeric(t *testing.T) {
	// Regression guard: extending common/str.go with the new helper must NOT change
	// the existing GetRandomString contract that callers rely on (e.g. AffCode).
	got := GetRandomString(32)
	require.Len(t, got, 32)
	require.True(t, regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(got),
		"GetRandomString produced non-alphanumeric output %q", got)
	// Sanity: zero length still returns empty.
	require.Equal(t, "", GetRandomString(0))
	// Sanity: not just one repeated char across 1000 calls (probabilistic — see below).
	uniq := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		uniq[GetRandomString(4)] = struct{}{}
	}
	require.Greater(t, len(uniq), 100,
		"GetRandomString appears to be returning duplicates suspiciously often: only %d unique",
		len(uniq))
	_ = strings.ToLower // keep "strings" import used even if future refactors drop it
}
