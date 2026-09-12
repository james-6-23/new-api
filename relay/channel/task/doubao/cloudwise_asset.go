package doubao

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/pkg/errors"
)

// cloudwiseAssetClient implements assetClient for the third-party cloudwise asset proxy.
//
// Protocol (verified from cloudwise-api-docs.md, 2026-08-10 export):
//   - Auth: Authorization: Bearer {channel api key} — no AK/SK required.
//   - Success envelope for all three operations is PascalCase:
//     {ResponseMetadata:{RequestId, ...}, Result:{...}}
//   - Create-asset body mixes cases: assetType/groupId/url and lowercase
//     name/description, with a nested PascalCase Moderation.Strategy.
//   - An asset is usable at Status=="Active"; "Processing" keeps polling and
//     "Failed" is terminal.
//
// ERROR SHAPE — NOT VERIFIED. The document has no error-response example for any
// asset operation. Two shapes are therefore both accepted, and neither is
// confirmed for asset endpoints:
//   - ResponseMetadata.Error{Code, Message}: inferred from the sibling BytePlus
//     provider (see byteplus_asset.go), on the assumption that cloudwise proxies
//     the same underlying Volcengine service.
//   - top-level lowercase {code, message}: the shape this gateway demonstrably
//     uses for its task endpoints. A top-level "error" object is also accepted and
//     is likewise documented, carrying a message with or without a code — see
//     parseCloudwiseLowercaseError for the provenance of each form.
//
// When neither parses on a non-2xx, the HTTP status becomes the error code so
// that classification still has something to work with. Revisit once a real
// asset-operation error is observed in production logs.
//
// DISCREPANCY vs task-4-brief.md (document wins per task instructions):
//   - Brief claimed "upload uses {code,message,data} (snake_case)" — WRONG.
//     Document shows /api/v1/assets/create returns {ResponseMetadata, Result:{Id}}.
//   - Brief claimed create-group request is PascalCase — WRONG.
//     Document shows {"name":"","description":"..."} (lowercase).
//   - Brief claimed upload request is snake_case — WRONG.
//     Document shows mixed: assetType/groupId/url (camelCase), Moderation (PascalCase).
//   - Brief claimed ready status is "completed" — WRONG.
//     Document shows Status:"Active" (same as BytePlus).
type cloudwiseAssetClient struct {
	baseURL        string
	apiKey         string
	skipModeration bool
	httpClient     *http.Client
	// pollInterval/pollTimeout zero values fall back to package-level defaults.
	pollInterval time.Duration
	pollTimeout  time.Duration
}

const (
	cloudwisePollInterval = 2 * time.Second
	cloudwisePollTimeout  = 60 * time.Second
)

func (cl *cloudwiseAssetClient) intervalOrDefault() time.Duration {
	if cl.pollInterval > 0 {
		return cl.pollInterval
	}
	return cloudwisePollInterval
}

func (cl *cloudwiseAssetClient) timeoutOrDefault() time.Duration {
	if cl.pollTimeout > 0 {
		return cl.pollTimeout
	}
	return cloudwisePollTimeout
}

