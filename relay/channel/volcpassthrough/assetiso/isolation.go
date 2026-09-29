// Package assetiso 提供火山透传渠道资产隔离所需的纯解析函数。
//
// 单独成包是为了打断导入环:service 需要这些解析函数,而 volcpassthrough 依赖
// relay/channel -> service。本包只依赖 strings 与 common,任何层都能安全导入。
package assetiso

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// 资产隔离所需的请求/响应解析。
//
// 这里只做「读 JSON、取字段、改字段」的纯函数,不碰数据库、不碰 gin ——
// 归属判断要落到 model 层,而解析规则要能单测穷举官方响应形状。
//
// 官方控制面响应统一是两层信封:
//
//	{"ResponseMetadata":{...}, "Result":{...}}
//
// 单资源创建返回 Result.Id;Get 返回 Result 的完整字段;List 返回 Result.Items[]
// 与 Result.NextToken(游标分页)或 Result.TotalCount(页码分页)。
// 唯一的例外是 GetVisualValidateResult —— 官方示例里它直接返回 {"GroupId": "..."},
// 没有信封,故取 GroupId 时两层都要试。

// AssetURIScheme 素材引用前缀。视频提交时 content 里的 url 形如
// asset://asset-20260410114236-8cdfz。
const AssetURIScheme = "asset://"

// singleResourceActions 带 Id 的单资源 Action → 该 Id 的资源类型。
// 这些 Action 必须在转发前校验归属,不能发上游。
var singleResourceActions = map[string]string{
	"GetAsset":         resourceTypeAsset,
	"UpdateAsset":      resourceTypeAsset,
	"DeleteAsset":      resourceTypeAsset,
	"GetAssetGroup":    resourceTypeAssetGroup,
	"UpdateAssetGroup": resourceTypeAssetGroup,
	"DeleteAssetGroup": resourceTypeAssetGroup,
}

// 资源类型字面量。与 model.VolcResourceType* 取值一致,但不 import model ——
// relay 层不该依赖 model 层,故在此各留一份常量,由测试锁定二者相等。
const (
	resourceTypeAsset      = "asset"
	resourceTypeAssetGroup = "asset_group"
)

// listActions List 类 Action → Items 元素的资源类型。
// 这些 Action 转发后要按归属过滤 Result.Items。
var listActions = map[string]string{
	"ListAssets":      resourceTypeAsset,
	"ListAssetGroups": resourceTypeAssetGroup,
}

// SingleResourceTarget 描述一个待校验归属的单资源请求。
type SingleResourceTarget struct {
	ResourceType string
	ResourceId   string
}

// ParseSingleResourceTarget 从请求体取出单资源 Action 的目标 Id。
//
// 返回 ok=false 表示该 Action 不属于单资源类,调用方无需校验;
// Action 属于单资源类但 Id 缺失时返回 ok=true 且 ResourceId 为空 ——
// 交给归属校验去拒(空 id 永远不属于任何人),不在这里替官方做参数校验。
func ParseSingleResourceTarget(action string, body []byte) (SingleResourceTarget, bool) {
	resourceType, ok := singleResourceActions[action]
	if !ok {
		return SingleResourceTarget{}, false
	}
	return SingleResourceTarget{
		ResourceType: resourceType,
		ResourceId:   stringField(decodeJSONObject(body), "Id"),
	}, true
}

// ParseCreateAssetGroupRef 取 CreateAsset 请求体里的 GroupId。
//
// 必须校验:否则客户能往别人的素材组里塞素材,组的归属就形同虚设。
func ParseCreateAssetGroupRef(action string, body []byte) (string, bool) {
	if action != "CreateAsset" {
		return "", false
	}
	return stringField(decodeJSONObject(body), "GroupId"), true
}

// ListResourceType 返回 List 类 Action 的元素资源类型。
func ListResourceType(action string) (string, bool) {
	t, ok := listActions[action]
	return t, ok
}

// CreatedResource 从创建类响应里解析出的新资源。
type CreatedResource struct {
	ResourceType string
	ResourceId   string
	GroupId      string
	Name         string
	AssetType    string
	Status       string
}

