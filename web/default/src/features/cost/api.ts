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
import { api, type ApiRequestConfig } from '@/lib/api'
import type {
  ChannelCostVersion,
  ChannelCostVersionInput,
  CostDimension,
  CostOverview,
  CostPage,
  CostQueryParams,
} from './types'

export async function getCostOverview(
  params: CostQueryParams
): Promise<CostOverview> {
  const res = await api.get('/api/cost/overview', { params })
  return res.data.data
}

export async function getCostByDimension(
  dim: CostDimension,
  params: CostQueryParams
): Promise<CostPage> {
  const res = await api.get(`/api/cost/${dim}`, { params })
  return res.data.data
}

/**
 * The version endpoints reject with the backend's own message so callers can
 * report it themselves, which means the global handlers must both stand down:
 * the interceptor would toast the message and react-query's default onError
 * would stack a generic "Something went wrong!" on top of it (the rejection is
 * a plain Error, so handleServerError has no response body to read).
 * Same idiom as `channelActionConfig` in features/channels/api.ts.
 */
const costVersionActionConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

/** Newest first (effective_from desc), matching the backend ordering. */
export async function getChannelCostVersions(
  channelId: number
): Promise<ChannelCostVersion[]> {
  const res = await api.get(
    `/api/cost/channels/${channelId}/versions`,
    costVersionActionConfig()
  )
  // Rejecting rather than falling back to []: an empty list renders as "this
  // channel has no price history", which is a different fact from "the history
  // could not be loaded".
  if (!res.data?.success) throw new Error(res.data?.message)
  return res.data.data ?? []
}

/**
 * Rejects on a refused write (e.g. a duplicate effective_from) so react-query
 * doesn't run onSuccess for something the server never stored.
 */
export async function createChannelCostVersion(
  channelId: number,
  body: ChannelCostVersionInput
): Promise<ChannelCostVersion> {
  const res = await api.post(
    `/api/cost/channels/${channelId}/versions`,
    body,
    costVersionActionConfig()
  )
  if (!res.data?.success) throw new Error(res.data?.message)
  return res.data.data
}

export async function deleteChannelCostVersion(
  versionId: number
): Promise<void> {
  const res = await api.delete(
    `/api/cost/versions/${versionId}`,
    costVersionActionConfig()
  )
  if (!res.data?.success) throw new Error(res.data?.message)
}
