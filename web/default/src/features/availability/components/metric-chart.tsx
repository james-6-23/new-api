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
import { VChart } from '@visactor/react-vchart'
import { useTranslation } from 'react-i18next'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'
import { formatMetric, lineColor, METRIC_COLORS } from '../lib'
import type { AvailabilityMetric, AvailabilityMetricKey } from '../types'

interface MetricChartProps {
  metricKey: AvailabilityMetricKey
  metric: AvailabilityMetric
  hours: string[]
  title: string
}

interface ChartDatum {
  hour: string
  series: string
  value: number
}

export function MetricChart({
  metricKey,
  metric,
  hours,
  title,
}: MetricChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const bestLabel = t('Best')

  const spec = useMemo(() => {
    const values: ChartDatum[] = []

    // null 点直接跳过而不是填 0：VChart 会在缺口处断线，
    // 这正是「该小时无流量」应有的表现。
    metric.best.forEach((v, i) => {
      if (v === null) return
      values.push({ hour: hours[i], series: bestLabel, value: v })
    })
    metric.lines.forEach((line) => {
      line.points.forEach((v, i) => {
        if (v === null) return
        values.push({ hour: hours[i], series: line.name, value: v })
      })
    })

    const domain = [bestLabel, ...metric.lines.map((l) => l.name)]
    const range = [
      METRIC_COLORS[metricKey].best,
      ...metric.lines.map((_, i) => lineColor(metricKey, i)),
    ]

    return {
      type: 'line',
      data: [{ id: `availability-${metricKey}`, values }],
      xField: 'hour',
      yField: 'value',
      seriesField: 'series',
      legends: { visible: true, position: 'middle', orient: 'bottom' },
      color: { type: 'ordinal', domain, range },
      line: {
        style: {
          // best 是包络线，用虚线区别于各条真实子线
          lineDash: (datum: Record<string, unknown>) =>
            String(datum?.series) === bestLabel ? [6, 4] : [0],
          lineWidth: (datum: Record<string, unknown>) =>
            String(datum?.series) === bestLabel ? 2.5 : 1.5,
          opacity: (datum: Record<string, unknown>) =>
            String(datum?.series) === bestLabel ? 1 : 0.7,
        },
      },
      point: { visible: false },
      crosshair: { xField: { visible: true } },
      axes: [
        { orient: 'bottom', label: { autoHide: true } },
        { orient: 'left', label: { visible: true } },
      ],
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: Record<string, unknown>) => String(datum?.series),
              value: (datum: Record<string, unknown>) =>
                formatMetric(metricKey, Number(datum?.value)),
            },
          ],
        },
        dimension: {
          content: [
            {
              key: (datum: Record<string, unknown>) => String(datum?.series),
              value: (datum: Record<string, unknown>) =>
                formatMetric(metricKey, Number(datum?.value)),
            },
          ],
        },
      },
      background: { fill: 'transparent' },
      animation: false,
    }
  }, [metric, hours, metricKey, bestLabel])

  return (
    <div className='flex flex-col gap-1.5'>
      <div className='text-muted-foreground text-xs font-medium'>{title}</div>
      <div className='h-[180px]'>
        {themeReady && (
          <VChart
            spec={{
              ...spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </div>
    </div>
  )
}
