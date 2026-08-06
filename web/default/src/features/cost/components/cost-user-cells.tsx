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
import type { CostBreakdownRow, CostDimension } from '../types'
import {
  breakdownChannelCount,
  effectiveCostRatioOf,
  formatCostRatio,
  trimRatioNumber,
} from '../lib'
import { EditRatioDialog } from './edit-ratio-dialog'

/**
 * The fields the cost-ratio/user-discount cells can read. Sub-rows carry the
 * pricing/user fields directly; parent rows carry `priced`/`breakdown`; the
 * footer summary carries neither and degrades to the weighted/'-' branches.
 */
export interface PricingCellRow {
  cost_cny: number
  list_usd: number
  channel_id?: number
  channel_name?: string
  cost_mode?: '' | 'ratio' | 'discount'
  cost_ratio?: number
  cost_discount?: number
  effective_ratio?: number
  priced?: boolean
  breakdown?: CostBreakdownRow[]
  user_group?: string
  group_ratio?: number
  group_ratio_known?: boolean
  group_ratio_special?: boolean
  group_ratio_mixed?: boolean
  /** Quota-weighted discount actually applied over the range (revenue ÷ list). */
  effective_discount?: number
  effective_discount_known?: boolean
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
  const text = `${blended ? '≈' : ''}${formatCostRatio(ratio)}`

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
          <p className='text-muted-foreground text-xs'>
            {blended
              ? t('Weighted across channels: cost ÷ list price')
              : t('Cost ratio configured on the channel')}
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
                <span className='tabular-nums whitespace-nowrap'>
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
  const isParent = row.priced !== undefined

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
            {row.cost_mode === 'discount' && (
              <span className='text-muted-foreground text-[11px] tabular-nums'>
                ≈{formatCostRatio(row.effective_ratio)}
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
 * The user's discount, in two layers:
 *
 *  - **shown**: the discount actually applied over the range
 *    (`effective_discount` = revenue ÷ list price, quota-weighted). This is
 *    what the customer really paid, and it reflects dedicated ratios,
 *    cross-group usage and mid-range ratio changes without consulting any
 *    config;
 *  - **hover**: the *current* configured ratio (dedicated when one applies),
 *    plus a warning when config and reality disagree — which is exactly the
 *    signal that the user changed groups or a ratio was edited mid-range.
 *
 * Renders '-' when the row has no single user, and falls back to the config
 * value when list price is 0 (free/unpriced models make the quotient
 * meaningless).
 */
export function UserDiscountCell({ row }: { row: PricingCellRow }) {
  const { t } = useTranslation()

  if (!row.user_group) {
    return <span className='text-muted-foreground'>-</span>
  }

  const configKnown = Boolean(row.group_ratio_known)
  const special = Boolean(row.group_ratio_special)
  const mixed = Boolean(row.group_ratio_mixed)
  const actualKnown = Boolean(row.effective_discount_known)
  const actual = row.effective_discount

  // Nothing to show at all: no usage to derive from and no config to fall back on.
  if (!actualKnown && !configKnown) {
    return <span className='text-muted-foreground'>-</span>
  }

  // Config and reality disagree beyond rounding — surfaced in the hover.
  const drifted =
    actualKnown &&
    configKnown &&
    Math.abs((actual ?? 0) - (row.group_ratio ?? 0)) > 0.005

  return (
    <HoverCard>
      <HoverCardTrigger
        delay={100}
        closeDelay={80}
        tabIndex={0}
        className={hoverTriggerClass}
      >
        {actualKnown ? (
          <span
            className={
              drifted ? 'text-warning inline-flex items-center gap-1' : undefined
            }
          >
            {trimRatioNumber(actual)}
          </span>
        ) : (
          // No list price to divide by — show the configured value instead,
          // marked as such so it is not read as a measured number.
          <span className='text-muted-foreground'>
            {t('{{v}} / configured', { v: trimRatioNumber(row.group_ratio) })}
          </span>
        )}
      </HoverCardTrigger>
      <HoverCardContent align='end' className='w-80'>
        <div className='flex flex-col gap-1.5 text-xs'>
          {actualKnown && (
            <div className='flex items-center justify-between gap-4'>
              <span className='text-muted-foreground'>
                {t('Actual discount (this range)')}
              </span>
              <span className='tabular-nums'>{trimRatioNumber(actual)}</span>
            </div>
          )}
          <div className='flex items-center justify-between gap-4'>
            <span className='text-muted-foreground'>{t('Current group')}</span>
            <span className='font-medium'>{row.user_group}</span>
          </div>
          {configKnown ? (
            <div className='flex items-center justify-between gap-4'>
              <span className='text-muted-foreground'>
                {special ? t('Dedicated ratio') : t('Group ratio')}
              </span>
              <span className='tabular-nums'>
                {mixed ? '≈' : ''}
                {trimRatioNumber(row.group_ratio)}
              </span>
            </div>
          ) : (
            <p className='text-muted-foreground'>
              {t('This group has no ratio configured.')}
            </p>
          )}
          {actualKnown && (
            <p className='text-muted-foreground'>
              {t('Actual discount is revenue ÷ list price over the selected range.')}
            </p>
          )}
          {special && (
            <p className='text-muted-foreground'>
              {t(
                'A dedicated (user group × token group) ratio is configured and takes priority over the group ratio.'
              )}
            </p>
          )}
          {mixed && (
            <p className='text-muted-foreground'>
              {t(
                'The user spent across several token groups in this range; the configured ratio shown is quota-weighted.'
              )}
            </p>
          )}
          {drifted && (
            <p className='text-warning'>
              {t(
                'Actual and configured discounts differ — the user may have changed groups, or the ratio was edited during this range.'
              )}
            </p>
          )}
          {!actualKnown && (
            <p className='text-muted-foreground'>
              {t(
                'No list price in this range (free or unpriced models), so the actual discount cannot be derived.'
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
        <span className={requestCount ? 'text-success' : 'text-muted-foreground'}>
          {formatNumber(requestCount)}
        </span>
        <span className='text-muted-foreground'> / </span>
        <span className={errorCount ? 'text-destructive' : 'text-muted-foreground'}>
          {formatNumber(errorCount)}
        </span>
      </span>
      <span className='text-muted-foreground text-[11px] tabular-nums'>
        {formatRate(successRate)}
      </span>
    </div>
  )
}

