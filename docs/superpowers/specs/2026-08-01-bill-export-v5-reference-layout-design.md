# 账单导出 v5 — 对齐参考账单版式：请求数/计费记录、精确刊例价金额、阶梯计费刊例价修复、汇总封面与三维汇总表

- 日期: 2026-08-01
- 状态: 已实现（2026-08-01，含测试）
- 前置: `2026-07-30-bill-export-v2-design.md`、`2026-07-30-bill-export-v3-granularity-task-billing-design.md`、`2026-07-30-bill-export-v4-listprice-ratio-design.md`
- 参考件: `接口文档/账单功能文档/Modex-AI_2026-07-20_至_2026-07-31_账单详情.xlsx`
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 2 (三库兼容，本次无 DB 改动)、Rule 5 (受保护标识)、Rule 7 (已读 `pkg/billingexpr/expr.md`)

## 1. 需求

1. **请求数**：账单导出与账单查询需要有请求数（及计费记录数）统计，对齐参考账单的「计费记录 / 请求数」两列。
2. **修复刊例价金额不对**：
   - 阶梯计费（`billing_mode=tiered_expr`）模型的日志 `Other` 中 `model_ratio=0`、`model_price=0`，导出侧 `capturePricing` 三分支全不命中 → 刊例价留空；若模型历史上用过倍率计费还会捕获到切换前旧价。
   - 「首见值胜出」的单价与跨调价聚合的金额不匹配；单价×总量无法还原金额（输出/缓存价不同、单位混装），客户复算对不上。
   - 修复方式：a) 补 tiered 单价解析分支；b) 新增**精确「刊例价金额」列**（见 §3.2）。
3. **对齐参考账单版式**：新增「账单汇总」封面 sheet 与 按日汇总 / 按令牌汇总 / 按模型汇总 三个 sheet。

## 2. 导出工作簿最终结构（bill summary 导出，`runBillExport`）

Sheet 顺序（`finalizeBillWorkbook` 保证）：

1. **账单汇总**（新增，封面）
2. **总对账单**（既有，加列）
3. **明细对账单**（既有，加列）
4. **按日汇总**（新增）
5. **按令牌汇总**（新增）
6. **按模型汇总**（新增）
7. `with_detail=1` 时的逐日明细 sheets（既有，不改动）

## 3. 核心口径定义

### 3.1 请求数 / 计费记录

任务型计费一次请求产生 pre_consume + settle/refund 多行（`service/task_billing.go`），直接数消费行会重复。定义（流式 O(1) 累加，无去重集合）：

- **计费记录** = 组内（消费 + 退款）日志行数。
- **请求数** = 组内 `type=消费` 且 `Other.billing_stage != "settle"` 的行数。
  - 普通请求：1 行消费 → 请求数 1。
  - 任务：pre_consume 1 行计入；settle 补扣行（消费类型、stage=settle）不计；refund 行不计。
  - 旧日志无 `billing_stage` 键 → 视为普通消费，计入。

### 3.2 刊例价金额（精确，逐条反推）

所有计费模式下 `log.Quota = 刊例价成本 × 生效分组倍率`（token 路径 `tokens×ratio×倍率`；按次 `price×QuotaPerUnit×倍率`；tiered `rawCost/1e6×QuotaPerUnit×倍率`；请求规则加价 `|||when(...)×N` 属于刊例的一部分，包含在内）。因此逐条日志：

```
effRatio  = user_group_ratio 有效(非 -1、>0) ? user_group_ratio : group_ratio
listQuota = quota / effRatio        （effRatio > 0）
listQuota = quota                   （无倍率键的旧日志，视 effRatio=1）
listQuota = 0                       （effRatio == 0，免费分组，quota 本身为 0）
```

- 退款行同样反推并**冲抵**（listQuota 取负累加）。
- 组内以 float64 累加 quota 单位，落表时 `/QuotaPerUnit` 后 `roundTo6`，样式与「汇总金额(美元)」一致（`0.000000` 数字格式）。
- 该列逐行满足 `刊例价金额 × 专属倍率 ≈ 汇总金额(美元)`（免费/旧日志行除外），客户可直接复算。

### 3.3 刊例价（单价列）修复 — tiered 分支

`logPricingInfo`（`controller/log_export.go`）新增字段：`BillingMode string json:"billing_mode"`、`ExprB64 string json:"expr_b64"`、`MatchedTier string json:"matched_tier"`。

