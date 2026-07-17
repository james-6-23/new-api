# 汇总账单 Phase 2 (表格展示 + 旧版前端) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增分页 JSON 汇总接口，让新版和旧版前端都能在筛选框下展示汇总账单表格（12 列 + 分页 + 全量合计），并保留 Excel 导出。

**Architecture:** 后端复用 Phase 1 的内存聚合（`newBillSummaryAgg`）+ 流式扫描，聚合全量后按页切片，返回 items + total + 全量合计的 JSON（`common.ApiSuccess`）。新版前端在现有账单页加「查询」按钮 + 只读表格（`@/components/ui/table`）；旧版前端新建 `/console/bill` 页面（Semi `Table`）。导出沿用 Phase 1 端点。

**Tech Stack:** Go / Gin / GORM / excelize；React 19 + TanStack Router + Base UI (web/default)；React 18 + Semi Design (web/classic)；axios；i18next。

## Global Constraints

- JSON 业务编解码用 `common.Marshal/Unmarshal`，禁止直接 `encoding/json`（Rule 1）。gin 响应用 `common.ApiSuccess(c, data)` / `common.ApiError(c, err)`。
- 三库兼容：只复用现有流式查询函数，不加库特有 SQL（Rule 2）。
- 前端包管理器用 `bun`，命令在对应 web 目录执行（Rule 3）。
- 禁止修改/删除 `QuantumNous` / `new-api` 品牌标识；新文件复制所在前端的版权头（Rule 5）。
- Go module: `github.com/QuantumNous/new-api`。
- 金额：USD = `float64(quota)/common.QuotaPerUnit`（=500000.0）；CNY = USD×rate；rate = `exchange_rate` 入参>0 时覆盖，否则 `operation_setting.USDExchangeRate`（只读局部，不改全局）；USD/CNY 用 `roundTo6`（已存在于 `controller/bill_summary_excel.go`）。
- 只聚合消费日志：`model.LogTypeConsume`；maxRows = `model.LogExportMaxRows("xlsx")`。
- 复用 Phase 1（已在 prod 上）：`newBillSummaryAgg()`, `(*billSummaryAgg).addBatch([]*model.Log)`, `(*billSummaryAgg).sortedKeys() []billSummaryKey`, 类型 `billSummaryKey{Day,Username,ChannelId,TokenName,ModelName}` / `billSummaryRow{Quota,PromptTokens,CompletionTokens,CacheReadTokens,CacheCreationTokens}`（均在 `controller/bill_summary.go`）。
- 测试 helper `tsOn(day string, hour int) int64` 已存在于 `controller/bill_summary_test.go`（同包，勿重定义）。

---

## 后端

### Task 1: 分页 JSON 汇总接口 + handlers

**Files:**
- Create: `controller/bill_summary_query.go`
- Test: `controller/bill_summary_query_test.go`

**Interfaces:**
- Consumes: `newBillSummaryAgg`, `billSummaryAgg.addBatch/sortedKeys`, `billSummaryKey`, `billSummaryRow`, `roundTo6` (Phase 1, same package); `model.GetAllLogsForExport`, `model.GetUserLogsForExport`, `model.LogExportMaxRows`, `model.LogTypeConsume`; `common.QuotaPerUnit`, `common.ApiSuccess`, `common.ApiError`; `operation_setting.USDExchangeRate`.
- Produces:
  - `type billSummaryItemDTO struct { Date string; Username string; ChannelId int; TokenName string; ModelName string; AmountUSD float64; ExchangeRate float64; AmountCNY float64; PromptTokens int; CompletionTokens int; CacheReadTokens int; CacheCreationTokens int }` (json tags: date, username, channel_id, token_name, model_name, amount_usd, exchange_rate, amount_cny, prompt_tokens, completion_tokens, cache_read_tokens, cache_creation_tokens)
  - `type billSummaryTotalsDTO struct { TotalAmountUSD float64; TotalAmountCNY float64; TotalPromptTokens int; TotalCompletionTokens int; TotalCacheReadTokens int; TotalCacheCreationTokens int }` (json: total_amount_usd, total_amount_cny, total_prompt_tokens, total_completion_tokens, total_cache_read_tokens, total_cache_creation_tokens)
  - `type billSummaryPageDTO struct { Items []billSummaryItemDTO; Total int; Page int; PageSize int; Summary billSummaryTotalsDTO }` (json: items, total, page, page_size, summary)
  - `func buildBillSummaryPage(agg *billSummaryAgg, exchangeRate float64, page, pageSize int) billSummaryPageDTO` — pure, testable: sortedKeys → total = len; compute full totals over all groups; slice [(page-1)*pageSize, +pageSize] into Items with per-row USD/CNY (roundTo6).
  - `func QueryBillSummaryAll(c *gin.Context)`, `func QueryBillSummarySelf(c *gin.Context)`.

