package doubao

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/pkg/errors"
)

func newCloudwiseTestClient(endpoint string) *cloudwiseAssetClient {
	return &cloudwiseAssetClient{
		baseURL:        endpoint,
		apiKey:         "sk-test",
		skipModeration: true,
		httpClient:     http.DefaultClient,
		pollInterval:   5 * time.Millisecond,
		pollTimeout:    2 * time.Second,
	}
}

// TestCloudwise_CreateAndWait_RoundTrip verifies a full upload→poll→return cycle.
// Confirms Authorization header carries the channel API key (not AK/SK).
// Uses channel id 9001 / URL https://example.com/cw-rt.jpg to avoid cache collisions
// with existing tests.
func TestCloudwise_CreateAndWait_RoundTrip(t *testing.T) {
	var gotAuth string
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			raw, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				t.Errorf("read create-asset body: %v", readErr)
			}
			if err := common.Unmarshal(raw, &createBody); err != nil {
				t.Errorf("decode create-asset body %q: %v", raw, err)
			}
			// Document: POST /api/v1/assets/create → {ResponseMetadata, Result:{Id}}
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-asset-1"}}`))
		case "/api/v1/assets/get":
			// Document: GET /api/v1/assets/get → {ResponseMetadata, Result:{Id,Status}}
			// Ready status from document is "Active".
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-1","Status":"Active"}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	id, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-rt.jpg", "Image")
	if err != nil {
		t.Fatalf("CreateAndWait: %v", err)
	}
	if id != "cw-asset-1" {
		t.Errorf("id = %q, want cw-asset-1", id)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}

	// Pin the exact wire field names. The document mixes camelCase data fields with
	// a nested PascalCase Moderation object; a "consistency" rename to snake_case
	// would break every production upload while leaving the rest of this suite green.
	for _, want := range []struct{ key, value string }{
		{"assetType", "Image"},
		{"groupId", "group-1"},
		{"url", "https://example.com/cw-rt.jpg"},
	} {
		if got := createBody[want.key]; got != want.value {
			t.Errorf("create-asset body[%q] = %v, want %q (body: %v)", want.key, got, want.value, createBody)
		}
	}
	mod, ok := createBody["Moderation"].(map[string]any)
	if !ok {
		t.Fatalf(`create-asset body missing PascalCase "Moderation" object (body: %v)`, createBody)
	}
	if mod["Strategy"] != "Skip" {
		t.Errorf(`Moderation.Strategy = %v, want "Skip"`, mod["Strategy"])
	}
}

// TestCloudwise_CreateAsset_ModerationOmittedWhenDisabled verifies that Moderation
// is absent from the request when skipModeration is false, rather than sent as an
// empty object the upstream would have to interpret.
func TestCloudwise_CreateAsset_ModerationOmittedWhenDisabled(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			raw, _ := io.ReadAll(r.Body)
			if err := common.Unmarshal(raw, &createBody); err != nil {
				t.Errorf("decode create-asset body %q: %v", raw, err)
			}
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-asset-2"}}`))
		case "/api/v1/assets/get":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-2","Status":"Active"}}`))
		}
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	cl.skipModeration = false
	if _, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-nomod.jpg", "Image"); err != nil {
		t.Fatalf("CreateAndWait: %v", err)
	}
	if _, present := createBody["Moderation"]; present {
		t.Errorf("Moderation must be omitted when skipModeration is false (body: %v)", createBody)
	}
}

