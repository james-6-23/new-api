// Package volcpassthrough 实现字节火山（国内方舟 / 海外 BytePlus ModelArk）透传渠道。
//
// 客户以与官方完全一致的方式调用：路径、query、请求体、响应体、错误码全部原样，
// 仅 URL 前缀换成本站点、密钥换成本站点令牌。站点只托管凭据并按需计费。
//
// 官方有两套并存的认证与两组 host：
//
//	数据面  /api/v3/...     Authorization: Bearer <API Key>
//	          国内 ark.cn-beijing.volces.com
//	          海外 ark.ap-southeast.bytepluses.com
//	控制面  /?Action=X      AK/SK HMAC-SHA256 签名（Service=ark）
//	          国内 ark.cn-beijing.volcengineapi.com
//	          海外 ark.ap-southeast-1.byteplusapi.com
//
// 注意海外数据面 host 用 ap-southeast（无 -1），而控制面 host 与签名
// CredentialScope 用 ap-southeast-1（有 -1）—— 两者差一个后缀且不可互推，
// 故数据面 host 直接取渠道 BaseURL，签名 region 独立解析。
package volcpassthrough

import (
	"net/http"
	"strings"
)

// 本渠道只提供 Seedance 视频生成与私域素材库两组能力，白名单是精确匹配而非
// 前缀通配。曾经放行 /api/v3/ 整个前缀（连带 /api/compatible/、/api/coding/），
// 等于把 chat/completions、embedding 等全部官方能力免费开给持站点令牌的客户 ——
// 那些路径不走计费链路，额度扣不到。故收缩为下表 4 条。
//
// 路径里的 {id} 用变量段匹配：官方任务 id 形如 cgt-2026...-abcde,不含 /。
const (
	// GenerationsTasksPath 视频生成任务集合路径（提交与列表共用）。
	GenerationsTasksPath = "/api/v3/contents/generations/tasks"
)

// allowedDataPlaneRoutes 放行的数据面路由:method → 路径模板。
// 模板中 "{id}" 匹配单个非空且不含 / 的段。
var allowedDataPlaneRoutes = map[string][]string{
	http.MethodPost:   {GenerationsTasksPath},
	http.MethodGet:    {GenerationsTasksPath, GenerationsTasksPath + "/{id}"},
	http.MethodDelete: {GenerationsTasksPath + "/{id}"},
}

// allowedControlPlaneActions 放行的 12 个控制面 Action:资产组 5 + 资产 5 + 真人认证 2。
// 其余 100 个官方 Action（Endpoint、Model、Usage、RateLimit 等）一律拒绝 ——
// 它们既与本渠道能力无关,又能改动站点自己的火山账号配置。
var allowedControlPlaneActions = map[string]bool{
	"CreateAssetGroup": true,
	"GetAssetGroup":    true,
	"ListAssetGroups":  true,
	"UpdateAssetGroup": true,
	"DeleteAssetGroup": true,

	"CreateAsset": true,
	"GetAsset":    true,
	"ListAssets":  true,
	"UpdateAsset": true,
	"DeleteAsset": true,

	"CreateVisualValidateSession": true,
	"GetVisualValidateResult":     true,
}

// IsAllowedControlPlaneAction 判断控制面 Action 是否在白名单内。
func IsAllowedControlPlaneAction(action string) bool {
	return allowedControlPlaneActions[action]
}

// matchPathTemplate 按模板匹配路径,"{id}" 匹配单个非空且不含 / 的段。
func matchPathTemplate(tpl, path string) bool {
	tplSegs := strings.Split(strings.Trim(tpl, "/"), "/")
	pathSegs := strings.Split(strings.Trim(path, "/"), "/")
	if len(tplSegs) != len(pathSegs) {
		return false
	}
	for i, seg := range tplSegs {
		if seg == "{id}" {
			if pathSegs[i] == "" {
				return false
			}
			continue
		}
		if seg != pathSegs[i] {
			return false
		}
	}
	return true
}

// IsControlPlane 判断请求是否走顶层 OpenAPI（控制面）。
// 官方控制面形如 POST https://<host>/?Action=CreateAsset&Version=2024-01-01，
// 路径恒为根，动作在 query 里。
func IsControlPlane(path string, query map[string][]string) bool {
	if p := strings.Trim(path, "/"); p != "" {
		return false
	}
	_, ok := query["Action"]
	return ok
}

// IsAllowedDataPlaneRequest 校验数据面 method + path 是否在白名单内。
//
// 方法必须一起校验:同一路径 POST 是计费的提交、GET 是免费的查询,
// 只看路径会让客户用 GET 之外的方法绕过计费判定。
func IsAllowedDataPlaneRequest(method, path string) bool {
	for _, tpl := range allowedDataPlaneRoutes[strings.ToUpper(method)] {
		if matchPathTemplate(tpl, path) {
			return true
		}
	}
	return false
}

// IsAllowedDataPlanePath 保留路径维度的判断,供只有路径可用的调用点使用
// （如错误提示与日志）。真正的放行判定用 IsAllowedDataPlaneRequest。
func IsAllowedDataPlanePath(path string) bool {
	for _, tpls := range allowedDataPlaneRoutes {
		for _, tpl := range tpls {
			if matchPathTemplate(tpl, path) {
				return true
			}
		}
	}
	return false
}

// hopByHopHeaders 是 RFC 7230 定义的逐跳首部，转发时必须丢弃。
var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// clientAuthHeaders 是客户用于向本站点认证的首部。它们携带的是站点令牌而非
// 上游凭据，必须在转发前剥离，再由渠道配置重新注入，绝不能透传给官方。
var clientAuthHeaders = []string{
	"Authorization",
	"X-Api-Key",
	"Api-Key",
	"X-Goog-Api-Key",
	"Cookie",
	"X-Content-Sha256",
	"X-Date",
	"Host",
	"Content-Length",
}

// CopyUpstreamRequestHeaders 把客户请求首部复制到上游请求，剥离逐跳首部与
// 客户侧认证信息。Content-Type 等业务首部原样保留（含 multipart boundary）。
func CopyUpstreamRequestHeaders(dst *http.Request, src *http.Request) {
	for k, vs := range src.Header {
		if isSkippedRequestHeader(k) {
			continue
		}
		for _, v := range vs {
			dst.Header.Add(k, v)
		}
	}
}

func isSkippedRequestHeader(name string) bool {
	for _, h := range hopByHopHeaders {
		if strings.EqualFold(name, h) {
			return true
		}
	}
	for _, h := range clientAuthHeaders {
		if strings.EqualFold(name, h) {
			return true
		}
	}
	return false
}

// CopyDownstreamResponseHeaders 把上游响应首部原样回写给客户，仅剥离逐跳首部。
// 保留 Content-Type/Content-Disposition 等，使二进制下载与 SSE 流式穿透可用。
func CopyDownstreamResponseHeaders(dst http.Header, src http.Header) {
	for k, vs := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func isHopByHop(name string) bool {
	for _, h := range hopByHopHeaders {
		if strings.EqualFold(name, h) {
			return true
		}
	}
	return false
}
