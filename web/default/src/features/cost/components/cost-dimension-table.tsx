/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Fragment, type ReactNode, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { StaticDataTable } from '@/components/data-table'
import { getCostByDimension } from '../api'
import { DEFAULT_EXCHANGE_RATE } from './cost-filter'
import type {
  CostBreakdownRow,
  CostChannelSubSupplier,
  CostDimension,
  CostDimensionRow,
  CostMoney,
} from '../types'
import {
  deriveCnyFromUsd,
  deriveUsdFromCny,
  formatAvgTtft,
  formatRate,
  mergeBreakdown,
  useMoneyPrimaryCurrency,
  type CostBreakdownGroupBy,
  type MoneyPrimaryCurrency,
} from '../lib'
import { MoneyDualCell } from './money-dual-cell'
import { TokensDetailHover } from './tokens-detail-hover'
import {
  CostHelpFormula,
  CostHelpHover,
  CostHelpNotes,
} from './cost-help-hover'
import {
  CostRatioDiscountCell,
  RequestOutcomeCell,
  UserDiscountCell,
} from './cost-user-cells'

const PAGE_SIZE = 20
const route = getRouteApi('/_authenticated/cost/')

interface CostDimensionTableProps {
  dim: CostDimension
  start: number
  end: number
  page: number
  onPageChange: (page: number) => void
  username?: string
  channel?: number
  modelName?: string
  exchangeRate?: number
}

/**
 * Row keys are namespaced per dim and always fall back to the row index. The
 * identity fields are `omitempty` on the backend, so a deleted user (user_id
 * absent) or an empty model name would otherwise collide across rows and make
 * React reuse the wrong row's expand/merge state.
 */
function rowKey(dim: CostDimension, row: CostDimensionRow, index: number) {
  if (dim === 'users') return `u-${row.user_id ?? `i${index}`}`
  if (dim === 'models') return `m-${row.model_name || `i${index}`}`
  return `c-${row.channel_id ?? `i${index}`}`
}

/**
 * Card-style row grouping, built on the SINGLE table rather than one table per
 * group: `border-separate` + `border-spacing-y-2` puts real whitespace between
 * groups, and each group draws its own border/rounding. Column widths stay
 * governed by one table layout, so parent and child cells line up exactly —
 * splitting into per-group tables would require a hand-tuned width budget and
 * reintroduce the misalignment this table was rebuilt to avoid.
 *
 * `border-separate` disables the `border-b` row rule from `ui/table`, so every
 * edge is drawn explicitly per cell here. Classes are written out in full
 * (no interpolation) because Tailwind only generates classes it can see
 * statically in the source.
 */
const GROUP_ROW_SIDES =
  '[&>td]:border-border/80 [&>td:first-child]:border-l [&>td:last-child]:border-r'
const GROUP_ROW_TOP =
  '[&>td]:border-t [&>td:first-child]:rounded-tl-lg [&>td:last-child]:rounded-tr-lg'
const GROUP_ROW_BOTTOM =
  '[&>td]:border-b [&>td:first-child]:rounded-bl-lg [&>td:last-child]:rounded-br-lg'

/** Border classes for a row at a given position within its group. */
function groupRowClass(isFirst: boolean, isLast: boolean): string {
  return cn(
    GROUP_ROW_SIDES,
    isFirst && GROUP_ROW_TOP,
    isLast && GROUP_ROW_BOTTOM
  )
}

function MoneyCell({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <TableCell className={cn('text-right tabular-nums', className)}>
      {children}
    </TableCell>
  )
}

/**
 * Metric column definitions shared verbatim between the top-level dimension
 * rows and their breakdown sub-rows, so both render the exact same set of
 * columns in the exact same order (v2 requirement).
 */
/**
 * A metric cell receives the money fields it always has, plus — when the caller
 * is a dimension row or a breakdown row — the identity/pricing fields those
 * carry. Columns that need more than money (cost ratio, group discount) read
 * the optional fields and degrade to "—" when absent (e.g. the footer summary,
 * which is money-only).
 */
type MetricRow = CostMoney &
  Partial<
    Pick<CostDimensionRow, 'breakdown' | 'priced' | 'channel_id' | 'channel_name'>
  > &
  Partial<Pick<CostBreakdownRow, 'cost_mode' | 'cost_ratio' | 'cost_discount'>>