- [ ] **Step 1: Write the failing test**

```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestBuildBillSummaryPage_PagingAndTotals(t *testing.T) {
	agg := newBillSummaryAgg()
	// 3 distinct groups (same day, different model) so ordering is deterministic.
	mk := func(model string, quota, p, c, cr, cc int) {
		agg.rows[billSummaryKey{Day: "2026-06-01", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: model}] =
			&billSummaryRow{Quota: quota, PromptTokens: p, CompletionTokens: c, CacheReadTokens: cr, CacheCreationTokens: cc}
	}
	mk("a-model", 1000, 10, 5, 4, 2)
	mk("b-model", 500, 2, 1, 1, 0)
	mk("c-model", 250, 1, 1, 0, 0)

	// page 1, size 2 -> first two by sort (Day, user, channel, model ASC): a-model, b-model
	page := buildBillSummaryPage(agg, 7.3, 1, 2)
	if page.Total != 3 {
		t.Fatalf("total = %d, want 3", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	if page.Items[0].ModelName != "a-model" || page.Items[1].ModelName != "b-model" {
		t.Fatalf("unexpected page-1 order: %+v", page.Items)
	}
	// per-row money
	wantUSD := roundTo6(1000 / common.QuotaPerUnit)
	if page.Items[0].AmountUSD != wantUSD {
		t.Fatalf("row USD = %v, want %v", page.Items[0].AmountUSD, wantUSD)
	}
	if page.Items[0].AmountCNY != roundTo6(wantUSD*7.3) {
		t.Fatalf("row CNY = %v", page.Items[0].AmountCNY)
	}
	if page.Items[0].ExchangeRate != 7.3 {
		t.Fatalf("rate = %v, want 7.3", page.Items[0].ExchangeRate)
	}
	// full totals over ALL 3 groups, independent of paging
	wantTotUSD := roundTo6((1000 + 500 + 250) / common.QuotaPerUnit)
	if page.Summary.TotalAmountUSD != wantTotUSD {
		t.Fatalf("total USD = %v, want %v", page.Summary.TotalAmountUSD, wantTotUSD)
	}
	if page.Summary.TotalPromptTokens != 13 || page.Summary.TotalCompletionTokens != 7 {
		t.Fatalf("token totals wrong: %+v", page.Summary)
	}
	if page.Summary.TotalCacheReadTokens != 5 || page.Summary.TotalCacheCreationTokens != 2 {
		t.Fatalf("cache totals wrong: %+v", page.Summary)
	}

	// page 2, size 2 -> only c-model
	page2 := buildBillSummaryPage(agg, 7.3, 2, 2)
	if len(page2.Items) != 1 || page2.Items[0].ModelName != "c-model" {
		t.Fatalf("page 2 wrong: %+v", page2.Items)
	}

	// page beyond range -> empty items, total still 3
	page3 := buildBillSummaryPage(agg, 7.3, 9, 2)
	if len(page3.Items) != 0 || page3.Total != 3 {
		t.Fatalf("page 3 wrong: len=%d total=%d", len(page3.Items), page3.Total)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/ -run TestBuildBillSummaryPage -v`
