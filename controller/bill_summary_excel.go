package controller

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/xuri/excelize/v2"
)

const (
	// billDailySheetPrefix 账单明细（按天/周/月聚合的每期汇总表）。
	// v2 精简（2026-08-02）：总对账单/按日汇总/按令牌汇总已删除，
	// 固定 sheet 收敛为 账单汇总(封面) + 账单明细 + 按模型汇总。
	billDailySheetPrefix = "账单明细"
)

// billMoneyNumFmt keeps the six-decimal display of v1 while the cell stays a
// real number (so Excel can sum the column directly).
const billMoneyNumFmt = "0.000000"

func roundTo6(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

// billSheetLayout is one summary sheet's shape; headers/widths depend on the
// export mode (external drops the channel/token columns).
type billSheetLayout struct {
	prefix  string
	headers []string
	widths  []float64
}

func billDailyLayout(external bool) billSheetLayout {
	if external {
		return billSheetLayout{
			prefix: billDailySheetPrefix,
			headers: []string{
				"日期", "用户名", "模型名称", "刊例价", "专属倍率",
				"计费记录", "请求数", "刊例价金额(美元)",
				"汇总金额(美元)", "汇率", "汇总金额(人民币)",
				"输入tokens", "输出tokens", "缓存读取tokens", "缓存创建tokens",
			},
			widths: []float64{14, 16, 22, 10, 10, 10, 10, 16, 16, 8, 18, 12, 12, 16, 16},
		}
	}
	return billSheetLayout{
		prefix: billDailySheetPrefix,
		headers: []string{
			"日期", "用户名", "渠道ID", "令牌名称", "模型名称", "刊例价", "专属倍率",
			"计费记录", "请求数", "刊例价金额(美元)",
			"汇总金额(美元)", "汇率", "汇总金额(人民币)",
			"输入tokens", "输出tokens", "缓存读取tokens", "缓存创建tokens",
		},
		widths: []float64{14, 16, 10, 16, 22, 10, 10, 10, 10, 16, 16, 8, 18, 12, 12, 16, 16},
	}
}

type billExcelStyles struct {
	header int
	money  int
}

func newBillExcelStyles(f *excelize.File) (billExcelStyles, error) {
	header, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return billExcelStyles{}, err
	}
	numFmt := billMoneyNumFmt
	money, err := f.NewStyle(&excelize.Style{CustomNumFmt: &numFmt})
	if err != nil {
		return billExcelStyles{}, err
	}
	return billExcelStyles{header: header, money: money}, nil
}

// writeBillRowsSheet streams pre-built rows into sheets named by layout.prefix,
// rolling to "<prefix> (2)" ... past excelSingleSheetSoftCap rows.
func writeBillRowsSheet(f *excelize.File, layout billSheetLayout, styles billExcelStyles, rows [][]any) error {
	var (
		sw       *excelize.StreamWriter
		sheetIdx int
		rowInSt  int
	)

	newSheet := func() error {
		if sw != nil {
			if err := sw.Flush(); err != nil {
				return err
			}
		}
		name := layout.prefix
		if sheetIdx > 0 {
			name = fmt.Sprintf("%s (%d)", layout.prefix, sheetIdx+1)
		}
		if _, err := f.NewSheet(name); err != nil {
			return err
		}
		s, err := f.NewStreamWriter(name)
		if err != nil {
			return err
		}
		for i, w := range layout.widths {
			if err := s.SetColWidth(i+1, i+1, w); err != nil {
				return err
			}
		}
		header := make([]any, len(layout.headers))
		for i, h := range layout.headers {
			header[i] = excelize.Cell{Value: h, StyleID: styles.header}
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
	for _, row := range rows {
		if rowInSt >= excelSingleSheetSoftCap {
			if err := newSheet(); err != nil {
				return err
			}
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
	return sw.Flush()
}

// writeBillSummarySheets writes the 账单明细 sheet (per-period aggregation).
// v2 精简：总对账单已删除——区间合计在封面（账单汇总），跨期净额可由明细列求和。
// Amount cells are written as float64 with a 0.000000 number format; no totals
// row is appended.
func writeBillSummarySheets(f *excelize.File, agg *billSummaryAgg, exchangeRate float64) error {
	styles, err := newBillExcelStyles(f)
	if err != nil {
		return err
	}
	keys := agg.sortedKeys()

	money := func(v float64) excelize.Cell {
		return excelize.Cell{Value: roundTo6(v), StyleID: styles.money}
	}

	dailyRows := make([][]any, 0, len(keys))
	for _, k := range keys {
		r := agg.rows[k]
		usd := float64(r.Quota) / common.QuotaPerUnit
		row := []any{billPeriodLabel(k.Day, agg.granularity), k.Username}
		if !agg.external {
			row = append(row, k.ChannelId, k.TokenName)
		}
		// 刊例价/专属倍率：旧日志无定价键时留空，不写 0（避免误读为免费）。
		var listPrice, ratio any
		if r.hasPrice {
			listPrice = r.ListPriceUSD
		}
		if r.hasRatio {
			ratio = r.EffectiveRatio
		}
		row = append(row, k.ModelName, listPrice, ratio,
			r.BillingRecords, r.RequestCount, money(r.ListQuota/common.QuotaPerUnit),
			money(usd), exchangeRate, money(usd*exchangeRate),
			r.PromptTokens, r.CompletionTokens, r.CacheReadTokens, r.CacheCreationTokens,
		)
		dailyRows = append(dailyRows, row)
	}
	return writeBillRowsSheet(f, billDailyLayout(agg.external), styles, dailyRows)
}
