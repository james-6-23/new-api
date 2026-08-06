package service

import (
	"context"

	"github.com/QuantumNous/new-api/model"
)

// UpstreamSupplierUsage 聚合系统下钻返回的单家上游供应商用量（M4 实装）。
type UpstreamSupplierUsage struct {
	SupplierName     string
	UsageUsd         float64 // 刊例消耗
	CostRatio        float64 // 该家供应商自己的 CNY:USD 倍率
	CostCny          float64
	Availability     float64 // 0-1
	CacheTokens      int64
	PromptTokens     int64
	CompletionTokens int64
	Raw              map[string]any
}

// UpstreamAdapter 按渠道拉取其背后各家供应商的真实用量/成本。
// M4：按渠道设置的 upstream_api 类型分发；一期无实现。
type UpstreamAdapter interface {
	FetchUsage(ctx context.Context, ch *model.Channel, start, end int64) ([]UpstreamSupplierUsage, error)
}
