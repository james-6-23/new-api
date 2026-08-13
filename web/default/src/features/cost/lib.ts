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
import { useMemo } from 'react'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { getBillingDisplayMeta } from '@/lib/currency'
import type {
  CostBreakdownRow,
  CostGranularity,
  CostMoney,
  CostStackPoint,
} from './types'

/**
 * Fixed-currency formatters for the cost accounting page. Unlike
 * `lib/currency.ts`, these always render USD / CNY regardless of the
 * admin-configured display currency — the cost report mixes both
 * currencies intentionally (list price in USD, settled cost in CNY).
 */
export function formatUsd(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    currencyDisplay: 'narrowSymbol',
    maximumFractionDigits: 2,
  }).format(value)
}

export function formatCny(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'CNY',
    currencyDisplay: 'narrowSymbol',
    maximumFractionDigits: 2,
  }).format(value)
}

/** `profit_rate` from the API is a fraction (e.g. 0.452 = 45.2%). */
export function formatRate(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return `${(value * 100).toFixed(1)}%`
}

/**
 * Which currency leads the dual-line money display, driven by the admin's
 * 系统设置 → 运营设置 → 额度展示类型 (`quota_display_type`). CNY-primary only
 * when the admin explicitly picked CNY; USD/TOKENS/CUSTOM all fall back to
 * USD-primary for the cost report (mirrors `getBillingDisplayMeta` in
 * `lib/currency.ts`, which forces a real currency for billing displays).
 */
export type MoneyPrimaryCurrency = 'usd' | 'cny'

export function useMoneyPrimaryCurrency(): MoneyPrimaryCurrency {
  const quotaDisplayType = useSystemConfigStore(
    (s) => s.config.currency.quotaDisplayType
  )
  return quotaDisplayType === 'CNY' ? 'cny' : 'usd'
}

/**
 * The single display currency the cost **charts** render in, following the
 * admin's 系统设置 → 运营设置 → 额度展示类型 (`quota_display_type`).
 *
 * The tables and KPI cards show USD and CNY side by side (that pairing is the
 * report's own semantics: list price is quoted in USD, upstream cost settles in
 * CNY). A chart axis has room for exactly one unit, so it resolves to a single
 * currency here — CNY / USD / the admin's custom currency, falling back to USD
 * for TOKENS via `getBillingDisplayMeta` (an amount expressed in tokens is
 * meaningless on a money axis).
 *
 * ## Two different exchange rates — do not conflate them
 *
 * - **query rate** (`exchange_rate` in the filter bar, default 6.8): a *cost
 *   accounting* input. The backend already applied it to produce every `*_cny`
 *   field, including `cost_cny`, which came from `list_usd × cost ratio`.
 * - **display rate** (`usdExchangeRate` / `customCurrencyExchangeRate`): a
 *   *presentation* setting for the whole site.
 *
 * `format()` therefore takes **USD**, never CNY. Callers holding a `*_cny`
 * value must divide by the query rate first (`deriveUsdFromCny`) so the number
 * passes through exactly one conversion; feeding CNY straight in would apply
 * both rates and inflate the axis by ~7x.
 */
export interface CostCurrency {
  symbol: string
  /** Multiplier from USD to the display currency. */
  rateFromUsd: number
  /** Format a **USD** amount into the display currency. */
  format: (usdValue: number | null | undefined) => string
}

export function useCostCurrency(): CostCurrency {
  const currency = useSystemConfigStore((s) => s.config.currency)
  return useMemo(() => {
    const meta = getBillingDisplayMeta(currency)
    // getBillingDisplayMeta never returns 'tokens'; the guard keeps the union
    // exhaustive for the type checker.
    const symbol = meta.kind === 'tokens' ? '$' : meta.symbol
    const rateFromUsd = meta.kind === 'tokens' ? 1 : meta.exchangeRate
    const currencyCode = meta.kind === 'currency' ? meta.currencyCode : undefined

    const format = (usdValue: number | null | undefined): string => {
      if (usdValue == null || Number.isNaN(usdValue)) return '-'
      const value = usdValue * rateFromUsd
      // A custom currency has no ISO code, so Intl's `style: 'currency'` can't
      // render it — prefix the admin's symbol onto a plain decimal instead.
      if (!currencyCode) {
        return `${symbol}${new Intl.NumberFormat(undefined, {
          maximumFractionDigits: 2,
        }).format(value)}`
      }
      return new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency: currencyCode,
        currencyDisplay: 'narrowSymbol',
        maximumFractionDigits: 2,
      }).format(value)
    }

    return { symbol, rateFromUsd, format }
  }, [currency])
}

