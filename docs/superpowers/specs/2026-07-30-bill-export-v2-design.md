# 账单导出 v2 — 总/明细对账单分层 + 内外口径 + 数字单元格 (Bill Export v2)

- 日期: 2026-07-30
- 状态: 已确认设计，随本次实现落地
- 前置: `2026-07-01-bill-summary-design.md`(汇总账单 v1)
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 2 (三库兼容——本次不触 SQL)、Rule 3 (Bun)、Rule 5 (受保护标识)

## 1. 背景与需求

v1 汇总账单同一天同一模型会因**渠道不同**出现多行，对外给客户对账时视觉上"模型重复"；
金额/汇率列以字符串写入 Excel（文本格式，无法求和）；汇总 sheet 排在每日明细之后。

本次三点优化：

1. **汇总账单分层 + 内外两种口径**
   - 新增「总对账单」sheet：覆盖**整个导出日期范围**（含开始日期/结束日期列），跨日聚合。
   - 原「汇总账单」更名「**明细对账单**」：仍按日聚合（v1 行为）。
   - 导出口径 `bill_mode`：
     - `external`（对外客户）：**不需要渠道**，同日同模型合并为一行（渠道、令牌维度均并掉，否则仍会出现模型重复行）；sheet 无「渠道ID」「令牌名称」列。
     - `internal`（内部，默认）：**分渠道分模型**，明细对账单保持 v1 的 `(日期,用户,渠道,令牌,模型)` 粒度与列。
2. **数字单元格**：汇总金额(美元)、汇率、汇总金额(人民币)及所有 token 数、渠道ID 一律写为原生数字（非文本）；金额列挂 `0.000000` 数字格式；每日明细 sheet 的「费用」列改为数字（`"$"0.000000` 格式保留 $ 显示）。**删除「合计」行**（数字化后 Excel 可直接求和，且总对账单已承担总计职能）。
3. **Sheet 顺序**：`总对账单` → `明细对账单` → 每日使用明细（日期 DESC）。激活 sheet = 总对账单。

## 2. 关键决策

| 主题 | 决策 |
|---|---|
| 口径参数 | 新 query 参数 `bill_mode=internal\|external`；缺省 `internal`（向后兼容 v1 行为）。 |
| external 聚合键 | 明细对账单 `(日期,用户名,模型)`；总对账单 `(用户名,模型)`。渠道与令牌维度都合并——只并渠道的话多令牌仍会造成模型重复行。 |
| internal 聚合键 | 明细对账单 `(日期,用户名,渠道,令牌,模型)`（=v1）；总对账单 `(用户名,渠道,模型)`（总表不细分令牌）。 |
| 总对账单日期范围 | 取**实际数据**的最小/最大归账日（非查询入参，入参可能为空/开放区间），每行写「开始日期」「结束日期」两列。 |
| 数字格式 | 金额 float64 + `CustomNumFmt "0.000000"`；汇率 float64 无格式（7.3 原样显示）；tokens/渠道ID 为 int；明细费用 `"$"0.000000`。StreamWriter 对 float64/int 写原生数字单元格。 |
| 合计行 | 全部移除（汇总/总对账单均不再追加）。 |
| Sheet 重排 | 汇总类 sheet 在流式明细之后才生成；收尾用 excelize `MoveSheet`（v2.10.1 起可用）把「总对账单*」「明细对账单*」按序挪到最前。 |
| 滚动 sheet | 保留 `excelSingleSheetSoftCap` 滚动 `(2)(3)` 逻辑（总对账单组数极少，但代码统一）。 |
| 每日明细 sheets | 列结构不变（本就无渠道列，不受口径影响），仅费用列数字化。 |
| 网页查询表格 | 不动（仍 internal 粒度）；本次只改导出。 |
| 通用日志导出 | `ExportAllLogs`/`ExportUserLogs`（日志页导出）不动，费用数字化仅在账单明细 writer 内覆写，避免影响 CSV 与旧行为。 |

## 3. 后端改动

- `controller/bill_summary.go`
  - `billSummaryAgg` 增加 `external bool`；`addBatch` 在 external 时将 key 的 `ChannelId`/`TokenName` 归零合并。
- `controller/bill_summary_excel.go`（重写）
  - 常量 `billGrandSheetPrefix = "总对账单"`、`billDailySheetPrefix = "明细对账单"`。
  - `billSheetLayout{prefix, headers, widths}` 按口径给出两套列布局；通用滚动写入器 `writeBillRowsSheet`。
  - `writeBillSummarySheets(f, agg, rate)`：由日聚合派生总对账单（累加原始 quota 再换算，min/max 日期），先写总对账单、再写明细对账单；金额单元格 `excelize.Cell{Value: float64, StyleID: money}`。
- `controller/bill_detail_excel.go`：费用列改写为 `excelize.Cell{Value: usd float64, StyleID: "$"0.000000}`。
- `controller/bill_summary_export.go`
  - 解析 `bill_mode`；`finalizeBillWorkbook` 改为：删 Sheet1 → `MoveSheet` 把汇总类 sheet 按创建序挪到最前 → 激活总对账单。

### 列布局

总对账单 internal：`开始日期 结束日期 用户名 渠道ID 模型名称 汇总金额(美元) 汇率 汇总金额(人民币) 输入tokens 输出tokens 缓存读取tokens 缓存创建tokens`
总对账单 external：同上去掉「渠道ID」。
明细对账单 internal（=v1 列）：`日期 用户名 渠道ID 令牌名称 模型名称 …同上金额/tokens 列`。
明细对账单 external：`日期 用户名 模型名称 …同上金额/tokens 列`。

## 4. 前端改动（两套主题）

- `web/default` 账单管理页：新增「导出口径」下拉（内部（分渠道分模型）/ 对外客户（合并渠道）），默认内部；`api.ts` `BillExportParams` 增加 `bill_mode`。
- `web/classic` `BillFilters.jsx`：新增 `Form.Select bill_mode`（默认 internal）；`index.jsx` 导出时附带该参数。
- i18n：`web/default/src/i18n/locales/zh.json` 增加 `Bill mode` / `Internal (split by channel & model)` / `External customer (merged channels)`。

## 5. 测试

- agg external 合并：同日同模型不同渠道/令牌 → 1 组。
- Excel：总对账单跨日聚合与日期范围列；明细对账单数字单元格（数字格式读回 `0.003000`、汇率 `7.3`）、无合计行；external 无渠道/令牌列且行合并。
- finalize：Sheet1 删除、tab 顺序 `总对账单, 明细对账单, 日期…`、激活 sheet。
- 明细费用列数字化（读回 `$…` 格式化值）。

## 6. 范围外

- 网页查询表格的口径切换、CSV 汇总导出、汇总物化表：不做。
