package controller

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

var billDetailColumns = func() []logExportColumn {
	keys := []string{"time", "username", "token", "group", "model", "type", "prompt", "completion"}
	cols := make([]logExportColumn, 0, len(keys)+5)
	for _, k := range keys {
		cols = append(cols, logExportColumnMap[k])
	}
	cols = append(cols, cacheReadColumn, cacheCreationColumn,
		logExportColumnMap["cost"], billingColumn, requestIdColumn)
	return cols
}()

// billDetailWriter 缓冲当天行，日切/finish 时按模型排序写出。
type billDetailWriter struct {
	f          *excelize.File
	splitModel bool
	headerID   int
	wrapID     int
	modelHdrID int
	costID     int
	softCap    int

	curDay string
	buffer []*model.Log
}

func newBillDetailWriter(f *excelize.File, splitModel bool) (*billDetailWriter, error) {
	headerID, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	wrapID, err := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}})
	if err != nil {
		return nil, err
	}
	modelHdrID, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#F2F2F2"}},
	})
	if err != nil {
		return nil, err
	}
	// 费用列写原生数字，$ 前缀由数字格式渲染，Excel 仍可直接求和。
	costFmt := `"$"` + billMoneyNumFmt
	costID, err := f.NewStyle(&excelize.Style{CustomNumFmt: &costFmt})
	if err != nil {
		return nil, err
	}
	return &billDetailWriter{
		f: f, splitModel: splitModel,
		headerID: headerID, wrapID: wrapID, modelHdrID: modelHdrID, costID: costID,
		softCap: excelSingleSheetSoftCap,
	}, nil
}

func (w *billDetailWriter) addBatch(logs []*model.Log) error {
	for _, log := range logs {
		// 明细只列消费与退款，其余类型跳过（与汇总口径一致）。
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund {
			continue
		}
		day := time.Unix(log.CreatedAt, 0).Format("2006-01-02")
		if w.curDay != "" && day != w.curDay {
			if err := w.flushDay(); err != nil {
				return err
			}
		}
		w.curDay = day
		w.buffer = append(w.buffer, log)
	}
	return nil
}

func (w *billDetailWriter) finish() error {
	if len(w.buffer) > 0 {
		return w.flushDay()
	}
	return nil
}

