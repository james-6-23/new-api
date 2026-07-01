package controller

import (
	"fmt"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

var billDetailColumns = func() []logExportColumn {
	keys := []string{"time", "username", "token", "group", "model", "prompt", "completion"}
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
	return &billDetailWriter{
		f: f, splitModel: splitModel,
		headerID: headerID, wrapID: wrapID, modelHdrID: modelHdrID,
		softCap: excelSingleSheetSoftCap,
	}, nil
}

func (w *billDetailWriter) addBatch(logs []*model.Log) error {
	for _, log := range logs {
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
	for i, col := range billDetailColumns {
		if col.key == "billing" {
			billingIdx = i
		}
	}

	for _, log := range logs {
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
			val := cellValue(col, log)
			if i == billingIdx {
				row[i] = excelize.Cell{Value: val, StyleID: w.wrapID}
			} else {
				row[i] = val
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
