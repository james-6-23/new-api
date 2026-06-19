package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/storage"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// mediaTransferTimeout is the HTTP timeout for downloading upstream media (5 minutes).
const mediaTransferTimeout = 5 * time.Minute

// GetUpstreamMediaURL returns the downloadable URL and extra request headers
// for the given task's upstream media, based on the channel type.
// This mirrors the branching logic in controller/video_proxy.go but is
// designed for background transfer rather than real-time proxying.
func GetUpstreamMediaURL(task *model.Task, channel *model.Channel) (mediaURL string, headers map[string]string, err error) {
	if task == nil || channel == nil {
		return "", nil, fmt.Errorf("task and channel must not be nil")
	}

	baseURL := channel.GetBaseURL()
	headers = make(map[string]string)

	switch channel.Type {
	case constant.ChannelTypeGemini:
		apiKey := task.PrivateData.Key
		if apiKey == "" {
			return "", nil, fmt.Errorf("missing stored API key for Gemini task %s", task.TaskID)
		}
		mediaURL, err = getGeminiMediaURL(channel, task, apiKey)
		if err != nil {
			return "", nil, fmt.Errorf("failed to resolve Gemini media URL for task %s: %w", task.TaskID, err)
		}
		headers["x-goog-api-key"] = apiKey

	case constant.ChannelTypeVertexAi:
		mediaURL, err = getVertexMediaURL(channel, task)
		if err != nil {
			return "", nil, fmt.Errorf("failed to resolve Vertex media URL for task %s: %w", task.TaskID, err)
		}

	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		mediaURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
		headers["Authorization"] = "Bearer " + channel.Key

	case constant.ChannelTypeAIGCVideo:
		if baseURL == "" {
			return "", nil, fmt.Errorf("AIGC Video channel %d has no base URL", channel.Id)
		}
		if strings.Contains(channel.Key, "|") {
			// HMAC auth (SubAppId|SecretId|SecretKey): direct AIGC gateway
			mediaURL = fmt.Sprintf("%s/v1/files/video?id=%s", baseURL, task.GetUpstreamTaskID())
		} else {
			// Bearer token (sk-xxx): upstream is likely another new-api instance
			mediaURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
			headers["Authorization"] = "Bearer " + channel.Key
		}

	default:
		// For all other channels, use the stored result URL directly
		mediaURL = task.GetResultURL()
	}

	mediaURL = strings.TrimSpace(mediaURL)
	if mediaURL == "" {
		return "", nil, fmt.Errorf("media URL is empty for task %s", task.TaskID)
	}

	return mediaURL, headers, nil
}

