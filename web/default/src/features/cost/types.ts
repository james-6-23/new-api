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
export interface CostMoney {
  revenue_usd: number
  revenue_cny: number
  list_usd: number
  cost_cny: number
  profit_cny: number
  profit_rate: number
  refund_usd: number
  prompt_tokens: number
  completion_tokens: number
  request_count: number
  // v2 raw additive metrics
  cache_read_tokens: number
  cache_creation_tokens: number
  total_tokens: number
  error_count: number
  frt_sum_ms: number
  frt_count: number
  // v2 derived rates (see `deriveRates` in controller/cost_stat.go for the
  // zero-denominator rules `mergeBreakdown` in lib.ts must mirror)
  success_rate: number
  cache_rate: number
  avg_ttft_ms: number
  /**
   * Discount actually applied over the range: revenue ÷ list price, weighted
   * by quota. Derived from the logs rather than from the current config, so
   * dedicated ratios, cross-group usage and mid-range ratio changes are
   * reflected automatically. `false` when list price is 0 (free/unpriced
   * models), where the quotient is meaningless.
   */
  effective_discount: number
  effective_discount_known: boolean
  /**
   * Cost ratio actually paid over the range (cost_cny ÷ list_usd, quota
   * weighted). `ratio_mixed` marks a range whose logs span several price
   * versions, i.e. the channel's price changed mid-range and no single
   * configured value describes it.
   */
  effective_ratio?: number
  effective_ratio_known?: boolean
  ratio_mixed?: boolean
  /** Range spans several discounts (group change or an edited ratio). */
  discount_mixed?: boolean
  /** A dedicated (user-group × token-group) ratio was active in the range. */
  discount_special?: boolean
  /**
   * Share of spend whose logs carry pricing info, 0..1. `omitempty` on the
   * backend and only assigned when there is a pricing basis at all, so an
   * absent value means 0, not "full coverage".
   */
  discount_coverage?: number
}

/** Channel `setting.sub_suppliers` entry (dto.ChannelSubSupplier mirror). */
export interface CostChannelSubSupplier {
  name: string
  cost_ratio?: number
}

export interface CostBreakdownRow extends CostMoney {
  username?: string
  model_name?: string
  channel_id?: number
  channel_name?: string
  // Pricing config of the channel this sub-row belongs to; absent when the
  // channel identity is merged away (grouped by model).
  cost_mode?: '' | 'ratio' | 'discount'
  cost_ratio?: number
  cost_discount?: number
  // Every list-price amount in this sub-row found a price version. false marks
  // an unpriced gap, which reads as a zero-cost (100% margin) row otherwise.
  priced?: boolean
}

export interface CostDimensionRow extends CostMoney {
  user_id?: number
  username?: string
  model_name?: string
  channel_id?: number
  channel_name?: string
  cost_ratio?: number
  priced: boolean
  user_count?: number
  breakdown?: CostBreakdownRow[]
  breakdown_truncated?: number
  // Channel-dim only (costDimChannel in controller/cost_stat.go).
  cost_mode?: '' | 'ratio' | 'discount'
  cost_discount?: number
  is_aggregator?: boolean
  sub_suppliers?: CostChannelSubSupplier[]
}

export interface CostTrendPoint {
  date: string
  revenue_cny: number
  cost_cny: number
  profit_cny: number
}

export interface CostStackPoint {
  date: string
  channel_id: number
  channel_name: string
  cost_cny: number
}

/** Time-bucket size of the overview trend/stack series. */
export type CostGranularity = 'hour' | 'day'

/** A channel with no cost pricing configured (its cost counts as 0). */
export interface CostUnpricedChannel {
  channel_id: number
  channel_name?: string
}

export interface CostOverview {
  totals: CostMoney
  unpriced_channel_count: number
  unpriced_channels?: CostUnpricedChannel[]
  exchange_rate: number
  /** Bucket size the backend chose (auto: hour for ranges <= 2 days). */
  granularity: CostGranularity
  trend: CostTrendPoint[]
  cost_stack: CostStackPoint[]
}

export interface CostPage {
  items: CostDimensionRow[]
  total: number
  page: number
  page_size: number
  summary: CostMoney
}

export type CostDimension = 'users' | 'models' | 'channels'

export interface CostQueryParams {
  start_timestamp: number
  end_timestamp: number
  p?: number
  page_size?: number
  username?: string
  channel?: number
  model_name?: string
  exchange_rate?: number
}

/**
 * One immutable price version of a channel (model.ChannelCostVersion mirror).
 * A version covers [effective_from, next version's effective_from); 0 is the
 * seeded "since forever" row, which the API refuses to create or delete.
 * `exchange_rate` is frozen at write time so historical cost never drifts.
 */
export interface ChannelCostVersion {
  id: number
  channel_id: number
  effective_from: number
  cost_mode: '' | 'ratio' | 'discount'
  cost_ratio: number
  cost_discount: number
  exchange_rate: number
  note?: string
  created_at: number
  created_by: number
}

/** POST body for a new version; the server fills the rest. */
export type ChannelCostVersionInput = Omit<
  ChannelCostVersion,
  'id' | 'channel_id' | 'created_at' | 'created_by'
>
