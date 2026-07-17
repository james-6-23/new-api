# 汇总账单导出 (Bill Summary Export) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在个人中心新增「账单管理」菜单，按时间/用户名/渠道ID 查询用量并导出「按模型单日汇总」的 Excel，可选附带每日明细（按日分 Sheet、模型分区块、超行自动新增 Sheet）。

**Architecture:** 复用现有流式导出基建。后端在 controller 包内做 Go 内存聚合（复用 `model.GetAllLogsForExport` / `GetUserLogsForExport` 的 keyset 流式分页 + controller 的缓存 JSON helper），单次扫描同时产出汇总 map 与可选明细 Sheet。前端 TanStack Router 新增路由与 feature，blob 下载。

**Tech Stack:** Go 1.22 / Gin / GORM / excelize v2；React 19 / TanStack Router / Base UI / Tailwind / axios；i18next。

## Global Constraints

- JSON 编解码一律用 `common.Marshal/Unmarshal/UnmarshalJsonStr`，禁止直接 `encoding/json`（CLAUDE Rule 1）。
- 三库兼容 SQLite / MySQL / PostgreSQL：只走 GORM 抽象与现有流式函数，不写库特有 SQL（CLAUDE Rule 2）。
- 前端包管理器用 `bun`（CLAUDE Rule 3）。
- 禁止修改/删除 `QuantumNous` / `new-api` 品牌标识；新文件保留现有版权头（CLAUDE Rule 5）。
- 金额换算常量：美元 = `float64(quota) / common.QuotaPerUnit`；`common.QuotaPerUnit = 500000.0`。
- 汇率：`operation_setting.USDExchangeRate`（默认 7.3），入参 `exchange_rate` 可覆盖。
- 导出门禁：`common.LogExportEnabled`（默认 true）。
- 只汇总消费日志：`model.LogTypeConsume`。
- Go module path: `github.com/QuantumNous/new-api`。
- 归日用服务器本地时区：`time.Unix(createdAt, 0).Format("2006-01-02")`。

---

## 后端

### Task 1: 汇总聚合数据结构与聚合函数

在 controller 包内实现纯逻辑的聚合（无 DB 依赖，便于单测）。复用已存在于 `controller/log_export.go` 的 `getCacheTokensFromOther` / `getCacheCreationTokensFromOther`。

**Files:**
- Create: `controller/bill_summary.go`
- Test: `controller/bill_summary_test.go`

**Interfaces:**
- Consumes: `model.Log`（字段 `CreatedAt int64, Username string, ChannelId int, TokenName string, ModelName string, Quota int, PromptTokens int, CompletionTokens int, Other string`）；`getCacheTokensFromOther(*model.Log, string) int`、`getCacheCreationTokensFromOther(*model.Log) int`（同包，已存在）。
- Produces:
  - `type billSummaryKey struct { Day string; Username string; ChannelId int; TokenName string; ModelName string }`
  - `type billSummaryRow struct { Quota int; PromptTokens int; CompletionTokens int; CacheReadTokens int; CacheCreationTokens int }`
  - `type billSummaryAgg struct { rows map[billSummaryKey]*billSummaryRow }`
  - `func newBillSummaryAgg() *billSummaryAgg`
  - `func (a *billSummaryAgg) addBatch(logs []*model.Log)`
  - `func (a *billSummaryAgg) sortedKeys() []billSummaryKey` —— 排序：Day DESC → Username ASC → ChannelId ASC → ModelName ASC → TokenName ASC。

- [ ] **Step 1: Write the failing test**

