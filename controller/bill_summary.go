package controller

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
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
}

type billSummaryAgg struct {
	rows map[billSummaryKey]*billSummaryRow
}

func newBillSummaryAgg() *billSummaryAgg {
	return &billSummaryAgg{rows: make(map[billSummaryKey]*billSummaryRow)}
}

func (a *billSummaryAgg) addBatch(logs []*model.Log) {
	for _, log := range logs {
		// 账单只计消费与退款；其余类型(充值/管理/系统/错误/登录)跳过。
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund {
			continue
		}
		key := billSummaryKey{
			Day:       time.Unix(log.CreatedAt, 0).Format("2006-01-02"),
			Username:  log.Username,
			ChannelId: log.ChannelId,
			TokenName: log.TokenName,
			ModelName: log.ModelName,
		}
		row := a.rows[key]
		if row == nil {
			row = &billSummaryRow{}
			a.rows[key] = row
		}
		if log.Type == model.LogTypeRefund {
			// 退款冲抵金额；退款日志无 tokens，不动 token 列。
			row.Quota -= log.Quota
			continue
		}
		row.Quota += log.Quota
		row.PromptTokens += log.PromptTokens
		row.CompletionTokens += log.CompletionTokens
		row.CacheReadTokens += getCacheTokensFromOther(log, "cache_tokens")
		row.CacheCreationTokens += getCacheCreationTokensFromOther(log)
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
