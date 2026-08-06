# 账单导出 v3 — 天/周/月汇总粒度 + 任务预扣费公式导出 + 消费退款对齐 (Bill Export v3)

- 日期: 2026-07-30
- 状态: 已确认设计，随本次实现落地
- 前置: `2026-07-30-bill-export-v2-design.md`(总/明细对账单)、`2026-07-12-seedance-task-billing-log-transparency-design.md`(三行关联 + billing_stage)
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 3 (Bun)、Rule 5 (受保护标识)

## 1. 需求

1. **筛选框上加「天/周/月」下拉** —— 汇总粒度选择:明细对账单(及网页查询表)按 天(现状)/自然周(周一起)/自然月 聚合。总对账单仍是整段范围,不受粒度影响。
2. **seedance 等预扣费任务的明细导出**:
   - 使用明细「计费过程」列导出**最新的阶段公式**(预扣/结算/退款各自的显式公式,替代现在对任务行输出的占位符或错误的按 token 公式);
   - **消费与退款对齐导出**:同一任务(同 `request_id`)的预扣、结算、退款行在明细 sheet 内相邻排列(组内按时间正序:预扣 → 结算/退款),一眼看清多退少补;
   - 退款行「费用」列写**负数**,与消费行同列可直接求和得净额。

## 2. 关键决策

| 主题 | 决策 |
|---|---|
| 粒度参数 | `granularity=day\|week\|month`,缺省 `day`(兼容);查询与导出两个接口都接受。 |
| 周口径 | 自然周,周一为起点;聚合键 = 周一日期 `2026-06-01`,显示为 `2026-06-01 ~ 2026-06-07`。月 = `2026-06`。 |
| 总对账单范围列 | 与粒度无关:agg 在 addBatch 里另行记录**真实**最早/最晚归账日(日历日),总对账单「开始/结束日期」始终是真实日期。 |
| 每日明细 sheets | 仍按**日历日**分 sheet(每日使用明细的定义不变),粒度只影响汇总行。 |
| 任务行识别 | `Other.billing_stage`(pre_consume/settle/refund)或 `task_id`/`is_task`(兼容旧日志),与前端 `renderTaskBillingProcess` 同判据。 |
| 公式文案 | 对齐前端模板(货币统一用导出既有的 USD 6 位小数):预扣行 `任务预扣费（估算…）` + `预扣金额 $q` + 单价/分组行;结算/退款行 `[实际结算 = tokens × 单价 $u / 1M × 分组 g[ × 视频折扣 v] = 应扣 $a]` + `预扣 $pre → 实扣 $actual，补扣/退款 $Δ` + `任务 <task_id>`;失败全额退款(无 pre/actual)`任务退款：退还 $q` + `原因 …`。 |
| 对齐键 | `log.RequestId` 列(三行已贯穿,带索引;旧日志结算/退款行无值则不对齐,自然降级)。普通请求一条日志成组大小 1,不受影响。同任务三行同模型,分模型区块内对齐不越块。跨日历日的预扣/退款各归各日 sheet(已知限制)。 |
| 退款费用符号 | 仅账单明细 writer 内生效(费用列 = −quota/QuotaPerUnit);通用日志导出(`ExportAllLogs`)与 CSV 不动。 |

## 3. 改动点

### 后端
- `controller/bill_summary.go`:agg 增加 `granularity`、`minDay`/`maxDay`(真实日);`billBucketDay(t, g)` 归桶、`billPeriodLabel(bucket, g)` 显示标签;addBatch 用桶做 key.Day。
- `controller/bill_summary_excel.go`:总对账单范围列用真实 min/max(空则回退桶推导);明细对账单日期列写 `billPeriodLabel`。
- `controller/bill_summary_query.go`:两个查询 handler 解析 `granularity`,DTO `date` 输出标签。
- `controller/bill_summary_export.go`:`parseBillExportParams` 解析 `granularity`。
- `controller/log_export.go`:`logPricingInfo` 增补任务键(`billing_stage`/`is_task`/`task_id`/`pre_consumed_quota`/`actual_quota`/`video_unit_price`/`video_tokens`/`video_input`/`reason`);`buildBillingText` 检测任务行分派 `buildTaskBillingText`(新)。
- `controller/bill_detail_excel.go`:`alignTaskPairs`(flushDay 排序后调用,分组锚定首现位置、组内时间正序);退款行费用取负。

### 前端(两套主题)
- default:账单管理页筛选卡片**顶部**加「统计粒度」Select(按天/按周/按月,默认按天),查询与导出都带 `granularity`;`api.ts` 加字段;zh.json 加 key。
- classic:`BillFilters.jsx` 首位加 `Form.Select granularity`;`index.jsx` `collectParams` 带上(查询+导出共用)。

## 4. 验证

- 单测:周/月归桶与标签;agg 周/月聚合 + 真实 min/max;总对账单月粒度下范围列仍为真实日期;任务公式四分支(预扣视频/结算补扣/差额退款/失败全额退款)+ 非任务行回归;alignTaskPairs 相邻性与组内正序;退款费用负数单元格。
- `go build ./...`、`go test ./controller/`;default 主题 `tsc -b`;落盘往返抽查。

## 5. 范围外

- 任务日志页(useTaskLogsData)、通用日志导出的公式/符号改造;跨日 sheet 的对齐;周起始日可配置。
