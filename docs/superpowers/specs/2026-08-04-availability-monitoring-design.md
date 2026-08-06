# 可用性监控（Availability Monitoring）

日期：2026-08-04
状态：设计已确认，待实现

## 背景

站点目前没有一个面向管理员的「服务可用性」总览页。已有的观测能力是碎片化的：

- `pkg/perf_metrics` + `model/perf_metric.go` 已按 **模型 + 分组 + 小时桶** 聚合了真实中转流量的成功率、TTFT、吞吐等指标，但只在「模型广场 / 性能指标」中按单模型展示。
- `controller/uptime_kuma.go` 可代理外部 Uptime Kuma 状态页，依赖外部部署。
- `controller/channel-test.go` 的自动测试只保留**最后一次**探测的标量（`TestTime` / `ResponseTime` / `Status`），没有历史时间序列。

参考实现 <https://live-demo.cch-plus.com/status> 提供了一个更合适的形态：以**分组 / 模型两个维度**呈现聚合可用性，而非逐渠道的上下线灯。本设计对齐该形态。

## 目标

- 新增「可用性监控」页面，两个前端主题（default / classic）均实现，UI 风格对齐参考页。
- 侧边栏新增独立菜单项，位于「成本核算」之下、「模型管理」之上。
- 运营设置中新增「可用性监控」开关，默认开启。关闭后：两个主题菜单入口消失，且 `/api/status/availability*` 接口直接拒绝。
- 数据完全复用既有 `perf_metrics`，不新增采集器、不新增表、不做数据库迁移。

## 非目标

- **不**新增渠道主动探测（probe）。空闲模型表现为「无数据」，而非「可用」。这是复用 `perf_metrics` 的自然结果，已确认接受。
- **不**实现 `cacheRate` 指标。参考页的第四个指标是缓存命中率，本仓库 `perf_metrics` 无缓存 token 字段；以既有的 `latency`（平均总时延）替代，避免改动中转热路径。
- **不**实现参考页的 SSE 实时流。实时 RPM 改为轻量轮询（见「实时 RPM」）。
- **不**提供 24h 以外的时间范围切换。固定 24 小时、小时粒度。
- **不**提供未登录可访问的公开 `/status` 页。本功能仅管理员可见。
- **不**修改 `perf_metrics` 既有的采集逻辑、表结构或 `/api/perf-metrics` 接口。

## 方案选择

### 数据来源：复用 `perf_metrics`（采纳）

`perf_metrics` 表已含本页所需的全部原始计数器，且默认按小时分桶（`perf_metrics_setting.BucketTime` 默认 `"hour"`），与参考页的小时 X 轴天然对齐。

否决的替代方案：**新建可用性探测表 + 后台探测任务**。它能在零流量时给出真实 uptime，但会重复 `perf_metrics` 的同一批数字、消耗上游配额，且产出的是「逐渠道上下线」而非参考页的「分组 / 模型」形态。

### 聚合时机：请求时聚合 + 60 秒进程内缓存（采纳）

每次请求跑一条针对最近 24 小时的分组 SQL，在 Go 内透视成响应结构，按维度缓存 60 秒。

否决的替代方案：**后台任务预计算快照**。前端本就是 `staleTime` 60s + `refetchInterval` 5min，60 秒服务端缓存已把真实查询频率压到约每分钟一次，稳态开销与预计算相同；而预计算要额外引入 `IsMasterNode` 门禁的后台任务，且首次加载也可能拿到 5 分钟前的数据。

## 架构

```
中转请求 ──► pkg/perf_metrics (内存桶) ──flush──► perf_metrics 表（模型+分组+小时桶）
                    │                                        │
                    │                                        │ 请求时分组聚合
              rolling 60s RPM 计数器                          ▼
                    │                            model/availability.go  透视
                    │                                        │
                    ▼                                        ▼
   GET /api/status/availability/rpm        GET /api/status/availability?dimension=
                    │                                        │
                    └──────────────┬─────────────────────────┘
                                   ▼
              middleware.AvailabilityMonitorEnabled() → middleware.AdminAuth()
                                   │
                    ┌──────────────┴──────────────┐
                    ▼                             ▼
            web/default 可用性监控页        web/classic 可用性监控页
```

开关沿用「成本核算」已验证的同构路径，单一数据源是 `common.AvailabilityMonitorEnabled`：

