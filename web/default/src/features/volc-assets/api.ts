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
import { api } from '@/lib/api'
import {
  type ApiResponse,
  type GetSelfVolcAssetQuotaResponse,
  type GetVolcAssetQuotaResponse,
  type GetVolcAssetsParams,
  type GetVolcAssetsResponse,
  type SyncVolcAssetsResponse,
} from './types'

function buildQuery(params: GetVolcAssetsParams): string {
  const query = new URLSearchParams()
  query.set('p', String(params.p ?? 1))
  query.set('page_size', String(params.page_size ?? 20))
  if (params.resource_type) query.set('resource_type', params.resource_type)
  if (params.status) query.set('status', params.status)
  if (params.keyword) query.set('keyword', params.keyword)
  if (params.user_id) query.set('user_id', String(params.user_id))
  if (params.channel_id) query.set('channel_id', String(params.channel_id))
  if (params.resource_id) query.set('resource_id', params.resource_id)
  if (params.start_timestamp)
    query.set('start_timestamp', String(params.start_timestamp))
  if (params.end_timestamp)
    query.set('end_timestamp', String(params.end_timestamp))
  return query.toString()
}

export async function getSelfVolcAssets(
  params: GetVolcAssetsParams = {}
): Promise<GetVolcAssetsResponse> {
  const res = await api.get(`/api/volc_asset/self?${buildQuery(params)}`)
  return res.data
}

export async function getAllVolcAssets(
  params: GetVolcAssetsParams = {}
): Promise<GetVolcAssetsResponse> {
  const res = await api.get(`/api/volc_asset/?${buildQuery(params)}`)
  return res.data
}

export async function getSelfVolcAssetQuota(): Promise<GetSelfVolcAssetQuotaResponse> {
  const res = await api.get('/api/volc_asset/self/quota')
  return res.data
}

export async function getVolcAssetQuota(): Promise<GetVolcAssetQuotaResponse> {
  const res = await api.get('/api/volc_asset/quota')
  return res.data
}

export async function syncVolcAssets(
  channelId?: number
): Promise<SyncVolcAssetsResponse> {
  const suffix = channelId && channelId > 0 ? `?channel_id=${channelId}` : ''
  const res = await api.post(`/api/volc_asset/sync${suffix}`)
  return res.data
}

export async function deleteVolcAsset(id: number): Promise<ApiResponse> {
  const res = await api.delete(`/api/volc_asset/${id}`)
  return res.data
}
