package doubao

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// dreaminaUnitPrice 官方海外定价矩阵：USD / 百万 token。
// 维度：model → 分辨率档位("base"=480p/720p, "1080p", "4k") → 是否含视频输入。
// 数值以分数形式无意义，直接用官方 USD/M 数。来源：
// 接口文档/海外byteplus-seedance文档/模型计费定价.md
var dreaminaUnitPrice = map[string]map[string]map[bool]float64{
	"dreamina-seedance-2-0-260128": {
		"base":  {false: 7.0, true: 4.3},
		"1080p": {false: 7.7, true: 4.7},
		"4k":    {false: 4.0, true: 2.4},
	},
	"dreamina-seedance-2-0-fast-260128": {
		"base": {false: 5.6, true: 3.3},
	},
	"dreamina-seedance-2-0-mini-260615": {
		"base": {false: 3.5, true: 2.1},
	},
}

// dreaminaBaseUnit 各模型基准单价(= base 档不含视频)，作为相对倍率的分母，
// 也是管理员推荐 modelRatio(=baseUnit/2)对应的有效单价。
var dreaminaBaseUnit = map[string]float64{
	"dreamina-seedance-2-0-260128":      7.0,
	"dreamina-seedance-2-0-fast-260128": 5.6,
	"dreamina-seedance-2-0-mini-260615": 3.5,
}

// IsDreaminaSeedance2 判定是否为本次纳入海外计费优化的三个 dreamina 模型。
func IsDreaminaSeedance2(model string) bool {
	_, ok := dreaminaUnitPrice[model]
	return ok
}

// ClassifyResTier 把任意分辨率字符串归一到 {base, 1080p, 4k}。
func ClassifyResTier(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	switch {
	case t == "4k" || t == "2160p" || t == "3840x2160":
		return "4k"
	case t == "1080p" || t == "1920x1080":
		return "1080p"
	default:
		return "base"
	}
}

// DreaminaCellUnitUSD 返回某格有效单价(USD/M)与该模型基准单价。
// 不支持的(model,tier)回退到 base 档。
func DreaminaCellUnitUSD(model, tier string, hasVideo bool) (unit, baseUnit float64, ok bool) {
	tiers, ok := dreaminaUnitPrice[model]
	if !ok {
		return 0, 0, false
	}
	cell, has := tiers[tier]
	if !has {
		cell = tiers["base"]
	}
	return cell[hasVideo], dreaminaBaseUnit[model], true
}

// DreaminaPricingRatio 返回相对基准的合并倍率 ratio = cellUnit / baseUnit。
func DreaminaPricingRatio(model, tier string, hasVideo bool) (ratio, baseUnit float64, ok bool) {
	unit, base, ok := DreaminaCellUnitUSD(model, tier, hasVideo)
	if !ok || base <= 0 {
		return 0, 0, false
	}
	return unit / base, base, true
}

// resolutionFromMetadata 从 task metadata 读取 resolution 字段(纯 map，便于单测)。
func resolutionFromMetadata(metadata map[string]interface{}) string {
	if metadata == nil {
		return ""
	}
	if v, ok := metadata["resolution"].(string); ok {
		return v
	}
	return ""
}

// detectDreaminaResolution 优先取 metadata.resolution，回退扫描 raw body 顶层 resolution。
func detectDreaminaResolution(c *gin.Context, metadata map[string]interface{}) string {
	if s := resolutionFromMetadata(metadata); s != "" {
		return s
	}
	if c == nil {
		return ""
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return ""
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) == 0 {
		return ""
	}
	var top struct {
		Resolution string `json:"resolution"`
	}
	if err := common.Unmarshal(raw, &top); err == nil {
		return top.Resolution
	}
	return ""
}

// dreaminaVideoBilling 聚合：检测含视频输入 + 分辨率档位 → 计费倍率与展示快照。
func dreaminaVideoBilling(c *gin.Context, info *relaycommon.RelayInfo) (float64, types.VideoBillingDisplay, bool) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return 0, types.VideoBillingDisplay{}, false
	}
	hasVideo := hasVideoInMetadata(req.Metadata)
	tier := ClassifyResTier(detectDreaminaResolution(c, req.Metadata))
	ratio, baseUnit, ok := DreaminaPricingRatio(info.OriginModelName, tier, hasVideo)
	if !ok {
		return 0, types.VideoBillingDisplay{}, false
	}
	disp := types.VideoBillingDisplay{
		ResolutionTier:  tier,
		HasVideoInput:   hasVideo,
		BaseUnitUSDPerM: baseUnit,
		PricingRatio:    ratio,
	}
	return ratio, disp, true
}