// TestCloudwise_CreateAsset_ErrorCode verifies that a structured upstream error
// propagates as an assetAPIError carrying the message, and that
// IsGroupExhausted returns false for a generic quota/rate-limit code.
// This is the regression guard: a bare quota code must NOT trigger group rotation.
// Uses URL https://example.com/cw-err.jpg to avoid cache collisions.
func TestCloudwise_CreateAsset_ErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simulates a generic quota/rate-limit error that is NOT group exhaustion.
		// Code "QuotaExceeded" must not be treated as group-full (regression guard).
		w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1","Error":{"Code":"QuotaExceeded","Message":"API rate limit exceeded, please retry later"}},"Result":{}}`))
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-err.jpg", "Image")
	if err == nil {
		t.Fatal("expected error for upstream ErrorCode response")
	}
	if !strings.Contains(err.Error(), "QuotaExceeded") {
		t.Errorf("error should carry upstream code, got %v", err)
	}
	// Key regression guard: a generic quota/rate-limit code must NOT be treated as
	// group exhaustion — that would trigger runaway group creation under throttling.
	if cl.IsGroupExhausted(err) {
		t.Error("QuotaExceeded must not be classified as group-exhausted (regression guard)")
	}
}

// TestCloudwise_CreateGroup verifies that CreateGroup hits the correct path and
// returns the group id from the document-defined envelope.
// Uses channel id 9001 to avoid cache collisions.
func TestCloudwise_CreateGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/assets/groups/create" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// Document: {ResponseMetadata, Result:{Id}} — PascalCase envelope.
		w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-group-9"}}`))
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	id, err := cl.CreateGroup(context.Background(), "newapi-ch1")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if id != "cw-group-9" {
		t.Errorf("group id = %q, want cw-group-9", id)
	}
}

// TestCloudwise_IsGroupExhausted_GroupFullCode verifies that a code whose name
// contains an unambiguous group-capacity indicator IS classified as exhausted.
func TestCloudwise_IsGroupExhausted_GroupFullCode(t *testing.T) {
	cl := &cloudwiseAssetClient{}
	err := &assetAPIError{Code: "GroupFull", Message: "the group is full"}
	if !cl.IsGroupExhausted(err) {
		t.Error("GroupFull should be classified as group-exhausted")
	}
}

// TestCloudwise_IsGroupExhausted_FallbackKeyword verifies the message-text fallback:
// requires BOTH a group indicator AND a capacity word.
func TestCloudwise_IsGroupExhausted_FallbackKeyword(t *testing.T) {
	cl := &cloudwiseAssetClient{}
	groupFullMsg := &assetAPIError{Code: "UnknownCode", Message: "the asset group has exceeded its capacity limit"}
	if !cl.IsGroupExhausted(groupFullMsg) {
		t.Error("message containing 'group' and 'capacity' should be classified as group-exhausted")
	}
	// Discriminating negative: carries an exceed-word, so it only stays false while
	// the group conjunction is intact.
	authErr := &assetAPIError{Code: "AuthFailure", Message: "quota exceeded"}
	if cl.IsGroupExhausted(authErr) {
		t.Error("auth failure without 'group' indicator must not be classified as group-exhausted")
	}
	// A throttle message is group-scoped AND contains "exceeded". Matching bare
	// exceed-words here would mint one junk asset group per concurrent throttled
	// request, each with a channel-row write.
	throttled := &assetAPIError{Code: "Throttling", Message: "asset group request rate exceeded, retry later"}
	if cl.IsGroupExhausted(throttled) {
		t.Error("group-scoped throttling must not be classified as group-exhausted (runaway-creation guard)")
	}
	// A missing group id is an operator misconfiguration; rotating would hide it.
	notFound := &assetAPIError{Code: "UnknownCode", Message: "asset group not found"}
	if cl.IsGroupExhausted(notFound) {
		t.Error("'group not found' text must not be classified as group-exhausted")
	}
}