Expected: FAIL (`undefined: buildBillSummaryPage`).

- [ ] **Step 3: Write minimal implementation**

```go
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
```

- [ ] **Step 4: Run test + build**

Run: `go test ./controller/ -run TestBuildBillSummaryPage -v` → PASS
Run: `go build ./...` → clean
(Ignore pre-existing failing `TestListModelsTokenLimitIncludesTieredBillingModel` — unrelated, missing users table in test DB.)

- [ ] **Step 5: Commit**

```bash
git add controller/bill_summary_query.go controller/bill_summary_query_test.go
git commit -m "feat(bill): paginated JSON bill summary query endpoints

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: 注册 JSON 汇总路由

**Files:**
- Modify: `router/api-router.go` (after the two Phase-1 `bill/export` lines)

**Interfaces:**
- Consumes: `controller.QueryBillSummaryAll`, `controller.QueryBillSummarySelf` (Task 1).

- [ ] **Step 1: Add routes**

After the existing lines:
```go
		logRoute.GET("/bill/export", middleware.AdminAuth(), controller.ExportBillSummaryAll)
		logRoute.GET("/self/bill/export", middleware.UserAuth(), controller.ExportBillSummarySelf)
```
insert:
```go
		logRoute.GET("/bill/summary", middleware.AdminAuth(), controller.QueryBillSummaryAll)
		logRoute.GET("/self/bill/summary", middleware.UserAuth(), controller.QueryBillSummarySelf)
```

- [ ] **Step 2: Verify build**

Run: `go build ./...` → clean.

- [ ] **Step 3: Commit**

```bash
git add router/api-router.go
git commit -m "feat(bill): register bill summary query routes

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 新版前端 (`web/default`)

> 命令在 `web/default/`。新文件复制现有版权头（见 `hooks/use-admin.ts` 行 1-18）。i18n key 为英文源串，补 `src/i18n/locales/zh.json`。

### Task 3: 新版 — 汇总查询 API

**Files:**
- Modify: `web/default/src/features/bill-management/api.ts`

**Interfaces:**
- Consumes: `api` from `@/lib/api`.
- Produces: `BillSummaryItem`, `BillSummaryTotals`, `BillSummaryResponse` interfaces + `getBillSummary(params, isAdmin, page, pageSize)`.

- [ ] **Step 1: Append to api.ts**

在文件末尾追加（保留现有 `exportBillSummary`）：
```ts
export interface BillSummaryItem {
  date: string
  username: string
  channel_id: number
  token_name: string
  model_name: string
  amount_usd: number
  exchange_rate: number
  amount_cny: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
}

export interface BillSummaryTotals {
  total_amount_usd: number
  total_amount_cny: number
  total_prompt_tokens: number
  total_completion_tokens: number
  total_cache_read_tokens: number
  total_cache_creation_tokens: number
}

export interface BillSummaryResponse {
  items: BillSummaryItem[]
  total: number
  page: number
  page_size: number
  summary: BillSummaryTotals
}

export async function getBillSummary(
  params: Omit<BillExportParams, 'with_detail' | 'detail_split_model'>,
  isAdmin: boolean,
  page: number,
  pageSize: number
): Promise<BillSummaryResponse> {
  const path = isAdmin ? '/api/log/bill/summary' : '/api/log/self/bill/summary'
  const search = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== '' && v !== null) search.append(k, String(v))
  })
  search.append('p', String(page))
  search.append('page_size', String(pageSize))
  const res = await api.get(`${path}?${search.toString()}`)
  return res.data.data as BillSummaryResponse
}
```

- [ ] **Step 2: Typecheck**

Run: `bun run tsc --noEmit` (or `bun run build` if no fast typecheck) → clean.

- [ ] **Step 3: Commit**

