package doubao

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
	"github.com/QuantumNous/new-api/common/volcsign"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/hot"
)

// BytePlus 素材库（CreateAsset / GetAsset）调用封装。
//
// 文档要点：
//   - CreateAsset 异步，只接受公网 URL（不支持 base64），需指定已存在的 GroupId 与 ProjectName。
//   - 上传后需轮询 GetAsset 直到 Status=Active 才能用，Failed 表示处理失败。
//   - 鉴权为 Volcengine 风格 HMAC-SHA256 AK/SK 签名（service=ark，region 如 ap-southeast-1）。
//
// 顶层 OpenAPI 形如：POST https://ark.{region}.byteplusapi.com/?Action=CreateAsset&Version=2024-01-01

const (
	bytePlusAPIVersion = "2024-01-01"
	bytePlusService    = "ark"

	// 轮询参数：CreateAsset 异步无 SLA，这里给一个保守的上限。
	bytePlusPollInterval = 2 * time.Second
	bytePlusPollTimeout  = 60 * time.Second
)

// bytePlusAssetClient 封装"上传单个媒体 URL → 轮询到 Active → 返回 assetId"。
type bytePlusAssetClient struct {
	ak, sk         string
	region         string
	projectName    string
	groupId        string
	skipModeration bool
	httpClient     *http.Client

	// endpoint 为 OpenAPI 根地址（含 scheme 与 host，不含 path/query）。
	// 留空时按 region 推导为 https://ark.{region}.byteplusapi.com。
	// 单元测试通过设置此字段指向 httptest.Server。
	endpoint string

	// pollInterval / pollTimeout 为零时回退到包级默认值（生产路径不设置，
	// 单元测试用小值避免真实等待）。
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func (cl *bytePlusAssetClient) intervalOrDefault() time.Duration {
	if cl.pollInterval > 0 {
		return cl.pollInterval
	}
	return bytePlusPollInterval
}

func (cl *bytePlusAssetClient) timeoutOrDefault() time.Duration {
	if cl.pollTimeout > 0 {
		return cl.pollTimeout
	}
	return bytePlusPollTimeout
}

func (cl *bytePlusAssetClient) baseEndpoint() string {
	if cl.endpoint != "" {
		return strings.TrimRight(cl.endpoint, "/")
	}
	return fmt.Sprintf("https://ark.%s.byteplusapi.com", cl.region)
}

// createAssetRequest 是 CreateAsset 的请求体。
type createAssetRequest struct {
	GroupId     string                 `json:"GroupId"`
	URL         string                 `json:"URL"`
	AssetType   string                 `json:"AssetType"`
	Moderation  *createAssetModeration `json:"Moderation,omitempty"`
	ProjectName string                 `json:"ProjectName,omitempty"`
}

type createAssetModeration struct {
	Strategy string `json:"Strategy"`
}

// bytePlusEnvelope 是 Volcengine OpenAPI 的统一响应外壳。
type bytePlusEnvelope struct {
	ResponseMetadata struct {
		RequestId string `json:"RequestId"`
		Error     *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
	} `json:"ResponseMetadata"`
	Result struct {
		Id     string `json:"Id"`
		Status string `json:"Status"`
	} `json:"Result"`
}

// CreateAndWait 上传单个素材并轮询到 Active，返回 assetId。
func (cl *bytePlusAssetClient) CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error) {
	id, err := cl.createAsset(ctx, mediaURL, assetType)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("byteplus CreateAsset returned empty asset id")
	}

	deadline := time.Now().Add(cl.timeoutOrDefault())
	for {
		status, err := cl.getAsset(ctx, id)
		if err != nil {
			return "", err
		}
		switch status {
		case "Active":
			return id, nil
		case "Failed":
			return "", errors.Errorf("byteplus asset %s processing failed", id)
		}
		// Processing / 其它中间态：继续轮询。
		if time.Now().After(deadline) {
			return "", errors.Errorf("byteplus asset %s not active within %s (last status: %s)", id, cl.timeoutOrDefault(), status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.intervalOrDefault()):
		}
	}
}

// createAsset 调用 CreateAsset，返回 asset id。
func (cl *bytePlusAssetClient) createAsset(ctx context.Context, mediaURL, assetType string) (string, error) {
	reqBody := createAssetRequest{
		GroupId:     cl.groupId,
		URL:         mediaURL,
		AssetType:   assetType,
		ProjectName: cl.projectName,
	}
	if cl.skipModeration {
		reqBody.Moderation = &createAssetModeration{Strategy: "Skip"}
	}

	env, err := cl.doAction(ctx, "CreateAsset", reqBody)
	if err != nil {
		return "", errors.Wrap(err, "byteplus CreateAsset failed")
	}
	return env.Result.Id, nil
}