interface MetricColumnSpec {
  id: string
  header: ReactNode
  cellClassName?: string | ((row: MetricRow) => string | undefined)
  cell: (row: MetricRow) => ReactNode
}

/** Header help for the profit rate — the "why doesn't this add up" column. */
function ProfitRateHeader() {
  const { t } = useTranslation()
  return (
    <CostHelpHover label={t('Rate')}>
      <div className='flex flex-col gap-2'>
        <CostHelpFormula
          term={t('Profit rate')}
          expression={t('profit ÷ revenue (CNY)')}
        />
        <CostHelpFormula
          term={t('Revenue (CNY)')}
          expression={t('paid (USD) × exchange rate')}
        />
        <CostHelpFormula
          term={t('Cost (CNY)')}
          expression={t(
            'list price (USD) × the cost ratio of the price version in effect for each log'
          )}
        />
        <CostHelpNotes
          notes={[
            t('Revenue already includes the group discount and nets out refunds.'),
            t('Channels with no cost ratio set count as zero cost, which inflates the rate.'),
            t('Summary rows are total profit ÷ total revenue, not an average of the row rates.'),
          ]}
        />
      </div>
    </CostHelpHover>
  )
}

/** Header help for the unified cost ratio / discount column. */
function CostRatioHeader() {
  const { t } = useTranslation()
  return (
    <CostHelpHover label={t('Cost Ratio / Discount')}>
      <div className='flex flex-col gap-2'>
        <CostHelpFormula
          term={t('{{v}} / ratio', { v: '2.5' })}
          expression={t('the channel bills ¥2.5 per $1 of list price')}
        />
        <CostHelpFormula
          term={t('{{v}} / discount', { v: '0.8' })}
          expression={t(
            'the channel bills 80% of list price, settled at the rate frozen with that price version'
          )}
        />
        <CostHelpNotes
          notes={[
            t('User/model rows blend several channels: shown as ≈ cost ÷ list price; hover to see each channel.'),
            t('"Not set" means the channel has no cost pricing configured and counts as zero cost.'),
          ]}
        />
      </div>
    </CostHelpHover>
  )
}

/** Header help for the user-discount column. */
function UserDiscountHeader() {
  const { t } = useTranslation()
  return (
    <CostHelpHover label={t('User Discount')}>
      <div className='flex flex-col gap-2'>
        <CostHelpFormula
          term={t('User Discount')}
          expression={t('revenue ÷ list price over the selected range')}
        />
        <CostHelpNotes
          notes={[
            t('This is the discount actually applied, weighted by quota — dedicated ratios and mid-range group changes are already reflected.'),
            t('Hover a value to see the signals derived from the range: dedicated ratio, mid-range changes and pricing coverage.'),
            t('Shows \'-\' when the range has no list price (free or unpriced models).'),
          ]}
        />
      </div>
    </CostHelpHover>
  )
}

/** Header help for the cache hit rate — the denominator needs explaining. */
function CacheRateHeader() {
  const { t } = useTranslation()
  return (
    <CostHelpHover label={t('Cache Hit Rate')}>
      <div className='flex flex-col gap-2'>
        <CostHelpFormula
          term={t('Cache hit rate')}
          expression={t('cache read ÷ total input tokens')}
        />
        <CostHelpFormula
          term={t('Total input tokens')}
          expression={t('uncached input + cache read + cache creation')}
        />
        <CostHelpNotes
          notes={[
            t('Output tokens are excluded — they can never be a cache hit.'),
            t('Claude reports input excluding cache while OpenAI includes it; both are normalized to uncached input so channels stay comparable.'),
          ]}
        />
      </div>
    </CostHelpHover>
  )
}