// cloudwisePollServer serves one create-asset response and then replies to every
// poll with the given status body, counting the polls.
func cloudwisePollServer(t *testing.T, assetID, statusBody string, polls *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"` + assetID + `"}}`))
		case "/api/v1/assets/get":
			atomic.AddInt32(polls, 1)
			w.Write([]byte(statusBody))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
}

// TestCloudwise_CreateAndWait_Failed covers the terminal Failed branch: polling
// stops immediately and the masked response body is reported for triage
// (moderation rejections arrive this way).
func TestCloudwise_CreateAndWait_Failed(t *testing.T) {
	var polls int32
	srv := cloudwisePollServer(t, "cw-asset-failed",
		`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-failed","Status":"Failed"}}`, &polls)
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-failed.jpg", "Image")
	if err == nil {
		t.Fatal("expected error for Failed asset status")
	}
	if !strings.Contains(err.Error(), "cw-asset-failed") || !strings.Contains(err.Error(), "processing failed") {
		t.Errorf("error should name the asset and the failure, got %v", err)
	}
	if n := atomic.LoadInt32(&polls); n != 1 {
		t.Errorf("Failed is terminal, want exactly 1 poll, got %d", n)
	}
}

// TestCloudwise_CreateAndWait_Timeout covers the deadline branch: Processing never
// resolves, so the call gives up and reports the last status.
func TestCloudwise_CreateAndWait_Timeout(t *testing.T) {
	var polls int32
	srv := cloudwisePollServer(t, "cw-asset-slow",
		`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-slow","Status":"Processing"}}`, &polls)
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	cl.pollInterval = time.Millisecond
	cl.pollTimeout = 20 * time.Millisecond

	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-slow.jpg", "Image")
	if err == nil {
		t.Fatal("expected timeout error when status never leaves Processing")
	}
	if !strings.Contains(err.Error(), "not ready within") || !strings.Contains(err.Error(), "Processing") {
		t.Errorf("error should report the timeout and last status, got %v", err)
	}
	if n := atomic.LoadInt32(&polls); n < 1 {
		t.Error("expected at least one poll before the deadline")
	}
}

// TestCloudwise_CreateAndWait_ContextCanceled covers the ctx.Done() branch: a
// cancelled client request aborts polling instead of running to the poll deadline.
func TestCloudwise_CreateAndWait_ContextCanceled(t *testing.T) {
	polled := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-asset-cancel"}}`))
		case "/api/v1/assets/get":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-cancel","Status":"Processing"}}`))
			select {
			case polled <- struct{}{}:
			default:
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-polled
		cancel()
	}()

	cl := newCloudwiseTestClient(srv.URL)
	// Interval far longer than the poll round-trip, so the call is parked in the
	// select when the cancel lands and cannot pass the deadline branch instead.
	cl.pollInterval = 30 * time.Second
	cl.pollTimeout = 5 * time.Minute

	_, err := cl.CreateAndWait(ctx, "group-1", "https://example.com/cw-cancel.jpg", "Image")
	if err == nil {
		t.Fatal("expected error when the request context is cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should unwrap to context.Canceled, got %v", err)
	}
}

// TestCloudwise_HTTPStatusError_Classifiable guards the non-2xx path: the asset
// error format is undocumented, so a plain HTTP error must still arrive as an
// *assetAPIError. Otherwise IsGroupExhausted stops at its errors.As check and
// group rotation is silently dead on this provider.
func TestCloudwise_HTTPStatusError_Classifiable(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		body          string
		wantCode      string
		wantExhausted bool
	}{
		{
			// The lowercase shape this gateway uses for its task endpoints.
			name:          "lowercase code/message, group full",
			status:        http.StatusBadRequest,
			body:          `{"code":"1026","message":"asset group is full"}`,
			wantCode:      "1026",
			wantExhausted: true,
		},
		{
			name:          "lowercase nested error object",
			status:        http.StatusBadRequest,
			body:          `{"error":{"code":1026,"message":"asset group capacity reached"}}`,
			wantCode:      "1026",
			wantExhausted: true,
		},
		{
			// Neither shape parses: fall back to the status so the operator still
			// sees something classifiable, and do not rotate.
			name:          "unparseable body falls back to status",
			status:        http.StatusBadGateway,
			body:          `<html>502 Bad Gateway</html>`,
			wantCode:      "HTTP502",
			wantExhausted: false,
		},
		{
			name:          "lowercase throttling is not exhaustion",
			status:        http.StatusTooManyRequests,
			body:          `{"code":"Throttling","message":"asset group request rate exceeded"}`,
			wantCode:      "Throttling",
			wantExhausted: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			cl := newCloudwiseTestClient(srv.URL)
			_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-http.jpg", "Image")
			if err == nil {
				t.Fatal("expected error for non-2xx response")
			}
			var apiErr *assetAPIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("non-2xx must produce *assetAPIError so it can be classified, got %T: %v", err, err)
			}
			if apiErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tc.wantCode)
			}
			if got := cl.IsGroupExhausted(err); got != tc.wantExhausted {
				t.Errorf("IsGroupExhausted = %v, want %v (code %q, message %q)",
					got, tc.wantExhausted, apiErr.Code, apiErr.Message)
			}
		})
	}
}

