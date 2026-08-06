# seedance 使用记录单行化 + 任务态展示 — 设计文档

- 日期: 2026-08-01
- 状态: 已实现（2026-08-01，含测试；生成秒数按 §3.3 简化为增强阶段解析）
- 前置: `2026-07-12-seedance-task-billing-log-transparency-design.md`（三行透明化，request_id 关联）、`2026-06-25-dreamina-seedance2-video-billing-design.md`、`2026-07-28-seedance-thirdparty-video-channel-design.md`、`2026-07-31-task-upstream-error-passthrough-design.md`
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 2 (三库兼容——LIKE 过滤与迁移必须 SQLite/MySQL/PG 通过)、Rule 5 (受保护标识)

## 1. 需求

seedance 视频任务采用预扣费+多退少补，使用日志中一个任务出现 2~3 行（预扣消费 / 结算补扣 / 退款），用户观感差。改造为：

1. **使用日志一个任务只显示一行**（预扣行为主行），settle/refund 行从列表隐藏（DB 不删）。
2. 主行新增**完成状态列**（对齐任务日志页观感：StatusBadge + 进度条 + 百分比）。
3. **费用列**：进行中→「生成中」灰标签；成功→最终实扣净额；失败→`$0` + 「已退款」角标。
4. **详情弹窗**扩展 seedance 任务区块：分辨率档位（480p/720p/1080p/4K）、生成秒数、是否含参考视频、输出 tokens、单价、**专属倍率**（user_group_ratio 有效时优先展示并标注）、预扣→实扣结算过程、失败原因（脱敏后）、任务 ID、上游任务 ID（仅管理员）。
5. **历史记录**也呈现新样式（含 2026-07-12 之前无 task_id/billing_stage 的旧日志——需一次性回填）。
6. 账单导出**逐日明细 sheet** 同 request_id 的 seedance 多行合并为一行（净额+完整过程文字）；汇总类 sheet 与原始日志导出不变。

## 2. 已确认决策

| 主题 | 决策 |
|---|---|
| 合并层 | **A. 展示层合并**：DB 仍写三行（审计/账单/净额统计不动），查询/展示时折叠。历史记录自动生效。 |
| 费用口径 | 进行中不显示预扣金额（详情内仍可见"预扣 $X，结算后多退少补"）；成功显示实扣净额；失败显示 $0+已退款。 |
| 范围 | 模型名含 `seedance` 的视频任务日志（doubao-seedance-\*、dreamina-seedance-\*、第三方通道 59 同名模型）。Grok 视频、其它任务、聊天日志不变。 |
| 逐日明细 | 合并为一行：费用列净额，计费过程单元格保留各阶段完整文字。 |
| 原始日志导出 | （2026-08-01 追加确认）同样合并为一行：settle/refund 行不再导出，预扣行费用写实扣净额、计费过程写任务全过程（状态/预扣→实扣/失败原因）。仅挂接 `ExportAllLogs`/`ExportUserLogs`（bill-\*.xlsx/csv），账单汇总导出的流式扫描不受影响。 |
| 上游任务 ID | 仅管理员可见（沿用"客户端只拿公开 task id"隐私边界，`formatUserLogs` 同款处理）。 |
| 刷新 | 不做自动轮询，与任务日志页一致手动刷新。 |
| 退款筛选副作用 | 列表按"退款"类型筛选时不再出现 seedance 退款行（信息在主行详情内），接受。 |

## 3. 架构

### 3.1 后端：查询时排除 + 增强（`model/log.go` + 新 `model/log_task_enrich.go`）

**列表排除**（`GetAllLogs`/`GetUserLogs` 的分页查询与 count，两处同规则）：

- 追加条件：`NOT (model_name LIKE '%seedance%' AND (other LIKE '%"billing_stage":"settle"%' OR other LIKE '%"billing_stage":"refund"%'))`。
- 纯字符串 LIKE，SQLite/MySQL/PG 通杀；`"billing_stage":"..."` 由 `common.Marshal` 生成、无空格，模式稳定。
- 账单汇总导出的流式扫描（`GetAllLogsForExport`/`GetUserLogsForExport`）**不加**此过滤——净额、计费记录、请求数口径不变。

