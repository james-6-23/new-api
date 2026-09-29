package assetiso

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

// ---------------------------------------------------------------------------
// ParseSingleResourceTarget —— 单资源 Action 的目标 Id
// ---------------------------------------------------------------------------

func TestParseSingleResourceTarget_Accepted(t *testing.T) {
	cases := []struct {
		action   string
		body     string
		wantType string
		wantId   string
	}{
		{"GetAsset", `{"Id":"asset-1"}`, resourceTypeAsset, "asset-1"},
		{"UpdateAsset", `{"Id":"asset-2","Name":"n"}`, resourceTypeAsset, "asset-2"},
		{"DeleteAsset", `{"Id":"asset-3"}`, resourceTypeAsset, "asset-3"},
		{"GetAssetGroup", `{"Id":"group-1"}`, resourceTypeAssetGroup, "group-1"},
		{"UpdateAssetGroup", `{"Id":"group-2"}`, resourceTypeAssetGroup, "group-2"},
		{"DeleteAssetGroup", `{"Id":"group-3"}`, resourceTypeAssetGroup, "group-3"},
	}
	for _, c := range cases {
		target, ok := ParseSingleResourceTarget(c.action, []byte(c.body))
		if !ok {
			t.Fatalf("%s: expected ok=true", c.action)
		}
		if target.ResourceType != c.wantType {
			t.Errorf("%s: ResourceType = %q, want %q", c.action, target.ResourceType, c.wantType)
		}
		if target.ResourceId != c.wantId {
			t.Errorf("%s: ResourceId = %q, want %q", c.action, target.ResourceId, c.wantId)
		}
	}
}

// 非单资源 Action 必须返回 ok=false,否则调用方会去校验一个不存在的目标。
func TestParseSingleResourceTarget_NotSingleResource(t *testing.T) {
	for _, action := range []string{
		"CreateAsset", "ListAssets", "CreateAssetGroup", "ListAssetGroups",
		"CreateVisualValidateSession", "GetVisualValidateResult",
		"ChatCompletions", "",
	} {
		if _, ok := ParseSingleResourceTarget(action, []byte(`{"Id":"x"}`)); ok {
			t.Errorf("%s: expected ok=false, it is not a single-resource action", action)
		}
	}
}

// Id 缺失时 ok 必须仍为 true：空 id 永远不属于任何人，交给归属校验去拒。
// 若这里返回 false，调用方会跳过校验，等于把请求直接放行给上游。
func TestParseSingleResourceTarget_MissingIdStillReturnsTrue(t *testing.T) {
	for _, body := range []string{`{}`, `{"Id":123}`, `{"Id":""}`, `not json`, ``} {
		target, ok := ParseSingleResourceTarget("GetAsset", []byte(body))
		if !ok {
			t.Fatalf("body %q: expected ok=true so ownership check rejects it", body)
		}
		if target.ResourceId != "" {
			t.Errorf("body %q: ResourceId = %q, want empty", body, target.ResourceId)
		}
	}
}

// ---------------------------------------------------------------------------
// ParseCreateAssetGroupRef —— CreateAsset 的 GroupId
// ---------------------------------------------------------------------------

func TestParseCreateAssetGroupRef(t *testing.T) {
	groupId, ok := ParseCreateAssetGroupRef("CreateAsset", []byte(`{"GroupId":"group-1","Name":"n"}`))
	if !ok {
		t.Fatal("expected ok=true for CreateAsset")
	}
	if groupId != "group-1" {
		t.Fatalf("GroupId = %q, want group-1", groupId)
	}

	// GroupId 缺失时仍返回 ok=true，由归属校验拒掉（往未知组里塞素材）。
	groupId, ok = ParseCreateAssetGroupRef("CreateAsset", []byte(`{"Name":"n"}`))
	if !ok || groupId != "" {
		t.Fatalf("missing GroupId: got (%q,%v), want (\"\",true)", groupId, ok)
	}

	// 其余 Action 一律 ok=false。
	for _, action := range []string{"GetAsset", "CreateAssetGroup", "ListAssets", ""} {
		if _, ok := ParseCreateAssetGroupRef(action, []byte(`{"GroupId":"g"}`)); ok {
			t.Errorf("%s: expected ok=false", action)
		}
	}
}

// ---------------------------------------------------------------------------
// ListResourceType
// ---------------------------------------------------------------------------

