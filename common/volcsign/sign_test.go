package volcsign

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"
)

// fixedTime is a deterministic timestamp so signatures are reproducible.
var fixedTime = time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)

func newReq(t *testing.T, method, rawURL string, body []byte) *http.Request {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

func TestSignAt_SetsExpectedHeaders(t *testing.T) {
	body := []byte(`{"GroupId":"group-1","URL":"https://example.com/i.jpg","AssetType":"Image"}`)
	req := newReq(t, http.MethodPost, "https://ark.ap-southeast-1.byteplusapi.com/?Action=CreateAsset&Version=2024-01-01", body)
	req.Header.Set("Content-Type", "application/json")

	if err := signAt(req, body, "AKxxxx", "SKyyyy", "ap-southeast-1", "ark", fixedTime); err != nil {
		t.Fatalf("signAt: %v", err)
	}

	if got := req.Header.Get("X-Date"); got != "20260328T000000Z" {
		t.Errorf("X-Date = %q, want 20260328T000000Z", got)
	}
	// sha256 of the body is stable; assert it is set and hex-length 64.
	if got := req.Header.Get("X-Content-Sha256"); len(got) != 64 {
		t.Errorf("X-Content-Sha256 length = %d, want 64 (%q)", len(got), got)
	}
	if got := req.Header.Get("Host"); got != "ark.ap-southeast-1.byteplusapi.com" {
		t.Errorf("Host = %q", got)
	}
	auth := req.Header.Get("Authorization")
	wantPrefix := "HMAC-SHA256 Credential=AKxxxx/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature="
	if len(auth) <= len(wantPrefix) || auth[:len(wantPrefix)] != wantPrefix {
		t.Errorf("Authorization = %q,\nwant prefix %q", auth, wantPrefix)
	}
}

// TestSignAt_Deterministic ensures identical inputs produce identical signatures.
func TestSignAt_Deterministic(t *testing.T) {
	body := []byte(`{"a":1}`)
	mk := func() string {
		req := newReq(t, http.MethodPost, "https://ark.ap-southeast-1.byteplusapi.com/?Action=GetAsset&Version=2024-01-01", body)
		req.Header.Set("Content-Type", "application/json")
		if err := signAt(req, body, "ak", "sk", "ap-southeast-1", "ark", fixedTime); err != nil {
			t.Fatalf("signAt: %v", err)
		}
		return req.Header.Get("Authorization")
	}
	if mk() != mk() {
		t.Error("signatures differ for identical inputs")
	}
}

// TestSignAt_RegionServiceAffectSignature guards against region/service being
// ignored (which would silently break jimeng's cn-north-1/cv contract).
func TestSignAt_RegionServiceAffectSignature(t *testing.T) {
	body := []byte(`{}`)
	sign := func(region, service string) string {
		req := newReq(t, http.MethodPost, "https://example.com/?Action=X&Version=1", body)
		req.Header.Set("Content-Type", "application/json")
		if err := signAt(req, body, "ak", "sk", region, service, fixedTime); err != nil {
			t.Fatalf("signAt: %v", err)
		}
		return req.Header.Get("Authorization")
	}
	if sign("cn-north-1", "cv") == sign("ap-southeast-1", "ark") {
		t.Error("region/service did not affect signature")
	}
}