// cloudwiseEnvelope is the unified response envelope for all cloudwise asset operations.
// Same shape as bytePlusEnvelope — cloudwise acts as a Bearer-auth proxy to the same
// underlying Volcengine service.
type cloudwiseEnvelope struct {
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

// cloudwiseCreateGroupReq: document shows lowercase field names.
type cloudwiseCreateGroupReq struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// cloudwiseCreateAssetReq: document shows camelCase for data fields, PascalCase for Moderation.
type cloudwiseCreateAssetReq struct {
	AssetType  string                    `json:"assetType"`
	GroupId    string                    `json:"groupId"`
	URL        string                    `json:"url"`
	Name       string                    `json:"name,omitempty"`
	Moderation *cloudwiseAssetModeration `json:"Moderation,omitempty"`
}

type cloudwiseAssetModeration struct {
	Strategy string `json:"Strategy"`
}

// cloudwiseGetAssetReq: document shows PascalCase Id field.
type cloudwiseGetAssetReq struct {
	Id string `json:"Id"`
}

// CreateGroup creates a new cloudwise asset group and returns its id.
// POST {base}/api/v1/assets/groups/create
func (cl *cloudwiseAssetClient) CreateGroup(ctx context.Context, name string) (string, error) {
	env, _, err := cl.post(ctx, "/api/v1/assets/groups/create", cloudwiseCreateGroupReq{Name: name})
	if err != nil {
		return "", errors.Wrap(err, "cloudwise CreateGroup failed")
	}
	if env.Result.Id == "" {
		return "", errors.New("cloudwise CreateGroup returned empty group id")
	}
	return env.Result.Id, nil
}

// CreateAndWait uploads a single asset and polls until Active, returning assetId.
func (cl *cloudwiseAssetClient) CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error) {
	req := cloudwiseCreateAssetReq{
		AssetType: assetType,
		GroupId:   groupID,
		URL:       mediaURL,
	}
	if cl.skipModeration {
		req.Moderation = &cloudwiseAssetModeration{Strategy: "Skip"}
	}
	env, _, err := cl.post(ctx, "/api/v1/assets/create", req)
	if err != nil {
		return "", errors.Wrap(err, "cloudwise CreateAsset failed")
	}
	id := env.Result.Id
	if id == "" {
		return "", errors.New("cloudwise CreateAsset returned empty asset id")
	}

	deadline := time.Now().Add(cl.timeoutOrDefault())
	for {
		statusEnv, raw, pollErr := cl.post(ctx, "/api/v1/assets/get", cloudwiseGetAssetReq{Id: id})
		if pollErr != nil {
			return "", errors.Wrap(pollErr, "cloudwise GetAsset failed")
		}
		switch statusEnv.Result.Status {
		case "Active":
			return id, nil
		case "Failed":
			// raw is already masked by post; only truncate so a large rejection
			// body cannot be embedded whole into the relay response and logs.
			return "", errors.Errorf("cloudwise asset %s processing failed: %s", id, truncate([]byte(raw), 512))
		}
		if time.Now().After(deadline) {
			return "", errors.Errorf("cloudwise asset %s not ready within %s (last status: %s)",
				id, cl.timeoutOrDefault(), statusEnv.Result.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.intervalOrDefault()):
		}
	}
}

// cloudwiseGroupExhaustedCodes are string error codes that indicate the asset group is
// full, invalid, or otherwise unusable for new uploads.
//
// PROVENANCE: The cloudwise API documentation contains no error-code table.
// These code names are guesses based on the underlying Volcengine/BytePlus platform's
// naming conventions (cloudwise appears to be a Bearer-auth proxy to the same service).
// They are NOT confirmed from any documentation.
//
// When a real group-full or group-not-found error is observed in production logs,
// verify the actual Code value and update this list accordingly.
//
// CONFIRMED IN PRODUCTION (2026-08): the real code is "NotFound.group_id" with
// message "The specified asset_group group-... is not found." — now listed
// below. The message-text fallback is a second net for the string-shaped
// {"error":"..."} bodies that carry no code at all.
//
// Design intent: same as groupExhaustedCodes in byteplus_asset.go.
//   - Do NOT add bare quota or rate-limit codes — they would cause runaway group creation
//     under API throttling.
//   - Unknown codes return false; the operator sees the real error.
var cloudwiseGroupExhaustedCodes = map[string]bool{
	"GroupFull":                   true,
	"InvalidGroupId":              true,
	"GroupNotFound":               true,
	"AssetGroupCapacityExceeded":  true,
	"InvalidParameter.GroupId":    true,
	"ResourceNotFound.GroupId":    true,
	"LimitExceeded.GroupCapacity": true,
	"NotFound.group_id":           true,
}