export interface DualMoneyText {
  primary: string
  secondary: string
}

/**
 * Render a USD/CNY pair as {primary, secondary} display strings, ordered by
 * the admin's configured display currency. Used for the four cost-report
 * money columns (Cost/Revenue/Profit/List Price) everywhere they appear:
 * KPI cards, dimension table cells, and breakdown sub-rows.
 */
export function formatDualMoney(
  usd: number | null | undefined,
  cny: number | null | undefined,
  primary: MoneyPrimaryCurrency
): DualMoneyText {
  return primary === 'cny'
    ? { primary: formatCny(cny), secondary: formatUsd(usd) }
    : { primary: formatUsd(usd), secondary: formatCny(cny) }
}

/**
 * Derive a USD amount from a CNY-only field (cost_cny/profit_cny) using the
 * exchange rate echoed by the cost overview response / filter bar.
 */
export function deriveUsdFromCny(
  cny: number,
  exchangeRate: number
): number {
  return exchangeRate > 0 ? cny / exchangeRate : 0
}

/**
 * Derive a CNY amount from a USD-only field (list_usd) using the exchange
 * rate echoed by the cost overview response / filter bar.
 */
export function deriveCnyFromUsd(usd: number, exchangeRate: number): number {
  return usd * exchangeRate
}

/** Fields a breakdown row can be grouped by when the user merges away a dimension. */
export type CostBreakdownGroupBy = 'username' | 'model_name' | 'channel'

function round6(value: number): number {
  return Math.round(value * 1e6) / 1e6
}

const ZERO_MONEY: CostMoney = {
  revenue_usd: 0,
  revenue_cny: 0,
  list_usd: 0,
  cost_cny: 0,
  profit_cny: 0,
  profit_rate: 0,
  refund_usd: 0,
  prompt_tokens: 0,
  completion_tokens: 0,
  request_count: 0,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  total_tokens: 0,
  error_count: 0,
  frt_sum_ms: 0,
  frt_count: 0,
  success_rate: 0,
  cache_rate: 0,
  avg_ttft_ms: 0,
  effective_discount: 0,
  effective_discount_known: false,
  effective_ratio: 0,
  effective_ratio_known: false,
  ratio_mixed: false,
  discount_mixed: false,
  discount_special: false,
  discount_coverage: 0,
}

/**
 * Client-side re-fold of already-fetched breakdown rows onto a single
 * remaining identity field. Sums every raw additive field and re-derives
 * profit_rate/success_rate/cache_rate/avg_ttft_ms with the exact
 * zero-denominator rules `costMoney.add`/`deriveRates` use in
 * controller/cost_stat.go, so a client merge matches what the backend would
 * have returned for the same grouping.
 */