```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// 2026-06-01 12:00:00 与 13:00:00 同一天；2026-06-02 另一天（服务器本地时区）。
func tsOn(day string, hour int) int64 {
	t, _ := timeParseLocal(day, hour)
	return t
}

func TestBillSummaryAgg_AggregatesByDayModel(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		{CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5,
			Other: `{"cache_tokens":4,"cache_creation_tokens":2,"cache_creation_tokens_5m":1}`},
		{CreatedAt: tsOn("2026-06-01", 13), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1,
			Other: `{"cache_tokens":1}`},
		{CreatedAt: tsOn("2026-06-02", 9), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 200, PromptTokens: 1, CompletionTokens: 1, Other: ``},
	})

	keys := agg.sortedKeys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(keys))
	}
	// Day DESC: 06-02 first
	if keys[0].Day != "2026-06-02" || keys[1].Day != "2026-06-01" {
		t.Fatalf("unexpected key order: %+v", keys)
	}
	r := agg.rows[keys[1]] // 2026-06-01 group
	if r.Quota != 1500 || r.PromptTokens != 12 || r.CompletionTokens != 6 {
		t.Fatalf("bad sums: %+v", r)
	}
	if r.CacheReadTokens != 5 { // 4 + 1
		t.Fatalf("cache read = %d, want 5", r.CacheReadTokens)
	}
	if r.CacheCreationTokens != 3 { // 2 + 1(5m)
		t.Fatalf("cache creation = %d, want 3", r.CacheCreationTokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/ -run TestBillSummaryAgg -v`
Expected: FAIL（`undefined: newBillSummaryAgg` / `timeParseLocal`）。

- [ ] **Step 3: Write minimal implementation**

创建 `controller/bill_summary.go`（含版权头，复制自 `controller/log_export.go` 顶部若有；该文件当前无版权头则不加）：

```go
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
```

在测试文件顶部补一个本地时区时间构造 helper（放 `bill_summary_test.go` 内）：

```go
func timeParseLocal(day string, hour int) (int64, error) {
	t, err := time.ParseInLocation("2006-01-02 15", day+" "+itoa2(hour), time.Local)
	if err != nil {
		return 0, err
	}
	return t.Unix(), nil
}

func itoa2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
```

并在测试文件 import 补 `"strconv"` 与 `"time"`。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./controller/ -run TestBillSummaryAgg -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add controller/bill_summary.go controller/bill_summary_test.go
git commit -m "feat(bill): add bill summary aggregation over consume logs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: 汇总 Sheet 写入（12 列 + 合计行）

把聚合结果写成首个 sheet「汇总账单」。用 excelize StreamWriter，超软上限滚动 `汇总账单 (2)`。

**Files:**
- Create: `controller/bill_summary_excel.go`
- Test: `controller/bill_summary_excel_test.go`

**Interfaces:**
- Consumes: `*billSummaryAgg`、`billSummaryKey`、`billSummaryRow`（Task 1）；`common.QuotaPerUnit`。
- Produces:
  - `const billSummarySheetPrefix = "汇总账单"`
  - `var billSummaryHeaders = []string{...}`（12 列，见下）
  - `func writeBillSummarySheet(f *excelize.File, agg *billSummaryAgg, exchangeRate float64) error` —— 创建汇总 sheet（首个 tab），写表头 + 排序数据行 + 合计行；超 `excelSingleSheetSoftCap` 滚动。金额/tokens 以数字写入，美元/人民币保留 6 位小数（用 `roundTo6`）。

- [ ] **Step 1: Write the failing test**

```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/xuri/excelize/v2"
)

func TestWriteBillSummarySheet_ValuesAndTotals(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.rows[billSummaryKey{Day: "2026-06-01", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o"}] =
		&billSummaryRow{Quota: 1500, PromptTokens: 12, CompletionTokens: 6, CacheReadTokens: 5, CacheCreationTokens: 3}

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheet(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	// header
	h, _ := f.GetCellValue(billSummarySheetPrefix, "A1")
	if h != "日期" {
		t.Fatalf("A1 = %q, want 日期", h)
	}
	// data row
	usd, _ := f.GetCellValue(billSummarySheetPrefix, "F2")
	wantUSD := formatPrice(1500 / common.QuotaPerUnit)
	if usd != wantUSD {
		t.Fatalf("USD cell = %q, want %q", usd, wantUSD)
	}
	rate, _ := f.GetCellValue(billSummarySheetPrefix, "G2")
	if rate != "7.3" {
		t.Fatalf("rate cell = %q, want 7.3", rate)
	}
	prompt, _ := f.GetCellValue(billSummarySheetPrefix, "I2")
	if prompt != "12" {
		t.Fatalf("prompt cell = %q, want 12", prompt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/ -run TestWriteBillSummarySheet -v`