**行增强**：对页内 `model_name` 含 `seedance` 且 `Other.billing_stage=="pre_consume"`（或回填后等价）的行：

1. 解析各行 `Other.task_id`，**批量** `WHERE task_id IN (...)` 查 `tasks`（task_id 有索引）。
2. 收集这些行的 `request_id`，**批量**查兄弟日志（settle/refund，request_id 有索引），仅取本页涉及的。
3. `Log` 结构体挂临时字段（`gorm:"-"`，序列化为 `task_info`，omitempty）：

```
type LogTaskInfo struct {
    Status         string  // SUBMITTED/QUEUED/IN_PROGRESS/SUCCESS/FAILURE/UNKNOWN
    Progress       string  // "50%"
    FailReason     string  // 已脱敏(MaskSensitiveInfo 已在写入 task 时做过)
    UpstreamTaskId string  // 仅 admin 路径填充
    FinalQuota     int     // 实扣净额(quota 单位)：预扣+settle−refund
    OutputTokens   int     // video/completion tokens
    ResolutionTier string  // 480p/720p/1080p/4k
    DurationS      int     // 生成秒数,解析不到为 0(前端不显示)
    HasInput       bool    // 是否含参考视频
    EffectiveRatio float64 // user_group_ratio 有效优先,否则 group_ratio
    IsUserRatio    bool    // true=专属倍率
}
```

4. 数据优先级：task 行 → 兄弟日志 Other → 主行 Other。**task 已被清理的兜底**：有 refund 兄弟→FAILURE、有 settle→SUCCESS、都无→UNKNOWN（FinalQuota=预扣额）。
5. `formatUserLogs`（self 路径）清空 `UpstreamTaskId`。

### 3.2 历史回填（一次性启动迁移，`model/migration_seedance_backfill.go`）

旧日志（三行透明化之前）`Other` 无 `task_id`/`billing_stage`，无法关联。按既有幂等迁移模式（options 表标记 `seedance_log_backfill_done`，完成即跳过）：

- 分批（每批 500）遍历 `tasks` 表中 platform/action 为视频生成且模型含 seedance 的行，取 `private_data.request_id` + `task_id`。
- 对每个 task，按 `request_id` 查其 seedance 日志行：
  - type=2 且 Other 无 `billing_stage` 且无 `pre_consumed_quota` → 注入 `"billing_stage":"pre_consume","task_id":"..."`；
  - type=2 且有 `pre_consumed_quota`（或 Content 为结算文案）→ `settle`；
  - type=6 → `refund`。
- `Other` 读改写全程 `common.UnmarshalJsonStr`/`common.Marshal`；UPDATE 只动 `other` 列；单行失败记日志继续，不中断启动。
- `private_data.request_id` 为空的更早期任务跳过（该部分历史行维持旧样式，接受）。

### 3.3 生成秒数来源（实现简化：不改写入侧）

`relaycommon.TaskInfo` 无 Duration 字段，而每个任务的 `task.Data` 已存有（脱敏后的）上游最终响应 JSON（提交时与轮询时都会更新）。因此生成秒数统一在**增强阶段**从 `task.Data` 尽力解析（`duration`/`seconds`/`duration_seconds` 等键，顶层与一层嵌套），新旧记录同一条代码路径，不需要在结算时新增 `video_duration_s` 写入。解析不到则详情里不显示该项。

### 3.4 前端 — 默认主题（`web/default/src/features/usage-logs/`）

