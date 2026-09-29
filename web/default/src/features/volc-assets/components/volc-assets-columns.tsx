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
import { type ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { formatTimestampToDate } from '@/lib/format'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import {
  VOLC_ASSET_ORPHAN_OWNER_ID,
  VOLC_ASSET_STATUSES,
  VOLC_RESOURCE_TYPE,
} from '../constants'
import { type VolcAsset } from '../types'
import { DataTableRowActions } from './data-table-row-actions'
import { VolcAssetIdCell } from './volc-assets-cells'

/**
 * 资产表列。
 *
 * admin 为 true 时多出用户列、渠道列与删除操作 —— 与后端 /api/volc_asset/ 一组
 * AdminAuth 接口一一对应,普通用户拿不到这些字段也调不动这些接口。
 */
export function useVolcAssetsColumns(admin: boolean): ColumnDef<VolcAsset>[] {
  const { t } = useTranslation()

  const columns: ColumnDef<VolcAsset>[] = [
    {
      accessorKey: 'id',
      header: t('ID'),
      meta: { mobileHidden: true },
      cell: ({ row }) => (
        <TableId value={row.getValue('id') as number} className='w-[60px]' />
      ),
      size: 80,
    },
    {
      accessorKey: 'resource_id',
      header: t('Asset ID'),
      meta: { mobileTitle: true },
      cell: ({ row }) => (
        <div className='max-w-[260px] min-w-[180px]'>
          <VolcAssetIdCell value={row.getValue('resource_id') as string} />
        </div>
      ),
      size: 260,
    },
    {
      accessorKey: 'name',
      header: t('Name'),
      cell: ({ row }) => (
        <div className='max-w-[180px] truncate font-medium'>
          {(row.getValue('name') as string) || '-'}
        </div>
      ),
      size: 200,
    },
    {
      accessorKey: 'resource_type',
      header: t('Resource Type'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const value = row.getValue('resource_type') as string
        return (
          <StatusBadge
            label={
              value === VOLC_RESOURCE_TYPE.ASSET_GROUP
                ? t('Asset Group')
                : t('Asset')
            }
            variant={
              value === VOLC_RESOURCE_TYPE.ASSET_GROUP ? 'purple' : 'info'
            }
            copyable={false}
            className='-ml-1.5'
          />
        )
      },
      size: 120,
    },
    {
      accessorKey: 'asset_type',
      header: t('Asset Type'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const value = (row.getValue('asset_type') as string) || ''
        return <div className='text-sm'>{value ? t(value) : '-'}</div>
      },
      size: 100,
    },
    {
      accessorKey: 'group_id',
      header: t('Asset Group'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const value = (row.getValue('group_id') as string) || ''
        return (
          <div className='text-muted-foreground max-w-[180px] truncate font-mono text-sm'>
            {value || '-'}
          </div>
        )
      },
      size: 200,
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      meta: { mobileBadge: true },
      cell: ({ row }) => {
        const value = (row.getValue('status') as string) || ''
        const config = VOLC_ASSET_STATUSES[value]
        if (!config) {
          return (
            <span className='text-muted-foreground text-sm'>
              {value || '-'}
            </span>
          )
        }
        return (
          <StatusBadge
            label={t(config.labelKey)}
            variant={config.variant}
            copyable={false}
            className='-ml-1.5'
          />
        )
      },
      filterFn: (row, id, value: string[]) =>
        value.includes(String(row.getValue(id))),
      size: 120,
    },
    {
      accessorKey: 'created_at',
      header: t('Created Time'),
      cell: ({ row }) => (
        <div className='min-w-[160px] font-mono text-sm'>
          {formatTimestampToDate(row.getValue('created_at') as number)}
        </div>
      ),
      size: 180,
    },
  ]

  if (!admin) {
    return columns
  }

  // 用户列插在资产 ID 之前:超管翻全站账本时先看归属,再看具体是哪条资源。
  columns.splice(1, 0, {
    accessorKey: 'user_id',
    header: t('User'),
    cell: ({ row }) => {
      const asset = row.original
      if (asset.user_id === VOLC_ASSET_ORPHAN_OWNER_ID) {
        return (
          <StatusBadge
            label={t('Orphan')}
            variant='warning'
            copyable={false}
            className='-ml-1.5'
          />
        )
      }
      return (
        <div className='max-w-[140px] truncate text-sm'>
          {asset.username || `#${asset.user_id}`}
        </div>
      )
    },
    size: 140,
  })

  columns.splice(2, 0, {
    accessorKey: 'channel_id',
    header: t('Channel'),
    meta: { mobileHidden: true },
    cell: ({ row }) => <TableId value={row.getValue('channel_id') as number} />,
    size: 90,
  })

  columns.push({
    id: 'actions',
    cell: ({ row }) => <DataTableRowActions row={row} />,
    enableSorting: false,
    enableHiding: false,
    size: 60,
    meta: { pinned: 'right' as const },
  })

  return columns
}
