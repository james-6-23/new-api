package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// 透传路由必须与后台管理 API 共存:数据面挂在站点根的 /api/v3 等前缀上,
// 与 /api/user、/api/channel 等既有管理路由不能互相遮蔽,否则要么后台失效,
// 要么客户请求 404。
func TestVolcPassthroughRoutesCoexistWithApiRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	SetRelayRouter(engine)
	SetVideoRouter(engine)
	SetVolcPassthroughRouter(engine)

	want := map[string]bool{
		"/api/v3/*path": false,
		"/volc/*path":   false,
		"/volc":         false,
	}
	// 收缩白名单后不再挂这两个前缀,注册了反而会多收一批注定 404 的请求。
	gone := []string{"/api/compatible/*path", "/api/coding/*path"}
	admin := map[string]bool{
		"/api/status": false,
	}
	for _, r := range engine.Routes() {
		if _, ok := want[r.Path]; ok {
			want[r.Path] = true
		}
		if _, ok := admin[r.Path]; ok {
			admin[r.Path] = true
		}
	}
	for p, got := range want {
		if !got {
			t.Fatalf("passthrough route %s not registered", p)
		}
	}
	for p, got := range admin {
		if !got {
			t.Fatalf("admin route %s was shadowed by passthrough routes", p)
		}
	}
	for _, r := range engine.Routes() {
		for _, g := range gone {
			if r.Path == g {
				t.Fatalf("de-whitelisted prefix %s is still registered", g)
			}
		}
	}
}
