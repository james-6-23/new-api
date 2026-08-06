# seedance 展示与统计 v2 — 状态环 UX、统计口径修复、请求参数真实值、Excel 精简、账单页重做

- 日期: 2026-08-02
- 状态: 已实现（2026-08-02，含测试）
- 前置: `2026-08-01-seedance-usage-log-single-row-design.md`（单行化 + task_info 增强）、`2026-07-30-bill-export-v2/v3/v4`、`2026-08-01-bill-export-v5-reference-layout-design.md`
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 2 (三库兼容)、Rule 5 (受保护标识)

## 1. 需求（用户原话拆解）

1. 状态字段放到「详情」前一位，普通用户也可见；现样式丑——进度条换**圆形**，环中显示 `50%` 百分比，显示**中文状态**，100% 即成功。
2. 排查系统中所有 seedance 金额统计：数据面板模型消耗柱状图只记了预扣、未算退费，数字虚高。
3. 详情里「分辨率档位 480p / 720p」不对——要显示**用户请求的真实值**：`"resolution":"720p"`、`"ratio":"16:9"`、`"duration":5` 三个请求参数都要显示；duration 目前不显示。
4. 输出 tokens 要在使用记录的「输出 tokens」列展示。
5. 账单导出 Excel sheet 太多，精简为简单明了（已确认方案 A）。
6. 账单管理前端页仿使用日志样式重做，新老前端都要。

## 2. 排查结论（已完成，作为设计依据）

### 2.1 统计虚高根因

结算差额与退款只写日志行，从不回写三个计数器：

| 统计面 | 数据源 | 现状 |
|---|---|---|
| 数据面板柱状图 | `quota_data`（仅 `RecordConsumeLog`→`LogQuotaData` 写入，即只有预扣） | **虚高**（漏退款也漏结算补扣） |
| 用户已用额度 | `user.used_quota`（预扣+补扣时加，退款不减） | **虚高** |
| 渠道已用额度 | `channel.used_quota`（同上） | **虚高** |
| 使用日志消耗额度 | `SumUsedQuota` 查询时净额重算 | 正确 |

### 2.2 请求参数存储

用户原始请求 JSON 完整存于 `tasks.properties.input`（提交时落库，32KB 上限，超限存 `{"_truncated":true}`）；`resolution`/`ratio` 等顶层未知字段会被 `promoteUnknownFieldsToMetadata` 收进 `metadata.*`。sora 通道用 `size`+`seconds`。现有 `parseTaskVideoMeta` 只解析上游最终响应 `task.Data`，因此 duration 常缺失、分辨率只有计费档位。**历史任务无需迁移即可从 Properties.Input 恢复。**

## 3. 设计

### 3.1 状态列 UX（两前端）

- 列位置：移至「详情」列前一位；普通用户默认可见（default 主题从列可见性默认值、classic 主题从 `getDefaultColumnVisibility` 均置 true——现已如此，仅调位置）。
- 样式：**圆形进度环**，环心显示百分比文字（`50%`）；环旁显示中文状态（排队中/生成中/成功/失败/未知）。配色：SUCCESS=绿、FAILURE=红、进行中=蓝、排队=橙/黄、未知=灰。SUCCESS 恒显 100%。
  - default 主题：自绘小型 SVG 圆环组件（半径 ~14px，stroke 按状态色），文件放 `features/usage-logs/components/task-progress-ring.tsx`；详情弹窗状态行复用同组件小尺寸。
  - classic 主题：Semi `<Progress type='circle' size='small' percent={pct} stroke={色}/>`，`format` 显示百分比，右侧中文状态 Tag 保留但去掉线性进度条。
- 数据不变（`task_info.status/progress`）。

### 3.2 统计口径修复（后端）

写入侧对称化（`service/task_billing.go`）：

- **结算补扣**（`RecalculateTaskQuota` delta>0）：现有 user/channel 回写保留，**补写 `model.LogQuotaData(delta)`** 镜像（复用 `RecordConsumeLog` 内相同的参数组装：userId/username/modelName/createdAt/delta/0 token）。
- **退款**（`RefundTaskQuota` 全额 与 delta<0 分支）：新增三处回冲——
  - `model.UpdateUserUsedQuotaDelta(userId, -refund)`：新增的仅调 used_quota、不动 request_count 的变体（请求数在预扣时已计 1，退款不应减请求数）；
  - `model.UpdateChannelUsedQuota(channelId, -refund)`（现函数支持负值即复用，不支持则同样加变体）；
  - `model.LogQuotaData(-refund)` 负值镜像（`quota_data.quota` 为 int，`sum` 自然净额；count 传 0，不虚减请求计数）。