function buildMetricColumns(
  t: ReturnType<typeof useTranslation>['t'],
  primary: MoneyPrimaryCurrency,
  exchangeRate: number,
  dim: CostDimension
): MetricColumnSpec[] {
  // List price leads: it's the basis both other money columns derive from
  // (× cost ratio = cost, × group discount = revenue).
  const columns: MetricColumnSpec[] = [
    {
      id: 'list_usd',
      header: t('List Price'),
      cell: (row) => (
        <MoneyDualCell
          usd={row.list_usd}
          cny={deriveCnyFromUsd(row.list_usd, exchangeRate)}
          primary={primary}
        />
      ),
    },
  ]

  // The two derivation columns apply to every dim: cost ratio / discount says
  // how list price became cost; user discount says how list price became
  // revenue. Parent rows without a single channel show the weighted ratio;
  // parent rows without a single user show '-'.
  columns.push(
    {
      id: 'cost_ratio',
      header: <CostRatioHeader />,
      cell: (row) => (
        <CostRatioDiscountCell
          row={row}
          dim={dim}
          exchangeRate={exchangeRate}
        />
      ),
    },
    {
      id: 'user_discount',
      header: <UserDiscountHeader />,
      cell: (row) => <UserDiscountCell row={row} />,
    }
  )

  columns.push(
    {
      id: 'cost_cny',
      header: t('Cost'),
      cell: (row) => (
        <MoneyDualCell
          usd={deriveUsdFromCny(row.cost_cny, exchangeRate)}
          cny={row.cost_cny}
          primary={primary}
        />
      ),
    },
    {
      id: 'revenue',
      header: t('Revenue'),
      cell: (row) => (
        <MoneyDualCell
          usd={row.revenue_usd}
          cny={row.revenue_cny}
          primary={primary}
        />
      ),
    },
    {
      id: 'profit_cny',
      header: t('Profit'),
      cell: (row) => (
        <MoneyDualCell
          usd={deriveUsdFromCny(row.profit_cny, exchangeRate)}
          cny={row.profit_cny}
          primary={primary}
          primaryClassName={
            row.profit_cny >= 0 ? 'text-success' : 'text-destructive'
          }
        />
      ),
    },
    { id: 'profit_rate', header: <ProfitRateHeader />, cell: (row) => formatRate(row.profit_rate) },
    {
      id: 'request_outcome',
      header: t('Success / Failed'),
      cell: (row) => (
        <RequestOutcomeCell
          requestCount={row.request_count}
          errorCount={row.error_count}
          successRate={row.success_rate}
          formatNumber={formatNumber}
          formatRate={formatRate}
        />
      ),
    },
    {
      id: 'total_tokens',
      header: t('Total Tokens'),
      cell: (row) => (
        <TokensDetailHover
          promptTokens={row.prompt_tokens}
          completionTokens={row.completion_tokens}
          cacheReadTokens={row.cache_read_tokens}
          cacheCreationTokens={row.cache_creation_tokens}
          totalTokens={row.total_tokens}
        />
      ),
    },
    {
      id: 'cache_rate',
      header: <CacheRateHeader />,
      cell: (row) => formatRate(row.cache_rate),
    },
    {
      id: 'avg_ttft_ms',
      header: t('Average TTFT'),
      cell: (row) => formatAvgTtft(row),
    }
  )

  return columns
}

type IdentityField = CostBreakdownGroupBy
type ViewMode = 'detail' | IdentityField

const IDENTITY_MERGE_LABEL_KEY: Record<IdentityField, string> = {
  username: 'Merge users',
  model_name: 'Merge models',
  channel: 'Merge channels',
}

/**
 * Fixed per-dim order for the two breakdown identity columns (v3: always
 * both columns, "—" for whichever is collapsed by the merge view — this
 * keeps sub-row column widths stable whether the parent row is in detail or
 * merged mode).
 */
const BREAKDOWN_IDENTITY_ORDER: Record<CostDimension, [IdentityField, IdentityField]> = {
  users: ['channel', 'model_name'],
  models: ['username', 'channel'],
  channels: ['username', 'model_name'],
}

/**
 * Fixed per-dim merge options (each collapses one identity field, grouping
 * by the other). Order matches the product spec: users -> Merge
 * models/Merge channels; models -> Merge users/Merge channels; channels ->
 * Merge users/Merge models.
 */
const MERGE_OPTIONS_BY_DIM: Record<
  CostDimension,
  { groupBy: IdentityField; collapsed: IdentityField }[]
