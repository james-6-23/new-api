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
import { useCallback, useMemo } from 'react'
import { VChart } from '@visactor/react-vchart'
import { LineChart, BarChart3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'
import type { CostGranularity, CostOverview } from '../types'
import {
  cycleThemeChartColors,
  deriveUsdFromCny,
  foldChannelStack,
  formatBucketLabel,
  formatBucketTooltip,
  getThemeBackgroundColor,
  sampleBucketTicks,
  useCostCurrency,
} from '../lib'

interface CostChartsProps {
  overview?: CostOverview
  loading?: boolean
}

/** Shared shell so both charts get identical header/height/empty-state treatment. */
function ChartCard({
  icon,
  title,
  isEmpty,
  emptyText,
  children,
}: {
  icon: React.ReactNode
  title: string
  isEmpty: boolean
  emptyText: string
  children: React.ReactNode
}) {
  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
        {icon}
        <div className='text-sm font-semibold'>{title}</div>
      </div>
      <div className='h-[280px] p-1.5 sm:h-80 sm:p-2'>
        {isEmpty ? (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {emptyText}
          </div>
        ) : (
          children
        )}
      </div>
    </div>
  )
}

export function CostCharts({ overview, loading }: CostChartsProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const currency = useCostCurrency()

  const revenueLabel = t('Revenue')
  const costLabel = t('Cost')
  const profitLabel = t('Profit')
  const otherLabel = t('Other')

  const granularity: CostGranularity = overview?.granularity ?? 'day'
  // The trend API only carries `*_cny`, whose revenue side was multiplied by
  // the query rate (cost_cny is a native CNY figure from 采购倍率 and is the
  // display-currency N already). Recover revenue's N by dividing the rate out
  // once, so the axis shows the display currency's N and `currency.format`
  // just attaches the symbol.
  const queryRate = overview?.exchange_rate ?? 0
  const toN = useCallback(
    (cny: number) => deriveUsdFromCny(cny, queryRate),
    [queryRate]
  )

  const trendSpec = useMemo(() => {
    const values: Array<{ date: string; series: string; value: number }> = []
    for (const point of overview?.trend ?? []) {
      const revenueN = toN(point.revenue_cny)
      values.push({
        date: point.date,
        series: revenueLabel,
        value: revenueN,
      })
      values.push({
        date: point.date,
        series: costLabel,
        value: point.cost_cny,
      })
      values.push({
        date: point.date,
        series: profitLabel,
        value: revenueN - point.cost_cny,
      })
    }
    const domain = [revenueLabel, costLabel, profitLabel]
    const range = cycleThemeChartColors(domain.length)
    const buckets = (overview?.trend ?? []).map((p) => p.date)
    const ticks = sampleBucketTicks(buckets)

    return {
      type: 'line',
      data: [{ id: 'costTrend', values }],
      xField: 'date',
      yField: 'value',
      seriesField: 'series',
      legends: { visible: true },
      color: { type: 'ordinal', domain, range },
      line: { style: { lineWidth: 2 } },
      point: { visible: false },
      crosshair: { xField: { visible: true } },
      axes: [
        {
          orient: 'bottom',
          label: {
            formatMethod: (value: unknown) => {
              const bucket = String(value)
              // Hour ranges carry up to ~49 buckets; blanking the labels
              // between ticks keeps the axis readable without dropping points.
              return ticks.has(bucket)
                ? formatBucketLabel(bucket, granularity)
                : ''
            },
          },
        },
        {
          orient: 'left',
          label: {
            formatMethod: (value: unknown) => currency.format(Number(value) || 0),
          },
        },
      ],
      tooltip: {
        dimension: {
          title: {
            value: (datum: Record<string, unknown>) =>
              formatBucketTooltip(String(datum?.date), granularity),
          },
          // Without an explicit content pattern VChart falls back to the raw
          // measure value, which renders full float precision.
          content: [
            {
              key: (datum: Record<string, unknown>) => String(datum?.series),
              value: (datum: Record<string, unknown>) =>
                currency.format(Number(datum?.value) || 0),
            },
          ],
        },
        mark: {
          title: {
            value: (datum: Record<string, unknown>) =>
              formatBucketTooltip(String(datum?.date), granularity),
          },
          content: [
            {
              key: (datum: Record<string, unknown>) => String(datum?.series),
              value: (datum: Record<string, unknown>) =>
                currency.format(Number(datum?.value) || 0),
            },
          ],
        },
      },
      background: { fill: 'transparent' },
      animation: true,
    }
  }, [
    overview?.trend,
    revenueLabel,
    costLabel,
    profitLabel,
    granularity,
    currency,
    toN,
  ])

  const stackSpec = useMemo(() => {
    const folded = foldChannelStack(overview?.cost_stack ?? [], otherLabel, 8)
    const range = cycleThemeChartColors(folded.domain.length)
    const gapStroke = getThemeBackgroundColor()
    const buckets = Array.from(new Set(folded.data.map((d) => d.date))).sort()
    const ticks = sampleBucketTicks(buckets)
    // Same as the trend chart: values are already in the display currency.
    const values = folded.data.map((d) => ({
      ...d,
      cost_display: d.cost_cny,
    }))

    return {
      type: 'bar',
      data: [{ id: 'costStack', values }],
      xField: 'date',
      yField: 'cost_display',
      seriesField: 'channel',
      stack: true,
      legends: { visible: true },
      color: { type: 'ordinal', domain: folded.domain, range },
      bar: {
        style: { stroke: gapStroke, lineWidth: 2 },
        state: { hover: { stroke: gapStroke, lineWidth: 2 } },
      },
      axes: [
        {
          orient: 'bottom',
          label: {
            formatMethod: (value: unknown) => {
              const bucket = String(value)
              return ticks.has(bucket)
                ? formatBucketLabel(bucket, granularity)
                : ''
            },
          },
        },
        {
          orient: 'left',
          label: {
            formatMethod: (value: unknown) => currency.format(Number(value) || 0),
          },
        },
      ],
      tooltip: {
        mark: {
          title: {
            value: (datum: Record<string, unknown>) =>
              formatBucketTooltip(String(datum?.date), granularity),
          },
          content: [
            {
              key: (datum: Record<string, unknown>) => String(datum?.channel),
              value: (datum: Record<string, unknown>) =>
                currency.format(Number(datum?.cost_display) || 0),
            },
          ],
        },
        dimension: {
          title: {
            value: (datum: Record<string, unknown>) =>
              formatBucketTooltip(String(datum?.date), granularity),
          },
          content: [
            {
              key: (datum: Record<string, unknown>) => String(datum?.channel),
              value: (datum: Record<string, unknown>) =>
                currency.format(Number(datum?.cost_display) || 0),
            },
          ],
        },
      },
      background: { fill: 'transparent' },
      animation: true,
    }
  }, [overview?.cost_stack, otherLabel, granularity, currency])

  const chartKey = [
    resolvedTheme,
    loading ? 'loading' : 'ready',
    granularity,
    currency.symbol,
    overview?.trend?.length ?? 0,
    overview?.cost_stack?.length ?? 0,
  ].join('-')

  const chartsReady = themeReady && !loading
  const trendEmpty = chartsReady && !(overview?.trend?.length ?? 0)
  const stackEmpty = chartsReady && !(overview?.cost_stack?.length ?? 0)
  const emptyText = t('No data available')

  // The unit belongs in the title: with a single-currency axis there is no
  // other place a reader can confirm which currency the numbers are in.
  const unitSuffix = ` (${currency.symbol})`

  return (
    <div className='grid gap-4 lg:grid-cols-2'>
      <ChartCard
        icon={<LineChart className='text-muted-foreground/60 size-4' />}
        title={t('Revenue / Cost / Profit Trend') + unitSuffix}
        isEmpty={trendEmpty}
        emptyText={emptyText}
      >
        {chartsReady && (
          <VChart
            key={`trend-${chartKey}`}
            spec={{
              ...trendSpec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </ChartCard>

      <ChartCard
        icon={<BarChart3 className='text-muted-foreground/60 size-4' />}
        title={t('Cost by Channel') + unitSuffix}
        isEmpty={stackEmpty}
        emptyText={emptyText}
      >
        {chartsReady && (
          <VChart
            key={`stack-${chartKey}`}
            spec={{
              ...stackSpec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </ChartCard>
    </div>
  )
}
