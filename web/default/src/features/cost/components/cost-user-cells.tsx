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
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'
import {
  breakdownChannelCount,
  effectiveCostRatioOf,
  formatCostRatio,
  trimRatioNumber,
} from '../lib'
import type { CostBreakdownRow, CostDimension } from '../types'
import { EditRatioDialog } from './edit-ratio-dialog'

/**
 * The fields the cost-ratio/user-discount cells can read. Sub-rows carry the
 * pricing fields directly; only parent rows carry `breakdown`; the footer
 * summary carries neither and degrades to the weighted/'-' branches.
 */
export interface PricingCellRow {
  cost_cny: number
  list_usd: number
  channel_id?: number
  channel_name?: string
  cost_mode?: '' | 'ratio' | 'discount'
  cost_ratio?: number
  cost_discount?: number
  priced?: boolean
  breakdown?: CostBreakdownRow[]
  /** Cost ratio actually paid over the range (cost ÷ list, quota weighted). */
  effective_ratio?: number
  effective_ratio_known?: boolean
  /** Range spans several price versions, so no single configured value fits. */
  ratio_mixed?: boolean
  /** Quota-weighted discount actually applied over the range (revenue ÷ list). */
  effective_discount?: number
  effective_discount_known?: boolean
  discount_mixed?: boolean
  discount_special?: boolean
  discount_coverage?: number
}

const hoverTriggerClass =
  'cursor-help rounded tabular-nums underline decoration-dotted underline-offset-2 outline-none focus-visible:ring-[3px]'

/**
 * "2.5 / ratio" | "0.8 / discount" — the channel's configured pricing, in the
 * value-slash-mode format the report uses everywhere a single channel's config
 * is shown. `null` when the row has no usable config.
 */
function configuredPricingLabel(
  row: Pick<PricingCellRow, 'cost_mode' | 'cost_ratio' | 'cost_discount'>,
  t: (key: string, options?: Record<string, unknown>) => string
): string | null {
  if (row.cost_mode === 'discount' && row.cost_discount) {
    return t('{{v}} / discount', { v: trimRatioNumber(row.cost_discount) })
  }
  if (row.cost_ratio) {
    return t('{{v}} / ratio', { v: trimRatioNumber(row.cost_ratio) })
  }
  return null
}

/**
 * Weighted cost ratio with a per-channel hover: shown on user/model parent
 * rows, which span several channels and therefore have no single configured
 * value. The number is cost ÷ list price over the row's own money; the hover
 * lists each channel's configured pricing so the blend is auditable.
 */
function WeightedCostRatio({ row }: { row: PricingCellRow }) {
  const { t } = useTranslation()
  const ratio = effectiveCostRatioOf(row)
  const breakdown = row.breakdown ?? []
  const channelCount = breakdownChannelCount(breakdown)

  if (ratio == null) return <span className='text-muted-foreground'>-</span>

  const blended = channelCount > 1
  // A single channel can still be a blend: the range may span price versions.
  const versioned = Boolean(row.ratio_mixed)
  const text = `${blended || versioned ? '≈' : ''}${formatCostRatio(ratio)}`

  if (!breakdown.length) return <span className='tabular-nums'>{text}</span>

  return (
    <HoverCard>
      <HoverCardTrigger
        delay={100}
        closeDelay={80}
        tabIndex={0}
        className={hoverTriggerClass}
      >
        {text}
      </HoverCardTrigger>
      <HoverCardContent align='end' className='w-72'>
        <div className='flex flex-col gap-1.5'>
          {/* Every branch has to describe a *derived* number: the value shown is
              cost ÷ list price over the row's own money, never a stored config.
              Saying "configured" here contradicts the list below whenever the
              channel is discount-mode (0.8 configured renders as 5.76 derived)
              or part of the range has no price version. */}
          <p className='text-muted-foreground text-xs'>
            {blended
              ? t('Weighted across channels: cost ÷ list price')
              : versioned
                ? t('Weighted across price versions: cost ÷ list price')
                : t('Actual cost ratio: cost ÷ list price')}
          </p>
          <div className='flex flex-col divide-y'>
            {breakdown.map((b, index) => (
              <div
                key={`${b.channel_id ?? 'none'}-${index}`}
                className='flex items-center justify-between gap-4 py-1 text-xs'
              >
                <span className='truncate'>
                  {b.channel_name || t('Unknown channel')}
                </span>
                <span className='whitespace-nowrap tabular-nums'>
                  {configuredPricingLabel(b, t) ?? t('Not set')}
                </span>
              </div>
            ))}
          </div>
        </div>
      </HoverCardContent>
    </HoverCard>
  )
}

/**
 * The unified Cost Ratio / Discount cell, shared by all three dims, parent and
 * sub-rows alike:
 *  - channels-dim parent: own config in "2.5 / ratio" form + Not set badge +
 *    the edit dialog (the one place the config is editable);
 *  - any row with channel pricing attached (sub-rows; channels sub-rows get it
 *    injected from their parent): the config label; "Not set" when the channel
 *    is known but unpriced;
 *  - user/model parents: weighted ratio + per-channel hover;
 *  - merged sub-rows / footer summary: weighted ratio, or '-' with no basis.
 */
