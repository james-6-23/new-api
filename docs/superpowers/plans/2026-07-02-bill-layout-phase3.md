# 汇总账单 Phase 3 (账单页布局对齐使用日志页) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让新版和旧版账单管理页的布局与使用日志页一致（标题/卡片头 → 筛选工具栏区 → 表格 → 分页），纯布局调整不改业务逻辑。

**Architecture:** 新版用 usage-logs 同款 `SectionPageLayout`（Title + Content）包裹现有筛选与轻量 `ui/table`；旧版用 usage-logs 同款 `CardPro`（searchArea=筛选、children=表格、paginationArea=`createCardProPagination`）并移除表格内 Semi 分页。

**Tech Stack:** React 19 + TanStack Router + Base UI (web/default)；React 18 + Semi Design (web/classic)；bun。

## Global Constraints

- 前端包管理器用 `bun`（Rule 3）。
- 禁止修改/删除 `QuantumNous` / `new-api` 品牌标识；保留现有文件版权头（Rule 5）。
- 只改布局/容器 JSX，不改查询/导出/鉴权/字段/状态逻辑，不改后端/api/i18n。
- 新版参考文件：`web/default/src/features/usage-logs/index.tsx`（`SectionPageLayout` 用法）、`web/default/src/components/layout/components/section-page-layout.tsx`（slot 定义：`.Title`/`.Content`，`fixedContent` prop）。`SectionPageLayout` 从 `@/components/layout` 导出。
- 旧版参考文件：`web/classic/src/components/table/usage-logs/index.jsx`（`CardPro type='type2'` + `searchArea`/`paginationArea` 用法）。`createCardProPagination` 从 `../../../helpers/utils` 导出，签名 `({currentPage, pageSize, total, onPageChange, onPageSizeChange, isMobile, pageSizeOpts, showSizeChanger, t})`，`total<=0` 时返回 `null`。`useIsMobile` 从 `../../../hooks/common/useIsMobile`。

---

## Task 1: 新版账单页布局对齐 (`web/default`)

**Files:**
- Modify: `web/default/src/features/bill-management/components/bill-export-page.tsx`（只改 `return (...)` 的外壳；imports 增加 `SectionPageLayout`；组件顶部逻辑/状态/handler 全部保留不变）

**Interfaces:**
- Consumes: `SectionPageLayout` from `@/components/layout`（slot 组件 `.Title`/`.Content`，`fixedContent` prop）。所有现有 state/handler（`runQuery`/`handleExport`/`toUnix`/`isAdmin`/`data`/`page`/`pageSize` 等）保持不变。

- [ ] **Step 1: 加 import**

在现有 import 区（`import { Table, ... } from '@/components/ui/table'` 附近）加：
```tsx
import { SectionPageLayout } from '@/components/layout'
```

- [ ] **Step 2: 替换 return 的外壳 JSX**

把当前 `return ( <div className='p-4 max-w-2xl space-y-4'> ... </div> )` 整体替换为下面结构。**内部的筛选网格、开关、按钮、合计、表格、分页片段原样保留**，只是外层容器改成 `SectionPageLayout` + Title + Content，并把筛选块收进一个带边框的工具栏区：

