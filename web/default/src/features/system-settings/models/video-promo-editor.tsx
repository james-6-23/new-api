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
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ChangeEvent,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { dateToUnixTimestamp } from '@/lib/time'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { DateTimePicker } from '@/components/datetime-picker'
import type { VideoPromoConfig } from '../types'

/** 后端 base 档对应 480p/720p 共用价,展示时换成运营看得懂的口径。 */
const BASE_TIER = 'base'

type VideoPromoEditorProps = {
  modelName?: string
  /** 该模型在原价矩阵中真实存在的档位,由后端给出。 */
  tiers: string[]
  value: VideoPromoConfig | null
  onChange: (next: VideoPromoConfig | null) => void
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

/**
 * 校验 JSON 模式文本。返回 null 表示非法,调用方须留在 JSON 模式。
 */
function parsePromoJson(text: string): VideoPromoConfig | null {
  const trimmed = text.trim()
  if (!trimmed) return { factors: {}, start_at: 0, end_at: 0 }
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    return null
  }
  if (!isPlainObject(parsed)) return null
  const { factors, start_at: startAt, end_at: endAt } = parsed
  if (!isPlainObject(factors)) return null
  const normalized: Record<string, number> = {}
  for (const [tier, factor] of Object.entries(factors)) {
    if (typeof factor !== 'number' || !Number.isFinite(factor)) return null
    normalized[tier] = factor
  }
  if (typeof startAt !== 'number' || typeof endAt !== 'number') return null
  return { factors: normalized, start_at: startAt, end_at: endAt }
}
export function VideoPromoEditor({
  modelName,
  tiers,
  value,
  onChange,
}: VideoPromoEditorProps) {
  const { t } = useTranslation()
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [jsonText, setJsonText] = useState('')

  const factors = useMemo(() => value?.factors ?? {}, [value])
  const startAt = value?.start_at ?? 0
  const endAt = value?.end_at ?? 0

  const tierLabel = useCallback(
    (tier: string) => (tier === BASE_TIER ? t('480p / 720p') : tier),
    [t]
  )

  /** 空配置写回 null,避免给后端提交一张只有零值的空壳。 */
  const emit = useCallback(
    (next: VideoPromoConfig) => {
      const hasFactors = Object.keys(next.factors).length > 0
      if (!hasFactors && !next.start_at && !next.end_at) {
        onChange(null)
        return
      }
      onChange(next)
    },
    [onChange]
  )

  const setFactor = useCallback(
    (tier: string, raw: string) => {
      const nextFactors = { ...factors }
      const trimmed = raw.trim()
      if (!trimmed) {
        // 留空 = 该档不打折,删键而不是写 0。
        delete nextFactors[tier]
      } else {
        const parsed = Number(trimmed)
        if (!Number.isFinite(parsed)) return
        nextFactors[tier] = parsed
      }
      emit({ factors: nextFactors, start_at: startAt, end_at: endAt })
    },
    [emit, endAt, factors, startAt]
  )

  // 读时钟必须留在 effect 里:既满足 React 纯度约束,也让窗口跨越起止点时徽标自动翻转。
  const [nowSec, setNowSec] = useState(0)
  useEffect(() => {
    const tick = () => setNowSec(Math.floor(Date.now() / 1000))
    tick()
    const timer = setInterval(tick, 1000)
    return () => clearInterval(timer)
  }, [])

  const windowState = useMemo(() => {
    if (!startAt || !endAt || !nowSec) return null
    if (nowSec < startAt) return t('Not started')
    if (nowSec > endAt) return t('Ended')
    return t('Active')
  }, [endAt, nowSec, startAt, t])

  const switchToJsonMode = useCallback(() => {
    setJsonText(
      JSON.stringify(value ?? { factors: {}, start_at: 0, end_at: 0 }, null, 2)
    )
    setEditMode('json')
  }, [value])

  const switchToVisualMode = useCallback(() => {
    const parsed = parsePromoJson(jsonText)
    if (!parsed) {
      toast.error(t('Invalid video promo JSON format'))
      return
    }
    emit(parsed)
    setEditMode('visual')
  }, [emit, jsonText, t])

  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <Label>{t('Video limited-time discount')}</Label>
          {modelName ? (
            <span className='text-muted-foreground font-mono text-xs'>
              {modelName}
            </span>
          ) : null}
          {windowState ? <Badge variant='outline'>{windowState}</Badge> : null}
        </div>
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            variant={editMode === 'visual' ? 'default' : 'outline'}
            size='sm'
            onClick={editMode === 'json' ? switchToVisualMode : undefined}
          >
            {t('Visual')}
          </Button>
          <Button
            type='button'
            variant={editMode === 'json' ? 'default' : 'outline'}
            size='sm'
            onClick={editMode === 'visual' ? switchToJsonMode : undefined}
          >
            JSON
          </Button>
        </div>
      </div>

      {editMode === 'json' ? (
        <Textarea
          value={jsonText}
          onChange={(e: ChangeEvent<HTMLTextAreaElement>) =>
            setJsonText(e.target.value)
          }
          className='min-h-40 font-mono text-xs'
          spellCheck={false}
        />
      ) : tiers.length === 0 ? (
        <Alert>
          <AlertDescription>
            {t('This model has no video pricing tiers')}
          </AlertDescription>
        </Alert>
      ) : (
        <div className='space-y-3'>
          {tiers.map((tier) => {
            const factor = factors[tier]
            const hasFactor = typeof factor === 'number'
            const percent = hasFactor ? Math.round((1 - factor) * 1000) / 10 : 0
            return (
              <div key={tier} className='flex items-end gap-3'>
                <Field className='w-40'>
                  <FieldLabel>{tierLabel(tier)}</FieldLabel>
                  <Input
                    type='number'
                    step='0.01'
                    min='0'
                    max='1'
                    placeholder={t('No discount')}
                    value={hasFactor ? String(factor) : ''}
                    onChange={(e: ChangeEvent<HTMLInputElement>) =>
                      setFactor(tier, e.target.value)
                    }
                  />
                </Field>
                <span className='text-muted-foreground pb-2 text-xs'>
                  {hasFactor
                    ? t('{{percent}}% off', { percent })
                    : t('No discount')}
                </span>
              </div>
            )
          })}

          <div className='flex flex-wrap items-end gap-3'>
            <Field className='w-auto'>
              <FieldLabel>{t('Start time')}</FieldLabel>
              <DateTimePicker
                withSeconds
                value={startAt ? new Date(startAt * 1000) : undefined}
                onChange={(next) =>
                  emit({
                    factors,
                    start_at: next ? dateToUnixTimestamp(next) : 0,
                    end_at: endAt,
                  })
                }
              />
            </Field>
            <Field className='w-auto'>
              <FieldLabel>{t('End time')}</FieldLabel>
              <DateTimePicker
                withSeconds
                value={endAt ? new Date(endAt * 1000) : undefined}
                onChange={(next) =>
                  emit({
                    factors,
                    start_at: startAt,
                    end_at: next ? dateToUnixTimestamp(next) : 0,
                  })
                }
              />
            </Field>
          </div>
        </div>
      )}
    </div>
  )
}
