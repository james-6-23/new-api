package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type billExportParams struct {
	startTimestamp int64
	endTimestamp   int64
	username       string
	channel        int
	tokenName      string
	modelName      string
	group          string
	withDetail     bool
	detailSplit    bool
	exchangeRate   float64
}

func parseBillExportParams(c *gin.Context) billExportParams {
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	channel, _ := strconv.Atoi(c.Query("channel"))
	rate := operation_setting.USDExchangeRate
	if raw := strings.TrimSpace(c.Query("exchange_rate")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			rate = v
		}
	}
	return billExportParams{
		startTimestamp: start,
		endTimestamp:   end,
		username:       c.Query("username"),
		channel:        channel,
		tokenName:      c.Query("token_name"),
		modelName:      c.Query("model_name"),
		group:          c.Query("group"),
		withDetail:     c.Query("with_detail") == "1",
		detailSplit:    c.Query("detail_split_model") == "1",
		exchangeRate:   rate,
	}
}

// runBillExport 执行一次流式扫描并输出 xlsx。streamFn 由调用方绑定 admin/self 版本。
func runBillExport(c *gin.Context, p billExportParams,
	streamFn func(maxRows int, consume func([]*model.Log) error) (bool, error)) {

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	agg := newBillSummaryAgg()
	var detail *billDetailWriter
	if p.withDetail {
		var err error
		detail, err = newBillDetailWriter(f, p.detailSplit)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	maxRows := model.LogExportMaxRows("xlsx")
	truncated, err := streamFn(maxRows, func(batch []*model.Log) error {
		agg.addBatch(batch)
		if detail != nil {
			return detail.addBatch(batch)
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if detail != nil {
		if err := detail.finish(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := writeBillSummarySheet(f, agg, p.exchangeRate); err != nil {
		common.ApiError(c, err)
		return
	}

	finalizeBillWorkbook(f)

	if truncated {
		c.Writer.Header().Set("X-Export-Truncated", "1")
		c.Writer.Header().Set("X-Export-Max-Rows", strconv.Itoa(maxRows))
	}
	filename := "bill-summary-" + time.Now().Format("20060102-150405") + ".xlsx"
	c.Writer.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.WriteHeader(http.StatusOK)
	if err := f.Write(c.Writer); err != nil {
		common.SysError(fmt.Sprintf("bill summary xlsx write failed uid=%d: %v", c.GetInt("id"), err))
	}
}

// ExportBillSummaryAll 管理员：可按 username / channel 查任意用户。
func ExportBillSummaryAll(c *gin.Context) {
	p := parseBillExportParams(c)
	runBillExport(c, p, func(maxRows int, consume func([]*model.Log) error) (bool, error) {
		return model.GetAllLogsForExport(model.LogTypeUnknown, p.startTimestamp, p.endTimestamp,
			p.modelName, p.username, p.tokenName, p.channel, p.group, "", maxRows, consume)
	})
}

// ExportBillSummarySelf 普通用户：忽略 username/channel，强制锁定本人。
func ExportBillSummarySelf(c *gin.Context) {
	if !common.LogExportEnabled {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "导出功能已关闭"})
		return
	}
	userId := c.GetInt("id")
	p := parseBillExportParams(c)
	p.username = ""
	p.channel = 0
	runBillExport(c, p, func(maxRows int, consume func([]*model.Log) error) (bool, error) {
		return model.GetUserLogsForExport(userId, model.LogTypeUnknown, p.startTimestamp, p.endTimestamp,
			p.modelName, p.tokenName, p.group, "", maxRows, consume)
	})
}

// finalizeBillWorkbook removes the default Sheet1 and makes the summary sheet
// the active/first tab. Detail sheets are created before the summary sheet, so
// after Sheet1 is deleted the summary must be located by name, not index 0.
func finalizeBillWorkbook(f *excelize.File) {
	if err := f.DeleteSheet("Sheet1"); err != nil {
		common.SysLog("bill export: delete default sheet: " + err.Error())
	}
	if idx, err := f.GetSheetIndex(billSummarySheetPrefix); err == nil && idx >= 0 {
		f.SetActiveSheet(idx)
	}
}