```bash
git add web/default/src/features/bill-management/api.ts
git commit -m "feat(bill): default frontend summary query api

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: 新版 — 页面加「查询」按钮 + 汇总表格 + 合计

**Files:**
- Modify: `web/default/src/features/bill-management/components/bill-export-page.tsx`
- Modify: `web/default/src/i18n/locales/zh.json`

**Interfaces:**
- Consumes: `getBillSummary`, `BillSummaryItem`, `BillSummaryResponse` (Task 3); `Table, TableHeader, TableBody, TableHead, TableRow, TableCell` from `@/components/ui/table`; existing `Button`, `useIsAdmin`, `useTranslation`, `toast`.

- [ ] **Step 1: Add state, query handler, and table to the page**

在 `BillExportPage` 组件里（保留现有导出逻辑）新增：

```tsx
// add imports at top:
// import { Table, TableHeader, TableBody, TableHead, TableRow, TableCell } from '@/components/ui/table'
// import { getBillSummary, type BillSummaryResponse } from '../api'

  const [data, setData] = useState<BillSummaryResponse | null>(null)
  const [page, setPage] = useState(1)
  const pageSize = 20
  const [querying, setQuerying] = useState(false)

  async function runQuery(targetPage: number) {
    setQuerying(true)
    try {
      const params = {
        start_timestamp: toUnix(start),
        end_timestamp: toUnix(end),
        token_name: tokenName || undefined,
        model_name: modelName || undefined,
        exchange_rate: rate ? Number(rate) : undefined,
        ...(isAdmin
          ? { username: username || undefined, channel: channel ? Number(channel) : undefined }
          : {}),
      }
      const res = await getBillSummary(params, isAdmin, targetPage, pageSize)
      setData(res)
      setPage(targetPage)
    } catch (e) {
      toast.error(String(e))
    } finally {
      setQuerying(false)
    }
  }
```

在「导出 Excel」按钮旁加「查询」按钮：
```tsx
      <div className="flex gap-2">
        <Button onClick={() => runQuery(1)} disabled={querying}>
          {t('Query')}
        </Button>
        <Button variant="outline" onClick={handleExport} disabled={loading}>
          {t('Export Summary Bill')}
        </Button>
      </div>
