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

/*
 * 阈值、配色与格式化与 web/default 端的 features/availability/lib.ts 保持逐项一致，
 * 两套主题读同一个接口，展示口径不允许出现分歧。
 */

/** 成功率阈值：≥99 正常，≥95 降级，其余故障。 */
const OPERATIONAL_THRESHOLD = 99;
const DEGRADED_THRESHOLD = 95;

/**
 * 总体状态取所有实体中最差的成功率。
 * 无任何数据时按 operational 处理——空系统不是故障系统。
 */
export function deriveOverallStatus(entities) {
  const rates = (entities || [])
    .map((e) => e?.current?.successRatePct)
    .filter((v) => v !== null && v !== undefined);

  if (rates.length === 0) return 'operational';

  const worst = Math.min(...rates);
  if (worst >= OPERATIONAL_THRESHOLD) return 'operational';
  if (worst >= DEGRADED_THRESHOLD) return 'degraded';
  return 'incident';
}

/** 状态色，与 default 端 Tailwind emerald/amber/rose 取同一组色值。 */
export const STATUS_COLORS = {
  operational: '#10b981',
  degraded: '#f59e0b',
  incident: '#f43f5e',
};

/** 指标配色族，取自参考实现的低饱和蓝绿系。 */
export const METRIC_COLORS = {
  successRate: { best: '#3f9d6b', lines: ['#9ad3b4', '#65b98c', '#2f7d54'] },
  ttft: { best: '#4f86c6', lines: ['#9ab8dd', '#6f9fd0', '#3f6fa8'] },
  tps: { best: '#4a93b5', lines: ['#97c2d4', '#6aaac6', '#3a7892'] },
  latency: { best: '#3f9b9b', lines: ['#8fc6c6', '#5fb2b2', '#327e7e'] },
};

/** 指标展示顺序，与后端 availabilityMetricKeys 一致。 */
export const METRIC_KEYS = ['successRate', 'ttft', 'tps', 'latency'];

/** 指标数值格式化。null 统一显示为破折号，避免读成 0。 */
export function formatMetric(metric, value) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  switch (metric) {
    case 'successRate':
      return `${Number(value).toFixed(2)}%`;
    case 'ttft':
    case 'latency':
      return `${Math.round(value)} ms`;
    case 'tps':
      return Number(value).toFixed(1);
    default:
      return String(value);
  }
}

/** 为子线按索引取色，超出调色板长度后循环。 */
export function lineColor(metric, index) {
  const palette = METRIC_COLORS[metric].lines;
  return palette[index % palette.length];
}

/** 从 entity.current 里按指标键取值。 */
export function currentValue(current, metric) {
  if (!current) return null;
  switch (metric) {
    case 'successRate':
      return current.successRatePct;
    case 'ttft':
      return current.ttftMs;
    case 'tps':
      return current.tps;
    case 'latency':
      return current.latencyMs;
    default:
      return null;
  }
}
