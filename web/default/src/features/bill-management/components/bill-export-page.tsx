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
import { Table, TableHeader, TableBody, TableHead, TableRow, TableCell } from '@/components/ui/table'
import { exportBillSummary, getBillSummary, type BillExportParams, type BillSummaryResponse } from '../api'

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
  const [data, setData] = useState<BillSummaryResponse | null>(null)
  const [page, setPage] = useState(1)
  const pageSize = 20
  const [querying, setQuerying] = useState(false)

  async function runQuery(targetPage: number) {
    setQuerying(true)
    try {
      const params = {
        start_timestamp: toUnix(start),
        end_timestamp: toUnix(end),
        token_name: tokenName || undefined,
        model_name: modelName || undefined,
        exchange_rate: rate ? Number(rate) : undefined,
        ...(isAdmin
          ? { username: username || undefined, channel: channel ? Number(channel) : undefined }
          : {}),
      }
      const res = await getBillSummary(params, isAdmin, targetPage, pageSize)
      setData(res)
      setPage(targetPage)
    } catch (e) {
      toast.error(String(e))
    } finally {
      setQuerying(false)
    }
  }

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

      <div className='flex gap-2'>
        <Button onClick={() => runQuery(1)} disabled={querying}>
          {t('Query')}
        </Button>
        <Button variant='outline' onClick={handleExport} disabled={loading}>
          {t('Export Summary Bill')}
        </Button>
      </div>

      {data && (
        <div className='space-y-2'>
          <div className='text-sm text-muted-foreground'>
            {t('Total')}: ${data.summary.total_amount_usd.toFixed(6)} / ¥
            {data.summary.total_amount_cny.toFixed(6)} · {t('Prompt Tokens')}{' '}
            {data.summary.total_prompt_tokens} · {t('Completion Tokens')}{' '}
            {data.summary.total_completion_tokens} · {t('Cache Read Tokens')}{' '}
            {data.summary.total_cache_read_tokens} · {t('Cache Creation Tokens')}{' '}
            {data.summary.total_cache_creation_tokens}
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Date')}</TableHead>
                {isAdmin && <TableHead>{t('Username')}</TableHead>}
                {isAdmin && <TableHead>{t('Channel ID')}</TableHead>}
                <TableHead>{t('Token Name')}</TableHead>
                <TableHead>{t('Model Name')}</TableHead>
                <TableHead>{t('Amount (USD)')}</TableHead>
                <TableHead>{t('Exchange Rate')}</TableHead>
                <TableHead>{t('Amount (CNY)')}</TableHead>
                <TableHead>{t('Prompt Tokens')}</TableHead>
                <TableHead>{t('Completion Tokens')}</TableHead>
                <TableHead>{t('Cache Read Tokens')}</TableHead>
                <TableHead>{t('Cache Creation Tokens')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((it, i) => (
                <TableRow key={i}>
                  <TableCell>{it.date}</TableCell>
                  {isAdmin && <TableCell>{it.username}</TableCell>}
                  {isAdmin && <TableCell>{it.channel_id}</TableCell>}
                  <TableCell>{it.token_name}</TableCell>
                  <TableCell>{it.model_name}</TableCell>
                  <TableCell>${it.amount_usd.toFixed(6)}</TableCell>
                  <TableCell>{it.exchange_rate}</TableCell>
                  <TableCell>¥{it.amount_cny.toFixed(6)}</TableCell>
                  <TableCell>{it.prompt_tokens}</TableCell>
                  <TableCell>{it.completion_tokens}</TableCell>
                  <TableCell>{it.cache_read_tokens}</TableCell>
                  <TableCell>{it.cache_creation_tokens}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <div className='flex items-center gap-2'>
            <Button variant='outline' disabled={page <= 1 || querying} onClick={() => runQuery(page - 1)}>
              {t('Previous')}
            </Button>
            <span className='text-sm'>
              {page} / {Math.max(1, Math.ceil(data.total / pageSize))}
            </span>
            <Button
              variant='outline'
              disabled={page >= Math.ceil(data.total / pageSize) || querying}
              onClick={() => runQuery(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
