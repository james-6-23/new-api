package doubao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/hot"
)

// assetClient uploads one media URL to an asset library and waits until it is
// ready.  groupID is a per-call parameter so the factory does not need to know
// which group to use at construction time.
type assetClient interface {
	CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error)
	// CreateGroup creates a new asset group with the given name and returns its id.
	CreateGroup(ctx context.Context, name string) (string, error)
	// IsGroupExhausted returns true only for errors that indicate the group is
	// full, invalid, or otherwise unusable. Returns false for auth failures,
	// moderation rejections, and anything not positively recognised.
	IsGroupExhausted(err error) bool
}

// assetAPIError is returned by doAction when the upstream API responds with a
// structured error in ResponseMetadata.Error.  Callers can type-assert to
// inspect Code without string-matching the error message.
type assetAPIError struct {
	Code    string
	Message string
	Raw     string // masked copy of the full response body
	// MessageFromRawBody marks Message as the whole (masked) response body,
	// substituted because the body carried no parseable business message.
	//
	// Classification must never substring-match such a message: a body like
	// {"detail":"asset group upload capacity temporarily throttled"} contains
	// both "group" and "capacity" without being a group-full error at all, and
	// under a throttling burst that mints one junk group and one channel-row
	// write per concurrent request. This is the same hazard the HTTP-200 branch
	// of cloudwiseAssetClient.post already refuses to create.
	//
	// The flag lives on the message rather than on a synthesized "HTTP<status>"
	// code on purpose: a body such as {"code":{"x":1},"message":"asset group is
	// full"} also lands on a synthesized code, yet its message is genuine and
	// must still classify.
	MessageFromRawBody bool
}

func (e *assetAPIError) Error() string {
	// Either half can be empty, and each degenerate form used to render a dangling
	// colon that reads like a truncated log line:
	//   - empty Message: the cloudwise status-code fallback leaves it empty for an
	//     empty error body, which rendered as "HTTP500: ".
	//   - empty Code: a business error arriving at HTTP 200 has no status-code
	//     fallback to borrow, so {"message":"asset group is full"} rendered as
	//     ": asset group is full".
	// The fully populated form stays "Code: Message" — tests and log greps rely on it.
	switch {
	case e.Message == "":
		return e.Code
	case e.Code == "":
		return e.Message
	default:
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
}

// newAssetClient constructs an asset client from the adaptor's channel settings.
// Provider is selected by s.ResolveAssetProvider():
//   - dto.AssetProviderCloudwise: cloudwiseAssetClient (Bearer auth via channel API key/base URL)
//   - anything else (including empty): bytePlusAssetClient (Volcengine AK/SK signing)
func (a *TaskAdaptor) newAssetClient() (assetClient, error) {
	s := a.otherSettings
	httpClient, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return nil, errors.Wrap(err, "create http client for asset upload failed")
	}

	switch s.ResolveAssetProvider() {
	case dto.AssetProviderCloudwise:
		baseURL := a.baseURL
		if a.endpointOverride != "" {
			baseURL = a.endpointOverride
		}
		if a.apiKey == "" {
			return nil, errors.New("cloudwise asset upload enabled but channel API key is missing")
		}
		// skipModeration is shared config (default true); cloudwise honours it via
		// Moderation.Strategy=Skip exactly like the byteplus path. region/project
		// are byteplus-only and unused here.
		_, _, skipMod := s.ResolveBytePlusAsset()
		return &cloudwiseAssetClient{
			baseURL:        baseURL,
			apiKey:         a.apiKey,
			skipModeration: skipMod,
			httpClient:     httpClient,
		}, nil
	default:
		if s.BytePlusAccessKey == "" || s.BytePlusSecretKey == "" {
			return nil, errors.New("byteplus asset upload enabled but access_key/secret_key is missing in channel settings")
		}
		region, project, skipMod := s.ResolveBytePlusAsset()
		return &bytePlusAssetClient{
			ak:             s.BytePlusAccessKey,
			sk:             s.BytePlusSecretKey,
			region:         region,
			projectName:    project,
			skipModeration: skipMod,
			httpClient:     httpClient,
			endpoint:       a.endpointOverride,
		}, nil
	}
}

