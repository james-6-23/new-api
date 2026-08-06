package seedance

import (
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// EstimateBilling 是两个适配器共用的 Seedance 2.0 计费入口:检测含视频输入 + 分辨率档位,
// 查矩阵得到单一 video_pricing 倍率,写入展示快照 info.PriceData.VideoBilling,
// 并返回 OtherRatios。ratio==1.0(base 档不含视频)时只写快照、返回 nil(不追加冗余倍率)。
// 非 Seedance 2.0 模型返回 nil。
func EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	ratio, disp, ok := ResolveVideoBilling(c, info.OriginModelName)
	if !ok {
		return nil
	}
	d := disp
	info.PriceData.VideoBilling = &d
	if ratio != 1.0 {
		return map[string]float64{"video_pricing": ratio}
	}
	return nil
}

// ResolveVideoBilling 检测请求并计算 (video_pricing 倍率, 展示快照)。
// 供 EstimateBilling 使用,也便于单测。
func ResolveVideoBilling(c *gin.Context, model string) (float64, types.VideoBillingDisplay, bool) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return 0, types.VideoBillingDisplay{}, false
	}
	hasVideo := HasVideoInput(c, &req)
	tier := ClassifyResTier(DetectResolution(c, &req))
	ratio, base, ok := PricingRatio(model, tier, hasVideo)
	if !ok {
		return 0, types.VideoBillingDisplay{}, false
	}
	return ratio, types.VideoBillingDisplay{
		ResolutionTier:  tier,
		HasVideoInput:   hasVideo,
		BaseUnitUSDPerM: base,
		PricingRatio:    ratio,
	}, true
}

// HasVideoInput 检测请求是否含视频输入(type=video_url)。优先扫描 metadata.content,
// 再回退扫描 raw body 顶层 content[](覆盖 OpenAI content[] 风格中转与火山原生两种形态)。
func HasVideoInput(c *gin.Context, req *relaycommon.TaskSubmitReq) bool {
	if req != nil && req.Metadata != nil && contentHasType(req.Metadata["content"], "video_url") {
		return true
	}
	return rawBodyContentHasType(c, "video_url")
}

// HasImageInput 检测请求是否含图片输入。优先复用 TaskSubmitReq.HasImage()(涵盖
// images / image_reference / image_references),再扫描 metadata.content 与 raw body。
// sora 适配器用它把「文生视频」动作纠正为「图生视频」,保证日志类型列正确。
func HasImageInput(c *gin.Context, req *relaycommon.TaskSubmitReq) bool {
	if req != nil && req.HasImage() {
		return true
	}
	if req != nil && req.Metadata != nil && contentHasType(req.Metadata["content"], "image_url") {
		return true
	}
	return rawBodyContentHasType(c, "image_url")
}

// DetectResolution 探测请求分辨率字符串。优先级:metadata.resolution > req.Size > raw body 顶层 resolution。
func DetectResolution(c *gin.Context, req *relaycommon.TaskSubmitReq) string {
	if req != nil && req.Metadata != nil {
		if s, ok := req.Metadata["resolution"].(string); ok && s != "" {
			return s
		}
	}
	if req != nil && req.Size != "" {
		return req.Size
	}
	if raw, ok := peekRawBody(c); ok {
		var top struct {
			Resolution string `json:"resolution"`
		}
		if err := common.Unmarshal(raw, &top); err == nil && top.Resolution != "" {
			return top.Resolution
		}
	}
	return ""
}

// contentHasType 判断 metadata.content(interface{} 形态的数组)是否含指定 type 的条目,
// 兼容 {"type":"video_url",...} 与 {"video_url":{...}} 两种写法。
func contentHasType(raw interface{}, typ string) bool {
	arr, ok := raw.([]interface{})
	if !ok {
		return false
	}
	key := keyForType(typ)
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == typ {
			return true
		}
		if _, has := m[key]; has {
			return true
		}
	}
	return false
}

// rawBodyContentHasType 扫描 raw body 顶层 content[] 是否含指定 type 的条目。
func rawBodyContentHasType(c *gin.Context, typ string) bool {
	raw, ok := peekRawBody(c)
	if !ok {
		return false
	}
	var top struct {
		Content []map[string]interface{} `json:"content"`
	}
	if err := common.Unmarshal(raw, &top); err != nil {
		return false
	}
	key := keyForType(typ)
	for _, item := range top.Content {
		if item["type"] == typ {
			return true
		}
		if _, has := item[key]; has {
			return true
		}
	}
	return false
}

// keyForType 把 type 值("video_url"/"image_url")映射到其在条目里的字段名(同名)。
func keyForType(typ string) string {
	return typ
}

// peekRawBody 读取已缓存的原始请求体(不消费 body),失败或空返回 false。
func peekRawBody(c *gin.Context) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, false
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}
