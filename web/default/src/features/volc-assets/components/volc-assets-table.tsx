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
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { DataTablePage, useDataTable } from '@/components/data-table'
import { getAllVolcAssets, getSelfVolcAssets } from '../api'
import { ERROR_MESSAGES } from '../constants'
import {
  VolcAssetChannelQuotaStats,
  VolcAssetSelfQuotaStats,
} from './volc-asset-quota-stats'
import { useVolcAssetsColumns } from './volc-assets-columns'
import { VolcAssetsFilterBar } from './volc-assets-filter-bar'
import { useVolcAssets } from './volc-assets-provider'

const route = getRouteApi('/_authenticated/volc-assets/')

interface VolcAssetsTableProps {
  /** true 走超管的全站账本接口,false 走 /self —— 列与接口必须同时切,不能只换其一。 */
  admin: boolean
}

export function VolcAssetsTable({ admin }: VolcAssetsTableProps) {
  const { t } = useTranslation()
  const { refreshTrigger } = useVolcAssets()
  const columns = useVolcAssetsColumns(admin)

  const search = route.useSearch()

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search,
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'resource_type', searchKey: 'resourceType', type: 'array' },
    ],
  })

  const statusFilter =
    (columnFilters.find((f) => f.id === 'status')?.value as
      | string[]
      | undefined) || []
  const resourceTypeFilter =
    (columnFilters.find((f) => f.id === 'resource_type')?.value as
      | string[]
      | undefined) || []

  // 后端的时间戳是秒（与 volc_assets.created_at 同单位），URL 里存的是毫秒。
  const toSeconds = (ms?: number) => (ms ? Math.floor(ms / 1000) : undefined)

  // eslint-disable-next-line @tanstack/query/exhaustive-deps
  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'volc-assets',
      admin ? 'all' : 'self',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilter.join(','),
      resourceTypeFilter.join(','),
      search.assetId,
      search.startTime,
      search.endTime,
      search.userId,
      search.channelId,
      refreshTrigger,
    ],
    queryFn: async () => {
      const params = {
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        resource_type: resourceTypeFilter[0],
        status: statusFilter[0],
        resource_id: search.assetId || undefined,
        start_timestamp: toSeconds(search.startTime),
        end_timestamp: toSeconds(search.endTime),
        keyword: admin ? globalFilter || undefined : undefined,
        user_id: admin ? search.userId : undefined,
        channel_id: admin ? search.channelId : undefined,
      }
      const result = admin
        ? await getAllVolcAssets(params)
        : await getSelfVolcAssets(params)

      if (!result.success) {
        toast.error(result.message || t(ERROR_MESSAGES.LOAD_FAILED))
        return { items: [], total: 0 }
      }
      return {
        items: result.data?.items || [],
        total: result.data?.total || 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const assets = data?.items || []

  const { table } = useDataTable({
    data: assets,
    columns,
    columnFilters,
    globalFilter,
    pagination,
    globalFilterFn: () => true,
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Assets Found')}
      emptyDescription={t(
        'Assets created through the Volcengine passthrough channel will show up here.'
      )}
      skeletonKeyPrefix='volc-assets-skeleton'
      applyHeaderSize
      toolbar={
        <VolcAssetsFilterBar
          table={table}
          admin={admin}
          stats={
            admin ? <VolcAssetChannelQuotaStats /> : <VolcAssetSelfQuotaStats />
          }
        />
      }
    />
  )
}
