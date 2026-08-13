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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { dateToUnixTimestamp, formatDateTimeObject } from '@/lib/time'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'
import { getChannel, updateChannel } from '@/features/channels/api'
import {
  createChannelCostVersion,
  deleteChannelCostVersion,
  getChannelCostVersions,
} from '../api'
import type { ChannelCostVersion } from '../types'

type CostPricingMode = 'ratio' | 'discount'

/** The version api rejects with the backend's message; fall back to nothing. */
function errorMessage(error: unknown): string | undefined {
  return error instanceof Error ? error.message || undefined : undefined
}

interface EditRatioDialogProps {
  channelId: number
  channelName: string
  currentRatio: number
  currentMode?: string
  currentDiscount?: number
  exchangeRate: number
}

export function EditRatioDialog({
  channelId,
  channelName,
  currentRatio,
  currentMode,
  currentDiscount,
  exchangeRate,
}: EditRatioDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<CostPricingMode>(
    currentMode === 'discount' ? 'discount' : 'ratio'
  )
  const [ratioValue, setRatioValue] = useState(
    currentRatio ? String(currentRatio) : ''
  )
  const [discountValue, setDiscountValue] = useState(
    currentDiscount ? String(currentDiscount) : ''
  )
  const [submitting, setSubmitting] = useState(false)

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setMode(currentMode === 'discount' ? 'discount' : 'ratio')
      setRatioValue(currentRatio ? String(currentRatio) : '')
      setDiscountValue(currentDiscount ? String(currentDiscount) : '')
    }
    setOpen(nextOpen)
  }

  const parsedRatio = Number(ratioValue)
  const isValidRatio =
    ratioValue.trim() !== '' && Number.isFinite(parsedRatio) && parsedRatio >= 0
  const parsedDiscount = Number(discountValue)
  const isValidDiscount =
    discountValue.trim() !== '' &&
    Number.isFinite(parsedDiscount) &&
    parsedDiscount >= 0
  const isValid = mode === 'discount' ? isValidDiscount : isValidRatio

  const handleSubmit = async () => {
    if (!isValid) return
    setSubmitting(true)
    try {
      const channelRes = await getChannel(channelId)
      if (!channelRes.success || !channelRes.data) {
        throw new Error(channelRes.message || t('Failed to load channel'))
      }

      let settingObj: Record<string, unknown> = {}
      if (channelRes.data.setting) {
        try {
          const parsed = JSON.parse(channelRes.data.setting)
          if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            settingObj = parsed as Record<string, unknown>
          }
        } catch {
          settingObj = {}
        }
      }
      settingObj.cost_mode = mode
      if (mode === 'discount') {
        settingObj.cost_discount = parsedDiscount
      } else {
        settingObj.cost_ratio = parsedRatio
      }

      const updateRes = await updateChannel(channelId, {
        setting: JSON.stringify(settingObj),
      })
      if (!updateRes.success) {
        throw new Error(updateRes.message || t('Failed to update cost ratio'))
      }

      toast.success(t('Cost ratio updated'))
      // Saving appends a price version server-side, so the history list below is
      // stale too. ['cost'] does not prefix-match ['cost-versions', id], and the
      // global staleTime would serve the cached list if the admin reopens the
      // dialog to confirm — contradicting the copy that promises it was recorded.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['cost'] }),
        queryClient.invalidateQueries({
          queryKey: ['cost-versions', channelId],
        }),
      ])
      setOpen(false)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to update cost ratio')
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      trigger={
        <Button
          type='button'
          variant='ghost'
          size='icon'
          className='size-6'
          aria-label={t('Edit Cost Ratio')}
        >
          <Pencil className='size-3.5' />
        </Button>
      }
      title={t('Edit Cost Ratio')}
      description={channelName}
      contentClassName='sm:max-w-md'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => setOpen(false)}
            disabled={submitting}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={handleSubmit}
            disabled={!isValid || submitting}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='flex flex-col gap-4'>
        <div className='flex flex-col gap-2'>
          <Label>{t('Pricing Mode')}</Label>
          <Select
            value={mode}
            onValueChange={(value) => setMode(value as CostPricingMode)}
            items={[
              { value: 'ratio', label: t('Cost Ratio (CNY per USD)') },
              { value: 'discount', label: t('Cost Discount') },
            ]}
          >
            <SelectTrigger className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='ratio'>
                  {t('Cost Ratio (CNY per USD)')}
                </SelectItem>
                <SelectItem value='discount'>{t('Cost Discount')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        {mode === 'discount' ? (
          <div className='flex flex-col gap-2'>
            <Label htmlFor='cost-discount-input'>{t('Cost Discount')}</Label>
            <Input
              id='cost-discount-input'
              type='number'
              min={0}
              step={0.01}
              value={discountValue}
              onChange={(event) => setDiscountValue(event.target.value)}
            />
            <p className='text-muted-foreground text-xs'>
              {isValidDiscount
                ? t(
                    'Discount {{d}} × rate {{r}} → $1 list price costs ¥{{cny}}',
                    {
                      d: parsedDiscount,
                      r: exchangeRate,
                      cny: (parsedDiscount * exchangeRate).toFixed(2),
                    }
                  )
                : t('Used by cost accounting. Leave empty if unknown.')}
            </p>
          </div>
        ) : (
          <div className='flex flex-col gap-2'>
            <Label htmlFor='cost-ratio-input'>
              {t('Cost Ratio (CNY per USD)')}
            </Label>
            <Input
              id='cost-ratio-input'
              type='number'
              min={0}
              step={0.01}
              value={ratioValue}
              onChange={(event) => setRatioValue(event.target.value)}
            />
            <p className='text-muted-foreground text-xs'>
              {isValidRatio
                ? t(
                    'Ratio {{r}} → every $1 of list-price usage costs ¥{{cny}}',
                    { r: parsedRatio, cny: parsedRatio.toFixed(2) }
                  )
                : t('Used by cost accounting. Leave empty if unknown.')}
            </p>
          </div>
        )}

        <p className='text-muted-foreground text-xs'>
          {t(
            'Saving records the current price as a new version; historical ranges keep their original price.'
          )}
        </p>

        <Separator />

        <VersionHistoryPanel
          channelId={channelId}
          open={open}
          exchangeRate={exchangeRate}
        />
      </div>
    </Dialog>
  )
}

