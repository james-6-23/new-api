# 账单导出 v4 — 明细对账单增加「刊例价」「专属倍率」两列

- 日期: 2026-07-30
- 状态: 已确认设计，随本次实现落地
- 前置: `2026-07-30-bill-export-v2-design.md`、`2026-07-30-bill-export-v3-granularity-task-billing-design.md`
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 5 (受保护标识)

## 1. 需求

「明细对账单」sheet(按天/周/月聚合的每日明细汇总表)增加两列:

- **刊例价**:模型标价(USD,不含分组折扣)。按次计费模型 = `model_price`(每次);视频任务 = `video_unit_price`(每 1M tokens);其余按 token 模型 = `model_ratio × 2`(每 1M 输入 tokens,与日志页「输入价格」一致)。
- **专属倍率**:实际生效的分组倍率 —— `user_group_ratio` 有效(非 -1、>0)时优先,否则 `group_ratio`。刊例价 × 倍率 ≈ 实际单价,客户一眼看懂折扣。

## 2. 决策

| 主题 | 决策 |
|---|---|
| 列位置 | 「模型名称」之后、「汇总金额(美元)」之前;internal 与 external 两种布局都加。 |
| 数据来源 | 聚合时从每条日志 `Other` 解析(复用 `logPricingInfo`);消费与退款行都参与捕获。 |
| 组内多值 | 日志流按 `created_at DESC` 到达,**首见值(即最新值)胜出**;同组内倍率/价格中途变更时展示最新一次,可接受。 |
| 无数据 | `Other` 为空或无价格键(旧日志)→ 单元格留空(nil),不写 0(避免误读为免费)。 |
| 单元格类型 | 原生 float64 数字(General 格式,与「汇率」列一致;20、0.05 直接可读、可求和)。 |
| 总对账单 | 不加(跨日聚合下价格/倍率可能变化,总表保持纯金额;需求也只提每日明细汇总表)。 |
| 网页查询表 | 不动(需求仅导出 Excel)。 |

## 3. 改动点

- `controller/bill_summary.go`:`billSummaryRow` 增加 `ListPriceUSD`/`EffectiveRatio` + `hasPrice`/`hasRatio`;`capturePricing(log)` 在 addBatch 里对每条日志调用(refund `continue` 之前)。
- `controller/bill_summary_excel.go`:`billDailyLayout` 两种口径插入两列表头与列宽;`writeBillSummarySheets` 明细行写入两值(缺失写 nil)。
- 测试:更新明细对账单列位移(USD internal F→H、external D→F 等);新增带 Other 定价的聚合/导出断言(含 user_group_ratio 优先、-1 回退 group_ratio、旧日志留空)。

## 4. 验证

`go build ./...`;`go test ./controller/ -run 'TestBill|TestWriteBill|TestFinalizeBill|TestBuildBill'`;落盘往返抽查两列数值类型与位置。
