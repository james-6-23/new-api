package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay/channel/volcpassthrough"
	"github.com/QuantumNous/new-api/relay/channel/volcpassthrough/assetiso"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// RelayVolcPassthrough 把请求原样转发到火山方舟 / BytePlus 官方,并把响应原样回写。
//
// 覆盖除视频生成提交外的全部官方接口(112 个控制面 Action、Files 上传下载、
// 任务查询与删除、Embedding、Rerank 等):路径、query、请求体、响应体、状态码、
// 错误结构全部不做解析与改写,站点只替换 URL 前缀与凭据。
//
// 这些接口不计费(仅视频生成计费),因此不进任务链路,直接字节流转发。
func RelayVolcPassthrough(c *gin.Context) {
	cred, err := buildPassthroughCredentials(c)
	if err != nil {
		respondPassthroughError(c, http.StatusInternalServerError, "channel_config_error", err.Error())
		return
	}

	path := c.GetString(middleware.VolcPassthroughPathKey)
	target, err := volcpassthrough.ResolveTarget(cred, c.Request.Method, path, c.Request.URL.RawQuery)
	if err != nil {
		// 白名单外的路径与 Action 一律 404,且用站点自己的错误形状 ——
		// 不伪装成官方错误,客户能立刻分清是站点挡的还是火山挡的。
		if errors.Is(err, volcpassthrough.ErrPathNotAllowed) || errors.Is(err, volcpassthrough.ErrActionNotAllowed) {
			respondPassthroughError(c, http.StatusNotFound, "endpoint_not_supported",
				"本渠道仅提供 Seedance 视频生成与素材库接口,该接口未开放")
			return
		}
		respondPassthroughError(c, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}

	// 控制面签名需要对完整 body 做 SHA256,必须先持有全部字节;
	// 数据面虽不签名,但重试与错误回放同样依赖可重读的 body,故统一读缓存。
	body, err := readPassthroughBody(c)
	if err != nil {
		respondPassthroughError(c, http.StatusBadRequest, "read_request_body_failed", err.Error())
		return
	}

	owner := volcAssetOwner(c)
	action := ""
	if target.ControlPlane {
		action = c.Query("Action")
		if !guardVolcAssetOwnership(c, owner, action, body) {
			return
		}
		// 全链路唯一的请求改写:客户未传 ProjectName 时补渠道配置的项目名。
		// 官方要求 CreateAsset 的 ProjectName 与目标素材组所属项目一致,而项目是
		// 站点 IAM 账号下的概念,客户无从知晓。客户传了就用客户的。
		body = assetiso.InjectProjectName(action, body, cred.OtherSettings.BytePlusProjectName)
	}

	req, err := volcpassthrough.BuildUpstreamRequest(cred, target, c.Request.Method, body, c.Request)
	if err != nil {
		respondPassthroughError(c, http.StatusInternalServerError, "build_upstream_request_failed", err.Error())
		return
	}
	if target.ControlPlane {
		// 控制面响应要解析(过滤 List、落库新建资源),必须拿到明文 JSON。
		// 客户的 Accept-Encoding 若原样带上,Go 不会自动解压,响应体是 gzip 字节 ——
		// 解析静默失败、过滤被跳过,等于把别人的素材列表照原样回给客户。
		// 删掉后由 transport 自己协商压缩并透明解压。
		req.Header.Del("Accept-Encoding")
	}

	resp, err := volcpassthrough.DoUpstream(cred, req)
	if err != nil {
		respondPassthroughError(c, http.StatusBadGateway, "do_request_failed", err.Error())
		return
	}
	defer resp.Body.Close()

	if target.ControlPlane {
		relayVolcControlPlaneResponse(c, owner, action, body, resp)
		return
	}

	volcpassthrough.CopyDownstreamResponseHeaders(c.Writer.Header(), resp.Header)
	c.Writer.WriteHeader(resp.StatusCode)
	// 用 io.Copy 而非先读全量:文件下载与 SSE 流式响应必须边收边发,
	// 否则大文件会把整个响应压进内存、流式响应会等到上游结束才吐出。
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c, "volc passthrough copy response failed: "+err.Error())
	}
	c.Writer.Flush()
}

// buildPassthroughCredentials 从已选定的渠道上下文取出转发所需凭据。
func buildPassthroughCredentials(c *gin.Context) (volcpassthrough.ChannelCredentials, error) {
	baseURL := common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl)
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeVolcPassthrough]
	}
	other, _ := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	setting, _ := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)

	cred := volcpassthrough.ChannelCredentials{
		BaseURL:       baseURL,
		APIKey:        common.GetContextKeyString(c, constant.ContextKeyChannelKey),
		AccessKey:     other.BytePlusAccessKey,
		SecretKey:     other.BytePlusSecretKey,
		Proxy:         setting.Proxy,
		OtherSettings: other,
	}
	if cred.BaseURL == "" {
		return cred, fmt.Errorf("channel base url is empty")
	}
	return cred, nil
}

// readPassthroughBody 读取请求体。GET/DELETE 等无体请求返回 nil。
func readPassthroughBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

// respondPassthroughError 返回站点侧错误。
//
// 只用于站点自身的失败(渠道配置缺失、路径不在白名单、连不上上游)。
// 上游返回的任何状态码与错误体都原样透传,绝不在这里改写 ——
// 客户按官方文档处理错误,站点插一层自定义结构会破坏兼容性。
func respondPassthroughError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "new_api_error",
		},
	})
	c.Abort()
}

