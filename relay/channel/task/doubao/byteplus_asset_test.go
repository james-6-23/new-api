package doubao

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestClient returns a client pointed at the given test server with fast polling.
func newTestClient(endpoint string) *bytePlusAssetClient {
	return &bytePlusAssetClient{
		ak:             "ak",
		sk:             "sk",
		region:         "ap-southeast-1",
		projectName:    "default",
		groupId:        "group-test",
		skipModeration: true,
		httpClient:     http.DefaultClient,
		endpoint:       endpoint,
		pollInterval:   5 * time.Millisecond,
		pollTimeout:    2 * time.Second,
	}
}

func TestCreateAndWait_ProcessingThenActive(t *testing.T) {
	var getCalls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		switch action {
		case "CreateAsset":
			// Verify the body carries Moderation.Skip and the group id.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"asset-123"}}`))
		case "GetAsset":
			mu.Lock()
			getCalls++
			n := getCalls
			mu.Unlock()
			status := "Processing"
			if n >= 2 {
				status = "Active"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"asset-123","Status":"` + status + `"}}`))
		default:
			t.Errorf("unexpected action %q", action)
		}
	}))
	defer srv.Close()

	cl := newTestClient(srv.URL)
	id, err := cl.CreateAndWait(context.Background(), "https://example.com/i.jpg", "Image")
	if err != nil {
		t.Fatalf("CreateAndWait: %v", err)
	}
	if id != "asset-123" {
		t.Errorf("id = %q, want asset-123", id)
	}
	mu.Lock()
	defer mu.Unlock()
	if getCalls < 2 {
		t.Errorf("expected at least 2 GetAsset polls, got %d", getCalls)
	}
}

func TestCreateAndWait_Failed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"asset-bad"}}`))
		case "GetAsset":
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"asset-bad","Status":"Failed"}}`))
		}
	}))
	defer srv.Close()

	cl := newTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "https://example.com/i.jpg", "Image")
	if err == nil || !strings.Contains(err.Error(), "processing failed") {
		t.Fatalf("expected processing failed error, got %v", err)
	}
}

func TestCreateAndWait_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1","Error":{"Code":"InvalidParameter","Message":"bad group"}},"Result":{}}`))
	}))
	defer srv.Close()

	cl := newTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "https://example.com/i.jpg", "Image")
	if err == nil || !strings.Contains(err.Error(), "InvalidParameter") {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestCreateAndWait_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			_, _ = w.Write([]byte(`{"Result":{"Id":"asset-slow"}}`))
		case "GetAsset":
			_, _ = w.Write([]byte(`{"Result":{"Id":"asset-slow","Status":"Processing"}}`))
		}
	}))
	defer srv.Close()

	cl := newTestClient(srv.URL)
	// Use a context deadline rather than the package timeout to keep the test fast.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := cl.CreateAndWait(ctx, "https://example.com/i.jpg", "Image")
	if err == nil {
		t.Fatal("expected timeout/cancel error, got nil")
	}
}
