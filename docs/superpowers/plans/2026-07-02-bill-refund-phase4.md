# 汇总账单 Phase 4 (退款计入账单/净消费) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让账单汇总把退款(LogTypeRefund)冲抵消费(LogTypeConsume)，显示净消费金额；明细账列出退款行并加「类型」列。

**Architecture:** 账单专用流式调用改传 LogTypeUnknown(扫全部类型)，聚合层 addBatch 与明细 writer 只认 consume/refund：消费 Quota 加、退款 Quota 减(tokens 只来自消费)，其余类型跳过。前端无需改，净额通过既有列如实展示。

**Tech Stack:** Go / GORM / excelize。

## Global Constraints

- 三库兼容：不新增/改 SQL，复用现有"logType==LogTypeUnknown 时不加 type 过滤"的既有分支(Rule 2)。
- 禁止修改/删除 QuantumNous / new-api 品牌标识(Rule 5)。
- Go module: github.com/QuantumNous/new-api。
- 退款日志 Quota 为正数、Type=LogTypeRefund、无 tokens；消费日志 Quota 为正数、Type=LogTypeConsume。
- 复用现有(勿重定义)：`getCacheTokensFromOther(*model.Log,string)int`、`getCacheCreationTokensFromOther(*model.Log)int`、`cellValue(logExportColumn,*model.Log)any`(含 `case "type": logTypeLabel`)、`logExportColumnMap`(含 key "type")、`logTypeLabel`(已含「消费」「退款」)。
- 测试 helper `tsOn(day,hour)` 已存在于 controller/bill_summary_test.go。

---

## Task 1: 聚合层按类型净化 (消费加/退款减)

**Files:**
- Modify: `controller/bill_summary.go`（`addBatch` 方法）
- Test: `controller/bill_summary_test.go`（新增退款净化测试）

**Interfaces:**
- Consumes: `model.LogTypeConsume`, `model.LogTypeRefund`, `model.LogTypeTopup`; 现有 cache helper。
- Produces: `addBatch` 语义变更 — 只累计 consume(+)/refund(−Quota)，其余类型跳过；tokens 只来自 consume。

- [ ] **Step 1: Write the failing test**

在 `controller/bill_summary_test.go` 追加：
```go
func TestBillSummaryAgg_RefundNetsConsumption(t *testing.T) {
	agg := newBillSummaryAgg()
	// 同一分组：消费 1000 + 退款 300 => 净 700；tokens 只来自消费
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5, Other: `{"cache_tokens":4}`},
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 11), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 300, PromptTokens: 0, CompletionTokens: 0, Other: ``},
		// 充值日志必须被忽略
		{Type: model.LogTypeTopup, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 99999, Other: ``},
	})

	keys := agg.sortedKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 group (topup ignored), got %d: %+v", len(keys), keys)
	}
	r := agg.rows[keys[0]]
	if r.Quota != 700 {
		t.Fatalf("net quota = %d, want 700 (1000 consume - 300 refund)", r.Quota)
	}
	if r.PromptTokens != 10 || r.CompletionTokens != 5 || r.CacheReadTokens != 4 {
		t.Fatalf("tokens must come from consume only: %+v", r)
	}
}

func TestBillSummaryAgg_RefundOnlyGroupIsNegative(t *testing.T) {
	agg := newBillSummaryAgg()
	// 退款独立 key（模型不同于任何消费）=> 单独成行，净额为负
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 9), Username: "bob", ChannelId: 1, TokenName: "t2", ModelName: "refunded-model",
			Quota: 500, Other: ``},
	})
	keys := agg.sortedKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 group, got %d", len(keys))
	}
	if agg.rows[keys[0]].Quota != -500 {
		t.Fatalf("refund-only quota = %d, want -500", agg.rows[keys[0]].Quota)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/ -run 'TestBillSummaryAgg_Refund' -v`
Expected: FAIL（当前 addBatch 无脑累加：净额会是 1000+300=1300，且 topup 被计入 → 断言失败）。

- [ ] **Step 3: Modify addBatch**