// assetCacheTTL is the URL→assetId mapping TTL.  Short enough to cover a
// typical batch request, conservative enough to avoid stale mappings after
// an asset expires upstream.
const assetCacheTTL = 6 * time.Hour

// preuploadAssets replaces public media URLs in payload.Content with
// asset://<id> references by uploading them to the BytePlus asset library.
// When the feature flag is disabled it is a no-op.
func (a *TaskAdaptor) preuploadAssets(c *gin.Context, payload *requestPayload) error {
	s := a.otherSettings
	if !s.BytePlusAssetEnabled {
		return nil
	}

	cl, err := a.newAssetClient()
	if err != nil {
		return err
	}

	_, project, _ := s.ResolveBytePlusAsset()
	provider := s.ResolveAssetProvider()

	// ResolveAssetGroupId, not the raw field: a group id minted by the other asset
	// library is meaningless here and must be treated as absent (see FIX 4 in
	// dto.ChannelOtherSettings.ResolveAssetGroupId).
	groupID := s.ResolveAssetGroupId()

	ctx := c.Request.Context()
	// newGroupCreated tracks whether we already created a new group during this
	// request.  We cap at one new group per request to avoid runaway creation.
	newGroupCreated := false

	// ensureGroup bootstraps a group id when the channel has none usable (blank
	// field, or one minted by the other provider).  A blank group id is the normal
	// first-run state for a new channel, so bootstrap must be explicit rather than
	// riding on the upstream rejecting groupId:"" with a code that happens to be in
	// the (admittedly guessed) exhaustion tables.
	//
	// Bootstrap is deliberately NOT a rotation: it does not set newGroupCreated, so
	// a first request that bootstraps a group still keeps its one rotation in hand
	// if that fresh group turns out to be unusable.
	//
	// It runs lazily, right before the first real upload, so a request whose media
	// are all cached (or that carries no media at all) never creates a group.
	ensureGroup := func() error {
		if groupID != "" {
			return nil
		}
		newGroupID, createErr := cl.CreateGroup(ctx, assetGroupName(a.channelId))
		if createErr != nil {
			return errors.Wrap(createErr, "create asset group failed (no group id configured)")
		}
		groupID = newGroupID
		// Persistence failure is non-fatal, exactly as on the rotation path below.
		a.persistGroupID(newGroupID)
		return nil
	}

	for i := range payload.Content {
		item := &payload.Content[i]
		media, assetType := pickMedia(item)
		if media == nil {
			continue // text and other non-media items
		}
		url := strings.TrimSpace(media.URL)
		if url == "" {
			continue
		}
		// Already an asset reference — idempotent skip.
		if strings.HasPrefix(url, "asset://") {
			continue
		}
		// CreateAsset only accepts public HTTP(S) URLs; base64/data URIs are unsupported.
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return errors.Errorf("asset upload requires a public http(s) URL, got unsupported input for %s (base64/data URIs are not supported)", item.Type)
		}

		cacheKey := assetCacheKey(a.channelId, provider, project, url)
		if assetID, ok := getCachedAssetID(cacheKey); ok {
			media.URL = "asset://" + assetID
			continue
		}

		if err := ensureGroup(); err != nil {
			return errors.Wrapf(err, "preupload %s", item.Type)
		}

		assetID, uploadErr := cl.CreateAndWait(ctx, groupID, url, assetType)
		if uploadErr != nil {
			// Attempt group rotation on first exhaustion within this request.
			if cl.IsGroupExhausted(uploadErr) && !newGroupCreated {
				newGroupID, createErr := cl.CreateGroup(ctx, assetGroupName(a.channelId))
				if createErr != nil {
					return errors.Wrapf(createErr, "preupload %s: group exhausted and group creation failed", item.Type)
				}
				newGroupCreated = true
				groupID = newGroupID

				// Persist the new group id so subsequent requests reuse it.
				// Two concurrent requests can both find the group exhausted and
				// both create new groups — the last write wins in the DB and
				// cache, which is safe: both new groups are valid, the next
				// request will simply use whichever id was persisted last.
				// Persistence failure is non-fatal: we carry on with the
				// in-memory groupID for this request.
				a.persistGroupID(newGroupID)

				// Retry the upload into the fresh group.
				assetID, uploadErr = cl.CreateAndWait(ctx, groupID, url, assetType)
				if uploadErr != nil {
					return errors.Wrapf(uploadErr, "preupload %s to asset library failed (after group rotation)", item.Type)
				}
			} else {
				return errors.Wrapf(uploadErr, "preupload %s to asset library failed", item.Type)
			}
		}

		setCachedAssetID(cacheKey, assetID)
		media.URL = "asset://" + assetID
	}
	return nil
}