> = {
  users: [
    { groupBy: 'channel', collapsed: 'model_name' },
    { groupBy: 'model_name', collapsed: 'channel' },
  ],
  models: [
    { groupBy: 'channel', collapsed: 'username' },
    { groupBy: 'username', collapsed: 'channel' },
  ],
  channels: [
    { groupBy: 'model_name', collapsed: 'username' },
    { groupBy: 'username', collapsed: 'model_name' },
  ],
}

/** Per-row compact merge-view Select, rendered in the parent row's Actions column. */
function MergeViewSelect({
  dim,
  viewMode,
  onViewModeChange,
}: {
  dim: CostDimension
  viewMode: ViewMode
  onViewModeChange: (mode: ViewMode) => void
}) {
  const { t } = useTranslation()
  const mergeOptions = MERGE_OPTIONS_BY_DIM[dim]
  return (
    <Select
      value={viewMode}
      onValueChange={(value) => onViewModeChange(value as ViewMode)}
      items={[
        { value: 'detail', label: t('Detail') },
        ...mergeOptions.map((option) => ({
          value: option.groupBy,
          label: t(IDENTITY_MERGE_LABEL_KEY[option.collapsed]),
        })),
      ]}
    >
      <SelectTrigger size='sm' className='h-7 w-28 text-xs'>
        <SelectValue />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          <SelectItem value='detail'>{t('Detail')}</SelectItem>
          {mergeOptions.map((option) => (
            <SelectItem key={option.groupBy} value={option.groupBy}>
              {t(IDENTITY_MERGE_LABEL_KEY[option.collapsed])}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

/** `true` when the merge view has collapsed this identity field away. */
function isFieldCollapsed(field: IdentityField, viewMode: ViewMode): boolean {
  return viewMode !== 'detail' && field !== viewMode
}

function BreakdownIdentityValue({
  field,
  row,
  collapsed,
}: {
  field: IdentityField
  row: CostBreakdownRow
  collapsed: boolean
}) {
  const { t } = useTranslation()
  if (collapsed) return <span className='text-muted-foreground'>—</span>
  if (field === 'username') return <>{row.username || '-'}</>
  if (field === 'model_name') return <>{row.model_name || '-'}</>
  // channel_id falsy (undefined/0) => no channel selected on the underlying
  // logs, not a real channel.
  if (!row.channel_id) return <>{t('Unknown channel')}</>
  return <>{row.channel_name ? `${row.channel_name} (#${row.channel_id})` : `#${row.channel_id}`}</>
}

/**
 * Both identity columns for a dim, always rendered (task v3): the two
 * "other" dimensions per dim (e.g. users dim -> channel, model). Rendered as
 * a fixed 2-column grid inside one <td> so widths stay put across
 * detail/merged views.
 */
function BreakdownIdentityCell({
  dim,
  row,
  viewMode,
}: {
  dim: CostDimension
  row: CostBreakdownRow
  viewMode: ViewMode
}) {
  const { t } = useTranslation()
  const [fieldA, fieldB] = BREAKDOWN_IDENTITY_ORDER[dim]
  const identityLabels: Record<IdentityField, string> = {
    username: t('Username'),
    model_name: t('Model'),
    channel: t('Channel'),
  }
  return (
    <div className='grid grid-cols-2 gap-3'>
      {[fieldA, fieldB].map((field) => (
        <div key={field} className='flex flex-col gap-0.5'>
          <span className='text-muted-foreground text-[11px]'>
            {identityLabels[field]}
          </span>
          <BreakdownIdentityValue
            field={field}
            row={row}
            collapsed={isFieldCollapsed(field, viewMode)}
          />
        </div>
      ))}
    </div>
  )
}

/**
 * Breakdown sub-rows for an expanded parent row, rendered as additional
 * <tr>s in the SAME table as the parent so metric columns line up exactly
 * (v3 requirement). The leading identity+ratio columns are merged into one
 * cell holding both breakdown identity fields; the Actions column carries
 * the "view channel only" action when applicable.
 */
function BreakdownRows({
  dim,
  row,
  metricColumns,
  viewMode,
  onViewChannel,
  totalColSpan,
}: {
  dim: CostDimension
  row: CostDimensionRow
  metricColumns: MetricColumnSpec[]
  viewMode: ViewMode
  onViewChannel: (channelId: number) => void
  totalColSpan: number
}) {
  const { t } = useTranslation()
  const rawBreakdown = row.breakdown ?? []
  const breakdown =
    viewMode === 'detail' ? rawBreakdown : mergeBreakdown(rawBreakdown, viewMode)
  // Channels dim only: the channel identity is folded into the parent, so copy
  // the parent's channel id + pricing config onto every sub-row (the cost-ratio
  // cell then renders "2.5 / ratio" instead of a blank). Nothing to copy for
  // the other dims — the ratio/discount signals are derived per row from the
  // logs it covers, so a sub-row's own values are the accurate ones.
  const enrich = (b: CostBreakdownRow): CostBreakdownRow => {
    if (dim === 'channels') {
      return {
        ...b,
        channel_id: row.channel_id,
        channel_name: row.channel_name,
        cost_mode: row.cost_mode,
        cost_ratio: row.cost_ratio,
        cost_discount: row.cost_discount,
      }
    }
    return b
  }
  // Breakdown rows for the "channels" dim never carry a `channel` identity
  // field (the parent row already is the channel), so there's no "view
  // channel only" action to show there.
  const channelActionAvailable = dim !== 'channels'
  const subSuppliers = dim === 'channels' ? row.sub_suppliers : undefined
  const truncated = Boolean(row.breakdown_truncated)
  // The group's closing edge belongs to whichever child row renders last.
  const lastIndex = breakdown.length - 1
  const bottomOnTruncationNote = truncated
  const bottomOnLastBreakdown = !truncated

  return (
    <>
      {subSuppliers && subSuppliers.length > 0 && (
        <TableRow className={cn('hover:bg-transparent', GROUP_ROW_SIDES)}>
          <TableCell colSpan={totalColSpan} className='bg-muted/30 p-0'>
            <div className='flex flex-col gap-1.5 p-2'>
              <p className='text-muted-foreground text-xs'>
                {t('Report cost uses the channel-level pricing')}
              </p>
              <StaticDataTable<CostChannelSubSupplier>
                columns={[
                  {
                    id: 'name',
                    header: t('Sub-supplier'),
                    cell: (supplier) => supplier.name,
                  },
                  {
                    id: 'cost_ratio',
                    header: t('Ratio'),
                    className: 'text-right',
                    cellClassName: 'text-right tabular-nums',
                    cell: (supplier) => supplier.cost_ratio ?? '-',
                  },
                ]}
                data={subSuppliers}
                tableClassName='text-xs'
              />
            </div>
          </TableCell>
        </TableRow>
      )}

      {breakdown.map((bRow, index) => {
        const enriched = enrich(bRow)
        return (
          <TableRow
            key={index}
            className={cn(
              'bg-muted/20 hover:bg-muted/30',
              groupRowClass(false, bottomOnLastBreakdown && index === lastIndex)
            )}
          >
            {/* 2px rail on the leading cell ties every child visually to its parent. */}
            <TableCell className='border-primary/30 border-l-2' />
            <TableCell>
              <BreakdownIdentityCell dim={dim} row={bRow} viewMode={viewMode} />
            </TableCell>
            {metricColumns.map((col) => (
              <MoneyCell
                key={col.id}
                className={
                  typeof col.cellClassName === 'function'
                    ? col.cellClassName(enriched)
                    : col.cellClassName
                }
              >
                {col.cell(enriched)}
              </MoneyCell>
            ))}
            <TableCell>
              {channelActionAvailable &&
                !isFieldCollapsed('channel', viewMode) &&
                Boolean(bRow.channel_id) && (
                  <Button
                    type='button'
                    variant='link'
                    size='xs'
                    className='h-auto p-0 text-xs'
                    onClick={() => onViewChannel(bRow.channel_id!)}
                  >
                    {t('View channel only')}
                  </Button>
                )}
            </TableCell>
          </TableRow>
        )
      })}

      {truncated && (
        <TableRow
          className={cn(
            'hover:bg-transparent',
            groupRowClass(false, bottomOnTruncationNote)
          )}
        >
          <TableCell
            colSpan={totalColSpan}
            className='text-muted-foreground p-2 text-xs'
          >
            {t('… and {{n}} more rows omitted', { n: row.breakdown_truncated })}
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

function DimensionCell({
  dim,
  row,
}: {
  dim: CostDimension
  row: CostDimensionRow
}) {
  const { t } = useTranslation()
  if (dim === 'users') return <>{row.username || '-'}</>
  if (dim === 'models') return <>{row.model_name || '-'}</>
  // channel_id is omitted (omitempty) by the backend when it's 0, i.e. "no
  // channel selected" on the underlying logs — not a real, unpriced channel.
  // Render it distinctly instead of "Unnamed channel #undefined".
  const isUnknownChannel = !row.channel_id
  return (
    <div className='flex flex-col gap-0.5'>
      <div className='flex items-center gap-1.5'>
        <span>
          {isUnknownChannel
            ? t('Unknown channel')
            : row.channel_name || t('Unnamed channel')}
        </span>
        {row.is_aggregator && (
          <Badge className='bg-info/10 text-info border-transparent'>
            {t('Aggregator')}
          </Badge>
        )}
      </div>
      <span className='text-muted-foreground text-xs'>
        #{isUnknownChannel ? 0 : row.channel_id}
      </span>
    </div>
  )
}

function summaryLabel(dim: CostDimension, t: (key: string) => string) {
  if (dim === 'users') return t('All Users')
  if (dim === 'models') return t('All Models')
  return t('All Channels')
}

export function CostDimensionTable({
  dim,
  start,
  end,
  page,
  onPageChange,
  username,
  channel,
  modelName,
  exchangeRate,
}: CostDimensionTableProps) {
  const { t } = useTranslation()
  const navigate = route.useNavigate()
  const [expanded, setExpanded] = useState<Set<string | number>>(new Set())
  const [viewModeByRow, setViewModeByRow] = useState<
    Map<string | number, ViewMode>
  >(new Map())

  // Row keys only identify a row within one result set, so any change of dim,
  // range, filter or page can hand the same key to a different row. Drop the
  // per-row expand/merge state whenever the result set changes, otherwise one
  // row inherits another's expanded breakdown or merge view.
  const resultSetId = [dim, start, end, page, username, channel, modelName].join('|')
  const [lastResultSetId, setLastResultSetId] = useState(resultSetId)
  if (lastResultSetId !== resultSetId) {
    setLastResultSetId(resultSetId)
    if (expanded.size) setExpanded(new Set())
    if (viewModeByRow.size) setViewModeByRow(new Map())
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'cost',
      dim,
      start,
      end,
      page,
      username,
      channel,
      modelName,
      exchangeRate,
    ],
    queryFn: () =>
      getCostByDimension(dim, {
        start_timestamp: start,
        end_timestamp: end,
        p: page,
        page_size: PAGE_SIZE,
        username,
        channel,
        model_name: modelName,
        exchange_rate: exchangeRate,
      }),
    placeholderData: keepPreviousData,
  })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const summary: CostMoney | undefined = data?.summary
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const primary = useMoneyPrimaryCurrency()
  const effectiveExchangeRate = exchangeRate ?? DEFAULT_EXCHANGE_RATE
  const metricColumns = buildMetricColumns(t, primary, effectiveExchangeRate, dim)
  // expand toggle + identity + metrics + actions
  const totalColSpan = 3 + metricColumns.length

  const toggleExpanded = (key: string | number) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const getViewMode = (key: string | number): ViewMode =>
    viewModeByRow.get(key) ?? 'detail'

  const setViewMode = (key: string | number, mode: ViewMode) => {
    setViewModeByRow((prev) => {
      const next = new Map(prev)
      next.set(key, mode)
      return next
    })
  }

  const handleViewChannel = (channelId: number) => {
    navigate({
      search: (prev) => ({
        ...prev,
        channel: channelId,
        username: undefined,
        model_name: undefined,
        tab: 'channels',
        p: undefined,
      }),
    })
  }

  const identityHeader =
    dim === 'users' ? t('Username') : dim === 'models' ? t('Model') : t('Channel')

  return (
    <div className='flex flex-col gap-2'>
      {/* No outer border: each row group draws its own card frame instead. */}
      <div className='overflow-x-auto'>
        <Table className='border-separate border-spacing-y-2'>
          <TableHeader>
            <TableRow>
              <TableHead className='w-8' />
              <TableHead>{identityHeader}</TableHead>
              {metricColumns.map((col) => (
                <TableHead key={col.id} className='text-right'>
                  {col.header}
                </TableHead>
              ))}
              <TableHead className='w-28'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 5 }).map((_, index) => (
                <TableRow
                  key={`skeleton-${index}`}
                  className={groupRowClass(true, true)}
                >
                  <TableCell colSpan={totalColSpan}>
                    <Skeleton className='h-6 w-full' />
                  </TableCell>
                </TableRow>
              ))
            ) : items.length === 0 ? (
              <TableRow className={groupRowClass(true, true)}>
                <TableCell
                  colSpan={totalColSpan}
                  className='text-muted-foreground h-24 text-center'
                >
                  {t('No data available')}
                </TableCell>
              </TableRow>
            ) : (
              items.map((row, index) => {
                const key = rowKey(dim, row, index)
                const hasBreakdown = Boolean(row.breakdown?.length)
                const isExpanded = expanded.has(key)
                // Collapsed (or childless) rows are a one-row group, so the
                // parent carries both the opening and closing edge.
                const childrenVisible = hasBreakdown && isExpanded
                return (
                  <Fragment key={key}>
                    <TableRow
                      className={cn(
                        groupRowClass(true, !childrenVisible),
                        // Tint the parent while its children are showing, so
                        // the eye can find the group head in a long list.
                        childrenVisible && 'bg-accent/40'
                      )}
                    >
                      <TableCell>
                        {hasBreakdown && (
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            className='size-6'
                            aria-expanded={isExpanded}
                            onClick={() => toggleExpanded(key)}
                          >
                            {isExpanded ? (
                              <ChevronDown className='size-3.5' />
                            ) : (
                              <ChevronRight className='size-3.5' />
                            )}
                          </Button>
                        )}
                      </TableCell>
                      <TableCell>
                        <DimensionCell dim={dim} row={row} />
                      </TableCell>
                      {metricColumns.map((col) => (
                        <MoneyCell
                          key={col.id}
                          className={
                            typeof col.cellClassName === 'function'
                              ? col.cellClassName(row)
                              : col.cellClassName
                          }
                        >
                          {col.cell(row)}
                        </MoneyCell>
                      ))}
                      <TableCell>
                        {hasBreakdown && (
                          <MergeViewSelect
                            dim={dim}
                            viewMode={getViewMode(key)}
                            onViewModeChange={(mode) => setViewMode(key, mode)}
                          />
                        )}
                      </TableCell>
                    </TableRow>
                    {hasBreakdown && isExpanded && (
                      <BreakdownRows
                        dim={dim}
                        row={row}
                        metricColumns={metricColumns}
                        viewMode={getViewMode(key)}
                        onViewChannel={handleViewChannel}
                        totalColSpan={totalColSpan}
                      />
                    )}
                  </Fragment>
                )
              })
            )}
          </TableBody>
          {summary && items.length > 0 && (
            <TableFooter>
              {/* Its own single-row card, matching the group frames above. */}
              <TableRow className={groupRowClass(true, true)}>
                <TableCell />
                <TableCell className='font-semibold'>
                  {summaryLabel(dim, t)}
                </TableCell>
                {metricColumns.map((col) => (
                  <MoneyCell
                    key={col.id}
                    className={cn(
                      'font-semibold',
                      typeof col.cellClassName === 'function'
                        ? col.cellClassName(summary)
                        : col.cellClassName
                    )}
                  >
                    {col.cell(summary)}
                  </MoneyCell>
                ))}
                <TableCell />
              </TableRow>
            </TableFooter>
          )}
        </Table>
      </div>

      <div
        className={cn(
          'flex items-center justify-between px-1 transition-opacity',
          isFetching && !isLoading && 'opacity-60'
        )}
      >
        <span className='text-muted-foreground text-xs'>
          {t('Total:')} {formatNumber(total)}
        </span>
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={page <= 1}
            onClick={() => onPageChange(page - 1)}
          >
            {t('Previous')}
          </Button>
          <span className='text-xs tabular-nums'>
            {t('Page {{page}} of {{total}}', { page, total: totalPages })}
          </span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={page >= totalPages}
            onClick={() => onPageChange(page + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
    </div>
  )
}
