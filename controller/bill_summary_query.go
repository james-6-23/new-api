package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type billSummaryItemDTO struct {
	Date                string  `json:"date"`
	Username            string  `json:"username"`
	ChannelId           int     `json:"channel_id"`
	TokenName           string  `json:"token_name"`
	ModelName           string  `json:"model_name"`
	AmountUSD           float64 `json:"amount_usd"`
	ExchangeRate        float64 `json:"exchange_rate"`
	AmountCNY           float64 `json:"amount_cny"`
	PromptTokens        int     `json:"prompt_tokens"`
	CompletionTokens    int     `json:"completion_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
}

type billSummaryTotalsDTO struct {
	TotalAmountUSD           float64 `json:"total_amount_usd"`
	TotalAmountCNY           float64 `json:"total_amount_cny"`
	TotalPromptTokens        int     `json:"total_prompt_tokens"`
	TotalCompletionTokens    int     `json:"total_completion_tokens"`
	TotalCacheReadTokens     int     `json:"total_cache_read_tokens"`
	TotalCacheCreationTokens int     `json:"total_cache_creation_tokens"`
}

type billSummaryPageDTO struct {
	Items    []billSummaryItemDTO `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Summary  billSummaryTotalsDTO `json:"summary"`
}

// buildBillSummaryPage converts an aggregation into one page of items plus
// full totals across every group (independent of paging).
func buildBillSummaryPage(agg *billSummaryAgg, exchangeRate float64, page, pageSize int) billSummaryPageDTO {
	keys := agg.sortedKeys()
	var totals billSummaryTotalsDTO
	for _, k := range keys {
		r := agg.rows[k]
		usd := roundTo6(float64(r.Quota) / common.QuotaPerUnit)
		totals.TotalAmountUSD += usd
		totals.TotalAmountCNY += roundTo6(usd * exchangeRate)
		totals.TotalPromptTokens += r.PromptTokens
		totals.TotalCompletionTokens += r.CompletionTokens
		totals.TotalCacheReadTokens += r.CacheReadTokens
		totals.TotalCacheCreationTokens += r.CacheCreationTokens
	}
	totals.TotalAmountUSD = roundTo6(totals.TotalAmountUSD)
	totals.TotalAmountCNY = roundTo6(totals.TotalAmountCNY)

	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	items := make([]billSummaryItemDTO, 0, pageSize)
	if start < len(keys) {
		if end > len(keys) {
			end = len(keys)
		}
		for _, k := range keys[start:end] {
			r := agg.rows[k]
			usd := roundTo6(float64(r.Quota) / common.QuotaPerUnit)
			items = append(items, billSummaryItemDTO{
				Date:                k.Day,
				Username:            k.Username,
				ChannelId:           k.ChannelId,
				TokenName:           k.TokenName,
				ModelName:           k.ModelName,
				AmountUSD:           usd,
				ExchangeRate:        exchangeRate,
				AmountCNY:           roundTo6(usd * exchangeRate),
				PromptTokens:        r.PromptTokens,
				CompletionTokens:    r.CompletionTokens,
				CacheReadTokens:     r.CacheReadTokens,
				CacheCreationTokens: r.CacheCreationTokens,
			})
		}
	}
	return billSummaryPageDTO{Items: items, Total: len(keys), Page: page, PageSize: pageSize, Summary: totals}
}

func parseBillSummaryPaging(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.Query("p"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(c.Query("page_size"))
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func billSummaryRate(c *gin.Context) float64 {
	rate := operation_setting.USDExchangeRate
	if raw := strings.TrimSpace(c.Query("exchange_rate")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			rate = v
		}
	}
	return rate
}

// QueryBillSummaryAll — admin, may filter by username/channel.
func QueryBillSummaryAll(c *gin.Context) {
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	channel, _ := strconv.Atoi(c.Query("channel"))
	page, pageSize := parseBillSummaryPaging(c)
	rate := billSummaryRate(c)

	agg := newBillSummaryAgg()
	maxRows := model.LogExportMaxRows("xlsx")
	_, err := model.GetAllLogsForExport(model.LogTypeConsume, start, end,
		c.Query("model_name"), c.Query("username"), c.Query("token_name"),
		channel, c.Query("group"), "", maxRows, func(batch []*model.Log) error {
			agg.addBatch(batch)
			return nil
		})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildBillSummaryPage(agg, rate, page, pageSize))
}

// QueryBillSummarySelf — normal user, locked to self; ignores username/channel.
func QueryBillSummarySelf(c *gin.Context) {
	userId := c.GetInt("id")
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	page, pageSize := parseBillSummaryPaging(c)
	rate := billSummaryRate(c)

	agg := newBillSummaryAgg()
	maxRows := model.LogExportMaxRows("xlsx")
	_, err := model.GetUserLogsForExport(userId, model.LogTypeConsume, start, end,
		c.Query("model_name"), c.Query("token_name"), c.Query("group"), "", maxRows,
		func(batch []*model.Log) error {
			agg.addBatch(batch)
			return nil
		})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildBillSummaryPage(agg, rate, page, pageSize))
}
