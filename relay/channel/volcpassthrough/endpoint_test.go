package volcpassthrough

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

func TestIsControlPlane(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		query map[string][]string
		want  bool
	}{
		{"root with action", "/", map[string][]string{"Action": {"CreateAsset"}}, true},
		{"empty path with action", "", map[string][]string{"Action": {"ListAssets"}}, true},
		{"root without action", "/", map[string][]string{"Version": {"2024-01-01"}}, false},
		{"data plane path with action", "/api/v3/chat/completions", map[string][]string{"Action": {"X"}}, false},
		{"data plane path", "/api/v3/contents/generations/tasks", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsControlPlane(tc.path, tc.query); got != tc.want {
				t.Fatalf("IsControlPlane(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestIsAllowedDataPlaneRequest 锁住白名单只有 4 条路由。
//
// method 必须一起判定:同一路径 POST 是计费的提交、GET 是免费的查询,
// 只看路径客户就能用 GET 之外的方法绕开计费分支。
func TestIsAllowedDataPlaneRequest(t *testing.T) {
	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v3/contents/generations/tasks"},
		{http.MethodGet, "/api/v3/contents/generations/tasks"},
		{http.MethodGet, "/api/v3/contents/generations/tasks/cgt-20260101120000-abcde"},
		{http.MethodDelete, "/api/v3/contents/generations/tasks/cgt-20260101120000-abcde"},
	}
	for _, tc := range allowed {
		if !IsAllowedDataPlaneRequest(tc.method, tc.path) {
			t.Fatalf("expected %s %s allowed", tc.method, tc.path)
		}
	}

	denied := []struct {
		method string
		path   string
	}{
		// 方法不对:同路径其余方法一律拒。
		{http.MethodDelete, "/api/v3/contents/generations/tasks"},
		{http.MethodPut, "/api/v3/contents/generations/tasks"},
		{http.MethodPost, "/api/v3/contents/generations/tasks/cgt-1"},
		// {id} 必须是非空且不含 / 的单段。末尾斜杠会被归一掉,
		// 故 GET .../tasks/ 落到集合路径上(见下方 normalization 用例)。
		{http.MethodGet, "/api/v3/contents/generations/tasks/a/b"},
		{http.MethodDelete, "/api/v3/contents/generations/tasks/a/b"},
		// 收缩掉的三组宽前缀:这些路径不计费,放行等于免费开全部官方能力。
		{http.MethodPost, "/api/v3/chat/completions"},
		{http.MethodPost, "/api/v3/embeddings"},
		{http.MethodPost, "/api/v3/images/generations"},
		{http.MethodPost, "/api/v3/files"},
		{http.MethodPost, "/api/compatible/v1/chat/completions"},
		{http.MethodPost, "/api/coding/v1/messages"},
		{http.MethodPost, "/api/v3/responses"},
		// 杂项。
		{http.MethodGet, "/"},
		{http.MethodGet, "/api/v3"},
		{http.MethodGet, "/admin"},
		{http.MethodGet, ""},
	}
	for _, tc := range denied {
		if IsAllowedDataPlaneRequest(tc.method, tc.path) {
			t.Fatalf("expected %s %s denied", tc.method, tc.path)
		}
	}

	// 模板匹配前会 Trim 掉首尾斜杠（与 IsControlPlane 对根路径的判定一致），
	// 故末尾斜杠与缺前导斜杠都归一到同一路径,gin 给的 URL.Path 恒带前导斜杠。
	if !IsAllowedDataPlaneRequest(http.MethodGet, "api/v3/contents/generations/tasks") {
		t.Fatal("expected leading-slash normalization to match collection path")
	}
}

// TestIsAllowedControlPlaneAction 锁住控制面只剩 12 个 Action。
func TestIsAllowedControlPlaneAction(t *testing.T) {
	allowed := []string{
		"CreateAssetGroup", "GetAssetGroup", "ListAssetGroups", "UpdateAssetGroup", "DeleteAssetGroup",
		"CreateAsset", "GetAsset", "ListAssets", "UpdateAsset", "DeleteAsset",
		"CreateVisualValidateSession", "GetVisualValidateResult",
	}
	for _, a := range allowed {
		if !IsAllowedControlPlaneAction(a) {
			t.Fatalf("expected %q allowed", a)
		}
	}

	denied := []string{
		// 能读写站点自己火山账号配置的 Action。
		"CreateEndpoint", "ListEndpoints", "DeleteEndpoint",
		"GetApiKey", "RegeneratePersonalApiKey", "GetTeamSeatApiKey",
		"ListFoundationModels", "ActivateModels", "UpdateCustomModel",
		"ListAuditLogs", "GetInferenceUsage", "GetRecordExportTask",
		// 单个动作名的大小写是敏感的,别让客户用小写绕过白名单。
		"createasset", "LISTASSETS", "listassets",
		"", "X",
	}
	for _, a := range denied {
		if IsAllowedControlPlaneAction(a) {
			t.Fatalf("expected %q denied", a)
		}
	}
}

func TestResolveTargetDataPlane(t *testing.T) {
	cred := ChannelCredentials{BaseURL: "https://ark.cn-beijing.volces.com/"}
	target, err := ResolveTarget(cred, http.MethodPost, "/api/v3/contents/generations/tasks", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.ControlPlane {
		t.Fatal("expected data plane")
	}
	want := "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks"
	if target.URL != want {
		t.Fatalf("URL = %q, want %q", target.URL, want)
	}
}

// 数据面 host 直接取渠道 BaseURL —— 海外 host 是 ap-southeast（无 -1）,
// 与签名 scope 的 ap-southeast-1（有 -1）差一个后缀,不可互推。
func TestResolveTargetDataPlaneKeepsQuery(t *testing.T) {
	cred := ChannelCredentials{BaseURL: "https://ark.ap-southeast.bytepluses.com"}
	target, err := ResolveTarget(cred, http.MethodGet, "/api/v3/contents/generations/tasks", "page_size=10")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks?page_size=10"
	if target.URL != want {
		t.Fatalf("URL = %q, want %q", target.URL, want)
	}
}

func TestResolveTargetControlPlaneOverseas(t *testing.T) {
	cred := ChannelCredentials{BaseURL: "https://ark.ap-southeast.bytepluses.com"}
	target, err := ResolveTarget(cred, http.MethodPost, "/", "Action=CreateAsset&Version=2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if !target.ControlPlane {
		t.Fatal("expected control plane")
	}
	if target.SignRegion != "ap-southeast-1" {
		t.Fatalf("SignRegion = %q, want ap-southeast-1", target.SignRegion)
	}
	want := "https://ark.ap-southeast-1.byteplusapi.com/?Action=CreateAsset&Version=2024-01-01"
	if target.URL != want {
		t.Fatalf("URL = %q, want %q", target.URL, want)
	}
}

func TestResolveTargetControlPlaneDomestic(t *testing.T) {
	cred := ChannelCredentials{BaseURL: "https://ark.cn-beijing.volces.com"}
	target, err := ResolveTarget(cred, http.MethodPost, "", "Action=ListAssets&Version=2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if target.SignRegion != "cn-beijing" {
		t.Fatalf("SignRegion = %q, want cn-beijing", target.SignRegion)
	}
	want := "https://ark.cn-beijing.volcengineapi.com/?Action=ListAssets&Version=2024-01-01"
	if target.URL != want {
		t.Fatalf("URL = %q, want %q", target.URL, want)
	}
}

func TestResolveTargetControlPlaneRespectsOverrides(t *testing.T) {
	cred := ChannelCredentials{
		BaseURL: "https://ark.cn-beijing.volces.com",
		OtherSettings: dto.ChannelOtherSettings{
			VolcSignRegion:      "cn-shanghai",
			VolcOpenAPIEndpoint: "https://private.gateway.internal/",
		},
	}
	target, err := ResolveTarget(cred, http.MethodPost, "/", "Action=GetAsset")
	if err != nil {
		t.Fatal(err)
	}
	if target.SignRegion != "cn-shanghai" {
		t.Fatalf("SignRegion = %q", target.SignRegion)
	}
	if target.URL != "https://private.gateway.internal/?Action=GetAsset" {
		t.Fatalf("URL = %q", target.URL)
	}
}

// 白名单外的控制面 Action 必须在发出上游请求前就拒掉,
// 不能让站点托管的 AK/SK 去执行一个客户点名的动作。
func TestResolveTargetRejectsUnknownControlPlaneAction(t *testing.T) {
	cred := ChannelCredentials{BaseURL: "https://ark.cn-beijing.volces.com"}
	if _, err := ResolveTarget(cred, http.MethodPost, "/", "Action=CreateEndpoint"); err != ErrActionNotAllowed {
		t.Fatalf("err = %v, want ErrActionNotAllowed", err)
	}
}

func TestResolveTargetRejectsUnknownPath(t *testing.T) {
	cred := ChannelCredentials{BaseURL: "https://ark.cn-beijing.volces.com"}
	if _, err := ResolveTarget(cred, http.MethodGet, "/internal/debug", ""); err != ErrPathNotAllowed {
		t.Fatalf("err = %v, want ErrPathNotAllowed", err)
	}
	if _, err := ResolveTarget(cred, http.MethodPost, "/api/v3/chat/completions", ""); err != ErrPathNotAllowed {
		t.Fatalf("err = %v, want ErrPathNotAllowed", err)
	}
}

func TestCopyUpstreamRequestHeadersStripsClientAuth(t *testing.T) {
	src, _ := http.NewRequest(http.MethodPost, "https://site.example/volc/api/v3/contents/generations/tasks", nil)
	src.Header.Set("Authorization", "Bearer site-token")
	src.Header.Set("X-Api-Key", "site-token")
	src.Header.Set("Cookie", "session=1")
	src.Header.Set("Connection", "keep-alive")
	src.Header.Set("Transfer-Encoding", "chunked")
	src.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	src.Header.Set("X-Client-Request-Id", "req-1")

	dst, _ := http.NewRequest(http.MethodPost, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks", nil)
	CopyUpstreamRequestHeaders(dst, src)

	for _, h := range []string{"Authorization", "X-Api-Key", "Cookie", "Connection", "Transfer-Encoding"} {
		if dst.Header.Get(h) != "" {
			t.Fatalf("header %s should be stripped, got %q", h, dst.Header.Get(h))
		}
	}
	if dst.Header.Get("Content-Type") != "multipart/form-data; boundary=xyz" {
		t.Fatalf("Content-Type lost: %q", dst.Header.Get("Content-Type"))
	}
	if dst.Header.Get("X-Client-Request-Id") != "req-1" {
		t.Fatal("business header lost")
	}
}

func TestCopyDownstreamResponseHeadersKeepsBusinessHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("Content-Type", "video/mp4")
	src.Set("Content-Disposition", `attachment; filename="a.mp4"`)
	src.Set("Authorization", "upstream-secret-echo")
	src.Set("Connection", "close")
	src.Set("Transfer-Encoding", "chunked")

	dst := http.Header{}
	CopyDownstreamResponseHeaders(dst, src)

	if dst.Get("Content-Type") != "video/mp4" {
		t.Fatal("Content-Type lost")
	}
	if dst.Get("Content-Disposition") == "" {
		t.Fatal("Content-Disposition lost")
	}
	for _, h := range []string{"Connection", "Transfer-Encoding"} {
		if dst.Get(h) != "" {
			t.Fatalf("hop-by-hop %s leaked", h)
		}
	}
	// 响应方向只剥离逐跳首部，上游业务首部一律原样回给客户。
	if dst.Get("Authorization") != "upstream-secret-echo" {
		t.Fatal("non hop-by-hop response header should pass through")
	}
}
