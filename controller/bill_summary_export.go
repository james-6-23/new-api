package controller

import (
	"fmt"
	"net/http"
	"sort"
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
	external       bool
	granularity    string
	exchangeRate   float64
	// coverCustomer 封面「客户」：admin 导出 = username 筛选值（空则"全部用户"），
	// self 导出 = 当前登录用户名（p.username 会被清空，故单独携带）。
	coverCustomer string
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
		external:       c.Query("bill_mode") == "external",
		granularity:    normalizeBillGranularity(c.Query("granularity")),
		exchangeRate:   rate,
	}
}

// runBillExport 执行一次流式扫描并输出 xlsx。streamFn 由调用方绑定 admin/self 版本。
func runBillExport(c *gin.Context, p billExportParams,
	streamFn func(maxRows int, consume func([]*model.Log) error) (bool, error)) {

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	agg := newBillSummaryAgg()
	agg.external = p.external
	agg.granularity = p.granularity
	ref := newBillRefAgg()
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
		ref.addBatch(batch)
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
	if err := writeBillSummarySheets(f, agg, p.exchangeRate); err != nil {
		common.ApiError(c, err)
		return
	}
	styles, err := newBillExcelStyles(f)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := writeBillCoverSheet(f, styles, billCoverMeta{
		Customer:     p.coverCustomer,
		StartTs:      p.startTimestamp,
		EndTs:        p.endTimestamp,
		ExchangeRate: p.exchangeRate,
		Filters:      billFilterEcho(p),
		Truncated:    truncated,
		MaxRows:      maxRows,
	}, ref.totals()); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := writeBillReferenceSheets(f, styles, ref, !p.external, p.exchangeRate); err != nil {
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
	p.coverCustomer = p.username
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
	p.coverCustomer = c.GetString("username")
	runBillExport(c, p, func(maxRows int, consume func([]*model.Log) error) (bool, error) {
		return model.GetUserLogsForExport(userId, model.LogTypeUnknown, p.startTimestamp, p.endTimestamp,
			p.modelName, p.tokenName, p.group, "", maxRows, consume)
	})
}

// billFilterEcho 把非空筛选条件渲染为封面说明行。
func billFilterEcho(p billExportParams) []string {
	var out []string
	if p.username != "" {
		out = append(out, "用户名="+p.username)
	}
	if p.channel != 0 {
		out = append(out, fmt.Sprintf("渠道=%d", p.channel))
	}
	if p.tokenName != "" {
		out = append(out, "令牌="+p.tokenName)
	}
	if p.modelName != "" {
		out = append(out, "模型="+p.modelName)
	}
	if p.group != "" {
		out = append(out, "分组="+p.group)
	}
	return out
}

// finalizeBillWorkbook removes the default Sheet1, moves the summary sheets
// (账单汇总, 账单明细*, 按模型汇总*) in front of the daily detail sheets — they
// are created after the streamed detail sheets, but must appear first in the
// tab order — and activates the cover (falling back to the first summary when
// no cover was written, e.g. legacy tests).
func finalizeBillWorkbook(f *excelize.File) {
	if err := f.DeleteSheet("Sheet1"); err != nil {
		common.SysLog("bill export: delete default sheet: " + err.Error())
	}
	orderedPrefixes := []string{billCoverSheetName, billDailySheetPrefix, billByModelSheetPrefix}
	rank := func(name string) int {
		for idx, prefix := range orderedPrefixes {
			if strings.HasPrefix(name, prefix) {
				return idx
			}
		}
		return len(orderedPrefixes)
	}
	var summaries []string
	firstOther := ""
	for _, name := range f.GetSheetList() {
		if rank(name) < len(orderedPrefixes) {
			summaries = append(summaries, name)
		} else if firstOther == "" {
			firstOther = name
		}
	}
	// 同前缀内保持创建顺序（(2)(3)… 分片），前缀间按 orderedPrefixes 排列。
	sort.SliceStable(summaries, func(i, j int) bool {
		return rank(summaries[i]) < rank(summaries[j])
	})
	if firstOther != "" {
		for _, name := range summaries {
			if err := f.MoveSheet(name, firstOther); err != nil {
				common.SysLog("bill export: move sheet " + name + ": " + err.Error())
			}
		}
	}
	if idx, err := f.GetSheetIndex(billCoverSheetName); err == nil && idx >= 0 {
		f.SetActiveSheet(idx)
		return
	}
	if idx, err := f.GetSheetIndex(billDailySheetPrefix); err == nil && idx >= 0 {
		f.SetActiveSheet(idx)
	}
}