func TestListResourceType(t *testing.T) {
	cases := map[string]string{
		"ListAssets":      resourceTypeAsset,
		"ListAssetGroups": resourceTypeAssetGroup,
	}
	for action, want := range cases {
		got, ok := ListResourceType(action)
		if !ok || got != want {
			t.Errorf("%s: got (%q,%v), want (%q,true)", action, got, ok, want)
		}
	}
	for _, action := range []string{"GetAsset", "CreateAsset", "GetVisualValidateResult", ""} {
		if _, ok := ListResourceType(action); ok {
			t.Errorf("%s: expected ok=false", action)
		}
	}
}

// 资源类型字面量必须与 model.VolcResourceType* 一致。assetiso 不 import model
// （relay 层不依赖 model 层），故在此锁定取值，防止两边各自漂移。
func TestResourceTypeLiteralsMatchModel(t *testing.T) {
	if resourceTypeAsset != "asset" {
		t.Fatalf("resourceTypeAsset = %q, want \"asset\"", resourceTypeAsset)
	}
	if resourceTypeAssetGroup != "asset_group" {
		t.Fatalf("resourceTypeAssetGroup = %q, want \"asset_group\"", resourceTypeAssetGroup)
	}
}

// ---------------------------------------------------------------------------
// ParseCreatedResource
// ---------------------------------------------------------------------------

func TestParseCreatedResource_CreateAsset(t *testing.T) {
	req := []byte(`{"GroupId":"group-1","Name":"my-clip","AssetType":"Video"}`)
	resp := []byte(`{"ResponseMetadata":{"RequestId":"r"},"Result":{"Id":"asset-1"}}`)

	got, ok := ParseCreatedResource("CreateAsset", req, resp)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.ResourceType != resourceTypeAsset || got.ResourceId != "asset-1" {
		t.Fatalf("got %+v, want asset/asset-1", got)
	}
	// 创建响应只回 Id，其余字段只能从请求体补。
	if got.GroupId != "group-1" || got.Name != "my-clip" || got.AssetType != "Video" {
		t.Fatalf("snapshot fields not taken from request body: %+v", got)
	}
}

func TestParseCreatedResource_CreateAssetGroup(t *testing.T) {
	got, ok := ParseCreatedResource("CreateAssetGroup",
		[]byte(`{"Name":"ag-1"}`),
		[]byte(`{"ResponseMetadata":{},"Result":{"Id":"group-1"}}`))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.ResourceType != resourceTypeAssetGroup || got.ResourceId != "group-1" || got.Name != "ag-1" {
		t.Fatalf("got %+v", got)
	}
}

// CreateVisualValidateResult 官方示例不带信封，直接返回 {"GroupId": ...}；
// 必须两层都试，否则真人认证换来的资产组不落库 —— 那等于给所有人共用。
func TestParseCreatedResource_VisualValidateTwoLevelEnvelope(t *testing.T) {
	// 不带信封（官方示例形状）
	got, ok := ParseCreatedResource("GetVisualValidateResult", nil, []byte(`{"GroupId":"group-real"}`))
	if !ok || got.ResourceType != resourceTypeAssetGroup || got.ResourceId != "group-real" {
		t.Fatalf("flat envelope: got (%+v,%v)", got, ok)
	}

	// 带信封（官方哪天补上信封也不能漏记）
	got, ok = ParseCreatedResource("GetVisualValidateResult", nil,
		[]byte(`{"ResponseMetadata":{},"Result":{"GroupId":"group-real-2"}}`))
	if !ok || got.ResourceId != "group-real-2" {
		t.Fatalf("nested envelope: got (%+v,%v)", got, ok)
	}

	// 两层都没有 GroupId 时不落库
	if _, ok := ParseCreatedResource("GetVisualValidateResult", nil, []byte(`{"Result":{}}`)); ok {
		t.Fatal("expected ok=false when GroupId absent in both layers")
	}
}

// CreateVisualValidateSession 返回的是 30 分钟有效的临时 BytedToken，不是资源。
// 落库只会留下一行永远查不到的垃圾。
func TestParseCreatedResource_SessionNotRecorded(t *testing.T) {
	resp := []byte(`{"ResponseMetadata":{},"Result":{"BytedToken":"tok","GroupId":"group-x"}}`)
	if _, ok := ParseCreatedResource("CreateVisualValidateSession", nil, resp); ok {
		t.Fatal("CreateVisualValidateSession must not be recorded as a resource")
	}
}

