/*
Copyright (C) 2025 QuantumNous

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

import { API } from '../../helpers';

function buildQuery(params) {
  const query = new URLSearchParams();
  query.set('p', String(params.p ?? 1));
  query.set('page_size', String(params.pageSize ?? 20));
  if (params.resourceType) query.set('resource_type', params.resourceType);
  if (params.status) query.set('status', params.status);
  if (params.keyword) query.set('keyword', params.keyword);
  if (params.resourceId) query.set('resource_id', params.resourceId);
  if (params.userId) query.set('user_id', String(params.userId));
  if (params.channelId) query.set('channel_id', String(params.channelId));
  // 时间戳单位是秒，与 volc_assets.created_at 以及任务日志的约定一致。
  if (params.startTimestamp)
    query.set('start_timestamp', String(params.startTimestamp));
  if (params.endTimestamp)
    query.set('end_timestamp', String(params.endTimestamp));
  return query.toString();
}

export async function getSelfVolcAssets(params = {}) {
  const res = await API.get(`/api/volc_asset/self?${buildQuery(params)}`);
  return res.data;
}

export async function getAllVolcAssets(params = {}) {
  const res = await API.get(`/api/volc_asset/?${buildQuery(params)}`);
  return res.data;
}

/** 客户视角的额度：只讲「我能建多少、还剩多少」。 */
export async function getSelfVolcAssetQuota() {
  const res = await API.get('/api/volc_asset/self/quota');
  return res.data;
}

export async function getVolcAssetQuota() {
  const res = await API.get('/api/volc_asset/quota');
  return res.data;
}

export async function syncVolcAssets() {
  const res = await API.post('/api/volc_asset/sync');
  return res.data;
}

export async function deleteVolcAsset(id) {
  const res = await API.delete(`/api/volc_asset/${id}`);
  return res.data;
}
