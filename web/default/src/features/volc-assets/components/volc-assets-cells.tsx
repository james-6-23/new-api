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
import { Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { Button } from '@/components/ui/button'
import { SUCCESS_MESSAGES } from '../constants'

/**
 * 资产 ID 一键复制。
 *
 * 官方 ID 形如 asset-xxxxxxxx,提交视频任务时要原样拼成 asset://<id>,手抄极易出错,
 * 所以整格可点即复制,而不是只给一个小图标。
 */
export function VolcAssetIdCell({ value }: { value: string }) {
  const { t } = useTranslation()

  if (!value) {
    return <span className='text-muted-foreground'>-</span>
  }

  const handleCopy = async () => {
    const ok = await copyToClipboard(value)
    if (ok) {
      toast.success(t(SUCCESS_MESSAGES.COPY_SUCCESS))
    }
  }

  return (
    <Button
      variant='ghost'
      size='sm'
      onClick={handleCopy}
      aria-label={t('Copy asset ID')}
      className='h-7 max-w-full min-w-0 justify-start gap-1.5 px-0 font-mono text-sm hover:bg-transparent'
    >
      <span className='truncate'>{value}</span>
      <Copy className='text-muted-foreground size-3.5 shrink-0' />
    </Button>
  )
}
