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
import { DollarSign, PiggyBank, Receipt, TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { StatCard } from '@/features/dashboard/components/ui/stat-card'
import type { CostOverview } from '../types'
import {
  formatRate,
  profitAmountOf,
  profitRateOf,
  resolveDualMoney,
  useMoneyPrimaryCurrency,
} from '../lib'

interface CostKpiCardsProps {
  overview?: CostOverview
  loading?: boolean
}

export function CostKpiCards({ overview, loading }: CostKpiCardsProps) {
  const { t } = useTranslation()
  const primary = useMoneyPrimaryCurrency()
  const totals = overview?.totals
  const exchangeRate = overview?.exchange_rate ?? 0
  const unpricedCount = overview?.unpriced_channel_count ?? 0
  // Profit is recomputed on the display currency's own figures so the three
  // headline cards add up; the backend's profit_cny mixes in the filter rate.
  const profitAmount = totals ? profitAmountOf(totals) : 0
  const profitTone = profitAmount < 0 ? ('rose' as const) : ('teal' as const)

  const revenue = totals
    ? resolveDualMoney(totals.revenue_usd, primary, exchangeRate)
    : undefined
  const cost = totals
    ? resolveDualMoney(totals.cost_cny, primary, exchangeRate)
    : undefined
  const listPrice = totals
    ? resolveDualMoney(totals.list_usd, primary, exchangeRate)
    : undefined
  const refund = totals
    ? resolveDualMoney(totals.refund_usd, primary, exchangeRate)
    : undefined
  const profit = totals
    ? resolveDualMoney(profitAmount, primary, exchangeRate)
    : undefined

  return (
    <div className='flex flex-col gap-3'>
      {unpricedCount > 0 && (
        <div className='border-warning/40 bg-warning/10 text-warning rounded-md border px-3 py-2 text-sm'>
          {t(
            '{{count}} channels have no cost ratio set; their cost is counted as 0',
            { count: unpricedCount }
          )}
        </div>
      )}

      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <div className='bg-card rounded-xl border p-3'>
          <StatCard
            title={t('Revenue')}
            value={loading || !totals || !revenue ? '--' : revenue.primary}
            description={
              loading || !totals || !revenue || !refund
                ? ''
                : `${revenue.secondary} · ${t('Refunds')} -${refund.primary}`
            }
            icon={DollarSign}
            tone='gray'
            loading={loading}
          />
        </div>
        <div className='bg-card rounded-xl border p-3'>
          <StatCard
            title={t('Cost')}
            value={loading || !totals || !cost ? '--' : cost.primary}
            description={
              loading || !totals || !cost || !listPrice
                ? ''
                : `${cost.secondary} · ${t('List Price')} ${listPrice.primary} × ${t('per-channel ratios')}`
            }
            icon={Receipt}
            tone='gray'
            loading={loading}
          />
        </div>
        <div className='bg-card rounded-xl border p-3'>
          <StatCard
            title={t('Profit')}
            value={
              loading || !totals || !profit ? (
                '--'
              ) : (
                <span
                  className={cn(
                    profitAmount >= 0 ? 'text-success' : 'text-destructive'
                  )}
                >
                  {profit.primary}
                </span>
              )
            }
            description={loading || !totals || !profit ? '' : profit.secondary}
            icon={PiggyBank}
            tone={profitTone}
            loading={loading}
          />
        </div>
        <div className='bg-card rounded-xl border p-3'>
          <StatCard
            title={t('Profit Rate')}
            value={loading || !totals ? '--' : formatRate(profitRateOf(totals))}
            description={
              loading || !totals || !profit || !revenue
                ? ''
                : `${profit.primary} / ${revenue.primary}`
            }
            icon={TrendingUp}
            tone={profitTone}
            loading={loading}
          />
        </div>
      </div>
    </div>
  )
}