export function mergeBreakdown(
  rows: CostBreakdownRow[],
  groupBy: CostBreakdownGroupBy
): CostBreakdownRow[] {
  const groups = new Map<string, CostBreakdownRow>()

  for (const row of rows) {
    const key =
      groupBy === 'username'
        ? (row.username ?? '')
        : groupBy === 'model_name'
          ? (row.model_name ?? '')
          : String(row.channel_id ?? '')

    let acc = groups.get(key)
    if (!acc) {
      acc = {
        ...ZERO_MONEY,
        ...(groupBy === 'username' ? { username: row.username } : {}),
        ...(groupBy === 'model_name' ? { model_name: row.model_name } : {}),
        ...(groupBy === 'channel'
          ? { channel_id: row.channel_id, channel_name: row.channel_name }
          : {}),
      }
      // Config fields that ride along with an identity survive a merge that
      // KEEPS that identity: grouping by channel keeps the channel's pricing
      // config. (All rows in a group share the identity, so first-row values
      // are the group's values.) The discount/ratio signals are not copied —
      // they are derived from the summed money below.
      if (groupBy === 'channel') {
        acc.cost_mode = row.cost_mode
        acc.cost_ratio = row.cost_ratio
        acc.cost_discount = row.cost_discount
      }
      groups.set(key, acc)
    }

    acc.revenue_usd = round6(acc.revenue_usd + row.revenue_usd)
    acc.revenue_cny = round6(acc.revenue_cny + row.revenue_cny)
    acc.list_usd = round6(acc.list_usd + row.list_usd)
    acc.cost_cny = round6(acc.cost_cny + row.cost_cny)
    acc.profit_cny = round6(acc.profit_cny + row.profit_cny)
    acc.refund_usd = round6(acc.refund_usd + row.refund_usd)
    acc.prompt_tokens += row.prompt_tokens
    acc.completion_tokens += row.completion_tokens
    acc.request_count += row.request_count
    acc.cache_read_tokens += row.cache_read_tokens
    acc.cache_creation_tokens += row.cache_creation_tokens
    acc.error_count += row.error_count
    acc.frt_sum_ms = round6(acc.frt_sum_ms + row.frt_sum_ms)
    acc.frt_count += row.frt_count
    // A signal that fired for any member fires for the merge. Coverage is a
    // share, so it accumulates as an absolute list-price basis here and is
    // turned back into a share once list_usd is final.
    acc.ratio_mixed = acc.ratio_mixed || row.ratio_mixed
    acc.discount_mixed = acc.discount_mixed || row.discount_mixed
    acc.discount_special = acc.discount_special || row.discount_special
    acc.discount_coverage =
      (acc.discount_coverage ?? 0) + (row.discount_coverage ?? 0) * row.list_usd
  }

  const merged = Array.from(groups.values())
  for (const acc of merged) {
    // prompt_tokens is normalized server-side to "non-cached input", so the
    // four token buckets never overlap and sum to the total. Cache rate uses
    // input tokens as the denominator — output can never be a cache hit.
    const inputTokens =
      acc.prompt_tokens + acc.cache_read_tokens + acc.cache_creation_tokens
    acc.total_tokens = inputTokens + acc.completion_tokens
    acc.profit_rate =
      acc.revenue_cny === 0 ? 0 : round6(acc.profit_cny / acc.revenue_cny)
    acc.success_rate =
      acc.request_count + acc.error_count === 0
        ? 1
        : round6(acc.request_count / (acc.request_count + acc.error_count))
    acc.cache_rate =
      inputTokens === 0 ? 0 : round6(acc.cache_read_tokens / inputTokens)
    acc.avg_ttft_ms = acc.frt_count === 0 ? 0 : round6(acc.frt_sum_ms / acc.frt_count)
    // Re-derived from the summed totals, not averaged across rows — matches
    // `deriveRates` in controller/cost_stat.go.
    acc.effective_discount_known = acc.list_usd !== 0
    acc.effective_discount = acc.effective_discount_known
      ? round6(acc.revenue_usd / acc.list_usd)
      : 0
    acc.effective_ratio_known = acc.list_usd !== 0
    acc.effective_ratio = acc.effective_ratio_known
      ? round6(acc.cost_cny / acc.list_usd)
      : 0
    // Re-share the accumulated basis. Without this the merged row would report
    // 0 coverage and warn that no spend has pricing info.
    //
    // Guarded on the sign, not just on truthiness: list_usd goes negative when
    // the range holds a refund but not the charge it reverses, and Math.min has
    // no lower bound — a negative denominator would surface as "-320% of spend
    // has pricing info". Matches the backend's `> 0` gate in cost_stat.go.
    const coverageBasis = acc.discount_coverage ?? 0
    acc.discount_coverage =
      acc.list_usd > 0 ? round6(Math.min(coverageBasis / acc.list_usd, 1)) : 0
  }

  merged.sort((a, b) => b.revenue_cny - a.revenue_cny)
  return merged
}

