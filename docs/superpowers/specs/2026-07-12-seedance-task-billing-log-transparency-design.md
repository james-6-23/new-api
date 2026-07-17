# 异步任务（seedance 系列/视频）计费日志透明化 + 预扣↔结算↔退款关联设计

日期:2026-07-12
范围:异步任务计费日志的**显示与关联**(通用,不限 seedance)。seedance 视频任务是首个受益者,因为它携带视频计费标记。

涉及层次:
- 后端:`service/task_billing.go`、`controller/relay.go`、`model/log.go`、`model/task.go`
- 前端(classic):`web/classic/src/helpers/render.jsx`、`web/classic/src/hooks/usage-logs/useUsageLogsData.jsx`

---

## Context(为什么做这件事)

一条 seedance 视频任务在日志里目前会产生**三行相互割裂**的记录,客户看不懂、也无法关联:

| 行 | 写入位置 | 携带字段 | 缺失 |
|---|---|---|---|
| **预扣费**(type 2) | `LogTaskConsumption`(`controller/relay.go:577`) | `is_task`、`model_ratio`、`group_ratio`、`video_unit_price`、`request_id`(列) | **无 `task_id`**、无公式、无 token 数 |
| **结算/补扣**(type 2) | `RecalculateTaskQuota`(`service/task_billing.go`) | `task_id`、`pre_consumed_quota`、`actual_quota`、`reason` | **无 `request_id`(列)** |
| **退款**(type 6) | `RefundTaskQuota` / 差额退款 | `task_id`、`pre_consumed_quota`、`actual_quota` | **无 `request_id`(列)** |

具体症状(均已核实根因):

1. **`输出价格 ¥NaN`** — `renderLogContent` 计算 `modelRatio*2*completion_ratio*rate`(`render.jsx:2122`),而任务日志的 `other` 里从不带 `completion_ratio` → `undefined` → `NaN`;并且完全忽略了视频折扣(`video_input`/分辨率)。
2. **`操作 textGenerate`** — 直接用 `info.Action`,不是视频语义标签。
3. **计费公式不明显** — 预扣行的「计费过程」只有固定文案 `任务预扣费（将在任务完成后按实际token重算）`,零数字。
4. **退款不明显** — 退款行的「计费过程」只是原始 `reason` 串 `token重算：tokens=…`,没有「退了多少/为什么」。
5. **无法关联** — 结构性缺口:**预扣行有 `request_id`(列)无 `task_id`;结算/退款行有 `task_id`(JSON)无 `request_id`(列)。三行没有任何共享的可筛选键。**

## 目标

- 修掉 `¥NaN`,视频任务显示正确的视频单价与折扣。
- 预扣、结算、退款每一行的「计费过程」都用**显式公式 + 数字**说明,客户对扣费无疑问。
- 多条并发请求时,可以把**同一请求的预扣↔结算↔退款**一键关联(按 `request_id`)。

## 关键取舍(已与用户确认)

- **范围**:通用任务日志(渲染层 + 结算层是所有异步任务共用),不给 seedance 加模型特例分支。
- **关联键 = `request_id`**:它是 `logs` 表上**已建索引的列**(`idx_logs_request_id`),`GetAllLogs`/`GetUserLogs` 已支持按它过滤,`UsageLogsFilters` 已有 `request_id` 搜索框,展开面板**已经在显示 Request ID**(`useUsageLogsData.jsx:397`)。而 `task_id` 只存在于 `Other` JSON 里,**无法跨三库(SQLite/MySQL/PG)安全过滤(Rule 2)**,只作为展示/文案用途。
- **实现路线 = 方案 A(前端渲染 + 后端加结构化键)**:后端只加数据(键/列),前端新增任务/视频专用渲染。保留「后端存结构化 `other`,前端渲染」的既有模式;尊重 CNY/USD 显示切换;**无表结构迁移**(复用现有 `request_id` 列)。
- **已排除方案 C(单行原地更新)**:会破坏账单净额聚合(`2026-07-12-usage-log-consume-stat-net-of-refund-design.md` 与 `bill-refund-phase4`)依赖的「消费/退款分行」审计轨迹。

---

## 组件设计

### 后端改动 1:关联键落地(`request_id` 贯穿三行 + 预扣行补 `task_id`)

