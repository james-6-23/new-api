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
import { useQueryClient } from '@tanstack/react-query'
import { Pencil } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getChannel, updateChannel } from '@/features/channels/api'
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
import { Dialog } from '@/components/dialog'

type CostPricingMode = 'ratio' | 'discount'

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
      await queryClient.invalidateQueries({ queryKey: ['cost'] })
      setOpen(false)
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to update cost ratio')
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
      contentClassName='sm:max-w-sm'
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
      </div>
    </Dialog>
  )
}
