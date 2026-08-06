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
   * by quota. Unlike `group_ratio` (a current-config snapshot) this reflects
   * dedicated ratios, cross-group usage and mid-range ratio changes
   * automatically. `false` when list price is 0 (free/unpriced models), where
   * the quotient is meaningless.
   */
  effective_discount: number
  effective_discount_known: boolean
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
  effective_ratio?: number
  // Current discount of the user this sub-row belongs to (dedicated ratio
  // takes priority); absent when the user identity is merged away.
  user_group?: string
  group_ratio?: number
  group_ratio_known?: boolean
  group_ratio_special?: boolean
  /** Range spans several using-groups; `group_ratio` is the quota-weighted blend. */
  group_ratio_mixed?: boolean
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
  effective_ratio?: number
  is_aggregator?: boolean
  sub_suppliers?: CostChannelSubSupplier[]
  // User-dim only: the user's *current* group and that group's configured
  // ratio (a query-time config snapshot, not weighted over the logs in range).
  // group_ratio_special marks a dedicated (user-group × using-group) ratio hit.
  user_group?: string
  group_ratio?: number
  group_ratio_known?: boolean
  group_ratio_special?: boolean
  /** Range spans several using-groups; `group_ratio` is the quota-weighted blend. */
  group_ratio_mixed?: boolean
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