// TestCloudwise_HTTP200BusinessError_Classifiable guards the case that reopened the
// bug the non-2xx path was meant to close: this gateway puts the business result in
// the body, not the status line (cloudwise-api-docs.md:308-333 shows a *successful*
// fetch as HTTP 200 with code "10000"), so a group-full error can arrive at HTTP 200.
// Gating the lowercase parse on non-2xx left that body looking like a success with an
// empty Result, and the caller returned a plain "empty asset id" error that
// IsGroupExhausted could not classify — no rotation, and a misleading message.
func TestCloudwise_HTTP200BusinessError_Classifiable(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		wantCode      string
		wantExhausted bool
	}{
		{
			name:          "group full at 200 rotates",
			body:          `{"code":"1026","message":"asset group is full"}`,
			wantCode:      "1026",
			wantExhausted: true,
		},
		{
			name:          "success false with group capacity text",
			body:          `{"code":"GroupFull","message":"asset group capacity reached","success":false}`,
			wantCode:      "GroupFull",
			wantExhausted: true,
		},
		{
			name:          "throttling at 200 must not rotate",
			body:          `{"code":"Throttling","message":"asset group request rate exceeded","success":false}`,
			wantCode:      "Throttling",
			wantExhausted: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			cl := newCloudwiseTestClient(srv.URL)
			_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-200err.jpg", "Image")
			if err == nil {
				t.Fatal("expected error for HTTP 200 business error")
			}
			var apiErr *assetAPIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("HTTP 200 business error must produce *assetAPIError, got %T: %v", err, err)
			}
			if apiErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tc.wantCode)
			}
			if strings.Contains(err.Error(), "empty asset id") {
				t.Errorf("business error must not be reported as an empty-id error: %v", err)
			}
			if got := cl.IsGroupExhausted(err); got != tc.wantExhausted {
				t.Errorf("IsGroupExhausted = %v, want %v (code %q, message %q)",
					got, tc.wantExhausted, apiErr.Code, apiErr.Message)
			}
		})
	}
}

// TestCloudwise_HTTP200BusinessError_NoFalsePositives is the other half of that
// guard: CreateGroup and the poll path share post(), so a result-less 2xx must not be
// invented into an error. A poll parked at Processing carries no Id at all.
func TestCloudwise_HTTP200BusinessError_NoFalsePositives(t *testing.T) {
	t.Run("processing poll without id keeps polling", func(t *testing.T) {
		var polls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/assets/create":
				w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-asset-proc"}}`))
			case "/api/v1/assets/get":
				// No Id in the poll body, only a status — the shape that must never be
				// mistaken for a business error.
				if atomic.AddInt32(&polls, 1) == 1 {
					w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Status":"Processing"}}`))
					return
				}
				w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r3"},"Result":{"Status":"Active"}}`))
			default:
				t.Errorf("unexpected path %q", r.URL.Path)
			}
		}))
		defer srv.Close()

		cl := newCloudwiseTestClient(srv.URL)
		id, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-proc-ok.jpg", "Image")
		if err != nil {
			t.Fatalf("a Processing poll must not become an error: %v", err)
		}
		if id != "cw-asset-proc" {
			t.Errorf("id = %q, want cw-asset-proc", id)
		}
		if n := atomic.LoadInt32(&polls); n != 2 {
			t.Errorf("expected 2 polls (Processing then Active), got %d", n)
		}
	})

	t.Run("documented success code with empty result is not an api error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// The gateway's documented success envelope. It yields no asset id, which is
			// still a failure, but it must stay the plain empty-id error rather than being
			// dressed up as code "10000" and fed to the rotation check.
			w.Write([]byte(`{"code":"10000","data":{},"success":true}`))
		}))
		defer srv.Close()

		cl := newCloudwiseTestClient(srv.URL)
		_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-200ok-empty.jpg", "Image")
		if err == nil {
			t.Fatal("expected the empty-asset-id error")
		}
		var apiErr *assetAPIError
		if errors.As(err, &apiErr) {
			t.Errorf("a successful envelope must not be classified as an api error, got code %q", apiErr.Code)
		}
		if !strings.Contains(err.Error(), "empty asset id") {
			t.Errorf("expected the empty-id error, got %v", err)
		}
		if cl.IsGroupExhausted(err) {
			t.Error("a successful envelope must never trigger group rotation")
		}
	})
}

