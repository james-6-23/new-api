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
/** 可用性监控的两个聚合维度。 */
export type AvailabilityDimension = 'group' | 'model'

/** 指标键，与后端 availabilityMetricKeys 一致。 */
export type AvailabilityMetricKey = 'successRate' | 'ttft' | 'tps' | 'latency'

/** 总体状态，由最差实体的成功率推导。 */
export type OverallStatus = 'operational' | 'degraded' | 'incident'

/**
 * 数值一律 `number | null`：null 表示该桶无数据，图表应断线而非归零。
 */
export interface AvailabilityCurrent {
  successRatePct: number | null
  ttftMs: number | null
  tps: number | null
  latencyMs: number | null
}

export interface AvailabilityLine {
  id: string
  name: string
  points: (number | null)[]
}

export interface AvailabilityMetric {
  best: (number | null)[]
  lines: AvailabilityLine[]
}

export interface AvailabilityEntity {
  id: string
  name: string
  requests: number
  hours: string[]
  current: AvailabilityCurrent
  metrics: Record<AvailabilityMetricKey, AvailabilityMetric>
}

export interface AvailabilityResponse {
  generatedAt: number
  dimension: AvailabilityDimension
  truncated: boolean
  /** 后端性能采集开关关闭时为 true，此时 entities 必为空。 */
  metricsDisabled: boolean
  entities: AvailabilityEntity[]
}