func TestParseCreatedResource_MissingId(t *testing.T) {
	// 响应成功但没回 Id（或干脆不是 JSON）时不落库，避免写进一行空 id 记录。
	cases := []struct {
		action string
		resp   string
	}{
		{"CreateAsset", `{"ResponseMetadata":{},"Result":{}}`},
		{"CreateAsset", `not json`},
		{"CreateAsset", ``},
		{"CreateAssetGroup", `{"ResponseMetadata":{},"Result":{"Id":""}}`},
	}
	for _, c := range cases {
		if _, ok := ParseCreatedResource(c.action, []byte(`{}`), []byte(c.resp)); ok {
			t.Errorf("%s with resp %q: expected ok=false", c.action, c.resp)
		}
	}

	// 非创建类 Action 一律不落库
	for _, action := range []string{"GetAsset", "ListAssets", "DeleteAsset", ""} {
		if _, ok := ParseCreatedResource(action, []byte(`{}`), []byte(`{"Result":{"Id":"x"}}`)); ok {
			t.Errorf("%s: expected ok=false", action)
		}
	}
}

// ---------------------------------------------------------------------------
// ParseResourceSnapshot / ParseListedResources
// ---------------------------------------------------------------------------

func TestParseResourceSnapshot(t *testing.T) {
	resp := []byte(`{"ResponseMetadata":{},"Result":{
		"Id":"asset-1","GroupId":"group-1","Name":"clip","AssetType":"Video","Status":"Active"}}`)
	got := ParseResourceSnapshot(resp)
	want := CreatedResource{
		ResourceId: "asset-1", GroupId: "group-1", Name: "clip",
		AssetType: "Video", Status: "Active",
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// 非信封响应不 panic，全字段为空。
	if got := ParseResourceSnapshot([]byte(`garbage`)); got != (CreatedResource{}) {
		t.Fatalf("non-json: got %+v, want zero value", got)
	}
}

func TestParseListedResources(t *testing.T) {
	resp := []byte(`{"ResponseMetadata":{},"Result":{"Items":[
		{"Id":"a1","GroupId":"g1","Name":"n1","AssetType":"Image","Status":"Active"},
		{"Id":"a2","GroupId":"g1","Name":"n2","AssetType":"Video","Status":"Processing"}
	],"TotalCount":2}}`)
	items := ParseListedResources(resp)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ResourceId != "a1" || items[1].ResourceId != "a2" {
		t.Fatalf("order not preserved: %+v", items)
	}
	if items[1].AssetType != "Video" || items[1].Status != "Processing" {
		t.Fatalf("field mapping wrong: %+v", items[1])
	}

	// 无 Items / 非 JSON 时返回空切片而不是 nil，调用方可直接 range。
	if got := ParseListedResources([]byte(`{"Result":{}}`)); len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
	if got := ParseListedResources([]byte(`nope`)); len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// FilterListResponse —— List 越权过滤
// ---------------------------------------------------------------------------

// TotalCount 必须一起改小：否则客户看到「共 100 条」却只拿到自己的 3 条，
// 会以为站点丢数据。NextToken 是官方游标，站点无法也不该重算，原样保留。
func TestFilterListResponse_RewritesTotalCountKeepsNextToken(t *testing.T) {
	resp := []byte(`{"ResponseMetadata":{},"Result":{"Items":[
		{"Id":"mine-1"},{"Id":"other-1"},{"Id":"mine-2"},{"Id":"other-2"}
	],"TotalCount":100,"NextToken":"cursor-abc"}}`)

	mine := map[string]bool{"mine-1": true, "mine-2": true}
	out, kept, changed := FilterListResponse(resp, func(r ListedResource) bool { return mine[r.ResourceId] })
	if !changed {
		t.Fatal("expected changed=true")
	}
	if kept != 2 {
		t.Fatalf("kept = %d, want 2", kept)
	}

	var parsed map[string]any
	if err := common.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("filtered output is not valid JSON: %v", err)
	}
	result := parsed["Result"].(map[string]any)
	if got := result["TotalCount"].(float64); got != 2 {
		t.Fatalf("TotalCount = %v, want 2 (must shrink with the filtered items)", got)
	}
	if got := result["NextToken"].(string); got != "cursor-abc" {
		t.Fatalf("NextToken = %q, want cursor-abc preserved", got)
	}
	items := result["Items"].([]any)
	if len(items) != 2 {
		t.Fatalf("Items len = %d, want 2", len(items))
	}
}

// NextToken 模式（无 TotalCount）不得凭空造出 TotalCount 字段。
func TestFilterListResponse_NoTotalCountStaysAbsent(t *testing.T) {
	resp := []byte(`{"Result":{"Items":[{"Id":"a"},{"Id":"b"}],"NextToken":"c"}}`)
	out, kept, changed := FilterListResponse(resp, func(r ListedResource) bool { return r.ResourceId == "a" })
	if !changed || kept != 1 {
		t.Fatalf("got (changed=%v, kept=%d), want (true,1)", changed, kept)
	}
	var parsed map[string]any
	if err := common.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	result := parsed["Result"].(map[string]any)
	if _, has := result["TotalCount"]; has {
		t.Fatal("TotalCount must not be invented in NextToken pagination mode")
	}
}

// 解析失败（不是 JSON、没有 Result、没有 Items）时原样返回，changed=false，
// 由客户按官方语义处理；调用方据此判断是否发生了过滤。
func TestFilterListResponse_UnparseableReturnsOriginal(t *testing.T) {
	for _, body := range []string{`not json`, ``, `{"Result":"str"}`, `{"ResponseMetadata":{}}`} {
		out, kept, changed := FilterListResponse([]byte(body), func(ListedResource) bool { return true })
		if changed {
			t.Errorf("body %q: expected changed=false", body)
		}
		if kept != 0 {
			t.Errorf("body %q: kept = %d, want 0", body, kept)
		}
		if string(out) != body {
			t.Errorf("body %q: got %q, want original bytes", body, string(out))
		}
	}
}

// 全过滤掉时 Items 是空数组而不是 null —— 客户按 [] 遍历，收到 null 会崩。
func TestFilterListResponse_AllFilteredYieldsEmptyArray(t *testing.T) {
	resp := []byte(`{"Result":{"Items":[{"Id":"other"}],"TotalCount":1}}`)
	out, kept, changed := FilterListResponse(resp, func(ListedResource) bool { return false })
	if !changed || kept != 0 {
		t.Fatalf("got (changed=%v,kept=%d), want (true,0)", changed, kept)
	}
	var parsed map[string]any
	if err := common.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	items := parsed["Result"].(map[string]any)["Items"]
	arr, ok := items.([]any)
	if !ok {
		t.Fatalf("Items type = %T, want []any (not null)", items)
	}
	if len(arr) != 0 {
		t.Fatalf("Items len = %d, want 0", len(arr))
	}
}

// 非对象元素（官方理论上不会返回，但 JSON 数组里什么都能塞）跳过而不 panic。
func TestFilterListResponse_SkipsNonObjectItems(t *testing.T) {
	resp := []byte(`{"Result":{"Items":[{"Id":"keep"},"str",42,null]}}`)
	out, kept, changed := FilterListResponse(resp, func(ListedResource) bool { return true })
	if !changed || kept != 1 {
		t.Fatalf("got (changed=%v,kept=%d), want (true,1)", changed, kept)
	}
	var parsed map[string]any
	if err := common.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if n := len(parsed["Result"].(map[string]any)["Items"].([]any)); n != 1 {
		t.Fatalf("Items len = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// ExtractAssetIds —— 隔离的闭环
// ---------------------------------------------------------------------------

// 官方 content 的形态随模型演进（image_url / video_url / audio_url / 数组套数组），
// 写死路径迟早漏一处，而漏一处就是越权。故不按结构下钻，递归扫全部字符串。
func TestExtractAssetIds_MixedContent(t *testing.T) {
	body := []byte(`{
		"model":"doubao-seedance-1-0-pro-250528",
		"content":[
			{"type":"text","text":"the whole string is a prompt, not a uri"},
			{"type":"image_url","image_url":{"url":"asset://asset-b"}},
			{"type":"video_url","video_url":{"url":"https://example.com/x.mp4"}},
			{"type":"audio_url","audio_url":{"url":"asset://asset-c"}}
		],
		"extra":{"nested":{"deep":["asset://asset-d"]}}
	}`)
	got := ExtractAssetIds(body)
	want := map[string]bool{"asset-b": true, "asset-c": true, "asset-d": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %d unique ids", got, len(want))
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected id %q in %v", id, got)
		}
	}
}

// 只有整串以 asset:// 开头才算引用 —— 提示词里嵌着 asset:// 字样属于文案，
// 不是资源引用，不该被当成越权去拦（否则客户写句「用 asset://xxx 那张图」
// 就被误判）。
//
// 反过来，以 asset:// 开头的串一律取其后的全部内容为 id（trim 首尾空白），
// 不在这里做 id 形状校验：官方 id 大小写敏感、内部结构可能变，站点规范化
// 会导致查不到自己的资产。形状不对的 id 会在归属校验里查不到而 404，
// 这正是想要的结果 —— 宁可多扫出个查不到的 id，也不可漏扫放行越权。
func TestExtractAssetIds_WholeStringMustBeUri(t *testing.T) {
	// 不以前缀开头：不是引用
	for _, s := range []string{
		"请看 asset://asset-a 这张图",
		"ref:asset://asset-a",
		"see asset://asset-a and more",
	} {
		body, err := common.Marshal(map[string]any{"content": []any{s}})
		if err != nil {
			t.Fatal(err)
		}
		if got := ExtractAssetIds(body); len(got) != 0 {
			t.Errorf("string %q: got %v, want no ids", s, got)
		}
	}

	// 以前缀开头：id 原样取出（含尾部多余内容与斜杠），交给归属校验拒。
	body, err := common.Marshal(map[string]any{"content": []any{
		"asset://asset-a/extra", "asset://  spaced  ",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractAssetIds(body)
	want := map[string]bool{"asset-a/extra": true, "spaced": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %d ids", got, len(want))
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected id %q in %v (id must be preserved as-is, only trimmed)", id, got)
		}
	}
}

// 同一 id 出现多次要去重，否则归属校验会重复查库。
func TestExtractAssetIds_Dedup(t *testing.T) {
	body := []byte(`{"content":["asset://same","asset://same",{"u":"asset://same"}]}`)
	got := ExtractAssetIds(body)
	if len(got) != 1 || got[0] != "same" {
		t.Fatalf("got %v, want [same]", got)
	}
}

// scheme 大小写不敏感（RFC 3986）：客户大写传来不该被当成普通 url 放过去。
// id 本身保持原样 —— 官方 id 大小写敏感，规范化会导致查不到自己的资产。
func TestExtractAssetIds_SchemeCaseInsensitiveIdPreserved(t *testing.T) {
	body := []byte(`{"content":["ASSET://CaseSensitive-Id","Asset://AnotherID"]}`)
	got := ExtractAssetIds(body)
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 ids", got)
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	if !seen["CaseSensitive-Id"] || !seen["AnotherID"] {
		t.Fatalf("id case must be preserved, got %v", got)
	}
}

// 非 asset 协议不得误伤：http/file/data 里出现 "asset://" 子串也不认。
func TestExtractAssetIds_NonAssetSchemesNotMisfired(t *testing.T) {
	body := []byte(`{"content":[
		"https://example.com/asset://fake",
		"file:///tmp/asset://fake2",
		"oss://bucket/x",
		"myasset://nope"
	]}`)
	if got := ExtractAssetIds(body); len(got) != 0 {
		t.Fatalf("got %v, want no ids", got)
	}
}

// 空 id、只有空格、非 JSON 时返回空，调用方不必特判。
func TestExtractAssetIds_EmptyAndInvalid(t *testing.T) {
	for _, body := range []string{`{"content":["asset://"]}`, `{"content":["asset://   "]}`, `not json`, ``} {
		if got := ExtractAssetIds([]byte(body)); len(got) != 0 {
			t.Errorf("body %q: got %v, want empty", body, got)
		}
	}
}

// ---------------------------------------------------------------------------
// InjectProjectName —— 整条透传链路上唯一的请求改写
// ---------------------------------------------------------------------------

// 客户显式传了就用客户的：站点不替客户决定项目，只补客户不可能知道的默认值。
func TestInjectProjectName_OnlyWhenAbsent(t *testing.T) {
	out := InjectProjectName("CreateAsset", []byte(`{"Name":"n"}`), "proj-1")
	var parsed map[string]any
	if err := common.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["ProjectName"] != "proj-1" {
		t.Fatalf("ProjectName = %v, want proj-1", parsed["ProjectName"])
	}
	if parsed["Name"] != "n" {
		t.Fatalf("existing fields must survive injection, got %+v", parsed)
	}

	// 客户已传：原样返回（连字节都不改，避免无谓的重排）。
	body := []byte(`{"ProjectName":"customer-proj","Name":"n"}`)
	if out := InjectProjectName("CreateAsset", body, "proj-1"); string(out) != string(body) {
		t.Fatalf("got %q, want original %q", string(out), string(body))
	}

	// 客户传了空串视同未传
	out = InjectProjectName("CreateAsset", []byte(`{"ProjectName":""}`), "proj-1")
	if err := common.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["ProjectName"] != "proj-1" {
		t.Fatalf("empty ProjectName must be treated as absent, got %v", parsed["ProjectName"])
	}
}

// 空 projectName、非 JSON、非对象一律原样返回：宁可让官方报错，
// 也不要把无效改写发上去。
func TestInjectProjectName_NoRewriteCases(t *testing.T) {
	body := []byte(`{"Name":"n"}`)

	if out := InjectProjectName("CreateAsset", body, ""); string(out) != string(body) {
		t.Fatalf("empty projectName must not rewrite, got %q", string(out))
	}
	if out := InjectProjectName("CreateAsset", []byte(`not json`), "proj-1"); string(out) != "not json" {
		t.Fatalf("non-json must not rewrite, got %q", string(out))
	}
	if out := InjectProjectName("CreateAsset", []byte(`[1,2]`), "proj-1"); string(out) != "[1,2]" {
		t.Fatalf("non-object json must not rewrite, got %q", string(out))
	}
	if out := InjectProjectName("CreateAsset", nil, "proj-1"); len(out) != 0 {
		t.Fatalf("nil body must stay nil, got %q", string(out))
	}
}

// 12 个 Action 全部注入；白名单外的 Action（尤其是数据面 & 未知 Action）绝不改写。
func TestInjectProjectName_ActionWhitelist(t *testing.T) {
	expected := []string{
		"CreateAssetGroup", "GetAssetGroup", "ListAssetGroups", "UpdateAssetGroup", "DeleteAssetGroup",
		"CreateAsset", "GetAsset", "ListAssets", "UpdateAsset", "DeleteAsset",
		"CreateVisualValidateSession", "GetVisualValidateResult",
	}
	if len(expected) != len(projectNameActions) {
		t.Fatalf("projectNameActions has %d entries, test expects %d — keep them in sync",
			len(projectNameActions), len(expected))
	}
	for _, action := range expected {
		out := InjectProjectName(action, []byte(`{"Name":"n"}`), "proj-1")
		var parsed map[string]any
		if err := common.Unmarshal(out, &parsed); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if parsed["ProjectName"] != "proj-1" {
			t.Errorf("%s: ProjectName not injected (got %v)", action, parsed["ProjectName"])
		}
	}

	for _, action := range []string{"ChatCompletions", "CreateVisualValidateSessionX", "DeleteAssetGroupX", "GetAssetX", ""} {
		if out := InjectProjectName(action, []byte(`{"Name":"n"}`), "proj-1"); string(out) != `{"Name":"n"}` {
			t.Errorf("%s: must not be rewritten, got %q", action, string(out))
		}
	}
}

// ---------------------------------------------------------------------------
// EnvelopeError / ParseEnvelopeError —— 官方把业务错误放在 HTTP 200 里
// ---------------------------------------------------------------------------

func TestParseEnvelopeError(t *testing.T) {
	// 有错误：取出 Code 与 Message
	resp := []byte(`{"ResponseMetadata":{"Error":{"Code":"InvalidParameter","Message":"bad group"}},"Result":{}}`)
	err := ParseEnvelopeError(resp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	env, ok := err.(*EnvelopeError)
	if !ok {
		t.Fatalf("error type = %T, want *EnvelopeError", err)
	}
	if env.Code != "InvalidParameter" || env.Message != "bad group" {
		t.Fatalf("got %+v", env)
	}
	if env.Error() != "InvalidParameter: bad group" {
		t.Fatalf("Error() = %q", env.Error())
	}

	// 成功响应（无 Error）返回 nil
	if err := ParseEnvelopeError([]byte(`{"ResponseMetadata":{"RequestId":"r"},"Result":{"Id":"x"}}`)); err != nil {
		t.Fatalf("success response must yield nil, got %v", err)
	}

	// 非信封响应（如 GetVisualValidateResult 的平铺形状）同样返回 nil ——
	// 「官方没按信封回答」该由调用方按官方语义处理，不在这里凭空造错。
	if err := ParseEnvelopeError([]byte(`{"GroupId":"g"}`)); err != nil {
		t.Fatalf("flat response must yield nil, got %v", err)
	}
	if err := ParseEnvelopeError([]byte(`not json`)); err != nil {
		t.Fatalf("non-json must yield nil, got %v", err)
	}

	// Error 是空对象时不算错误
	if err := ParseEnvelopeError([]byte(`{"ResponseMetadata":{"Error":{}}}`)); err != nil {
		t.Fatalf("empty Error must yield nil, got %v", err)
	}
}

// 只有 Code 或只有 Message 时都要能构造出可读的错误信息。
func TestEnvelopeError_ErrorString(t *testing.T) {
	if got := (&EnvelopeError{Code: "C"}).Error(); got != "C" {
		t.Errorf("code only: got %q, want C", got)
	}
	if got := (&EnvelopeError{Message: "M"}).Error(); got != "M" {
		t.Errorf("message only: got %q, want M", got)
	}
}

// ---------------------------------------------------------------------------
// ParseTotalCount / ParseNextToken —— 两种互斥分页模式
// ---------------------------------------------------------------------------

// TotalCount 只在「PageSize + PageNumber」模式下返回，对账要的就是这个权威
// 总数（含孤儿），故对账请求必须走页码分页。
func TestParseTotalCount(t *testing.T) {
	// JSON 数字反序列化恒为 float64
	if n, ok := ParseTotalCount([]byte(`{"Result":{"TotalCount":42}}`)); !ok || n != 42 {
		t.Fatalf("got (%d,%v), want (42,true)", n, ok)
	}
	if n, ok := ParseTotalCount([]byte(`{"Result":{"TotalCount":0}}`)); !ok || n != 0 {
		t.Fatalf("got (%d,%v), want (0,true) — 0 is a valid count, not absence", n, ok)
	}

	// NextToken 模式不返回该字段 → ok=false，调用方不得当成 0
	for _, body := range []string{
		`{"Result":{"NextToken":"c"}}`,
		`{"Result":{}}`,
		`{"Result":"str"}`,
		`not json`,
		``,
		`{"Result":{"TotalCount":"42"}}`, // 字符串类型不认
	} {
		if n, ok := ParseTotalCount([]byte(body)); ok {
			t.Errorf("body %q: got (%d,true), want ok=false", body, n)
		}
	}
}

func TestParseNextToken(t *testing.T) {
	if got := ParseNextToken([]byte(`{"Result":{"NextToken":"c1"}}`)); got != "c1" {
		t.Fatalf("got %q, want c1", got)
	}
	// 遍历结束 / 响应不带游标 → 空串
	for _, body := range []string{`{"Result":{}}`, `{"Result":{"NextToken":""}}`, `not json`, ``} {
		if got := ParseNextToken([]byte(body)); got != "" {
			t.Errorf("body %q: got %q, want empty", body, got)
		}
	}
}

// ---------------------------------------------------------------------------
// AssetGroupTypes —— 对账与遍历必须两类各查一次再相加
// ---------------------------------------------------------------------------

// 官方把 Filter.GroupType 定为必选，取值只有 AIGC 与 LivenessFace，两套独立
// 命名空间 —— 查一类拿不到另一类，少查一类就会把该类素材整批漏报成「不存在」。
func TestAssetGroupTypes(t *testing.T) {
	if len(AssetGroupTypes) != 2 {
		t.Fatalf("AssetGroupTypes = %v, want exactly 2 (AIGC + LivenessFace)", AssetGroupTypes)
	}
	seen := map[string]bool{}
	for _, g := range AssetGroupTypes {
		seen[g] = true
	}
	if !seen[AssetGroupTypeAIGC] || !seen[AssetGroupTypeLivenessFace] {
		t.Fatalf("AssetGroupTypes = %v, want AIGC and LivenessFace", AssetGroupTypes)
	}
}
