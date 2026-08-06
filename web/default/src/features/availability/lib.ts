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
import type {
  AvailabilityEntity,
  AvailabilityMetricKey,
  OverallStatus,
} from './types'

/** 成功率阈值：≥99 正常，≥95 降级，其余故障。 */
const OPERATIONAL_THRESHOLD = 99
const DEGRADED_THRESHOLD = 95

/**
 * 总体状态取所有实体中最差的成功率。
 *
 * 无任何数据时按 operational 处理——空系统不是故障系统。
 */
export function deriveOverallStatus(
  entities: AvailabilityEntity[]
): OverallStatus {
  const rates = entities
    .map((e) => e.current.successRatePct)
    .filter((v): v is number => v !== null)

  if (rates.length === 0) return 'operational'

  const worst = Math.min(...rates)
  if (worst >= OPERATIONAL_THRESHOLD) return 'operational'
  if (worst >= DEGRADED_THRESHOLD) return 'degraded'
  return 'incident'
}

/** 状态对应的 Tailwind 类名族。 */
export const STATUS_STYLES: Record<
  OverallStatus,
  { dot: string; text: string; bg: string }
> = {
  operational: {
    dot: 'bg-emerald-500',
    text: 'text-emerald-600 dark:text-emerald-400',
    bg: 'bg-emerald-500/10',
  },
  degraded: {
    dot: 'bg-amber-500',
    text: 'text-amber-600 dark:text-amber-400',
    bg: 'bg-amber-500/10',
  },
  incident: {
    dot: 'bg-rose-500',
    text: 'text-rose-600 dark:text-rose-400',
    bg: 'bg-rose-500/10',
  },
}

/** 指标配色族，取自参考实现的低饱和蓝绿系。 */
export const METRIC_COLORS: Record<
  AvailabilityMetricKey,
  { best: string; lines: string[] }
> = {
  successRate: { best: '#3f9d6b', lines: ['#9ad3b4', '#65b98c', '#2f7d54'] },
  ttft: { best: '#4f86c6', lines: ['#9ab8dd', '#6f9fd0', '#3f6fa8'] },
  tps: { best: '#4a93b5', lines: ['#97c2d4', '#6aaac6', '#3a7892'] },
  latency: { best: '#3f9b9b', lines: ['#8fc6c6', '#5fb2b2', '#327e7e'] },
}

/** 指标展示顺序，与后端 map 键一致。 */
export const METRIC_KEYS: AvailabilityMetricKey[] = [
  'successRate',
  'ttft',
  'tps',
  'latency',
]

/**
 * 指标数值格式化。null 统一显示为破折号，避免读成 0。
 */
export function formatMetric(
  metric: AvailabilityMetricKey,
  value: number | null
): string {
  if (value === null || Number.isNaN(value)) return '—'
  switch (metric) {
    case 'successRate':
      return `${value.toFixed(2)}%`
    case 'ttft':
    case 'latency':
      return `${Math.round(value)} ms`
    case 'tps':
      return value.toFixed(1)
  }
}

/** 为子线按索引取色，超出调色板长度后循环。 */
export function lineColor(
  metric: AvailabilityMetricKey,
  index: number
): string {
  const palette = METRIC_COLORS[metric].lines
  return palette[index % palette.length]
}
