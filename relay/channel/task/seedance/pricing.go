// Package seedance 是 Seedance 2.0 视频计费的单一事实来源(single source of truth)。
//
// 背景:历史上存在两套并行实现 —— sora 适配器(OpenAI /v1/videos 风格中转)用
// 「video_input × resolution 两倍率相乘」的近似算法;doubao 适配器(火山/豆包通用协议)
// 用「官方二维矩阵单一精确倍率 video_pricing」并输出展示快照。本包把两者对齐为一套:
// 精确矩阵 + 单一 video_pricing 倍率 + VideoBillingDisplay 快照,供两个适配器共同调用。
//
// 计费只依赖「相对基准倍率」= 单元格单价 ÷ 基准单价(base 档、不含视频),因此矩阵里
// 单价的币种(USD / 元)不影响扣费,仅作为展示回退值;实际生效单价 =
// 管理员配置的 modelRatio × 2 × video_pricing。
package seedance

import "strings"

// unitPrice 官方单价矩阵:model → 分辨率档位(base/1080p/4k)→ 是否含视频输入 → 单价(每百万 token)。
//
// 命名对应两个上游/渠道:
//   - dreamina-*  海外 BytePlus,USD/M  —— 走 doubao 适配器(火山/豆包通用协议)
//   - doubao-*    国内火山方舟,元/M    —— 走 sora 适配器(OpenAI /v1/videos 风格中转)或 doubao 适配器
//
// 相对倍率两侧一致(如 260128 含视频折扣 dreamina 4.3/7.0、doubao 28/46),但绝对价随市场不同。
var unitPrice = map[string]map[string]map[bool]float64{
	// 海外 BytePlus dreamina 命名(USD/百万 token)
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
	// 国内火山方舟 doubao 命名(元/百万 token)。官方价:2.0 有 1080p 档,fast 无 1080p/4k,
	// 数值来源:接口文档/seedance_docs/01_计费说明.md。
	"doubao-seedance-2-0-260128": {
		"base":  {false: 46.0, true: 28.0},
		"1080p": {false: 51.0, true: 31.0},
	},
	"doubao-seedance-2-0-fast-260128": {
		"base": {false: 37.0, true: 22.0},
	},
	// mini 仅按含/不含视频区分定价,不支持输出 1080p(无 1080p/4k 档):
	// 输入不含视频 23.00 元/M,输入包含视频 14.00 元/M。
	"doubao-seedance-2-0-mini-260615": {
		"base": {false: 23.0, true: 14.0},
	},
}

// IsSeedance2 判定模型是否纳入 Seedance 2.0 矩阵计费(两种命名均覆盖)。
func IsSeedance2(model string) bool {
	_, ok := unitPrice[model]
	return ok
}

// ClassifyResTier 把任意分辨率字符串归一到 {base, 1080p, 4k}。
// 未识别(含空串、480p/720p)一律归 base。
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

// CellUnit 返回某格(model,tier,hasVideo)的单价与该模型基准单价(base 档不含视频)。
// 不支持的档位回退到 base 档(例如 fast/mini 传 1080p/4k)。
func CellUnit(model, tier string, hasVideo bool) (unit, base float64, ok bool) {
	tiers, ok := unitPrice[model]
	if !ok {
		return 0, 0, false
	}
	cell, has := tiers[tier]
	if !has {
		cell = tiers["base"]
	}
	return cell[hasVideo], tiers["base"][false], true
}

// PricingRatio 返回相对基准的单一合并倍率 video_pricing = 单元格单价 ÷ 基准单价,
// 以及基准单价(供展示回退)。
func PricingRatio(model, tier string, hasVideo bool) (ratio, base float64, ok bool) {
	unit, base, ok := CellUnit(model, tier, hasVideo)
	if !ok || base <= 0 {
		return 0, 0, false
	}
	return unit / base, base, true
}
