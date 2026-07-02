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
import { Table, Typography } from '@douyinfe/semi-ui';

const money = (v) => (typeof v === 'number' ? v.toFixed(6) : v);

const BillSummaryTable = ({ items, total, page, pageSize, summary, isAdminUser, onPageChange, t }) => {
  const columns = [
    { title: t('日期'), dataIndex: 'date' },
    ...(isAdminUser
      ? [
          { title: t('用户名'), dataIndex: 'username' },
          { title: t('渠道ID'), dataIndex: 'channel_id' },
        ]
      : []),
    { title: t('令牌名称'), dataIndex: 'token_name' },
    { title: t('模型名称'), dataIndex: 'model_name' },
    { title: t('金额(美元)'), dataIndex: 'amount_usd', render: (v) => `$${money(v)}` },
    { title: t('汇率'), dataIndex: 'exchange_rate' },
    { title: t('金额(人民币)'), dataIndex: 'amount_cny', render: (v) => `¥${money(v)}` },
    { title: t('输入tokens'), dataIndex: 'prompt_tokens' },
    { title: t('输出tokens'), dataIndex: 'completion_tokens' },
    { title: t('缓存读取tokens'), dataIndex: 'cache_read_tokens' },
    { title: t('缓存创建tokens'), dataIndex: 'cache_creation_tokens' },
  ];

  return (
    <div>
      {summary && (
        <Typography.Text type='secondary' style={{ display: 'block', margin: '8px 0' }}>
          {t('合计')}: ${money(summary.total_amount_usd)} / ¥{money(summary.total_amount_cny)} ·{' '}
          {t('输入tokens')} {summary.total_prompt_tokens} · {t('输出tokens')} {summary.total_completion_tokens} ·{' '}
          {t('缓存读取tokens')} {summary.total_cache_read_tokens} · {t('缓存创建tokens')}{' '}
          {summary.total_cache_creation_tokens}
        </Typography.Text>
      )}
      <Table
        columns={columns}
        dataSource={items}
        rowKey={(r, i) => `${r.date}-${r.username}-${r.channel_id}-${r.token_name}-${r.model_name}-${i}`}
        pagination={{
          currentPage: page,
          pageSize,
          total,
          onPageChange,
        }}
      />
    </div>
  );
};

export default BillSummaryTable;