```tsx
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Bill Management')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4 overflow-auto'>
          {/* 筛选工具栏区（标题之下、表格之上） */}
          <div className='space-y-4 rounded-lg border p-4'>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-1'>
                <Label>{t('Start Time')}</Label>
                <Input
                  type='datetime-local'
                  value={start}
                  onChange={(e) => setStart(e.target.value)}
                />
              </div>
              <div className='space-y-1'>
                <Label>{t('End Time')}</Label>
                <Input
                  type='datetime-local'
                  value={end}
                  onChange={(e) => setEnd(e.target.value)}
                />
              </div>

              {isAdmin && (
                <>
                  <div className='space-y-1'>
                    <Label>{t('Username')}</Label>
                    <Input
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                    />
                  </div>
                  <div className='space-y-1'>
                    <Label>{t('Channel ID')}</Label>
                    <Input
                      value={channel}
                      onChange={(e) => setChannel(e.target.value)}
                    />
                  </div>
                </>
              )}

              <div className='space-y-1'>
                <Label>{t('Token Name')}</Label>
                <Input
                  value={tokenName}
                  onChange={(e) => setTokenName(e.target.value)}
                />
              </div>
              <div className='space-y-1'>
                <Label>{t('Model Name')}</Label>
                <Input
                  value={modelName}
                  onChange={(e) => setModelName(e.target.value)}
                />
              </div>
              <div className='space-y-1'>
                <Label>{t('Exchange rate (USD to CNY)')}</Label>
                <Input
                  value={rate}
                  onChange={(e) => setRate(e.target.value)}
                  placeholder='7.3'
                />
              </div>
            </div>

            <div className='flex items-center gap-2'>
              <Switch checked={withDetail} onCheckedChange={setWithDetail} />
              <Label>{t('Include daily detail')}</Label>
            </div>
            {withDetail && (
              <div className='flex items-center gap-2'>
                <Switch checked={splitModel} onCheckedChange={setSplitModel} />
                <Label>{t('Split detail by model')}</Label>
              </div>
            )}

            <div className='flex gap-2'>
              <Button onClick={() => runQuery(1)} disabled={querying}>
                {t('Query')}
              </Button>
              <Button variant='outline' onClick={handleExport} disabled={loading}>
                {t('Export Summary Bill')}
              </Button>
            </div>
          </div>

          {/* 表格区 */}
          {data && (
            <div className='min-h-0 flex-1 space-y-2'>
              <div className='text-sm text-muted-foreground'>
                {t('Total')}: ${data.summary.total_amount_usd.toFixed(6)} / ¥
                {data.summary.total_amount_cny.toFixed(6)} · {t('Prompt Tokens')}{' '}
                {data.summary.total_prompt_tokens} · {t('Completion Tokens')}{' '}
                {data.summary.total_completion_tokens} · {t('Cache Read Tokens')}{' '}
                {data.summary.total_cache_read_tokens} · {t('Cache Creation Tokens')}{' '}
                {data.summary.total_cache_creation_tokens}
              </div>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Date')}</TableHead>
                    {isAdmin && <TableHead>{t('Username')}</TableHead>}
                    {isAdmin && <TableHead>{t('Channel ID')}</TableHead>}
                    <TableHead>{t('Token Name')}</TableHead>
                    <TableHead>{t('Model Name')}</TableHead>
                    <TableHead>{t('Amount (USD)')}</TableHead>
                    <TableHead>{t('Exchange Rate')}</TableHead>
                    <TableHead>{t('Amount (CNY)')}</TableHead>
                    <TableHead>{t('Prompt Tokens')}</TableHead>
                    <TableHead>{t('Completion Tokens')}</TableHead>
                    <TableHead>{t('Cache Read Tokens')}</TableHead>
                    <TableHead>{t('Cache Creation Tokens')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.map((it, i) => (
                    <TableRow key={i}>
                      <TableCell>{it.date}</TableCell>
                      {isAdmin && <TableCell>{it.username}</TableCell>}
                      {isAdmin && <TableCell>{it.channel_id}</TableCell>}
                      <TableCell>{it.token_name}</TableCell>
                      <TableCell>{it.model_name}</TableCell>
                      <TableCell>${it.amount_usd.toFixed(6)}</TableCell>
                      <TableCell>{it.exchange_rate}</TableCell>
                      <TableCell>¥{it.amount_cny.toFixed(6)}</TableCell>
                      <TableCell>{it.prompt_tokens}</TableCell>
                      <TableCell>{it.completion_tokens}</TableCell>
                      <TableCell>{it.cache_read_tokens}</TableCell>
                      <TableCell>{it.cache_creation_tokens}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <div className='flex items-center gap-2'>
                <Button
                  variant='outline'
                  disabled={page <= 1 || querying}
                  onClick={() => runQuery(page - 1)}
                >
                  {t('Previous Page')}
                </Button>
                <span className='text-sm'>
                  {page} / {Math.max(1, Math.ceil(data.total / pageSize))}
                </span>
                <Button
                  variant='outline'
                  disabled={page >= Math.ceil(data.total / pageSize) || querying}
                  onClick={() => runQuery(page + 1)}
                >
                  {t('Next Page')}
                </Button>
              </div>
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
```