Expected: FAIL（`undefined: writeBillSummarySheet`）。

- [ ] **Step 3: Write minimal implementation**

```go
package controller

import (
	"fmt"
	"math"
	"strconv"

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
	_ = strconv.Itoa // keep import if unused elsewhere
	return nil
}
```

> 注：`formatPrice`(6 位小数)、`formatRatio`(去尾零) 已存在于 `controller/log_export.go`；`excelSingleSheetSoftCap` 同文件已定义。若 `strconv` 未用则删除该 import 与占位行。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./controller/ -run TestWriteBillSummarySheet -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add controller/bill_summary_excel.go controller/bill_summary_excel_test.go
git commit -m "feat(bill): render summary sheet with totals row

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: 每日明细 Sheet（按日分 Sheet、模型分区块、超行滚动）

明细行流式到达（`created_at DESC`，同一天连续）。缓冲当天全部行，日切时按模型排序后写出，模型区块前插标题行；当天行数超软上限时分片 `2026-06-01 (2)`。

**Files:**
- Create: `controller/bill_detail_excel.go`
- Test: `controller/bill_detail_excel_test.go`

**Interfaces:**
- Consumes: `model.Log`；`cellValue(logExportColumn, *model.Log)`、`logExportColumnMap`（`controller/log_export.go` 已存在）。
- Produces:
  - `var billDetailColumns []logExportColumn` —— 明细列：time, username, token, group, model, prompt, completion, cache_read, cache_creation, cost, billing, request_id（全部取自 `logExportColumnMap` / 已有的 `cacheReadColumn`/`cacheCreationColumn`/`billingColumn`/`requestIdColumn`）。
  - `type billDetailWriter struct {...}`
  - `func newBillDetailWriter(f *excelize.File, splitModel bool) (*billDetailWriter, error)`
  - `func (w *billDetailWriter) addBatch(logs []*model.Log) error`
  - `func (w *billDetailWriter) finish() error`

- [ ] **Step 1: Write the failing test**

```go
package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

func TestBillDetailWriter_SplitByModelWithinDay(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	w, err := newBillDetailWriter(f, true)
	if err != nil {
		t.Fatal(err)
	}
	// 同一天两模型，DESC 顺序到达
	if err := w.addBatch([]*model.Log{
		{CreatedAt: tsOn("2026-06-01", 15), Username: "a", ModelName: "gpt-4o", Type: model.LogTypeConsume},
		{CreatedAt: tsOn("2026-06-01", 14), Username: "a", ModelName: "claude-3", Type: model.LogTypeConsume},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}

	if _, err := f.GetSheetIndex("2026-06-01"); err != nil {
		t.Fatalf("day sheet missing: %v", err)
	}
	// 该 sheet 内应含两处「模型：」标题行
	rows, _ := f.GetRows("2026-06-01")
	count := 0
	for _, r := range rows {
		if len(r) > 0 && strings.HasPrefix(r[0], "模型：") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("model header rows = %d, want 2", count)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/ -run TestBillDetailWriter -v`
Expected: FAIL（`undefined: newBillDetailWriter`）。

- [ ] **Step 3: Write minimal implementation**