// TestCloudwise_NestedErrorWithoutCode_UsesTopLevelCode covers the nested-error
// precedence fix: the nested object used to win wholesale, so a body carrying the
// real code at the top level and only a message under "error" was classified as
// "HTTP400" and a genuine GroupFull never rotated the group.
func TestCloudwise_NestedErrorWithoutCode_UsesTopLevelCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"GroupFull","error":{"message":"no code here"}}`))
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-nested-nocode.jpg", "Image")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	var apiErr *assetAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *assetAPIError, got %T: %v", err, err)
	}
	if apiErr.Code != "GroupFull" {
		t.Errorf("Code = %q, want GroupFull (top-level code must survive a codeless nested error)", apiErr.Code)
	}
	if apiErr.Message != "no code here" {
		t.Errorf("Message = %q, want the nested message", apiErr.Message)
	}
	if !cl.IsGroupExhausted(err) {
		t.Error("GroupFull must still be classified as group-exhausted")
	}
}

// TestCloudwise_ErrorCode_NonScalarAndEmptyMessage pins the two rendering fixes: a
// non-scalar code is dropped instead of leaking raw JSON into the operator-facing
// text (the message still drives classification), and an empty message does not
// render a dangling colon.
func TestCloudwise_ErrorCode_NonScalarAndEmptyMessage(t *testing.T) {
	t.Run("object code does not leak raw json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"code":{"x":1},"message":"asset group is full"}`))
		}))
		defer srv.Close()

		cl := newCloudwiseTestClient(srv.URL)
		_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-objcode.jpg", "Image")
		if err == nil {
			t.Fatal("expected error for 400 response")
		}
		var apiErr *assetAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *assetAPIError, got %T: %v", err, err)
		}
		if strings.Contains(apiErr.Code, "{") {
			t.Errorf("Code must not carry raw JSON, got %q", apiErr.Code)
		}
		if apiErr.Code != "HTTP400" {
			t.Errorf("Code = %q, want the status fallback HTTP400", apiErr.Code)
		}
		// The message survived, so the group-capacity text fallback still classifies it.
		if !cl.IsGroupExhausted(err) {
			t.Errorf("message %q should still classify as group-exhausted", apiErr.Message)
		}
	})

	t.Run("quoted code with inner escape decodes", func(t *testing.T) {
		if got := cloudwiseErrorCode([]byte(`"a\"b"`)); got != `a"b` {
			t.Errorf("cloudwiseErrorCode = %q, want %q", got, `a"b`)
		}
		if got := cloudwiseErrorCode([]byte(`1026`)); got != "1026" {
			t.Errorf("numeric code = %q, want 1026", got)
		}
		if got := cloudwiseErrorCode([]byte(`null`)); got != "" {
			t.Errorf("null code = %q, want empty", got)
		}
	})

	t.Run("empty message renders without dangling colon", func(t *testing.T) {
		if got := (&assetAPIError{Code: "HTTP500"}).Error(); got != "HTTP500" {
			t.Errorf("Error() = %q, want HTTP500", got)
		}
		// The populated form is unchanged; existing tests assert on it.
		if got := (&assetAPIError{Code: "GroupFull", Message: "full"}).Error(); got != "GroupFull: full" {
			t.Errorf("Error() = %q, want \"GroupFull: full\"", got)
		}
	})

	// The mirror case: a business error at HTTP 200 has no status code to borrow, so
	// Code is empty and the separator has to drop from the other side.
	t.Run("empty code renders without leading colon", func(t *testing.T) {
		if got := (&assetAPIError{Message: "asset group is full"}).Error(); got != "asset group is full" {
			t.Errorf("Error() = %q, want the bare message with no leading colon", got)
		}
	})
}