> 注意：只改外壳与筛选块位置；原 `<h1>` 标题被 `SectionPageLayout.Title` 取代（删掉旧的 `<h1>`）。组件上半部分（imports 之外的所有 state / `runQuery` / `handleExport` / `toUnix`）一行都不动。

- [ ] **Step 3: 构建校验**

Run（在 `web/default/`）: `bun run build`
Expected: 构建成功，无类型错误。

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/bill-management/components/bill-export-page.tsx
git commit -m "refactor(bill): align default bill page layout with usage logs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: 旧版账单页布局对齐 (`web/classic`)

**Files:**
- Modify: `web/classic/src/components/table/bill-summary/index.jsx`（外壳换 CardPro，分页交给 paginationArea）
- Modify: `web/classic/src/components/table/bill-summary/BillSummaryTable.jsx`（移除内部 Semi 分页 prop）

**Interfaces:**
- Consumes: `CardPro` from `../../common/ui/CardPro`；`createCardProPagination` from `../../../helpers/utils`；`useIsMobile` from `../../../hooks/common/useIsMobile`。runQuery/runExport/collectParams/鉴权保持不变。

- [ ] **Step 1: 改 index.jsx — 引入 CardPro 排布**

在 import 区加：
```jsx
import CardPro from '../../common/ui/CardPro';
import { createCardProPagination } from '../../../helpers/utils';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
```
（注意：`downloadBlobAsFile` 现有 import 保留；`createCardProPagination` 若已在别处 import 勿重复。）

在组件内、`return` 之前加一行：
```jsx
  const isMobile = useIsMobile();
```

把 `return ( <div style={{ padding: 16 }}> ... </div> )` 替换为：
```jsx
  return (
    <CardPro
      type='type2'
      searchArea={
        <BillFilters
          formApiRef={formApiRef}
          isAdminUser={isAdminUser}
          onQuery={() => runQuery(1)}
          onExport={runExport}
          loading={loading}
          t={t}
        />
      }
      paginationArea={createCardProPagination({
        currentPage: page,
        pageSize: PAGE_SIZE,
        total: data?.total ?? 0,
        onPageChange: (pg) => runQuery(pg),
        isMobile,
        showSizeChanger: false,
        t,
      })}
      t={t}
    >
      {data && (
        <BillSummaryTable
          items={data.items}
          summary={data.summary}
          isAdminUser={isAdminUser}
          t={t}
        />
      )}
    </CardPro>
  );
```

> `createCardProPagination` 在 `total<=0` 时返回 `null`，所以未查询前分页区自动隐藏。分页不再传给 BillSummaryTable（改由 CardPro 底部统一渲染）。`showSizeChanger: false` 保持每页 20 固定（与现有 PAGE_SIZE 行为一致；如需切页大小可后续再加）。

- [ ] **Step 2: 改 BillSummaryTable.jsx — 移除内部分页**

把 props 从 `({ items, total, page, pageSize, summary, isAdminUser, onPageChange, t })` 精简为 `({ items, summary, isAdminUser, t })`，并把 `<Table>` 的 `pagination` prop 删除（分页交给 CardPro）。替换后的组件：

