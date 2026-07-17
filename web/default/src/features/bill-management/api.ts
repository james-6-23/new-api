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

export interface BillExportParams {
  start_timestamp?: number
  end_timestamp?: number
  username?: string
  channel?: number
  token_name?: string
  model_name?: string
  group?: string
  with_detail?: 0 | 1
  detail_split_model?: 0 | 1
  exchange_rate?: number
}

export async function exportBillSummary(
  params: BillExportParams,
  isAdmin: boolean
): Promise<{ truncated: boolean }> {
  const path = isAdmin ? '/api/log/bill/export' : '/api/log/self/bill/export'
  const search = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== '' && v !== null) {
      search.append(k, String(v))
    }
  })
  const res = await api.get(`${path}?${search.toString()}`, {
    responseType: 'blob',
  })
  const truncated = res.headers['x-export-truncated'] === '1'

  const blob = new Blob([res.data], {
    type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `bill-summary-${Date.now()}.xlsx`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)

  return { truncated }
}

export interface BillSummaryItem {
  date: string
  username: string
  channel_id: number
  token_name: string
  model_name: string
  amount_usd: number
  exchange_rate: number
  amount_cny: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
}

export interface BillSummaryTotals {
  total_amount_usd: number
  total_amount_cny: number
  total_prompt_tokens: number
  total_completion_tokens: number
  total_cache_read_tokens: number
  total_cache_creation_tokens: number
}

export interface BillSummaryResponse {
  items: BillSummaryItem[]
  total: number
  page: number
  page_size: number
  summary: BillSummaryTotals
}

export async function getBillSummary(
  params: Omit<BillExportParams, 'with_detail' | 'detail_split_model'>,
  isAdmin: boolean,
  page: number,
  pageSize: number
): Promise<BillSummaryResponse> {
  const path = isAdmin ? '/api/log/bill/summary' : '/api/log/self/bill/summary'
  const search = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== '' && v !== null) search.append(k, String(v))
  })
  search.append('p', String(page))
  search.append('page_size', String(pageSize))
  const res = await api.get(`${path}?${search.toString()}`)
  return res.data.data as BillSummaryResponse
}
