# 汇总账单功能 — 设计方案 (Bill Summary Export)

- 日期: 2026-07-01
- 状态: 已确认设计，待写实现计划
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 2 (三库兼容)、Rule 3 (Bun)、Rule 5 (受保护标识)

## 1. 背景与目标

在「个人中心 (Personal)」下新增「账单管理 (Bill Management)」菜单，支持按**时间范围、用户名、渠道ID**（以及令牌名、模型名）查询用量，并导出**汇总账单 Excel**：

- 汇总账单按**模型名称做单日汇总**，每行是 `(日期, 用户名, 渠道ID, 令牌名称, 模型名称)` 组合的单日聚合。
- Excel 列：日期、用户名、渠道ID、令牌名称、模型名称、汇总金额(美元)、汇率、汇总金额(人民币)、输入tokens、输出tokens、缓存读取tokens、缓存创建tokens。
- 可选「附带每日明细账」，明细「按日期分 Sheet，模型分区块」。
- 明细行数超出 Excel 单 sheet 上限时自动新增 sheet。

## 2. 已确认的关键决策

| 主题 | 决策 |
|---|---|
| 权限范围 | 管理员 + 普通用户自助。普通用户后端强制锁定本人 `user_id`，忽略 `username`/`channel` 入参；管理员可查任意用户。 |
| 汇率来源 | 全局 `operation_setting.USDExchangeRate`（默认 7.3），**导出参数 `exchange_rate` 可覆盖**；导出时取一次快照，每行写同一汇率。 |
| 明细组织 | 按日期分 Sheet，Sheet 内按模型排序并分区块（每个模型块前插一行「模型：xxx」标题行）。 |
| 汇总金额口径 | 累加 `Log.Quota`（实扣）换算美元，**不重算倍率**。tokens 分列汇总。 |
| 缓存列 | 拆两列：「缓存读取tokens」(`cache_tokens`) 与「缓存创建tokens」(`cache_creation_tokens` + `_5m` + `_1h` 合计)。 |
| 汇总粒度 | `日期 + 用户 + 渠道 + 令牌 + 模型`。 |
| 实现方案 | 方案 A：单次流式扫描 + Go 内存聚合（复用现有 `streamLogsForExport`）。 |

## 3. 技术约束（探索结论）

1. **缓存 tokens 不是数据库列**，存在 `Log.Other` JSON blob 里（`cache_tokens` / `cache_creation_tokens` / `cache_creation_tokens_5m` / `cache_creation_tokens_1h`）。因此纯 SQL `GROUP BY` 无法汇总缓存 —— 这是选方案 A 的决定性原因。
2. 已存在成熟的流式导出基建 `controller/log_export.go`：
   - `model.streamLogsForExport(buildBase, maxRows, consume)` —— keyset 分页 (`created_at DESC, id DESC`)，跨三库一致。
   - `excelSheetWriter` —— 按日历日滚动 sheet，超 `excelSingleSheetSoftCap`(100万) 自动 `(2)(3)`。
   - `getCacheTokensFromOther` / `getCacheCreationTokensFromOther` —— 从 Other JSON 提取缓存 tokens。
   - `X-Export-Truncated` / `X-Export-Max-Rows` 截断响应头。
3. 金额换算：美元 = `float64(Quota) / common.QuotaPerUnit`；人民币 = 美元 × 汇率。
4. 归日一律用**服务器本地时区** `time.Unix(CreatedAt, 0).Format("2006-01-02")`，保证汇总与明细「单日」定义一致。

## 4. 方案选型

| 方案 | 做法 | 取舍 |
|---|---|---|
| **A（采用）** | 复用 `streamLogsForExport`，逐批把行按汇总 key 累加进 `map`；同批可同时喂给明细 writer | 复用基建；天然处理 JSON 缓存；内存 = 汇总组数（远小于行数）；跨库一致；一次扫描同时产出汇总+明细 |
| B | SQL GROUP BY 聚合 quota/tokens + 缓存 Go 补扫 | 两条路径两次扫描；`GROUP_CONCAT`/`STRING_AGG` 跨库分支，复杂 |
| C | 物化每日汇总表 | 过度设计 (YAGNI)；一致性维护负担 |

