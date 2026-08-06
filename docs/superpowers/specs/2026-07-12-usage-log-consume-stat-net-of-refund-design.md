# 使用日志「消耗额度」统计计入退款(净消费)设计

日期:2026-07-12
范围:后端统计函数 `model.SumUsedQuota`(使用日志页顶部「消耗额度 / Usage」统计卡),同时作用于管理员「所有日志」(`GetLogsStat`)与用户「我的日志」(`GetLogsSelfStat`)。

---

## 背景与目标

使用日志的「消耗额度」统计只累加消费日志(`LogTypeConsume`),未减去退款(`LogTypeRefund`),导致显示的消耗额度偏高。目标:**消耗额度 = 消费 − 退款(净消费)**,更真实反映实际消耗。

这与账单(bill summary)已在 `docs/superpowers/specs/2026-07-02-bill-refund-phase4-design.md` 落地的口径一致(账单聚合已 `row.Quota -= log.Quota`)。本次把同一净额口径补到使用日志的统计卡。

## 数据事实(探索结论)

- 退款日志 `Type = LogTypeRefund(6)`,`Quota` 存**正数**(退回额度的正值,见 `service/task_billing.go:251` `logQuota = -quotaDelta` 后由 RecordTaskBillingLog 记为正 Quota)。消费日志 `Type = LogTypeConsume(2)`,`Quota` 也是正数。
- `SumUsedQuota` 原实现:`Select("sum(quota) quota")` + `WHERE type = LogTypeConsume`,只算消费。
- 同函数内另有 `rpmTpmQuery` 单独统计 RPM/TPM,过滤 `type = LogTypeConsume`。退款日志**不带 tokens**,不应影响 RPM/TPM。

## 组件设计(后端唯一改动:`model/log.go` `SumUsedQuota`)

1. quota 求和改为按类型带符号累加(消费计正、退款计负):
   ```go
   Select("sum(case when type = ? then -quota else quota end) quota", LogTypeRefund)
   ```
2. 过滤放宽到消费+退款两类:
   ```go
   tx = tx.Where("type IN ?", []int{LogTypeConsume, LogTypeRefund})
   ```
3. `rpmTpmQuery` 保持 `type = LogTypeConsume` 不变(退款无 tokens)。

`CASE WHEN` / `IN` 在 SQLite / MySQL / PostgreSQL 均兼容(Rule 2)。`sum(...)` 无匹配行返回 NULL→扫描为 0,与原行为一致。

## 不改动

- 前端 `common-logs-stats.tsx`:仍直接展示 `stats.quota`,标签 `Usage/消耗额度` 不变(语义仍是"消耗",只是更准)。
- RPM/TPM 统计口径不变。
- 账单(bill summary/detail)已是净额口径,不动。

## 取舍与已知限制

- **时间边界**:退款与其原始消费可能落在不同时间窗(如跨天)。按 `created_at` 过滤后做净额,与账单口径一致;窗口边界处净额可能与"按原始消费归属"略有偏差,属可接受的既有口径。

## 验证

1. 单测(`model/log_stat_test.go`,复用现有 in-memory sqlite TestMain):
   - 消费 1000 + 500、退款 300、充值/管理噪声 → 净消费 = 1200;
   - 无退款回归 → 等于消费总额 1000。
2. `go test ./model/` 全过;`go build ./...`、`go vet ./model/ ./controller/` 干净。
