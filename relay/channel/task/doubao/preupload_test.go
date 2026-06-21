package doubao

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
)

// newAdaptorWithServer returns a TaskAdaptor configured to talk to the test
// server, with BytePlus asset upload enabled. The asset client endpoint is
// injected via a hook so we can point it at httptest.
func enabledSettings() dto.ChannelOtherSettings {
	return dto.ChannelOtherSettings{
		BytePlusAssetEnabled: true,
		BytePlusAccessKey:    "ak",
		BytePlusSecretKey:    "sk",
		BytePlusAssetGroupId: "group-test",
		BytePlusProjectName:  "default",
		BytePlusRegion:       "ap-southeast-1",
	}
}

// ginCtx returns a minimal *gin.Context wrapping a background request.
func ginCtx() *gin.Context {
	req, _ := http.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	c := &gin.Context{Request: req}
	return c
}

func TestPreuploadAssets_Disabled_Passthrough(t *testing.T) {
	a := &TaskAdaptor{} // otherSettings zero => disabled
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/i.jpg"}},
	}}
	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := payload.Content[0].ImageURL.URL; got != "https://example.com/i.jpg" {
		t.Errorf("url mutated while disabled: %q", got)
	}
}

func TestPreuploadAssets_Base64Rejected(t *testing.T) {
	a := &TaskAdaptor{otherSettings: enabledSettings(), endpointOverride: "http://127.0.0.1:0"}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "data:image/png;base64,AAAA"}},
	}}
	err := a.preuploadAssets(ginCtx(), payload)
	if err == nil || !strings.Contains(err.Error(), "public http(s) URL") {
		t.Fatalf("expected base64 rejection, got %v", err)
	}
}

func TestPreuploadAssets_ReplacesAndIsIdempotent(t *testing.T) {
	var createCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			n := atomic.AddInt32(&createCalls, 1)
			w.Write([]byte(`{"Result":{"Id":"asset-` + string(rune('0'+n)) + `"}}`))
		case "GetAsset":
			w.Write([]byte(`{"Result":{"Status":"Active"}}`))
		}
	}))
	defer srv.Close()

	a := &TaskAdaptor{otherSettings: enabledSettings(), endpointOverride: srv.URL, channelId: 4242}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/uniq-1.jpg"}},
		{Type: "video_url", VideoURL: &MediaURL{URL: "asset://asset-preexisting"}}, // idempotent skip
		{Type: "text", Text: "hello"},
	}}
	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := payload.Content[0].ImageURL.URL; !strings.HasPrefix(got, "asset://asset-") {
		t.Errorf("image url not replaced: %q", got)
	}
	if got := payload.Content[1].VideoURL.URL; got != "asset://asset-preexisting" {
		t.Errorf("preexisting asset:// url should be untouched, got %q", got)
	}
	if got := payload.Content[2].Text; got != "hello" {
		t.Errorf("text item mutated: %q", got)
	}

	// Second call with same URL must hit the cache (no new CreateAsset).
	before := atomic.LoadInt32(&createCalls)
	payload2 := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/uniq-1.jpg"}},
	}}
	if err := a.preuploadAssets(ginCtx(), payload2); err != nil {
		t.Fatalf("preuploadAssets (2): %v", err)
	}
	if atomic.LoadInt32(&createCalls) != before {
		t.Errorf("expected cache hit (no new CreateAsset), calls went %d -> %d", before, atomic.LoadInt32(&createCalls))
	}
}

func TestPreuploadAssets_MissingCreds(t *testing.T) {
	a := &TaskAdaptor{otherSettings: dto.ChannelOtherSettings{BytePlusAssetEnabled: true}}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/i.jpg"}},
	}}
	if err := a.preuploadAssets(ginCtx(), payload); err == nil {
		t.Fatal("expected error for missing credentials")
	}
}