// IsGroupExhausted returns true only when err is an *assetAPIError whose Code
// positively matches a known group-unusable error code.
// Keyword fallback requires BOTH a group indicator AND a capacity/existence
// indicator so that auth or moderation failures cannot trip it.
func (cl *cloudwiseAssetClient) IsGroupExhausted(err error) bool {
	var apiErr *assetAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if cloudwiseGroupExhaustedCodes[apiErr.Code] {
		return true
	}
	// Nothing parseable came back, so Message is the raw response body. Matching
	// keywords against a whole body is how a plain HTTP 429 whose payload reads
	// {"detail":"asset group upload capacity temporarily throttled, retry in 30s"}
	// got classified as exhausted — one junk group and one channel-row write per
	// concurrent throttled request. The raw body still reaches the operator through
	// the error text; it is only barred from driving classification.
	if apiErr.MessageFromRawBody {
		return false
	}
	// Message-text fallback for undocumented codes observed in production.
	// Requires BOTH a group-scoped word AND an unambiguous capacity/existence
	// word, so that auth or moderation failures cannot trip it.
	//
	// Deliberately NOT matched here:
	//   - "exceed": it also matches "exceeded", which nearly every quota and
	//     throttle message contains ("asset group request rate exceeded, retry
	//     later"). Under throttling that would mint one junk group per concurrent
	//     request, the exact runaway that keeping QuotaExceeded out of the code map
	//     prevents.
	//
	// "not found" IS matched: production shows this gateway's own auto-created
	// groups vanishing upstream (assets in them are deleted too), so a missing
	// group here is an upstream lifecycle event, not an operator typo. Rotation
	// re-creates the group and logs a WARN with the old/new ids, so the
	// overwrite — if it ever is an operator misconfiguration — stays visible.
	msg := strings.ToLower(apiErr.Message)
	return strings.Contains(msg, "group") &&
		(strings.Contains(msg, "full") || strings.Contains(msg, "capacity") ||
			strings.Contains(msg, "not found"))
}

// post sends a POST to {baseURL}{path} with Bearer auth and returns the parsed envelope.
// The masked raw response is returned alongside for error reporting.
func (cl *cloudwiseAssetClient) post(ctx context.Context, path string, payload any) (*cloudwiseEnvelope, string, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, "", err
	}

	url := strings.TrimRight(cl.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cl.apiKey)

	client := cl.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	maskedRaw := common.MaskSensitiveInfo(string(respBody))

	var env cloudwiseEnvelope
	envErr := common.Unmarshal(respBody, &env)

	if envErr == nil && env.ResponseMetadata.Error != nil && env.ResponseMetadata.Error.Code != "" {
		return nil, maskedRaw, &assetAPIError{
			Code:    env.ResponseMetadata.Error.Code,
			Message: env.ResponseMetadata.Error.Message,
			Raw:     maskedRaw,
		}
	}
	// Every error response must surface as *assetAPIError: IsGroupExhausted starts
	// with an errors.As check, so a plain error here would make group rotation
	// unreachable on this provider. The lowercase {code,message} shape and a
	// status-code fallback cover the undocumented asset-error format.
	if resp.StatusCode >= http.StatusBadRequest {
		code, message := parseCloudwiseLowercaseError(respBody)
		if code == "" {
			code = "HTTP" + strconv.Itoa(resp.StatusCode)
		}
		messageFromRawBody := false
		if message == "" {
			// Keep the body as the message so the operator still sees what the
			// upstream said. It is flagged so IsGroupExhausted will not keyword-match
			// against it — see assetAPIError.MessageFromRawBody.
			message = truncate([]byte(maskedRaw), 512)
			messageFromRawBody = true
		}
		return nil, maskedRaw, &assetAPIError{
			Code:               code,
			Message:            message,
			Raw:                maskedRaw,
			MessageFromRawBody: messageFromRawBody,
		}
	}
	// A 2xx carrying a business error is this gateway's documented convention, not a
	// hypothetical: cloudwise-api-docs.md:308-333 shows a *successful* task fetch as
	// HTTP 200 with {"code":"10000", ..., "success":true}, so the business result
	// rides in the body rather than the status line. A 200 whose Result has neither
	// Id nor Status therefore may really be an error such as
	// {"code":"1026","message":"asset group is full"}, and it has to arrive as an
	// *assetAPIError or the caller reports "empty asset id" and never rotates the
	// group. Only a result-less response takes this path, so a poll still sitting at
	// Status:"Processing" is untouched.
	if envErr == nil && env.Result.Id == "" && env.Result.Status == "" {
		// Deliberately NO raw-body fallback for the message here, unlike the non-2xx
		// branch above. There the response IS an error, so any body text helps. Here
		// the body may be a *success* payload, and assetAPIError.Message is what
		// IsGroupExhausted substring-matches: feeding it
		// {"code":0,"message":"","data":{"groupId":"g-1","capacity":1000}} matched
		// "group" via "groupId" and "capacity" via "capacity", reporting exhaustion
		// for a healthy response and minting a junk group plus a channel-row write.
		// parseCloudwiseBusinessError only reports isErr with a real code or message,
		// so an unrecognised body stays the plain empty-id error.
		if code, message, isErr := parseCloudwiseBusinessError(respBody); isErr {
			return nil, maskedRaw, &assetAPIError{Code: code, Message: message, Raw: maskedRaw}
		}
	}
	if envErr != nil {
		return nil, maskedRaw, errors.Wrapf(envErr,
			"cloudwise: unmarshal response failed (status %d, body: %s)", resp.StatusCode, truncate(respBody, 512))
	}
	return &env, maskedRaw, nil
}

