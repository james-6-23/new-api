package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() {
	// Tests run independently of main.go; gin will otherwise log "[GIN-debug] [WARNING]"
	// noise that pollutes verbose test output. Release mode silences it.
	gin.SetMode(gin.TestMode)
}

func TestBuildAutoCreatePreviewHandler_HappyPath(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/auto/preview", nil)

	custom := operation_setting.AutoCreateUserSetting{
		UsernamePrefix:        "QA-",
		UsernameSuffixLength:  3,
		UsernameSuffixCharset: operation_setting.AutoCreateUserCharsetDigits,
		PasswordMode:          operation_setting.AutoCreateUserPasswordSameAsUsername,
		DefaultQuota:          77,
		DefaultGroup:          "qa-group",
	}
	getSetting := func() operation_setting.AutoCreateUserSetting { return custom }
	randomFn := func(n int, charset string) string { return charset + "-stub" }
	exists := service.UsernameExistsFunc(func(name string) (bool, error) {
		return false, nil // never collides
	})

	handler := BuildAutoCreatePreviewHandler(getSetting, randomFn, exists)
	handler(c)

	require.Equal(t, http.StatusOK, w.Code)

	body := decodeBody(t, w)
	require.Equal(t, true, body["success"], "raw body=%s", w.Body.String())
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "data field is missing or wrong shape; body=%s", w.Body.String())
	require.Equal(t, "QA-digits-stub", data["username"])
	require.Equal(t, "QA-digits-stub", data["password"], "same_as_username mode echoes the username")
	require.Equal(t, "qa-group", data["group"])
	require.EqualValues(t, 77, data["quota"])
}

func TestBuildAutoCreatePreviewHandler_CollisionExhaustedReturnsApiError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/auto/preview", nil)

	getSetting := func() operation_setting.AutoCreateUserSetting {
		return operation_setting.AutoCreateUserSetting{
			UsernamePrefix:        "x",
			UsernameSuffixLength:  1,
			UsernameSuffixCharset: operation_setting.AutoCreateUserCharsetAlphanumeric,
			PasswordMode:          operation_setting.AutoCreateUserPasswordSameAsUsername,
			DefaultGroup:          "default",
		}
	}
	randomFn := func(int, string) string { return "0" }
	exists := service.UsernameExistsFunc(func(name string) (bool, error) {
		return true, nil // always collides → exhausted
	})

	handler := BuildAutoCreatePreviewHandler(getSetting, randomFn, exists)
	handler(c)

	require.Equal(t, http.StatusOK, w.Code, "ApiErrorI18n still returns HTTP 200 per project convention")
	body := decodeBody(t, w)
	require.Equal(t, false, body["success"])
	require.NotEmpty(t, body["message"], "collision-exhausted MUST surface a non-empty translated message")
}

func TestBuildAutoCreatePreviewHandler_UnexpectedExistsErrorIsBubbled(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/auto/preview", nil)

	sentinel := errors.New("db connection refused")
	getSetting := func() operation_setting.AutoCreateUserSetting {
		return operation_setting.AutoCreateUserSetting{
			UsernamePrefix:       "x",
			UsernameSuffixLength: 1,
			PasswordMode:         operation_setting.AutoCreateUserPasswordSameAsUsername,
			DefaultGroup:         "default",
		}
	}
	randomFn := func(int, string) string { return "0" }
	exists := service.UsernameExistsFunc(func(name string) (bool, error) { return false, sentinel })

	handler := BuildAutoCreatePreviewHandler(getSetting, randomFn, exists)
	handler(c)

	require.Equal(t, http.StatusOK, w.Code)
	body := decodeBody(t, w)
	require.Equal(t, false, body["success"])
	require.Contains(t, body["message"], "db connection refused",
		"non-collision errors must surface verbatim through common.ApiError so operators can see the underlying cause")
}

// Wires the production-default handler to confirm it factory-injects the real
// AutoCreateUserSetting accessor. We don't actually hit the DB; we override the
// setting via SetAutoCreateUserSettingForTest and supply a randomFn that does
// not collide.
func TestGetAutoCreateUserPreview_UsesPackageLevelSetting(t *testing.T) {
	defer operation_setting.ResetAutoCreateUserSettingForTest()
	operation_setting.SetAutoCreateUserSettingForTest(operation_setting.AutoCreateUserSetting{
		UsernamePrefix:        "PROD-",
		UsernameSuffixLength:  2,
		UsernameSuffixCharset: operation_setting.AutoCreateUserCharsetLetters,
		PasswordMode:          operation_setting.AutoCreateUserPasswordSameAsUsername,
		DefaultGroup:          "default",
		DefaultQuota:          0, // forces fallback to common.QuotaForNewUser
	})
	// Capture and restore the global QuotaForNewUser so the fallback assertion is
	// deterministic.
	origQuota := common.QuotaForNewUser
	defer func() { common.QuotaForNewUser = origQuota }()
	common.QuotaForNewUser = 42

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/auto/preview", nil)

	// Stub the random + exists deps but keep the production setting/getter wiring.
	handler := BuildAutoCreatePreviewHandler(
		operation_setting.GetAutoCreateUserSetting,
		func(n int, _ string) string { return "ab"[:n] },
		func(string) (bool, error) { return false, nil },
	)
	handler(c)

	require.Equal(t, http.StatusOK, w.Code)
	body := decodeBody(t, w)
	require.Equal(t, true, body["success"])
	data := body["data"].(map[string]any)
	require.Equal(t, "PROD-ab", data["username"])
	require.EqualValues(t, 42, data["quota"], "DefaultQuota=0 must fall back to common.QuotaForNewUser via the service")
}

// decodeBody is a tiny helper so each test stays focused on the assertion, not the plumbing.
func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body), "body=%s", w.Body.String())
	return body
}

// _ silences unused-import lint when the test list shrinks during local iteration.
var _ dto.AutoCreateUserPreviewResponse
