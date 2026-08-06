package controller

import (
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/xuri/excelize/v2"
)

const (
	billCoverSheetName = "账单汇总"
	// billByDaySheetPrefix / billByTokenSheetPrefix 的 sheet 已在 v2 精简中删除，
	// 常量保留供 finalize 前缀匹配的历史工作簿兼容与测试断言"不存在"使用。
	billByDaySheetPrefix   = "按日汇总"
	billByTokenSheetPrefix = "按令牌汇总"
	billByModelSheetPrefix = "按模型汇总"
	billTsFormat           = "2006-01-02 15:04:05"
)

func billRefTs(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).Format(billTsFormat)
}

// billCoverMeta 封面（账单汇总 sheet）的导出上下文：客户、结算区间、汇率、
// 筛选回显与截断标志。
type billCoverMeta struct {
	Customer     string
	StartTs      int64
	EndTs        int64
	ExchangeRate float64
	Filters      []string
	Truncated    bool
	MaxRows      int
}

// writeBillCoverSheet 输出参考账单版式的封面：结算信息、用量与计数、金额口径
// 与合计、筛选/截断说明。固定行号便于测试与阅读（A1 标题，4-6 结算，8-12 用量，
// 14-17 金额，19 起说明）。
func writeBillCoverSheet(f *excelize.File, styles billExcelStyles, meta billCoverMeta, tot billRefRow) error {
	if _, err := f.NewSheet(billCoverSheetName); err != nil {
		return err
	}
	set := func(cell string, v any) {
		_ = f.SetCellValue(billCoverSheetName, cell, v)
	}
	setMoney := func(cell string, v float64) {
		_ = f.SetCellValue(billCoverSheetName, cell, roundTo6(v))
		_ = f.SetCellStyle(billCoverSheetName, cell, cell, styles.money)
	}
	tsOrDash := func(ts int64, fallback int64) string {
		if ts == 0 {
			ts = fallback
		}
		if ts == 0 {
			return "-"
		}
		return time.Unix(ts, 0).Format(billTsFormat)
	}

	for col, w := range map[string]float64{"A": 20, "B": 26, "C": 20, "D": 26, "E": 14, "F": 22} {
		_ = f.SetColWidth(billCoverSheetName, col, col, w)
	}

	set("A1", "账单详情")
	set("A3", "结算信息")
	customer := meta.Customer
	if customer == "" {
		customer = "全部用户"
	}
	set("A4", "客户")
	set("B4", customer)
	set("C4", "时区")
	set("D4", fmt.Sprintf("UTC%s（服务器时区）", time.Now().Format("-07:00")))
	set("E4", "生成时间")
	set("F4", time.Now().Format(billTsFormat))
	set("A5", "结算开始")
	set("B5", tsOrDash(meta.StartTs, tot.FirstTs))
	set("C5", "结算结束")
	set("D5", tsOrDash(meta.EndTs, tot.LastTs))
	set("E5", "边界")
	set("F5", "包含")
	set("A6", "首笔计费")
	set("B6", billRefTs(tot.FirstTs))
	set("C6", "末笔计费")
	set("D6", billRefTs(tot.LastTs))

	set("A8", "用量与计数")
	set("A9", "计费记录")
	set("B9", tot.BillingRecords)
	set("C9", "请求数")
	set("D9", tot.RequestCount)
	set("A10", "输入 tokens")
	set("B10", tot.PromptTokens)
	set("C10", "输出 tokens")
	set("D10", tot.CompletionTokens)
	set("A11", "缓存读取 tokens")
	set("B11", tot.CacheReadTokens)
	set("C11", "缓存创建 tokens")
	set("D11", tot.CacheCreationTokens)
	set("A12", "Quota Units")
	set("B12", tot.Quota)

	set("A14", "金额")
	set("A15", "金额口径")
	set("B15", "USD = quota_units / "+strconv.FormatFloat(common.QuotaPerUnit, 'f', -1, 64))
	set("A16", "刊例价金额(美元)")
	setMoney("B16", tot.ListQuota/common.QuotaPerUnit)
	set("C16", "账单金额(美元)")
	usd := float64(tot.Quota) / common.QuotaPerUnit
	setMoney("D16", usd)
	set("A17", "汇率(CNY/USD)")
	set("B17", meta.ExchangeRate)
	set("C17", "账单金额(人民币)")
	setMoney("D17", usd*meta.ExchangeRate)

	set("A19", "说明")
	rowIdx := 20
	for _, filter := range meta.Filters {
		set(fmt.Sprintf("A%d", rowIdx), "筛选："+filter)
		rowIdx++
	}
	if meta.Truncated {
		set(fmt.Sprintf("A%d", rowIdx), fmt.Sprintf("数据已按上限 %d 行截断，金额仅覆盖已导出行", meta.MaxRows))
	}
	for _, cell := range []string{"A1", "A3", "A8", "A14", "A19"} {
		_ = f.SetCellStyle(billCoverSheetName, cell, cell, styles.header)
	}
	return nil
}

// writeBillReferenceSheets 输出 按模型汇总 sheet（参考账单版式）。
// v2 精简：按日汇总/按令牌汇总 已删除——按日走账单明细，按令牌信息价值低。
// withUser=true（internal 模式）时增加用户名首列。
func writeBillReferenceSheets(f *excelize.File, styles billExcelStyles, ref *billRefAgg, withUser bool, exchangeRate float64) error {
	money := func(v float64) excelize.Cell {
		return excelize.Cell{Value: roundTo6(v), StyleID: styles.money}
	}
	numRow := func(r *billRefRow) []any {
		return []any{
			r.BillingRecords, r.RequestCount,
			r.PromptTokens, r.CompletionTokens, r.CacheReadTokens, r.CacheCreationTokens,
			r.Quota, money(r.ListQuota / common.QuotaPerUnit), money(float64(r.Quota) / common.QuotaPerUnit),
		}
	}

	headers := []string{"模型名称", "计费记录", "请求数", "输入tokens", "输出tokens",
		"缓存读取tokens", "缓存创建tokens", "Quota Units", "刊例价金额(美元)", "汇总金额(美元)",
		"首笔计费时间", "末笔计费时间"}
	widths := []float64{22, 10, 10, 12, 12, 16, 16, 14, 16, 16, 20, 20}
	if withUser {
		headers = append([]string{"用户名"}, headers...)
		widths = append([]float64{16}, widths...)
	}
	layout := billSheetLayout{prefix: billByModelSheetPrefix, headers: headers, widths: widths}

	modKeys, modMap := ref.byModel(withUser)
	rows := make([][]any, 0, len(modKeys))
	for _, k := range modKeys {
		r := modMap[k]
		row := make([]any, 0, 14)
		if withUser {
			row = append(row, k.Username)
		}
		row = append(row, k.Name)
		row = append(row, numRow(r)...)
		row = append(row, billRefTs(r.FirstTs), billRefTs(r.LastTs))
		rows = append(rows, row)
	}
	return writeBillRowsSheet(f, layout, styles, rows)
}
