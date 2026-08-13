package doubao

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
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

// TestPreuploadAssets_GroupExhausted_RetrySucceeds verifies that when a group
// is full, exactly one new group is created and the upload retries successfully.
func TestPreuploadAssets_GroupExhausted_RetrySucceeds(t *testing.T) {
	const chanID = 8001
	var createGroupCalls int32
	var createAssetAttempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			n := atomic.AddInt32(&createAssetAttempts, 1)
			if n == 1 {
				// First attempt: group full.
				w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"GroupFull","Message":"group is full"}}}`))
				return
			}
			// Retry into new group: succeed.
			w.Write([]byte(`{"Result":{"Id":"asset-new-group"}}`))
		case "GetAsset":
			w.Write([]byte(`{"Result":{"Status":"Active"}}`))
		case "CreateAssetGroup":
			atomic.AddInt32(&createGroupCalls, 1)
			w.Write([]byte(`{"Result":{"Id":"group-new-1"}}`))
		}
	}))
	defer srv.Close()

	settings := enabledSettings()
	settings.BytePlusAssetGroupId = "group-exhausted"
	a := &TaskAdaptor{
		otherSettings:    settings,
		endpointOverride: srv.URL,
		channelId:        chanID,
	}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/rotate-test-ch8001.jpg"}},
	}}

	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := payload.Content[0].ImageURL.URL; got != "asset://asset-new-group" {
		t.Errorf("expected asset://asset-new-group, got %q", got)
	}
	if n := atomic.LoadInt32(&createGroupCalls); n != 1 {
		t.Errorf("expected exactly 1 CreateAssetGroup call, got %d", n)
	}
	if n := atomic.LoadInt32(&createAssetAttempts); n != 2 {
		t.Errorf("expected 2 CreateAsset attempts (initial + retry), got %d", n)
	}
	// New group id must be reflected in adaptor state.
	if a.otherSettings.BytePlusAssetGroupId != "group-new-1" {
		t.Errorf("expected group id updated to group-new-1, got %q", a.otherSettings.BytePlusAssetGroupId)
	}
}

// TestPreuploadAssets_GroupExhausted_DoubleExhaustion verifies that when the
// retry into the fresh group also fails with exhaustion, the error surfaces and
// still exactly one group was created (no second creation attempt).
func TestPreuploadAssets_GroupExhausted_DoubleExhaustion(t *testing.T) {
	const chanID = 8002
	var createGroupCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			// Always fail with GroupFull — both initial and retry attempts.
			w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"GroupFull","Message":"group is full"}}}`))
		case "CreateAssetGroup":
			atomic.AddInt32(&createGroupCalls, 1)
			w.Write([]byte(`{"Result":{"Id":"group-double-1"}}`))
		}
	}))
	defer srv.Close()

	settings := enabledSettings()
	settings.BytePlusAssetGroupId = "group-will-exhaust"
	a := &TaskAdaptor{
		otherSettings:    settings,
		endpointOverride: srv.URL,
		channelId:        chanID,
	}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/double-exhaust-ch8002.jpg"}},
	}}

	err := a.preuploadAssets(ginCtx(), payload)
	if err == nil {
		t.Fatal("expected error for double exhaustion, got nil")
	}
	if n := atomic.LoadInt32(&createGroupCalls); n != 1 {
		t.Errorf("expected exactly 1 CreateAssetGroup call (no runaway), got %d", n)
	}
}

