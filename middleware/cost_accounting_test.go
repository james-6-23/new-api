package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	_ = i18n.Init()
	os.Exit(m.Run())
}

// withCostAccountingEnabled 临时设置全局开关，测试结束后自动还原。
func withCostAccountingEnabled(t *testing.T, enabled bool) {
	t.Helper()

	previous := common.CostAccountingEnabled
	common.CostAccountingEnabled = enabled

	t.Cleanup(func() {
		common.CostAccountingEnabled = previous
	})
}

// performCostAccountingRequest 构造一个挂载了被测中间件的最小路由并发起请求。
// 末端 handler 返回 200，因此响应码为 200 即代表中间件放行。
func performCostAccountingRequest(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/cost/overview", CostAccountingEnabled(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/cost/overview", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	return recorder
}

func TestCostAccountingEnabledAllowsWhenOn(t *testing.T) {
	withCostAccountingEnabled(t, true)

	recorder := performCostAccountingRequest(t)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}

func TestCostAccountingEnabledRejectsWhenOff(t *testing.T) {
	withCostAccountingEnabled(t, false)

	recorder := performCostAccountingRequest(t)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	// 末端 handler 不应被执行
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}
