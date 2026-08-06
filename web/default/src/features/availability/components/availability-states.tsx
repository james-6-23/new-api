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
import { Skeleton } from '@/components/ui/skeleton'

/** 折叠列表的加载骨架，条数与常见实体数量接近。 */
export function AvailabilitySkeleton() {
  return (
    <div className='flex flex-col gap-2'>
      {Array.from({ length: 4 }).map((_, i) => (
        <Skeleton key={i} className='h-[62px] w-full rounded-2xl' />
      ))}
    </div>
  )
}

interface AvailabilityEmptyProps {
  /** 后端采集开关关闭时展示不同的文案，避免误认为「没有流量」。 */
  metricsDisabled?: boolean
}

export function AvailabilityEmpty({ metricsDisabled }: AvailabilityEmptyProps) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col items-center gap-1.5 rounded-2xl border border-dashed px-5 py-12 text-center'>
      <span className='text-sm font-medium'>
        {metricsDisabled
          ? t('Performance metrics collection is disabled')
          : t('No availability data yet')}
      </span>
      <span className='text-muted-foreground max-w-md text-xs'>
        {metricsDisabled
          ? t(
              'Enable performance metrics collection in system settings to start recording availability data'
            )
          : t('Availability data appears once requests have been relayed')}
      </span>
    </div>
  )
}
