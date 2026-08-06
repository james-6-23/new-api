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
import { useState } from 'react'
import { CalendarRange } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  dateToUnixTimestamp,
  getEndOfDay,
  getRollingDateRange,
  getStartOfDay,
} from '@/lib/time'
import { formatDateStr } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'
import {
  LogsFilterField,
  LogsFilterInput,
} from '@/features/usage-logs/components/logs-filter-toolbar'

export const DEFAULT_EXCHANGE_RATE = 6.8

export interface CostFilterValue {
  start: number
  end: number
  username?: string
  channel?: number
  model_name?: string
  exchange_rate: number
}

interface CostFilterProps {
  value: CostFilterValue
  onApply: (next: CostFilterValue) => void
}

interface Preset {
  label: string
  range: () => { start: Date; end: Date }
}

function serializeFilterValue(value: CostFilterValue): string {
  return [
    value.start,
    value.end,
    value.username ?? '',
    value.channel ?? '',
    value.model_name ?? '',
    value.exchange_rate,
  ].join('|')
}

/**
 * Default filter range is TODAY (00:00:00 local -> now), matching the
 * "Today" quick-range preset below. Exported so the route/page-level default
 * (used before the user has ever applied a filter) stays in sync with what
 * Reset restores.
 */
export function defaultFilterValue(): CostFilterValue {
  return {
    start: dateToUnixTimestamp(getStartOfDay(new Date())),
    end: dateToUnixTimestamp(new Date()),
    username: undefined,
    channel: undefined,
    model_name: undefined,
    exchange_rate: DEFAULT_EXCHANGE_RATE,
  }
}

