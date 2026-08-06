package controller

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// billRefAgg 为参考账单版式（按日/按令牌/按模型汇总 + 封面合计）维护独立聚合，
// key 始终保留 token 维度，不受 external 模式明细合并影响。
type billRefKey struct {
	Day       string
	Username  string
	TokenName string
	ModelName string
}

type billRefRow struct {
	BillingRecords, RequestCount                                         int
	PromptTokens, CompletionTokens, CacheReadTokens, CacheCreationTokens int
	Quota                                                                int
	ListQuota                                                            float64
	FirstTs, LastTs                                                      int64
}

func (r *billRefRow) merge(o *billRefRow) {
	r.BillingRecords += o.BillingRecords
	r.RequestCount += o.RequestCount
	r.PromptTokens += o.PromptTokens
	r.CompletionTokens += o.CompletionTokens
	r.CacheReadTokens += o.CacheReadTokens
	r.CacheCreationTokens += o.CacheCreationTokens
	r.Quota += o.Quota
	r.ListQuota += o.ListQuota
	if r.FirstTs == 0 || (o.FirstTs != 0 && o.FirstTs < r.FirstTs) {
		r.FirstTs = o.FirstTs
	}
	if o.LastTs > r.LastTs {
		r.LastTs = o.LastTs
	}
}

type billRefAgg struct {
	rows map[billRefKey]*billRefRow
}

func newBillRefAgg() *billRefAgg {
	return &billRefAgg{rows: make(map[billRefKey]*billRefRow)}
}

func (a *billRefAgg) addBatch(logs []*model.Log) {
	for _, log := range logs {
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund {
			continue
		}
		key := billRefKey{
			Day:       time.Unix(log.CreatedAt, 0).Format("2006-01-02"),
			Username:  log.Username,
			TokenName: log.TokenName,
			ModelName: log.ModelName,
		}
		row := a.rows[key]
		if row == nil {
			row = &billRefRow{}
			a.rows[key] = row
		}
		info := parseLogPricingInfo(log)
		row.BillingRecords++
		if row.FirstTs == 0 || log.CreatedAt < row.FirstTs {
			row.FirstTs = log.CreatedAt
		}
		if log.CreatedAt > row.LastTs {
			row.LastTs = log.CreatedAt
		}
		listQ := logListQuota(log, info)
		if log.Type == model.LogTypeRefund {
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

func (a *billRefAgg) totals() billRefRow {
	var t billRefRow
	for _, r := range a.rows {
		t.merge(r)
	}
	return t
}

func (a *billRefAgg) byDay() ([]string, map[string]*billRefRow) {
	m := make(map[string]*billRefRow)
	for k, r := range a.rows {
		g := m[k.Day]
		if g == nil {
			g = &billRefRow{}
			m[k.Day] = g
		}
		g.merge(r)
	}
	days := make([]string, 0, len(m))
	for d := range m {
		days = append(days, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	return days, m
}

// billRefDimKey 按令牌/按模型 sheet 的行键；withUser=false（external 客户账单）
// 时 Username 恒空，同名维度跨用户合并。
type billRefDimKey struct {
	Username string
	Name     string
}

func (a *billRefAgg) byDim(withUser bool, name func(billRefKey) string) ([]billRefDimKey, map[billRefDimKey]*billRefRow) {
	m := make(map[billRefDimKey]*billRefRow)
	for k, r := range a.rows {
		dk := billRefDimKey{Name: name(k)}
		if withUser {
			dk.Username = k.Username
		}
		g := m[dk]
		if g == nil {
			g = &billRefRow{}
			m[dk] = g
		}
		g.merge(r)
	}
	keys := make([]billRefDimKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		x, y := keys[i], keys[j]
		if m[x].Quota != m[y].Quota {
			return m[x].Quota > m[y].Quota // 金额大者在前
		}
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		return x.Username < y.Username
	})
	return keys, m
}

func (a *billRefAgg) byToken(withUser bool) ([]billRefDimKey, map[billRefDimKey]*billRefRow) {
	return a.byDim(withUser, func(k billRefKey) string { return k.TokenName })
}

func (a *billRefAgg) byModel(withUser bool) ([]billRefDimKey, map[billRefDimKey]*billRefRow) {
	return a.byDim(withUser, func(k billRefKey) string { return k.ModelName })
}