```
运营设置表单 (两主题)
    │  PUT /api/option  {key: "AvailabilityMonitorEnabled", value: "true"|"false"}
    ▼
model/option.go  updateOptionMap  ──►  common.AvailabilityMonitorEnabled
    │                                        │
    │                                        └──► middleware.AvailabilityMonitorEnabled()
    │                                                挂在可用性路由，关闭时 403
    └──►  GET /api/status  availability_monitor_enabled  ──►  两主题侧边栏显隐
```

## 后端改动

### 1. 开关（完全对齐 `CostAccountingEnabled` 的六处同步）

| 文件 | 改动 |
|---|---|
| `common/constants.go` | `var AvailabilityMonitorEnabled = true`，紧邻 `CostAccountingEnabled` |
| `model/option.go` `InitOptionMap` | `common.OptionMap["AvailabilityMonitorEnabled"] = strconv.FormatBool(common.AvailabilityMonitorEnabled)` |
| `model/option.go` `updateOptionMap` switch | `case "AvailabilityMonitorEnabled": common.AvailabilityMonitorEnabled = boolValue` |
| `controller/misc.go` `GetStatus` | `"availability_monitor_enabled": common.AvailabilityMonitorEnabled,` |
| `middleware/availability_monitor.go`（新建） | 导出 `AvailabilityMonitorEnabled() gin.HandlerFunc`，关闭时 403 |
| `i18n/` en + zh | **无需改动**：已确认 `i18n/keys.go:17` 的 `MsgFeatureDisabled = "common.feature_disabled"` 正是 `middleware/cost_accounting.go` 使用的通用文案，直接复用 |

`/api/status` 是公开接口，多下发一个功能开关布尔值不泄露敏感信息——`enable_drawing`、`cost_accounting_enabled` 已是同样处理。

### 2. `router/api-router.go`

挂在既有 `/status/test` 附近，与之保持一致的 `AdminAuth()` 权限，并保留参考页的原始 URL：

```go
availabilityRoute := apiRouter.Group("/status/availability")
availabilityRoute.Use(middleware.AvailabilityMonitorEnabled())
availabilityRoute.Use(middleware.AdminAuth())
{
    availabilityRoute.GET("", controller.GetAvailabilityStatus)
    availabilityRoute.GET("/rpm", controller.GetAvailabilityRpm)
}
```

中间件顺序：**开关在鉴权之前**。功能关闭时任何调用方（含未登录探测）得到一致的「功能已关闭」，而不是先暴露权限错误，同时省掉一次鉴权开销。

已确认 Gin 路由不冲突：`apiRouter.GET("/status", ...)` 与 `apiRouter.GET("/status/test", ...)` 已共存，`/status/availability` 属同一形态。

### 3. `model/availability.go`（新建）

一条分组查询覆盖最近 24 小时：

```go
DB.Model(&PerfMetric{}).
    Select("model_name, " + commonGroupCol + " as group_name, bucket_ts, " +
        "SUM(request_count) as request_count, SUM(success_count) as success_count, " +
        "SUM(total_latency_ms) as total_latency_ms, SUM(ttft_sum_ms) as ttft_sum_ms, " +
        "SUM(ttft_count) as ttft_count, SUM(output_tokens) as output_tokens, " +
        "SUM(generation_ms) as generation_ms").
    Where("bucket_ts >= ?", startTs).
    Group("model_name, " + commonGroupCol + ", bucket_ts").
    Find(&rows)
```

- `group` 是三库保留字，必须经既有的 `commonGroupCol` 变量引用（CLAUDE.md 规则 2）；结果列改名为 `group_name` 以避开扫描期的再次转义。
- 仅用 GORM 构造器与 `SUM`/`GROUP BY`（三库通用），不使用任何方言特有函数或操作符。
- 不新增表或列，故 `model/main.go` 的 `migrateDB` / `migrateDBFast` **均无需改动**。

### 4. 透视与派生指标

维度映射：

- `dimension=group` → 每个分组一个 entity，其子线是该分组下的各模型
- `dimension=model` → 每个模型一个 entity，其子线是承载它的各分组

派生公式（逐桶、逐子线）：

