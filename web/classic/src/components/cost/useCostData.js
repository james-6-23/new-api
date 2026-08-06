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

import { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import dayjs from 'dayjs';
import { API, showError } from '../../helpers';

const DEFAULT_PAGE_SIZE = 20;
// 默认汇率：与后端 operation_setting.USDExchangeRate 未显式传参时的兜底一致的展示默认值
const DEFAULT_EXCHANGE_RATE = 6.8;

// 默认时间范围：今天
const getDefaultDateRange = () => [
  dayjs().startOf('day').toDate(),
  dayjs().endOf('day').toDate(),
];

const getDefaultFilters = () => ({
  dateRange: getDefaultDateRange(),
  username: '',
  channel: undefined,
  modelName: '',
  exchangeRate: DEFAULT_EXCHANGE_RATE,
});

export const useCostData = () => {
  const { t } = useTranslation();

  // ========== 查询条件 ==========
  const [filters, setFilters] = useState(getDefaultFilters);
  const [activeTab, setActiveTabState] = useState('users'); // users | models | channels
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);

  // ========== 数据 ==========
  const [overview, setOverview] = useState(null);
  const [overviewLoading, setOverviewLoading] = useState(false);
  // pageData 带上它属于哪个维度（dim）。三个 tab 共用这一份状态，切 tab 时新请求
  // 还在飞行中，旧维度的 items 会先用新维度的列定义渲染出来（用户列里出现渠道数据）。
  // 消费方按 dim !== activeTab 判定为陈旧数据，渲染空表 + loading，不再串数据。
  const [pageData, setPageData] = useState({
    dim: 'users',
    items: [],
    total: 0,
    page: 1,
    page_size: DEFAULT_PAGE_SIZE,
    summary: null,
  });
  const [tableLoading, setTableLoading] = useState(false);

  const { dateRange, username, channel, modelName, exchangeRate } = filters;
  const startTimestamp = Math.floor((dateRange[0]?.getTime() || 0) / 1000);
  const endTimestamp = Math.floor((dateRange[1]?.getTime() || 0) / 1000);
  const effectiveExchangeRate =
    Number(exchangeRate) > 0 ? Number(exchangeRate) : DEFAULT_EXCHANGE_RATE;
  const effectiveChannel = Number(channel) > 0 ? Number(channel) : 0;

  const commonParams = {
    start_timestamp: startTimestamp,
    end_timestamp: endTimestamp,
    username: username || undefined,
    channel: effectiveChannel || undefined,
    model_name: modelName || undefined,
    exchange_rate: effectiveExchangeRate,
  };

  // ========== 加载函数 ==========
  const loadOverview = useCallback(async () => {
    setOverviewLoading(true);
    try {
      const res = await API.get('/api/cost/overview', {
        params: commonParams,
      });
      const { success, message, data } = res.data;
      if (success) {
        setOverview(data);
      } else {
        showError(message);
      }
    } catch (e) {
      showError(t('请求失败'));
    } finally {
      setOverviewLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [startTimestamp, endTimestamp, username, effectiveChannel, modelName, effectiveExchangeRate]);

  const loadDimension = useCallback(async () => {
    const requestedDim = activeTab;
    setTableLoading(true);
    try {
      const res = await API.get(`/api/cost/${requestedDim}`, {
        params: {
          ...commonParams,
          p: activePage,
          page_size: pageSize,
        },
      });
      const { success, message, data } = res.data;
      if (success) {
        // 标记数据所属维度；两个 tab 的请求乱序返回时，晚到的旧维度响应会被消费方
        // 按 dim 判定为陈旧而忽略。
        setPageData({ ...data, dim: requestedDim });
      } else {
        showError(message);
      }
    } catch (e) {
      showError(t('请求失败'));
    } finally {
      setTableLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    activeTab,
    activePage,
    pageSize,
    startTimestamp,
    endTimestamp,
    username,
    effectiveChannel,
    modelName,
    effectiveExchangeRate,
  ]);

  useEffect(() => {
    loadOverview();
  }, [loadOverview]);

  useEffect(() => {
    loadDimension();
  }, [loadDimension]);

  // ========== 交互函数 ==========
  const setActiveTab = (tab) => {
    setActiveTabState(tab);
    setActivePage(1);
  };

  const handlePageChange = (page) => setActivePage(page);
  const handlePageSizeChange = (size) => {
    setPageSize(size);
    setActivePage(1);
  };

  // 由查询表单提交时调用（查询按钮），提交后统一刷新并回到第 1 页
  const applyFilters = (values) => {
    if (!values) return;
    setFilters((prev) => {
      const next = { ...prev };
      if (
        Array.isArray(values.dateRange) &&
        values.dateRange.length === 2 &&
        values.dateRange[0] &&
        values.dateRange[1]
      ) {
        next.dateRange = values.dateRange;
      }
      if ('username' in values) next.username = values.username || '';
      if ('channel' in values) next.channel = values.channel;
      if ('modelName' in values) next.modelName = values.modelName || '';
      if ('exchangeRate' in values) {
        next.exchangeRate =
          Number(values.exchangeRate) > 0
            ? Number(values.exchangeRate)
            : DEFAULT_EXCHANGE_RATE;
      }
      return next;
    });
    setActivePage(1);
  };

  // 重置按钮：恢复默认筛选条件并重新查询
  const resetFilters = () => {
    setFilters(getDefaultFilters());
    setActivePage(1);
  };

  // 「只看该渠道」：设置渠道筛选并切到供应商维度 tab
  const filterByChannel = (channelId) => {
    setFilters((prev) => ({ ...prev, channel: channelId }));
    setActiveTabState('channels');
    setActivePage(1);
  };

  const refresh = () => {
    loadOverview();
    loadDimension();
  };

  return {
    t,
    filters,
    applyFilters,
    resetFilters,
    filterByChannel,
    defaultFilters: getDefaultFilters(),
    activeTab,
    setActiveTab,
    activePage,
    pageSize,
    handlePageChange,
    handlePageSizeChange,
    overview,
    overviewLoading,
    pageData,
    tableLoading,
    refresh,
    loadOverview,
    loadDimension,
  };
};

export default useCostData;