```jsx
import React from 'react';
import { Table, Typography } from '@douyinfe/semi-ui';

const money = (v) => (typeof v === 'number' ? v.toFixed(6) : v);

const BillSummaryTable = ({ items, summary, isAdminUser, t }) => {
  const columns = [
    { title: t('日期'), dataIndex: 'date' },
    ...(isAdminUser
      ? [
          { title: t('用户名'), dataIndex: 'username' },
          { title: t('渠道ID'), dataIndex: 'channel_id' },
        ]
      : []),
    { title: t('令牌名称'), dataIndex: 'token_name' },
    { title: t('模型名称'), dataIndex: 'model_name' },
    { title: t('金额(美元)'), dataIndex: 'amount_usd', render: (v) => `$${money(v)}` },
    { title: t('汇率'), dataIndex: 'exchange_rate' },
    { title: t('金额(人民币)'), dataIndex: 'amount_cny', render: (v) => `¥${money(v)}` },
    { title: t('输入tokens'), dataIndex: 'prompt_tokens' },
    { title: t('输出tokens'), dataIndex: 'completion_tokens' },
    { title: t('缓存读取tokens'), dataIndex: 'cache_read_tokens' },
    { title: t('缓存创建tokens'), dataIndex: 'cache_creation_tokens' },
  ];

  return (
    <div>
      {summary && (
        <Typography.Text type='secondary' style={{ display: 'block', margin: '8px 0' }}>
          {t('合计')}: ${money(summary.total_amount_usd)} / ¥{money(summary.total_amount_cny)} ·{' '}
          {t('输入tokens')} {summary.total_prompt_tokens} · {t('输出tokens')} {summary.total_completion_tokens} ·{' '}
          {t('缓存读取tokens')} {summary.total_cache_read_tokens} · {t('缓存创建tokens')}{' '}
          {summary.total_cache_creation_tokens}
        </Typography.Text>
      )}
      <Table
        columns={columns}
        dataSource={items}
        pagination={false}
        rowKey={(r, i) => `${r.date}-${r.username}-${r.channel_id}-${r.token_name}-${r.model_name}-${i}`}
      />
    </div>
  );
};

export default BillSummaryTable;
```

> 保留版权头（文件顶部 1-18 行不动）。`pagination={false}` 明确关闭 Semi 内置分页。

- [ ] **Step 3: 构建校验**

Run（在 `web/classic/`）: `bun run build`
Expected: 构建成功。

- [ ] **Step 4: Commit**

```bash
git add web/classic/src/components/table/bill-summary/index.jsx web/classic/src/components/table/bill-summary/BillSummaryTable.jsx
git commit -m "refactor(bill): align classic bill page layout with usage logs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Manual Verification

- [ ] 新版 `web/default`：`bun run build` 成功；账单页标题在顶（SectionPageLayout.Title），筛选区在标题下的边框卡片内，查询后表格 + 合计 + 分页在筛选下方；查询/翻页/导出正常；管理员见用户名/渠道ID 列，普通用户不见。
- [ ] 旧版 `web/classic`：`bun run build` 成功；`/console/bill` 页用 CardPro 卡片，筛选在 searchArea、表格在中部、分页在卡片底部（CardPro paginationArea）；查询/翻页/导出正常；列差异不变。

## Self-Review Notes

- **Spec 覆盖**：新版 SectionPageLayout 外壳 + 筛选工具栏区 (Task 1)；旧版 CardPro + paginationArea + 移除表格内分页 (Task 2)。
- **不变量**：两端所有查询/导出/鉴权/字段逻辑与状态未改；无新增 i18n；无后端/api 改动。
- **类型一致**：新版 `SectionPageLayout` slot 用法与 usage-logs 一致；旧版 `createCardProPagination` 入参与 usage-logs 调用同构（`currentPage/pageSize/total/onPageChange/isMobile/t`）。
- **YAGNI**：新版未重构成 DataTablePage；旧版分页复用现成 helper，不自造。
