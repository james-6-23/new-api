package types

import (
	"fmt"
	"math"
)

type GroupRatioInfo struct {
	GroupRatio        float64
	GroupSpecialRatio float64
	HasSpecialRatio   bool
}

type PriceData struct {
	FreeModel            bool
	ModelPrice           float64
	ModelRatio           float64
	CompletionRatio      float64
	CacheRatio           float64
	CacheCreationRatio   float64
	CacheCreation5mRatio float64
	CacheCreation1hRatio float64
	ImageRatio           float64
	AudioRatio           float64
	AudioCompletionRatio float64
	OtherRatios          map[string]float64
	UsePrice             bool
	Quota                int // 按次计费的最终额度（MJ / Task）
	QuotaToPreConsume    int // 按量计费的预消耗额度
	GroupRatioInfo       GroupRatioInfo
	VideoBilling         *VideoBillingDisplay
}

// VideoBillingDisplay 视频计费的展示用快照(不参与上游 marshal,仅用于日志/前端核价)。
// 原价单价 = BaseUnitUSDPerM * PricingRatio(再随 modelRatio 加价整体缩放);
// 实收单价 = 原价单价 * PromoFactor。
type VideoBillingDisplay struct {
	ResolutionTier  string  // 实际计价档位:"base" / "1080p" / "4k"
	HasVideoInput   bool    // 是否含视频输入
	BaseUnitUSDPerM float64 // 基准单价(USD / 百万 token)
	PricingRatio    float64 // 相对基准的合并倍率(video_pricing),**纯原价,不含折扣**
	VideoTokens     int     // 结算阶段回填的实际 completion_tokens
	PromoFactor     float64 // 限时折扣系数;未打折为 1
	PromoStartAt    int64   // 活动开始 Unix 秒;未打折为 0
	PromoEndAt      int64   // 活动结束 Unix 秒;未打折为 0
}

func (p *PriceData) AddOtherRatio(key string, ratio float64) {
	if p.OtherRatios == nil {
		p.OtherRatios = make(map[string]float64)
	}
	// NaN/Inf would poison every downstream quota multiplication
	// (int(NaN * quota) wraps to a negative charge).
	if !(ratio > 0) || math.IsInf(ratio, 1) {
		return
	}
	p.OtherRatios[key] = ratio
}

func (p *PriceData) ToSetting() string {
	return fmt.Sprintf("ModelPrice: %f, ModelRatio: %f, CompletionRatio: %f, CacheRatio: %f, GroupRatio: %f, UsePrice: %t, CacheCreationRatio: %f, CacheCreation5mRatio: %f, CacheCreation1hRatio: %f, QuotaToPreConsume: %d, ImageRatio: %f, AudioRatio: %f, AudioCompletionRatio: %f", p.ModelPrice, p.ModelRatio, p.CompletionRatio, p.CacheRatio, p.GroupRatioInfo.GroupRatio, p.UsePrice, p.CacheCreationRatio, p.CacheCreation5mRatio, p.CacheCreation1hRatio, p.QuotaToPreConsume, p.ImageRatio, p.AudioRatio, p.AudioCompletionRatio)
}
