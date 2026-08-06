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
/**
 * Bill management page — usage-logs-style toolbar + table.
 * Filters live in a compact toolbar row (like the usage-logs filter bar), the
 * summary strip mirrors the usage-logs stat line, and the table uses
 * StaticDataTable so column styling matches the rest of the console.
 */
import { useState } from 'react'
import { Download, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from '@/components/ui/select'
import { SectionPageLayout } from '@/components/layout'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { LogsFilterField } from '@/features/usage-logs/components/logs-filter-toolbar'
import {
  exportBillSummary,
  getBillSummary,
  type BillExportParams,
  type BillSummaryItem,
  type BillSummaryResponse,
} from '../api'

function toUnix(local: string): number | undefined {
  if (!local) return undefined
  const ms = new Date(local).getTime()
  return Number.isNaN(ms) ? undefined : Math.floor(ms / 1000)
}

function money(v: number, symbol: string): string {
  return `${symbol}${v.toFixed(6)}`
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
  const [billMode, setBillMode] = useState<'internal' | 'external'>('internal')
  const [granularity, setGranularity] = useState<'day' | 'week' | 'month'>(
    'day'
  )
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
        granularity,
        ...(isAdmin
          ? {
              username: username || undefined,
              channel: channel ? Number(channel) : undefined,
            }
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
        bill_mode: billMode,
        granularity,
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

  const columns: StaticDataTableColumn<BillSummaryItem>[] = [
    { id: 'date', header: t('Date'), cell: (it) => it.date },
    ...(isAdmin
      ? ([
          { id: 'username', header: t('Username'), cell: (it) => it.username },
          {
            id: 'channel',
            header: t('Channel ID'),
            cell: (it) => (it.channel_id ? it.channel_id : '-'),
          },
        ] as StaticDataTableColumn<BillSummaryItem>[])
      : []),
    {
      id: 'token',
      header: t('Token Name'),
      cell: (it) => it.token_name || '-',
    },
    { id: 'model', header: t('Model Name'), cell: (it) => it.model_name },
    {
      id: 'requests',
      header: t('Request Count'),
      cell: (it) => <span className='tabular-nums'>{it.request_count}</span>,
    },
    {
      id: 'list_amount',
      header: t('List Amount (USD)'),
      cell: (it) => (
        <span className='font-mono text-xs tabular-nums'>
          {money(it.list_amount_usd, '$')}
        </span>
      ),
    },
    {
      id: 'amount_usd',
      header: t('Amount (USD)'),
      cell: (it) => (
        <span className='border-border/80 bg-muted/60 inline-flex h-6 w-fit items-center rounded-md border px-2 font-mono text-xs font-semibold tabular-nums'>
          {money(it.amount_usd, '$')}
        </span>
      ),
    },
    {
      id: 'amount_cny',
      header: t('Amount (CNY)'),
      cell: (it) => (
        <span className='font-mono text-xs tabular-nums'>
          {money(it.amount_cny, '¥')}
        </span>
      ),
    },
    {
      id: 'tokens',
      header: `${t('Prompt Tokens')} / ${t('Completion Tokens')}`,
      cell: (it) => (
        <span className='font-mono text-xs tabular-nums'>
          {it.prompt_tokens.toLocaleString()} /{' '}
          {it.completion_tokens.toLocaleString()}
        </span>
      ),
    },
    {
      id: 'cache',
      header: `${t('Cache Read Tokens')} / ${t('Cache Creation Tokens')}`,
      cell: (it) => (
        <span className='text-muted-foreground font-mono text-xs tabular-nums'>
          {it.cache_read_tokens.toLocaleString()} /{' '}
          {it.cache_creation_tokens.toLocaleString()}
        </span>
      ),
    },
  ]

  const totalPages = data ? Math.max(1, Math.ceil(data.total / pageSize)) : 1

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Bill Management')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-3 overflow-auto pt-2'>
          {/* 筛选工具栏（对齐使用日志筛选条样式） */}
          <div className='flex flex-wrap items-end gap-2'>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Start Time')}
              </Label>
              <Input
                type='datetime-local'
                className='h-8 w-48'
                value={start}
                onChange={(e) => setStart(e.target.value)}
              />
            </div>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('End Time')}
              </Label>
              <Input
                type='datetime-local'
                className='h-8 w-48'
                value={end}
                onChange={(e) => setEnd(e.target.value)}
              />
            </div>
            {isAdmin && (
              <>
                <Input
                  className='h-8 w-32'
                  placeholder={t('Username')}
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                />
                <Input
                  className='h-8 w-24'
                  placeholder={t('Channel ID')}
                  value={channel}
                  onChange={(e) => setChannel(e.target.value)}
                />
              </>
            )}
            <Input
              className='h-8 w-32'
              placeholder={t('Token Name')}
              value={tokenName}
              onChange={(e) => setTokenName(e.target.value)}
            />
            <Input
              className='h-8 w-40'
              placeholder={t('Model Name')}
              value={modelName}
              onChange={(e) => setModelName(e.target.value)}
            />
            <LogsFilterField className='w-28'>
              <Select
                items={[
                  { value: 'day', label: t('By day') },
                  { value: 'week', label: t('By week') },
                  { value: 'month', label: t('By month') },
                ]}
                value={granularity}
                onValueChange={(value) =>
                  setGranularity(
                    value === 'week' || value === 'month' ? value : 'day'
                  )
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectItem value='day'>{t('By day')}</SelectItem>
                    <SelectItem value='week'>{t('By week')}</SelectItem>
                    <SelectItem value='month'>{t('By month')}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </LogsFilterField>
            <Input
              className='h-8 w-20'
              placeholder={t('Exchange Rate')}
              value={rate}
              onChange={(e) => setRate(e.target.value)}
            />

            {/* 导出配置 + 查询/导出按钮（右侧收拢，按钮相邻） */}
            <div className='ml-auto flex flex-wrap items-end gap-2'>
              <LogsFilterField className='w-44'>
                <Select
                  items={[
                    {
                      value: 'internal',
                      label: t('Internal (split by channel & model)'),
                    },
                    {
                      value: 'external',
                      label: t('External customer (merged channels)'),
                    },
                  ]}
                  value={billMode}
                  onValueChange={(value) =>
                    setBillMode(value === 'external' ? 'external' : 'internal')
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='internal'>
                        {t('Internal (split by channel & model)')}
                      </SelectItem>
                      <SelectItem value='external'>
                        {t('External customer (merged channels)')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </LogsFilterField>
              <LogsFilterField className='w-36'>
                <Select
                  items={[
                    { value: 'no', label: t('Exclude daily detail') },
                    { value: 'yes', label: t('Include daily detail') },
                  ]}
                  value={withDetail ? 'yes' : 'no'}
                  onValueChange={(value) => setWithDetail(value === 'yes')}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='no'>
                        {t('Exclude daily detail')}
                      </SelectItem>
                      <SelectItem value='yes'>
                        {t('Include daily detail')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </LogsFilterField>
              {withDetail && (
                <LogsFilterField className='w-40'>
                  <Select
                    items={[
                      { value: 'no', label: t('Detail not split by model') },
                      { value: 'yes', label: t('Split detail by model') },
                    ]}
                    value={splitModel ? 'yes' : 'no'}
                    onValueChange={(value) => setSplitModel(value === 'yes')}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='no'>
                          {t('Detail not split by model')}
                        </SelectItem>
                        <SelectItem value='yes'>
                          {t('Split detail by model')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </LogsFilterField>
              )}
              <Button
                size='sm'
                className='h-8'
                onClick={() => runQuery(1)}
                disabled={querying}
              >
                <Search className='mr-1 size-3.5' />
                {t('Query')}
              </Button>
              <Button
                size='sm'
                variant='outline'
                className='h-8'
                onClick={handleExport}
                disabled={loading}
              >
                <Download className='mr-1 size-3.5' />
                {t('Export Summary Bill')}
              </Button>
            </div>
          </div>

          {/* 汇总统计条（对齐使用日志统计区样式） */}
          {data && (
            <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
              <span>
                {t('Total')}:{' '}
                <span className='text-foreground font-semibold tabular-nums'>
                  ${data.summary.total_amount_usd.toFixed(6)}
                </span>{' '}
                / ¥{data.summary.total_amount_cny.toFixed(6)}
              </span>
              <span>
                {t('Request Count')}{' '}
                <span className='tabular-nums'>
                  {data.summary.total_request_count}
                </span>
              </span>
              <span>
                {t('List Amount (USD)')}{' '}
                <span className='tabular-nums'>
                  ${data.summary.total_list_amount_usd.toFixed(6)}
                </span>
              </span>
              <span>
                {t('Prompt Tokens')}{' '}
                <span className='tabular-nums'>
                  {data.summary.total_prompt_tokens.toLocaleString()}
                </span>
              </span>
              <span>
                {t('Completion Tokens')}{' '}
                <span className='tabular-nums'>
                  {data.summary.total_completion_tokens.toLocaleString()}
                </span>
              </span>
              <span>
                {t('Cache Read Tokens')}{' '}
                <span className='tabular-nums'>
                  {data.summary.total_cache_read_tokens.toLocaleString()}
                </span>
              </span>
              <span>
                {t('Cache Creation Tokens')}{' '}
                <span className='tabular-nums'>
                  {data.summary.total_cache_creation_tokens.toLocaleString()}
                </span>
              </span>
            </div>
          )}

          {/* 表格区 */}
          {data && (
            <div className='min-h-0 flex-1 space-y-2'>
              <StaticDataTable<BillSummaryItem>
                columns={columns}
                data={data.items}
                getRowKey={(_, i) => i}
                empty={data.items.length === 0}
                emptyContent={t('No Logs Found')}
                tableClassName='text-[13px]'
              />
              <div className='flex items-center gap-2'>
                <Button
                  size='sm'
                  variant='outline'
                  disabled={page <= 1 || querying}
                  onClick={() => runQuery(page - 1)}
                >
                  {t('Previous Page')}
                </Button>
                <span className='text-muted-foreground text-sm tabular-nums'>
                  {page} / {totalPages}
                </span>
                <Button
                  size='sm'
                  variant='outline'
                  disabled={page >= totalPages || querying}
                  onClick={() => runQuery(page + 1)}
                >
                  {t('Next Page')}
                </Button>
              </div>
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
