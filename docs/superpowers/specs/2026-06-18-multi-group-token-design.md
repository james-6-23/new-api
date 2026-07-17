# 多分组令牌（跨分组调用）设计文档

- 日期：2026-06-18
- 主题：令牌（Token）支持绑定多个分组，实现一个令牌跨分组调用
- 状态：待评审

## 1. 背景与目标

### 背景

当前 new-api 中，一个令牌只能绑定**单个**分组（`Token.Group` 为单个字符串）。
若用户既想调用 `claude` 分组又想调用 `gpt` 分组，必须创建两个令牌，分别绑定不同分组，
使用上割裂。

### 目标

让令牌在创建/编辑时可以**选择多个分组**，请求时按用户设定的**顺序**遍历这些分组选择渠道：
某分组没有该模型的可用渠道、或调用失败时，自动切换到下一个分组（跨分组重试）。
这样一个令牌即可同时调用 claude 与 gpt，无需重新创建令牌。

### 非目标（YAGNI）

- 不做"自动选价格最低分组"等复杂调度策略。
- 不新增数据库列、不做数据迁移。
- 不改变现有单分组令牌、空分组令牌、`auto` 模式令牌的任何行为。

## 2. 关键设计决策（已与用户确认）

| 决策点 | 选择 | 说明 |
| --- | --- | --- |
| 多分组都能服务某模型时如何选分组 | **按用户设定顺序遍历 + 跨分组重试** | 复用现有 `auto` 模式逻辑；计费按实际命中的分组 |
| 产品形态 | **分组选择框改为多选** | 0 个=回退用户默认；1 个=等价单分组；≥2 个=跨分组模式 |
| 存储方式 | **复用 `group` 字段存逗号分隔字符串** | 例 `"claude,gpt"`；零迁移、三库通用、向后兼容 |
| 跨分组重试开关 | **多选 ≥2 个时自动启用并隐藏开关** | `auto` 模式保留原有开关 |
| `auto` 与具体分组 | **互斥** | 选 `auto` 不能再选具体分组，反之亦然 |

## 3. 数据与存储

- `Token.Group` 字段**类型不变**（数据库仍为 string/varchar 列），语义升级为
  "逗号分隔的有序分组名"，例如 `"claude,gpt"`。
- 向后兼容：
  - `"vip"` → 解析为 `["vip"]`（单分组行为不变）
  - `""` → 空列表（回退用户默认分组，行为不变）
  - `"auto"` → 解析为 `["auto"]`（auto 模式不变）
- 在 `model/token.go` 新增辅助方法：
  - `func (token *Token) GetGroups() []string`：按逗号 split、`TrimSpace`、过滤空项后返回有序列表。
- 写入：controller 把前端传来的分组数组用 `strings.Join(groups, ",")` 拼成字符串存库。
- 交互边界：DTO/前端用**数组**，存储层用**字符串**，转换集中在 controller。
- 约束：分组名不得包含逗号（与现有分组命名规则一致），无需额外迁移或约束。
- 兼容性：底层仍是普通 TEXT/varchar，满足 SQLite/MySQL/PostgreSQL 三库通用（CLAUDE.md Rule 2）。

## 4. 后端校验（`controller/token.go`）

令牌创建（`AddToken`）/更新（`UpdateToken`）时新增校验：

1. 将传入分组解析为列表。
2. 逐个校验每个分组都在该用户可用分组 `service.GetUserUsableGroups(userGroup)` 内；
   越权分组报错。
3. `auto` 互斥校验：若列表包含 `auto`，则列表长度必须为 1；否则报错
   "auto 分组不能与其他分组同时选择"。
4. 校验通过后 `strings.Join(",")` 存库。
5. 错误文案走后端 i18n（`i18n/`，en/zh）。

> 说明：当前创建/编辑对 group 几乎无服务端校验，本次顺带补齐，保证多分组场景安全。

## 5. 请求分发与跨分组遍历

### 5.1 认证中间件（`middleware/auth.go`）

当前逻辑：`token.Group != ""` 时校验单值并用其覆盖 `userGroup`。

改为基于 `token.GetGroups()` 分流：

- **0 个**：维持现状，使用 `userGroup`。
- **含 `auto`**（此时长度必为 1）：维持现有 auto 流程不变。
- **1 个具体分组**：等价现状，校验后用其覆盖 `userGroup`，写入 `ContextKeyUsingGroup`。
- **≥2 个具体分组**：进入**多分组模式**：
  - 逐个校验每个分组在用户可用分组内（沿用现有校验逻辑，扩展为遍历）。
  - 将有序分组列表写入新的 context key `ContextKeyTokenGroups`。
  - 将 `ContextKeyUsingGroup` 设为列表第一个分组（作为初始/默认分组）。
  - 视为开启跨分组重试。