| 指标 | 公式 | 更优方向 |
|---|---|---|
| `successRate` | `success_count / request_count × 100` | 高 |
| `ttft` | `ttft_sum_ms / ttft_count` | **低** |
| `tps` | `output_tokens / (generation_ms / 1000)` | 高 |
| `latency` | `total_latency_ms / request_count` | **低** |

因此虚线 `best` 包络线对 `successRate` / `tps` 取子线 **max**，对 `ttft` / `latency` 取 **min**。

分母为 0 的桶输出 `null` 而非 `0`——图表应断线，而不是塌到零造成「性能骤降」的误读。

**`current`** 取**最近 3 个小时桶**的合计，而非最新单桶。刚开始的小时桶可能只有一两个请求，一次失败就会把总体横幅翻成 `incident`。三桶既跟得上当前状态，又不至于抖动。

**载荷上界**：按 24h `request_count` 取前 12 个 entity，每个 entity 取前 6 条子线。被截断时响应带 `truncated: true`，由 UI 明示「仅显示流量最高的 N 项」——不做静默截断。

**总体状态**阈值沿用参考页：entity 间 `min(successRatePct)` → `≥99 operational` / `≥95 degraded` / 其余 `incident`。

### 5. 响应结构

```
{
  generatedAt: int64,        // 秒级时间戳
  dimension:  "group"|"model",
  truncated:  bool,
  metricsDisabled: bool,     // perf_metrics 采集被关闭
  entities: [
    {
      id: string, name: string,
      requests: int64,                 // 24h 请求数
      hours: string[],                 // X 轴标签，最多 24 项（无数据的尾部桶不补齐）
      current: { successRatePct, ttftMs, tps, latencyMs },
      metrics: {
        successRate: { best: (number|null)[], lines: [{ id, name, points: (number|null)[] }] },
        ttft:    { ... },
        tps:     { ... },
        latency: { ... }
      }
    }
  ]
}
```

`hours` 每个 entity 下发一次，各指标序列是扁平 `number[]`（与参考页一致），而非逐点对象，以压低载荷体积。

所有 JSON 编解码走 `common.Marshal` / `common.Unmarshal`（CLAUDE.md 规则 1）。

### 6. 实时 RPM

`GET /api/status/availability/rpm` → `{ rpm }`。

数据源是一个轻量的进程内滚动计数器（60 个 1 秒槽的环形缓冲，或按秒分桶的 map + 惰性淘汰），不落库，不引入 SSE。

自增点是 `perfmetrics.RecordRelaySample` **函数内部**。该函数是中转记录的唯一漏斗，已被三处调用：`controller/relay.go:245`（失败）、`service/quota.go:378` 与 `service/text_quota.go:479`（成功）。在函数内自增而非在三个调用点分别自增，避免漏改其中之一导致 RPM 系统性偏低。

前端每 10 秒轮询一次，在客户端累积出 sparkline 折线。视觉与参考页一致，但没有 SSE 基础设施，且多副本部署下每个节点只报自己的计数——语义清晰（当前节点 RPM），不会产生跨节点的错误求和。

### 7. 边界情况

- `perf_metrics_setting.Enabled == false` → 返回 `200`，`entities: []` 且 `metricsDisabled: true`，让页面说明「性能指标采集已关闭」，而不是显示一个语焉不详的空状态。
- 24 小时内完全无流量 → `entities: []`，`metricsDisabled: false` → 页面显示常规空状态。
- 某 entity 的某指标全为 `null`（如流式指标缺失导致 `ttft_count` 恒为 0）→ 该小图渲染空状态占位，不渲染空白坐标系。

## 前端改动 — default 主题

技术栈：React 19 + TanStack Router（文件路由，`routeTree.gen.ts` 自动生成，**不得手改**）+ Base UI + Tailwind v4 + VChart（`@visactor/react-vchart` ^2.0.22）。

### 路由与页面

- 新建 `web/default/src/routes/_authenticated/availability/index.tsx` → URL `/availability`。`beforeLoad` 复制 `routes/_authenticated/cost/index.tsx` 的形态：role 低于 `ADMIN` → `redirect({ to: '/403' })`；`config.availabilityMonitorEnabled === false` → 同样重定向。这不是安全边界（安全边界在后端中间件），只为避免先渲染骨架再吃 403。
- 新建 `web/default/src/features/availability/`，沿用 `features/cost/` 的组织：`index.tsx`（页面）、`api.ts`、`types.ts`、`lib.ts`、`components/`。

