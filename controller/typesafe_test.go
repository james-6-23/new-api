package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	typeSafeGatewayKey   = "typesafegatewaytesttoken"
	typeSafeInitialQuota = 100000
	typeSafeTestBody     = `{"model":"jev-latest","state":{"text":"Please help urgently","zero":0},"questions":{"urgent":{"type":"noul","instructions":"Is it urgent?"},"team":{"type":"choice","instructions":"Which team?","criteria":{"technical":"Integration failures","billing":null}},"severity":{"type":"score","instructions":"Rate severity","criteria":["low","high"]}},"extra":false}`
	typeSafeTestResponse = `{"model":"jev-1.13.0","answers":{"urgent":{"type":"noul","noul":0},"team":{"type":"choice","choice":"technical","confidence":1,"probabilities":{"technical":1,"billing":0}},"severity":{"type":"score","score":0,"confidence":1,"legend":{"0":"low","1":"high"},"probabilities":{"0":1,"1":0}}},"usage":{"input_tokens":1000,"output_tokens":50},"extra":false}`
	// 1000 input tokens * 0.021 model ratio; output is free.
	typeSafeExpectedQuota = 21
)

func setupTypeSafeGateway(t *testing.T, baseURL, upstreamKey string) (*gin.Engine, *model.Channel) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prevPath := common.SQLitePath
	prevDB, prevLogDB := model.DB, model.LOG_DB
	prevMaster, prevRedis, prevMemory := common.IsMasterNode, common.RedisEnabled, common.MemoryCacheEnabled
	prevBatch, prevLog := common.BatchUpdateEnabled, common.LogConsumeEnabled
	prevCount, prevRetry, prevPre := constant.CountToken, common.RetryTimes, common.PreConsumedQuota
	prevRatios, prevCompletion, prevPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString(), ratio_setting.ModelPrice2JSONString()

	common.SQLitePath = filepath.Join(t.TempDir(), "typesafe-test.db")
	common.IsMasterNode = true
	common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, false, true
	constant.CountToken, common.RetryTimes, common.PreConsumedQuota = false, 0, 500
	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.SQLitePath = prevPath
		model.DB, model.LOG_DB = prevDB, prevLogDB
		common.IsMasterNode, common.RedisEnabled, common.MemoryCacheEnabled = prevMaster, prevRedis, prevMemory
		common.BatchUpdateEnabled, common.LogConsumeEnabled = prevBatch, prevLog
		constant.CountToken, common.RetryTimes, common.PreConsumedQuota = prevCount, prevRetry, prevPre
		_ = ratio_setting.UpdateModelRatioByJSONString(prevRatios)
		_ = ratio_setting.UpdateCompletionRatioByJSONString(prevCompletion)
		_ = ratio_setting.UpdateModelPriceByJSONString(prevPrices)
	})

	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"jev-latest":0.021}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"jev-latest":0}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	service.InitHttpClient()

	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "typesafe-test", Password: "x", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: typeSafeInitialQuota}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 1, UserId: 1, Name: "typesafe-test", Key: typeSafeGatewayKey, Status: common.TokenStatusEnabled, RemainQuota: typeSafeInitialQuota, ExpiredTime: -1}).Error)
	ch := &model.Channel{Type: constant.ChannelTypeTypeSafe, Name: "typesafe-test", Key: upstreamKey, Models: "jev-latest", Group: "default", BaseURL: &baseURL, Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0)}
	require.NoError(t, ch.Insert())

	engine := gin.New()
	engine.Use(middleware.BodyStorageCleanup(), middleware.TokenAuth(), middleware.ModelRequestRateLimit(), middleware.Distribute())
	engine.POST("/v1/systemone", func(c *gin.Context) { Relay(c, types.RelayFormatTypeSafe) })
	engine.POST("/v1/chat/completions", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAI) })
	return engine, ch
}

func requestTypeSafe(t *testing.T, engine http.Handler, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	return w
}

// Refunds run asynchronously, so poll until balances settle.
func assertTypeSafeQuota(t *testing.T, used int) {
	t.Helper()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var user model.User
		var token model.Token
		require.NoError(collect, model.DB.First(&user, 1).Error)
		require.NoError(collect, model.DB.First(&token, 1).Error)
		assert.Equal(collect, typeSafeInitialQuota-used, user.Quota)
		assert.Equal(collect, typeSafeInitialQuota-used, token.RemainQuota)
		assert.Equal(collect, used, token.UsedQuota)
	}, 3*time.Second, 20*time.Millisecond)
}