### 5.2 渠道选择（`service/channel_select.go` 的 `CacheGetRandomSatisfiedChannel`）

现状：`param.TokenGroup == "auto"` 分支遍历 `GetUserAutoGroup(userGroup)`（全局自动分组列表）。

重构：

1. 抽出一个内部"按有序分组列表遍历选渠道"的逻辑（输入：有序分组列表）。
2. **模式判定（触发条件）**：在 `CacheGetRandomSatisfiedChannel` 入口处先读取
   `ContextKeyTokenGroups`：
   - 若存在且元素 ≥2 → 走**多分组遍历分支**（列表 = 该 context 值）。
   - 否则若 `param.TokenGroup == "auto"` → 走 **auto 遍历分支**（列表 = `GetUserAutoGroup(userGroup)`）。
   - 否则 → 走原**单分组分支**（直接用 `param.TokenGroup`）。
   - 两个遍历分支复用同一内部函数，仅分组列表来源不同。
3. 遍历语义沿用 auto：
   - 按序找第一个"该模型存在可用渠道"的分组并选渠道。
   - 该分组渠道失败时，`ContextKeyAutoGroupIndex` 前进到下一个分组重试。
4. 计费分组（返回值 `selectGroup`）= 实际命中的那个分组，确保按命中分组的
   group ratio 计费（对应决策：按命中分组计费）。

### 5.3 Context Key（`constant/context_key.go`）

- 新增 `ContextKeyTokenGroups`：存解析后的有序分组列表（`[]string`）。
- 复用现有 `ContextKeyAutoGroupIndex` 等遍历状态 key。

## 6. 前端（`web/default` 与 `web/classic`）

两套前端均需改动（default: React19；classic: React18）：

- 令牌编辑弹窗的分组 `Form.Select` 增加 `multiple`，值类型改为数组。
- 提交时数组 `join(',')`；回显时把字符串 `split(',')` 成数组。
- `auto` 与具体分组**互斥**：选中 `auto` 时禁用其它选项，选中具体分组时禁用 `auto`
  （前端交互校验 + 后端兜底校验）。
- 跨分组重试开关：
  - 选 `auto` 时保留原开关行为。
  - 选 ≥2 个具体分组时隐藏开关，并提示"已自动启用跨分组重试"。
- i18n：新增/调整文案走前端 i18next（`web/default/src/i18n/locales/`，zh/en 为主，
  其余语言按现有 `bun run i18n:sync` 流程）。

## 7. 测试与验证

### 单元测试（Go）

- `Token.GetGroups()` 解析：空串、单值、多值、含空格、`auto`、含空项。
- controller 校验：越权分组被拒、`auto` 与其它分组同选被拒。
- `channel_select` 多分组遍历：命中第一个分组、第一个分组无渠道时回退第二个、
  全部无渠道时报错。
- 计费分组正确性：返回的 `selectGroup` 为实际命中的分组。

### 手动验证

1. 建一个绑 `claude,gpt` 的令牌。
2. 请求只在 `claude` 分组配了渠道的模型 → 命中 claude，计费按 claude。
3. 请求只在 `gpt` 分组配了渠道的模型 → 命中 gpt，计费按 gpt。
4. 两分组都配了同名模型 → 命中顺序靠前的分组。
5. 第一个分组该模型暂时无可用渠道 → 自动回退到第二个分组。

### 回归

- 老令牌（单分组 / 空 / `auto`）行为完全不变。
- 三库（SQLite/MySQL/PostgreSQL）均通过。

## 8. 涉及文件清单（预估）

- `model/token.go`：新增 `GetGroups()`；`Update()` select 列不变（仍含 `group`）。
- `controller/token.go`：`AddToken`/`UpdateToken` 校验与数组↔字符串转换。
- `middleware/auth.go`：多分组分流逻辑。
- `service/channel_select.go`：抽取共用遍历逻辑，多分组模式接入。
- `constant/context_key.go`：新增 `ContextKeyTokenGroups`。
- `i18n/`：后端错误文案（en/zh）。
- `web/default/src/...`：令牌编辑组件多选改造 + i18n。
- `web/classic/src/components/table/tokens/modals/EditTokenModal.jsx`：多选改造 + i18n。

## 9. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| 误改协议/计费导致老令牌行为变化 | 严格按 `GetGroups()` 长度分流，单值/空/auto 全部走原路径；补回归测试 |
| 多分组遍历与 auto 逻辑耦合出错 | 抽取共用函数、两模式仅列表来源不同，集中测试 |
| 计费走错分组 | `selectGroup` 始终取实际命中分组，单测覆盖 |
| 分组名含逗号 | 与现有命名规则一致，禁止；不引入新风险 |