### 组件分解

每个组件单一职责，可独立理解与测试：

| 组件 | 职责 |
|---|---|
| `index.tsx` | 页面骨架，`SectionPageLayout` + 数据获取编排 |
| `components/overall-banner.tsx` | 总体状态横幅：脉冲圆点 + 文案 + 更新时间 |
| `components/live-rpm-card.tsx` | 实时 RPM 卡片：轮询、点累积、VChart `area` sparkline |
| `components/dimension-tabs.tsx` | `groups \| models` 切换，带计数 chip |
| `components/entity-accordion.tsx` | entity 列表容器（`allowsMultipleExpanded`，首项默认展开） |
| `components/entity-row.tsx` | 折叠头：状态点、名称、24h 请求数、右侧四指标摘要条 |
| `components/metric-chart.tsx` | 单指标小图：`best` 虚线 + 各子线，`height=180` |
| `components/metric-tooltip.tsx` | 自定义 tooltip，`best` 置顶后加分隔线 |
| `lib.ts` | 阈值判定、指标格式化、successRate→颜色标尺，纯函数 |

### 数据获取

```ts
useQuery({
  queryKey: ['status', 'availability', dimension],
  queryFn: () => fetchAvailability(dimension),
  staleTime: 60_000,
  refetchInterval: 300_000,
})
```

RPM 卡片独立 `useQuery`，`refetchInterval: 10_000`。维度经 `validateSearch` 的 zod schema 落到 URL search param，保证刷新与分享保持视图。

### UI 风格对齐

参考页的样式已从其构建产物中还原，直接复用其 Tailwind 语义：

- 卡片 `rounded-2xl border border-separator/60 bg-surface p-4`；数字一律 `tabular-nums`
- 状态语义色走仓库既有的 success / warning / danger token，横幅底色用 `/10` 透明度变体
- 折叠头 `flex w-full items-center gap-3 px-5 py-3.5`；面板 `grid gap-6 px-5 pb-5 pt-1 lg:grid-cols-2`
- 四指标摘要条 `ml-auto flex shrink-0 items-center gap-3 text-xs tabular-nums`，`md` 断点以下隐藏
- 骨架屏 `bg-surface-secondary animate-pulse rounded-lg`
- 图表低饱和配色，按指标分族（参考页取值）：
  - `ttft` best `#4f86c6`，子线 `#9ab8dd` `#6f9fd0` `#3f6fa8`
  - `tps` best `#4a93b5`，子线 `#97c2d4` `#6aaac6` `#3a7892`
  - `latency` best `#3f9b9b`，子线 `#8fc6c6` `#5fb2b2` `#327e7e`（复用参考页 cacheRate 族）
  - `successRate` 用连续标尺：`#3f9d6b`（优）→ `#c2a15c`（中）→ `#c47b72`（差）
- `best` 线 `strokeDasharray="6 4"`、`strokeWidth 2.5`；子线 `strokeWidth 1.5`、`strokeOpacity .6`；所有线 `dot={false}`、`isAnimationActive={false}`
- X 轴 `dataKey="hour"`、`interval="preserveStartEnd"`、`tickMargin 8`
- 指标格式化：`successRate` → `x.xx%`，`ttft` / `latency` → `NNN ms`，`tps` → `x.x`

### 菜单与开关同步

菜单项置于「成本核算」之后、「模型管理」之前，保留前一版设计确立的「渠道管理 → 成本核算」相邻关系。

| 文件 | 改动 |
|---|---|
| `hooks/use-sidebar-data.ts` | 该 hook 现仅派生 `isSuperAdmin`（第 53 行），需另加 `const isAdmin = userRole !== undefined && userRole >= ROLE.ADMIN`（`ROLE.ADMIN` 已存在于 `lib/roles.ts`）；`admin` 组内 Cost 之后插入条目，条件 `isAdmin && availabilityMonitorEnabled !== false` |
| `hooks/use-system-config.ts` | 类型加 `availability_monitor_enabled?: boolean`；`mapStatusDataToConfig` 加 `availabilityMonitorEnabled: data.availability_monitor_enabled ?? true` |
| `stores/system-config-store.ts` | `SystemConfig` 加 `availabilityMonitorEnabled?: boolean` |

`?? true` 是关键：旧后端未返回该字段时默认放行，避免升级过程中菜单误消失。