export function CostFilter({ value, onApply }: CostFilterProps) {
  const { t } = useTranslation()
  const [rangeOpen, setRangeOpen] = useState(false)
  const [draft, setDraft] = useState<CostFilterValue>(value)
  const [prevValueKey, setPrevValueKey] = useState(() =>
    serializeFilterValue(value)
  )
  const [rangeDraftStart, setRangeDraftStart] = useState<Date>(
    new Date(value.start * 1000)
  )
  const [rangeDraftEnd, setRangeDraftEnd] = useState<Date>(
    new Date(value.end * 1000)
  )

  const valueKey = serializeFilterValue(value)
  if (valueKey !== prevValueKey) {
    setPrevValueKey(valueKey)
    setDraft(value)
  }

  const presets: Preset[] = [
    {
      label: t('Today'),
      range: () => ({ start: getStartOfDay(new Date()), end: new Date() }),
    },
    {
      label: t('Yesterday'),
      range: () => {
        const yesterday = new Date()
        yesterday.setDate(yesterday.getDate() - 1)
        return { start: getStartOfDay(yesterday), end: getEndOfDay(yesterday) }
      },
    },
    {
      label: t('Last 7 days'),
      range: () => getRollingDateRange(7),
    },
    {
      label: t('Last 30 days'),
      range: () => getRollingDateRange(30),
    },
    {
      label: t('This month'),
      range: () => {
        const now = new Date()
        return {
          start: getStartOfDay(new Date(now.getFullYear(), now.getMonth(), 1)),
          end: now,
        }
      },
    },
  ]

  const handleRangeOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setRangeDraftStart(new Date(draft.start * 1000))
      setRangeDraftEnd(new Date(draft.end * 1000))
    }
    setRangeOpen(nextOpen)
  }

  const applyRange = (start: Date, end: Date) => {
    setDraft((prev) => ({
      ...prev,
      start: dateToUnixTimestamp(start),
      end: dateToUnixTimestamp(end),
    }))
    setRangeOpen(false)
  }

  const handleSearch = () => {
    const rate =
      Number.isFinite(draft.exchange_rate) && draft.exchange_rate > 0
        ? draft.exchange_rate
        : DEFAULT_EXCHANGE_RATE
    onApply({ ...draft, exchange_rate: rate })
  }

  const handleReset = () => {
    const next = defaultFilterValue()
    setDraft(next)
    onApply(next)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') handleSearch()
  }

  const triggerLabel = `${formatDateStr(new Date(draft.start * 1000))} – ${formatDateStr(new Date(draft.end * 1000))}`

  return (
    <div className='bg-card/50 rounded-lg border p-2.5 sm:p-3'>
      <div className='grid grid-cols-1 gap-2 sm:grid-cols-[repeat(auto-fit,minmax(10rem,1fr))]'>
        <LogsFilterField wide>
          <Dialog
            open={rangeOpen}
            onOpenChange={handleRangeOpenChange}
            trigger={
              <Button
                variant='outline'
                className='h-8 w-full justify-start gap-2 px-2.5 text-sm leading-5 font-normal'
              >
                <CalendarRange className='text-muted-foreground size-4 shrink-0' />
                <span className='truncate'>{triggerLabel}</span>
              </Button>
            }
            title={t('Time Range')}
            contentClassName='sm:max-w-md'
            footer={
              <>
                <Button variant='outline' onClick={() => setRangeOpen(false)}>
                  {t('Cancel')}
                </Button>
                <Button onClick={() => applyRange(rangeDraftStart, rangeDraftEnd)}>
                  {t('Apply')}
                </Button>
              </>
            }
          >
            <div className='flex flex-col gap-4'>
              <div className='grid gap-2'>
                <Label className='text-muted-foreground text-xs'>
                  {t('Quick Range')}
                </Label>
                <div className='grid grid-cols-2 gap-2 sm:flex sm:flex-wrap'>
                  {presets.map((preset) => (
                    <Button
                      key={preset.label}
                      type='button'
                      size='sm'
                      variant='outline'
                      onClick={() => {
                        const { start, end } = preset.range()
                        applyRange(start, end)
                      }}
                    >
                      {preset.label}
                    </Button>
                  ))}
                </div>
              </div>

              <div className='grid gap-2'>
                <Label htmlFor='cost-filter-start'>{t('Start Time')}</Label>
                <DateTimePicker
                  value={rangeDraftStart}
                  onChange={(date) => date && setRangeDraftStart(date)}
                />
              </div>

              <div className='grid gap-2'>
                <Label htmlFor='cost-filter-end'>{t('End Time')}</Label>
                <DateTimePicker
                  value={rangeDraftEnd}
                  onChange={(date) => date && setRangeDraftEnd(date)}
                />
              </div>
            </div>
          </Dialog>
        </LogsFilterField>

        <LogsFilterField>
          <LogsFilterInput
            aria-label={t('Username')}
            placeholder={t('Username')}
            value={draft.username ?? ''}
            onChange={(e) =>
              setDraft((prev) => ({
                ...prev,
                username: e.target.value || undefined,
              }))
            }
            onKeyDown={handleKeyDown}
          />
        </LogsFilterField>

        <LogsFilterField>
          <LogsFilterInput
            aria-label={t('Channel ID')}
            type='number'
            placeholder={t('Channel ID')}
            value={draft.channel ?? ''}
            onChange={(e) =>
              setDraft((prev) => ({
                ...prev,
                channel:
                  e.target.value === '' ? undefined : Number(e.target.value),
              }))
            }
            onKeyDown={handleKeyDown}
          />
        </LogsFilterField>

        <LogsFilterField>
          <LogsFilterInput
            aria-label={t('Model Name')}
            placeholder={t('Model Name')}
            value={draft.model_name ?? ''}
            onChange={(e) =>
              setDraft((prev) => ({
                ...prev,
                model_name: e.target.value || undefined,
              }))
            }
            onKeyDown={handleKeyDown}
          />
        </LogsFilterField>

        <LogsFilterField>
          <InputGroup className='h-8 w-full min-w-0'>
            <InputGroupAddon>$1 =</InputGroupAddon>
            <InputGroupInput
              aria-label={t('Exchange Rate')}
              type='number'
              step={0.1}
              value={
                Number.isFinite(draft.exchange_rate) ? draft.exchange_rate : ''
              }
              onChange={(e) =>
                setDraft((prev) => ({
                  ...prev,
                  exchange_rate:
                    e.target.value === '' ? NaN : Number(e.target.value),
                }))
              }
              onKeyDown={handleKeyDown}
            />
            <InputGroupAddon align='inline-end'>CNY</InputGroupAddon>
          </InputGroup>
        </LogsFilterField>
      </div>

      <div className='mt-2 flex flex-wrap items-center gap-2'>
        <div className='ms-auto flex flex-wrap items-center justify-end gap-1.5 sm:gap-2'>
          <Button type='button' variant='outline' onClick={handleReset}>
            {t('Reset')}
          </Button>
          <Button type='button' onClick={handleSearch}>
            {t('Search')}
          </Button>
        </div>
      </div>
    </div>
  )
}