// TestCloudwise_HTTP200_SuccessPayloadNotAnError guards the rotation classifier
// against its own success responses. The 200 path must not substitute the raw body
// for a missing message: assetAPIError.Message is what IsGroupExhausted
// substring-matches, and a healthy body carries "groupId" (matching "group") next to
// "capacity", so the fallback read a successful upload as an exhausted group and
// minted a junk group plus a channel-row write.
func TestCloudwise_HTTP200_SuccessPayloadNotAnError(t *testing.T) {
	t.Run("error code without message does not borrow the body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// A real business error code, but no message. The body text around it is
			// exactly what used to trip the group+capacity conjunction.
			w.Write([]byte(`{"code":"1026","data":{"groupId":"g-1","capacity":1000}}`))
		}))
		defer srv.Close()

		cl := newCloudwiseTestClient(srv.URL)
		_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-200-rawmsg.jpg", "Image")
		if err == nil {
			t.Fatal("expected an error for a business error code")
		}
		var apiErr *assetAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *assetAPIError, got %T: %v", err, err)
		}
		if apiErr.Message != "" {
			t.Errorf("Message = %q, want empty: the raw body must not be substituted on the 200 path", apiErr.Message)
		}
		if strings.Contains(apiErr.Message, "groupId") || strings.Contains(apiErr.Message, "capacity") {
			t.Errorf("Message leaked body text into the rotation classifier: %q", apiErr.Message)
		}
		if cl.IsGroupExhausted(err) {
			t.Error("a codeless-message business error must not mint a new asset group")
		}
	})

	t.Run("plain success payload stays the empty-id error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// The verified failure body: a success with an empty message whose data
			// happens to name a group and a capacity.
			w.Write([]byte(`{"code":0,"message":"","data":{"groupId":"g-1","capacity":1000}}`))
		}))
		defer srv.Close()

		cl := newCloudwiseTestClient(srv.URL)
		_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-200-success.jpg", "Image")
		if err == nil {
			t.Fatal("expected the empty-asset-id error")
		}
		var apiErr *assetAPIError
		if errors.As(err, &apiErr) {
			t.Errorf("a success payload must not become an api error, got code %q message %q", apiErr.Code, apiErr.Message)
		}
		if !strings.Contains(err.Error(), "empty asset id") {
			t.Errorf("expected the plain empty-id error, got %v", err)
		}
		if cl.IsGroupExhausted(err) {
			t.Error("a success payload must never trigger group rotation")
		}
	})
}

