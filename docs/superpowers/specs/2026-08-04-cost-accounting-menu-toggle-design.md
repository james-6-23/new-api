# 成本核算：菜单归位与功能开关

日期：2026-08-04
状态：设计已确认，待实现

## 背景

成本核算（Cost Accounting）功能已上线，但存在两个问题：

1. **菜单位置不合理**。成本核算的数据来源是渠道成本，语义上紧贴渠道管理，但两个前端主题都把它排在管理组的尾部（default 在订阅管理之后，classic 在系统设置之后），与渠道管理隔了三到五个条目。
2. **无法关闭**。该功能对 Root/超级管理员始终可见可用，没有站点级别的总开关。

## 目标

- 两个主题的侧边栏中，「成本核算」紧跟在「渠道管理」下一行（平级，不做嵌套子菜单）。
- 运营设置中新增「成本核算」开关，默认开启。关闭后：侧边栏入口消失，且 `/api/cost/*` 接口直接拒绝。

## 非目标

- 不改成本核算页面本身的任何业务逻辑、查询或展示。
- 不改 `/api/cost/*` 现有的 Root 权限要求（开关是在权限之上的额外门禁，不是替代）。
- 不引入 `setting/operation_setting` 结构化配置容器（见「方案选择」）。

## 方案选择

考虑过两种存储方式：

**A. 复用 `common` 全局变量 + status 下发**（采纳）
与既有的 `DrawingEnabled` / `TaskEnabled` / `DataExportEnabled` 完全同构。零新概念，读写路径与另外六个同类开关一致。

**B. 新建 `setting/operation_setting/cost_setting.go` 结构体**（否决）
为单个 bool 引入一层结构，前端还要走 `xxx_setting.enabled` 点号 key，且不会自动出现在 `/api/status`，需单独下发。当前只有一个开关，属于 YAGNI。将来成本核算真要加第二、第三个配置项时再迁移，迁移成本仅为一个 option key 的搬家。

## 架构

开关是一个布尔 option，存于 `options` 表，键名 `CostAccountingEnabled`，默认 `true`。

```
运营设置表单 (两主题)
    │  PUT /api/option  {key: "CostAccountingEnabled", value: "true"|"false"}
    ▼
model/option.go  updateOptionMap  ──►  common.CostAccountingEnabled (进程内全局)
    │                                        │
    │                                        ├──►  middleware.CostAccountingEnabled()
    │                                        │        挂在 /api/cost 路由组，关闭时 403
    │                                        │
    └──►  GET /api/status  cost_accounting_enabled  ──►  两主题侧边栏显隐
```

单一数据源是 `common.CostAccountingEnabled`；接口拦截与菜单显隐都由它派生，不存在两处独立配置漂移的可能。

## 后端改动

### 1. `common/constants.go`

在 `DataExportEnabled` 附近新增：

```go
var CostAccountingEnabled = true
```

### 2. `model/option.go`

- `InitOptionMap`：`common.OptionMap["CostAccountingEnabled"] = strconv.FormatBool(common.CostAccountingEnabled)`
- `updateOptionMap` 的 switch：`case "CostAccountingEnabled": common.CostAccountingEnabled = boolValue`

两处紧邻 `DataExportEnabled` 的现有行，保持同类开关聚拢。

### 3. `controller/misc.go`

status 响应 map 中新增：

```go
"cost_accounting_enabled": common.CostAccountingEnabled,
```

`/api/status` 是公开接口。多下发一个功能开关布尔值不泄露敏感信息——`enable_drawing`、`enable_task`、`enable_data_export` 已是同样处理。

### 4. `middleware/cost_accounting.go`（新建）

导出 `CostAccountingEnabled() gin.HandlerFunc`。当 `common.CostAccountingEnabled` 为 `false` 时，以 403 中止：

```go
c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
    "success": false,
    "message": <i18n 文案>,
})
```

否则 `c.Next()`。错误文案走 `i18n/` 现有机制，en/zh 各加一条。

### 5. `router/api-router.go`

`costRoute` 路由组挂载中间件，**位置在 `RootAuth()` 之前**：

```go
costRoute := apiRouter.Group("/cost")
costRoute.Use(middleware.CostAccountingEnabled())
costRoute.Use(middleware.RootAuth())
```

顺序的理由：功能关闭时，任何调用方（包括未登录的探测）得到的都是一致的「功能已关闭」，而不是先暴露一个权限错误；同时省掉一次 session/token 鉴权开销。

## 前端改动 — default 主题

### 菜单移位与显隐

`web/default/src/hooks/use-sidebar-data.ts`：把 `Cost Accounting` 条目从订阅管理之后移到 `Channels` 之后、`Models` 之前。展开条件由 `isSuperAdmin` 改为 `isSuperAdmin && costAccountingEnabled`。

开关值来自 `useStatus()` / 系统配置，sidebar 已在客户端且 status 已缓存到 localStorage，不产生额外请求。

### 系统配置映射

`web/default/src/hooks/use-system-config.ts` 的 `mapStatusDataToConfig` 新增：

```ts
costAccountingEnabled: data.cost_accounting_enabled ?? true,
```

`?? true` 是关键：旧版后端或 status 尚未返回该字段时默认放行，避免升级过程中菜单误消失。`SystemConfig` 类型同步加字段。