- 数据面板 `sum(quota)`、用户/渠道已用额度即成净额口径。
- **历史数据不回填**（统计表按小时聚合，历史修正成本高；只修今后）。
- 非 seedance 的其它任务（同走这两个函数的）一并受益——修的是通用任务计费路径。

### 3.3 请求参数真实值（后端增强）

`model/log_task_enrich.go`：

- `LogTaskInfo` 增加 `Resolution string`（"720p"）、`Ratio string`（"16:9"）；`DurationS` 语义改为**请求时长优先**。
- 新增 `parseTaskRequestParams(input string)`：解析 `Properties.Input` JSON——顶层 `resolution`/`ratio`/`duration`/`seconds`、嵌套 `metadata.resolution`/`metadata.ratio`；sora 形态从 `size`（如 "1280x720"→ratio 约减为 "16:9"，resolution 取短边 "720p"）与 `seconds` 推导；`_truncated` 或解析失败返回零值。
- 增强优先级：Properties.Input（请求值）> task.Data（上游响应兜底，现逻辑保留）。
- 详情展示（两前端）：`分辨率 720p`、`宽高比 16:9`、`时长 5s` 三行替换现「分辨率档位」行；计费档位（base/1080p/4k）仅保留在计费公式上下文中。历史任务自动生效。

### 3.4 输出 tokens 落列（两前端）

使用记录列表「输出 tokens」列：行有 `task_info.output_tokens` 且列原值为 0 时显示该值。导出侧上一轮已落列（`applySeedanceExportMerge`），本轮仅补列表展示。

### 3.5 Excel 导出精简（方案 A）

- 固定 sheet 收敛为 3 个：**账单汇总**（封面，不变）、**账单明细**（原「明细对账单」改名，internal/external 布局与列不变）、**按模型汇总**（不变）；`with_detail=1` 时追加逐日明细 sheets。
- **删除**：总对账单、按日汇总、按令牌汇总（生成逻辑、布局、`finalizeBillWorkbook` 顺序表、相关测试同步删改）。
- 激活 sheet 仍为账单汇总；顺序：账单汇总 → 账单明细 → 按模型汇总 → 逐日明细。
- 网页查询 API 不动。

### 3.6 账单管理前端页重做（两前端，仿使用日志样式）

- **default**（`features/bill-management/`）：重写为使用日志同款骨架——顶部筛选工具栏（时间范围、令牌、模型、粒度、汇率；管理员加用户/渠道）、`data-table` 风格表格（列头/斑马纹/分页与使用日志一致）、汇总行以统计条形式置于表格上方（对齐使用日志页的统计区样式）、「导出对账单」「包含逐日明细」等收进工具栏右侧。金额列复用使用日志费用列的徽章样式。
- **classic**（`bill-summary/`）：同步仿其使用日志页——Semi Form 筛选工具栏 + Semi Table（列样式对齐 UsageLogsTable），导出按钮进工具栏。
- 查询字段沿用现有 API（`billing_records/request_count/list_amount_usd` 已具备）。

## 4. 测试计划（TDD）

1. 统计修复：settle 补扣写 quota_data；退款回冲 user/channel/quota_data 三处（负值镜像、request_count 不动）；非任务路径回归。
2. 请求参数：Properties.Input 顶层/metadata 嵌套/sora size+seconds 推导/_truncated 兜底/Input 优先于 Data 的用例。
3. Excel 精简：3 固定 sheet 存在、被删 sheet 不存在、顺序与激活、逐日明细开关回归；被删 sheet 的既有测试移除。
4. 输出 tokens 列与状态环为前端展示逻辑：两主题 build 通过 + 渲染分支走查。
5. 全量：`go build ./...`、`go test ./model/ ./controller/ ./service/ ./pkg/billingexpr/`（除既有无关失败）、两前端 build。

## 5. 不做的事

- 不回填历史 quota_data/used_quota；不改任务日志页；不加前端轮询；不动查询 API 契约（只增不减）；不动受保护标识（Rule 5）。

## 6. 分阶段实施

1. **P1** 统计口径修复（service/model，TDD）。
2. **P2** 请求参数真实值增强（model，TDD）。
3. **P3** Excel 精简（controller，TDD）。
4. **P4** default 前端：状态环列位/输出 tokens 列/详情参数行/账单页重做。
5. **P5** classic 前端同步。
6. **P6** 全量回归 + spec 收尾。