`capturePricing` 捕获顺序调整为：

1. `billing_mode == "tiered_expr"`：base64 解码 `expr_b64`，用 Go 版 tier 解析（见 §4.3）取 `matched_tier` 匹配档（找不到或键缺失时取第一档）的 `p` 系数。表达式系数即真实 $/1M 价（expr.md 原则 3），**不乘 2**。解析失败 → 不设 hasPrice，继续等组内其他日志（不回退到 model_ratio，避免旧价污染）。
2. `video_unit_price > 0`（不变）
3. `model_price > 0`（不变）
4. `model_ratio > 0` → `×2`（不变）

「首见值（最新值）胜出」策略保留（v4 决策）；价与量的对账职责由 §3.2 的刊例价金额列承担。

### 3.4 聚合数据结构

- 既有 `billSummaryAgg`/`billSummaryRow` 扩展：`BillingRecords int`、`RequestCount int`、`ListQuota float64`（刊例价金额累加，quota 单位）。总对账单派生聚合（`billGrandKey`）同步累加这三项。
- 新增独立参考维度聚合 `billRefAgg`（不侵入既有 external 合并逻辑）：
  - key = `(day, username, tokenName, modelName)`，**始终保留 token 维度**（external 模式的明细对账单继续丢弃 token，不受影响）。
  - value = 计费记录、请求数、输入/输出/缓存读取/缓存创建 tokens、Quota（净额）、ListQuota、firstTs/lastTs（`min/max created_at`）。
  - 按日 / 按令牌 / 按模型三表由该 map 折叠派生，一次遍历完成。

## 4. Sheet 明细设计

### 4.1 账单汇总（封面）

键值对区块（A 列标签、B 列值，部分两列一组），全部为既有信息的汇总，不引入新口径：

- **结算信息**：客户（admin 导出取 `username` 筛选值，空则「全部用户」；self 导出取当前用户名）、时区（服务器本地时区名）、结算开始/结束（导出参数时间，秒级「2006-01-02 15:04:05」；未传则用实际首末笔）、边界（包含）、生成时间。
- **用量与计数**：首笔计费 / 末笔计费（实际日志 min/max）、计费记录、请求数、输入 tokens、输出 tokens、缓存读取 tokens、缓存创建 tokens、Quota Units（净额合计）。
- **金额**：金额口径（`USD = quota_units / {common.QuotaPerUnit 实际值}`）、刊例价金额(美元)、账单金额(美元)、汇率(CNY/USD)、账单金额(人民币)。
- **说明**：筛选条件回显（分组/模型/令牌/渠道，非空才列）；若触发行数上限截断，追加「数据已按上限 N 行截断，金额仅覆盖已导出行」。

### 4.2 各数据表列布局

新增三列统一命名：「计费记录」「请求数」「刊例价金额(美元)」。

- **总对账单** external：`开始日期, 结束日期, 用户名, 模型名称, 计费记录, 请求数, 刊例价金额(美元), 汇总金额(美元), 汇率, 汇总金额(人民币), 输入tokens, 输出tokens, 缓存读取tokens, 缓存创建tokens`；internal 在「用户名」后加 `渠道ID`。
- **明细对账单** external：`日期, 用户名, 模型名称, 刊例价, 专属倍率, 计费记录, 请求数, 刊例价金额(美元), 汇总金额(美元), 汇率, 汇总金额(人民币), 输入tokens×4`；internal 在「用户名」后加 `渠道ID, 令牌名称`。
- **按日汇总**：`日期, 计费记录, 请求数, 输入tokens, 输出tokens, 缓存读取tokens, 缓存创建tokens, Quota Units, 刊例价金额(美元), 汇总金额(美元)`。日期恒为自然日（不受 `granularity` 参数影响）。
- **按令牌汇总**：`[用户名(仅 internal)], 令牌名称, 计费记录, 请求数, 输入tokens, 输出tokens, 缓存读取tokens, 缓存创建tokens, Quota Units, 刊例价金额(美元), 汇总金额(美元), 首笔计费时间, 末笔计费时间`。
- **按模型汇总**：同按令牌，首列换 `[用户名(仅 internal)], 模型名称`。
- 时间戳一律 `2006-01-02 15:04:05` 字符串；金额列沿用 `0.000000` 数字格式；tokens/计数为原生整数；Quota Units 为原生整数（净额，退款已冲抵）。
- 排序：按日汇总日期 DESC；按令牌/按模型按 Quota 净额 DESC（金额大者在前，对齐客户关注点）；同额按名称 ASC。