// ParseCreatedResource 从成功响应里取出应当落库的新资源。
//
// 三个来源:
//
//	CreateAsset             Result.Id → asset,快照用请求体里的 GroupId/Name/AssetType
//	                        (创建响应只回 Id,别的字段官方不返回)
//	CreateAssetGroup        Result.Id → asset_group
//	GetVisualValidateResult GroupId  → asset_group(真人认证换来的真实资产组,
//	                        不纳入隔离就等于给所有人共用)
//
// CreateVisualValidateSession 故意不在此列:它返回的是 30 分钟有效的临时
// BytedToken,不是资源,落库只会留下一行永远查不到的垃圾。
func ParseCreatedResource(action string, reqBody, respBody []byte) (CreatedResource, bool) {
	resp := decodeJSONObject(respBody)
	result := objectField(resp, "Result")

	switch action {
	case "CreateAsset":
		id := stringField(result, "Id")
		if id == "" {
			return CreatedResource{}, false
		}
		req := decodeJSONObject(reqBody)
		return CreatedResource{
			ResourceType: resourceTypeAsset,
			ResourceId:   id,
			GroupId:      stringField(req, "GroupId"),
			Name:         stringField(req, "Name"),
			AssetType:    stringField(req, "AssetType"),
		}, true
	case "CreateAssetGroup":
		id := stringField(result, "Id")
		if id == "" {
			return CreatedResource{}, false
		}
		req := decodeJSONObject(reqBody)
		return CreatedResource{
			ResourceType: resourceTypeAssetGroup,
			ResourceId:   id,
			Name:         stringField(req, "Name"),
		}, true
	case "GetVisualValidateResult":
		// 官方示例里这个 Action 不带 ResponseMetadata 信封,直接返回 {"GroupId": ...}。
		// 两层都试,免得官方哪天补上信封就静默漏记。
		id := stringField(result, "GroupId")
		if id == "" {
			id = stringField(resp, "GroupId")
		}
		if id == "" {
			return CreatedResource{}, false
		}
		return CreatedResource{
			ResourceType: resourceTypeAssetGroup,
			ResourceId:   id,
			Name:         "real-person verification",
		}, true
	}
	return CreatedResource{}, false
}

// ParseResourceSnapshot 从 Get 类响应里取出可刷新的快照字段。
//
// 校验分支本来就要解析响应,顺手刷新本地快照,零额外上游调用 ——
// 客户轮询越勤,管理页面看到的状态越新。
func ParseResourceSnapshot(respBody []byte) CreatedResource {
	result := objectField(decodeJSONObject(respBody), "Result")
	return CreatedResource{
		ResourceId: stringField(result, "Id"),
		GroupId:    stringField(result, "GroupId"),
		Name:       stringField(result, "Name"),
		AssetType:  stringField(result, "AssetType"),
		Status:     stringField(result, "Status"),
	}
}

// ListedResource List 响应里的一个条目。
type ListedResource struct {
	ResourceId string
	GroupId    string
	Name       string
	AssetType  string
	Status     string
}

// ParseListedResources 取出 Result.Items 的全部条目,顺序与响应一致。
func ParseListedResources(respBody []byte) []ListedResource {
	result := objectField(decodeJSONObject(respBody), "Result")
	items, _ := result["Items"].([]any)
	out := make([]ListedResource, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, ListedResource{
			ResourceId: stringField(item, "Id"),
			GroupId:    stringField(item, "GroupId"),
			Name:       stringField(item, "Name"),
			AssetType:  stringField(item, "AssetType"),
			Status:     stringField(item, "Status"),
		})
	}
	return out
}

