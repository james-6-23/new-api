# 汇总账单 Phase 2 — 双端表格展示 + 旧版前端 设计方案

- 日期: 2026-07-01
- 状态: 已确认设计，待写实现计划
- 关联: 增量于 `2026-07-01-bill-summary-design.md`（Phase 1 导出功能已完成并在 prod 上）
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 2 (三库兼容)、Rule 3 (Bun)、Rule 5 (受保护标识)

## 1. 背景与目标

Phase 1 已实现「账单管理」的 Excel 汇总/明细导出（新版 `web/default`）。Phase 2 追加三项需求：

1. **旧版前端 `web/classic`（Semi Design）** 也加「账单管理」功能。
2. **两端** 在筛选框下面展示**汇总表格**（屏幕上可见的聚合结果）。
3. 表格数据即「需要导出的汇总数据」，导出按钮沿用 Phase 1 的 Excel 下载。

## 2. 已确认的关键决策

| 主题 | 决策 |
|---|---|
| 表格数据来源 | 后端**新增 JSON 汇总接口**，**分页返回**（现有导出端点只流式文件，无法填表）。 |
| 表格 vs 导出 | 「查询」拉 JSON 填表格；「导出 Excel」用同一套筛选条件完整下载（不受分页限制）。两者解耦。 |
| 旧版菜单 | 独立页面/菜单 `/console/bill`「账单管理」，放 personal 组。 |
| 金额展示 | 表格同时展示美元 + 人民币两列 + 汇率列（与 Excel 列对齐）。 |
| 合计 | JSON 接口除分页 items 外返回**全量合计**（所有组的总美元/人民币/各 tokens），表格固定展示。 |
| 聚合内存 | 每请求流式聚合全量后切片当前页，受现有 `LogExportMaxRows`(~1M 行) 约束；不建物化表 (YAGNI)。 |

## 3. 后端：新增分页 JSON 汇总接口

### 3.1 新增文件
- `controller/bill_summary_query.go`

### 3.2 路由（`router/api-router.go`，logRoute 组内，紧跟 Phase 1 两条 bill 路由后）
```
logRoute.GET("/bill/summary", middleware.AdminAuth(), controller.QueryBillSummaryAll)
logRoute.GET("/self/bill/summary", middleware.UserAuth(), controller.QueryBillSummarySelf)
```

### 3.3 Query 参数
`start_timestamp`、`end_timestamp`、`username`(admin)、`channel`(admin)、`token_name`、`model_name`、`group`、`exchange_rate`(可选)、`p`(页码，默认1)、`page_size`(默认20，clamp 上限如100)。self 版忽略 username/channel，用 `c.GetInt("id")`。

### 3.4 复用与算法
- 复用 Phase 1 的 `newBillSummaryAgg()` / `addBatch()` / `sortedKeys()`（`controller/bill_summary.go`）。
- 复用 `model.GetAllLogsForExport` / `GetUserLogsForExport`（固定 `model.LogTypeConsume`，maxRows = `model.LogExportMaxRows("xlsx")`）流式扫描并聚合。
- 聚合后 `sortedKeys()`，按 `(p-1)*page_size` 起切 `page_size` 个 key 组装当前页 items。
- 全量合计：遍历所有组累加（美元/人民币/各 tokens）。
- 金额：USD = `float64(Quota)/common.QuotaPerUnit`；rate = `exchange_rate` 入参>0 时覆盖，否则 `operation_setting.USDExchangeRate`（只读局部，不改全局）；CNY = USD×rate；USD/CNY 用 `roundTo6`。

### 3.5 返回结构（经 `common` gin helper 输出）
```json
{ "success": true, "data": {
  "items": [ {
    "date": "2026-06-01", "username": "alice", "channel_id": 3,
    "token_name": "tk", "model_name": "gpt-4o",
    "amount_usd": 0.003, "exchange_rate": 7.3, "amount_cny": 0.0219,
    "prompt_tokens": 12, "completion_tokens": 6,
    "cache_read_tokens": 5, "cache_creation_tokens": 3
  } ],
  "total": 128, "page": 1, "page_size": 20,
  "summary": {
    "total_amount_usd": 1.23, "total_amount_cny": 8.98,
    "total_prompt_tokens": 1000, "total_completion_tokens": 500,
    "total_cache_read_tokens": 40, "total_cache_creation_tokens": 12
  }
} }
```
- JSON 序列化经 gin `c.JSON`（项目 gin helper），DTO 用 struct tag。业务层如需 Marshal 走 `common.Marshal`（Rule 1）。

### 3.6 DTO
在 `dto/` 或 `controller` 内定义 `BillSummaryItem` / `BillSummaryTotals` / `BillSummaryResponse` struct（json tag 同上）。字段为展示用，非 relay 上行 DTO，Rule 6 不适用。

