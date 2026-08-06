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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getAvailability } from './api'
import {
  AvailabilityEmpty,
  AvailabilitySkeleton,
} from './components/availability-states'
import { EntityAccordion } from './components/entity-accordion'
import { LiveRpmCard } from './components/live-rpm-card'
import { OverallBanner } from './components/overall-banner'
import { deriveOverallStatus } from './lib'
import type { AvailabilityDimension } from './types'

const route = getRouteApi('/_authenticated/availability/')

export function AvailabilityMonitor() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()

  const dimension: AvailabilityDimension = search.dimension ?? 'group'

  const { data, isLoading } = useQuery({
    queryKey: ['status', 'availability', dimension],
    queryFn: () => getAvailability(dimension),
    staleTime: 60_000,
    refetchInterval: 300_000,
  })

  const handleDimensionChange = (next: string) => {
    navigate({
      search: (prev) => ({
        ...prev,
        dimension: next === 'group' ? undefined : (next as AvailabilityDimension),
      }),
    })
  }

  const entities = data?.entities ?? []
  const status = deriveOverallStatus(entities)

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Availability Monitor')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <div className='grid gap-4 lg:grid-cols-[2fr_1fr]'>
            <OverallBanner status={status} loading={isLoading} />
            <LiveRpmCard />
          </div>

          <div className='flex items-center justify-between gap-3'>
            <Tabs value={dimension} onValueChange={handleDimensionChange}>
              <TabsList>
                <TabsTrigger value='group'>{t('Groups')}</TabsTrigger>
                <TabsTrigger value='model'>{t('Models')}</TabsTrigger>
              </TabsList>
            </Tabs>

            {data?.truncated && (
              <span className='text-muted-foreground text-xs'>
                {t('Showing the busiest entries only')}
              </span>
            )}
          </div>

          {isLoading ? (
            <AvailabilitySkeleton />
          ) : entities.length === 0 ? (
            <AvailabilityEmpty metricsDisabled={data?.metricsDisabled} />
          ) : (
            <EntityAccordion entities={entities} />
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