把 `controller/bill_summary.go` 的 `addBatch` 方法体替换为：
```go
func (a *billSummaryAgg) addBatch(logs []*model.Log) {
	for _, log := range logs {
		// 账单只计消费与退款；其余类型(充值/管理/系统/错误/登录)跳过。
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund {
			continue
		}
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
		if log.Type == model.LogTypeRefund {
			// 退款冲抵金额；退款日志无 tokens，不动 token 列。
			row.Quota -= log.Quota
			continue
		}
		row.Quota += log.Quota
		row.PromptTokens += log.PromptTokens
		row.CompletionTokens += log.CompletionTokens
		row.CacheReadTokens += getCacheTokensFromOther(log, "cache_tokens")
		row.CacheCreationTokens += getCacheCreationTokensFromOther(log)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./controller/ -run 'Bill|Finalize' -v` → 全绿（新增两测 + 既有 Bill 测试）。
Run: `go build ./...` → clean。
(忽略无关的既有失败 `TestListModelsTokenLimitIncludesTieredBillingModel`。)

- [ ] **Step 5: Commit**

```bash
git add controller/bill_summary.go controller/bill_summary_test.go
git commit -m "feat(bill): net refunds against consumption in bill summary

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: 明细 writer 过滤 + 新增「类型」列

**Files:**
- Modify: `controller/bill_detail_excel.go`（`billDetailColumns` 加 type 列；`addBatch` 过滤非 consume/refund）
- Test: `controller/bill_detail_excel_test.go`（断言退款行 + 类型列）

**Interfaces:**
- Consumes: `logExportColumnMap["type"]`, `model.LogTypeConsume/LogTypeRefund`, `cellValue`(type 列走 `logTypeLabel`)。
- Produces: 明细每日 sheet 含「类型」列；退款行以「退款」出现；非 consume/refund 行不进明细。

- [ ] **Step 1: Write the failing test**

在 `controller/bill_detail_excel_test.go` 追加：
```go
func TestBillDetailWriter_IncludesRefundRowWithTypeColumn(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	w, err := newBillDetailWriter(f, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "a", ModelName: "gpt-4o", Quota: 1000},
		{Type: model.LogTypeRefund, CreatedAt: tsOn("2026-06-01", 11), Username: "a", ModelName: "gpt-4o", Quota: 300},
		{Type: model.LogTypeTopup, CreatedAt: tsOn("2026-06-01", 12), Username: "a", ModelName: "gpt-4o", Quota: 99999},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}

	rows, err := f.GetRows("2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	// 表头行 + 2 数据行(消费/退款)，充值被过滤 => 共 3 行
	if len(rows) != 3 {
		t.Fatalf("expected header + 2 data rows (topup filtered), got %d: %v", len(rows), rows)
	}
	// 表头含「类型」列
	header := rows[0]
	typeIdx := -1
	for i, h := range header {
		if h == "类型" {
			typeIdx = i
		}
	}
	if typeIdx == -1 {
		t.Fatalf("header missing 类型 column: %v", header)
	}
	// 存在一行「类型」为「退款」
	foundRefund := false
	for _, r := range rows[1:] {
		if typeIdx < len(r) && r[typeIdx] == "退款" {
			foundRefund = true
		}
	}
	if !foundRefund {
		t.Fatalf("no 退款 row found in detail: %v", rows)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/ -run TestBillDetailWriter_IncludesRefundRow -v`
Expected: FAIL（当前无「类型」列，且 topup 未过滤 → 行数/表头断言失败）。

- [ ] **Step 3: Add type column to billDetailColumns**

在 `controller/bill_detail_excel.go` 顶部的 `billDetailColumns` 定义里，把 model 之后加入 "type"。当前：
```go
	keys := []string{"time", "username", "token", "group", "model", "prompt", "completion"}
```
改为：
```go
	keys := []string{"time", "username", "token", "group", "model", "type", "prompt", "completion"}
```
（`logExportColumnMap["type"]` 已存在，header「类型」，`cellValue` 的 `case "type"` 返回 `logTypeLabel(log.Type)`，无需改。）

- [ ] **Step 4: Filter non consume/refund in detail addBatch**

在 `controller/bill_detail_excel.go` 的 `addBatch` 循环体开头加过滤（buffer 前）：
```go
func (w *billDetailWriter) addBatch(logs []*model.Log) error {
	for _, log := range logs {
		// 明细只列消费与退款，其余类型跳过（与汇总口径一致）。
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund {
			continue
		}
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
```

- [ ] **Step 5: Run tests**

Run: `go test ./controller/ -run 'Bill|Finalize' -v` → 全绿（含新测；既有明细测试若因新增「类型」列改变列数需同步——检查 `TestBillDetailWriter_SplitByModelWithinDay`/`ModelHeaderReEmittedOnRoll` 是否断言了具体列，若断言了模型标题行文本"模型：xxx"则不受影响，因为那是整行首格；若有按列索引的断言需相应更新）。
Run: `go build ./...` → clean。

> 若既有明细测试因列集变化而失败，按新列集(含 type)更新其断言即可——但不要改动 Task 3 中的模型标题行逻辑（那是首格文本，与列数无关）。

- [ ] **Step 6: Commit**

```bash
git add controller/bill_detail_excel.go controller/bill_detail_excel_test.go
git commit -m "feat(bill): include refund rows and type column in bill detail

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: 账单调用点改扫全部类型

**Files:**
- Modify: `controller/bill_summary_export.go`（2 处 LogTypeConsume → LogTypeUnknown）
- Modify: `controller/bill_summary_query.go`（2 处 LogTypeConsume → LogTypeUnknown）

**Interfaces:**
- Consumes: `model.LogTypeUnknown`。聚合/明细 writer 自身已过滤类型(Task 1/2)。

- [ ] **Step 1: 改导出 handlers**

`controller/bill_summary_export.go`：把两处 `model.GetAllLogsForExport(model.LogTypeConsume, ...)` 与 `model.GetUserLogsForExport(userId, model.LogTypeConsume, ...)` 里的 `model.LogTypeConsume` 改为 `model.LogTypeUnknown`。

具体：
- 第 116 行附近 `return model.GetAllLogsForExport(model.LogTypeConsume, p.startTimestamp, ...)` → `model.GetAllLogsForExport(model.LogTypeUnknown, p.startTimestamp, ...)`
- 第 132 行附近 `return model.GetUserLogsForExport(userId, model.LogTypeConsume, p.startTimestamp, ...)` → `model.GetUserLogsForExport(userId, model.LogTypeUnknown, p.startTimestamp, ...)`

- [ ] **Step 2: 改查询 handlers**

`controller/bill_summary_query.go`：同样两处：
- 第 132 行附近 `model.GetAllLogsForExport(model.LogTypeConsume, start, end, ...)` → `model.GetAllLogsForExport(model.LogTypeUnknown, start, end, ...)`
- 第 159 行附近 `model.GetUserLogsForExport(userId, model.LogTypeConsume, start, end, ...)` → `model.GetUserLogsForExport(userId, model.LogTypeUnknown, start, end, ...)`

> 注意：只改账单专用文件。`controller/log_export.go` 的通用日志导出**不动**。

- [ ] **Step 3: Build + full bill tests**

Run: `go build ./...` → clean。
Run: `go test ./controller/ -run 'Bill|Finalize' -v` → 全绿。

- [ ] **Step 4: Commit**

```bash
git add controller/bill_summary_export.go controller/bill_summary_query.go
git commit -m "feat(bill): scan all log types so refunds reach bill aggregation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Manual Verification

- [ ] 后端：`go test ./controller/ -run 'Bill|Finalize' -v` 全绿；`go build ./...` clean。
- [ ] 造数据：同一 (日+用户+渠道+令牌+模型) 有消费 + 退款 → 汇总金额 = 消费 − 退款（净额）；tokens 只反映消费。
- [ ] 充值/管理等其它类型日志不出现在账单汇总与明细。
- [ ] 勾选「附带每日明细账」→ 明细含「类型」列，退款以「退款」行出现。
- [ ] JSON 查询接口(admin/self)与 Excel 导出金额一致，均为净额。
- [ ] 前端表格/合计如实显示净额（可能出现负额，属预期）。

## Self-Review Notes

- **Spec 覆盖**：净化口径 (Task 1)、明细退款行+类型列 (Task 2)、扫全部类型 (Task 3)。
- **不变量**：tokens 只来自消费；分组 key 不变；通用日志导出 log_export.go 不动；三库无新 SQL。
- **类型一致**：`addBatch` 过滤条件 (consume/refund) 在聚合层与明细 writer 一致；「类型」列复用 `logExportColumnMap["type"]` + `logTypeLabel`。
- **YAGNI**：不加多类型 IN 查询参数；不改前端；退款不冲抵 tokens。
- **风险**：Task 2 新增「类型」列会改变明细列集，既有明细测试若按列索引断言需同步更新（Step 5 已提示）。