// TransferTaskMedia transfers the task's result media from the upstream
// provider to CloudPaste storage. It is safe to call from a goroutine.
func TransferTaskMedia(task *model.Task) error {
	ctx := context.Background()

	common.SysLog(fmt.Sprintf("starting media transfer for task %s (channel: %d, platform: %s)", task.TaskID, task.ChannelId, task.Platform))

	// 1. Check if CloudPaste is enabled
	if !setting.CloudPasteEnabled {
		return fmt.Errorf("CloudPaste is not enabled")
	}
	if setting.CloudPasteBaseURL == "" || setting.CloudPasteAPIKey == "" {
		return fmt.Errorf("CloudPaste configuration is incomplete")
	}

	// 2. Skip if already transferred successfully
	if task.PrivateData.StorageStatus == model.StorageStatusSuccess {
		logger.LogInfo(ctx, fmt.Sprintf("Task %s already transferred, skipping", task.TaskID))
		return nil
	}

	// 3. Update status to uploading
	task.SetStorageResult("", "", model.StorageStatusUploading, "")
	if err := task.UpdateStorageResult(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Failed to update storage status for task %s: %s", task.TaskID, err.Error()))
	}

	// 4. Get channel information
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		transferErr := fmt.Sprintf("failed to get channel %d for task %s: %s", task.ChannelId, task.TaskID, err.Error())
		markTransferFailed(task, transferErr)
		return errors.New(transferErr)
	}

	// 5. Get upstream URL and headers
	mediaURL, extraHeaders, err := GetUpstreamMediaURL(task, channel)
	if err != nil {
		transferErr := fmt.Sprintf("failed to get upstream URL for task %s: %s", task.TaskID, err.Error())
		markTransferFailed(task, transferErr)
		return errors.New(transferErr)
	}

	// Skip data: URIs – they are inline and cannot be streamed from upstream
	if strings.HasPrefix(mediaURL, "data:") {
		transferErr := fmt.Sprintf("data URI media not supported for transfer: task %s", task.TaskID)
		markTransferFailed(task, transferErr)
		return errors.New(transferErr)
	}

	// SSRF protection
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(
		mediaURL,
		fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp,
		fetchSetting.DomainFilterMode,
		fetchSetting.IpFilterMode,
		fetchSetting.DomainList,
		fetchSetting.IpList,
		fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain,
	); err != nil {
		transferErr := fmt.Sprintf("media URL blocked for task %s: %v", task.TaskID, err)
		markTransferFailed(task, transferErr)
		return errors.New(transferErr)
	}

	// 6. Fetch upstream media with optional proxy
	proxy := channel.GetSetting().Proxy
	httpClient, err := GetHttpClientWithProxy(proxy)
	if err != nil {
		transferErr := fmt.Sprintf("failed to create HTTP client for task %s: %s", task.TaskID, err.Error())
		markTransferFailed(task, transferErr)
		return errors.New(transferErr)
	}

	// Single timeout context for both download and upload
	transferCtx, cancel := context.WithTimeout(ctx, mediaTransferTimeout)
	defer cancel()

	upstreamResp, err := doUpstreamFetch(transferCtx, httpClient, mediaURL, extraHeaders, channel, task)
	if err != nil {
		transferErr := fmt.Sprintf("failed to fetch upstream media for task %s: %s", task.TaskID, err.Error())
		markTransferFailed(task, transferErr)
		return errors.New(transferErr)
	}
	defer upstreamResp.Body.Close()

	// 7. Determine content type
	contentType := upstreamResp.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = inferContentType(task)
	}

	// 8. Generate filename
	filename := generateFilename(task, contentType)

	// 9. Stream upload to CloudPaste
	cpClient := storage.NewCloudPasteClient(
		setting.CloudPasteBaseURL,
		setting.CloudPasteAPIKey,
		setting.CloudPasteStorageConfigID,
	)

	logger.LogInfo(ctx, fmt.Sprintf("Starting CloudPaste transfer for task %s, file=%s, contentType=%s, size=%d",
		task.TaskID, filename, contentType, upstreamResp.ContentLength))

	result, err := cpClient.StreamUpload(transferCtx, filename, contentType, upstreamResp.Body, upstreamResp.ContentLength)

	// 10/11. Update storage result
	if err != nil {
		transferErr := fmt.Sprintf("CloudPaste upload failed for task %s: %s", task.TaskID, err.Error())
		markTransferFailed(task, transferErr)
		return errors.New(transferErr)
	}

	downloadURL := result.DownloadURL
	if downloadURL == "" {
		downloadURL = result.URL
	}
	previewURL := result.PreviewURL

	task.SetStorageResult(downloadURL, previewURL, model.StorageStatusSuccess, "")

	// 12. Persist
	if err := task.UpdateStorageResult(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Failed to persist storage result for task %s: %s", task.TaskID, err.Error()))
		return fmt.Errorf("failed to persist storage result: %w", err)
	}

	common.SysLog(fmt.Sprintf("media transfer completed for task %s: storage_url=%s", task.TaskID, downloadURL))
	logger.LogInfo(ctx, fmt.Sprintf("Task %s media transferred successfully: url=%s", task.TaskID, downloadURL))
	return nil
}

// doUpstreamFetch performs the HTTP GET to download upstream media.
// For AIGC Video channels, it tries unauthenticated first, then falls back to authenticated.
func doUpstreamFetch(ctx context.Context, client *http.Client, mediaURL string, headers map[string]string, channel *model.Channel, task *model.Task) (*http.Response, error) {
	parsedURL, err := url.Parse(mediaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse media URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create upstream request: %w", err)
	}

	// Set extra headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}

	// For AIGC Video: if unauthenticated request fails, try with Bearer token
	if channel.Type == constant.ChannelTypeAIGCVideo && resp.StatusCode >= 400 {
		resp.Body.Close()

		baseURL := channel.GetBaseURL()
		authURL := fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())

		authParsedURL, err := url.Parse(authURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse auth fallback URL: %w", err)
		}

		authReq, err := http.NewRequestWithContext(ctx, http.MethodGet, authParsedURL.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create auth fallback request: %w", err)
		}
		authReq.Header.Set("Authorization", "Bearer "+channel.Key)

		resp, err = client.Do(authReq)
		if err != nil {
			return nil, fmt.Errorf("auth fallback request failed: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned status %d: %s", resp.StatusCode, string(body))
	}

	return resp, nil
}

// inferContentType guesses the content type based on the task's action/platform.
func inferContentType(task *model.Task) string {
	action := strings.ToLower(task.Action)
	platform := strings.ToLower(string(task.Platform))

	// Check action keywords
	switch {
	case strings.Contains(action, "video") || strings.Contains(platform, "video"):
		return "video/mp4"
	case strings.Contains(action, "image") || strings.Contains(action, "img"):
		return "image/png"
	case strings.Contains(action, "audio") || strings.Contains(action, "music") || strings.Contains(action, "song"):
		return "audio/mpeg"
	}

	return "application/octet-stream"
}

