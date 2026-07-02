# 汇总账单 Phase 4 — 退款计入账单（净消费） 设计方案

- 日期: 2026-07-02
- 状态: 已确认设计，待写实现计划
- 关联: 增量于 Phase 1/2/3（账单导出 + 双端汇总表格 + 布局）
- 相关规则: CLAUDE.md Rule 2 (三库兼容)、Rule 5 (受保护标识)

## 1. 背景与目标

账单当前只聚合消费日志（`LogTypeConsume`），忽略了退款（`LogTypeRefund`），导致显示的消费额度偏高。需求：**退款也计入账单，冲抵消费额度**——即汇总金额 = 消费 − 退款（净消费）。

## 2. 数据事实（探索结论）

- 退款日志 `Type = LogTypeRefund`，`Quota` 存**正数**（退回额度的正值，见 `service/task_billing.go`）。消费日志 `Type = LogTypeConsume`，`Quota` 也是正数（扣费额度）。
- 退款日志**不带 tokens**（`RecordTaskBillingLogParams` 无 PromptTokens/CompletionTokens 字段）——退款只退钱，不退 token 用量。
- 底层流式函数 `GetAllLogsForExport`/`GetUserLogsForExport` 的 `logType` 参数：传具体类型 → `WHERE type = ?`（单类型）；传 `LogTypeUnknown(0)` → 不加 type 过滤（扫全部类型）。**不支持多值 IN 过滤**。
- 账单聚合 `addBatch` 当前 `row.Quota += log.Quota`（无脑累加，不分 type）。
- 明细列 `billDetailColumns` 当前**无「类型」列**，退款行在明细里无法辨识。`logTypeLabel` 已有「消费」「退款」标签。

## 3. 已确认决策

| 主题 | 决策 |
|---|---|
| 金额口径 | 净消费 = 消费Quota总和 − 退款Quota总和（同分组 key 净化，净额可为负）。 |
| tokens 列 | 不受退款影响（退款日志本无 tokens）。 |
| 退款分组归属 | 退款按自身 (日期+用户+渠道+令牌+模型) key 归组，同 key 净化，不同 key 单独成行（可能负额）。 |
| 明细账退款行 | 退款日志也列进每日明细（作为退款行，费用为其正 Quota），并新增「类型」列辨识消费/退款。 |
| 实现方案 | 方案 A：底层查询改传 `LogTypeUnknown`（扫全部类型），聚合层/明细 writer 只认 consume/refund 两类，退款 Quota 取负累加（金额），其余类型跳过。 |

## 4. 方案 A 详解

### 4.1 调用点改类型（4 处）

把账单专用的 4 个流式调用从 `model.LogTypeConsume` 改为 `model.LogTypeUnknown`：
- `controller/bill_summary_export.go:116`（admin 导出）
- `controller/bill_summary_export.go:132`（self 导出）
- `controller/bill_summary_query.go:132`（admin JSON 查询）
- `controller/bill_summary_query.go:159`（self JSON 查询）

> `controller/log_export.go` 的普通日志导出（717/740 行）**不动**——那是用户选类型的通用日志导出，与账单无关。

### 4.2 聚合层过滤 + 净化（`controller/bill_summary.go`）

`addBatch` 改为按 `log.Type` 区分：
```
switch log.Type {
case model.LogTypeConsume:
    row.Quota += log.Quota
    row.PromptTokens += log.PromptTokens
    row.CompletionTokens += log.CompletionTokens
    row.CacheReadTokens += getCacheTokensFromOther(log, "cache_tokens")
    row.CacheCreationTokens += getCacheCreationTokensFromOther(log)
case model.LogTypeRefund:
    row.Quota -= log.Quota   // 退款冲抵金额，不动 tokens
default:
    continue                 // 充值/管理/系统/错误/登录等不计入账单
}
```
- 分组 key 计算不变（退款按自身 date+user+channel+token+model 归组）。
- 金额可为负（退多于耗）——汇总/明细 writer/JSON 接口如实展示。

### 4.3 明细 writer 过滤 + 类型列（`controller/bill_detail_excel.go`）

- `addBatch` 里 buffer 前先过滤：只保留 `LogTypeConsume`/`LogTypeRefund`，其余跳过。
- `billDetailColumns` 在 "model" 之后插入 `logExportColumnMap["type"]`（「类型」列），复用 `cellValue` 现有 `case "type": return logTypeLabel(log.Type)`（已有「消费」「退款」标签，无需改）。
- 退款行费用列 `cost` = 其正 Quota 换算（现有 `cellValue` 逻辑不变；退款行 cost 为正，代表退回金额，与消费行区分靠「类型」列）。

### 4.4 受益范围

汇总 sheet、明细 sheet、Excel 导出（admin/self）、JSON 查询接口（admin/self）全部走同一 `addBatch` 与明细 writer，改动后**自动全部生效**，前端无需改（表格/合计如实显示净额）。

## 5. 三库兼容性

- 不新增/修改任何 SQL；`LogTypeUnknown` 分支复用现有"不加 type 过滤"的既有代码路径。Rule 2 无风险。

## 6. 测试策略

- **后端单测**（扩展 `controller/bill_summary_test.go`）：
  - 净消费：同分组含消费 + 退款，断言 `row.Quota` = 消费 − 退款；tokens 只来自消费。
  - 退款单独 key：退款 key 与消费不同 → 单独成行，Quota 为负。
  - 非 consume/refund 类型（如充值 LogTypeTopup）被跳过，不计入。
- 明细 writer：退款行出现在明细、「类型」列显示「退款」（可加断言）。
- 两端 `bun run build` 通过（前端无逻辑改动，仅数据变化）。

## 7. 范围外 (YAGNI)

- 不给流式函数加多类型 IN 参数（改底层签名，波及面大）。
- 不改 tokens 冲抵逻辑（退款无 tokens 数据）。
- 不改前端（净额通过既有列如实展示；如需"退款金额"单列展示是更大改动，本期不做）。
