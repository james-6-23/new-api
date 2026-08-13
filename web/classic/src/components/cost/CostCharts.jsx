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

import React, { useEffect, useMemo } from 'react';
import { Card, Empty } from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { initVChartSemiTheme } from '@visactor/vchart-semi-theme';
import { CHART_CONFIG, CARD_PROPS } from '../../constants/dashboard.constants';
import { getCostChartCurrency, deriveUsdFromCny } from './costFormat';

const MAX_STACK_SERIES = 8;

// 轴刻度上限：小时粒度最多约 49 个桶，全画出来标签会糊成一片。
const MAX_AXIS_TICKS = 12;

/**
 * 桶标签 → 轴标签。后端桶格式：日粒度 "2026-06-01"、小时粒度 "2026-06-01 15"。
 */
function formatBucketLabel(bucket, granularity) {
  const s = String(bucket || '');
  if (granularity === 'hour') {
    const hour = s.slice(11, 13);
    return hour ? `${hour}:00` : s;
  }
  return s.slice(5) || s;
}

/** 悬浮提示里的完整时间，日期不能省略否则跨天时有歧义。 */
function formatBucketTooltip(bucket, granularity) {
  const s = String(bucket || '');
  if (granularity === 'hour') {
    const hour = s.slice(11, 13);
    return hour ? `${s.slice(0, 10)} ${hour}:00` : s;
  }
  return s;
}

/** 抽样出要显示刻度的桶：点全保留，只是标签隔几个画一次。 */
function sampleBucketTicks(buckets, maxTicks = MAX_AXIS_TICKS) {
  if (buckets.length <= maxTicks) return new Set(buckets);
  const step = Math.ceil(buckets.length / maxTicks);
  const kept = new Set();
  for (let i = 0; i < buckets.length; i += step) kept.add(buckets[i]);
  kept.add(buckets[buckets.length - 1]);
  return kept;
}

// 固定配色，与 web/default 端保持一致的语义：收入蓝、成本橙、利润绿
const TREND_COLORS = {
  revenue: '#3b82f6',
  cost: '#f59e0b',
  profit: '#10b981',
};

const STACK_COLORS = [
  '#3b82f6',
  '#10b981',
  '#f59e0b',
  '#ef4444',
  '#8b5cf6',
  '#06b6d4',
  '#ec4899',
  '#6366f1',
  '#94a3b8', // 其他
];

// 将 trend 数据（宽表：每行含 revenue_cny/cost_cny/profit_cny）转换为 VChart 需要的长表。
// 金额统一先按查询汇率折回美元，再由 currency.format() 换算成展示货币——后端的
// *_cny 已经乘过一次查询汇率，直接用会叠加两个汇率。
function buildTrendData(trend, seriesNames, queryRate) {
  const rows = [];
  const toUsd = (cny) => deriveUsdFromCny(cny, queryRate);
  (trend || []).forEach((point) => {
    rows.push({
      date: point.date,
      series: seriesNames.revenue,
      value: toUsd(point.revenue_cny),
    });
    rows.push({
      date: point.date,
      series: seriesNames.cost,
      value: toUsd(point.cost_cny),
    });
    rows.push({
      date: point.date,
      series: seriesNames.profit,
      value: toUsd(point.profit_cny),
    });
  });
  return rows;
}

// 按渠道折叠成本堆叠数据：超过 MAX_STACK_SERIES 个渠道时，按总成本排序保留前 N 个，其余归入"其他"
function foldChannelStack(points, otherLabel, maxSeries = MAX_STACK_SERIES) {
  if (!points || points.length === 0) return { data: [], domain: [] };

  const totals = new Map();
  const firstSeenOrder = [];
  points.forEach((point) => {
    const name = point.channel_name;
    if (!totals.has(name)) firstSeenOrder.push(name);
    totals.set(name, (totals.get(name) || 0) + (Number(point.cost_cny) || 0));
  });

  const shouldFold = firstSeenOrder.length > maxSeries;
  let keep;
  if (shouldFold) {
    const ranked = Array.from(totals.entries()).sort((a, b) => b[1] - a[1]);
    keep = new Set(ranked.slice(0, maxSeries).map(([name]) => name));
  } else {
    keep = new Set(firstSeenOrder);
  }

  const domain = firstSeenOrder.filter((name) => keep.has(name));
  if (shouldFold) domain.push(otherLabel);

  const byDate = new Map();
  points.forEach((point) => {
    const key = keep.has(point.channel_name) ? point.channel_name : otherLabel;
    if (!byDate.has(point.date)) byDate.set(point.date, new Map());
    const bucket = byDate.get(point.date);
    bucket.set(key, (bucket.get(key) || 0) + (Number(point.cost_cny) || 0));
  });

  const dates = Array.from(byDate.keys()).sort();
  const data = [];
  dates.forEach((date) => {
    const bucket = byDate.get(date);
    domain.forEach((channel) => {
      data.push({
        date,
        channel,
        cost_cny: Number((bucket.get(channel) || 0).toFixed(2)),
      });
    });
  });

  return { data, domain };
}

