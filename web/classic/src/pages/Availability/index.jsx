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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Card,
  Collapse,
  Empty,
  Skeleton,
  Tabs,
  TabPane,
  Typography,
} from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { initVChartSemiTheme } from '@visactor/vchart-semi-theme';
import { CARD_PROPS, CHART_CONFIG } from '../../constants/dashboard.constants';
import { showError } from '../../helpers';
import { getAvailability, getAvailabilityRpm } from './api';
import {
  currentValue,
  deriveOverallStatus,
  formatMetric,
  lineColor,
  METRIC_COLORS,
  METRIC_KEYS,
  STATUS_COLORS,
} from './lib';
import styles from './styles.module.css';

const { Text } = Typography;

/** sparkline 保留的采样点数。 */
const MAX_RPM_POINTS = 30;
/** RPM 轮询间隔，与 default 端一致。 */
const RPM_POLL_INTERVAL_MS = 10000;
/** 主数据自动刷新间隔，与后端 60s 缓存配合。 */
const REFRESH_INTERVAL_MS = 300000;

function OverallBanner({ status, loading, t }) {
  if (loading) {
    return <Skeleton.Title style={{ width: '100%', height: 74 }} />;
  }

  const color = STATUS_COLORS[status];
  const labels = {
    operational: t('全部服务正常'),
    degraded: t('部分性能降级'),
    incident: t('服务故障'),
  };
  const descriptions = {
    operational: t('最近若干小时内所有分组响应正常'),
    degraded: t('部分分组出现了偏高的失败率'),
    incident: t('一个或多个分组正在大量失败'),
  };

  return (
    <div
      className={styles.banner}
      style={{ backgroundColor: `${color}1a` }}
      data-testid='status-overall-banner'
    >
      <span className={styles.dot}>
        <span className={styles.dotPing} style={{ backgroundColor: color }} />
        <span className={styles.dotCore} style={{ backgroundColor: color }} />
      </span>
      <div>
        <div className={styles.bannerTitle} style={{ color }}>
          {labels[status]}
        </div>
        <div className={styles.bannerDesc}>{descriptions[status]}</div>
      </div>
    </div>
  );
}

