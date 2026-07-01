package controller

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/xuri/excelize/v2"
)

const billSummarySheetPrefix = "汇总账单"

var billSummaryHeaders = []string{
	"日期", "用户名", "渠道ID", "令牌名称", "模型名称",
	"汇总金额(美元)", "汇率", "汇总金额(人民币)",
	"输入tokens", "输出tokens", "缓存读取tokens", "缓存创建tokens",
}

var billSummaryColWidths = []float64{14, 16, 10, 16, 22, 16, 8, 18, 12, 12, 16, 16}

func roundTo6(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

// writeBillSummarySheet 创建首个汇总 sheet 并写入排序后的聚合行与合计行。
// 超 excelSingleSheetSoftCap 时滚动为 "汇总账单 (2)" ...
func writeBillSummarySheet(f *excelize.File, agg *billSummaryAgg, exchangeRate float64) error {
	keys := agg.sortedKeys()

	headerStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}

	var (
		sw        *excelize.StreamWriter
		sheetIdx  = 0
		rowInSt   = 0
		totalUSD  float64
		totalCNY  float64
		sumPrompt int
		sumComp   int
		sumCRead  int
		sumCCreat int
	)

	newSheet := func() error {
		if sw != nil {
			if err := sw.Flush(); err != nil {
				return err
			}
		}
		name := billSummarySheetPrefix
		if sheetIdx > 0 {
			name = fmt.Sprintf("%s (%d)", billSummarySheetPrefix, sheetIdx+1)
		}
		idx, err := f.NewSheet(name)
		if err != nil {
			return err
		}
		if sheetIdx == 0 {
			f.SetActiveSheet(idx)
		}
		s, err := f.NewStreamWriter(name)
		if err != nil {
			return err
		}
		for i, w := range billSummaryColWidths {
			if err := s.SetColWidth(i+1, i+1, w); err != nil {
				return err
			}
		}
		header := make([]any, len(billSummaryHeaders))
		for i, h := range billSummaryHeaders {
			header[i] = excelize.Cell{Value: h, StyleID: headerStyle}
		}
		if err := s.SetRow("A1", header); err != nil {
			return err
		}
		sw = s
		rowInSt = 1
		sheetIdx++
		return nil
	}

	if err := newSheet(); err != nil {
		return err
	}

	for _, k := range keys {
		if rowInSt >= excelSingleSheetSoftCap {
			if err := newSheet(); err != nil {
				return err
			}
		}
		r := agg.rows[k]
		usd := roundTo6(float64(r.Quota) / common.QuotaPerUnit)
		cny := roundTo6(usd * exchangeRate)
		totalUSD += usd
		totalCNY += cny
		sumPrompt += r.PromptTokens
		sumComp += r.CompletionTokens
		sumCRead += r.CacheReadTokens
		sumCCreat += r.CacheCreationTokens

		row := []any{
			k.Day, k.Username, k.ChannelId, k.TokenName, k.ModelName,
			formatPrice(usd), formatRatio(exchangeRate), formatPrice(cny),
			r.PromptTokens, r.CompletionTokens, r.CacheReadTokens, r.CacheCreationTokens,
		}
		cell, err := excelize.CoordinatesToCellName(1, rowInSt+1)
		if err != nil {
			return err
		}
		if err := sw.SetRow(cell, row); err != nil {
			return err
		}
		rowInSt++
	}

	// 合计行
	totalRow := []any{
		"合计", "", "", "", "",
		formatPrice(roundTo6(totalUSD)), "", formatPrice(roundTo6(totalCNY)),
		sumPrompt, sumComp, sumCRead, sumCCreat,
	}
	cell, err := excelize.CoordinatesToCellName(1, rowInSt+1)
	if err != nil {
		return err
	}
	if err := sw.SetRow(cell, totalRow); err != nil {
		return err
	}

	if err := sw.Flush(); err != nil {
		return err
	}
	return nil
}
