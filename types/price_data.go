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
// 有效单价 = BaseUnitUSDPerM * PricingRatio(再随 modelRatio 加价整体缩放)。
type VideoBillingDisplay struct {
	ResolutionTier  string  // "base" / "1080p" / "4k"
	HasVideoInput   bool    // 是否含视频输入
	BaseUnitUSDPerM float64 // 基准单价(USD / 百万 token)
	PricingRatio    float64 // 相对基准的合并倍率(video_pricing)
	VideoTokens     int     // 结算阶段回填的实际 completion_tokens
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