```go
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

	curDay  string
	buffer  []*model.Log
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
	return &billDetailWriter{f: f, splitModel: splitModel, headerID: headerID, wrapID: wrapID, modelHdrID: modelHdrID}, nil
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

	// 单 sheet 内分片：超 excelSingleSheetSoftCap 滚动 (2)(3)
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
		lastModel = ""
		suffix++
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
		if rowIn >= excelSingleSheetSoftCap {
			if err := openSheet(); err != nil {
				return err
			}
		}
		if w.splitModel && log.ModelName != lastModel {
			lastModel = log.ModelName
			cell, _ := excelize.CoordinatesToCellName(1, rowIn+1)
			if err := sw.SetRow(cell, []any{excelize.Cell{Value: "模型：" + log.ModelName, StyleID: w.modelHdrID}}); err != nil {
				return err
			}
			rowIn++
			if rowIn >= excelSingleSheetSoftCap {
				if err := openSheet(); err != nil {
					return err
				}
			}
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./controller/ -run TestBillDetailWriter -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add controller/bill_detail_excel.go controller/bill_detail_excel_test.go
git commit -m "feat(bill): per-day detail sheets with model blocks

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: HTTP handlers（汇总 + 可选明细 + 汇率 + 截断头）

组装：流式扫描消费日志 → 聚合 + 可选明细 → 写汇总 sheet → 输出 xlsx。管理员/普通用户两个 handler 共用一个内部实现。

**Files:**
- Create: `controller/bill_summary_export.go`
- Modify: 无（仅新增；handler 在 Task 5 挂路由）

**Interfaces:**
- Consumes: `model.GetAllLogsForExport(...)`、`model.GetUserLogsForExport(...)`、`model.LogExportMaxRows("xlsx")`、`model.LogTypeConsume`；`newBillSummaryAgg`、`newBillDetailWriter`、`writeBillSummarySheet`；`operation_setting.USDExchangeRate`、`common.LogExportEnabled`、`common.ApiError`。
- Produces: `func ExportBillSummaryAll(c *gin.Context)`、`func ExportBillSummarySelf(c *gin.Context)`。

- [ ] **Step 1: Write the implementation**

```go
package controller

import (
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

	_ = f.DeleteSheet("Sheet1")
	f.SetActiveSheet(0)

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
		common.SysError("failed to write bill summary xlsx: " + err.Error())
	}
}

