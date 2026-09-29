package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

// volcPassthroughDataPlanePrefixes 官方数据面路径前缀。
//
// 客户只需把官方 host 换成站点地址,路径逐字保持官方原样:
//
//	官方  POST https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks
//	站点  POST https://<站点>/api/v3/contents/generations/tasks
//
// 这些前缀与后台管理 API(/api/user、/api/channel 等)不重叠,故可直接挂在
// 站点根上而不加任何自定义前缀 —— 官方 SDK 只改 base_url 即可直接用。
//
// 只保留 /api/v3:曾经还挂过 /api/compatible 与 /api/coding,那是通配全量透传
// 时代的产物。路由放行不等于接口放行 —— 真正的判定在
// volcpassthrough.allowedDataPlaneRoutes（4 条精确路由）,这里只是让请求能
// 进到中间件里被判定。前缀留着也只会多收一批注定 404 的请求。
var volcPassthroughDataPlanePrefixes = []string{
	"/api/v3",
}

// SetVolcPassthroughRouter 注册字节火山透传路由。
//
// 白名单内的请求保持官方原样透传:路径、query、请求体与响应的状态码、首部、
// 响应体一律不改写,站点只替换 URL 前缀与凭据。
//
// 白名单只有两组能力:Seedance 视频生成（4 条数据面路由）与私域素材库
// （12 个控制面 Action,含真人认证）。其余官方接口一律 404 —— 它们不走计费
// 链路,放行等于把 chat/completions、embedding 免费开给持站点令牌的客户。
func SetVolcPassthroughRouter(router *gin.Engine) {
	chain := func() []gin.HandlerFunc {
		return []gin.HandlerFunc{
			middleware.RouteTag("relay"),
			middleware.TokenAuth(),
			middleware.VolcPassthroughDistribute(),
			middleware.VolcAssetReferenceGuard(),
			volcPassthroughDispatch,
		}
	}

	for _, prefix := range volcPassthroughDataPlanePrefixes {
		router.Any(prefix+"/*path", chain()...)
	}

	// 控制面官方挂在 host 根路径上(POST https://<host>/?Action=X&Version=2024-01-01),
	// 而站点根被前端页面占用,无法让出。故控制面保留 /volc 前缀:
	//
	//	官方  POST https://ark.cn-beijing.volcengineapi.com/?Action=CreateAsset&Version=2024-01-01
	//	站点  POST https://<站点>/volc/?Action=CreateAsset&Version=2024-01-01
	//
	// /volc/* 同时兼容数据面写法,便于把整站统一到单一前缀。
	ctl := router.Group("/volc")
	ctl.Use(middleware.RouteTag("relay"))
	ctl.Use(middleware.TokenAuth(), middleware.VolcPassthroughDistribute(), middleware.VolcAssetReferenceGuard())
	{
		ctl.Any("/*path", volcPassthroughDispatch)
	}
	// gin 的 /*path 不匹配组根,控制面请求(POST /volc/?Action=X)需单独注册。
	ctl.Any("", volcPassthroughDispatch)
}

// volcPassthroughDispatch 按中间件的计费标记分流:
// 视频生成提交走既有 RelayTask 计费链路,其余原样字节转发、不计费。
func volcPassthroughDispatch(c *gin.Context) {
	if c.GetBool(middleware.VolcPassthroughBilledKey) {
		controller.RelayTask(c)
		return
	}
	controller.RelayVolcPassthrough(c)
}
