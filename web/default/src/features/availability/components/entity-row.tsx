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
import { cn } from '@/lib/utils'
import { deriveOverallStatus, formatMetric, STATUS_STYLES } from '../lib'
import type { AvailabilityEntity } from '../types'

interface EntityRowProps {
  entity: AvailabilityEntity
}

/**
 * 折叠头内容：状态点、名称、24h 请求数，以及右侧四个指标摘要。
 * 指标摘要在窄屏隐藏，避免和名称挤在一行。
 */
export function EntityRow({ entity }: EntityRowProps) {
  const { t } = useTranslation()

  // 单实体状态复用同一套阈值，传入只含自己的数组
  const status = deriveOverallStatus([entity])
  const style = STATUS_STYLES[status]

  const summary = [
    { label: t('Success Rate'), value: formatMetric('successRate', entity.current.successRatePct) },
    { label: t('TTFT'), value: formatMetric('ttft', entity.current.ttftMs) },
    { label: t('TPS'), value: formatMetric('tps', entity.current.tps) },
    { label: t('Latency'), value: formatMetric('latency', entity.current.latencyMs) },
  ]

  return (
    <div className='flex w-full items-center gap-3'>
      <span
        className={cn('size-2 shrink-0 rounded-full', style.dot)}
        aria-hidden
      />
      <div className='flex min-w-0 flex-col items-start gap-0.5'>
        <span className='truncate text-sm font-medium'>{entity.name}</span>
        <span className='text-muted-foreground text-[11px] tabular-nums'>
          {t('{{count}} requests / 24h', { count: entity.requests })}
        </span>
      </div>
      <div className='ml-auto flex shrink-0 items-center gap-3 pr-2 text-xs tabular-nums'>
        {summary.map((item) => (
          <div
            key={item.label}
            className='hidden flex-col items-end md:flex'
          >
            <span className='text-muted-foreground text-[10px]'>
              {item.label}
            </span>
            <span className='font-medium'>{item.value}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
