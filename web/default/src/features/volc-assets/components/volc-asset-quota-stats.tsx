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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getSelfVolcAssetQuota, getVolcAssetQuota } from '../api'
import { useVolcAssets } from './volc-assets-provider'

/**
 * 额度展示。
 *
 * 不做成独立页签的卡片墙：额度是看表格时的参照物（还能建几条、哪个渠道该清理），
 * 拆到另一个页签就得来回切。这里做成筛选栏行内的一排徽章，跟表格同屏。
 *
 * 上限没有官方 API 可查 —— 火山把素材配额放在控制台「配额管理」里，按主账号计，
 * 提额走「配额中心 → 申请配额」。故站点侧的上限只能由管理员配置。
 */
function QuotaBadge(props: {
  label: string
  value: string | number
  accent: string
  hint?: string
}) {
  const badge = (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
  if (!props.hint) return badge
  return (
    <Tooltip>
      <TooltipTrigger render={badge} />
      <TooltipContent>{props.hint}</TooltipContent>
    </Tooltip>
  )
}

function QuotaSkeleton() {
  return (
    <div className='flex items-center gap-2'>
      <Skeleton className='h-7 w-[120px] rounded-md' />
      <Skeleton className='h-7 w-[120px] rounded-md' />
      <Skeleton className='h-7 w-[100px] rounded-md' />
    </div>
  )
}

/**
 * 客户视角的额度：我能建多少、已经建了多少、还剩多少。
 *
 * 上限为 0 表示站点没设限，此时只报已用量 —— 画一个「12 / 0」既难看也没意义。
 */
export function VolcAssetSelfQuotaStats() {
  const { t } = useTranslation()
  const { refreshTrigger } = useVolcAssets()

  const { data, isLoading } = useQuery({
    queryKey: ['volc-assets', 'self-quota', refreshTrigger],
    queryFn: async () => {
      const result = await getSelfVolcAssetQuota()
      return result.success ? result.data : undefined
    },
    placeholderData: (previousData) => previousData,
  })

  if (isLoading) return <QuotaSkeleton />
  if (!data) return null

  const hasLimit = data.limit > 0
  const remaining = Math.max(0, data.limit - data.used)

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <QuotaBadge
        label={t('Assets Used')}
        value={
          hasLimit
            ? `${formatNumber(data.used)} / ${formatNumber(data.limit)}`
            : formatNumber(data.used)
        }
        accent='bg-teal-500'
        hint={
          data.count_asset_groups
            ? t('Assets and asset groups both count toward this limit.')
            : t('Only assets count toward this limit, not asset groups.')
        }
      />
      {hasLimit && (
        <QuotaBadge
          label={t('Remaining')}
          value={formatNumber(remaining)}
          accent={remaining > 0 ? 'bg-sky-500' : 'bg-rose-500'}
          hint={
            data.enforced
              ? t('New assets are rejected once the limit is reached.')
              : t(
                  'The limit is informational right now; creation is not blocked.'
                )
          }
        />
      )}
    </div>
  )
}

/**
 * 超管视角的对账：每个渠道一组徽章。
 *
 * 上游数是权威总数（含孤儿），本地数是账本数，孤儿与未记录分列 —— 前者靠超管
 * 清理，后者靠跑同步消化，处置完全不同，合成一个数就没法判断该做哪件事。
 */
export function VolcAssetChannelQuotaStats() {
  const { t } = useTranslation()
  const { refreshTrigger } = useVolcAssets()

  const { data, isLoading } = useQuery({
    queryKey: ['volc-assets', 'quota', refreshTrigger],
    queryFn: async () => {
      const result = await getVolcAssetQuota()
      return result.success ? result.data?.items || [] : []
    },
    placeholderData: (previousData) => previousData,
  })

  if (isLoading) return <QuotaSkeleton />

  const items = data || []
  if (items.length === 0) return null

  return (
    <div className='flex flex-wrap items-center gap-2'>
      {items.map((item) => {
        const upstream = item.upstream_assets + item.upstream_groups
        const orphans = item.orphan_assets + item.orphan_groups
        return (
          <div
            key={item.channel_id}
            className='flex flex-wrap items-center gap-1.5'
          >
            <span className='text-muted-foreground text-xs font-medium'>
              {item.channel_name || `#${item.channel_id}`}
            </span>
            {item.error ? (
              <QuotaBadge
                label={t('Error')}
                value={t('Unavailable')}
                accent='bg-rose-500'
                hint={item.error}
              />
            ) : (
              <>
                <QuotaBadge
                  label={t('Upstream Used')}
                  value={
                    item.quota_limit > 0
                      ? `${formatNumber(upstream)} / ${formatNumber(item.quota_limit)}`
                      : formatNumber(upstream)
                  }
                  accent='bg-teal-500'
                  hint={
                    item.quota_limit > 0
                      ? t('Set manually in the channel settings')
                      : t('No limit configured for this channel')
                  }
                />
                <QuotaBadge
                  label={t('Local Ledger')}
                  value={formatNumber(item.local_assets + item.local_groups)}
                  accent='bg-sky-500'
                />
                {(orphans > 0 || item.unrecorded > 0) && (
                  <QuotaBadge
                    label={t('Orphan Records')}
                    value={`${formatNumber(orphans)} / ${formatNumber(item.unrecorded)}`}
                    accent='bg-rose-500'
                    hint={t(
                      'Orphans need cleanup; unrecorded upstream assets need a sync.'
                    )}
                  />
                )}
              </>
            )}
          </div>
        )
      })}
    </div>
  )
}
