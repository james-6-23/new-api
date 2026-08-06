package seedance3rd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/hot"
)

const (
	assetPollInterval = 2 * time.Second
	assetPollTimeout  = 60 * time.Second
)

// assetClient:上传单个媒体 URL → 轮询到就绪 → 返回 assetId。
type assetClient interface {
	CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error)
}

// ---- sdAssetClient: /v1/sd/assets(hc 变体) ----

type sdAssetClient struct {
	baseURL      string
	apiKey       string
	httpClient   *http.Client
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func (cl *sdAssetClient) interval() time.Duration {
	if cl.pollInterval > 0 {
		return cl.pollInterval
	}
	return assetPollInterval
}

func (cl *sdAssetClient) timeout() time.Duration {
	if cl.pollTimeout > 0 {
		return cl.pollTimeout
	}
	return assetPollTimeout
}

type sdCreateResp struct {
	Data struct {
		Id     string `json:"Id"`
		Status string `json:"Status"`
	} `json:"data"`
}

func (cl *sdAssetClient) CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error) {
	body, _ := common.Marshal(map[string]string{"URL": mediaURL, "Name": "newapi", "AssetType": assetType})
	var cr sdCreateResp
	if _, err := cl.do(ctx, http.MethodPost, "/v1/sd/assets", body, &cr); err != nil {
		return "", errors.Wrap(err, "sd create asset failed")
	}
	if cr.Data.Id == "" {
		return "", errors.New("sd create asset returned empty id")
	}
	deadline := time.Now().Add(cl.timeout())
	for {
		var gr sdCreateResp
		raw, err := cl.do(ctx, http.MethodGet, "/v1/sd/assets/"+cr.Data.Id, nil, &gr)
		if err != nil {
			return "", errors.Wrap(err, "sd get asset failed")
		}
		switch gr.Data.Status {
		case "Active":
			return cr.Data.Id, nil
		case "Failed":
			// 失败详情（审核拒绝原因等）在响应体扩展字段里，整段透传。
			return "", errors.Errorf("sd asset %s failed: %s", cr.Data.Id, raw)
		}
		if time.Now().After(deadline) {
			return "", errors.Errorf("sd asset %s not active within %s (last: %s)", cr.Data.Id, cl.timeout(), gr.Data.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.interval()):
		}
	}
}

// do 发起一次 Bearer 鉴权的 JSON 请求并解析到 out，返回响应体原文。
func (cl *sdAssetClient) do(ctx context.Context, method, path string, body []byte, out any) (string, error) {
	return doJSON(ctx, cl.httpClient, method, cl.baseURL+path, cl.apiKey, body, out)
}

// doJSON 是包内共用的 Bearer JSON 请求辅助；返回响应体原文（截断 512），
// 供 Failed 分支透传上游失败详情（审核拒绝原因等字段名不固定）。
func doJSON(ctx context.Context, client *http.Client, method, url, apiKey string, body []byte, out any) (string, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	preview := truncate(respBody, 512)
	if resp.StatusCode >= http.StatusBadRequest {
		return preview, errors.Errorf("upstream status %d: %s", resp.StatusCode, preview)
	}
	if out != nil {
		if err := common.Unmarshal(respBody, out); err != nil {
			return preview, errors.Wrapf(err, "unmarshal failed (body: %s)", preview)
		}
	}
	return preview, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// pickMedia 返回 content item 的媒体引用与 AssetType(本包独立实现,保隔离)。
func pickMedia(item *ContentItem) (*MediaURL, string) {
	switch {
	case item.ImageURL != nil:
		return item.ImageURL, "Image"
	case item.VideoURL != nil:
		return item.VideoURL, "Video"
	case item.AudioURL != nil:
		return item.AudioURL, "Audio"
	default:
		return nil, ""
	}
}

// ---- groupAssetClient: /v1/asset-groups + /v1/assets(260128 等变体) ----

type groupAssetClient struct {
	baseURL      string
	apiKey       string
	groupName    string
	httpClient   *http.Client
	pollInterval time.Duration
	pollTimeout  time.Duration
	groupID      string // 缓存的组 id;失效时清空重建
}

func (cl *groupAssetClient) interval() time.Duration {
	if cl.pollInterval > 0 {
		return cl.pollInterval
	}
	return assetPollInterval
}

func (cl *groupAssetClient) timeoutDur() time.Duration {
	if cl.pollTimeout > 0 {
		return cl.pollTimeout
	}
	return assetPollTimeout
}

func (cl *groupAssetClient) ensureGroup(ctx context.Context) (string, error) {
	if cl.groupID != "" {
		return cl.groupID, nil
	}
	body, _ := common.Marshal(map[string]string{"name": cl.groupName, "description": "new-api auto group"})
	var gr struct {
		ID string `json:"id"`
	}
	if _, err := doJSON(ctx, cl.httpClient, http.MethodPost, cl.baseURL+"/v1/asset-groups", cl.apiKey, body, &gr); err != nil {
		return "", errors.Wrap(err, "create asset group failed")
	}
	if gr.ID == "" {
		return "", errors.New("create asset group returned empty id")
	}
	cl.groupID = gr.ID
	return cl.groupID, nil
}

type groupCreateAssetResp struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

func (cl *groupAssetClient) CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error) {
	created, err := cl.createAssetWithGroup(ctx, mediaURL, assetType)
	if err != nil {
		// 组可能被上游轮转失效:清空缓存组、重建、重试一次。
		cl.groupID = ""
		created, err = cl.createAssetWithGroup(ctx, mediaURL, assetType)
		if err != nil {
			return "", err
		}
	}
	if created.ID == "" {
		return "", errors.New("create asset returned empty id")
	}

	deadline := time.Now().Add(cl.timeoutDur())
	for {
		var gr groupCreateAssetResp
		getBody, _ := common.Marshal(map[string]string{"asset_id": created.ID, "task_id": created.TaskID})
		raw, err := doJSON(ctx, cl.httpClient, http.MethodPost, cl.baseURL+"/v1/assets/get", cl.apiKey, getBody, &gr)
		if err != nil {
			return "", errors.Wrap(err, "get asset failed")
		}
		switch gr.Status {
		case "completed":
			return created.ID, nil
		case "failed":
			// 失败详情（审核拒绝原因等）在响应体扩展字段里，整段透传。
			return "", errors.Errorf("group asset %s failed: %s", created.ID, raw)
		}
		if time.Now().After(deadline) {
			return "", errors.Errorf("group asset %s not completed within %s (last: %s)", created.ID, cl.timeoutDur(), gr.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.interval()):
		}
	}
}