// getAsset 调用 GetAsset，返回 Status。
func (cl *bytePlusAssetClient) getAsset(ctx context.Context, id string) (string, error) {
	reqBody := map[string]any{
		"Id":          id,
		"ProjectName": cl.projectName,
	}
	env, err := cl.doAction(ctx, "GetAsset", reqBody)
	if err != nil {
		return "", errors.Wrap(err, "byteplus GetAsset failed")
	}
	return env.Result.Status, nil
}

// doAction 发起一次签名的 OpenAPI 调用并解析统一响应外壳。
func (cl *bytePlusAssetClient) doAction(ctx context.Context, action string, payload any) (*bytePlusEnvelope, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}

	uri := fmt.Sprintf("%s/?Action=%s&Version=%s", cl.baseEndpoint(), action, bytePlusAPIVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if err := volcsign.SignRequest(req, body, cl.ak, cl.sk, cl.region, bytePlusService); err != nil {
		return nil, errors.Wrap(err, "sign request failed")
	}

	client := cl.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var env bytePlusEnvelope
	if err := common.Unmarshal(respBody, &env); err != nil {
		return nil, errors.Wrapf(err, "unmarshal response failed (status %d, body: %s)", resp.StatusCode, truncate(respBody, 512))
	}
	if env.ResponseMetadata.Error != nil && env.ResponseMetadata.Error.Code != "" {
		return nil, errors.Errorf("%s: %s", env.ResponseMetadata.Error.Code, env.ResponseMetadata.Error.Message)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, errors.Errorf("upstream returned status %d: %s", resp.StatusCode, truncate(respBody, 512))
	}
	return &env, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// assetCacheTTL 为 URL→assetId 映射缓存有效期。取较短值，覆盖典型批量请求即可，
// 避免素材实际过期后仍命中旧映射。
const assetCacheTTL = 6 * time.Hour

// preuploadAssets 在开关开启时，把 payload.Content 中每个公网媒体 URL 预上传到
// BytePlus 素材库，并就地替换为 asset://<id>。开关关闭时直接返回（零行为变更）。
func (a *TaskAdaptor) preuploadAssets(c *gin.Context, payload *requestPayload) error {
	s := a.otherSettings
	if !s.BytePlusAssetEnabled {
		return nil
	}
	if s.BytePlusAccessKey == "" || s.BytePlusSecretKey == "" || s.BytePlusAssetGroupId == "" {
		return errors.New("byteplus asset upload enabled but access_key/secret_key/group_id is missing in channel settings")
	}

	region, project, skipMod := s.ResolveBytePlusAsset()

	httpClient, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return errors.Wrap(err, "create http client for byteplus asset upload failed")
	}

	cl := &bytePlusAssetClient{
		ak:             s.BytePlusAccessKey,
		sk:             s.BytePlusSecretKey,
		region:         region,
		projectName:    project,
		groupId:        s.BytePlusAssetGroupId,
		skipModeration: skipMod,
		httpClient:     httpClient,
		endpoint:       a.endpointOverride,
	}

	ctx := c.Request.Context()
	for i := range payload.Content {
		item := &payload.Content[i]
		media, assetType := pickMedia(item)
		if media == nil {
			continue // text 等无媒体条目
		}
		url := strings.TrimSpace(media.URL)
		if url == "" {
			continue
		}
		// 已是 asset:// 则幂等跳过（允许调用方自带 assetId）。
		if strings.HasPrefix(url, "asset://") {
			continue
		}
		// CreateAsset 只接受公网 URL，不支持 base64 / data:。
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return errors.Errorf("byteplus asset upload requires a public http(s) URL, got unsupported input for %s (base64/data URIs are not supported)", item.Type)
		}

		cacheKey := assetCacheKey(a.channelId, cl.groupId, project, url)
		if assetID, ok := getCachedAssetID(cacheKey); ok {
			media.URL = "asset://" + assetID
			continue
		}

		assetID, err := cl.CreateAndWait(ctx, url, assetType)
		if err != nil {
			return errors.Wrapf(err, "preupload %s to byteplus asset library failed", item.Type)
		}
		setCachedAssetID(cacheKey, assetID)
		media.URL = "asset://" + assetID
	}
	return nil
}

// pickMedia 返回 content item 中的媒体引用与对应 AssetType。
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

func assetCacheKey(channelId int, groupId, project, url string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", channelId, groupId, project, url)))
	return hex.EncodeToString(h[:])
}

// assetIDCache 复用项目统一缓存层（Redis 命中优先，否则内存回退），
// 存 URL→assetId 映射，避免对同一参考媒体重复执行异步上传+轮询。
var (
	assetIDCache     *cachex.HybridCache[string]
	assetIDCacheOnce sync.Once
)

func getAssetIDCache() *cachex.HybridCache[string] {
	assetIDCacheOnce.Do(func() {
		assetIDCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace: cachex.Namespace("byteplus_asset"),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.StringCodec{},
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

func setCachedAssetID(key, assetID string) {
	if err := getAssetIDCache().SetWithTTL(key, assetID, assetCacheTTL); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("cache byteplus asset id failed: %s", err.Error()))
	}
}
