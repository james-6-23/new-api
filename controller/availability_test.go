package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestAvailabilityRouteShapeRegisters 确认 /status/availability 与既有的
// /status、/status/test 共存时 gin 不会因路由冲突 panic。
// gin 的冲突检测发生在注册期，构建通过并不代表路由可用。
func TestAvailabilityRouteShapeRegisters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	require.NotPanics(t, func() {
		r := gin.New()
		api := r.Group("/api")
		api.GET("/status", func(c *gin.Context) {})
		api.GET("/status/test", func(c *gin.Context) {})
		group := api.Group("/status/availability")
		group.GET("", func(c *gin.Context) { c.String(http.StatusOK, "avail") })
		group.GET("/rpm", func(c *gin.Context) { c.String(http.StatusOK, "rpm") })

		// 确认两个路径都能被真实命中
		for path, want := range map[string]string{
			"/api/status/availability":     "avail",
			"/api/status/availability/rpm": "rpm",
		} {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, rec.Code, path)
			require.Equal(t, want, rec.Body.String(), path)
		}
	})
}

func TestAvailabilityCacheRoundTrip(t *testing.T) {
	t.Cleanup(func() {
		availabilityCacheMu.Lock()
		availabilityCache = map[string]availabilityCacheEntry{}
		availabilityCacheMu.Unlock()
	})

	_, ok := loadAvailabilityCache("group")
	require.False(t, ok, "空缓存不应命中")

	storeAvailabilityCache("group", model.AvailabilityResponse{Dimension: "group", GeneratedAt: 42})

	got, ok := loadAvailabilityCache("group")
	require.True(t, ok)
	require.Equal(t, int64(42), got.GeneratedAt)

	// 维度隔离：group 的缓存不能被 model 读到
	_, ok = loadAvailabilityCache("model")
	require.False(t, ok, "不同维度必须使用独立缓存槽")
}

func TestAvailabilityCacheExpires(t *testing.T) {
	t.Cleanup(func() {
		availabilityCacheMu.Lock()
		availabilityCache = map[string]availabilityCacheEntry{}
		availabilityCacheMu.Unlock()
	})

	// 手动写入一条已过期的记录
	availabilityCacheMu.Lock()
	availabilityCache["group"] = availabilityCacheEntry{
		payload:  model.AvailabilityResponse{GeneratedAt: 1},
		cachedAt: time.Now().Add(-availabilityCacheTTL - time.Second),
	}
	availabilityCacheMu.Unlock()

	_, ok := loadAvailabilityCache("group")
	require.False(t, ok, "超过 TTL 的缓存必须失效")
}