```

在按钮下方渲染表格（当 `data` 有值）：
```tsx
      {data && (
        <div className="space-y-2">
          <div className="text-sm text-muted-foreground">
            {t('Total')}: ${data.summary.total_amount_usd.toFixed(6)} / ¥
            {data.summary.total_amount_cny.toFixed(6)} · {t('Prompt Tokens')}{' '}
            {data.summary.total_prompt_tokens} · {t('Completion Tokens')}{' '}
            {data.summary.total_completion_tokens} · {t('Cache Read Tokens')}{' '}
            {data.summary.total_cache_read_tokens} · {t('Cache Creation Tokens')}{' '}
            {data.summary.total_cache_creation_tokens}
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Date')}</TableHead>
                {isAdmin && <TableHead>{t('Username')}</TableHead>}
                {isAdmin && <TableHead>{t('Channel ID')}</TableHead>}
                <TableHead>{t('Token Name')}</TableHead>
                <TableHead>{t('Model Name')}</TableHead>
                <TableHead>{t('Amount (USD)')}</TableHead>
                <TableHead>{t('Exchange Rate')}</TableHead>
                <TableHead>{t('Amount (CNY)')}</TableHead>
                <TableHead>{t('Prompt Tokens')}</TableHead>
                <TableHead>{t('Completion Tokens')}</TableHead>
                <TableHead>{t('Cache Read Tokens')}</TableHead>
                <TableHead>{t('Cache Creation Tokens')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((it, i) => (
                <TableRow key={i}>
                  <TableCell>{it.date}</TableCell>
                  {isAdmin && <TableCell>{it.username}</TableCell>}
                  {isAdmin && <TableCell>{it.channel_id}</TableCell>}
                  <TableCell>{it.token_name}</TableCell>
                  <TableCell>{it.model_name}</TableCell>
                  <TableCell>${it.amount_usd.toFixed(6)}</TableCell>
                  <TableCell>{it.exchange_rate}</TableCell>
                  <TableCell>¥{it.amount_cny.toFixed(6)}</TableCell>
                  <TableCell>{it.prompt_tokens}</TableCell>
                  <TableCell>{it.completion_tokens}</TableCell>
                  <TableCell>{it.cache_read_tokens}</TableCell>
                  <TableCell>{it.cache_creation_tokens}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <div className="flex items-center gap-2">
            <Button variant="outline" disabled={page <= 1 || querying} onClick={() => runQuery(page - 1)}>
              {t('Previous')}
            </Button>
            <span className="text-sm">
              {page} / {Math.max(1, Math.ceil(data.total / pageSize))}
            </span>
            <Button
              variant="outline"
              disabled={page >= Math.ceil(data.total / pageSize) || querying}
              onClick={() => runQuery(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      )}
```

> 若 `Button` 无 `variant="outline"`，去掉该 prop（先读 `@/components/ui/button` 确认支持的 variants）。

- [ ] **Step 2: Add i18n keys**

在 `web/default/src/i18n/locales/zh.json` 补（已存在的跳过，防重复键）：
```json
  "Query": "查询",
  "Date": "日期",
  "Amount (USD)": "金额(美元)",
  "Amount (CNY)": "金额(人民币)",
  "Exchange Rate": "汇率",
  "Prompt Tokens": "输入tokens",
  "Completion Tokens": "输出tokens",
  "Cache Read Tokens": "缓存读取tokens",
  "Cache Creation Tokens": "缓存创建tokens",
  "Total": "合计",
  "Previous": "上一页",
  "Next": "下一页"
```

- [ ] **Step 3: Build**

Run: `bun run build` → 成功。

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/bill-management/components/bill-export-page.tsx web/default/src/i18n/locales/zh.json
git commit -m "feat(bill): default frontend summary table with paging and totals

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 旧版前端 (`web/classic`, Semi Design)

> 命令在 `web/classic/`。新 `.jsx` 复制 classic 版权头（见 `components/table/usage-logs/UsageLogsTable.jsx` 行 1-18）。`API`/`isAdmin`/`downloadBlobAsFile` 从 helpers 引入。classic i18n key 为中文。

### Task 5: 旧版 — 菜单项 + 路由

**Files:**
- Modify: `web/classic/src/components/layout/SiderBar.jsx` (routerMap + personal group item)
- Modify: `web/classic/src/App.jsx` (import + route)
- Create: `web/classic/src/pages/Bill/index.jsx` (minimal shell so the route/build works; table added in Task 6)

**Interfaces:**
- Produces: route `/console/bill` renders `<Bill/>`; sidebar item "账单管理" (itemKey `bill`).

- [ ] **Step 1: SiderBar — routerMap + item**

在 `SiderBar.jsx` 的 `routerMap` 对象里加：
```js
  bill: '/console/bill',
```
在 personal 组（`text: t('个人设置'), itemKey: 'personal'` 那一项附近，同一个 items 数组内）加：
```js
      {
        text: t('账单管理'),
        itemKey: 'bill',
        to: '/bill',
      },
```
(遵循同组现有项结构；若该组用 `isModuleVisible('personal', item.itemKey)` 过滤，保持一致——`bill` 默认可见即可。)

- [ ] **Step 2: Bill page shell**

Create `web/classic/src/pages/Bill/index.jsx`:
```jsx
// <复制 classic 版权头>
import React from 'react';
import BillSummary from '../../components/table/bill-summary';

const Bill = () => {
  return <BillSummary />;
};

export default Bill;
```

> Task 6 会创建 `components/table/bill-summary/index.jsx`。为使本任务可独立 build，先在 Task 6 之前创建一个最小占位 `components/table/bill-summary/index.jsx`（`export default () => null`），或将 Task 5+6 作为一个提交单元实现（推荐后者：先建组件再 build）。

- [ ] **Step 3: App.jsx — import + route**

顶部 import（与其他页面 import 一起）：
```jsx
import Bill from './pages/Bill';
```
在 `/console/log` 路由块之后加：
```jsx
        <Route
          path='/console/bill'
          element={
            <PrivateRoute>
              <Bill />
            </PrivateRoute>
          }
        />
```

- [ ] **Step 4: Build** (defer to Task 6 — needs the real component)

Run (after Task 6 component exists): `bun run build` → 成功。

- [ ] **Step 5: Commit** (合并到 Task 6 一起提交，因组件耦合)

---

### Task 6: 旧版 — 汇总表格页面（Semi Table + 分页 + 合计 + 导出）

**Files:**
- Create: `web/classic/src/components/table/bill-summary/index.jsx`
- Create: `web/classic/src/components/table/bill-summary/BillFilters.jsx`
- Create: `web/classic/src/components/table/bill-summary/BillSummaryTable.jsx`
- Modify: classic i18n zh file (`web/classic/src/i18n/locales/zh.json`) — add any missing keys used.

**Interfaces:**
- Consumes: `API` (from `../../../helpers`), `isAdmin`, `downloadBlobAsFile` (from `../../../helpers/utils`); Semi `Form, Button, Table, Space, Typography` from `@douyinfe/semi-ui`; `useTranslation`.
- Produces: `default` export component rendering filters + table + pagination + totals + export.

- [ ] **Step 1: BillSummaryTable.jsx (presentational Semi Table)**

```jsx
// <复制 classic 版权头>
import React from 'react';
import { Table, Typography } from '@douyinfe/semi-ui';

const money = (v) => (typeof v === 'number' ? v.toFixed(6) : v);

const BillSummaryTable = ({ items, total, page, pageSize, summary, isAdminUser, onPageChange, t }) => {
  const columns = [
    { title: t('日期'), dataIndex: 'date' },
    ...(isAdminUser
      ? [
          { title: t('用户名'), dataIndex: 'username' },
          { title: t('渠道ID'), dataIndex: 'channel_id' },
        ]
      : []),
    { title: t('令牌名称'), dataIndex: 'token_name' },
    { title: t('模型名称'), dataIndex: 'model_name' },
    { title: t('金额(美元)'), dataIndex: 'amount_usd', render: (v) => `$${money(v)}` },
    { title: t('汇率'), dataIndex: 'exchange_rate' },
    { title: t('金额(人民币)'), dataIndex: 'amount_cny', render: (v) => `¥${money(v)}` },
    { title: t('输入tokens'), dataIndex: 'prompt_tokens' },
    { title: t('输出tokens'), dataIndex: 'completion_tokens' },
    { title: t('缓存读取tokens'), dataIndex: 'cache_read_tokens' },
    { title: t('缓存创建tokens'), dataIndex: 'cache_creation_tokens' },
  ];
  return (
    <div>
      {summary && (
        <Typography.Text type="secondary" style={{ display: 'block', margin: '8px 0' }}>
          {t('合计')}: ${money(summary.total_amount_usd)} / ¥{money(summary.total_amount_cny)} ·{' '}
          {t('输入tokens')} {summary.total_prompt_tokens} · {t('输出tokens')} {summary.total_completion_tokens} ·{' '}
          {t('缓存读取tokens')} {summary.total_cache_read_tokens} · {t('缓存创建tokens')}{' '}
          {summary.total_cache_creation_tokens}
        </Typography.Text>
      )}
      <Table
        columns={columns}
        dataSource={items}
        rowKey={(r, i) => `${r.date}-${r.username}-${r.channel_id}-${r.token_name}-${r.model_name}-${i}`}
        pagination={{
          currentPage: page,
          pageSize,
          total,
          onPageChange,
        }}
      />
    </div>
  );
};

export default BillSummaryTable;
```

- [ ] **Step 2: BillFilters.jsx (Semi Form + Query/Export buttons)**

```jsx
// <复制 classic 版权头>
import React from 'react';
import { Form, Button, Space } from '@douyinfe/semi-ui';

const BillFilters = ({ formApiRef, isAdminUser, onQuery, onExport, loading, t }) => {
  return (
    <Form getFormApi={(api) => (formApiRef.current = api)} layout="horizontal">
      <Form.DatePicker field="start_time" label={t('开始时间')} type="dateTime" />
      <Form.DatePicker field="end_time" label={t('结束时间')} type="dateTime" />
      {isAdminUser && <Form.Input field="username" label={t('用户名')} />}
      {isAdminUser && <Form.Input field="channel" label={t('渠道ID')} />}
      <Form.Input field="token_name" label={t('令牌名称')} />
      <Form.Input field="model_name" label={t('模型名称')} />
      <Form.Input field="exchange_rate" label={t('汇率')} placeholder="7.3" />
      <Form.Switch field="with_detail" label={t('附带每日明细账')} />
      <Form.Switch field="detail_split_model" label={t('明细分不同模型')} />
      <Space>
        <Button theme="solid" loading={loading} onClick={onQuery}>
          {t('查询')}
        </Button>
        <Button onClick={onExport}>{t('导出汇总账单')}</Button>
      </Space>
    </Form>
  );
};

export default BillFilters;
```

- [ ] **Step 3: index.jsx (state, API calls, export)**

```jsx
// <复制 classic 版权头>
import React, { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { API, isAdmin, showError } from '../../../helpers';
import { downloadBlobAsFile } from '../../../helpers/utils';
import BillFilters from './BillFilters';
import BillSummaryTable from './BillSummaryTable';

const PAGE_SIZE = 20;

const toUnix = (v) => {
  if (!v) return undefined;
  const ms = new Date(v).getTime();
  return Number.isNaN(ms) ? undefined : Math.floor(ms / 1000);
};

const BillSummary = () => {
  const { t } = useTranslation();
  const isAdminUser = isAdmin();
  const formApiRef = useRef(null);
  const [data, setData] = useState(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);

  const collectParams = () => {
    const v = formApiRef.current ? formApiRef.current.getValues() : {};
    const p = {
      start_timestamp: toUnix(v.start_time),
      end_timestamp: toUnix(v.end_time),
      token_name: v.token_name || undefined,
      model_name: v.model_name || undefined,
      exchange_rate: v.exchange_rate || undefined,
    };
    if (isAdminUser) {
      p.username = v.username || undefined;
      p.channel = v.channel || undefined;
    }
    return { values: v, params: p };
  };

  const buildQuery = (params) => {
    const s = new URLSearchParams();
    Object.entries(params).forEach(([k, val]) => {
      if (val !== undefined && val !== '' && val !== null) s.append(k, String(val));
    });
    return s;
  };

  const runQuery = async (targetPage) => {
    setLoading(true);
    try {
      const { params } = collectParams();
      const s = buildQuery(params);
      s.append('p', String(targetPage));
      s.append('page_size', String(PAGE_SIZE));
      const path = isAdminUser ? '/api/log/bill/summary' : '/api/log/self/bill/summary';
      const res = await API.get(`${path}?${s.toString()}`);
      if (res.data.success) {
        setData(res.data.data);
        setPage(targetPage);
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const runExport = async () => {
    try {
      const { values, params } = collectParams();
      const s = buildQuery(params);
      s.append('with_detail', values.with_detail ? '1' : '0');
      s.append('detail_split_model', values.with_detail && values.detail_split_model ? '1' : '0');
      const path = isAdminUser ? '/api/log/bill/export' : '/api/log/self/bill/export';
      const res = await API.get(`${path}?${s.toString()}`, { responseType: 'blob' });
      downloadBlobAsFile(new Blob([res.data]), `bill-summary-${Date.now()}.xlsx`);
    } catch (e) {
      showError(e.message);
    }
  };

  return (
    <div style={{ padding: 16 }}>
      <BillFilters
        formApiRef={formApiRef}
        isAdminUser={isAdminUser}
        onQuery={() => runQuery(1)}
        onExport={runExport}
        loading={loading}
        t={t}
      />
      {data && (
        <BillSummaryTable
          items={data.items}
          total={data.total}
          page={page}
          pageSize={PAGE_SIZE}
          summary={data.summary}
          isAdminUser={isAdminUser}
          onPageChange={(pg) => runQuery(pg)}
          t={t}
        />
      )}
    </div>
  );
};

export default BillSummary;
```

> 先读 `web/classic/src/helpers/index.js*` 确认 `API`, `isAdmin`, `showError` 都从 `../../../helpers` 导出（`isAdmin`/`downloadBlobAsFile` 已知在 `helpers/utils.jsx` 且经 helpers 索引再导出——若 `downloadBlobAsFile` 未从 `../../../helpers` 直出，则从 `../../../helpers/utils` 引入，如上）。若 `showError` 名称不同，用 classic 现有的错误提示函数（在 usage-logs hook 里查其用法）。

- [ ] **Step 4: i18n keys (classic zh.json)**

检查 `web/classic/src/i18n/locales/zh.json`，为上面用到的中文 key 补齐缺失项（classic 里 key 即中文，多数如「用户名」「模型名称」「导出汇总账单」「账单管理」「查询」「合计」「金额(美元)」「金额(人民币)」「汇率」「输入tokens」「输出tokens」「缓存读取tokens」「缓存创建tokens」「开始时间」「结束时间」「令牌名称」「渠道ID」「附带每日明细账」「明细分不同模型」可能已存在或需新增）。只补缺失的，勿造重复键，保持 JSON 合法。

- [ ] **Step 5: Build**

Run (in `web/classic/`): `bun run build` → 成功（含 Task 5 的菜单/路由/页面壳）。

- [ ] **Step 6: Commit (Tasks 5+6 together — coupled)**

```bash
git add web/classic/src/components/layout/SiderBar.jsx web/classic/src/App.jsx web/classic/src/pages/Bill/ web/classic/src/components/table/bill-summary/ web/classic/src/i18n/locales/zh.json
git commit -m "feat(bill): classic frontend bill management page

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Manual Verification

- [ ] 后端：`go test ./controller/ -run 'Bill|Finalize' -v` 全绿；`go build ./...` clean。
- [ ] 新版 `web/default`：`bun run build` 成功；账单页有「查询」按钮，点后筛选框下出现表格 + 分页 + 合计；管理员见用户名/渠道ID 列，普通用户不见；导出仍工作。
- [ ] 旧版 `web/classic`：`bun run build` 成功；侧边栏个人组出现「账单管理」→ `/console/bill`；查询填表 + 分页 + 合计；管理员/普通用户列差异；导出 Excel 工作。
- [ ] self 接口不泄露他人数据（普通用户查询只见本人）。

## Self-Review Notes

- **Spec 覆盖**：JSON 分页接口 + 全量合计 (Task 1)、路由 (Task 2)、新版 API (Task 3) + 表格/合计/分页 (Task 4)、旧版菜单/路由 (Task 5) + 表格/合计/分页/导出 (Task 6)、两端 i18n (Task 4/6)。均有任务。
- **类型一致性**：`buildBillSummaryPage` / `billSummaryItemDTO`(json: date,username,channel_id,token_name,model_name,amount_usd,exchange_rate,amount_cny,prompt_tokens,completion_tokens,cache_read_tokens,cache_creation_tokens) 与前端 `BillSummaryItem` 字段逐一对齐；`summary` 字段与 `BillSummaryTotals` 对齐。
- **耦合处理**：classic Task 5(路由引用组件) 与 Task 6(组件) 合并为一个提交单元，避免中间 build 断裂——已在步骤中标注。
- **YAGNI**：新版表格用轻量 `ui/table` 而非 data-table 全套；不排序切换；不物化表。
- **复用**：后端 100% 复用 Phase 1 聚合与流式函数；classic 复用 `downloadBlobAsFile`。