- **「状态」列**（新增，列设置可隐藏，非任务行留空）：`log.task_info` 存在时渲染——复用 `taskStatusMapper` 配色的 `StatusBadge` + `ui/progress` 细进度条 + 百分比文字。
- **费用列**（common-logs-columns.tsx cost cell）：`task_info` 存在时按状态分支——进行中灰标签「生成中」；SUCCESS 显示 `FinalQuota` 金额；FAILURE 显示 `$0` + 「已退款」小角标；UNKNOWN 按现值显示。
- **详情弹窗**：`VideoPricingBreakdown` 扩展为 seedance 任务区块，新增行：状态+进度、分辨率档位（复用现有 tierLabelMap）、生成秒数、是否含参考视频、输出 tokens、倍率行标注「专属倍率」/「分组倍率」、预扣→实扣→多退少补过程、失败原因、任务 ID、上游任务 ID（admin）。
- i18n：新 key 补 zh（en 为源串），必要时其余 4 语言跟随现有惯例。

### 3.4b 前端 — Classic 主题（`web/classic/src/`）

新旧两个前端都要改，展示口径完全一致（数据都来自同一 `task_info` 字段）：

- **「状态」列**：`components/table/usage-logs/UsageLogsColumnDefs.jsx` 新增列——Semi Design `Tag`（状态配色对齐 `task-logs/TaskLogsColumnDefs.jsx` 的状态映射）+ `Progress` 组件（参照 TaskLogsColumnDefs.jsx:445 的用法）+ 百分比。
- **费用列**：同默认主题三态分支（生成中/实扣净额/$0+已退款）。
- **详情/计费过程**：`helpers/render.jsx` 的视频计费渲染（renderTaskBillingProcess 等）扩展同样的 seedance 任务区块字段（档位/秒数/参考视频/输出 tokens/专属倍率/失败原因/任务 ID/上游 ID admin）。
- i18n：`web/classic/src/i18n/` 按其现有惯例补 key。
- 构建验证：classic 使用 Vite（`web/classic` 内 `bun run build` 或其 package.json 实际脚本）。

### 3.5 账单导出逐日明细合并（`controller/bill_detail_excel.go`）

`alignTaskPairs` 已把同 `request_id` 行相邻排列；在其后新增 `mergeSeedanceTaskRows`：模型含 seedance 且同 request_id 的 2~3 行折叠为主行——费用列写净额（消费−退款），「计费过程」单元格按时间序拼接各行 `buildBillingText` 全文（`预扣…\n实际结算…\n退款…`），类型列显示「消费」。其他模型行为不变。

## 4. 测试计划（TDD）

1. `model`：列表排除（settle/refund 隐藏、pre_consume 保留、非 seedance 不受影响、count 一致）；增强（成功/失败/进行中/task 被清理四态、admin/self 的 UpstreamTaskId 差异、批量查询不 N+1——用查询计数或直接断言单批 IN）。
2. 回填迁移：三种 stage 推断、幂等（二跑无变更）、request_id 缺失跳过、坏 JSON 容错。
3. 增强：生成秒数/输出 tokens 从 `task.Data` 解析的各形态用例。
4. `controller`：逐日明细合并（2 行/3 行/失败全退、净额、过程文字完整、非 seedance 不合并）。
5. 前端：默认主题 `bun run build` + classic 主题构建均通过；两主题状态列/费用列分支的组件渲染逻辑走查。
6. 回归：账单汇总导出（v5 全套 sheet 数值不变）、`go build ./...`、bill 相关全部既有测试。

## 5. 不做的事

- 不删除/不合并 DB 中的 settle/refund 行；不改任务日志页；不做前端自动轮询；不动非 seedance 任务（Grok 视频等）；不动受保护标识（Rule 5）。

## 6. 分阶段实施

1. **Phase 1**：`model` 列表排除 + LogTaskInfo 增强（含秒数解析，含测试）。
2. **Phase 2**：历史回填迁移（含测试）。
3. **Phase 3**：默认主题前端（状态列/费用列/详情弹窗 + i18n + build）。
4. **Phase 4**：Classic 主题前端（同口径列与详情改造 + i18n + build）。
5. **Phase 5**：逐日明细合并 + 全量回归。
