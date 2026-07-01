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
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { exportBillSummary, type BillExportParams } from '../api'

function toUnix(local: string): number | undefined {
  if (!local) return undefined
  const ms = new Date(local).getTime()
  return Number.isNaN(ms) ? undefined : Math.floor(ms / 1000)
}

export function BillExportPage() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()

  const [start, setStart] = useState('')
  const [end, setEnd] = useState('')
  const [username, setUsername] = useState('')
  const [channel, setChannel] = useState('')
  const [tokenName, setTokenName] = useState('')
  const [modelName, setModelName] = useState('')
  const [rate, setRate] = useState('')
  const [withDetail, setWithDetail] = useState(false)
  const [splitModel, setSplitModel] = useState(false)
  const [loading, setLoading] = useState(false)

  async function handleExport() {
    setLoading(true)
    try {
      const params: BillExportParams = {
        start_timestamp: toUnix(start),
        end_timestamp: toUnix(end),
        token_name: tokenName || undefined,
        model_name: modelName || undefined,
        with_detail: withDetail ? 1 : 0,
        detail_split_model: withDetail && splitModel ? 1 : 0,
        exchange_rate: rate ? Number(rate) : undefined,
      }
      if (isAdmin) {
        params.username = username || undefined
        params.channel = channel ? Number(channel) : undefined
      }
      const { truncated } = await exportBillSummary(params, isAdmin)
      if (truncated) {
        toast.warning(t('Export truncated, please narrow the time range'))
      } else {
        toast.success(t('Export Summary Bill'))
      }
    } catch (e) {
      toast.error(String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className='p-4 max-w-2xl space-y-4'>
      <h1 className='text-xl font-semibold'>{t('Bill Management')}</h1>

      <div className='grid grid-cols-2 gap-4'>
        <div className='space-y-1'>
          <Label>{t('Start Time')}</Label>
          <Input
            type='datetime-local'
            value={start}
            onChange={(e) => setStart(e.target.value)}
          />
        </div>
        <div className='space-y-1'>
          <Label>{t('End Time')}</Label>
          <Input
            type='datetime-local'
            value={end}
            onChange={(e) => setEnd(e.target.value)}
          />
        </div>

        {isAdmin && (
          <>
            <div className='space-y-1'>
              <Label>{t('Username')}</Label>
              <Input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </div>
            <div className='space-y-1'>
              <Label>{t('Channel ID')}</Label>
              <Input
                value={channel}
                onChange={(e) => setChannel(e.target.value)}
              />
            </div>
          </>
        )}

        <div className='space-y-1'>
          <Label>{t('Token Name')}</Label>
          <Input
            value={tokenName}
            onChange={(e) => setTokenName(e.target.value)}
          />
        </div>
        <div className='space-y-1'>
          <Label>{t('Model Name')}</Label>
          <Input
            value={modelName}
            onChange={(e) => setModelName(e.target.value)}
          />
        </div>
        <div className='space-y-1'>
          <Label>{t('Exchange rate (USD to CNY)')}</Label>
          <Input
            value={rate}
            onChange={(e) => setRate(e.target.value)}
            placeholder='7.3'
          />
        </div>
      </div>

      <div className='flex items-center gap-2'>
        <Switch
          checked={withDetail}
          onCheckedChange={setWithDetail}
        />
        <Label>{t('Include daily detail')}</Label>
      </div>
      {withDetail && (
        <div className='flex items-center gap-2'>
          <Switch
            checked={splitModel}
            onCheckedChange={setSplitModel}
          />
          <Label>{t('Split detail by model')}</Label>
        </div>
      )}

      <Button onClick={handleExport} disabled={loading}>
        {t('Export Summary Bill')}
      </Button>
    </div>
  )
}