## 5. 后端设计

### 5.1 新增文件

- `model/bill_summary.go` —— 聚合结构 + 流式聚合函数。
- `controller/bill_summary_export.go` —— Excel 组装 + HTTP handler。
- 路由注册于 `router/api-router.go` 现有 logRoute 组。

### 5.2 路由（对齐现有 self/admin 双路由）

```
GET /api/log/self/bill/export   middleware.UserAuth()   // 普通用户，强制锁本人 user_id
GET /api/log/bill/export        middleware.AdminAuth()  // 管理员，可按 username / channel
```

- 复用 `common.LogExportEnabled` 开关门禁（与 `ExportUserLogs` 一致）。普通用户路由在功能关闭时返回 403。

### 5.3 Query 参数（对齐现有导出，前端复用序列化器）

`start_timestamp`、`end_timestamp`、`username`（admin）、`channel`（admin）、`token_name`、`model_name`、`group`、`with_detail=0|1`、`detail_split_model=0|1`、`exchange_rate=<float>`（可选）。

普通用户路由忽略 `username`/`channel`，一律用 `c.GetInt("id")`。

### 5.4 聚合核心 (`model/bill_summary.go`)

```go
type BillSummaryKey struct {
    Day       string // "2006-01-02"，服务器本地时区
    Username  string
    ChannelId int
    TokenName string
    ModelName string
}

type BillSummaryRow struct {
    Quota               int // 累加 Log.Quota（实扣）
    PromptTokens        int
    CompletionTokens    int
    CacheReadTokens     int // 累加 Other.cache_tokens
    CacheCreationTokens int // 累加 Other.cache_creation(+5m+1h)
}
```

- `buildBase` 复制 `GetAllLogsForExport` 的 filter 链，**固定 `type = LogTypeConsume`**（只汇总消费日志）。
- 普通用户版加 `user_id = ?`。
- `consume` 回调逐行累加进 `map[BillSummaryKey]*BillSummaryRow`；缓存 tokens 复用现有两个 helper。
- `maxRows` 用于**明细行数**上限（汇总组数本身远小于行数，但仍复用同一扫描的截断标记）。
- 若 `with_detail=1`，同批行同时交给明细 writer，**无需二次扫描**。

导出函数签名（示意）：

```go
// 返回聚合结果 + 是否截断；若 withDetail，detailConsume 会被逐批调用
func StreamBillSummary(
    base BillSummaryFilter, maxRows int,
    detailConsume func([]*Log) error, // nil 表示不导出明细
) (rows map[BillSummaryKey]*BillSummaryRow, truncated bool, err error)
```

### 5.5 金额与汇率

- 美元 = `float64(row.Quota) / common.QuotaPerUnit`。
- 汇率 = `exchange_rate` 入参优先，否则 `operation_setting.USDExchangeRate`，导出时取一次快照。
- 人民币 = 美元 × 汇率（写入时保留 6 位小数，与现有 `formatPrice` 一致）。

## 6. Excel 文件结构

### 6.1 汇总 Sheet（必有，首个 sheet「汇总账单」）

列顺序（12 列，缓存拆两列）：

| 日期 | 用户名 | 渠道ID | 令牌名称 | 模型名称 | 汇总金额(美元) | 汇率 | 汇总金额(人民币) | 输入tokens | 输出tokens | 缓存读取tokens | 缓存创建tokens |
|---|---|---|---|---|---|---|---|---|---|---|---|

- 行 = 一个 `(日期,用户,渠道,令牌,模型)` 组合的单日汇总。
- 排序：日期 DESC → 用户名 → 渠道ID → 模型名。
- 末尾追加**合计行**：美元/人民币/各 tokens 总和。
- 汇总组数超单 sheet 软上限(100万) 时滚动为「汇总账单 (2)」「汇总账单 (3)」。
- 金额/tokens 以数字类型写入（Excel 可求和），美元/人民币保留 6 位小数。

