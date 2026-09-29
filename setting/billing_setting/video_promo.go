package billing_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"github.com/samber/lo"
)

// VideoPromo 单个模型的限时折扣活动。DB key: billing_setting.video_promo(模型名 → 本结构)。
//
// 设计要点:
//   - 折扣**不写入原价矩阵**。矩阵只存原价,折扣是独立乘子,详见
//     docs/superpowers/specs/2026-09-11-seedance-promo-pricing-layer-design.md 第三节。
//   - 窗口是**模型级**而非档位级:同一模型多个档位若需不同起止时间,视为两场活动,当前不支持。
type VideoPromo struct {
	// Factors 计价档位 → 折扣系数,如 {"1080p": 0.72}。
	// 键必须是该模型在原价矩阵中真实存在的档位;未列出的档位不打折。
	Factors map[string]float64 `json:"factors"`

	// StartAt / EndAt 活动窗口,Unix 秒(精确到秒),判定为闭区间 StartAt <= now <= EndAt。
	// 0 表示「未配置」而非「永久有效」:Factors 非空却漏填任一端是事故而非永久活动,
	// 此时整个模型的折扣不生效 —— 宁可少收折扣,不可无限期打折。
	StartAt int64 `json:"start_at"`
	EndAt   int64 `json:"end_at"`
}

// nowFn 是可注入的时钟,仅测试会替换。仓库无既有时钟注入约定,
// 刻意用包内私有变量而非把 common.GetTimestamp 改成 var(后者约 50 个调用点,爆炸半径过大)。
var nowFn = common.GetTimestamp

// GetVideoPromoCopy 返回折扣配置副本,与 GetBillingModeCopy 一致,热路径不共享 map。
func GetVideoPromoCopy() map[string]VideoPromo {
	return lo.Assign(billingSetting.VideoPromo)
}

// ResolveVideoPromo 返回指定模型在指定**实际计价档位**上当前生效的折扣系数与活动窗口。
// 无活动 / 未开始 / 已结束 / 配置非法时返回 (1, 0, 0),调用方据此判定「不打折」。
//
// 坏值一律向原价降级,不放大扣费、不 panic。两种降级粒度:
// 坏系数只废该档位;坏窗口废整个模型(窗口是模型级的,说不清起止的活动整场都不该生效)。
func ResolveVideoPromo(model, tier string) (factor float64, startAt, endAt int64) {
	promo, ok := billingSetting.VideoPromo[model]
	if !ok || len(promo.Factors) == 0 {
		return 1.0, 0, 0
	}
	if promo.StartAt <= 0 || promo.EndAt <= 0 || promo.StartAt >= promo.EndAt {
		common.SysLog(fmt.Sprintf(
			"video_promo: model %s has factors but invalid window (start=%d end=%d), discount disabled for the whole model",
			model, promo.StartAt, promo.EndAt))
		return 1.0, 0, 0
	}
	if now := nowFn(); now < promo.StartAt || now > promo.EndAt {
		return 1.0, 0, 0
	}
	f, has := promo.Factors[tier]
	if !has {
		return 1.0, 0, 0
	}
	if f <= 0 || f > 1 {
		common.SysLog(fmt.Sprintf(
			"video_promo: model %s tier %s has invalid factor %v (want 0 < f <= 1), this tier falls back to list price",
			model, tier, f))
		return 1.0, 0, 0
	}
	return f, promo.StartAt, promo.EndAt
}

// ValidateVideoPromo 保存前校验(参照 controller/option.go 的 case 分支模式)。
// 只做结构校验;档位键是否属于该模型矩阵由 controller 层校验 —— 在此 import seedance
// 会造成 billing_setting → seedance → billing_setting 循环导入。
func ValidateVideoPromo(jsonStr string) error {
	if jsonStr == "" {
		return nil
	}
	var cfg map[string]VideoPromo
	if err := common.UnmarshalJsonStr(jsonStr, &cfg); err != nil {
		return fmt.Errorf("invalid video_promo JSON: %w", err)
	}
	for model, p := range cfg {
		if len(p.Factors) == 0 {
			continue
		}
		if p.StartAt <= 0 || p.EndAt <= 0 {
			return fmt.Errorf("model %s: start_at and end_at are both required when factors is set", model)
		}
		if p.StartAt >= p.EndAt {
			return fmt.Errorf("model %s: start_at must be earlier than end_at", model)
		}
		for tier, f := range p.Factors {
			if f <= 0 || f > 1 {
				return fmt.Errorf("model %s tier %s: factor %v must be within (0, 1]", model, tier, f)
			}
		}
	}
	return nil
}

// SetVideoPromoForTest 供**跨包测试**注入折扣配置与固定时钟,返回还原函数;
// 调用方 `defer SetVideoPromoForTest(cfg, now)()` 即可。now 传 0 表示不改时钟。
//
// 刻意返回闭包而非收 testing.TB 参数:后者会把 testing 包链进生产二进制。
// 本包内的测试请用私有 withPromo,不必经过这里。
func SetVideoPromoForTest(cfg map[string]VideoPromo, now int64) (restore func()) {
	oldCfg, oldNow := billingSetting.VideoPromo, nowFn
	billingSetting.VideoPromo = cfg
	if now != 0 {
		nowFn = func() int64 { return now }
	}
	return func() {
		billingSetting.VideoPromo = oldCfg
		nowFn = oldNow
	}
}