func (w *billDetailWriter) flushDay() error {
	logs := w.buffer
	w.buffer = nil
	day := w.curDay

	if w.splitModel {
		sort.SliceStable(logs, func(i, j int) bool {
			if logs[i].ModelName != logs[j].ModelName {
				return logs[i].ModelName < logs[j].ModelName
			}
			return logs[i].CreatedAt > logs[j].CreatedAt // DESC within model
		})
	}
	logs = alignTaskPairs(logs)
	drs := mergeSeedanceTaskRows(logs)

	// 单 sheet 内分片：超 softCap 行时滚动为 (2)(3)…
	suffix := 1
	rowIn := 0
	var sw *excelize.StreamWriter
	var lastModel string

	openSheet := func() error {
		if sw != nil {
			if err := sw.Flush(); err != nil {
				return err
			}
		}
		name := day
		if suffix > 1 {
			name = fmt.Sprintf("%s (%d)", day, suffix)
		}
		if _, err := w.f.NewSheet(name); err != nil {
			return err
		}
		s, err := w.f.NewStreamWriter(name)
		if err != nil {
			return err
		}
		for i, col := range billDetailColumns {
			if err := s.SetColWidth(i+1, i+1, col.width); err != nil {
				return err
			}
		}
		header := make([]any, len(billDetailColumns))
		for i, col := range billDetailColumns {
			header[i] = excelize.Cell{Value: col.header, StyleID: w.headerID}
		}
		if err := s.SetRow("A1", header); err != nil {
			return err
		}
		sw = s
		rowIn = 1
		lastModel = "" // reset so emitModelHeader re-fires on the new sheet
		suffix++
		return nil
	}

	// emitModelHeader writes "模型：<name>" into the current row and advances rowIn.
	// If writing the header itself hits the soft cap, the sheet is rolled first so
	// the header always lands as the first content row on a fresh sheet.
	emitModelHeader := func(modelName string) error {
		if rowIn >= w.softCap {
			if err := openSheet(); err != nil {
				return err
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, rowIn+1)
		if err := sw.SetRow(cell, []any{excelize.Cell{Value: "模型：" + modelName, StyleID: w.modelHdrID}}); err != nil {
			return err
		}
		rowIn++
		lastModel = modelName
		return nil
	}

	if err := openSheet(); err != nil {
		return err
	}

	billingIdx := -1
	costIdx := -1
	for i, col := range billDetailColumns {
		if col.key == "billing" {
			billingIdx = i
		}
		if col.key == "cost" {
			costIdx = i
		}
	}

	for _, dr := range drs {
		log := dr.log
		// Roll the sheet if we are at or over the cap before writing the data row.
		// After openSheet, lastModel=="" so emitModelHeader will fire below.
		if rowIn >= w.softCap {
			if err := openSheet(); err != nil {
				return err
			}
		}
		// Emit (or re-emit after a sheet roll) the model header whenever the model changes.
		if w.splitModel && log.ModelName != lastModel {
			if err := emitModelHeader(log.ModelName); err != nil {
				return err
			}
			// The header write may have itself triggered a sheet roll via the cap
			// check inside emitModelHeader; either way lastModel is now set correctly
			// and the data row follows immediately on the correct sheet.
		}
		row := make([]any, len(billDetailColumns))
		for i, col := range billDetailColumns {
			switch {
			case i == costIdx:
				usd := roundTo6(float64(log.Quota) / common.QuotaPerUnit)
				if dr.mergedText != "" {
					// seedance 合并行：费用列直接写净额（预扣±结算差额后）。
					usd = roundTo6(float64(dr.netQuota) / common.QuotaPerUnit)
				} else if log.Type == model.LogTypeRefund {
					// 退款行费用写负数：与消费同列求和即得净额（多退少补一目了然）。
					usd = -usd
				}
				row[i] = excelize.Cell{Value: usd, StyleID: w.costID}
			case i == billingIdx:
				if dr.mergedText != "" {
					row[i] = excelize.Cell{Value: dr.mergedText, StyleID: w.wrapID}
				} else {
					row[i] = excelize.Cell{Value: cellValue(col, log), StyleID: w.wrapID}
				}
			case dr.mergedText != "" && col.key == "type":
				// 合并行统一显示消费（净额视角）。
				row[i] = "消费"
			default:
				row[i] = cellValue(col, log)
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, rowIn+1)
		if err := sw.SetRow(cell, row); err != nil {
			return err
		}
		rowIn++
	}
	return sw.Flush()
}

// billDetailRow 是逐日明细的展示行：seedance 任务多行合并后 mergedText 非空。
type billDetailRow struct {
	log        *model.Log
	mergedText string
	netQuota   int
}

// mergeSeedanceTaskRows 把 alignTaskPairs 排好的同 request_id seedance 多行
// 折叠为一行：净额 + 按时间序拼接的各阶段计费过程全文（alignTaskPairs 已保证
// 组内相邻且升序、pre_consume 在前）。其他行原样透传。
func mergeSeedanceTaskRows(logs []*model.Log) []billDetailRow {
	rows := make([]billDetailRow, 0, len(logs))
	i := 0
	for i < len(logs) {
		log := logs[i]
		rid := log.RequestId
		if rid == "" || !strings.Contains(log.ModelName, "seedance") || !strings.Contains(log.Other, `"billing_stage"`) {
			rows = append(rows, billDetailRow{log: log})
			i++
			continue
		}
		j := i
		var group []*model.Log
		for j < len(logs) && logs[j].RequestId == rid {
			group = append(group, logs[j])
			j++
		}
		if len(group) == 1 {
			rows = append(rows, billDetailRow{log: group[0]})
			i = j
			continue
		}
		net := 0
		texts := make([]string, 0, len(group))
		for _, g := range group {
			if g.Type == model.LogTypeRefund {
				net -= g.Quota
			} else {
				net += g.Quota
			}
			texts = append(texts, buildBillingText(g))
		}
		if net < 0 {
			net = 0
		}
		rows = append(rows, billDetailRow{log: group[0], mergedText: strings.Join(texts, "\n——\n"), netQuota: net})
		i = j
	}
	return rows
}

// alignTaskPairs pulls rows that share a request_id — an async task's
// pre-consume / settle / refund rows — next to each other so 多退少补 reads as
// one block. A group is anchored where its first row appears in the incoming
// order and sorted chronologically inside (pre-consume first). Ordinary
// requests produce a single log per request_id, so they pass through as-is;
// rows without a request_id (legacy logs) are left untouched.
func alignTaskPairs(logs []*model.Log) []*model.Log {
	byReq := make(map[string][]*model.Log)
	multi := false
	for _, log := range logs {
		if log.RequestId == "" {
			continue
		}
		byReq[log.RequestId] = append(byReq[log.RequestId], log)
		if len(byReq[log.RequestId]) > 1 {
			multi = true
		}
	}
	if !multi {
		return logs
	}
	result := make([]*model.Log, 0, len(logs))
	emitted := make(map[string]bool, len(byReq))
	for _, log := range logs {
		rid := log.RequestId
		if rid == "" {
			result = append(result, log)
			continue
		}
		if emitted[rid] {
			continue
		}
		emitted[rid] = true
		group := byReq[rid]
		if len(group) > 1 {
			sort.SliceStable(group, func(i, j int) bool { return group[i].CreatedAt < group[j].CreatedAt })
		}
		result = append(result, group...)
	}
	return result
}
