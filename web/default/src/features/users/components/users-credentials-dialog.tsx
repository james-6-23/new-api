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
import { useMemo } from 'react'
import { Copy, CopyCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  renderCopyItem,
  useAutoCreateUserSettings,
} from '../lib/auto-create-settings'
import { useUsers } from './users-provider'

type UsersCredentialsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through to legacy path
  }
  // Legacy fallback for non-secure contexts (rare in admin deployments but cheap to keep).
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

export function UsersCredentialsDialog({
  open,
  onOpenChange,
}: UsersCredentialsDialogProps) {
  const { t } = useTranslation()
  const { credentials } = useUsers()
  const { settings } = useAutoCreateUserSettings()

  const rows = useMemo(() => {
    if (!credentials) return []
    return settings.copy_templates.map((item) => ({
      label: item.label,
      text: renderCopyItem(item.template, {
        username: credentials.username,
        password: credentials.password,
        site: settings.site_url,
      }),
    }))
  }, [credentials, settings])

  const handleCopyRow = async (row: { label: string; text: string }) => {
    const ok = await copyToClipboard(row.text)
    if (ok) {
      toast.success(t('Copied'))
    } else {
      toast.error(t('Copy failed'))
    }
  }

  const handleCopyAll = async () => {
    const blob = rows.map((r) => `${r.label}: ${r.text}`).join('\n')
    const ok = await copyToClipboard(blob)
    if (ok) {
      toast.success(t('Copied'))
    } else {
      toast.error(t('Copy failed'))
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-[520px]'>
        <DialogHeader>
          <DialogTitle>{t('User created')}</DialogTitle>
          <DialogDescription>
            {t(
              'Copy these credentials and share them through your secure channel. They will not be shown again.'
            )}
          </DialogDescription>
        </DialogHeader>

        {rows.length === 0 ? (
          <div className='text-muted-foreground text-sm'>
            {t(
              'No copy items are configured. Add some in System Settings → Operations → Auto Create User.'
            )}
          </div>
        ) : (
          <div className='flex flex-col gap-2'>
            {rows.map((row, idx) => (
              <div
                key={idx}
                className='flex items-center gap-2 rounded-md border p-2'
              >
                <div className='min-w-[64px] text-xs text-muted-foreground'>
                  {row.label}
                </div>
                <div className='flex-1 break-all text-sm font-mono'>
                  {row.text || (
                    <span className='text-muted-foreground italic'>
                      {t('(empty)')}
                    </span>
                  )}
                </div>
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  onClick={() => handleCopyRow(row)}
                >
                  <Copy className='h-4 w-4' />
                </Button>
              </div>
            ))}
          </div>
        )}

        <DialogFooter>
          <DialogClose render={<Button variant='outline' />}>
            {t('Close')}
          </DialogClose>
          {rows.length > 0 && (
            <Button onClick={handleCopyAll}>
              <CopyCheck className='mr-1 h-4 w-4' />
              {t('Copy all')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