// assetGroupName is the name given to groups this gateway creates for a channel.
func assetGroupName(channelId int) string {
	return fmt.Sprintf("newapi-ch%d", channelId)
}

// persistGroupID saves newGroupID to the channel's OtherSettings in the DB and
// updates the in-memory channel cache.  The provider that minted the id is
// recorded alongside it so a later provider switch does not hand the id to an
// asset library that cannot resolve it.  Errors are logged as warnings and do
// not fail the caller.
func (a *TaskAdaptor) persistGroupID(newGroupID string) {
	provider := a.otherSettings.ResolveAssetProvider()

	// Always update the adaptor's in-memory copy so this request and any
	// subsequent in-process use sees the new group id regardless of DB state.
	a.otherSettings.BytePlusAssetGroupId = newGroupID
	a.otherSettings.AssetGroupProvider = provider

	if model.DB == nil {
		logger.LogWarn(context.Background(), fmt.Sprintf(
			"asset library: DB not initialised, skipping group_id persistence for channel %d", a.channelId))
		return
	}

	ch, err := model.GetChannelById(a.channelId, true)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf(
			"asset library: cannot load channel %d for group_id persistence: %v", a.channelId, err))
		return
	}

	settings := ch.GetOtherSettings()
	settings.BytePlusAssetGroupId = newGroupID
	settings.AssetGroupProvider = provider
	ch.SetOtherSettings(settings)

	if err := ch.Save(); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf(
			"asset library: failed to persist new group_id for channel %d: %v", a.channelId, err))
		return
	}

	// Also update the in-memory cache so the next in-process request picks up
	// the new group id without a DB round-trip.  CacheUpdateChannel replaces
	// the whole *Channel pointer, so we pass the full struct we just saved.
	model.CacheUpdateChannel(ch)
}

// pickMedia returns the media reference and its AssetType string for a content item.
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

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// assetCacheKey builds a cache key for a URL→assetId mapping.
//
// provider is part of the key because asset ids are only resolvable by the asset
// library that minted them: flipping a channel's asset_provider would otherwise
// keep serving BytePlus ids to cloudwise (or the reverse) for the rest of the TTL.
// Changing this key format costs one cold-cache window, which is expected.
//
// groupId is intentionally excluded: BytePlus asset ids are globally unique
// and remain valid after their group rotates.  If groupId were in the key, a
// group rotation would cause every previously-cached URL to miss and get
// re-uploaded into the fresh group, immediately consuming the new group's
// quota with duplicates of assets that already exist — the worst possible
// time to burn quota.  project is retained because the same URL uploaded to
// different projects yields a different asset id (different namespace/access
// scope).
func assetCacheKey(channelId int, provider, project, url string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", channelId, provider, project, url)))
	return hex.EncodeToString(h[:])
}

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