// volcAssetOwner 取出本次请求的归属上下文。
//
// 归属粒度是 user_id 而非 token_id:同一用户轮换令牌不该丢素材,跨用户才隔离。
// channel_id 必须一起带上 —— 不同渠道是不同的火山账号,同名 id 不同源。
func volcAssetOwner(c *gin.Context) service.VolcAssetOwner {
	return service.VolcAssetOwner{
		UserId:    common.GetContextKeyInt(c, constant.ContextKeyUserId),
		ChannelId: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
	}
}

// guardVolcAssetOwnership 转发前的归属校验,返回 false 表示已回写错误、调用方须直接返回。
//
// 这两类校验必须在发出上游请求之前完成:带 Id 的 Action 会读写/删除具体资源,
// 一旦发出就晚了;CreateAsset 的 GroupId 若不校验,客户能往别人的素材组塞东西。
func guardVolcAssetOwnership(c *gin.Context, owner service.VolcAssetOwner, action string, body []byte) bool {
	if _, err := service.CheckSingleResourceOwnership(owner, action, body); err != nil {
		respondVolcAssetError(c, err)
		return false
	}
	if err := service.CheckCreateAssetGroupRef(owner, action, body); err != nil {
		respondVolcAssetError(c, err)
		return false
	}
	// 上限校验也必须前置:转发后再拦,素材已建在火山侧占着配额,
	// 本地却因超限不记账 —— 直接变成谁都管不了的孤儿。
	if err := service.CheckVolcAssetLimit(owner, action); err != nil {
		respondVolcAssetError(c, err)
		return false
	}
	return true
}

// respondVolcAssetError 把归属校验失败翻译成响应。
//
// 归属不符统一回 404,且**不区分**「不存在」与「不属于你」—— 区分了,站点就成了
// 别人 asset id 的存在性探测器。数据库出错回 500 并拒绝转发:查不清归属时放行
// 等于没有隔离。
func respondVolcAssetError(c *gin.Context, err error) {
	// 超限回 403 而非 404:这不是「看不见」,是「你已经用满了」,
	// 客户需要据此去申请提额,含糊成不存在只会让人反复重试。
	if errors.Is(err, service.ErrAssetLimitExceeded) {
		respondPassthroughError(c, http.StatusForbidden, "asset_limit_exceeded", err.Error())
		return
	}
	if errors.Is(err, service.ErrAssetNotOwned) {
		respondPassthroughError(c, http.StatusNotFound, "resource_not_found",
			"资源不存在或不属于当前账号")
		return
	}
	respondPassthroughError(c, http.StatusInternalServerError, "asset_ownership_check_failed", err.Error())
}

// relayVolcControlPlaneResponse 回写控制面响应,途中完成落库、过滤与快照刷新。
//
// 控制面响应是小体积 JSON,可以整体读入;数据面(任务查询、SSE、文件流)仍走
// io.Copy 边收边发,不进这里。
func relayVolcControlPlaneResponse(c *gin.Context, owner service.VolcAssetOwner, action string, reqBody []byte, resp *http.Response) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		respondPassthroughError(c, http.StatusBadGateway, "read_upstream_response_failed", err.Error())
		return
	}

	// 只有 2xx 才动:失败响应里没有资源 Id,官方错误体必须原样透传。
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		filtered, ok := applyVolcAssetIsolation(c, owner, action, reqBody, respBody)
		if !ok {
			return
		}
		respBody = filtered
	}

	volcpassthrough.CopyDownstreamResponseHeaders(c.Writer.Header(), resp.Header)
	// 过滤后长度必然变化,沿用上游的 Content-Length 会让客户端读到截断的 JSON。
	// Content-Encoding 也要清掉:上游若压缩过,transport 已解压,首部会对不上。
	c.Writer.Header().Del("Content-Encoding")
	c.Writer.Header().Set("Content-Length", strconv.Itoa(len(respBody)))
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err := c.Writer.Write(respBody); err != nil {
		logger.LogError(c, "volc passthrough write response failed: "+err.Error())
	}
	c.Writer.Flush()
}

// applyVolcAssetIsolation 处理转发成功后的三件事:落库新建资源、刷新快照、过滤 List。
// 返回 false 表示已回写错误。
func applyVolcAssetIsolation(c *gin.Context, owner service.VolcAssetOwner, action string, reqBody, respBody []byte) ([]byte, bool) {
	// 落库失败不影响客户的响应:上游资产已经建好了,报错只会让客户以为没建成
	// 而重试,凭空多占一份额度。代价是产生孤儿(上游有、本地无),故必须打日志
	// 带上 resource_id,再由超管页面「上游已用 vs 本地记账」的差值兜底发现。
	if asset, err := service.RecordCreatedResource(owner, action, reqBody, respBody); err != nil {
		resourceId := ""
		if asset != nil {
			resourceId = asset.ResourceId
		}
		logger.LogError(c, fmt.Sprintf(
			"volc asset ownership record failed, orphan created: action=%s resource_id=%s err=%s",
			action, resourceId, err.Error()))
	}

	// 快照刷新只在归属已通过之后做,否则等于让任何人拿别人的响应改本地记录。
	service.RefreshSnapshotFromResponse(owner, action, respBody)

	out, changed, err := service.FilterListResponseByOwner(owner, action, respBody)
	if err != nil {
		// 过滤失败绝不能放原始列表过去 —— 那是把全渠道所有用户的素材摊给客户看。
		logger.LogError(c, "volc asset list filter failed: "+err.Error())
		respondPassthroughError(c, http.StatusInternalServerError, "asset_list_filter_failed",
			"资产列表过滤失败,请稍后重试")
		return nil, false
	}
	if changed {
		return out, true
	}
	return respBody, true
}
