package volcpassthrough

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common/volcsign"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
)

// UpstreamTarget 描述一次透传请求解析出的上游目标。
type UpstreamTarget struct {
	// URL 完整上游地址（含 query）。
	URL string
	// ControlPlane 为 true 时使用 AK/SK 签名，否则使用 Bearer 密钥。
	ControlPlane bool
	// SignRegion 控制面签名 region（CredentialScope），仅 ControlPlane 时有意义。
	SignRegion string
}

// ChannelCredentials 是透传所需的渠道凭据与配置。
type ChannelCredentials struct {
	// BaseURL 渠道配置的数据面 host，如 https://ark.cn-beijing.volces.com。
	BaseURL string
	// APIKey 数据面 Bearer 密钥。
	APIKey string
	// AccessKey / SecretKey 控制面签名凭据。
	AccessKey string
	SecretKey string
	// Proxy 渠道代理设置。
	Proxy string
	// OtherSettings 渠道扩展配置，提供签名 region 与控制面 host 覆盖。
	OtherSettings dto.ChannelOtherSettings
}

// ErrPathNotAllowed 表示请求路径不在透传白名单内。
var ErrPathNotAllowed = fmt.Errorf("path not allowed for volc passthrough")

// ErrActionNotAllowed 表示控制面 Action 不在白名单内。
var ErrActionNotAllowed = fmt.Errorf("action not allowed for volc passthrough")

// ResolveTarget 把客户请求的 method + path + query 解析成上游目标。
//
// path 是已剥离 /volc 前缀的官方原始路径；rawQuery 为客户原始 query 串，原样带给上游。
//
// method 必须参与白名单判定:同一路径 POST 是计费的视频提交、GET 是免费查询,
// 只按路径放行等于把计费入口暴露给任意方法。
func ResolveTarget(cred ChannelCredentials, method, path, rawQuery string) (*UpstreamTarget, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid query string: %w", err)
	}

	if IsControlPlane(path, query) {
		// 控制面只放行资产库与真人认证的 12 个 Action。其余官方 Action
		// （Endpoint / Model / Usage / RateLimit 等）能读写站点自己的火山账号配置,
		// 必须在发出上游请求之前挡掉。
		if !IsAllowedControlPlaneAction(query.Get("Action")) {
			return nil, ErrActionNotAllowed
		}
		host := cred.OtherSettings.ResolveVolcOpenAPIEndpoint(cred.BaseURL)
		target := host + "/"
		if rawQuery != "" {
			target += "?" + rawQuery
		}
		return &UpstreamTarget{
			URL:          target,
			ControlPlane: true,
			SignRegion:   cred.OtherSettings.ResolveVolcSignRegion(cred.BaseURL),
		}, nil
	}

	if !IsAllowedDataPlaneRequest(method, path) {
		return nil, ErrPathNotAllowed
	}

	target := strings.TrimRight(cred.BaseURL, "/") + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	return &UpstreamTarget{URL: target, ControlPlane: false}, nil
}

// BuildUpstreamRequest 构造发往官方的请求。
//
// body 为客户原始请求体字节（可为 nil）。控制面签名需要对完整 body 做 SHA256，
// 故签名路径必须持有全部字节；数据面不签名，调用方可传流式 reader 走 NewRequest。
func BuildUpstreamRequest(cred ChannelCredentials, target *UpstreamTarget, method string, body []byte, clientReq *http.Request) (*http.Request, error) {
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(clientReq.Context(), method, target.URL, bodyReader)
	if err != nil {
		return nil, err
	}
	CopyUpstreamRequestHeaders(req, clientReq)
	if len(body) > 0 {
		req.ContentLength = int64(len(body))
	}

	if target.ControlPlane {
		if cred.AccessKey == "" || cred.SecretKey == "" {
			return nil, fmt.Errorf("control plane request requires channel access key and secret key")
		}
		if err := volcsign.SignRequest(req, body, cred.AccessKey, cred.SecretKey, target.SignRegion, volcServiceName); err != nil {
			return nil, fmt.Errorf("sign control plane request failed: %w", err)
		}
		return req, nil
	}

	if cred.APIKey == "" {
		return nil, fmt.Errorf("data plane request requires channel api key")
	}
	req.Header.Set("Authorization", "Bearer "+cred.APIKey)
	return req, nil
}

// volcServiceName 是火山方舟 / BytePlus ModelArk 顶层 OpenAPI 的签名 Service 名。
const volcServiceName = "ark"

// DoUpstream 发送上游请求，遵循渠道代理设置。
func DoUpstream(cred ChannelCredentials, req *http.Request) (*http.Response, error) {
	client, err := service.GetHttpClientWithProxy(cred.Proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}
