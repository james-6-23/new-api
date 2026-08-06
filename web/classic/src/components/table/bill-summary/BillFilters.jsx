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

import React from 'react';
import { Form, Button } from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';

// 账单筛选工具栏：布局对齐使用日志页（紧凑网格 + pure/small 控件 + 底部操作行）。
const BillFilters = ({ formApiRef, isAdminUser, onQuery, onExport, loading, t }) => {
  return (
    <Form
      getFormApi={(api) => (formApiRef.current = api)}
      allowEmpty={true}
      autoComplete='off'
      layout='vertical'
    >
      <div className='flex flex-col gap-2'>
        <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-2'>
          <Form.DatePicker
            field='start_time'
            className='w-full'
            type='dateTime'
            placeholder={t('开始时间')}
            showClear
            pure
            size='small'
          />
          <Form.DatePicker
            field='end_time'
            className='w-full'
            type='dateTime'
            placeholder={t('结束时间')}
            showClear
            pure
            size='small'
          />
          {isAdminUser && (
            <>
              <Form.Input
                field='username'
                prefix={<IconSearch />}
                placeholder={t('用户名')}
                showClear
                pure
                size='small'
              />
              <Form.Input
                field='channel'
                prefix={<IconSearch />}
                placeholder={t('渠道ID')}
                showClear
                pure
                size='small'
              />
            </>
          )}
          <Form.Input
            field='token_name'
            prefix={<IconSearch />}
            placeholder={t('令牌名称')}
            showClear
            pure
            size='small'
          />
          <Form.Input
            field='model_name'
            prefix={<IconSearch />}
            placeholder={t('模型名称')}
            showClear
            pure
            size='small'
          />
          <Form.Input
            field='exchange_rate'
            placeholder={t('汇率')}
            showClear
            pure
            size='small'
          />
          <Form.Select
            field='granularity'
            placeholder={t('统计粒度')}
            initValue='day'
            className='w-full'
            pure
            size='small'
          >
            <Form.Select.Option value='day'>{t('按天')}</Form.Select.Option>
            <Form.Select.Option value='week'>{t('按周')}</Form.Select.Option>
            <Form.Select.Option value='month'>{t('按月')}</Form.Select.Option>
          </Form.Select>
        </div>

        {/* 操作按钮区域（导出配置 + 查询/导出按钮相邻收拢到右侧） */}
        <div className='flex flex-col sm:flex-row justify-end items-start sm:items-center gap-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <Form.Select
              field='bill_mode'
              initValue='internal'
              pure
              size='small'
              className='min-w-[180px]'
            >
              <Form.Select.Option value='internal'>
                {t('内部（分渠道分模型）')}
              </Form.Select.Option>
              <Form.Select.Option value='external'>
                {t('对外客户（合并渠道）')}
              </Form.Select.Option>
            </Form.Select>
            <Form.Select
              field='with_detail'
              initValue={false}
              pure
              size='small'
              className='min-w-[140px]'
            >
              <Form.Select.Option value={false}>
                {t('不含每日明细')}
              </Form.Select.Option>
              <Form.Select.Option value={true}>
                {t('附带每日明细账')}
              </Form.Select.Option>
            </Form.Select>
            <Form.Select
              field='detail_split_model'
              initValue={false}
              pure
              size='small'
              className='min-w-[140px]'
            >
              <Form.Select.Option value={false}>
                {t('明细不分模型')}
              </Form.Select.Option>
              <Form.Select.Option value={true}>
                {t('明细分不同模型')}
              </Form.Select.Option>
            </Form.Select>
            <Button theme='solid' size='small' loading={loading} onClick={onQuery}>
              {t('查询')}
            </Button>
            <Button size='small' onClick={onExport}>
              {t('导出汇总账单')}
            </Button>
          </div>
        </div>
      </div>
    </Form>
  );
};

export default BillFilters;