// cloudwiseSuccessCodes are the business codes the gateway documents for a
// successful response. A body carrying one is not an error no matter how empty its
// Result is.
//
// The document uses two spellings, both real:
//   - "10000": the quoted form on the video/task endpoints
//     (cloudwise-api-docs.md:310, :7326 — 14 occurrences).
//   - 0: the numeric form on the image endpoints, always paired with
//     "message":"success" (cloudwise-api-docs.md:10674-10675, :10709-10710 —
//     8 occurrences).
//
// Codes arrive here already normalised by cloudwiseErrorCode, which renders a JSON
// number as written and decodes a quoted string, so a bare 0 and a quoted "0" both
// land on "0" and only that one key is needed for the pair.
var cloudwiseSuccessCodes = map[string]bool{
	"10000": true,
	"0":     true,
}

// isCloudwiseSuccessCode reports whether a normalised business code means success.
func isCloudwiseSuccessCode(code string) bool {
	return cloudwiseSuccessCodes[code]
}

// parseCloudwiseLowercaseError extracts the lowercase {code,message} error shape.
//
// PROVENANCE, three forms, all observed:
//   - top-level lowercase code/message: DOCUMENTED for this gateway's task
//     endpoints (cloudwise-api-docs.md:310, :7326-7332).
//   - a top-level "error" OBJECT: DOCUMENTED, in two shapes. It carries only a
//     message at cloudwise-api-docs.md:3739-3741 ({"status":"failed", ...,
//     "error":{"message":"The request failed because ..."}}) and both code and
//     message at :12601-12609 ({"status":"failed","error":{"code":
//     "generation_failed","message":"..."}}). A third, deeper nesting under "task"
//     also exists (:11836-11843) and is not read here.
//     The message-only shape at :3739 is why the nested object is resolved per
//     field below rather than wholesale — do not collapse that.
//   - a top-level "error" STRING: OBSERVED IN PRODUCTION for the asset endpoints,
//     e.g. HTTP 400 {"error":"asset group not found"}. The struct form cannot
//     hold it (unmarshal fails and the whole body is treated as raw text), so it
//     is captured as the message — the only signal a placeholder-less body
//     carries, and the one the keyword fallback keys on.
//
// Neither form is documented for the asset operations specifically; see the
// ERROR SHAPE block at the top of this file. code is read as a raw scalar because
// the document shows it quoted ("10000") while a numeric code would otherwise
// break the whole parse. Returns empty strings when the body carries neither shape.
func parseCloudwiseLowercaseError(body []byte) (code, message string) {
	var payload struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	code, message = cloudwiseErrorCode(payload.Code), payload.Message
	// The nested object wins per field, not wholesale. Returning it unconditionally
	// discarded a usable top-level code: {"code":"GroupFull","error":{"message":"no
	// code here"}} classified as the HTTP status instead of GroupFull and so never
	// rotated the group. Falling through only when the nested object is entirely
	// empty would leave that same body broken, since it does carry a nested message.
	if len(payload.Error) > 0 {
		switch common.GetJsonType(payload.Error) {
		case "object":
			var nested struct {
				Code    json.RawMessage `json:"code"`
				Message string          `json:"message"`
			}
			if err := common.Unmarshal(payload.Error, &nested); err == nil {
				if c := cloudwiseErrorCode(nested.Code); c != "" {
					code = c
				}
				if nested.Message != "" {
					message = nested.Message
				}
			}
		case "string":
			// {"error":"asset group not found"} — no code anywhere; the string is
			// the whole story, so it becomes the message.
			if message == "" {
				var s string
				if err := common.Unmarshal(payload.Error, &s); err == nil {
					message = s
				}
			}
		}
	}
	return code, message
}