// ExportBillSummaryAll 管理员：可按 username / channel 查任意用户。
func ExportBillSummaryAll(c *gin.Context) {
	p := parseBillExportParams(c)
	runBillExport(c, p, func(maxRows int, consume func([]*model.Log) error) (bool, error) {
		return model.GetAllLogsForExport(model.LogTypeConsume, p.startTimestamp, p.endTimestamp,
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
		return model.GetUserLogsForExport(userId, model.LogTypeConsume, p.startTimestamp, p.endTimestamp,
			p.modelName, p.tokenName, p.group, "", maxRows, consume)
	})
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./controller/...`
Expected: 无错误。

- [ ] **Step 3: Commit**

```bash
git add controller/bill_summary_export.go
git commit -m "feat(bill): admin/self bill summary export handlers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: 注册路由

**Files:**
- Modify: `router/api-router.go`（在 `logRoute.GET("/self/export", ...)` 之后追加两行）

**Interfaces:**
- Consumes: `controller.ExportBillSummaryAll`、`controller.ExportBillSummarySelf`（Task 4）。

- [ ] **Step 1: Add routes**

在 `router/api-router.go` 中，`logRoute.GET("/self/export", middleware.UserAuth(), controller.ExportUserLogs)` 之后插入：

```go
		logRoute.GET("/bill/export", middleware.AdminAuth(), controller.ExportBillSummaryAll)
		logRoute.GET("/self/bill/export", middleware.UserAuth(), controller.ExportBillSummarySelf)
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: 无错误。

- [ ] **Step 3: Commit**

```bash
git add router/api-router.go
git commit -m "feat(bill): register bill summary export routes

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 前端 (`web/default`)

> 所有 `bun` 命令在 `web/default/` 目录执行。新文件顶部复制现有版权头（见任意 `.tsx`，如 `hooks/use-admin.ts` 第 1-18 行）。

### Task 6: 新增侧边栏菜单 + i18n key

**Files:**
- Modify: `web/default/src/hooks/use-sidebar-data.ts`（`personal` 组内、import 段）
- Modify: `web/default/src/i18n/locales/zh.json`

**Interfaces:**
- Produces: 侧边栏出现「账单管理」项，url `/bill-management`。

- [ ] **Step 1: 加图标 import 与菜单项**

在 `use-sidebar-data.ts` 顶部 lucide import 里加入 `ReceiptText`（按字母序插入）：

```ts
  Radio,
  ReceiptText,
  Settings,
```

在 `personal` 组 items 内、Profile 之后追加：

```ts
          {
            title: t('Bill Management'),
            url: '/bill-management',
            icon: ReceiptText,
          },
```

- [ ] **Step 2: 加中文翻译**

在 `web/default/src/i18n/locales/zh.json` 加入键值（保持 flat JSON，key 为英文源串）：

```json
  "Bill Management": "账单管理",
  "Export Summary Bill": "导出汇总账单",
  "Include daily detail": "附带每日明细账",
  "Split detail by model": "明细分不同模型",
  "Exchange rate (USD to CNY)": "汇率 (美元→人民币)",
  "Channel ID": "渠道ID",
  "Export truncated, please narrow the time range": "数据超出上限，已截断，请缩小时间范围",
```

（如某 key 已存在则跳过，避免重复键。）

- [ ] **Step 3: 校验构建**

Run（在 `web/default/`）: `bun run build`
Expected: 构建成功。

- [ ] **Step 4: Commit**

```bash
git add web/default/src/hooks/use-sidebar-data.ts web/default/src/i18n/locales/zh.json
git commit -m "feat(bill): add bill management sidebar entry and i18n

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: 前端下载 API + 路由文件

**Files:**
- Create: `web/default/src/features/bill-management/api.ts`
- Create: `web/default/src/routes/_authenticated/bill-management/index.tsx`

**Interfaces:**
- Consumes: `api` from `@/lib/api`。
- Produces:
  - `interface BillExportParams { start_timestamp?: number; end_timestamp?: number; username?: string; channel?: number; token_name?: string; model_name?: string; group?: string; with_detail?: 0 | 1; detail_split_model?: 0 | 1; exchange_rate?: number }`
  - `async function exportBillSummary(params: BillExportParams, isAdmin: boolean): Promise<{ truncated: boolean }>` —— blob 下载并触发保存，返回是否截断。

- [ ] **Step 1: 写 api.ts**

```ts
// <复制版权头>
import { api } from '@/lib/api'

export interface BillExportParams {
  start_timestamp?: number
  end_timestamp?: number
  username?: string
  channel?: number
  token_name?: string
  model_name?: string
  group?: string
  with_detail?: 0 | 1
  detail_split_model?: 0 | 1
  exchange_rate?: number
}

export async function exportBillSummary(
  params: BillExportParams,
  isAdmin: boolean
): Promise<{ truncated: boolean }> {
  const path = isAdmin ? '/api/log/bill/export' : '/api/log/self/bill/export'
  const search = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== '' && v !== null) {
      search.append(k, String(v))
    }
  })
  const res = await api.get(`${path}?${search.toString()}`, {
    responseType: 'blob',
  })
  const truncated = res.headers['x-export-truncated'] === '1'

  const blob = new Blob([res.data], {
    type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `bill-summary-${Date.now()}.xlsx`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)

  return { truncated }
}
```

- [ ] **Step 2: 写路由文件**

```tsx
// <复制版权头>
import { createFileRoute } from '@tanstack/react-router'
import { BillExportPage } from '@/features/bill-management/components/bill-export-page'

