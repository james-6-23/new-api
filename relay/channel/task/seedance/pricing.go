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
	// 2.5 支持 480p/720p(base)与 1080p 两档,按含/不含视频分别定价。
	// 1080p 存的是原价;限时折扣走后台配置 billing_setting.video_promo,永不写入本矩阵。
	"dreamina-seedance-2-5-260628": {
		"base":  {false: 10.7, true: 6.4},
		"1080p": {false: 11.7, true: 7.0},
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
	// 2.5 支持 480p/720p(base)与 1080p 两档:
	// base 输入不含视频 70.00 元/M、包含视频 42.00 元/M;
	// 1080p 原价 输入不含视频 77.00 元/M、包含视频 46.00 元/M。
	// 1080p 存的是原价;限时折扣走后台配置 billing_setting.video_promo,永不写入本矩阵。
	"doubao-seedance-2-5-260628": {
		"base":  {false: 70.0, true: 42.0},
		"1080p": {false: 77.0, true: 46.0},
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

// CellUnit 返回某格(model,tier,hasVideo)的原单价、该模型基准单价(base 档不含视频),
// 以及 tierHit —— 实际用于计价的档位。模型不支持请求档位时回退 base,tierHit 如实为 "base",
// 使日志能记录真实计价档位而非请求档位。
func CellUnit(model, tier string, hasVideo bool) (unit, base float64, tierHit string, ok bool) {
	tiers, exists := unitPrice[model]
	if !exists {
		return 0, 0, "", false
	}
	cell, has := tiers[tier]
	if has {
		tierHit = tier
	} else {
		cell = tiers["base"]
		tierHit = "base"
	}
	return cell[hasVideo], tiers["base"][false], tierHit, true
}

// PricingRatio 返回相对基准的单一合并倍率 video_pricing = 单元格原单价 ÷ 基准原单价,
// 基准单价(供展示回退),以及实际计价档位。该倍率**不含任何折扣**。
func PricingRatio(model, tier string, hasVideo bool) (ratio, base float64, tierHit string, ok bool) {
	unit, base, tierHit, ok := CellUnit(model, tier, hasVideo)
	if !ok || base <= 0 {
		return 0, 0, "", false
	}
	return unit / base, base, tierHit, true
}

// tierOrder 是档位的稳定展示顺序,后台配置界面按此渲染。
var tierOrder = []string{"base", "1080p", "4k"}

// TiersForModel 返回该模型原价矩阵中真实存在的档位(稳定顺序),供后台按模型渲染折扣行。
// 这从源头消掉「给只有 base 档的 fast 配 1080p 折扣」这类无效配置。
func TiersForModel(model string) []string {
	tiers, ok := unitPrice[model]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(tiers))
	for _, t := range tierOrder {
		if _, has := tiers[t]; has {
			out = append(out, t)
		}
	}
	return out
}

// AllModelTiers 返回所有 Seedance 模型的可用档位,供后台一次性拉取。
func AllModelTiers() map[string][]string {
	out := make(map[string][]string, len(unitPrice))
	for m := range unitPrice {
		out[m] = TiersForModel(m)
	}
	return out
}