// FilterListResponse 按 keep 判定过滤 Result.Items,并重算 TotalCount。
//
// 官方按上游全量分页,站点过滤后每页条数必然少于 PageSize —— 这是已知取舍,
// 文档写明「分页以 NextToken 为准,不要依赖单页条数」。这里只保证条目本身不越权。
//
// TotalCount 仅在页码分页模式下由官方返回;存在时必须一起改小,否则客户看到
// 「共 100 条」却只拿到自己的 3 条,会以为站点丢数据。NextToken 原样保留:
// 它是官方游标,站点无法也不该重算。
//
// 解析失败(响应不是 JSON、没有 Result)时原样返回,由客户按官方语义处理 ——
// 但此时 kept 为 0,调用方可据此判断是否发生了过滤。
func FilterListResponse(respBody []byte, keep func(ListedResource) bool) ([]byte, int, bool) {
	resp := decodeJSONObject(respBody)
	result := objectField(resp, "Result")
	if result == nil {
		return respBody, 0, false
	}
	items, ok := result["Items"].([]any)
	if !ok {
		return respBody, 0, false
	}

	filtered := make([]any, 0, len(items))
	for _, raw := range items {
		item, isObj := raw.(map[string]any)
		if !isObj {
			continue
		}
		if keep(ListedResource{
			ResourceId: stringField(item, "Id"),
			GroupId:    stringField(item, "GroupId"),
			Name:       stringField(item, "Name"),
			AssetType:  stringField(item, "AssetType"),
			Status:     stringField(item, "Status"),
		}) {
			filtered = append(filtered, raw)
		}
	}
	result["Items"] = filtered
	if _, has := result["TotalCount"]; has {
		result["TotalCount"] = len(filtered)
	}
	resp["Result"] = result

	out, err := common.Marshal(resp)
	if err != nil {
		return respBody, 0, false
	}
	return out, len(filtered), true
}

// ExtractAssetIds 扫出请求体里全部 asset:// 引用的 id,已去重。
//
// 这是隔离的闭环:视频提交时若不校验 content[] 里的素材引用,客户猜到别人的
// asset id 就能直接拿去生成视频,前面三类校验全部失效。
//
// 实现上不按官方 content 结构逐层下钻,而是递归遍历整个 JSON 找 asset:// 字符串 ——
// 官方 content 的形态随模型演进(image_url / video_url / audio_url / 数组套数组),
// 写死路径迟早漏一处,而漏一处就是越权。宁可多扫,不可漏扫。
func ExtractAssetIds(body []byte) []string {
	var root any
	if err := common.Unmarshal(body, &root); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	ids := make([]string, 0, 4)
	walkJSONStrings(root, func(s string) {
		id, ok := parseAssetURI(s)
		if !ok || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	})
	return ids
}

// parseAssetURI 从 asset://<id> 取出 id。
//
// scheme 大小写不敏感(官方文档写 asset://,但 URI scheme 按 RFC 3986 本就不区分
// 大小写,客户大写传来不该被当成普通 url 放过去);id 本身保持原样,官方 id
// 大小写敏感,规范化会导致查不到自己的资产。
func parseAssetURI(raw string) (string, bool) {
	if len(raw) < len(AssetURIScheme) {
		return "", false
	}
	if !strings.EqualFold(raw[:len(AssetURIScheme)], AssetURIScheme) {
		return "", false
	}
	id := strings.TrimSpace(raw[len(AssetURIScheme):])
	if id == "" {
		return "", false
	}
	return id, true
}

// walkJSONStrings 递归访问任意 JSON 值里的全部字符串。
func walkJSONStrings(node any, visit func(string)) {
	switch v := node.(type) {
	case string:
		visit(v)
	case []any:
		for _, item := range v {
			walkJSONStrings(item, visit)
		}
	case map[string]any:
		for _, item := range v {
			walkJSONStrings(item, visit)
		}
	}
}

// projectNameActions 需要注入 ProjectName 的 Action。
//
// 官方要求 CreateAsset 的 ProjectName 必须与目标资产组所属项目一致,而项目是
// 站点 IAM 账号下的概念,客户无从知晓 —— 不注入,客户只能反复撞 InvalidParameter。
// 其余带 ProjectName 的 Action 同理:客户没填时用渠道配置的项目名,否则查不到
// 自己刚创建的资源。
var projectNameActions = map[string]bool{
	"CreateAssetGroup": true,
	"GetAssetGroup":    true,
	"ListAssetGroups":  true,
	"UpdateAssetGroup": true,
	"DeleteAssetGroup": true,

	"CreateAsset": true,
	"GetAsset":    true,
	"ListAssets":  true,
	"UpdateAsset": true,
	"DeleteAsset": true,

	"CreateVisualValidateSession": true,
	"GetVisualValidateResult":     true,
}