export const Route = createFileRoute('/_authenticated/bill-management/')({
  component: BillExportPage,
})
```

- [ ] **Step 3: 生成路由树并校验**

Run（在 `web/default/`）: `bun run build`
Expected: 构建成功（TanStack 会重新生成 `routeTree.gen.ts`；`BillExportPage` 未创建前此步会失败——因此先做 Task 8 的组件再一起 build，或此步允许失败，留待 Task 8 通过）。

> 说明：Task 7 与 Task 8 是耦合的（路由引用组件）。实现时先建组件文件骨架再 build。若单独提交 Task 7，先建一个最小占位组件避免 build 失败。

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/bill-management/api.ts web/default/src/routes/_authenticated/bill-management/index.tsx
git commit -m "feat(bill): bill export api and route

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: 账单管理页面（筛选表单 + 导出）

**Files:**
- Create: `web/default/src/features/bill-management/components/bill-export-page.tsx`

**Interfaces:**
- Consumes: `exportBillSummary`、`BillExportParams`（Task 7）；`useIsAdmin` from `@/hooks/use-admin`；`useTranslation`；现有 UI 组件（Button/Input/Switch/Label —— 按 `web/default/src/components/ui/` 现有导出使用）；`toast`（现有通知，参考其他 feature 用法）。
- Produces: `export function BillExportPage()`。

- [ ] **Step 1: 探索现有 UI 组件与 toast 用法（只读，不改）**

Run（在仓库根）: `ls web/default/src/components/ui | grep -Ei 'button|input|switch|label'`
并 `grep -rn "from 'sonner'\|toast(" web/default/src/features/usage-logs/components/*.tsx | head`
用于确认 Button/Input/Switch/Label 的确切导入路径与 toast API。

- [ ] **Step 2: 写页面组件**

> 以下为结构化实现；`import` 路径以 Step 1 探明的为准（若组件库不同，替换为等价 Base UI 组件）。日期选择可先用两个 `datetime-local` `<input>`（转 Unix 秒），后续可替换现有 `compact-date-time-range-picker`。

```tsx
// <复制版权头>
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { exportBillSummary, type BillExportParams } from '../api'

function toUnix(local: string): number | undefined {
  if (!local) return undefined
  const ms = new Date(local).getTime()
  return Number.isNaN(ms) ? undefined : Math.floor(ms / 1000)
}

export function BillExportPage() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()

  const [start, setStart] = useState('')
  const [end, setEnd] = useState('')
  const [username, setUsername] = useState('')
  const [channel, setChannel] = useState('')
  const [tokenName, setTokenName] = useState('')
  const [modelName, setModelName] = useState('')
  const [rate, setRate] = useState('')
  const [withDetail, setWithDetail] = useState(false)
  const [splitModel, setSplitModel] = useState(false)
  const [loading, setLoading] = useState(false)

  async function handleExport() {
    setLoading(true)
    try {
      const params: BillExportParams = {
        start_timestamp: toUnix(start),
        end_timestamp: toUnix(end),
        token_name: tokenName || undefined,
        model_name: modelName || undefined,
        with_detail: withDetail ? 1 : 0,
        detail_split_model: withDetail && splitModel ? 1 : 0,
        exchange_rate: rate ? Number(rate) : undefined,
      }
      if (isAdmin) {
        params.username = username || undefined
        params.channel = channel ? Number(channel) : undefined
      }
      const { truncated } = await exportBillSummary(params, isAdmin)
      if (truncated) {
        toast.warning(t('Export truncated, please narrow the time range'))
      } else {
        toast.success(t('Export Summary Bill'))
      }
    } catch (e) {
      toast.error(String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="p-4 max-w-2xl space-y-4">
      <h1 className="text-xl font-semibold">{t('Bill Management')}</h1>

      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-1">
          <Label>{t('Start Time')}</Label>
          <Input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} />
        </div>
        <div className="space-y-1">
          <Label>{t('End Time')}</Label>
          <Input type="datetime-local" value={end} onChange={(e) => setEnd(e.target.value)} />
        </div>

        {isAdmin && (
          <>
            <div className="space-y-1">
              <Label>{t('Username')}</Label>
              <Input value={username} onChange={(e) => setUsername(e.target.value)} />
            </div>
            <div className="space-y-1">
              <Label>{t('Channel ID')}</Label>
              <Input value={channel} onChange={(e) => setChannel(e.target.value)} />
            </div>
          </>
        )}

        <div className="space-y-1">
          <Label>{t('Token Name')}</Label>
          <Input value={tokenName} onChange={(e) => setTokenName(e.target.value)} />
        </div>
        <div className="space-y-1">
          <Label>{t('Model Name')}</Label>
          <Input value={modelName} onChange={(e) => setModelName(e.target.value)} />
        </div>
        <div className="space-y-1">
          <Label>{t('Exchange rate (USD to CNY)')}</Label>
          <Input value={rate} onChange={(e) => setRate(e.target.value)} placeholder="7.3" />
        </div>
      </div>

      <div className="flex items-center gap-2">
        <Switch checked={withDetail} onCheckedChange={setWithDetail} />
        <Label>{t('Include daily detail')}</Label>
      </div>
      {withDetail && (
        <div className="flex items-center gap-2">
          <Switch checked={splitModel} onCheckedChange={setSplitModel} />
          <Label>{t('Split detail by model')}</Label>
        </div>
      )}

      <Button onClick={handleExport} disabled={loading}>
        {t('Export Summary Bill')}
      </Button>
    </div>
  )
}
```

> `t('Start Time')`、`t('End Time')`、`t('Username')`、`t('Token Name')`、`t('Model Name')` 大概率已在 `zh.json` 存在（usage-logs 复用）；若缺失则在 Task 6 的 zh.json 补上。

- [ ] **Step 3: 校验构建（含类型 + 路由树）**

Run（在 `web/default/`）: `bun run build`
Expected: 构建成功，`/bill-management` 路由生成。

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/bill-management/components/bill-export-page.tsx web/default/src/i18n/locales/zh.json
git commit -m "feat(bill): bill export page with admin-gated filters

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Manual Verification (功能整体)

- [ ] 后端全量测试：`go test ./controller/... -run 'Bill' -v` 全绿。
- [ ] 全量 build：仓库根 `go build ./...`；`web/default/` `bun run build`。
- [ ] 启动服务，普通用户登录：侧边栏「个人中心」出现「账单管理」；页面无用户名/渠道ID 字段；导出仅含本人数据。
- [ ] 管理员登录：出现用户名/渠道ID 字段；按用户名/渠道ID 过滤导出正确。
- [ ] 汇总 sheet：12 列、日期 DESC 排序、末尾合计行、金额=Quota 换算、汇率取全局或入参覆盖。
- [ ] 勾选「附带每日明细账」：每天一个 sheet；再勾「明细分不同模型」：sheet 内出现「模型：xxx」区块标题。
- [ ] （可选）构造超软上限数据验证 sheet 滚动与截断头 toast。

---

## Self-Review Notes

- **Spec 覆盖**：汇总 12 列 / 单日按模型汇总 (Task 1,2) / 明细分 sheet+模型分区块+超行滚动 (Task 3) / 汇率覆盖 (Task 4) / 权限双路由 (Task 4,5) / 菜单 (Task 6) / 前端页面下载 (Task 7,8)。均有对应任务。
- **偏离 spec 记录**：聚合逻辑放在 controller 包（非 spec 写的 `model/bill_summary.go`），因缓存 JSON helper 在 controller 且 model 已有流式导出函数可复用——更 DRY，无需新 model 函数。
- **类型一致性**：`billSummaryKey/Row/Agg`、`writeBillSummarySheet`、`newBillDetailWriter/addBatch/finish`、`ExportBillSummaryAll/Self`、`exportBillSummary` 全程签名一致。
- **缓存列**：读取=`cache_tokens`；创建=`cache_creation_tokens(+5m+1h)`，与现有明细导出口径一致。