export function CostRatioDiscountCell({
  row,
  dim,
  exchangeRate,
}: {
  row: PricingCellRow
  dim: CostDimension
  exchangeRate: number
}) {
  const { t } = useTranslation()
  // `priced` can no longer tell parent from child — the backend sends it on
  // breakdown rows too. Only a dimension row carries `breakdown`.
  const isParent = row.breakdown !== undefined

  if (dim === 'channels' && isParent) {
    // Falsy channel_id => "no channel selected" logs, not a real channel.
    if (!row.channel_id) return <span className='text-muted-foreground'>-</span>
    const label = configuredPricingLabel(row, t)
    return (
      <div className='flex items-center justify-end gap-1.5'>
        {!row.priced || !label ? (
          <Badge className='bg-warning/10 text-warning border-transparent'>
            {t('Not set')}
          </Badge>
        ) : (
          <div className='flex flex-col items-end leading-tight'>
            <span className='tabular-nums'>{label}</span>
            {/* The ¥/$1 the range actually paid. Worth a line when the label
                can't be read as one (discount mode) or when it no longer
                describes the whole range (the price changed mid-range). */}
            {row.effective_ratio_known &&
              (row.cost_mode === 'discount' || row.ratio_mixed) && (
                <span className='text-muted-foreground text-[11px] tabular-nums'>
                  ≈{formatCostRatio(row.effective_ratio)}
                </span>
              )}
            {row.ratio_mixed && (
              <span className='text-warning text-[11px]'>
                {t('Cost ratio changed during this range')}
              </span>
            )}
          </div>
        )}
        <EditRatioDialog
          channelId={row.channel_id}
          channelName={row.channel_name || `#${row.channel_id}`}
          currentRatio={row.cost_ratio ?? 0}
          currentMode={row.cost_mode}
          currentDiscount={row.cost_discount ?? 0}
          exchangeRate={exchangeRate}
        />
      </div>
    )
  }

  const label = configuredPricingLabel(row, t)
  if (label) return <span className='tabular-nums'>{label}</span>
  // A known channel with no pricing configured — distinct from "no basis".
  if (row.channel_id) {
    return <span className='text-muted-foreground'>{t('Not set')}</span>
  }
  if (isParent && row.breakdown?.length) return <WeightedCostRatio row={row} />
  if (row.list_usd) {
    return (
      <span className='tabular-nums'>
        ≈{formatCostRatio(effectiveCostRatioOf(row))}
      </span>
    )
  }
  return <span className='text-muted-foreground'>-</span>
}

/**
 * The discount actually applied over the range (`effective_discount` = revenue
 * ÷ list price, quota-weighted) — what the customer really paid. Every caveat
 * comes from the logs in range rather than from the current config, so a
 * dedicated ratio, a mid-range group change and an incomplete pricing history
 * each announce themselves in the hover.
 *
 * Renders '-' when the range has no list price to divide by (free or unpriced
 * models), where the quotient is meaningless.
 */
export function UserDiscountCell({ row }: { row: PricingCellRow }) {
  const { t } = useTranslation()

  const actualKnown = Boolean(row.effective_discount_known)
  const actual = row.effective_discount
  const mixed = Boolean(row.discount_mixed)
  const special = Boolean(row.discount_special)
  // `?? 0`, not `?? 1`: discount_coverage is omitempty and the backend only
  // assigns it when there is a pricing basis, so an absent value means no spend
  // carried pricing info at all. Rows with no logs exit on !actualKnown above.
  const coverage = row.discount_coverage ?? 0

  if (!actualKnown) {
    return <span className='text-muted-foreground'>-</span>
  }

  return (
    <HoverCard>
      <HoverCardTrigger
        delay={100}
        closeDelay={80}
        tabIndex={0}
        className={hoverTriggerClass}
      >
        <span
          className={
            mixed ? 'text-warning inline-flex items-center gap-1' : undefined
          }
        >
          {trimRatioNumber(actual)}
        </span>
      </HoverCardTrigger>
      <HoverCardContent align='end' className='w-80'>
        <div className='flex flex-col gap-1.5 text-xs'>
          <div className='flex items-center justify-between gap-4'>
            <span className='text-muted-foreground'>
              {t('Actual discount (this range)')}
            </span>
            <span className='tabular-nums'>{trimRatioNumber(actual)}</span>
          </div>
          <p className='text-muted-foreground'>
            {t(
              'Actual discount is revenue ÷ list price over the selected range.'
            )}
          </p>
          {special && (
            <p className='text-muted-foreground'>
              {t(
                'Dedicated ratio applied: a user-group × token-group ratio was active.'
              )}
            </p>
          )}
          {mixed && (
            <p className='text-warning'>
              {t(
                'Discount changed during this range — the user may have changed groups or a ratio was edited.'
              )}
            </p>
          )}
          {coverage < 0.99 && (
            <p className='text-muted-foreground'>
              {t(
                'Partial discount coverage: {{pct}}% of spend has pricing info; older logs may lack it.',
                { pct: Math.round(coverage * 100) }
              )}
            </p>
          )}
        </div>
      </HoverCardContent>
    </HoverCard>
  )
}

/**
 * Requests split into success / failure / success rate — one column instead of
 * the previous separate "Requests" and "Success rate" columns.
 */
export function RequestOutcomeCell({
  requestCount,
  errorCount,
  successRate,
  formatNumber,
  formatRate,
}: {
  requestCount: number
  errorCount: number
  successRate: number
  formatNumber: (value: number) => string
  formatRate: (value: number) => string
}) {
  return (
    <div className='flex flex-col items-end leading-tight'>
      <span className='tabular-nums'>
        <span
          className={requestCount ? 'text-success' : 'text-muted-foreground'}
        >
          {formatNumber(requestCount)}
        </span>
        <span className='text-muted-foreground'> / </span>
        <span
          className={errorCount ? 'text-destructive' : 'text-muted-foreground'}
        >
          {formatNumber(errorCount)}
        </span>
      </span>
      <span className='text-muted-foreground text-[11px] tabular-nums'>
        {formatRate(successRate)}
      </span>
    </div>
  )
}
