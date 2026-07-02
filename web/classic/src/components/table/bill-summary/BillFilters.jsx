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
import { Form, Button, Space } from '@douyinfe/semi-ui';

const BillFilters = ({ formApiRef, isAdminUser, onQuery, onExport, loading, t }) => {
  return (
    <Form getFormApi={(api) => (formApiRef.current = api)} layout='horizontal'>
      <Form.DatePicker field='start_time' label={t('开始时间')} type='dateTime' />
      <Form.DatePicker field='end_time' label={t('结束时间')} type='dateTime' />
      {isAdminUser && <Form.Input field='username' label={t('用户名')} />}
      {isAdminUser && <Form.Input field='channel' label={t('渠道ID')} />}
      <Form.Input field='token_name' label={t('令牌名称')} />
      <Form.Input field='model_name' label={t('模型名称')} />
      <Form.Input field='exchange_rate' label={t('汇率')} placeholder='7.3' />
      <Form.Switch field='with_detail' label={t('附带每日明细账')} />
      <Form.Switch field='detail_split_model' label={t('明细分不同模型')} />
      <Space>
        <Button theme='solid' loading={loading} onClick={onQuery}>
          {t('查询')}
        </Button>
        <Button onClick={onExport}>{t('导出汇总账单')}</Button>
      </Space>
    </Form>
  );
};

export default BillFilters;
