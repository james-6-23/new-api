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
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { METRIC_KEYS } from '../lib'
import type { AvailabilityEntity, AvailabilityMetricKey } from '../types'
import { EntityRow } from './entity-row'
import { MetricChart } from './metric-chart'

interface EntityAccordionProps {
  entities: AvailabilityEntity[]
}

export function EntityAccordion({ entities }: EntityAccordionProps) {
  const { t } = useTranslation()

  const metricTitles: Record<AvailabilityMetricKey, string> = {
    successRate: t('Success Rate'),
    ttft: t('Time To First Token'),
    tps: t('Tokens / sec'),
    latency: t('Average Latency'),
  }

  return (
    <Accordion
      multiple
      defaultValue={entities.length > 0 ? [entities[0].id] : []}
      className='overflow-hidden rounded-2xl border'
    >
      {entities.map((entity) => (
        <AccordionItem
          key={entity.id}
          value={entity.id}
          data-testid={`status-entity-${entity.id}`}
        >
          <AccordionTrigger className='px-5 py-3.5'>
            <EntityRow entity={entity} />
          </AccordionTrigger>
          <AccordionContent className='px-5 pb-5'>
            <div className='grid gap-6 lg:grid-cols-2'>
              {METRIC_KEYS.map((key) => (
                <MetricChart
                  key={key}
                  metricKey={key}
                  metric={entity.metrics[key]}
                  hours={entity.hours}
                  title={metricTitles[key]}
                />
              ))}
            </div>
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
  )
}