/** "8折"/"×8" style label for a channel cost discount (e.g. 0.8 -> zh "8折", en "×8"). */
export function formatDiscountLabel(
  discount: number | null | undefined,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  if (discount == null || !Number.isFinite(discount)) return '-'
  const tenths = Math.round(discount * 100) / 10
  const text = Number.isInteger(tenths) ? String(tenths) : tenths.toFixed(1)
  return t('×{{d}}', { d: text })
}

/**
 * The cost ratio actually applied to a row, derived as cost_cny / list_usd.
 *
 * A user row spans many channels with different ratios, so there is no single
 * configured ratio to show — this returns the *weighted* one implied by the
 * money already summed for the row. `null` when list_usd is 0 (free or
 * unpriced models), where the quotient is meaningless.
 */
export function effectiveCostRatioOf(
  row: Pick<CostMoney, 'cost_cny' | 'list_usd'>
): number | null {
  if (!row.list_usd) return null
  const ratio = row.cost_cny / row.list_usd
  return Number.isFinite(ratio) ? ratio : null
}

/** "¥7.20/$1" — the CNY paid upstream per USD of list price. */
export function formatCostRatio(ratio: number | null | undefined): string {
  if (ratio == null || !Number.isFinite(ratio)) return '-'
  return `¥${ratio.toFixed(2)}/$1`
}

/** "2.5", "0.8" — a configured ratio/discount number with trailing zeros trimmed. */
export function trimRatioNumber(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return String(Number(value.toFixed(4)))
}

/**
 * Distinct channel ids present in a row's breakdown. Used to decide whether a
 * user row's cost ratio is a single channel's configured value (exact) or a
 * weighted blend across channels (prefixed with ≈ in the UI).
 */
export function breakdownChannelCount(rows: CostBreakdownRow[] | undefined): number {
  if (!rows?.length) return 0
  return new Set(rows.map((r) => r.channel_id ?? 0)).size
}

/** `-` when the bucket has no FRT sample, otherwise a rounded ms value. */
export function formatAvgTtft(
  row: Pick<CostMoney, 'avg_ttft_ms' | 'frt_count'>
): string {
  if (!row.frt_count) return '-'
  return `${Math.round(row.avg_ttft_ms)} ms`
}

/**
 * Axis label for a bucket key produced by the backend
 * (`2026-06-01` for day buckets, `2026-06-01 15` for hour buckets).
 * Hour buckets render as `15:00`, day buckets as `06-01`.
 */
export function formatBucketLabel(
  bucket: string,
  granularity: CostGranularity
): string {
  if (granularity === 'hour') {
    const hour = bucket.slice(11, 13)
    return hour ? `${hour}:00` : bucket
  }
  return bucket.slice(5) || bucket
}

/** Full bucket text for tooltips, where the date must stay unambiguous. */
export function formatBucketTooltip(
  bucket: string,
  granularity: CostGranularity
): string {
  if (granularity === 'hour') {
    const day = bucket.slice(0, 10)
    const hour = bucket.slice(11, 13)
    return hour ? `${day} ${hour}:00` : bucket
  }
  return bucket
}

/**
 * Which bucket labels get an axis tick. An hourly range renders up to ~49
 * buckets; showing every label turns the axis into an unreadable smear, so
 * only every Nth is kept (the series itself keeps every point).
 */
export function sampleBucketTicks(
  buckets: string[],
  maxTicks = 12
): Set<string> {
  if (buckets.length <= maxTicks) return new Set(buckets)
  const step = Math.ceil(buckets.length / maxTicks)
  const kept = new Set<string>()
  for (let i = 0; i < buckets.length; i += step) kept.add(buckets[i])
  // Always anchor the last bucket so the axis ends on the real range end.
  kept.add(buckets[buckets.length - 1])
  return kept
}

