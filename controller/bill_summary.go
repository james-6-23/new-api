package controller

import (
	"encoding/base64"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

type billSummaryKey struct {
	Day       string
	Username  string
	ChannelId int
	TokenName string
	ModelName string
}

type billSummaryRow struct {
	Quota               int
	PromptTokens        int
	CompletionTokens    int
	CacheReadTokens     int
	CacheCreationTokens int
	// ListPriceUSD 刊例价（模型标价，不含分组折扣）：按次计费为每次价格，
	// 视频任务为每 1M tokens 单价，按 token 模型为 model_ratio×2（每 1M 输入）。
	// EffectiveRatio 专属倍率：user_group_ratio 有效时优先，否则 group_ratio。
	// 日志按 created_at DESC 到达，首见值即最新值；hasPrice/hasRatio 为 false
	// 时导出留空（旧日志无定价键，不写 0 以免误读为免费）。
	ListPriceUSD   float64
	EffectiveRatio float64
	hasPrice       bool
	hasRatio       bool
	// BillingRecords 计费记录（消费+退款行数）；RequestCount 请求数（消费且
	// billing_stage != "settle"，任务多行只按 pre_consume 计 1 次）；
	// ListQuota 刊例价金额（quota 单位）：逐条 quota/生效倍率 累加，退款冲抵，
	// 旧日志无倍率键时按实付兜底。
	BillingRecords int
	RequestCount   int
	ListQuota      float64
}

type billSummaryAgg struct {
	rows map[billSummaryKey]*billSummaryRow
	// external merges the channel/token dimensions away so a customer-facing
	// bill shows one row per (day, user, model) — no per-channel duplicates.
	external bool
	// granularity buckets the Day key: "day" (default), "week" (Monday-based
	// natural week, key = Monday's date) or "month" (key = "2006-01").
	granularity string
	// minDay/maxDay track the real calendar-day range of the aggregated logs,
	// independent of granularity bucketing (the grand sheet's range columns).
	minDay string
	maxDay string
}

func newBillSummaryAgg() *billSummaryAgg {
	return &billSummaryAgg{rows: make(map[billSummaryKey]*billSummaryRow)}
}

// normalizeBillGranularity clamps the query param to a known bucket size.
func normalizeBillGranularity(raw string) string {
	switch raw {
	case "week", "month":
		return raw
	default:
		return "day"
	}
}

// billBucketDay returns the aggregation bucket a timestamp falls into.
func billBucketDay(t time.Time, granularity string) string {
	switch granularity {
	case "week":
		offset := (int(t.Weekday()) + 6) % 7 // Monday-based
		return t.AddDate(0, 0, -offset).Format("2006-01-02")
	case "month":
		return t.Format("2006-01")
	default:
		return t.Format("2006-01-02")
	}
}

// billPeriodLabel renders a bucket key for display: weeks show their full
// span ("2026-06-01 ~ 2026-06-07"), day/month buckets are self-explanatory.
func billPeriodLabel(bucket, granularity string) string {
	if granularity == "week" {
		if t, err := time.ParseInLocation("2006-01-02", bucket, time.Local); err == nil {
			return bucket + " ~ " + t.AddDate(0, 0, 6).Format("2006-01-02")
		}
	}
	return bucket
}

func (a *billSummaryAgg) addBatch(logs []*model.Log) {
	for _, log := range logs {
		// 账单只计消费与退款；其余类型(充值/管理/系统/错误/登录)跳过。
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund {
			continue
		}
		logTime := time.Unix(log.CreatedAt, 0)
		day := logTime.Format("2006-01-02")
		if a.minDay == "" || day < a.minDay {
			a.minDay = day
		}
		if a.maxDay == "" || day > a.maxDay {
			a.maxDay = day
		}
		key := billSummaryKey{
			Day:       billBucketDay(logTime, a.granularity),
			Username:  log.Username,
			ChannelId: log.ChannelId,
			TokenName: log.TokenName,
			ModelName: log.ModelName,
		}
		if a.external {
			key.ChannelId = 0
			key.TokenName = ""
		}
		row := a.rows[key]
		if row == nil {
			row = &billSummaryRow{}
			a.rows[key] = row
		}
		info := parseLogPricingInfo(log)
		row.capturePricing(info)
		row.BillingRecords++
		listQ := logListQuota(log, info)
		if log.Type == model.LogTypeRefund {
			// 退款冲抵金额与刊例价金额；退款日志无 tokens，不动 token 列。
			row.Quota -= log.Quota
			row.ListQuota -= listQ
			continue
		}
		if !isSettleStageLog(info) {
			row.RequestCount++
		}
		row.Quota += log.Quota
		row.ListQuota += listQ
		row.PromptTokens += log.PromptTokens
		row.CompletionTokens += log.CompletionTokens
		row.CacheReadTokens += getCacheTokensFromOther(log, "cache_tokens")
		row.CacheCreationTokens += getCacheCreationTokensFromOther(log)
	}
}

// parseLogPricingInfo decodes Log.Other once per log; nil when absent/broken.
func parseLogPricingInfo(log *model.Log) *logPricingInfo {
	if strings.TrimSpace(log.Other) == "" {
		return nil
	}
	var info logPricingInfo
	if err := common.UnmarshalJsonStr(log.Other, &info); err != nil {
		return nil
	}
	return &info
}

// isSettleStageLog: 任务结算补扣行（消费类型、stage=settle）不计请求数。
func isSettleStageLog(info *logPricingInfo) bool {
	return info != nil && info.BillingStage == "settle"
}

// logListQuota 按刊例价反推该条日志的 quota（未折扣）。所有计费模式满足
// quota = 刊例成本 × 生效倍率，故除以倍率即精确刊例价金额；免费组 quota 为 0；
// 旧日志无倍率键时兜底返回实付 quota（视倍率 1）。
func logListQuota(log *model.Log, info *logPricingInfo) float64 {
	q := float64(log.Quota)
	if q == 0 {
		return 0
	}
	if info == nil {
		return q
	}
	eff := info.GroupRatio
	if isValidGroupRatio(info.UserGroupRatio) && info.UserGroupRatio > 0 {
		eff = info.UserGroupRatio
	}
	if eff > 0 {
		return q / eff
	}
	return q
}

// capturePricing 从已解析的日志定价信息提取刊例价与生效倍率。流式顺序为
// created_at DESC，因此首个携带定价键的日志（组内最新一条）胜出，后续不再覆盖。
// tiered_expr 日志的价格只能来自表达式匹配档（系数即真实 $/1M，不乘 2）；
// 解析失败时该条不出价，也绝不回退 model_ratio×2（避免取到切换前的旧倍率价）。
func (r *billSummaryRow) capturePricing(info *logPricingInfo) {
	if info == nil || (r.hasPrice && r.hasRatio) {
		return
	}
	if !r.hasPrice {
		if info.BillingMode == "tiered_expr" {
			if raw, err := base64.StdEncoding.DecodeString(info.ExprB64); err == nil {
				tiers := billingexpr.ParseTiers(string(raw))
				if tier := billingexpr.MatchTier(tiers, info.MatchedTier); tier != nil {
					if p, ok := tier.Prices["p"]; ok && p > 0 {
						r.ListPriceUSD = p
						r.hasPrice = true
					}
				}
			}
		} else {
			switch {
			case info.VideoUnitPrice > 0:
				r.ListPriceUSD = info.VideoUnitPrice
				r.hasPrice = true
			case info.ModelPrice > 0:
				r.ListPriceUSD = info.ModelPrice
				r.hasPrice = true
			case info.ModelRatio > 0:
				r.ListPriceUSD = info.ModelRatio * 2.0
				r.hasPrice = true
			}
		}
	}
	if !r.hasRatio {
		switch {
		case isValidGroupRatio(info.UserGroupRatio) && info.UserGroupRatio > 0:
			r.EffectiveRatio = info.UserGroupRatio
			r.hasRatio = true
		case info.GroupRatio > 0:
			r.EffectiveRatio = info.GroupRatio
			r.hasRatio = true
		}
	}
}

func (a *billSummaryAgg) sortedKeys() []billSummaryKey {
	keys := make([]billSummaryKey, 0, len(a.rows))
	for k := range a.rows {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		x, y := keys[i], keys[j]
		if x.Day != y.Day {
			return x.Day > y.Day // DESC
		}
		if x.Username != y.Username {
			return x.Username < y.Username
		}
		if x.ChannelId != y.ChannelId {
			return x.ChannelId < y.ChannelId
		}
		if x.ModelName != y.ModelName {
			return x.ModelName < y.ModelName
		}
		return x.TokenName < y.TokenName
	})
	return keys
}