### 4.3 Go 版 tier 解析（放入 `pkg/billingexpr`）

新增 `pkg/billingexpr/tiers.go`：`ParseTiers(exprStr string) []ParsedTier`（`ParsedTier{Label string; Prices map[string]float64}`）+ `MatchTier(tiers, label) *ParsedTier`。移植前端 `web/default/src/features/pricing/lib/billing-expr.ts` 的正则解析（strip `v1:` 前缀、strip `|||` 请求规则段、按 `tier("label", body)` 提取、body 内按变量系数正则取价），行为与前端 `parseTiersFromExpr` 保持一致，两处注释互相引用防漂移。导出侧只用 `p` 系数（输入 $/1M）。

## 5. 网页账单查询（次要项）

`billSummaryPageDTO`（`controller/bill_summary_query.go`）items 与 totals 增加 `billing_records`、`request_count`、`list_amount_usd` 字段（数据来自同一聚合，零额外查询）。前端账单页（web/default 账单查询表格）追加「请求数」「刊例价金额」两列并补 6 语言 i18n key。不改既有列。

## 6. 不做的事

- 不改 DB schema、不加索引（继续流式 keyset 扫描，Rule 2 无风险）。
- 不改逐日明细 sheets（`bill_detail_excel.go`）与逐行日志导出（`ExportAllLogs`/`ExportUserLogs`）。
- 不实现参考件中的人工对账区（生产汇总比对/PASS 状态/汇率确认状态）——那是对账动作的产物，不是导出器职责。
- 不动受保护项目标识（Rule 5）。

## 7. 改动点清单

| 文件 | 改动 |
|---|---|
| `controller/log_export.go` | `logPricingInfo` 加 `billing_mode/expr_b64/matched_tier` 三字段 |
| `controller/bill_summary.go` | `billSummaryRow` 加计数/ListQuota；`addBatch` 累加；`capturePricing` 加 tiered 分支；新增 `billRefAgg` 及派生折叠 |
| `controller/bill_summary_excel.go` | 两既有布局插三列；新增封面 sheet 与三个汇总 sheet 的写出 |
| `controller/bill_summary_export.go` | `runBillExport` 串接新聚合与新 sheet；封面所需参数（筛选回显、截断标志）传递 |
| `controller/bill_summary_query.go` | DTO 加三字段 |
| `pkg/billingexpr/tiers.go`（新增） | Go 版 tier 解析 + 单测 |
| `web/default/src/features/...`（账单查询页） | 两列 + i18n×6 |
| 测试 | 见 §8 |

## 8. 测试计划（TDD）

1. `pkg/billingexpr`：单档/多档/带条件/带 `|||` 请求规则/带 `v1:` 前缀/非法表达式的解析；matched_tier 命中与缺省取首档。
2. `capturePricing`：tiered 日志取档位 p 价不乘 2；tiered 解析失败不回退旧 model_ratio；既有 video/per-call/ratio 分支回归。
3. 计数：普通消费、任务 pre+settle+refund 三行 → 计费记录 3 / 请求数 1；旧日志无 stage 键计入请求数。
4. 刊例价金额：user_group_ratio 优先、-1 回退 group_ratio、ratio=0、旧日志无键视 1、退款冲抵为负。
5. 三个新汇总 sheet 与封面：布局、数值、时间戳格式、排序的落盘往返断言；external/internal 两模式。
6. 既有测试列位移更新（总对账单/明细对账单）。
7. 验证命令：`go build ./...`；`go test ./controller/ ./pkg/billingexpr/`；前端 `bun run build`。

## 9. 分阶段实施

1. **Phase 1**：`pkg/billingexpr` tier 解析（独立、可先行 TDD）。
2. **Phase 2**：`capturePricing` tiered 修复 + 计数/刊例价金额聚合（bill_summary.go）。
3. **Phase 3**：既有两 sheet 加列 + 三个新汇总 sheet + 封面（excel/export 层）。
4. **Phase 4**：查询 DTO + 前端列 + i18n。
5. 每阶段独立提交，测试通过后进入下一阶段。
