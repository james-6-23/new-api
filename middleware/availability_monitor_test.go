package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// withAvailabilityMonitorEnabled 临时设置全局开关，测试结束后自动还原。
func withAvailabilityMonitorEnabled(t *testing.T, enabled bool) {
	t.Helper()

	previous := common.AvailabilityMonitorEnabled
	common.AvailabilityMonitorEnabled = enabled

	t.Cleanup(func() {
		common.AvailabilityMonitorEnabled = previous
	})
}

// performAvailabilityRequest 构造一个挂载了被测中间件的最小路由并发起请求。
// 末端 handler 返回 200，因此响应码为 200 即代表中间件放行。
func performAvailabilityRequest(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/status/availability", AvailabilityMonitorEnabled(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/status/availability", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	return recorder
}

func TestAvailabilityMonitorEnabledAllowsWhenOn(t *testing.T) {
	withAvailabilityMonitorEnabled(t, true)

	recorder := performAvailabilityRequest(t)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}

func TestAvailabilityMonitorEnabledRejectsWhenOff(t *testing.T) {
	withAvailabilityMonitorEnabled(t, false)

	recorder := performAvailabilityRequest(t)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	// 末端 handler 不应被执行
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}