// TestPreuploadAssets_NonGroupErrors_NoGroupCreated verifies that errors
// unrelated to group exhaustion (auth, moderation, bad URL) propagate and zero
// groups are created.
func TestPreuploadAssets_NonGroupErrors_NoGroupCreated(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		chanID   int
		mediaURL string
	}{
		{"auth failure", "AuthFailure", 8101, "https://example.com/auth-fail-ch8101.jpg"},
		{"moderation rejection", "ContentRiskFailed", 8102, "https://example.com/moderation-ch8102.jpg"},
		{"invalid media URL", "InvalidParameter.URL", 8103, "https://example.com/bad-url-ch8103.jpg"},
		{"unknown error code", "InternalError", 8104, "https://example.com/unknown-err-ch8104.jpg"},
		// QuotaExceeded must NOT trigger group creation: it is indistinguishable
		// from API-level call-rate throttling, so treating it as group-exhausted
		// would cause a runaway CreateAssetGroup loop under sustained load.
		{"generic quota exceeded", "QuotaExceeded", 8105, "https://example.com/quota-exceeded-ch8105.jpg"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var createGroupCalls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Query().Get("Action") {
				case "CreateAsset":
					w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"` + tc.code + `","Message":"test error"}}}`))
				case "CreateAssetGroup":
					atomic.AddInt32(&createGroupCalls, 1)
					w.Write([]byte(`{"Result":{"Id":"should-never-be-created"}}`))
				}
			}))
			defer srv.Close()

			settings := enabledSettings()
			settings.BytePlusAssetGroupId = "group-non-exhausted"
			a := &TaskAdaptor{
				otherSettings:    settings,
				endpointOverride: srv.URL,
				channelId:        tc.chanID,
			}
			payload := &requestPayload{Content: []ContentItem{
				{Type: "image_url", ImageURL: &MediaURL{URL: tc.mediaURL}},
			}}

			err := a.preuploadAssets(ginCtx(), payload)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if n := atomic.LoadInt32(&createGroupCalls); n != 0 {
				t.Errorf("expected 0 CreateAssetGroup calls for non-group error %q, got %d", tc.code, n)
			}
		})
	}
}

// assetGroupRecorder is a BytePlus asset-library stub that records the GroupId of
// every CreateAsset call and counts CreateAssetGroup calls. createAssetBody returns
// the CreateAsset response for the n-th attempt (1-based), so a test can make the
// first attempt fail without touching the recording logic.
type assetGroupRecorder struct {
	mu             sync.Mutex
	uploadGroupIds []string

	groupCalls   int32
	assetCalls   int32
	newGroupIds  []string
	createAssetF func(attempt int) string
}

func (r *assetGroupRecorder) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Query().Get("Action") {
		case "CreateAssetGroup":
			n := int(atomic.AddInt32(&r.groupCalls, 1))
			if n > len(r.newGroupIds) {
				t.Errorf("unexpected CreateAssetGroup call #%d", n)
				w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"TooManyGroups","Message":"unexpected"}}}`))
				return
			}
			w.Write([]byte(`{"Result":{"Id":"` + r.newGroupIds[n-1] + `"}}`))
		case "CreateAsset":
			body, _ := io.ReadAll(req.Body)
			var parsed struct {
				GroupId string `json:"GroupId"`
			}
			_ = common.Unmarshal(body, &parsed)
			r.mu.Lock()
			r.uploadGroupIds = append(r.uploadGroupIds, parsed.GroupId)
			r.mu.Unlock()
			n := int(atomic.AddInt32(&r.assetCalls, 1))
			w.Write([]byte(r.createAssetF(n)))
		case "GetAsset":
			w.Write([]byte(`{"Result":{"Status":"Active"}}`))
		}
	}))
}

func (r *assetGroupRecorder) groups() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.uploadGroupIds...)
}

func alwaysCreated(id string) func(int) string {
	return func(int) string { return `{"Result":{"Id":"` + id + `"}}` }
}

// TestPreuploadAssets_BlankGroupId_Bootstraps is the regression guard for the
// bootstrap path. A channel with no group id is the normal first-run state, and it
// used to get a group only by accident: the request carried groupId:"" and the code
// waited for the upstream to reject it with a code that happened to be listed in the
// (openly guessed) exhaustion table. Bootstrap is now explicit — exactly one
// CreateAssetGroup, then a successful upload into it, with no error involved at all.
func TestPreuploadAssets_BlankGroupId_Bootstraps(t *testing.T) {
	rec := &assetGroupRecorder{
		newGroupIds:  []string{"group-bootstrap-1"},
		createAssetF: alwaysCreated("asset-bootstrap"),
	}
	srv := rec.server(t)
	defer srv.Close()

	settings := enabledSettings()
	settings.BytePlusAssetGroupId = "" // new channel: the frontend no longer requires one
	a := &TaskAdaptor{otherSettings: settings, endpointOverride: srv.URL, channelId: 8201}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/bootstrap-ch8201.jpg"}},
	}}

	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := payload.Content[0].ImageURL.URL; got != "asset://asset-bootstrap" {
		t.Errorf("expected asset://asset-bootstrap, got %q", got)
	}
	if n := atomic.LoadInt32(&rec.groupCalls); n != 1 {
		t.Errorf("expected exactly 1 CreateAssetGroup call, got %d", n)
	}
	if n := atomic.LoadInt32(&rec.assetCalls); n != 1 {
		t.Errorf("expected exactly 1 CreateAsset call (no error-driven retry), got %d", n)
	}
	if got := rec.groups(); len(got) != 1 || got[0] != "group-bootstrap-1" {
		t.Errorf("upload group ids = %v, want [group-bootstrap-1] (never an empty groupId)", got)
	}
	if a.otherSettings.BytePlusAssetGroupId != "group-bootstrap-1" {
		t.Errorf("bootstrapped group id not kept in adaptor state, got %q", a.otherSettings.BytePlusAssetGroupId)
	}
	if a.otherSettings.AssetGroupProvider != dto.AssetProviderBytePlus {
		t.Errorf("group provider marker = %q, want %q",
			a.otherSettings.AssetGroupProvider, dto.AssetProviderBytePlus)
	}
}