function LiveRpmCard({ t }) {
  const [history, setHistory] = useState([]);

  useEffect(() => {
    let cancelled = false;

    const sample = async () => {
      try {
        const rpm = await getAvailabilityRpm();
        if (cancelled) return;
        setHistory((prev) => [...prev, rpm].slice(-MAX_RPM_POINTS));
      } catch {
        // 单次采样失败只是曲线缺一个点，保持轮询继续
      }
    };

    sample();
    const timer = setInterval(sample, RPM_POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  const latest = history.length > 0 ? history[history.length - 1] : null;

  const spec = useMemo(
    () => ({
      type: 'area',
      // 用序号而非时钟做 x 轴：轮询间隔固定，序号足以表达先后
      data: [
        {
          id: 'rpm',
          values: history.map((value, index) => ({
            tick: String(index),
            value,
          })),
        },
      ],
      xField: 'tick',
      yField: 'value',
      area: { style: { fillOpacity: 0.18, fill: METRIC_COLORS.tps.best } },
      line: { style: { lineWidth: 2, stroke: METRIC_COLORS.tps.best } },
      point: { visible: false },
      axes: [
        { orient: 'bottom', visible: false },
        { orient: 'left', visible: false },
      ],
      tooltip: { visible: false },
    }),
    [history],
  );

  return (
    <Card
      {...CARD_PROPS}
      className='!rounded-2xl'
      bodyStyle={{ padding: 16 }}
      data-testid='status-live-rpm'
    >
      <div className='flex items-baseline justify-between gap-2'>
        <span className={styles.rpmLabel}>{t('实时请求数 / 分钟')}</span>
        <span className={styles.rpmValue}>
          {latest === null ? '—' : latest}
        </span>
      </div>
      <div className='h-[48px]'>
        {history.length > 1 && <VChart spec={spec} option={CHART_CONFIG} />}
      </div>
      <div className={styles.rpmHint}>{t('本节点最近 60 秒滚动计数')}</div>
    </Card>
  );
}

function MetricChart({ metricKey, metric, hours, title, t }) {
  const bestLabel = t('最优');

  const spec = useMemo(() => {
    const values = [];

    // null 点直接跳过而不是填 0：VChart 会在缺口处断线，
    // 这正是「该小时无流量」应有的表现。
    (metric.best || []).forEach((v, i) => {
      if (v === null || v === undefined) return;
      values.push({ hour: hours[i], series: bestLabel, value: v });
    });
    (metric.lines || []).forEach((line) => {
      (line.points || []).forEach((v, i) => {
        if (v === null || v === undefined) return;
        values.push({ hour: hours[i], series: line.name, value: v });
      });
    });

    const specified = { [bestLabel]: METRIC_COLORS[metricKey].best };
    (metric.lines || []).forEach((line, i) => {
      specified[line.name] = lineColor(metricKey, i);
    });

    return {
      type: 'line',
      data: [{ id: `availability-${metricKey}`, values }],
      xField: 'hour',
      yField: 'value',
      seriesField: 'series',
      legends: { visible: true, position: 'middle', orient: 'bottom' },
      color: { specified },
      line: {
        style: {
          // best 是包络线，用虚线区别于各条真实子线
          lineDash: (datum) => (datum?.series === bestLabel ? [6, 4] : [0]),
          lineWidth: (datum) => (datum?.series === bestLabel ? 2.5 : 1.5),
          opacity: (datum) => (datum?.series === bestLabel ? 1 : 0.7),
        },
      },
      point: { visible: false },
      axes: [
        { orient: 'bottom', label: { autoHide: true } },
        { orient: 'left', label: { visible: true } },
      ],
      tooltip: {
        mark: {
          content: [
            {
              key: (datum) => datum?.series,
              value: (datum) => formatMetric(metricKey, Number(datum?.value)),
            },
          ],
        },
      },
    };
  }, [metric, hours, metricKey, bestLabel]);

  return (
    <div>
      <div className={styles.chartTitle}>{title}</div>
      <div className='h-[180px]'>
        <VChart spec={spec} option={CHART_CONFIG} />
      </div>
    </div>
  );
}

function EntityHeader({ entity, metricLabels, t }) {
  // 单实体状态复用同一套阈值，传入只含自己的数组
  const status = deriveOverallStatus([entity]);

  return (
    <div className={styles.entityHeader}>
      <span
        className={styles.rowDot}
        style={{ backgroundColor: STATUS_COLORS[status] }}
      />
      <div className='min-w-0'>
        <div className={styles.entityName}>{entity.name}</div>
        <div className={styles.entityMeta}>
          {t('24 小时内 {{count}} 次请求', { count: entity.requests })}
        </div>
      </div>
      <div className={styles.metricStrip}>
        {METRIC_KEYS.map((key) => (
          <div key={key} className={styles.metricCell}>
            <span className={styles.metricLabel}>{metricLabels[key]}</span>
            <span className={styles.metricValue}>
              {formatMetric(key, currentValue(entity.current, key))}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

const Availability = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();

  const rawDimension = searchParams.get('dimension');
  const dimension = rawDimension === 'model' ? 'model' : 'group';

  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);

  // 与看板保持一致的 VChart 主题初始化
  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);

  const metricLabels = useMemo(
    () => ({
      successRate: t('成功率'),
      ttft: t('首字延迟'),
      tps: t('每秒输出'),
      latency: t('总延迟'),
    }),
    [t],
  );

  const load = useCallback(
    async (dim, { silent } = {}) => {
      if (!silent) setLoading(true);
      try {
        const payload = await getAvailability(dim);
        setData(payload);
      } catch (error) {
        showError(t('加载可用性数据失败'));
      } finally {
        if (!silent) setLoading(false);
      }
    },
    [t],
  );

  useEffect(() => {
    load(dimension);
    // 静默刷新：不闪骨架屏，避免看图时页面跳动
    const timer = setInterval(
      () => load(dimension, { silent: true }),
      REFRESH_INTERVAL_MS,
    );
    return () => clearInterval(timer);
  }, [dimension, load]);

  const handleDimensionChange = (next) => {
    // 写回 URL：刷新后维度保持不变
    const params = new URLSearchParams(searchParams);
    if (next === 'group') {
      params.delete('dimension');
    } else {
      params.set('dimension', next);
    }
    setSearchParams(params, { replace: true });
  };

  const entities = data?.entities || [];
  const status = deriveOverallStatus(entities);

  return (
    <div className='mt-[60px] px-2'>
      <div className='grid grid-cols-1 lg:grid-cols-[2fr_1fr] gap-4 mb-4'>
        <OverallBanner status={status} loading={loading} t={t} />
        <LiveRpmCard t={t} />
      </div>

      <div className='flex items-center justify-between gap-3 mb-2'>
        <Tabs
          type='button'
          activeKey={dimension}
          onChange={handleDimensionChange}
        >
          <TabPane tab={t('分组')} itemKey='group' />
          <TabPane tab={t('模型')} itemKey='model' />
        </Tabs>
        {data?.truncated && (
          <Text type='tertiary'>{t('仅展示最繁忙的条目')}</Text>
        )}
      </div>

      {loading ? (
        <div className='flex flex-col gap-2'>
          {[0, 1, 2, 3].map((i) => (
            <Skeleton.Title key={i} style={{ width: '100%', height: 62 }} />
          ))}
        </div>
      ) : entities.length === 0 ? (
        <Card {...CARD_PROPS} className='!rounded-2xl'>
          <Empty
            title={
              data?.metricsDisabled
                ? t('性能指标采集已关闭')
                : t('暂无可用性数据')
            }
            description={
              data?.metricsDisabled
                ? t('请在系统设置中开启性能指标采集以开始记录可用性数据')
                : t('中转过请求之后这里就会出现可用性数据')
            }
          />
        </Card>
      ) : (
        <Card
          {...CARD_PROPS}
          className='!rounded-2xl'
          bodyStyle={{ padding: 8 }}
        >
          <Collapse
            accordion={false}
            lazyRender
            defaultActiveKey={entities.length > 0 ? [entities[0].id] : []}
          >
            {entities.map((entity) => (
              <Collapse.Panel
                key={entity.id}
                itemKey={entity.id}
                header={
                  <EntityHeader
                    entity={entity}
                    metricLabels={metricLabels}
                    t={t}
                  />
                }
              >
                <div className='grid grid-cols-1 lg:grid-cols-2 gap-6'>
                  {METRIC_KEYS.map((key) => (
                    <MetricChart
                      key={key}
                      metricKey={key}
                      metric={entity.metrics?.[key] || { best: [], lines: [] }}
                      hours={entity.hours || []}
                      title={metricLabels[key]}
                      t={t}
                    />
                  ))}
                </div>
              </Collapse.Panel>
            ))}
          </Collapse>
        </Card>
      )}
    </div>
  );
};

export default Availability;