func TestTypeSafeGatewayBillingAndDiscovery(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"models":[{"name":"jev-latest","description":"Latest"},{"name":"jev-preview"}]}`)
			return
		}
		if r.URL.Path != "/v1/systemone" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := common.DecodeJson(r.Body, &body); err != nil || body["model"] != "jev-1.13.0" || body["extra"] != false {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"detail":"unexpected body"}`)
			return
		}
		calls.Add(1)
		_, _ = io.WriteString(w, typeSafeTestResponse)
	}))
	defer upstream.Close()

	engine, ch := setupTypeSafeGateway(t, upstream.URL+"/v1/", "upstream-key")
	ch.ModelMapping = common.GetPointer(`{"jev-latest":"jev-1.13.0"}`)
	require.NoError(t, ch.Save())

	w := requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, typeSafeGatewayKey)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, typeSafeTestResponse, w.Body.String())
	require.EqualValues(t, 1, calls.Load())
	assertTypeSafeQuota(t, typeSafeExpectedQuota)

	var logs []model.Log
	require.NoError(t, model.DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, 1000, logs[0].PromptTokens)
	require.Equal(t, 50, logs[0].CompletionTokens)
	require.Equal(t, typeSafeExpectedQuota, logs[0].Quota)
	require.Equal(t, "jev-latest", logs[0].ModelName)

	for _, base := range []string{upstream.URL, upstream.URL + "/", upstream.URL + "/v1", upstream.URL + "/v1/"} {
		ch.BaseURL = common.GetPointer(base)
		names, err := fetchChannelUpstreamModelIDs(ch)
		require.NoError(t, err)
		require.Equal(t, []string{"jev-latest", "jev-preview"}, names)
	}
}

func TestTypeSafeGatewayUpstreamErrorsRefund(t *testing.T) {
	for _, status := range []int{401, 422, 429, 529, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := `{"detail":[{"msg":"invalid question","type":"validation_error"}]}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()

			engine, _ := setupTypeSafeGateway(t, upstream.URL, "upstream-key")
			w := requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, typeSafeGatewayKey)
			if status == http.StatusOK {
				// Success status without answers/usage is a bad upstream response.
				require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
			} else {
				require.Equal(t, status, w.Code, w.Body.String())
				require.JSONEq(t, body, w.Body.String())
				require.Equal(t, "2", w.Header().Get("Retry-After"))
			}
			assertTypeSafeQuota(t, 0)
		})
	}
}

func TestTypeSafeGatewayRejectsInvalidRequests(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	engine, _ := setupTypeSafeGateway(t, upstream.URL, "upstream-key")
	w := requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, "invalidtoken")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	w = requestTypeSafe(t, engine, "/v1/chat/completions", `{"model":"jev-latest","messages":[{"role":"user","content":"hi"}]}`, typeSafeGatewayKey)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "/v1/systemone")
	for _, body := range []string{
		`{"model":"jev-latest","state":false,"questions":{}}`,
		`{"model":"jev-latest","state":"x","questions":{},"stream":true}`,
	} {
		w = requestTypeSafe(t, engine, "/v1/systemone", body, typeSafeGatewayKey)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	require.Zero(t, calls.Load())
	assertTypeSafeQuota(t, 0)
}

func TestTypeSafeGatewayRetryIsolation(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = common.DecodeJson(r.Body, &body)
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "Bearer first-key" {
			if body["model"] != "jev-preview" || body["attempt"] != "first" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(529)
			_, _ = io.WriteString(w, `{"detail":"busy"}`)
			return
		}
		_, hasAttempt := body["attempt"]
		if r.Header.Get("Authorization") != "Bearer second-key" || body["model"] != "jev-1.13.0" || hasAttempt || body["extra"] != false {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"detail":"leaked state from previous attempt"}`)
			return
		}
		_, _ = io.WriteString(w, typeSafeTestResponse)
	}))
	defer upstream.Close()

	engine, first := setupTypeSafeGateway(t, upstream.URL, "first-key")
	common.RetryTimes = 1
	first.Priority = common.GetPointer(int64(10))
	first.ModelMapping = common.GetPointer(`{"jev-latest":"jev-preview"}`)
	first.ParamOverride = common.GetPointer(`{"attempt":"first"}`)
	require.NoError(t, first.Save())
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", first.Id).Update("priority", 10).Error)
	second := &model.Channel{Type: constant.ChannelTypeTypeSafe, Name: "second", Key: "second-key", BaseURL: &upstream.URL, Models: "jev-latest", Group: "default", Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0), ModelMapping: common.GetPointer(`{"jev-latest":"jev-1.13.0"}`)}
	require.NoError(t, second.Insert())

	w := requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, typeSafeGatewayKey)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, typeSafeTestResponse, w.Body.String())
	require.EqualValues(t, 2, attempts.Load())
	assertTypeSafeQuota(t, typeSafeExpectedQuota)
}

func TestTypeSafeRejectsUnsupportedFormatsBeforeUpgrade(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatOpenAIRealtime} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime?model=jev-latest", nil)
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeTypeSafe)
		Relay(c, format)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "/v1/systemone")
	}
}

func TestTypeSafeChannelTest(t *testing.T) {
	var sawRequest atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request dto.TypeSafeRequest
		if r.URL.Path != "/v1/systemone" || common.DecodeJson(r.Body, &request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, hasStream := request.Fields["stream"]; hasStream || !strings.Contains(string(request.Fields["questions"]), `"noul"`) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, typeSafeTestResponse)
	}))
	defer upstream.Close()

	_, ch := setupTypeSafeGateway(t, upstream.URL, "upstream-key")
	result := testChannel(ch, 1, "", "", true)
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	require.True(t, sawRequest.Load())

	result = testChannel(ch, 1, "", string(constant.EndpointTypeOpenAI), false)
	require.Error(t, result.localErr)
}