**A. Task 持久化原始 `request_id`**
- `model/task.go`:`TaskPrivateData` 增加 `RequestId string \`json:"request_id,omitempty"\``。
- **在 `controller/relay.go` 组装 `PrivateData` 处写入**(`relay.go:584` 附近,已在设置 `BillingSource/TokenId/BillingContext` 的同一块):`task.PrivateData.RequestId = c.GetString(common.RequestIdKey)`。
  - 选此处而非 `InitTask` 内部:`c` 在此可直接取到请求 ID,且不改 `InitTask` 签名,改动最小、边界清晰。

**B. `RecordTaskBillingLog` 支持写 `request_id` 列**
- `model/log.go`:`RecordTaskBillingLogParams` 增加 `RequestId string`;`RecordTaskBillingLog` 里 `log.RequestId = params.RequestId`。
- `service/task_billing.go`:`RecalculateTaskQuota` 与 `RefundTaskQuota` 调 `RecordTaskBillingLog` 时传 `RequestId: task.PrivateData.RequestId`。

**C. 预扣行补 `task_id`(reorder)**
- `controller/relay.go`:`LogTaskConsumption`(577)当前在 `InitTask`(579)**之前**执行,拿不到公开 `task.TaskID`。将顺序调整为**先 `InitTask` 得到 task,再 `LogTaskConsumption`**,并给 `LogTaskConsumption` 传入 `task.TaskID`。
  - `LogTaskConsumption` 不依赖 task 对象,reorder 安全。
  - 在 `other` 里加 `other["task_id"] = taskID`。

**D. 阶段标记 `billing_stage`**
- 预扣行(`LogTaskConsumption`):`other["billing_stage"] = "pre_consume"`。
- 结算/退款行(`RecalculateTaskQuota`):`quotaDelta > 0` → `"settle"`;`quotaDelta < 0` → `"refund"`。
- 失败全额退款(`RefundTaskQuota`):`other["billing_stage"] = "refund"`。

> `request_id` 列对已写旧日志为空 —— 仅新任务生效,可接受(既有 net-of-refund 规格同样只对新数据生效)。

### 后端改动 2:不新增/不改动的部分(刻意)

- **不改** `service/task_billing.go` 的扣费/退款金额逻辑与 `RecalculateTaskQuotaByTokens` 的计算公式 —— 只加日志键。
- **不改** 账单聚合口径。
- **不加** 任何 seedance 模型特例分支;`video_*` 键沿用现有 `taskBillingOther` 产出。

### 前端改动 1:修 `¥NaN` + 视频价格行(`日志详情`,`renderLogContent`)

- **全局兜底**:`completion_ratio` 读取改为 `?? 1`(或渲染前判 `isNaN`),确保任何任务日志都不再出现 `NaN`。
- **视频价格块**:当 `other.video_unit_price`(或 `video_resolution_tier`/`video_has_input`)存在时,改渲染视频专用行,**不再引用 `completion_ratio`**:
  - `视频单价 {symbol}{video_unit_price × rate} / 1M tokens（{分辨率档}，{含/不含视频输入}），分组倍率 {ratio}`
  - `video_unit_price` 后端已算好(`modelRatio × 2 × PricingRatio`,`task_billing.go:50/148`)。

### 前端改动 2:各阶段透明公式(`计费过程`,`renderTaskBillingProcess`)

按 `other.billing_stage` 分支(货币始终走 `getCurrencyConfig()`):

- **`pre_consume`(预扣)**:
  ```
  任务预扣费（估算，完成后按实际 token 重算）
  预扣额度 = 预估用量 × 单价 {symbol}{p} × 分组 {g}[ × 视频折扣 {v}] = {symbol}{quota}
  仅供参考，以实际扣费为准
  ```
  - `p` = `video_unit_price`(视频)或 `model_ratio × 2`;`g` = 有效分组倍率;`v` = `video_input`/分辨率折扣(存在时才显示);`quota` = 该行 `Quota`(预扣额度)。