// TestPreuploadAssets_Bootstrap_DoesNotConsumeRotation pins the interaction between
// the two group-creating paths: bootstrapping a missing group is not a rotation, so a
// first request that bootstraps must still have its single rotation available when the
// fresh group turns out to be unusable.
func TestPreuploadAssets_Bootstrap_DoesNotConsumeRotation(t *testing.T) {
	rec := &assetGroupRecorder{
		newGroupIds: []string{"group-bootstrap-2", "group-rotated-2"},
		createAssetF: func(attempt int) string {
			if attempt == 1 {
				return `{"ResponseMetadata":{"Error":{"Code":"GroupFull","Message":"group is full"}}}`
			}
			return `{"Result":{"Id":"asset-after-rotation"}}`
		},
	}
	srv := rec.server(t)
	defer srv.Close()

	settings := enabledSettings()
	settings.BytePlusAssetGroupId = ""
	a := &TaskAdaptor{otherSettings: settings, endpointOverride: srv.URL, channelId: 8202}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/bootstrap-rotate-ch8202.jpg"}},
	}}

	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := payload.Content[0].ImageURL.URL; got != "asset://asset-after-rotation" {
		t.Errorf("expected asset://asset-after-rotation, got %q", got)
	}
	if n := atomic.LoadInt32(&rec.groupCalls); n != 2 {
		t.Errorf("expected 2 CreateAssetGroup calls (bootstrap + one rotation), got %d", n)
	}
	if got := rec.groups(); len(got) != 2 || got[0] != "group-bootstrap-2" || got[1] != "group-rotated-2" {
		t.Errorf("upload group ids = %v, want [group-bootstrap-2 group-rotated-2]", got)
	}
}

// TestPreuploadAssets_GroupIdFromOtherProvider_Ignored covers the provider scoping of
// the stored group id. Flipping asset_provider used to hand the other library's group
// id upstream, which at best rotated it away (destroying the operator's original id)
// and at worst failed every request. The marker makes the foreign id read as absent,
// so the request bootstraps a fresh group instead.
func TestPreuploadAssets_GroupIdFromOtherProvider_Ignored(t *testing.T) {
	rec := &assetGroupRecorder{
		newGroupIds:  []string{"group-fresh-3"},
		createAssetF: alwaysCreated("asset-fresh-3"),
	}
	srv := rec.server(t)
	defer srv.Close()

	settings := enabledSettings()
	settings.BytePlusAssetGroupId = "cw-group-from-cloudwise"
	settings.AssetGroupProvider = dto.AssetProviderCloudwise
	settings.AssetProvider = "" // empty == byteplus, i.e. the operator switched back
	a := &TaskAdaptor{otherSettings: settings, endpointOverride: srv.URL, channelId: 8203}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/foreign-group-ch8203.jpg"}},
	}}

	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if n := atomic.LoadInt32(&rec.groupCalls); n != 1 {
		t.Errorf("expected 1 CreateAssetGroup call for the foreign group id, got %d", n)
	}
	for _, g := range rec.groups() {
		if g != "group-fresh-3" {
			t.Errorf("upload used group %q, want group-fresh-3 (the cloudwise id must never be sent)", g)
		}
	}
	if a.otherSettings.AssetGroupProvider != dto.AssetProviderBytePlus {
		t.Errorf("group provider marker = %q, want %q after re-minting",
			a.otherSettings.AssetGroupProvider, dto.AssetProviderBytePlus)
	}
}

// TestPreuploadAssets_AbsentGroupProviderMarker_KeepsGroupId is the compatibility half
// of the same fix: rows written before the marker existed carry no marker, and they
// must keep using their stored group id rather than being re-bootstrapped.
func TestPreuploadAssets_AbsentGroupProviderMarker_KeepsGroupId(t *testing.T) {
	rec := &assetGroupRecorder{
		newGroupIds:  nil, // any CreateAssetGroup call is a failure
		createAssetF: alwaysCreated("asset-legacy-4"),
	}
	srv := rec.server(t)
	defer srv.Close()

	settings := enabledSettings()
	settings.BytePlusAssetGroupId = "group-legacy-4"
	settings.AssetGroupProvider = "" // legacy row: marker absent
	a := &TaskAdaptor{otherSettings: settings, endpointOverride: srv.URL, channelId: 8204}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/legacy-group-ch8204.jpg"}},
	}}

	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if n := atomic.LoadInt32(&rec.groupCalls); n != 0 {
		t.Errorf("an absent marker must not be treated as a mismatch, got %d CreateAssetGroup calls", n)
	}
	if got := rec.groups(); len(got) != 1 || got[0] != "group-legacy-4" {
		t.Errorf("upload group ids = %v, want [group-legacy-4]", got)
	}
}