// TestCloudwise_DocumentedSuccessCodes covers both spellings the document uses for a
// successful response: the quoted "10000" on the task endpoints and the numeric 0 on
// the image endpoints (cloudwise-api-docs.md:10674-10675). Only "10000" was in the
// guard, so a body reading {"code":0,"message":"success"} surfaced to the operator as
// `cloudwise CreateAsset failed: 0: success`.
func TestCloudwise_DocumentedSuccessCodes(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		mediaURL string
	}{
		{
			name:     "numeric zero with success message",
			body:     `{"code":0,"message":"success","request_id":"r","data":{"task_id":"t"}}`,
			mediaURL: "https://example.com/cw-code0.jpg",
		},
		{
			// The same code as a quoted string must be treated identically; only the
			// JSON spelling differs.
			name:     "quoted zero",
			body:     `{"code":"0","message":"success","request_id":"r"}`,
			mediaURL: "https://example.com/cw-code0str.jpg",
		},
		{
			name:     "quoted ten-thousand",
			body:     `{"code":"10000","message":"Task created successfully","data":{}}`,
			mediaURL: "https://example.com/cw-code10000.jpg",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			cl := newCloudwiseTestClient(srv.URL)
			_, err := cl.CreateAndWait(context.Background(), "group-1", tc.mediaURL, "Image")
			// No asset id in the body, so this is still a failure — but it must stay the
			// plain empty-id error, never a success reported as an api error.
			if err == nil {
				t.Fatal("expected the empty-asset-id error")
			}
			var apiErr *assetAPIError
			if errors.As(err, &apiErr) {
				t.Errorf("documented success code must not become an api error, got %q", apiErr.Error())
			}
			if !strings.Contains(err.Error(), "empty asset id") {
				t.Errorf("expected the plain empty-id error, got %v", err)
			}
			if cl.IsGroupExhausted(err) {
				t.Error("a documented success code must never trigger group rotation")
			}
		})
	}
}

// TestCloudwise_SuccessFlagPrecedence pins the chosen precedence for the "success"
// field, which is only defined as "Whether the request was successful"
// (cloudwise-api-docs.md:7320):
//
//	documented success code  >  explicit error code  >  success:true  >  message alone
//
// An explicit non-success code names a specific failure, so it outranks a
// contradictory success:true, which previously short-circuited before any code was
// read and silently disabled rotation on a group-full response.
func TestCloudwise_SuccessFlagPrecedence(t *testing.T) {
	t.Run("explicit error code beats success true", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"success":true,"code":"1026","message":"asset group is full"}`))
		}))
		defer srv.Close()

		cl := newCloudwiseTestClient(srv.URL)
		_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-succ-contra.jpg", "Image")
		if err == nil {
			t.Fatal("expected an error for an explicit group-full code")
		}
		var apiErr *assetAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("a contradictory success:true must not suppress the error code, got %T: %v", err, err)
		}
		if apiErr.Code != "1026" {
			t.Errorf("Code = %q, want 1026", apiErr.Code)
		}
		if !cl.IsGroupExhausted(err) {
			t.Error("group-full at 200 must still rotate even with success:true present")
		}
	})

	t.Run("success true still wins over a message alone", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// "message" carries the human text on success responses too, so with no
			// code to contradict it success:true remains decisive.
			w.Write([]byte(`{"success":true,"message":"Task created successfully","data":{}}`))
		}))
		defer srv.Close()

		cl := newCloudwiseTestClient(srv.URL)
		_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-succ-msgonly.jpg", "Image")
		if err == nil {
			t.Fatal("expected the empty-asset-id error")
		}
		var apiErr *assetAPIError
		if errors.As(err, &apiErr) {
			t.Errorf("success:true with only a message must stay a non-error, got %q", apiErr.Error())
		}
		if !strings.Contains(err.Error(), "empty asset id") {
			t.Errorf("expected the plain empty-id error, got %v", err)
		}
	})

	t.Run("documented success code beats success false", func(t *testing.T) {
		// The endpoint-specific code is the stronger signal, so it outranks a
		// contradictory success:false rather than inventing an error from code "0".
		if code, message, isErr := parseCloudwiseBusinessError(
			[]byte(`{"code":0,"message":"success","success":false}`)); isErr {
			t.Errorf("documented success code must win, got code %q message %q", code, message)
		}
	})
}

// TestCloudwise_HTTP200_CodelessErrorRendersCleanly is the end-to-end half of the
// empty-Code rendering fix. The 200 path has no HTTP status to fall back on, so a
// message-only business error leaves Code empty, and the operator saw
// `cloudwise CreateAsset failed: : asset group is full`. The message-only shape is
// documented (cloudwise-api-docs.md:3739-3741).
func TestCloudwise_HTTP200_CodelessErrorRendersCleanly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"asset group is full"}`))
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-200-nocode.jpg", "Image")
	if err == nil {
		t.Fatal("expected an error for a message-only business error")
	}
	var apiErr *assetAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *assetAPIError so it stays classifiable, got %T: %v", err, err)
	}
	if apiErr.Code != "" {
		t.Errorf("Code = %q, want empty on the 200 path (no status to borrow)", apiErr.Code)
	}
	if got := apiErr.Error(); got != "asset group is full" {
		t.Errorf("Error() = %q, want the bare message with no leading colon", got)
	}
	if strings.Contains(err.Error(), ": : ") {
		t.Errorf("operator text carries a dangling colon: %v", err)
	}
	// The message still classifies, so rotation works despite the missing code.
	if !cl.IsGroupExhausted(err) {
		t.Error("group-full text must still be classified as group-exhausted")
	}
}

