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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog } from '@/components/dialog'
import { setUserAssetLimit } from '../api'
import { VOLC_ASSET_LIMIT_UNLIMITED } from '../types'

type LimitMode = 'default' | 'custom' | 'unlimited'

interface UserAssetLimitDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  /** 该用户当前的个人覆盖值：0/缺省=沿用全局默认，-1=不限。 */
  currentLimit: number
  onSuccess: () => void
}

function modeOf(limit: number): LimitMode {
  if (limit === VOLC_ASSET_LIMIT_UNLIMITED) return 'unlimited'
  if (limit > 0) return 'custom'
  return 'default'
}

/**
 * 素材库提额。
 *
 * 三档而不是一个数字输入框：光给输入框的话，「回落到全局默认」和「不限」
 * 都得靠管理员记住 0 和 -1 两个哨兵值，没人记得住。
 */
export function UserAssetLimitDialog(props: UserAssetLimitDialogProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const [mode, setMode] = useState<LimitMode>(modeOf(props.currentLimit))
  const [amount, setAmount] = useState(
    props.currentLimit > 0 ? String(props.currentLimit) : ''
  )
  const [loading, setLoading] = useState(false)

  // 每次打开都回到该用户当前的覆盖值：对话框不卸载，上一次编辑到一半的
  // 输入会留在 state 里，换个用户再打开就会看到别人的数字。
  const [openedWith, setOpenedWith] = useState(props.open)
  if (props.open !== openedWith) {
    setOpenedWith(props.open)
    if (props.open) {
      setMode(modeOf(props.currentLimit))
      setAmount(props.currentLimit > 0 ? String(props.currentLimit) : '')
    }
  }

  const globalDefault = Number(status?.volc_asset_limit_default ?? 0)
  const enforced = Boolean(status?.volc_asset_limit_enabled)

  const handleConfirm = async () => {
    let value = 0
    if (mode === 'unlimited') {
      value = VOLC_ASSET_LIMIT_UNLIMITED
    } else if (mode === 'custom') {
      value = parseInt(amount, 10)
      if (!Number.isFinite(value) || value <= 0) {
        toast.error(t('Enter a limit greater than zero'))
        return
      }
    }

    setLoading(true)
    try {
      const result = await setUserAssetLimit({
        id: props.userId,
        action: 'set_volc_asset_limit',
        value,
      })
      if (result.success) {
        toast.success(t('Asset limit updated'))
        props.onOpenChange(false)
        props.onSuccess()
      } else {
        toast.error(result.message || t('Failed to update asset limit'))
      }
    } catch (e: unknown) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to update asset limit')
      )
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Asset Library Limit')}
      description={t('How many assets this user may keep in the library')}
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleConfirm} disabled={loading}>
            {loading ? t('Processing...') : t('Confirm')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='text-muted-foreground text-sm'>
          {enforced
            ? t('Site default: {{limit}} assets', { limit: globalDefault })
            : t(
                'Site default: {{limit}} assets (not enforced — shown as a reference only)',
                { limit: globalDefault }
              )}
        </div>

        <div className='space-y-2'>
          <Label>{t('Mode')}</Label>
          <div className='flex gap-1'>
            {(['default', 'custom', 'unlimited'] as const).map((m) => (
              <Button
                key={m}
                type='button'
                variant={mode === m ? 'default' : 'outline'}
                size='sm'
                onClick={() => setMode(m)}
              >
                {m === 'default'
                  ? t('Use site default')
                  : m === 'custom'
                    ? t('Custom limit')
                    : t('Unlimited')}
              </Button>
            ))}
          </div>
        </div>

        {mode === 'custom' && (
          <div className='space-y-2'>
            <Label>{t('Assets')}</Label>
            <Input
              type='number'
              min={1}
              step={1}
              placeholder={String(globalDefault || 100)}
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleConfirm()
              }}
            />
          </div>
        )}
      </div>
    </Dialog>
  )
}