const THEME_CHART_COLOR_VARIABLES = [
  '--chart-1',
  '--chart-2',
  '--chart-3',
  '--chart-4',
  '--chart-5',
] as const

/** Read the app's chart color tokens so cost charts follow the active theme/preset. */
export function getThemeChartColors(): string[] {
  if (typeof document === 'undefined') return []
  const bodyStyle = window.getComputedStyle(document.body)
  const rootStyle = window.getComputedStyle(document.documentElement)
  return THEME_CHART_COLOR_VARIABLES.map((name) =>
    (bodyStyle.getPropertyValue(name) || rootStyle.getPropertyValue(name)).trim()
  ).filter(Boolean)
}

/** Cycle the theme chart colors to cover `count` series, in stable order. */
export function cycleThemeChartColors(count: number): string[] {
  const colors = getThemeChartColors()
  const fallback = [
    '#5B8FF9',
    '#5AD8A6',
    '#F6BD16',
    '#E8684A',
    '#6DC8EC',
    '#9270CA',
    '#FF9D4D',
    '#269A99',
  ]
  const source = colors.length > 0 ? colors : fallback
  return Array.from(
    { length: count },
    (_, index) => source[index % source.length]
  )
}

/** Read the app's `--background` token so stacked bars can render a themed gap stroke. */
export function getThemeBackgroundColor(): string {
  if (typeof document === 'undefined') return '#ffffff'
  const bodyStyle = window.getComputedStyle(document.body)
  const rootStyle = window.getComputedStyle(document.documentElement)
  return (
    (
      bodyStyle.getPropertyValue('--background') ||
      rootStyle.getPropertyValue('--background')
    ).trim() || '#ffffff'
  )
}

export interface FoldedStackPoint {
  date: string
  channel: string
  cost_cny: number
}

export interface FoldedStackResult {
  data: FoldedStackPoint[]
  /** Series names in fixed first-seen order; "Other" (if present) is last. */
  domain: string[]
}

/**
 * Fold a per-channel cost stack down to at most `maxSeries` named channels,
 * bucketing the remainder into `otherLabel`. Series order is first-seen so
 * color assignment stays stable across re-renders / theme switches.
 */
export function foldChannelStack(
  points: CostStackPoint[],
  otherLabel: string,
  maxSeries = 8
): FoldedStackResult {
  if (points.length === 0) return { data: [], domain: [] }

  const totals = new Map<string, number>()
  const firstSeenOrder: string[] = []
  for (const point of points) {
    if (!totals.has(point.channel_name)) firstSeenOrder.push(point.channel_name)
    totals.set(
      point.channel_name,
      (totals.get(point.channel_name) || 0) + (Number(point.cost_cny) || 0)
    )
  }

  const shouldFold = firstSeenOrder.length > maxSeries
  let keep: Set<string>
  if (shouldFold) {
    const ranked = Array.from(totals.entries()).sort((a, b) => b[1] - a[1])
    keep = new Set(ranked.slice(0, maxSeries).map(([name]) => name))
  } else {
    keep = new Set(firstSeenOrder)
  }

  const domain = firstSeenOrder.filter((name) => keep.has(name))
  if (shouldFold) domain.push(otherLabel)

  const byDate = new Map<string, Map<string, number>>()
  for (const point of points) {
    const key = keep.has(point.channel_name) ? point.channel_name : otherLabel
    if (!byDate.has(point.date)) byDate.set(point.date, new Map())
    const bucket = byDate.get(point.date)!
    bucket.set(key, (bucket.get(key) || 0) + (Number(point.cost_cny) || 0))
  }

  const dates = Array.from(byDate.keys()).sort()
  const data: FoldedStackPoint[] = []
  for (const date of dates) {
    const bucket = byDate.get(date)!
    for (const channel of domain) {
      data.push({
        date,
        channel,
        cost_cny: Number((bucket.get(channel) || 0).toFixed(2)),
      })
    }
  }

  return { data, domain }
}
