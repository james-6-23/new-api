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

import React, { useRef } from 'react';
import { Card, Form, Button, Banner, Typography } from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import CardPro from '../common/ui/CardPro';
import CostCharts from './CostCharts';
import CostTables from './CostTables';
import { useCostData } from './useCostData';
import { createCardProPagination } from '../../helpers/utils';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { DATE_RANGE_PRESETS } from '../../constants/console.constants';
import {
  deriveUsdFromCny,
  formatDualMoney,
  getMoneyPrimaryCurrency,
} from './costFormat';

const { Text } = Typography;

const profitStyle = (v) => ({
  color:
    Number(v || 0) >= 0
      ? 'var(--semi-color-success)'
      : 'var(--semi-color-danger)',
});

const CostAccounting = () => {
  const costData = useCostData();
  const {
    t,
    filters,
    applyFilters,
    resetFilters,
    filterByChannel,
    defaultFilters,
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
  } = costData;
  const isMobile = useIsMobile();
  const filterFormApiRef = useRef(null);

  const totals = overview?.totals || {};
  const unpricedCount = overview?.unpriced_channel_count || 0;
  // 当前查询实际生效的汇率：以响应回显的 exchange_rate 为准（与后端换算口径
  // 一致），未加载时兜底为筛选栏当前值。
  const appliedExchangeRate = overview?.exchange_rate || filters.exchangeRate;
  const primaryCurrency = getMoneyPrimaryCurrency();

  const revenueMoney = formatDualMoney(
    totals.revenue_usd,
    totals.revenue_cny,
    primaryCurrency,
  );
  const costMoney = formatDualMoney(
    deriveUsdFromCny(totals.cost_cny, appliedExchangeRate),
    totals.cost_cny,
    primaryCurrency,
  );
  const profitMoney = formatDualMoney(
    deriveUsdFromCny(totals.profit_cny, appliedExchangeRate),
    totals.profit_cny,
    primaryCurrency,
  );

  const statsArea = (
    <div className='flex flex-col gap-3 mb-2'>
      {unpricedCount > 0 && (
        <Banner
          type='warning'
          description={t('{{count}} 个渠道未填成本倍率，其成本按 0 计', {
            count: unpricedCount,
          })}
          className='!rounded-lg'
          closeIcon={null}
        />
      )}
      <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4'>
        <Card bordered className='!rounded-2xl'>
          <div className='text-xs text-gray-500 mb-1'>{t('收入')}</div>
          <div className='text-lg font-semibold'>{revenueMoney.primary}</div>
          <Text type='tertiary' size='small'>
            {revenueMoney.secondary}
          </Text>
          {Number(totals.refund_usd || 0) > 0 && (
            <div
              className='text-xs mt-1'
              style={{ color: 'var(--semi-color-danger)' }}
            >
              {t('退款')} -${Number(totals.refund_usd || 0).toFixed(2)}
            </div>
          )}
        </Card>
        <Card bordered className='!rounded-2xl'>
          <div className='text-xs text-gray-500 mb-1'>{t('成本')}</div>
          <div className='text-lg font-semibold'>{costMoney.primary}</div>
          <Text type='tertiary' size='small'>
            {costMoney.secondary}
          </Text>
        </Card>
        <Card bordered className='!rounded-2xl'>
          <div className='text-xs text-gray-500 mb-1'>{t('利润')}</div>
          <div
            className='text-lg font-semibold'
            style={profitStyle(totals.profit_cny)}
          >
            {profitMoney.primary}
          </div>
          <Text type='tertiary' size='small'>
            {profitMoney.secondary}
          </Text>
        </Card>
        <Card bordered className='!rounded-2xl'>
          <div className='text-xs text-gray-500 mb-1'>{t('利润率')}</div>
          <div
            className='text-lg font-semibold'
            style={profitStyle(totals.profit_rate)}
          >
            {(Number(totals.profit_rate || 0) * 100).toFixed(2)}%
          </div>
        </Card>
      </div>
    </div>
  );

  const filterArea = (
    <Card bordered className='!rounded-2xl mb-4'>
      <Form
        initValues={filters}
        onSubmit={(values) => applyFilters(values)}
        allowEmpty={true}
        layout='vertical'
        trigger='change'
        stopValidateWithError={false}
        getFormApi={(api) => (filterFormApiRef.current = api)}
      >
        <div className='flex flex-col gap-2'>
          <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-2'>
            <div className='col-span-1 lg:col-span-2'>
              <Form.DatePicker
                field='dateRange'
                className='w-full'
                type='dateTimeRange'
                placeholder={[t('开始时间'), t('结束时间')]}
                showClear
                pure
                size='small'
                presets={DATE_RANGE_PRESETS.map((preset) => ({
                  text: t(preset.text),
                  start: preset.start(),
                  end: preset.end(),
                }))}
              />
            </div>

            <Form.Input
              field='username'
              prefix={<IconSearch />}
              placeholder={t('用户名称')}
              showClear
              pure
              size='small'
            />

            <Form.InputNumber
              field='channel'
              placeholder={t('渠道ID')}
              showClear
              pure
              min={0}
              size='small'
            />

            <Form.Input
              field='modelName'
              prefix={<IconSearch />}
              placeholder={t('模型名称')}
              showClear
              pure
              size='small'
            />

            {/* 这个汇率只折算收入展示：成本按各计价版本自己冻结的结算汇率核算，
                改这里不会动成本。不说清楚，它看起来就像一个成本旋钮。 */}
            <Form.InputNumber
              field='exchangeRate'
              placeholder='6.8'
              aria-label={t('收入折算汇率')}
              extraText={
                <Text type='tertiary' size='small'>
                  {t('仅折算收入展示；成本按各计价版本冻结的结算汇率核算。')}
                </Text>
              }
              showClear
              pure
              min={0}
              step={0.01}
              prefix='$1 ='
              suffix='CNY'
              size='small'
            />
          </div>

          <div className='flex flex-col sm:flex-row justify-end items-start sm:items-center gap-3'>
            <div className='flex gap-2 w-full sm:w-auto justify-end'>
              <Button
                type='tertiary'
                htmlType='submit'
                loading={overviewLoading}
                size='small'
              >
                {t('查询')}
              </Button>
              <Button
                type='tertiary'
                size='small'
                onClick={() => {
                  filterFormApiRef.current?.setValues(defaultFilters);
                  resetFilters();
                }}
              >
                {t('重置')}
              </Button>
            </div>
          </div>
        </div>
      </Form>
    </Card>
  );

  return (
    <>
      {filterArea}
      <CardPro
        type='type2'
        statsArea={statsArea}
        paginationArea={createCardProPagination({
          currentPage: activePage,
          pageSize,
          total: pageData?.total || 0,
          onPageChange: handlePageChange,
          onPageSizeChange: handlePageSizeChange,
          isMobile,
          t,
        })}
        t={t}
      >
        <CostCharts
          trend={overview?.trend}
          costStack={overview?.cost_stack}
          granularity={overview?.granularity}
          exchangeRate={overview?.exchange_rate}
          t={t}
        />
        <CostTables
          t={t}
          activeTab={activeTab}
          setActiveTab={setActiveTab}
          pageData={pageData}
          tableLoading={tableLoading}
          activePage={activePage}
          pageSize={pageSize}
          handlePageChange={handlePageChange}
          handlePageSizeChange={handlePageSizeChange}
          onRatioUpdated={refresh}
          onFilterByChannel={(channelId) => {
            filterByChannel(channelId);
            filterFormApiRef.current?.setValue('channel', channelId);
          }}
          exchangeRate={filters.exchangeRate}
        />
      </CardPro>
    </>
  );
};

export default CostAccounting;
