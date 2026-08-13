package doubao

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

// 这些用例锁定 Seedance 视频任务的上游路径分流:
//   - 存量渠道(other_settings 无 asset_provider)必须继续走火山方舟 ModelArk 路径;
//   - asset_provider=cloudwise 时提交与查询同时切到第三方网关路径。
//
// 请求体、响应体、状态词在两个网关上完全一致,所以这里只断言路径。

const (
	modelArkGeneratePath = "/api/v3/contents/generations/tasks"
	modelArkQueryPrefix  = "/api/v3/contents/generations/tasks/"

	cloudwiseGeneratePath = "/api/v1/aiproducts/video/seedance"
	cloudwiseQueryPrefix  = "/api/v1/aiproducts/video/seedance/tasks/"

	testTaskID = "cgt-20250101-abcde"
)

// ensureHTTPClient 初始化 service 包的全局 http client。
// FetchTask 内部通过 service.GetHttpClientWithProxy("") 取它,而该变量默认为 nil,
// 只有 main 会调 InitHttpClient;测试进程里必须自己初始化,否则空指针 panic。
func ensureHTTPClient(t *testing.T) {
	t.Helper()
	service.InitHttpClient()
}

// newPathCapturingServer 返回一个把收到的请求路径写进 gotPath 的测试服务器。
func newPathCapturingServer(t *testing.T, gotPath *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + testTaskID + `","status":"succeeded"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEndpointRouting_ByAssetProvider 覆盖请求链路(InitChannelMeta 已把渠道配置塞进
// RelayInfo)下的路径选择:只有显式 cloudwise 才切换,空值/未知值一律回落官方路径。
func TestEndpointRouting_ByAssetProvider(t *testing.T) {
	ensureHTTPClient(t)

	cases := []struct {
		name         string
		provider     string
		wantGenerate string
		wantQuery    string
	}{
		// 零迁移:存量渠道的 settings 里根本没有 asset_provider 这个 key。
		{"absent/empty keeps ModelArk", "", modelArkGeneratePath, modelArkQueryPrefix + testTaskID},
		{"explicit byteplus keeps ModelArk", dto.AssetProviderBytePlus, modelArkGeneratePath, modelArkQueryPrefix + testTaskID},
		// 配置写错时必须回落官方路径,而不是把请求发去一个不存在的网关。
		{"unknown value falls back to ModelArk", "wat", modelArkGeneratePath, modelArkQueryPrefix + testTaskID},
		{"cloudwise switches both paths", dto.AssetProviderCloudwise, cloudwiseGeneratePath, cloudwiseQueryPrefix + testTaskID},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			srv := newPathCapturingServer(t, &gotPath)

			a := &TaskAdaptor{}
			a.Init(&relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:          constant.ChannelTypeDoubaoVideo,
					ChannelBaseUrl:       srv.URL,
					ApiKey:               "sk-test",
					ChannelOtherSettings: dto.ChannelOtherSettings{AssetProvider: tc.provider},
				},
			})

			gotURL, err := a.BuildRequestURL(nil)
			require.NoError(t, err)
			require.Equal(t, srv.URL+tc.wantGenerate, gotURL, "generate URL for provider %q", tc.provider)

			resp, err := a.FetchTask(srv.URL, "sk-test", map[string]any{"task_id": testTaskID}, "")
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, tc.wantQuery, gotPath, "query path for provider %q", tc.provider)
		})
	}
}

// TestVideoPollingRelayInfoCarriesAssetProviderIntoQueryPath 是"轮询链路丢渠道配置"
// 这一类 bug 的回归护栏。
//
// 轮询不经过 InitChannelMeta —— 它没有 gin.Context,RelayInfo 由
// service.BuildVideoPollingRelayInfo 手工构造。历史实现只填了 BaseUrl 与 ApiKey:
// 一旦 ChannelOtherSettings 再次被漏掉,cloudwise 渠道提交成功但查询会退回 ModelArk
// 路径而 404,任务永远停在 in_progress,用户配额一直被预扣 —— 比直接失败更糟。
//
// 该用例直接消费生产构造器(而非复制它的逻辑),所以字段被删时必然失败。
func TestVideoPollingRelayInfoCarriesAssetProviderIntoQueryPath(t *testing.T) {
	ensureHTTPClient(t)

	cases := []struct {
		name      string
		provider  string
		wantQuery string
	}{
		{"cloudwise channel polls the cloudwise path", dto.AssetProviderCloudwise, cloudwiseQueryPrefix + testTaskID},
		{"legacy channel (no asset_provider) polls ModelArk", "", modelArkQueryPrefix + testTaskID},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			srv := newPathCapturingServer(t, &gotPath)

			// 走真实的 settings JSON 往返,顺带覆盖 GetOtherSettings 的反序列化。
			settingsJSON := ""
			if tc.provider != "" {
				raw, err := common.Marshal(dto.ChannelOtherSettings{AssetProvider: tc.provider})
				require.NoError(t, err)
				settingsJSON = string(raw)
			}

			baseURL := srv.URL
			ch := &model.Channel{
				Type:          constant.ChannelTypeDoubaoVideo,
				Key:           "sk-test",
				BaseURL:       &baseURL,
				OtherSettings: settingsJSON,
			}

			info := service.BuildVideoPollingRelayInfo(ch)

			// 直接锁定构造器契约:轮询用的 RelayInfo 必须带齐这三样。
			require.NotNil(t, info.ChannelMeta, "polling RelayInfo must carry ChannelMeta")
			require.Equal(t, srv.URL, info.ChannelBaseUrl, "polling RelayInfo must carry base url")
			require.Equal(t, "sk-test", info.ApiKey, "polling RelayInfo must carry api key")
			require.Equal(t, tc.provider, info.ChannelOtherSettings.AssetProvider,
				"polling RelayInfo must carry ChannelOtherSettings; dropping it silently sends queries to the wrong gateway")

			a := &TaskAdaptor{}
			a.Init(info)

			resp, err := a.FetchTask(ch.GetBaseURL(), ch.Key, map[string]any{"task_id": testTaskID}, "")
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, tc.wantQuery, gotPath, "polling query path for provider %q", tc.provider)
		})
	}
}
