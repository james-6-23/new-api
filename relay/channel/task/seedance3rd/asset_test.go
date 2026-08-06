package seedance3rd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
)

func TestSDAssetClient_CreateAndWait(t *testing.T) {
	var createHits, getHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sd/assets":
			createHits++
			w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","base_resp":{"status_code":0}}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/sd/assets/"):
			getHits++
			// 第一次 Processing,第二次 Active,验证轮询逻辑
			if getHits == 1 {
				w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","Status":"Processing"}}`))
			} else {
				w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","Status":"Active"}}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cl := &sdAssetClient{
		baseURL:      srv.URL,
		apiKey:       "sk-x",
		httpClient:   srv.Client(),
		pollInterval: time.Millisecond,
		pollTimeout:  time.Second,
	}
	id, err := cl.CreateAndWait(context.Background(), "https://x/a.png", "Image")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != "asset-abc" {
		t.Fatalf("id = %q, want asset-abc", id)
	}
	if createHits != 1 || getHits != 2 {
		t.Fatalf("createHits=%d getHits=%d, want 1/2", createHits, getHits)
	}
}

func TestGroupAssetClient_CreateAndWait(t *testing.T) {
	var groupCreates, assetCreates, getHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/asset-groups":
			groupCreates++
			w.Write([]byte(`{"id":"group-abc"}`))
		case "/v1/assets":
			assetCreates++
			w.Write([]byte(`{"id":"asset-xyz","task_id":"t-1","status":"processing"}`))
		case "/v1/assets/get":
			getHits++
			if getHits == 1 {
				w.Write([]byte(`{"id":"asset-xyz","status":"processing"}`))
			} else {
				w.Write([]byte(`{"id":"asset-xyz","status":"completed"}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cl := &groupAssetClient{
		baseURL:      srv.URL,
		apiKey:       "sk-x",
		groupName:    "newapi-ch7",
		httpClient:   srv.Client(),
		pollInterval: time.Millisecond,
		pollTimeout:  time.Second,
	}
	id, err := cl.CreateAndWait(context.Background(), "https://x/a.png", "Image")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != "asset-xyz" {
		t.Fatalf("id = %q", id)
	}
	if groupCreates != 1 || assetCreates != 1 || getHits != 2 {
		t.Fatalf("groupCreates=%d assetCreates=%d getHits=%d", groupCreates, assetCreates, getHits)
	}
}

func TestGroupAssetClient_RecreatesGroupOnFailure(t *testing.T) {
	var groupCreates, assetCreates int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/asset-groups":
			groupCreates++
			w.Write([]byte(`{"id":"group-new"}`))
		case "/v1/assets":
			assetCreates++
			if assetCreates == 1 {
				// 首次:组失效
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"asset group not found"}`))
				return
			}
			w.Write([]byte(`{"id":"asset-ok","task_id":"t","status":"processing"}`))
		case "/v1/assets/get":
			w.Write([]byte(`{"id":"asset-ok","status":"completed"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cl := &groupAssetClient{
		baseURL: srv.URL, apiKey: "sk-x", groupName: "newapi-ch7",
		httpClient: srv.Client(), pollInterval: time.Millisecond, pollTimeout: time.Second,
		groupID: "group-stale", // 预置一个失效组,验证清空重建
	}
	id, err := cl.CreateAndWait(context.Background(), "https://x/a.png", "Image")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != "asset-ok" {
		t.Fatalf("id = %q", id)
	}
	if groupCreates != 1 || assetCreates != 2 {
		t.Fatalf("groupCreates=%d assetCreates=%d, want 1/2 (recreate+retry)", groupCreates, assetCreates)
	}
}

func TestPreuploadAssets_DisabledIsNoop(t *testing.T) {
	a := &TaskAdaptor{}
	a.otherSettings = dto.ChannelOtherSettings{Seedance3rdAssetEnabled: false}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://x/a.png"}},
	}}
	if err := a.preuploadAssets(nil, payload); err != nil {
		t.Fatalf("err: %v", err)
	}
	if payload.Content[0].ImageURL.URL != "https://x/a.png" {
		t.Fatalf("url mutated while disabled: %q", payload.Content[0].ImageURL.URL)
	}
}

func TestNewAssetClient_SelectsByHCSuffix(t *testing.T) {
	a := &TaskAdaptor{baseURL: "https://x", apiKey: "sk"}
	if _, ok := a.newAssetClient("dreamina-seedance-2-0-260128-hc", nil).(*sdAssetClient); !ok {
		t.Fatalf("-hc should select sdAssetClient")
	}
	if _, ok := a.newAssetClient("dreamina-seedance-2-0-260128", nil).(*groupAssetClient); !ok {
		t.Fatalf("non-hc should select groupAssetClient")
	}
}