const CostCharts = ({ trend, costStack, granularity = 'day', exchangeRate, t }) => {
  // 与看板保持一致的 VChart 主题初始化
  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);

  // 展示货币跟随「系统设置 → 运营设置 → 额度展示类型」；查询汇率用于把后端的
  // *_cny 折回美元，两者不能混用（见 getCostChartCurrency 注释）。
  const currency = useMemo(() => getCostChartCurrency(), []);
  const queryRate = Number(exchangeRate) || 0;

  const seriesNames = useMemo(
    () => ({
      revenue: t('收入'),
      cost: t('成本'),
      profit: t('利润'),
    }),
    [t],
  );

  const trendData = useMemo(
    () => buildTrendData(trend, seriesNames, queryRate),
    [trend, seriesNames, queryRate],
  );

  const { data: stackData, domain: stackDomain } = useMemo(
    () => foldChannelStack(costStack, t('其他')),
    [costStack, t],
  );

  // 堆叠柱同样先折回美元，保证与折线图口径一致。
  const stackDisplayData = useMemo(
    () =>
      stackData.map((d) => ({
        ...d,
        cost_display: deriveUsdFromCny(d.cost_cny, queryRate),
      })),
    [stackData, queryRate],
  );

  const trendTicks = useMemo(
    () => sampleBucketTicks((trend || []).map((p) => p.date)),
    [trend],
  );

  const stackTicks = useMemo(
    () => sampleBucketTicks(Array.from(new Set(stackData.map((d) => d.date))).sort()),
    [stackData],
  );

  const trendSpec = useMemo(
    () => ({
      type: 'line',
      data: [{ id: 'trend', values: trendData }],
      xField: 'date',
      yField: 'value',
      seriesField: 'series',
      legends: { visible: true },
      title: {
        visible: true,
        text: t('收支趋势') + `（${currency.symbol}）`,
      },
      axes: [
        {
          orient: 'left',
          label: { formatMethod: (v) => currency.format(Number(v) || 0, 0) },
        },
        {
          orient: 'bottom',
          label: {
            formatMethod: (v) =>
              trendTicks.has(String(v)) ? formatBucketLabel(v, granularity) : '',
          },
        },
      ],
      line: { style: { lineWidth: 2 } },
      point: { visible: false },
      tooltip: {
        mark: {
          title: {
            value: (datum) => formatBucketTooltip(datum['date'], granularity),
          },
          content: [
            {
              key: (datum) => datum['series'],
              value: (datum) => currency.format(Number(datum['value']) || 0),
            },
          ],
        },
        // 维度提示默认开启，不给 content 就会退回原始测量值（满精度浮点）。
        dimension: {
          title: {
            value: (datum) => formatBucketTooltip(datum['date'], granularity),
          },
          content: [
            {
              key: (datum) => datum['series'],
              value: (datum) => currency.format(Number(datum['value']) || 0),
            },
          ],
        },
      },
      color: {
        specified: {
          [seriesNames.revenue]: TREND_COLORS.revenue,
          [seriesNames.cost]: TREND_COLORS.cost,
          [seriesNames.profit]: TREND_COLORS.profit,
        },
      },
    }),
    [trendData, seriesNames, t, currency, granularity, trendTicks],
  );

  const stackSpec = useMemo(
    () => ({
      type: 'bar',
      data: [{ id: 'costStack', values: stackDisplayData }],
      xField: 'date',
      yField: 'cost_display',
      seriesField: 'channel',
      stack: true,
      legends: { visible: true },
      title: {
        visible: true,
        text: t('渠道成本堆叠') + `（${currency.symbol}）`,
      },
      axes: [
        {
          orient: 'left',
          label: { formatMethod: (v) => currency.format(Number(v) || 0, 0) },
        },
        {
          orient: 'bottom',
          label: {
            formatMethod: (v) =>
              stackTicks.has(String(v)) ? formatBucketLabel(v, granularity) : '',
          },
        },
      ],
      bar: {
        state: { hover: { stroke: '#000', lineWidth: 1 } },
      },
      tooltip: {
        mark: {
          title: {
            value: (datum) => formatBucketTooltip(datum['date'], granularity),
          },
          content: [
            {
              key: (datum) => datum['channel'],
              value: (datum) => currency.format(Number(datum['cost_display']) || 0),
            },
          ],
        },
        dimension: {
          title: {
            value: (datum) => formatBucketTooltip(datum['date'], granularity),
          },
          content: [
            {
              key: (datum) => datum['channel'],
              value: (datum) => currency.format(Number(datum['cost_display']) || 0),
            },
          ],
        },
      },
      color: {
        type: 'ordinal',
        domain: stackDomain,
        range: STACK_COLORS,
      },
    }),
    [stackDisplayData, stackDomain, t, currency, granularity, stackTicks],
  );

  return (
    <div className='grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4'>
      <Card {...CARD_PROPS} className='!rounded-2xl' bodyStyle={{ padding: 8 }}>
        <div className='h-72'>
          {trendData.length > 0 ? (
            <VChart spec={trendSpec} option={CHART_CONFIG} />
          ) : (
            <div className='flex h-full items-center justify-center'>
              <Empty title={t('暂无数据')} />
            </div>
          )}
        </div>
      </Card>
      <Card {...CARD_PROPS} className='!rounded-2xl' bodyStyle={{ padding: 8 }}>
        <div className='h-72'>
          {stackDisplayData.length > 0 ? (
            <VChart spec={stackSpec} option={CHART_CONFIG} />
          ) : (
            <div className='flex h-full items-center justify-center'>
              <Empty title={t('暂无数据')} />
            </div>
          )}
        </div>
      </Card>
    </div>
  );
};

export default CostCharts;
