package doubao

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/volcsign"

	"github.com/pkg/errors"
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
// groupId 已从客户端状态移除；调用方通过 CreateAndWait 的 groupID 参数传入。
type bytePlusAssetClient struct {
	ak, sk         string
	region         string
	projectName    string
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

// createAssetRequest is the request body for CreateAsset.
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

// bytePlusEnvelope is the unified response envelope for Volcengine OpenAPI calls.
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
	// rawResult preserves the raw Result field; field names in failure payloads
	// (e.g. moderation rejection details) are not fixed, so we carry the whole
	// blob for transparent error reporting.
	rawResult json.RawMessage
}

// createAssetGroupRequest is the request body for CreateAssetGroup.
type createAssetGroupRequest struct {
	Name        string `json:"Name"`
	ProjectName string `json:"ProjectName,omitempty"`
}

// CreateGroup creates a new BytePlus asset group and returns its id.
func (cl *bytePlusAssetClient) CreateGroup(ctx context.Context, name string) (string, error) {
	reqBody := createAssetGroupRequest{
		Name:        name,
		ProjectName: cl.projectName,
	}
	env, err := cl.doAction(ctx, "CreateAssetGroup", reqBody)
	if err != nil {
		return "", errors.Wrap(err, "byteplus CreateAssetGroup failed")
	}
	if env.Result.Id == "" {
		return "", errors.New("byteplus CreateAssetGroup returned empty group id")
	}
	return env.Result.Id, nil
}

// groupExhaustedCodes are BytePlus error codes that indicate the asset group is
// full, invalid, or otherwise unusable for new uploads. The upstream API
// documentation contains no error-code table, so these are conservative guesses
// based on code names — not confirmed documented values.
//
// Design intent:
//   - Unknown codes return false (permissive-false): unrecognised errors surface
//     directly to the operator, which is far safer than creating junk groups on
//     every unrelated error. When a genuine group-full code is observed in
//     production logs, add it here.
//   - Do NOT add a bare/generic quota or rate-limit code (e.g. "QuotaExceeded",
//     "ThrottlingException", "RateLimitExceeded"). Such codes cannot be
//     distinguished from API-level call-rate throttling. Under sustained load
//     that would trigger a new CreateAssetGroup call on every throttled request —
//     a runaway group-creation loop against the upstream account.
//
// Every entry below is unambiguously group-scoped (the code name contains
// "Group", "GroupId", or "GroupCapacity").
var groupExhaustedCodes = map[string]bool{
	"GroupFull":                   true,
	"InvalidGroupId":              true,
	"GroupNotFound":               true,
	"AssetGroupCapacityExceeded":  true,
	"InvalidParameter.GroupId":    true,
	"ResourceNotFound.GroupId":    true,
	"LimitExceeded.GroupCapacity": true,
}

// IsGroupExhausted returns true only when err is an *assetAPIError whose Code
// positively matches a known group-unusable error. Returns false for auth
// failures, moderation rejections, network errors, and anything unrecognised.
func (cl *bytePlusAssetClient) IsGroupExhausted(err error) bool {
	var apiErr *assetAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return groupExhaustedCodes[apiErr.Code]
}

// CreateAndWait uploads a single asset and polls until Active, returning assetId.
// groupID is the BytePlus asset-library group to upload into.
func (cl *bytePlusAssetClient) CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error) {
	id, err := cl.createAsset(ctx, groupID, mediaURL, assetType)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("byteplus CreateAsset returned empty asset id")
	}

	deadline := time.Now().Add(cl.timeoutOrDefault())
	for {
		status, detail, err := cl.getAsset(ctx, id)
		if err != nil {
			return "", err
		}
		switch status {
		case "Active":
			return id, nil
		case "Failed":
			// 审核拒绝等失败原因在 Result 的扩展字段里（字段名不固定），
			// 带出响应体原文供排查（内容审核类拒绝对用户是关键信息）。
			if detail != "" {
				return "", errors.Errorf("byteplus asset %s processing failed: %s", id, detail)
			}
			return "", errors.Errorf("byteplus asset %s processing failed", id)
		}
		// Processing / other intermediate states: keep polling.
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

// createAsset calls CreateAsset and returns the asset id.
func (cl *bytePlusAssetClient) createAsset(ctx context.Context, groupID, mediaURL, assetType string) (string, error) {
	reqBody := createAssetRequest{
		GroupId:     groupID,
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

// getAsset calls GetAsset, returning Status and failure detail (raw Result blob).
func (cl *bytePlusAssetClient) getAsset(ctx context.Context, id string) (string, string, error) {
	reqBody := map[string]any{
		"Id":          id,
		"ProjectName": cl.projectName,
	}
	env, err := cl.doAction(ctx, "GetAsset", reqBody)
	if err != nil {
		return "", "", errors.Wrap(err, "byteplus GetAsset failed")
	}
	detail := ""
	if len(env.rawResult) > 0 {
		detail = truncate(env.rawResult, 512)
	}
	return env.Result.Status, detail, nil
}

// doAction issues one signed OpenAPI call and parses the unified response envelope.
// Structured upstream API errors (ResponseMetadata.Error) are returned as *assetAPIError.
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
	// Preserve the raw Result blob (failure field names are not fixed; carry the
	// whole thing so callers can report moderation-rejection details verbatim).
	var rawShell struct {
		Result json.RawMessage `json:"Result"`
	}
	if err := common.Unmarshal(respBody, &rawShell); err == nil {
		env.rawResult = rawShell.Result
	}
	if env.ResponseMetadata.Error != nil && env.ResponseMetadata.Error.Code != "" {
		return nil, &assetAPIError{
			Code:    env.ResponseMetadata.Error.Code,
			Message: env.ResponseMetadata.Error.Message,
			Raw:     common.MaskSensitiveInfo(string(respBody)),
		}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, errors.Errorf("upstream returned status %d: %s", resp.StatusCode, truncate(respBody, 512))
	}
	return &env, nil
}