### 6.2 每日明细 Sheets（`with_detail=1` 时追加）

- 每个日历日一个 sheet（名 = `2026-06-01`），复用 `excelSheetWriter` 的按日滚动 + 超行 `(2)(3)` 逻辑。
- `detail_split_model=1`（「分不同模型」）：sheet 内按模型名排序，每个模型区块前插一行浅色「模型：xxx」标题行，块内逐条明细。
- 明细列复用现有明细导出列：时间、用户、令牌、分组、模型、输入、输出、缓存读取、缓存创建、费用、计费过程、请求ID。
- 某天明细超单 sheet 上限 → `2026-06-01 (2)`（现有逻辑天然支持）。

> 注意：按模型分区块 + 按日滚动，要求明细行仍按 `created_at DESC` 到达；模型分组在**单个 sheet 内**做二次排序缓冲（先收集当天全部行，再按模型排序写出）。当天行数极大时以 `excelSingleSheetSoftCap` 为界分片。实现时该缓冲上限与内存权衡在实现计划中细化。

### 6.3 截断处理

沿用 `X-Export-Truncated` / `X-Export-Max-Rows` 响应头；前端 toast 提示缩小范围。空结果集仍产出仅含表头的有效 xlsx。

## 7. 前端设计 (`web/default`, TanStack Router + Base UI + Tailwind)

### 7.1 菜单

在 `hooks/use-sidebar-data.ts` 现有 `personal` 组内，Wallet/Profile 旁新增：

```ts
{ title: t('Bill Management'), url: '/bill-management', icon: ReceiptText }
```

### 7.2 路由与目录

- `routes/_authenticated/bill-management/index.tsx` —— route 文件。
- `features/bill-management/`：
  - `api.ts` —— blob 下载触发。
  - `components/bill-export-page.tsx` —— 页面主体。
  - `components/bill-filter-bar.tsx` —— 筛选表单。

### 7.3 表单字段

- 时间范围（复用 `compact-date-time-range-picker.tsx`）。
- 用户名 / 渠道ID（**仅管理员显示**，用现有权限 hook 判断）。
- 令牌名称、模型名称（选填）。
- 汇率（选填，占位符显示当前全局汇率）。
- 开关：☑ 附带每日明细账 · ☑ 明细分不同模型（仅在前者勾选时可用）。
- 「导出 Excel」按钮。

### 7.4 下载实现

`api.get(url, { responseType: 'blob' })` → `URL.createObjectURL` → 触发 `<a download>`；读 `X-Export-Truncated` 头决定 toast。管理员走 `/api/log/bill/export`，普通用户走 `/api/log/self/bill/export`。

### 7.5 i18n

新增 key（`Bill Management` 及字段标签），补 `web/default/src/i18n/locales/zh.json`（en 为 base）。

## 8. 测试策略

- **后端单测**（Go）：
  - 聚合正确性：构造多条 Log（同/异日、同/异模型、含 Other 缓存），断言 map 聚合金额/tokens 正确。
  - 缓存 JSON 解析：`cache_tokens` 与 `cache_creation(+5m+1h)` 分列求和。
  - 权限：普通用户路由忽略 username/channel，锁定本人。
  - 汇率覆盖：`exchange_rate` 入参优先于全局。
  - 三库兼容：filter 链走 GORM 抽象，无库特有 SQL。
- **Excel 结构**：小样本导出解析回读，断言列顺序、合计行、明细分 sheet/模型分区块、超软上限滚动 sheet。
- **前端**：手动验证菜单出现、管理员/普通用户字段差异、下载触发、截断 toast。

## 9. 范围外 (YAGNI)

- 不做汇总物化表 / 定时任务。
- 不做 CSV 汇总格式（仅 Excel；如需可后续复用现有 CSV writer）。
- 不做在线预览 / 图表；仅导出。