// InjectProjectName 客户未传 ProjectName 时注入渠道配置的项目名。
//
// 这是整条透传链路上**唯一**的请求改写。客户显式传了就用客户的 ——
// 站点不替客户决定项目,只补客户不可能知道的默认值。
//
// 空 projectName、非该 Action、body 不是 JSON 对象时原样返回:
// 宁可让官方报错,也不要把无效改写发上去。
func InjectProjectName(action string, body []byte, projectName string) []byte {
	if projectName == "" || !projectNameActions[action] {
		return body
	}
	obj := decodeJSONObject(body)
	if obj == nil {
		return body
	}
	if existing := stringField(obj, "ProjectName"); existing != "" {
		return body
	}
	obj["ProjectName"] = projectName
	out, err := common.Marshal(obj)
	if err != nil {
		return body
	}
	return out
}

// decodeJSONObject 把 body 解析成对象;不是对象或解析失败时返回 nil,
// 由调用方按「字段缺失」处理 —— 这些函数都不负责替官方校验参数。
func decodeJSONObject(body []byte) map[string]any {
	if len(body) == 0 {
		return nil
	}
	var obj map[string]any
	if err := common.Unmarshal(body, &obj); err != nil {
		return nil
	}
	return obj
}

// objectField 取嵌套对象;不存在或类型不符返回 nil。
func objectField(obj map[string]any, key string) map[string]any {
	if obj == nil {
		return nil
	}
	nested, _ := obj[key].(map[string]any)
	return nested
}

// stringField 取字符串字段;不存在或类型不符返回空串。
func stringField(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	s, _ := obj[key].(string)
	return s
}

// AssetGroupTypes 是 ListAssets / ListAssetGroups 必须逐类查询的全集。
//
// 官方把 Filter.GroupType 定为必选,取值只有这两个:虚拟人像 AIGC 与真人素材
// LivenessFace。它们是两套独立命名空间,查一类拿不到另一类 —— 对账与遍历
// 都必须两类各查一次再相加,少查一类就会把该类素材整批漏报成「不存在」。
var AssetGroupTypes = []string{AssetGroupTypeAIGC, AssetGroupTypeLivenessFace}

// 官方 Filter.GroupType 取值。
const (
	// AssetGroupTypeAIGC 虚拟人像素材组。
	AssetGroupTypeAIGC = "AIGC"
	// AssetGroupTypeLivenessFace 真人素材组。
	AssetGroupTypeLivenessFace = "LivenessFace"
)

// EnvelopeError 是官方响应信封里的业务错误。
//
// 官方把业务错误放在 HTTP 200 的 ResponseMetadata.Error 里,只看状态码会把
// 失败当成功 —— 对账时会把「查询失败」读成「没有素材」,进而把整批正常素材
// 误判成孤儿。
type EnvelopeError struct {
	Code    string
	Message string
}

func (e *EnvelopeError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// ParseEnvelopeError 取出信封里的业务错误;无错误时返回 nil。
//
// 响应不是信封结构（如 GetVisualValidateResult）时同样返回 nil —— 那属于
// 「官方没按信封回答」,该由调用方按官方语义处理,不在这里凭空造错。
func ParseEnvelopeError(respBody []byte) error {
	meta := objectField(decodeJSONObject(respBody), "ResponseMetadata")
	if meta == nil {
		return nil
	}
	errObj := objectField(meta, "Error")
	if errObj == nil {
		return nil
	}
	code := stringField(errObj, "Code")
	msg := stringField(errObj, "Message")
	if code == "" && msg == "" {
		return nil
	}
	return &EnvelopeError{Code: code, Message: msg}
}

// ParseTotalCount 取页码分页模式下的 Result.TotalCount。
//
// 官方只在「PageSize + PageNumber」模式下返回该字段,NextToken 模式不返回,
// 两种模式又互斥不可同时传。素材额度对账要的就是这个权威总数(含孤儿),
// 故对账请求必须走页码分页。
func ParseTotalCount(respBody []byte) (int, bool) {
	result := objectField(decodeJSONObject(respBody), "Result")
	if result == nil {
		return 0, false
	}
	switch v := result["TotalCount"].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	default:
		return 0, false
	}
}

// ParseNextToken 取 Result.NextToken。空串表示遍历结束或该响应不带游标。
func ParseNextToken(respBody []byte) string {
	return stringField(objectField(decodeJSONObject(respBody), "Result"), "NextToken")
}