- **`settle` / `refund`(结算/退款,`task_id` 存在)**:用 `pre_consumed_quota` + `actual_quota`:
  ```
  实际结算 = {tokens} tokens × 单价 {symbol}{p} × 分组 {g}[ × 视频折扣 {v}] = 应扣 {symbol}{actual}
  预扣 {symbol}{pre} → 实扣 {symbol}{actual}，{补扣/退款} {symbol}{|delta|}
  任务 {task_id}
  ```
  - `tokens` = 实际 token(视频取 `video_tokens`;**非视频任务 `other` 不带实际 token,则省略「实际结算 = tokens × …」这一行,只显示 `预扣 → 实扣 + 补扣/退款` 金额行**);`pre` = `pre_consumed_quota`;`actual` = `actual_quota`;`delta` = `actual − pre`(>0 补扣,<0 退款)。

### 前端改动 3:一键关联(点击 Request ID 按 `request_id` 过滤)

- 展开面板已有「Request ID」行(`useUsageLogsData.jsx:394-398`)。把该值改为**可点击**:点击 → `formApi.setValue('request_id', rid)` → `refresh()`(hook 已导出 `formApi` 与 `refresh`),列表即筛出同一请求的预扣/结算/退款三行。
- 「计费过程」文案里的 `task_id` 作为**展示用**标签(可点击复制),不承担过滤职责。

### 前端改动 4:操作标签(可选打磨,低优先级)

- 将 `textGenerate` 等 action 映射为「视频生成」等友好标签(前端 i18n 映射,作用于 `其他详情` 的 `操作 X` 展示)。独立小改,可后置。

---

## 数据契约(新增/复用键汇总)

写入 `log.Other`(前端读):

| 键 | 行 | 来源 | 用途 |
|---|---|---|---|
| `billing_stage` | 三行 | 后端新增 | 前端选模板:`pre_consume`/`settle`/`refund` |
| `task_id` | 预扣行(新增)、结算/退款行(已有) | `task.TaskID` | 展示用标签 |
| `video_unit_price`/`video_resolution_tier`/`video_has_input`/`video_tokens` | 视频行(已有) | `taskBillingOther` | 视频价格行 + 公式 |
| `pre_consumed_quota`/`actual_quota`(已有) | 结算/退款行 | `RecalculateTaskQuota` | 结算公式 |
| `model_ratio`/`group_ratio`/`user_group_ratio`/OtherRatios(已有) | 三行 | 现有 | 单价与折扣 |

写入 `log.RequestId`(索引列,后端过滤):

| 行 | 现状 | 改后 |
|---|---|---|
| 预扣 | 已写(`RecordConsumeLog`) | 不变 |
| 结算/退款 | **未写** | 由 `task.PrivateData.RequestId` 传入 `RecordTaskBillingLog` 写入 |

---

## 验证方案

### 后端(Go)
1. `TaskPrivateData.RequestId` 经 `Scan`/`Value`(`model/task.go:208-226`)JSON 往返不丢。
2. `RecordTaskBillingLog` 传 `RequestId` → `log.RequestId` 落库;`RecalculateTaskQuota`(settle & refund 两分支)与 `RefundTaskQuota` 均带上 `request_id` 与正确的 `billing_stage`。
3. reorder 后预扣行仍带 `task_id` 且金额不变。
4. `go build ./...`;`go test ./model/ ./service/`;`go vet ./service/ ./controller/ ./model/` 干净。

### 前端
5. 渲染单测/手测:
   - 任务日志无 `completion_ratio` → 不出现 `NaN`。
   - 带 `video_*` → 渲染视频单价行(不含 `NaN`,含分辨率/视频输入说明)。
   - `billing_stage=pre_consume/settle/refund` → 分别渲染对应公式,数字取自 `Quota`/`pre_consumed_quota`/`actual_quota`。
   - 点击展开面板 Request ID → 列表按该 `request_id` 过滤,预扣+结算/退款同现。
6. CNY 与 USD 两种 `quota_display_type` 下金额/单价显示正确(无硬编码 ¥)。

### 端到端(手动)
7. Sora/OpenAI 渠道跑一条 seedance 视频任务(含视频输入):核对预扣行公式含 `视频折扣`、结算行 `预扣→实扣` 与退款差额、点击 Request ID 关联出该任务全部行。

---

## 已知限制 / 后续

- `request_id` 关联仅对**本次改动上线后**产生的任务生效(旧任务结算/退款行无 `request_id` 列值)。
- 操作标签友好化(改动 4)为可选打磨,可独立后置。
- 若后续要在「任务日志」页(`useTaskLogsData`)也做同样关联,可复用同一 `request_id` 键;本次先覆盖「使用日志」页。
