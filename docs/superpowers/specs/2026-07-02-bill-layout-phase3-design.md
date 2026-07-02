# 汇总账单 Phase 3 — 账单页布局对齐使用日志页 设计方案

- 日期: 2026-07-02
- 状态: 已确认设计，待写实现计划
- 关联: 增量于 Phase 1/2（账单导出 + 双端汇总表格）
- 相关规则: CLAUDE.md Rule 3 (Bun)、Rule 5 (受保护标识)

## 1. 背景与目标

账单管理页目前是裸 `<div>` 平铺（筛选项贴页面顶部）。需求：两端账单页**布局完全照使用日志页排版**，让筛选区从"页面最顶裸排"变成"标题/卡片头之下、表格之上的工具栏区"，与使用日志页视觉一致。

纯布局/容器调整，**不改查询、导出、鉴权、字段逻辑**，无新增 i18n。

## 2. 已确认决策

| 主题 | 决策 |
|---|---|
| 改动范围 | 新版 `web/default` + 旧版 `web/classic` 都改。 |
| 新版表格 | 沿用现有轻量 `ui/table`，**不**重构成整套 `DataTablePage`/TanStack table；仅外壳与筛选位置对齐 usage-logs。 |
| 新版外壳 | 用 usage-logs 同款 `SectionPageLayout`（`.Title` + `.Content`）。 |
| 旧版外壳 | 用 usage-logs 同款 `CardPro`（`searchArea` 放筛选、children 放表格、`paginationArea` 放分页）。 |
| 旧版分页 | 改用 `createCardProPagination` 放进 `CardPro.paginationArea`，移除 `BillSummaryTable` 内部 Semi `pagination`。 |

## 3. 新版改法 (`web/default`)

文件：`web/default/src/features/bill-management/components/bill-export-page.tsx`（仅改 JSX 外壳与容器，逻辑/状态/handler 不动）。

- 顶层由 `<div className='p-4 max-w-2xl space-y-4'>` 换成 `SectionPageLayout`（`fixedContent`），来源 `@/components/layout`。
- `SectionPageLayout.Title` = `t('Bill Management')`（替换原 `<h1>`）。
- `SectionPageLayout.Content` 内部结构（参考 usage-logs 的 `flex h-full min-h-0 flex-col gap-4`）：
  1. **筛选工具栏区**（顶部）：现有筛选网格（时间/用户名/渠道/令牌/模型/汇率）+ 两个明细开关 + 查询/导出按钮，收进一个带间距的区块（`space-y-4`，可加 `rounded-lg border p-4` 使其像 usage-logs 工具栏卡片）。
  2. **表格区**（下方，`min-h-0 flex-1`）：现有合计行 + `ui/table` 12 列 + 分页按钮，保持不变。
- 不新增依赖、不改 api.ts、不改字段。

## 4. 旧版改法 (`web/classic`)

文件：
- `web/classic/src/components/table/bill-summary/index.jsx`（主容器，改为 CardPro 排布）。
- `web/classic/src/components/table/bill-summary/BillSummaryTable.jsx`（移除内部 Semi 分页，保留合计 + Table）。

改法（对齐 `components/table/usage-logs/index.jsx`）：
- 顶层 `<div style={{padding:16}}>` 换成 `CardPro type='type2'`：
  - `searchArea={<BillFilters ... />}`
  - `paginationArea={createCardProPagination({ currentPage: page, pageSize: PAGE_SIZE, total: data?.total ?? 0, onPageChange: (pg)=>runQuery(pg), isMobile, t })}`（`total` 为 0 时该函数返回 null，天然隐藏）
  - children = `<BillSummaryTable ... />`（`data` 有值时渲染，否则空）
- `createCardProPagination` 来自 `../../../helpers/utils`；`useIsMobile` 来自 `../../../hooks/common/useIsMobile`（与 usage-logs 同）。
- `BillSummaryTable`：删除 `<Table pagination={...}>` 里的 `pagination` prop（分页交给 CardPro），保留 `summary` 合计行与 columns。`onPageChange`/`page`/`pageSize`/`total` 不再由该组件使用（可从 props 移除或忽略）。
- 逻辑（runQuery/runExport/collectParams/鉴权）不动。

## 5. 测试策略

- 两端 `bun run build` 通过。
- 手动验证：两端账单页标题在上、筛选区在标题下、表格在筛选下、分页在底部；查询/分页/导出仍工作；管理员/普通用户列差异不变。

## 6. 范围外 (YAGNI)

- 不把新版表格重构为 DataTablePage/TanStack table。
- 不改后端、api、字段、i18n。
- 不动 Phase 1/2 已交付的查询/导出/鉴权逻辑。