### 路由守卫

`web/default/src/routes/_authenticated/cost/index.tsx` 的 `beforeLoad` 已有 role 检查，在同一处补上开关判断，关闭时同样 `redirect({ to: '/403' })`。

这不是安全边界（安全边界是后端中间件），只是避免用户先看到页面骨架再吃 403。因为该 `beforeLoad` 块已存在，增量仅一个条件。

### 运营设置开关

`web/default/src/features/system-settings/general/system-behavior-section.tsx`：在 `SelfUseModeEnabled` 之后追加第四个 `Switch`，字段名 `CostAccountingEnabled`。

连带同步四处：

| 文件 | 改动 |
|---|---|
| 同文件 `behaviorSchema` | 加 `CostAccountingEnabled: z.boolean()` |
| `system-settings/types.ts` | `OperationsSettings` 加 `CostAccountingEnabled: boolean` |
| `system-settings/operations/section-registry.tsx` | behavior section 的 `defaultValues` 传入该字段 |
| `system-settings/operations/index.tsx` | `defaultOperationsSettings` 加 `CostAccountingEnabled: true` |

### i18n

新增两个 key（开关标题、开关描述）。`Cost Accounting` 已存在，不重复添加。写入 `web/default/src/i18n/locales/` 下 en / zh / fr / ru / ja / vi 六个文件。

## 前端改动 — classic 主题

### 菜单移位与显隐

`web/classic/src/components/layout/SiderBar.jsx`：`adminItems` 数组中 `cost` 条目从末尾移到 `channel` 之后。

可见性在现有 `className: isRoot() ? '' : 'tableHiddle'` 之外，增加 status 标志判断，读法与 `enable_drawing` 的既有写法一致：

```js
localStorage.getItem('cost_accounting_enabled') !== 'false'
```

用 `!== 'false'` 而非 `=== 'true'`，同样是为了字段缺失时默认放行。该读取需加入 `useMemo` 依赖数组。

`web/classic/src/helpers/data.js` 的 `setStatusData` 新增：

```js
localStorage.setItem('cost_accounting_enabled', data.cost_accounting_enabled);
```

### 运营设置开关

`web/classic/src/pages/Setting/Operation/SettingsGeneral.jsx`：在含 `SelfUseModeEnabled` 的那个 `<Row gutter={16}>` 中追加一个 `<Col xs={24} sm={12} md={8} lg={8} xl={8}>` + `Form.Switch`，字段 `CostAccountingEnabled`，`onChange={handleFieldChange('CostAccountingEnabled')}`。

`web/classic/src/components/settings/OperationSetting.jsx` 的 `inputs` 初始值加 `CostAccountingEnabled: true`。

### 侧边栏模块配置补齐

`web/classic/src/pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx` 的 admin 区域模块列表中**缺少** `cost` 条目，但 `SiderBar.jsx` 对每一项都执行 `isModuleVisible('admin', item.itemKey)`。结果是成本核算的显隐由一个管理员看不见的默认值决定，而其余七个管理项均可配置。

该默认值已确认为放行：`web/classic/src/hooks/common/useSidebar.js` 的 `DEFAULT_ADMIN_CONFIG.admin` 中含 `cost: true`，且 `mergeAdminConfig` 以保存的配置覆盖默认值——保存的 JSON 中缺少 `cost` 键时仍取默认的 `true`。因此当前行为是「可见但不可配」。

本次在该列表中补上 `{ key: 'cost', title: t('成本核算'), description: t('渠道成本统计') }`，使其与另外七项一样可配。这是修补当前改动所触及代码中的既有缺口，不是新增功能。既有部署的配置 JSON 无需迁移。

### i18n

classic 的 key 为中文源串，需在其 8 个语言文件中补充译文。

## 兼容性

- **旧数据库**：`options` 表无 `CostAccountingEnabled` 行时，`InitOptionMap` 写入默认 `true`，行为与升级前一致。
- **旧前端 + 新后端**：status 多一个字段，旧前端忽略，菜单照常显示。
- **新前端 + 旧后端**：status 无该字段，两个主题均因默认放行逻辑（`?? true` / `!== 'false'`）保持菜单可见。
- **数据库兼容**：本改动不新增表或列，仅用既有 `options` 键值表，SQLite / MySQL / PostgreSQL 三库无差异。

## 测试

**后端**
- `middleware/cost_accounting_test.go`：开关为 `true` 时中间件放行（`c.Next()` 被调用）；为 `false` 时返回 403 且请求被 `Abort`。
- `go build ./...` 与 `go vet ./...` 通过。

**前端**
- default：`bun run build`（含 TypeScript 类型检查）通过。
- classic：构建通过。

**手工验证四条路径**
1. 开关关闭 → 两个主题侧边栏「成本核算」消失。
2. 开关关闭 → `curl /api/cost/overview` 返回 403。
3. 开关重新开启 → 菜单与接口均恢复。
4. 两个主题中「成本核算」显示在「渠道管理」正下方。

## 风险

主要风险是**遗漏同步点**：default 主题的表单字段分散在 schema、types、section-registry、index 四个文件，漏改任一处会导致开关不显示或保存后被默认值覆盖。实现时按上表逐项核对。

classic 侧边栏模块配置的默认放行行为已在实现前核实（见上节），无遗留未知项。