func (cl *groupAssetClient) createAssetWithGroup(ctx context.Context, mediaURL, assetType string) (groupCreateAssetResp, error) {
	groupID, err := cl.ensureGroup(ctx)
	if err != nil {
		return groupCreateAssetResp{}, err
	}
	body, _ := common.Marshal(map[string]string{
		"group_id": groupID, "url": mediaURL, "asset_type": assetType, "name": "newapi",
	})
	var cr groupCreateAssetResp
	if _, err := doJSON(ctx, cl.httpClient, http.MethodPost, cl.baseURL+"/v1/assets", cl.apiKey, body, &cr); err != nil {
		return groupCreateAssetResp{}, errors.Wrap(err, "create asset failed")
	}
	return cr, nil
}

const assetCacheTTL = 6 * time.Hour

// newAssetClient 按上游(带后缀)模型名选流程:-hc → sd/assets;其余 → 组模式。
func (a *TaskAdaptor) newAssetClient(model string, httpClient *http.Client) assetClient {
	base := a.baseURL
	if a.endpointOverride != "" {
		base = a.endpointOverride
	}
	if strings.HasSuffix(model, "-hc") {
		return &sdAssetClient{baseURL: base, apiKey: a.apiKey, httpClient: httpClient}
	}
	return &groupAssetClient{
		baseURL:    base,
		apiKey:     a.apiKey,
		groupName:  fmt.Sprintf("newapi-ch%d", a.channelId),
		httpClient: httpClient,
	}
}

// preuploadAssets 开关开启时,把 payload.Content 中每个公网媒体 URL 预上传素材库,
// 就地替换为 asset://<id>。开关关闭时零行为。
func (a *TaskAdaptor) preuploadAssets(c *gin.Context, payload *requestPayload) error {
	if !a.otherSettings.Seedance3rdAssetEnabled {
		return nil
	}
	httpClient, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return errors.Wrap(err, "create http client for seedance3rd asset upload failed")
	}
	cl := a.newAssetClient(payload.Model, httpClient)

	ctx := context.Background()
	if c != nil {
		ctx = c.Request.Context()
	}
	for i := range payload.Content {
		item := &payload.Content[i]
		media, assetType := pickMedia(item)
		if media == nil {
			continue
		}
		url := strings.TrimSpace(media.URL)
		if url == "" || strings.HasPrefix(url, "asset://") {
			continue
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return errors.Errorf("seedance3rd asset upload requires a public http(s) URL for %s (base64/data URIs unsupported)", item.Type)
		}
		key := assetCacheKey(a.channelId, payload.Model, url)
		if id, ok := getCachedAssetID(key); ok {
			media.URL = "asset://" + id
			continue
		}
		id, err := cl.CreateAndWait(ctx, url, assetType)
		if err != nil {
			return errors.Wrapf(err, "preupload %s to seedance3rd asset library failed", item.Type)
		}
		setCachedAssetID(key, id)
		media.URL = "asset://" + id
	}
	return nil
}

func assetCacheKey(channelId int, model, url string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", channelId, model, url)))
	return hex.EncodeToString(h[:])
}

var (
	assetIDCache     *cachex.HybridCache[string]
	assetIDCacheOnce sync.Once
)

func getAssetIDCache() *cachex.HybridCache[string] {
	assetIDCacheOnce.Do(func() {
		assetIDCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace:    cachex.Namespace("seedance3rd_asset"),
			Redis:        common.RDB,
			RedisEnabled: func() bool { return common.RedisEnabled && common.RDB != nil },
			RedisCodec:   cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, 10_000).
					WithTTL(assetCacheTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return assetIDCache
}

func getCachedAssetID(key string) (string, bool) {
	v, found, err := getAssetIDCache().Get(key)
	if err != nil || !found {
		return "", false
	}
	return v, true
}

func setCachedAssetID(key, id string) {
	if err := getAssetIDCache().SetWithTTL(key, id, assetCacheTTL); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("cache seedance3rd asset id failed: %s", err.Error()))
	}
}
