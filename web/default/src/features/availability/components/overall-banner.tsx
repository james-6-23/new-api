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
import { Skeleton } from '@/components/ui/skeleton'
import { STATUS_STYLES } from '../lib'
import type { OverallStatus } from '../types'

interface OverallBannerProps {
  status: OverallStatus
  loading?: boolean
}

export function OverallBanner({ status, loading }: OverallBannerProps) {
  const { t } = useTranslation()

  if (loading) {
    return <Skeleton className='h-[74px] w-full rounded-2xl' />
  }

  const style = STATUS_STYLES[status]
  const label: Record<OverallStatus, string> = {
    operational: t('All Systems Operational'),
    degraded: t('Partially Degraded Performance'),
    incident: t('Service Incident'),
  }
  const description: Record<OverallStatus, string> = {
    operational: t('All groups are responding normally over the last hours'),
    degraded: t('Some groups are showing elevated failure rates'),
    incident: t('One or more groups are failing a significant share of requests'),
  }

  return (
    <div
      data-testid='status-overall-banner'
      className={cn(
        'flex items-center gap-3 rounded-2xl px-5 py-4',
        style.bg
      )}
    >
      <span className='relative flex size-2.5 shrink-0'>
        <span
          className={cn(
            'absolute inline-flex size-full animate-ping rounded-full opacity-60',
            style.dot
          )}
        />
        <span
          className={cn('relative inline-flex size-2.5 rounded-full', style.dot)}
        />
      </span>
      <div className='flex flex-col gap-0.5'>
        <div className={cn('text-sm font-semibold', style.text)}>
          {label[status]}
        </div>
        <div className='text-muted-foreground text-xs'>
          {description[status]}
        </div>
      </div>
    </div>
  )
}