// TestCloudwise_NonJSONErrorBody_NotClassifiedByRawText closes the non-2xx half of the
// raw-body hazard the HTTP-200 branch already refuses to create. When nothing parses,
// the whole response body becomes the message purely so the operator can see it — but
// substring-matching that body classified a plain throttle as an exhausted group,
// minting one junk group and one channel-row write per concurrent throttled request.
func TestCloudwise_NonJSONErrorBody_NotClassifiedByRawText(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantText string
	}{
		{
			// The reported failure: 429 with an unrecognised key, whose text happens to
			// carry both halves of the group+capacity conjunction.
			name:     "throttle detail mentioning group and capacity",
			status:   http.StatusTooManyRequests,
			body:     `{"detail":"asset group upload capacity temporarily throttled, retry in 30s"}`,
			wantText: "temporarily throttled",
		},
		{
			name:     "html body mentioning group and full",
			status:   http.StatusBadGateway,
			body:     `<html>asset group queue full</html>`,
			wantText: "asset group queue full",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			cl := newCloudwiseTestClient(srv.URL)
			_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-rawbody.jpg", "Image")
			if err == nil {
				t.Fatal("expected error for non-2xx response")
			}
			var apiErr *assetAPIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("non-2xx must still produce *assetAPIError, got %T: %v", err, err)
			}
			if cl.IsGroupExhausted(err) {
				t.Errorf("a body with no parseable business error must not classify as group-exhausted (message %q)", apiErr.Message)
			}
			// The body is still operator-visible; only classification is barred from it.
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("raw body must remain in the operator-facing error, got %v", err)
			}
		})
	}
}

// TestCloudwise_ParsedGroupFullMessage_StillClassifies is the other side of that
// guard: a genuine group-full message that really was parsed out of the body must keep
// rotating, both when it arrives with a code and when the code had to fall back to the
// HTTP status.
func TestCloudwise_ParsedGroupFullMessage_StillClassifies(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantCode string
	}{
		{
			name:     "lowercase code and message",
			status:   http.StatusBadRequest,
			body:     `{"code":"1026","message":"asset group is full"}`,
			wantCode: "1026",
		},
		{
			// No usable code, but the message itself was parsed — the status fallback
			// must not disable the text classifier for it.
			name:     "message only, code falls back to status",
			status:   http.StatusBadRequest,
			body:     `{"message":"asset group capacity reached"}`,
			wantCode: "HTTP400",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			cl := newCloudwiseTestClient(srv.URL)
			_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-realfull.jpg", "Image")
			if err == nil {
				t.Fatal("expected error for non-2xx response")
			}
			var apiErr *assetAPIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *assetAPIError, got %T: %v", err, err)
			}
			if apiErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tc.wantCode)
			}
			if !cl.IsGroupExhausted(err) {
				t.Errorf("a parsed group-full message must still classify as exhausted (message %q)", apiErr.Message)
			}
		})
	}
}
