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
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface PageInfo<T> {
  page: number
  page_size: number
  total: number
  items: T[]
}

/** 与 constants.ts 的 VOLC_RESOURCE_TYPE / VOLC_ASSET_STATUS 值域对齐。 */
export type VolcResourceType = 'asset' | 'asset_group'
export type VolcAssetStatus = 'Active' | 'Processing' | 'Failed'

export interface VolcAsset {
  id: number
  user_id: number
  username?: string
  channel_id: number
  resource_type: string
  resource_id: string
  group_id: string
  name: string
  asset_type: string
  status: string
  created_at: number
  updated_at: number
}

export interface VolcAssetQuotaItem {
  channel_id: number
  channel_name: string
  quota_limit: number
  upstream_assets: number
  upstream_groups: number
  local_assets: number
  local_groups: number
  orphan_assets: number
  orphan_groups: number
  unrecorded: number
  error?: string
}

export interface VolcAssetQuotaOverview {
  items: VolcAssetQuotaItem[]
}

export interface VolcAssetSyncResult {
  channel_id: number
  claimed: number
  refreshed: number
  stale: number
  error?: string
}

export interface GetVolcAssetsParams {
  p?: number
  page_size?: number
  resource_type?: string
  status?: string
  keyword?: string
  user_id?: number
  channel_id?: number
  /** 官方资产 id，精确匹配。 */
  resource_id?: string
  /** 创建时间范围，单位秒（与任务日志一致，不是毫秒）。 */
  start_timestamp?: number
  end_timestamp?: number
}

/** 客户视角的额度：只讲“我能建多少、还剩多少”。 */
export interface SelfVolcAssetQuota {
  limit: number
  used: number
  enforced: boolean
  count_asset_groups: boolean
}

export type GetVolcAssetsResponse = ApiResponse<PageInfo<VolcAsset>>
export type GetVolcAssetQuotaResponse = ApiResponse<VolcAssetQuotaOverview>
export type GetSelfVolcAssetQuotaResponse = ApiResponse<SelfVolcAssetQuota>
export type SyncVolcAssetsResponse = ApiResponse<{ items: VolcAssetSyncResult[] }>
