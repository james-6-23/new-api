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
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useIsFetching } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { type Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import {
  getVolcAssetStatusOptions,
  getVolcResourceTypeOptions,
} from '../constants'
import { type VolcAssetStatus, type VolcResourceType } from '../types'

const route = getRouteApi('/_authenticated/volc-assets/')

/** 下拉的「全部」选项。Select 不接受空字符串作为 value，故用一个哨兵。 */
const ANY = '__any__'

interface VolcAssetsFilterBarProps<TData> {
  table: Table<TData>
  /** 超管视角多两个筛选项（用户 / 渠道）与关键字搜索。 */
  admin: boolean
  /** 额度条，渲染在工具栏的 stats 槽位 —— 额度不放页签卡片，跟着表格一起看。 */
  stats?: ReactNode
}

interface FilterState {
  startTime?: Date
  endTime?: Date
  assetId: string
  resourceType: string
  status: string
  keyword: string
  userId: string
  channelId: string
}

const EMPTY: FilterState = {
  startTime: undefined,
  endTime: undefined,
  assetId: '',
  resourceType: '',
  status: '',
  keyword: '',
  userId: '',
  channelId: '',
}

/**
 * 素材库筛选栏，样式与任务日志一致（复用 LogsFilterToolbar 这套壳）。
 *
 * 与表格的耦合点只有 URL：改完筛选点查询才写回 search params，表格那边靠
 * useTableUrlState 监听同一份 search 重新取数。不做即时联动 —— 日期范围
 * 每拖一下就发一次请求既慢又吵。
 */
export function VolcAssetsFilterBar<TData>(
  props: VolcAssetsFilterBarProps<TData>
) {
  const { t } = useTranslation()
  const navigate = route.useNavigate()
  const search = route.useSearch()
  const fetching = useIsFetching({ queryKey: ['volc-assets'] })

  const [filters, setFilters] = useState<FilterState>(EMPTY)

  // URL 是唯一真源：前进/后退、外部跳转都得让输入框跟着回到对应状态。
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setFilters({
      startTime: search.startTime ? new Date(search.startTime) : undefined,
      endTime: search.endTime ? new Date(search.endTime) : undefined,
      assetId: search.assetId ?? '',
      resourceType: search.resourceType?.[0] ?? '',
      status: search.status?.[0] ?? '',
      keyword: search.filter ?? '',
      userId: search.userId ? String(search.userId) : '',
      channelId: search.channelId ? String(search.channelId) : '',
    })
  }, [
    search.startTime,
    search.endTime,
    search.assetId,
    search.resourceType,
    search.status,
    search.filter,
    search.userId,
    search.channelId,
  ])

  const handleChange = useCallback(
    <K extends keyof FilterState>(field: K, value: FilterState[K]) => {
      setFilters((prev) => ({ ...prev, [field]: value }))
    },
    []
  )

  const handleApply = useCallback(() => {
    const parseId = (raw: string) => {
      const value = Number(raw)
      return raw && Number.isFinite(value) && value > 0 ? value : undefined
    }
    navigate({
      search: () => ({
        tab: search.tab,
        page: 1,
        pageSize: search.pageSize,
        startTime: filters.startTime?.getTime(),
        endTime: filters.endTime?.getTime(),
        assetId: filters.assetId || undefined,
        resourceType: filters.resourceType
          ? [filters.resourceType as VolcResourceType]
          : undefined,
        status: filters.status
          ? [filters.status as VolcAssetStatus]
          : undefined,
        filter: props.admin ? filters.keyword || undefined : undefined,
        userId: props.admin ? parseId(filters.userId) : undefined,
        channelId: props.admin ? parseId(filters.channelId) : undefined,
      }),
      replace: true,
    })
  }, [filters, navigate, props.admin, search.tab, search.pageSize])

  const handleReset = useCallback(() => {
    setFilters(EMPTY)
    navigate({ search: () => ({ tab: search.tab, page: 1 }), replace: true })
  }, [navigate, search.tab])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleApply()
    },
    [handleApply]
  )

  const renderSelect = (
    value: string,
    onValueChange: (next: string) => void,
    placeholder: string,
    options: { label: string; value: string }[]
  ) => (
    <Select
      value={value || ANY}
      onValueChange={(next) => onValueChange(!next || next === ANY ? '' : next)}
    >
      <SelectTrigger size='sm' className='h-8 w-full text-sm'>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ANY}>{placeholder}</SelectItem>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )

  const dateRangeFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={filters.startTime}
        end={filters.endTime}
        onChange={({ start, end }) => {
          handleChange('startTime', start)
          handleChange('endTime', end)
        }}
      />
    </LogsFilterField>
  )

  const assetIdFilter = (
    <LogsFilterField>
      <LogsFilterInput
        aria-label={t('Asset ID')}
        placeholder={t('Filter by asset ID')}
        value={filters.assetId}
        onChange={(e) => handleChange('assetId', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )

  const resourceTypeFilter = (
    <LogsFilterField>
      {renderSelect(
        filters.resourceType,
        (next) => handleChange('resourceType', next),
        t('Resource Type'),
        getVolcResourceTypeOptions(t)
      )}
    </LogsFilterField>
  )

  const statusFilter = (
    <LogsFilterField>
      {renderSelect(
        filters.status,
        (next) => handleChange('status', next),
        t('Status'),
        getVolcAssetStatusOptions(t)
      )}
    </LogsFilterField>
  )

  // 关键字与用户/渠道只对超管有意义：/self 的归属由认证上下文写死，
  // 给客户摆一个改不动的用户筛选框只会让人以为能看别人的账本。
  const adminFilters = props.admin ? (
    <>
      <LogsFilterField>
        <LogsFilterInput
          aria-label={t('Keyword')}
          placeholder={t('Search by asset ID or name...')}
          value={filters.keyword}
          onChange={(e) => handleChange('keyword', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          aria-label={t('User ID')}
          placeholder={t('User ID')}
          value={filters.userId}
          onChange={(e) => handleChange('userId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          aria-label={t('Channel ID')}
          placeholder={t('Channel ID')}
          value={filters.channelId}
          onChange={(e) => handleChange('channelId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
    </>
  ) : null

  const secondaryFilters = (
    <>
      {assetIdFilter}
      {resourceTypeFilter}
      {statusFilter}
      {adminFilters}
    </>
  )

  const activeCount = [
    filters.assetId,
    filters.resourceType,
    filters.status,
    filters.keyword,
    filters.userId,
    filters.channelId,
  ].filter(Boolean).length

  return (
    <LogsFilterToolbar
      table={props.table}
      primaryFilters={
        <>
          {dateRangeFilter}
          {secondaryFilters}
        </>
      }
      mobilePinnedFilters={dateRangeFilter}
      mobileFilters={secondaryFilters}
      mobileFilterCount={activeCount}
      stats={props.stats}
      hasActiveFilters={
        activeCount > 0 || !!filters.startTime || !!filters.endTime
      }
      onSearch={handleApply}
      searchLoading={fetching > 0}
      onReset={handleReset}
    />
  )
}