## 4. 新版前端 `web/default`（TanStack + Base UI）

### 4.1 api.ts 增量
`features/bill-management/api.ts` 新增：
```ts
export interface BillSummaryItem { date: string; username: string; channel_id: number; token_name: string; model_name: string; amount_usd: number; exchange_rate: number; amount_cny: number; prompt_tokens: number; completion_tokens: number; cache_read_tokens: number; cache_creation_tokens: number }
export interface BillSummaryTotals { total_amount_usd: number; total_amount_cny: number; total_prompt_tokens: number; total_completion_tokens: number; total_cache_read_tokens: number; total_cache_creation_tokens: number }
export interface BillSummaryResponse { items: BillSummaryItem[]; total: number; page: number; page_size: number; summary: BillSummaryTotals }
export async function getBillSummary(params, isAdmin, page, pageSize): Promise<BillSummaryResponse>
```
普通 axios GET（非 blob），admin→`/api/log/bill/summary`，self→`/api/log/self/bill/summary`。

### 4.2 页面 `bill-export-page.tsx` 增量
- 两个按钮：**查询**（拉 JSON 填表格，重置到第 1 页）与 **导出 Excel**（Phase 1 blob 下载），共用筛选 state。
- 筛选框**下面**渲染汇总表格（12 列）+ 分页器（翻页重新请求）+ 合计展示（来自 `summary`）。
- 非管理员隐藏用户名/渠道ID 筛选项与列（`useIsAdmin`）。
- 表格用现有表格 UI（对齐 usage-logs 的展示风格；简洁即可，不过度封装）。

### 4.3 i18n
`web/default/src/i18n/locales/zh.json` 补列头等新 key（英文 key）：如 `Date`、`Amount (USD)`、`Amount (CNY)`、`Exchange Rate`、`Prompt Tokens`、`Completion Tokens`、`Cache Read Tokens`、`Cache Creation Tokens`、`Query`、`Total`（已存在的不重复）。

## 5. 旧版前端 `web/classic`（Semi Design / React 18）

### 5.1 菜单 `components/layout/SiderBar.jsx`
- `routerMap` 加 `bill: '/console/bill'`。
- personal 组（`text: t('个人设置')` 附近）加 `{ text: t('账单管理'), itemKey: 'bill', to: '/bill' }`，遵循同组现有 `isModuleVisible` 模式。

### 5.2 路由 `App.jsx`
- import `Bill from './pages/Bill'`。
- 加 `<Route path='/console/bill' element={<PrivateRoute><Bill /></PrivateRoute>} />`（对齐 `/console/log` 写法）。

### 5.3 页面与组件
- `pages/Bill/index.jsx` — 页面壳，渲染表格组件。
- `components/table/bill-summary/`：
  - `BillFilters.jsx` — Semi `Form`：时间范围、用户名/渠道ID(仅 admin)、令牌、模型、汇率、附带明细开关、明细分模型开关、查询按钮、导出按钮。
  - `BillSummaryTable.jsx` — Semi `Table` + 分页 + 合计行（美元/人民币/tokens）。
  - `index.jsx` — 组合 filters + table。
  - `hooks/bill-summary/useBillSummaryData.jsx`（或就近放 hook）— `API.get` 拉 JSON、分页 state、导出用 `downloadBlobAsFile`。
- 复用：`API`（`../../helpers`）、`isAdmin()`、`downloadBlobAsFile()`（均在 `helpers/utils.jsx`）。
- 导出：GET blob 到 `/api/log/bill/export` 或 `/self/bill/export`（admin/self），`downloadBlobAsFile` 保存。

### 5.4 i18n
`web/classic/src/i18n` 语言文件（key 为中文）补 `账单管理` 等相关中文 key（若缺失）。

### 5.5 版权头
所有新增 `.jsx` 文件复制 classic 现有版权头（含受保护 QuantumNous / new-api 标识，Rule 5，不得改动）。

## 6. 测试策略

- **后端单测**（`controller/bill_summary_query_test.go`）：
  - 分页切片：构造 N 组，断言 page/page_size 切片与 total 正确。
  - 全量合计：断言 summary 各字段 = 所有组之和（与分页无关）。
  - self 锁本人：self 路径忽略 username/channel。
  - 汇率覆盖：`exchange_rate` 入参优先。
  - 复用 Phase 1 的 `tsOn` 等 helper。
- **前端**：两端 build 通过；手动验证菜单出现、查询填表、分页、合计、导出、管理员/普通用户列差异。

## 7. 范围外 (YAGNI)

- 不建物化汇总表 / 定时任务（沿用流式聚合）。
- 不做表格内联图表 / 排序切换（保持与导出一致的固定排序）。
- 不改 Phase 1 已交付的导出逻辑（仅新增查询接口与表格展示）。