// generateFilename creates a filename for the uploaded media.
func generateFilename(task *model.Task, contentType string) string {
	ext := extFromContentType(contentType)
	return fmt.Sprintf("%s%s", task.TaskID, ext)
}

// extFromContentType returns a file extension based on the actual content type.
func extFromContentType(ct string) string {
	switch {
	case strings.HasPrefix(ct, "video/"):
		return ".mp4"
	case strings.HasPrefix(ct, "image/"):
		return ".png"
	case strings.HasPrefix(ct, "audio/"):
		return ".mp3"
	default:
		return ".bin"
	}
}

// markTransferFailed logs the error and updates the task's storage status to failed.
func markTransferFailed(task *model.Task, errMsg string) {
	ctx := context.Background()
	logger.LogError(ctx, errMsg)
	task.SetStorageResult("", "", model.StorageStatusFailed, errMsg)
	if err := task.UpdateStorageResult(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Failed to persist storage failure for task %s: %s", task.TaskID, err.Error()))
	}
}

// --- Gemini / Vertex helper functions for media transfer ---
// These mirror the logic in controller/video_proxy_gemini.go but live in the
// service package to avoid cross-package dependencies.

func getGeminiMediaURL(channel *model.Channel, task *model.Task, apiKey string) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("invalid channel or task")
	}

	// Try extracting URL from task data (should already be populated when task reached SUCCESS)
	if u := extractVideoURLFromTaskData(task); u != "" {
		return ensureGeminiAPIKey(u, apiKey), nil
	}

	// Fall back to stored result URL
	if u := strings.TrimSpace(task.GetResultURL()); u != "" {
		return ensureGeminiAPIKey(u, apiKey), nil
	}

	return "", fmt.Errorf("gemini video url not found in task data for task %s", task.TaskID)
}

func getVertexMediaURL(channel *model.Channel, task *model.Task) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("invalid channel or task")
	}

	// Use stored result URL if it's not a self-referencing proxy URL
	if u := strings.TrimSpace(task.GetResultURL()); u != "" && !strings.Contains(u, "/v1/videos/"+task.TaskID+"/content") {
		return u, nil
	}

	// Try extracting from task data
	if u := extractVideoURLFromTaskData(task); u != "" {
		return u, nil
	}

	return "", fmt.Errorf("vertex video url not found in task data for task %s", task.TaskID)
}

func getVertexKey(channel *model.Channel, task *model.Task) string {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key
		}
	}
	if channel == nil {
		return ""
	}
	keys := channel.GetKeys()
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			return key
		}
	}
	return strings.TrimSpace(channel.Key)
}

// extractVideoURLFromTaskData tries to extract a video URL from the task's Data field.
func extractVideoURLFromTaskData(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	var payload map[string]any
	if err := common.Unmarshal(task.Data, &payload); err != nil {
		return ""
	}
	return extractVideoURLFromMap(payload)
}

// extractVideoURLFromPayload tries to extract a video URL from raw JSON bytes.
func extractVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return extractVideoURLFromMap(payload)
}

// extractVideoURLFromMap looks for a video URL in a parsed JSON map.
func extractVideoURLFromMap(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if uri, ok := payload["uri"].(string); ok && uri != "" {
		return uri
	}
	if resp, ok := payload["response"].(map[string]any); ok {
		if uri := extractVideoURLFromResponse(resp); uri != "" {
			return uri
		}
	}
	return ""
}

func extractVideoURLFromResponse(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	if gvr, ok := resp["generateVideoResponse"].(map[string]any); ok {
		if samples, ok := gvr["generatedSamples"].([]any); ok {
			for _, sample := range samples {
				if sm, ok := sample.(map[string]any); ok {
					if video, ok := sm["video"].(map[string]any); ok {
						if uri, ok := video["uri"].(string); ok && uri != "" {
							return uri
						}
					}
				}
			}
		}
	}
	if videos, ok := resp["videos"].([]any); ok {
		for _, video := range videos {
			if vm, ok := video.(map[string]any); ok {
				if uri, ok := vm["uri"].(string); ok && uri != "" {
					return uri
				}
			}
		}
	}
	if uri, ok := resp["video"].(string); ok && uri != "" {
		return uri
	}
	if uri, ok := resp["uri"].(string); ok && uri != "" {
		return uri
	}
	return ""
}

func ensureGeminiAPIKey(uri, key string) string {
	if key == "" || uri == "" {
		return uri
	}
	if strings.Contains(uri, "key=") {
		return uri
	}
	if strings.Contains(uri, "?") {
		return fmt.Sprintf("%s&key=%s", uri, key)
	}
	return fmt.Sprintf("%s?key=%s", uri, key)
}

// unused but kept for reference: derive extension from URL path
func extFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return path.Ext(parsed.Path)
}