运营设置开关需同步 **五**处（缺任一处会出现「开关能点但保存后回弹」或「侧边栏不刷新」）：

| 文件 | 改动 |
|---|---|
| `features/system-settings/general/system-behavior-section.tsx` | `behaviorSchema` 加 `AvailabilityMonitorEnabled: z.boolean()`；在 `CostAccountingEnabled` 之后追加一个 `Switch` |
| `features/system-settings/types.ts` | `OperationsSettings` 加 `AvailabilityMonitorEnabled: boolean` |
| `features/system-settings/operations/index.tsx` | `defaultOperationsSettings` 加 `AvailabilityMonitorEnabled: true` |
| `features/system-settings/operations/section-registry.tsx` | behavior section 的 `defaultValues` 传入该字段 |
| `features/system-settings/hooks/use-update-option.ts` | `STATUS_RELATED_KEYS` 数组加 `'AvailabilityMonitorEnabled'` |

最后一项是前一版「成本核算」设计文档漏记但代码中实际存在的同步点（`use-update-option.ts:26-39`）：它负责失效 `['status']` 查询并清除 `status` 的 localStorage 缓存，缺失会导致开关保存后侧边栏必须刷新页面才更新。

### i18n

`web/default/src/i18n/locales/` 下 en / zh / fr / ru / ja / vi 六个文件，键为英文源串。新增键：页面标题与描述、三个总体状态文案、`groups` / `models` 两个 tab、四个指标名与其提示、`best`、`N requests / 24h`、实时 RPM 标题与提示、空状态、采集已关闭提示、截断提示、开关标题与描述。

## 前端改动 — classic 主题

技术栈：React 18 + Vite + Semi Design。**不使用 Tailwind**，因此采用「带作用域 CSS 的忠实移植」：用 Semi 的 `Collapse` / `Tabs` / `Card` 承载结构，配一个 CSS Module 复刻圆角卡片、低饱和配色与等宽数字。

图表库已确认：**两个主题都用 VChart**，不用 Recharts。default 的依赖是 `@visactor/react-vchart` ^2.0.22，classic 是 ~1.8.8。default 的 `package.json` 虽然列了 `recharts` 3.8.1，但全仓库只有 `components/ui/chart.tsx` 这个未被任何文件引用的 shadcn 原语在 import 它——现有 8 个图表特性（cost、dashboard、rankings、pricing）一律用 VChart。因此新页面沿用 VChart 的 `line` 图元，`best` 线通过 `lineDash` 系列配置表达，并复用 `lib/use-chart-theme.ts` 与 `lib/vchart.ts` 两个既有辅助。两个主题的 VChart 大版本不同，但配色、阈值与格式化规则共用同一套取值，保证观感一致。

### 路由与页面

- `src/App.jsx`：`const Availability = lazy(() => import('./pages/Availability'))`，并新增 `<Route path='/console/availability' element={<AdminRoute><Suspense fallback={<Loading/>} key={location.pathname}><Availability /></Suspense></AdminRoute>} />`
- 新建 `src/pages/Availability/`，组件分解与 default 主题一一对应，共享同一套纯函数逻辑语义（阈值、格式化、颜色标尺），以 classic 自己的 `lib.js` 实现。

### 菜单与开关同步

| 文件 | 改动 |
|---|---|
| `components/layout/SiderBar.jsx` | ① `routerMap` 加 `availability: '/console/availability'`；② `adminItems` 在 `cost` 之后插入 `{ text: t('可用性监控'), itemKey: 'availability', to: '/console/availability', className: isAdmin() && localStorage.getItem('availability_monitor_enabled') !== 'false' ? '' : 'tableHiddle' }`；③ `useMemo` 依赖数组加 `localStorage.getItem('availability_monitor_enabled')` |
| `helpers/data.js` `setStatusData` | `localStorage.setItem('availability_monitor_enabled', data.availability_monitor_enabled)` |
| `hooks/common/useSidebar.js` | `DEFAULT_ADMIN_CONFIG.admin` 加 `availability: true` |
| `pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx` | admin 模块列表加 `{ key: 'availability', title: t('可用性监控'), description: t('分组与模型可用性总览') }` |
| `pages/Setting/Operation/SettingsGeneral.jsx` | 含 `CostAccountingEnabled` 的 `<Row gutter={16}>` 中追加 `<Col xs={24} sm={12} md={8}>` + `Form.Switch`，字段 `AvailabilityMonitorEnabled`，`onChange={handleFieldChange('AvailabilityMonitorEnabled')}` |
| `components/settings/OperationSetting.jsx` | `inputs` 初始值加 `AvailabilityMonitorEnabled: true`（此处的类型决定 `getOptions` 中的 `toBoolean()` 强转） |