/**
 * "0.8 × ¥7.20" for discount mode, "¥2.50" for ratio mode, 'Not set' when the
 * mode's own price is missing.
 *
 * An empty cost_mode means ratio (see model.ChannelCostVersion.CostMode). Ratio
 * channels normally persist with no cost_mode key at all — channel-form.ts only
 * writes it for discount mode, and both the seeder and the auto-append copy the
 * channel setting verbatim. Testing `!== 'discount'` rather than `=== 'ratio'`
 * keeps a channel priced at ¥2.5/$1 from reading as unpriced, which would invite
 * the admin to record a duplicate historical version.
 */
function describeVersion(v: ChannelCostVersion, t: TFunction): string {
  if (v.cost_mode === 'discount') {
    return v.cost_discount > 0
      ? `${v.cost_discount} × ¥${v.exchange_rate.toFixed(2)}`
      : t('Not set')
  }
  return v.cost_ratio > 0 ? `¥${v.cost_ratio.toFixed(2)}` : t('Not set')
}

/**
 * The channel's price versions. Saving above writes the *current* price; this
 * panel is how a past price gets recorded, so cost for older logs stops being
 * computed at today's rate.
 */
function VersionHistoryPanel({
  channelId,
  open,
  exchangeRate,
}: {
  channelId: number
  open: boolean
  exchangeRate: number
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [adding, setAdding] = useState(false)

  const {
    data: versions = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['cost-versions', channelId],
    queryFn: () => getChannelCostVersions(channelId),
    enabled: open,
    // The global predicate only opts out of Axios 401/403, so a business refusal
    // (HTTP 200 + success:false, thrown as a plain Error) would retry four times
    // with backoff and hold this panel on "Loading..." for ~7s before showing the
    // reason. Retrying cannot change a deterministic refusal, and reopening the
    // dialog is a cheap manual retry for a genuine network blip.
    retry: false,
  })

  // Cost figures are derived from the versions, so the report is stale too.
  const invalidate = () => {
    void queryClient.invalidateQueries({
      queryKey: ['cost-versions', channelId],
    })
    void queryClient.invalidateQueries({ queryKey: ['cost'] })
  }

  // The api layer silences the global handlers, so onError here is the only
  // toast — and it carries the backend's reason ("cannot delete the last
  // version…") instead of a generic failure.
  const remove = useMutation({
    mutationFn: (versionId: number) => deleteChannelCostVersion(versionId),
    onSuccess: () => {
      toast.success(t('Price version deleted'))
      invalidate()
    },
    onError: (err) => {
      toast.error(errorMessage(err) || t('Failed to delete price version'))
    },
  })

  return (
    <div className='flex flex-col gap-2'>
      <div className='flex items-center justify-between'>
        <Label>{t('Version history')}</Label>
        {!adding && (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-7 gap-1 text-xs'
            onClick={() => setAdding(true)}
          >
            <Plus className='size-3.5' />
            {t('Add historical price')}
          </Button>
        )}
      </div>

      {adding && (
        <AddVersionForm
          channelId={channelId}
          defaultExchangeRate={exchangeRate}
          onDone={() => {
            setAdding(false)
            invalidate()
          }}
          onCancel={() => setAdding(false)}
        />
      )}

      {isLoading ? (
        <p className='text-muted-foreground text-xs'>{t('Loading...')}</p>
      ) : error ? (
        <p className='text-destructive text-xs'>
          {errorMessage(error) || t('Failed to load price versions')}
        </p>
      ) : (
        <ul className='flex max-h-48 flex-col gap-1 overflow-y-auto text-xs'>
          {versions.map((v) => (
            <li
              key={v.id}
              className='hover:bg-muted/50 flex items-center justify-between gap-2 rounded-md px-2 py-1.5'
            >
              <div className='flex min-w-0 flex-col leading-tight'>
                {/* Date + time, not date alone: the picker captures hours and
                    minutes, so two versions effective the same day would render
                    as identical rows and the admin could not tell which one the
                    delete button belongs to. */}
                <span className='font-medium tabular-nums'>
                  {v.effective_from === 0
                    ? t('Initial')
                    : formatDateTimeObject(new Date(v.effective_from * 1000))}
                </span>
                {v.note && (
                  <span className='text-muted-foreground truncate'>
                    {v.note}
                  </span>
                )}
              </div>
              <div className='flex items-center gap-2'>
                <span className='text-muted-foreground tabular-nums'>
                  {describeVersion(v, t)}
                </span>
                {/* The seeded "since forever" row is what every log before the
                    first real version prices against; the API refuses to
                    delete it, so don't offer to. */}
                {v.effective_from !== 0 && (
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='size-6'
                    aria-label={t('Delete')}
                    // Scoped to the row in flight: one shared mutation would
                    // otherwise grey out every other row's delete button.
                    disabled={remove.isPending && remove.variables === v.id}
                    onClick={() => remove.mutate(v.id)}
                  >
                    <Trash2 className='size-3.5' />
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/**
 * Records a price that was already in effect from some past date. The exchange
 * rate is captured with the version rather than read at query time, so the
 * cost of old logs stays what it actually was.
 */
function AddVersionForm({
  channelId,
  defaultExchangeRate,
  onDone,
  onCancel,
}: {
  channelId: number
  defaultExchangeRate: number
  onDone: () => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const [effectiveFrom, setEffectiveFrom] = useState<Date | undefined>()
  const [mode, setMode] = useState<CostPricingMode>('ratio')
  const [value, setValue] = useState('')
  const [rate, setRate] = useState(
    Number.isFinite(defaultExchangeRate) ? String(defaultExchangeRate) : ''
  )
  const [note, setNote] = useState('')

  const parsedValue = Number(value)
  const parsedRate = Number(rate)
  // Both must be > 0, not >= 0: the API refuses a zero ratio/discount because
  // it would freeze a version whose logs can never be priced.
  const isValid =
    effectiveFrom !== undefined &&
    value.trim() !== '' &&
    Number.isFinite(parsedValue) &&
    parsedValue > 0 &&
    Number.isFinite(parsedRate) &&
    parsedRate > 0

  const create = useMutation({
    mutationFn: () =>
      createChannelCostVersion(channelId, {
        // effective_from 0 is reserved for the seeded row, and the picker
        // requires a date, so this is always a real timestamp.
        effective_from: dateToUnixTimestamp(effectiveFrom as Date),
        cost_mode: mode,
        cost_ratio: mode === 'ratio' ? parsedValue : 0,
        cost_discount: mode === 'discount' ? parsedValue : 0,
        exchange_rate: parsedRate,
        note: note.trim() || undefined,
      }),
    onSuccess: () => {
      toast.success(t('Price version added'))
      onDone()
    },
    onError: (err) => {
      toast.error(errorMessage(err) || t('Failed to add price version'))
    },
  })

  return (
    <div className='bg-muted/40 flex flex-col gap-2 rounded-md border p-2'>
      <div className='flex flex-col gap-1'>
        <Label className='text-xs' htmlFor='cost-version-from'>
          {t('Price effective from')}
        </Label>
        <DateTimePicker
          id='cost-version-from'
          value={effectiveFrom}
          onChange={setEffectiveFrom}
          className='w-full'
        />
      </div>

      <div className='grid grid-cols-2 gap-2'>
        <div className='flex flex-col gap-1'>
          <Label className='text-xs' htmlFor='cost-version-mode'>
            {t('Pricing Mode')}
          </Label>
          <Select
            value={mode}
            onValueChange={(v) => setMode(v as CostPricingMode)}
            items={[
              { value: 'ratio', label: t('Cost Ratio (CNY per USD)') },
              { value: 'discount', label: t('Cost Discount') },
            ]}
          >
            <SelectTrigger id='cost-version-mode' className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='ratio'>
                  {t('Cost Ratio (CNY per USD)')}
                </SelectItem>
                <SelectItem value='discount'>{t('Cost Discount')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
        <div className='flex flex-col gap-1'>
          <Label className='text-xs' htmlFor='cost-version-value'>
            {mode === 'discount'
              ? t('Cost Discount')
              : t('Cost Ratio (CNY per USD)')}
          </Label>
          <Input
            id='cost-version-value'
            type='number'
            min={0.01}
            step={0.01}
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        </div>
      </div>

      <div className='flex flex-col gap-1'>
        <Label className='text-xs' htmlFor='cost-version-rate'>
          {t('Settlement exchange rate')}
        </Label>
        <Input
          id='cost-version-rate'
          type='number'
          min={0.01}
          step={0.1}
          value={rate}
          onChange={(e) => setRate(e.target.value)}
        />
        <p className='text-muted-foreground text-xs'>
          {t('Frozen with this version so historical cost never drifts.')}
        </p>
      </div>

      <div className='flex flex-col gap-1'>
        <Label className='text-xs' htmlFor='cost-version-note'>
          {t('Note')}
        </Label>
        <Input
          id='cost-version-note'
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder={t('Optional')}
        />
      </div>

      <div className='flex justify-end gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={onCancel}
          disabled={create.isPending}
        >
          {t('Cancel')}
        </Button>
        <Button
          type='button'
          size='sm'
          onClick={() => create.mutate()}
          disabled={!isValid || create.isPending}
        >
          {t('Add')}
        </Button>
      </div>
    </div>
  )
}
