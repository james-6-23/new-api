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
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { syncVolcAssets } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import { useVolcAssets } from './volc-assets-provider'

/**
 * 回源官方全量对账并认领孤儿。
 *
 * 同步是幂等的,单渠道失败只会体现在返回的 items[].error 里,所以这里把失败渠道
 * 单独 toast 出来,而不是整体报一个"同步失败"就完事。
 */
export function VolcAssetsPrimaryButtons() {
  const { t } = useTranslation()
  const { triggerRefresh } = useVolcAssets()
  const [isSyncing, setIsSyncing] = useState(false)

  const handleSync = async () => {
    setIsSyncing(true)
    try {
      const result = await syncVolcAssets()
      if (!result.success) {
        toast.error(result.message || t(ERROR_MESSAGES.SYNC_FAILED))
        return
      }
      const items = result.data?.items || []
      const failed = items.filter((item) => item.error)
      const claimed = items.reduce((sum, item) => sum + item.claimed, 0)
      const refreshed = items.reduce((sum, item) => sum + item.refreshed, 0)
      const stale = items.reduce((sum, item) => sum + item.stale, 0)

      toast.success(
        `${t(SUCCESS_MESSAGES.SYNC_COMPLETED)} · ${t('Claimed')} ${claimed} · ${t('Refreshed')} ${refreshed} · ${t('Stale')} ${stale}`
      )
      failed.forEach((item) => {
        toast.error(`#${item.channel_id}: ${item.error}`)
      })
      triggerRefresh()
    } finally {
      setIsSyncing(false)
    }
  }

  return (
    <Button variant='outline' onClick={handleSync} disabled={isSyncing}>
      <RefreshCw className={isSyncing ? 'animate-spin' : undefined} />
      {isSyncing ? t('Syncing...') : t('Sync from Upstream')}
    </Button>
  )
}