`!== 'false'` 而非 `=== 'true'`，同样是为了字段缺失时默认放行。

`SiderBar.jsx` 对每一项都执行 `isModuleVisible('admin', item.itemKey)`，因此 `useSidebar.js` 的默认值与 `SettingsSidebarModulesAdmin.jsx` 的可配置列表必须同时补齐，否则会重现「成本核算」曾出现的「可见但不可配」缺口。

### i18n

classic 的键为中文源串，需在其 8 个语言文件（en / zh / zh-CN / zh-TW / fr / ru / ja / vi）中补充译文。

## 兼容性

- **旧数据库**：`options` 表无 `AvailabilityMonitorEnabled` 行时，`InitOptionMap` 写入默认 `true`。
- **无历史数据**：`perf_metrics` 为空时页面显示空状态；开启采集后数据自然累积，无需回填。
- **旧前端 + 新后端**：status 多一个字段，旧前端忽略。
- **新前端 + 旧后端**：status 无该字段，两主题均因默认放行逻辑（`?? true` / `!== 'false'`）保持菜单可见；接口 404 由页面错误态承接。
- **数据库兼容**：不新增表或列，仅读既有 `perf_metrics` 与 `options`。查询只用 GORM 构造器 + `SUM`/`GROUP BY`，`group` 列经 `commonGroupCol` 引用，SQLite / MySQL / PostgreSQL 三库一致。
- **多副本部署**：可用性查询是无状态读，任意节点可服务。RPM 是节点本地计数器，语义为「当前节点 RPM」。

## 测试

**后端**

- `middleware/availability_monitor_test.go`：开关 `true` 时放行；`false` 时 403 且 `Abort`。
- `model/availability_test.go`：透视纯函数的表驱动测试——`best` 对 `successRate`/`tps` 取 max、对 `ttft`/`latency` 取 min；`request_count = 0` 的桶产出 `null` 而非 `0`；`ttft_count = 0` 时 `ttft` 为 `null`；entity 与子线截断在 12 / 6 处生效且置位 `truncated`；`current` 覆盖最近 3 桶。
- `go build ./...` 与 `go vet ./...` 通过。

**前端**

- `lib.ts` 纯函数单测：阈值判定（99 / 95 边界）、四种指标格式化、颜色标尺。
- default：`bun run build`（含 TypeScript 类型检查）通过。
- classic：构建通过。

**手工验证**

1. 开关关闭 → 两主题侧边栏「可用性监控」消失；重新开启 → 恢复，且**无需刷新页面**（验证 `STATUS_RELATED_KEYS`）。
2. 开关关闭 → `curl /api/status/availability` 返回 403。
3. 非管理员账号访问 → 403 / 重定向。
4. `dimension` 在 `group` / `model` 间切换，URL search param 同步，刷新后保持。
5. 关闭 `perf_metrics_setting.enabled` → 页面提示采集已关闭，而非空白。
6. 两主题的菜单项均位于「成本核算」正下方。

## 风险

1. **同步点遗漏**（主要风险）。default 主题的开关分散在 5 个文件，classic 分散在 6 个文件。漏改 `use-update-option.ts` 的症状尤其隐蔽：开关看起来生效，但侧边栏要刷新页面才更新。实现时按上述两张表逐项核对。
2. **载荷体积**。模型 × 分组的笛卡尔积在大型部署中可能很大。已用 12 / 6 上限与扁平数组压制，并以 `truncated` 明示，不静默截断。
3. **`latency` 替代 `cacheRate` 的语义偏差**。参考页第四个指标是缓存命中率，本实现是平均总时延。这是有意的取舍（避免改动中转热路径），已在「非目标」中记录；若后续需要缓存命中率，需为 `perf_metrics` 增列并改 `Sample`。
4. **classic 视觉保真度**。Semi Design 的默认圆角、间距与配色与参考页不同，需靠作用域 CSS 覆盖。风险限于样式层，不影响数据正确性。