// parseCloudwiseBusinessError reports whether a 2xx body is really a lowercase
// business error. isErr is false for anything that does not positively look like
// one, so an empty-but-successful response is never turned into a bogus error.
func parseCloudwiseBusinessError(body []byte) (code, message string, isErr bool) {
	code, message = parseCloudwiseLowercaseError(body)

	// A documented success code is decisive and outranks everything else, including a
	// contradictory success:false, because it is the field the document actually
	// specifies per endpoint.
	if isCloudwiseSuccessCode(code) {
		return "", "", false
	}

	// PRECEDENCE for "success": weaker than an explicit error code, stronger than a
	// message alone. success is only defined as "Whether the request was successful"
	// (cloudwise-api-docs.md:7320) — real evidence, but not endpoint-specific, and
	// letting it short-circuit first meant
	// {"success":true,"code":"1026","message":"asset group is full"} returned the
	// plain empty-id error and rotation never ran. A non-success code names a
	// specific failure, so it wins; a message with no code is too weak to overrule
	// an explicit success:true, since "message" carries the human text on success
	// responses too (":10675" shows "message":"success").
	if code == "" && cloudwiseSuccessFlag(body) {
		return "", "", false
	}

	// Require some signal. A PascalCase envelope with an empty Result carries
	// neither field and must stay a non-error.
	if code == "" && message == "" {
		return "", "", false
	}
	return code, message, true
}

// cloudwiseSuccessFlag reports whether the body carries an explicit success:true.
func cloudwiseSuccessFlag(body []byte) bool {
	var flag struct {
		Success *bool `json:"success"`
	}
	if err := common.Unmarshal(body, &flag); err != nil {
		return false
	}
	return flag.Success != nil && *flag.Success
}

// cloudwiseErrorCode renders a lowercase "code" field as a plain string using the
// house helper, which decodes a JSON string properly (inner escapes included) and
// leaves a number as written. A non-scalar code is ignored rather than passed
// through raw, so an unexpected object cannot leak JSON into the operator-facing
// error text; the accompanying message still drives classification.
func cloudwiseErrorCode(raw json.RawMessage) string {
	switch common.GetJsonType(raw) {
	case "string", "number", "boolean":
		return common.JsonRawMessageToString(raw)
	default:
		return ""
	}
}
