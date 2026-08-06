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

import React, { useState } from 'react';
import {
  Tabs,
  TabPane,
  Tag,
  Button,
  Modal,
  InputNumber,
  Select,
  Typography,
} from '@douyinfe/semi-ui';
import { IconEdit, IconChevronDown, IconChevronRight } from '@douyinfe/semi-icons';
import CardTable from '../common/ui/CardTable';
import { API, showError, showSuccess } from '../../helpers';
import { mergeBreakdown, MERGE_VIEW_OPTIONS } from './costMerge';
import {
  deriveCnyFromUsd,
  deriveUsdFromCny,
  formatDualMoney,
  getMoneyPrimaryCurrency,
} from './costFormat';
import { CostHelpHeader, CostHelpFormula, CostHelpNotes } from './CostHelp';
import {
  CostRatioDiscountCell,
  RequestOutcomeCell,
  TokensHoverCell,
  UserDiscountCell,
} from './CostUserCells';

const { Text } = Typography;

const profitStyle = (v) => ({
  color:
    Number(v || 0) >= 0
      ? 'var(--semi-color-success)'
      : 'var(--semi-color-danger)',
});

const percent = (v, digits = 2) => `${(Number(v || 0) * 100).toFixed(digits)}%`;

// 双币种金额单元格：主行随管理员配置的 quota_display_type，副行为次要货币，
// 字号更小、灰色（与 web/default 端 MoneyDualCell 的展示口径一致）。
const DualMoneyCell = ({ primary, secondary }) => (
  <div className='flex flex-col items-end leading-tight'>
    <span>{primary}</span>
    <Text type='tertiary' size='small'>
      {secondary}
    </Text>
  </div>
);

// ========== 布局对齐用的显式列宽 ==========
// 展开明细行现已直接以普通行的形式插入同一张表的 dataSource（见下方
// buildFlattenedRows），与父行共用同一个 colgroup，天然对齐，不再需要单独的
// 宽度预算表来"凑"两张表的列宽。
const IDENTITY_COLUMN_WIDTH = 220; // 身份列（用户/模型/渠道），展开明细行在此列内缩进展示
const ACTION_COLUMN_WIDTH = 140; // 「操作」列：父行显示「查看方式」，明细行显示「只看该渠道」
const CHANNEL_USER_COUNT_WIDTH = 90; // 供应商维度 用户数列
const RATIO_DISCOUNT_WIDTH = 130; // 成本倍率/折扣列（2.5/倍率、0.8/折扣、加权 ≈）
const USER_DISCOUNT_WIDTH = 110; // 用户折扣列（0.8/分组、0.7/专属）

// 每个维度 Tab 下，展开明细行固定展示的两个身份字段（顺序与 costMerge.js 的
// MERGE_VIEW_OPTIONS 对应：字段若不在当前查看方式的 keyFields 里，视为"已合并"，
// 显示为"—"，但字段本身始终保留，避免切换查看方式时布局跳动）。
const BREAKDOWN_IDENTITY_FIELDS = {
  users: ['channel', 'model_name'],
  models: ['username', 'channel'],
  channels: ['username', 'model_name'],
};

// 身份字段 -> 判断其是否被当前 keyFields 保留所用的代表性行字段
const IDENTITY_FIELD_SIGNATURE = {
  channel: 'channel_id',
  model_name: 'model_name',
  username: 'username',
};

const isIdentityFieldCollapsed = (field, mode, keyFields) =>
  mode !== 'detail' && !(keyFields || []).includes(IDENTITY_FIELD_SIGNATURE[field]);

const renderIdentityFieldValue = (t, field, row) => {
  if (field === 'username') return row.username || '-';
  if (field === 'model_name') return row.model_name || '-';
  // field === 'channel'
  if (!row.channel_id) return t('未知渠道');
  return row.channel_name ? `${row.channel_name}（#${row.channel_id}）` : `#${row.channel_id}`;
};

// ========== 统一的指标列（顶层行与展开明细行共用同一套渲染） ==========
const buildMetricColumns = (t, exchangeRate, primaryCurrency, activeTab, onEditRatio) => [
  {
    key: 'list_usd',
    title: t('刊例价'),
    align: 'right',
    width: 130,
    render: (_, row) => {
      if (row.__isNote) return null;
      const { primary, secondary } = formatDualMoney(
        row.list_usd,
        deriveCnyFromUsd(row.list_usd, exchangeRate),
        primaryCurrency,
      );
      return <DualMoneyCell primary={primary} secondary={secondary} />;
    },
  },
  {
    key: 'cost_ratio_discount',
    title: (
      <CostHelpHeader title={t('成本倍率/折扣')}>
        <CostHelpFormula
          term={t('{{v}}/倍率', { v: '2.5' })}
          expression={t('渠道按每 $1 刊例价记成本 ¥2.5')}
        />
        <CostHelpFormula
          term={t('{{v}}/折扣', { v: '0.8' })}
          expression={t('渠道按刊例价的 80% × 汇率记成本')}
        />
        <CostHelpNotes
          notes={[
            t('用户/模型行跨多个渠道时显示 ≈ 加权值（成本 ÷ 刊例价），悬浮可看各渠道配置。'),
            t('「未填写」表示该渠道未配置成本计价，成本按 0 计。'),
          ]}
        />
      </CostHelpHeader>
    ),
    align: 'right',
    width: RATIO_DISCOUNT_WIDTH,
    render: (_, row) => {
      if (row.__isNote) return null;
      return (
        <CostRatioDiscountCell
          row={row}
          dim={activeTab}
          t={t}
          onEditRatio={onEditRatio}
        />
      );
    },
  },
  {
    key: 'user_discount',
    title: (
      <CostHelpHeader title={t('用户折扣')}>
        <CostHelpFormula
          term={t('用户折扣')}
          expression={t('所选区间内的收入 ÷ 刊例价')}
        />
        <CostHelpNotes
          notes={[
            t('显示的是实际生效折扣（按额度加权），专属倍率与区间内换组/改倍率都已自动反映。'),
            t('悬浮数值可与该分组当前配置的倍率作对比。'),
            t('专属倍率为「用户分组×令牌分组」的二维覆盖，优先于分组倍率生效。'),
            t('区间内没有刊例价（免费或未定价模型）时改为展示配置值。'),
            t('行内没有单一用户（模型/供应商父行）时显示「-」。'),
          ]}
        />
      </CostHelpHeader>
    ),
    align: 'right',
    width: USER_DISCOUNT_WIDTH,
    render: (_, row) => {
      if (row.__isNote) return null;
      return <UserDiscountCell row={row} t={t} />;
    },
  },
  {
    key: 'cost_cny',
    title: t('成本'),
    align: 'right',
    width: 130,
    render: (_, row) => {
      if (row.__isNote) return null;
      const { primary, secondary } = formatDualMoney(
        deriveUsdFromCny(row.cost_cny, exchangeRate),
        row.cost_cny,
        primaryCurrency,
      );
      return <DualMoneyCell primary={primary} secondary={secondary} />;
    },
  },
  {
    key: 'revenue',
    title: t('收入'),
    align: 'right',
    width: 130,
    render: (_, row) => {
      if (row.__isNote) return null;
      const { primary, secondary } = formatDualMoney(
        row.revenue_usd,
        row.revenue_cny,
        primaryCurrency,
      );
      return <DualMoneyCell primary={primary} secondary={secondary} />;
    },
  },
  {
    key: 'profit_cny',
    title: t('利润'),
    align: 'right',
    width: 130,
    render: (_, row) => {
      if (row.__isNote) return null;
      const { primary, secondary } = formatDualMoney(
        deriveUsdFromCny(row.profit_cny, exchangeRate),
        row.profit_cny,
        primaryCurrency,
      );
      return (
        <DualMoneyCell
          primary={<span style={profitStyle(row.profit_cny)}>{primary}</span>}
          secondary={secondary}
        />
      );
    },
  },
  {
    key: 'profit_rate',
    title: (
      <CostHelpHeader title={t('利润率')}>
        <CostHelpFormula term={t('利润率')} expression={t('利润 ÷ 收入')} />
        <CostHelpFormula term={t('利润')} expression={t('收入 − 成本')} />
        <CostHelpFormula term={t('成本')} expression={t('刊例价 × 成本倍率')} />
        <CostHelpNotes
          notes={[
            t('收入为 0 时利润率按 0 显示。'),
            t('成本倍率取渠道当前配置，不随历史变更回溯。'),
          ]}
        />
      </CostHelpHeader>
    ),
    align: 'right',
    width: 100,
    render: (_, row) =>
      row.__isNote ? null : (
        <span style={profitStyle(row.profit_rate)}>{percent(row.profit_rate)}</span>
      ),
  },
  {
    key: 'request_count',
    title: t('请求数'),
    align: 'right',
    width: 110,
    render: (_, row) =>
      row.__isNote ? null : (
        <RequestOutcomeCell
          requestCount={row.request_count}
          errorCount={row.error_count}
          successRate={row.success_rate}
          percent={percent}
        />
      ),
  },
  {
    key: 'total_tokens',
    title: t('总tokens'),
    align: 'right',
    width: 110,
    render: (_, row) =>
      row.__isNote ? null : <TokensHoverCell row={row} t={t} />,
  },
  {
    key: 'cache_rate',
    title: t('缓存命中率'),
    align: 'right',
    width: 100,
    render: (_, row) => (row.__isNote ? null : <span>{percent(row.cache_rate)}</span>),
  },
  {
    key: 'avg_ttft_ms',
    title: t('平均TTFT'),
    align: 'right',
    width: 110,
    render: (_, row) => {
      if (row.__isNote) return null;
      return Number(row.frt_count || 0) === 0 ? (
        <span>-</span>
      ) : (
        <span>{Number(row.avg_ttft_ms || 0).toFixed(0)} ms</span>
      );
    },
  },
];

const CostTables = ({
  t,
  activeTab,
  setActiveTab,
  pageData,
  tableLoading,
  activePage,
  pageSize,
  handlePageChange,
  handlePageSizeChange,
  onRatioUpdated,
  onFilterByChannel,
  exchangeRate,
}) => {
  const [ratioModal, setRatioModal] = useState({
    visible: false,
    channelId: null,
    channelName: '',
    mode: 'ratio',
    ratioValue: 0,
    discountValue: 0,
  });
  const [submitting, setSubmitting] = useState(false);
  const [mergeModes, setMergeModes] = useState({});
  const [expandedKeys, setExpandedKeys] = useState(() => new Set());
  const [subSuppliersModal, setSubSuppliersModal] = useState({
    visible: false,
    channelName: '',
    subSuppliers: [],
  });

  // 展开态与「查看方式」按行键存储。换维度/换页/换筛选后行集合整体变了，旧键要么
  // 指向不存在的行（残留内存），要么在分页复用同一批 user_id 时让新行意外呈展开态。
  // 这里在数据身份变化时清空，让每批数据都从收起状态开始。
  const dataIdentity = `${activeTab}|${activePage}|${pageSize}|${exchangeRate}`;
  const [stateScope, setStateScope] = useState(dataIdentity);
  if (stateScope !== dataIdentity) {
    setStateScope(dataIdentity);
    setExpandedKeys(new Set());
    setMergeModes({});
  }

  const primaryCurrency = getMoneyPrimaryCurrency();

  const openRatioModal = (record) => {
    setRatioModal({
      visible: true,
      channelId: record.channel_id,
      channelName: record.channel_name,
      mode: record.cost_mode === 'discount' ? 'discount' : 'ratio',
      ratioValue: record.cost_ratio || 0,
      discountValue: record.cost_discount || 0,
    });
  };

  const closeRatioModal = () => {
    if (submitting) return;
    setRatioModal((s) => ({ ...s, visible: false }));
  };

  const openSubSuppliersModal = (record) =>
    setSubSuppliersModal({
      visible: true,
      channelName: record.channel_name,
      subSuppliers: record.sub_suppliers || [],
    });
  const closeSubSuppliersModal = () =>
    setSubSuppliersModal((s) => ({ ...s, visible: false }));

  const activeRatioValue =
    ratioModal.mode === 'discount' ? ratioModal.discountValue : ratioModal.ratioValue;
  const isValidRatio =
    activeRatioValue !== null &&
    activeRatioValue !== undefined &&
    activeRatioValue !== '' &&
    !Number.isNaN(Number(activeRatioValue)) &&
    Number(activeRatioValue) >= 0;

  const submitRatio = async () => {
    if (!isValidRatio) return;
    setSubmitting(true);
    try {
      const channelRes = await API.get(`/api/channel/${ratioModal.channelId}`);
      const { success, message, data } = channelRes.data;
      if (!success) {
        showError(message);
        return;
      }

      let settingObj = {};
      if (data.setting) {
        try {
          const parsed = JSON.parse(data.setting);
          if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            settingObj = parsed;
          }
        } catch (e) {
          settingObj = {};
        }
      }
      settingObj.cost_mode = ratioModal.mode;
      settingObj.cost_ratio = Number(ratioModal.ratioValue) || 0;
      settingObj.cost_discount = Number(ratioModal.discountValue) || 0;

      const updateRes = await API.put('/api/channel/', {
        id: ratioModal.channelId,
        setting: JSON.stringify(settingObj),
      });
      if (updateRes.data.success) {
        showSuccess(t('操作成功完成！'));
        setRatioModal((s) => ({ ...s, visible: false }));
        onRatioUpdated && onRatioUpdated();
      } else {
        showError(updateRes.data.message);
      }
    } finally {
      setSubmitting(false);
    }
  };

  const rowKey = (record) => {
    if (activeTab === 'users') return `u-${record.user_id}`;
    if (activeTab === 'models') return `m-${record.model_name}`;
    return `c-${record.channel_id}`;
  };

  const getMergeMode = (rKey) => mergeModes[rKey] || 'detail';
  const setMergeMode = (rKey, mode) =>
    setMergeModes((prev) => ({ ...prev, [rKey]: mode }));

  const toggleExpanded = (rKey) =>
    setExpandedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(rKey)) next.delete(rKey);
      else next.add(rKey);
      return next;
    });

  const dimensionTitle =
    activeTab === 'users' ? t('用户') : activeTab === 'models' ? t('模型') : t('渠道');

  // 父行的身份列内容（不含展开箭头）：三个维度各自的展示逻辑与原实现一致。
  const renderParentIdentityContent = (row) => {
    if (activeTab === 'users') return row.username || '-';
    if (activeTab === 'models') return row.model_name || '-';
    return (
      <span className='flex items-center gap-1 flex-wrap'>
        <span>
          {row.channel_id
            ? `${row.channel_name}（#${row.channel_id}）`
            : `${t('未知渠道')}（#0）`}
        </span>
        {row.is_aggregator && (
          <Tag color='purple' shape='circle' size='small'>
            {t('聚合渠道')}
          </Tag>
        )}
        {(row.sub_suppliers || []).length > 0 && (
          <Button
            theme='borderless'
            type='tertiary'
            size='small'
            onClick={() => openSubSuppliersModal(row)}
          >
            {t('子供应商')}
          </Button>
        )}
      </span>
    );
  };

  // 展开明细行的身份列内容：固定展示当前维度的两个身份字段，合并掉的字段显示
  // "—"，缩进展示以区分层级。
  const renderChildIdentityContent = (row) => {
    const [fieldA, fieldB] = BREAKDOWN_IDENTITY_FIELDS[activeTab];
    return (
      <div className='pl-6 flex flex-col gap-0.5 text-xs'>
        {[fieldA, fieldB].map((field) => (
          <div key={field}>
            {isIdentityFieldCollapsed(field, row.__mode, row.__keyFields) ? (
              <Text type='tertiary'>—</Text>
            ) : (
              renderIdentityFieldValue(t, field, row)
            )}
          </div>
        ))}
      </div>
    );
  };

  const dimensionColumn = {
    key: 'dimension',
    title: dimensionTitle,
    fixed: 'left',
    width: IDENTITY_COLUMN_WIDTH,
    render: (_, row) => {
      if (row.__isNote) {
        return (
          <Text type='tertiary' size='small'>
            {row.__noteText}
          </Text>
        );
      }
      if (row.__isChild) {
        return renderChildIdentityContent(row);
      }
      const expandable = (row.breakdown || []).length > 0;
      const rKey = rowKey(row);
      const expanded = expandedKeys.has(rKey);
      return (
        <div className='flex items-center gap-1'>
          {expandable ? (
            <Button
              theme='borderless'
              type='tertiary'
              size='small'
              icon={expanded ? <IconChevronDown /> : <IconChevronRight />}
              onClick={() => toggleExpanded(rKey)}
              style={{ padding: 0, minWidth: 24 }}
            />
          ) : (
            <span style={{ width: 24, display: 'inline-block' }} />
          )}
          <span className='flex-1'>{renderParentIdentityContent(row)}</span>
        </div>
      );
    },
  };

  const channelExtraColumns =
    activeTab === 'channels'
      ? [
          {
            key: 'user_count',
            title: t('用户数'),
            align: 'right',
            width: CHANNEL_USER_COUNT_WIDTH,
            render: (_, row) =>
              row.__isChild ? (
                <Text type='tertiary'>—</Text>
              ) : (
                Number(row.user_count || 0)
              ),
          },
        ]
      : [];

  // 「操作」列：父行显示紧凑的「查看方式」Select（仅在该行有 breakdown 明细时
  // 展示）；展开出的明细行显示「只看该渠道」（仅在渠道身份字段未被合并、且该
  // 行确有 channel_id 时展示；供应商维度本身已按渠道分组，明细行没有渠道身份
  // 字段，不展示）。
  const actionColumn = {
    key: 'action',
    title: t('操作'),
    width: ACTION_COLUMN_WIDTH,
    render: (_, row) => {
      if (row.__isNote) return null;
      if (row.__isChild) {
        if (activeTab === 'channels') return null;
        if (isIdentityFieldCollapsed('channel', row.__mode, row.__keyFields)) return null;
        if (!row.channel_id) return null;
        return (
          <Button
            size='small'
            type='tertiary'
            theme='borderless'
            onClick={() => onFilterByChannel && onFilterByChannel(row.channel_id)}
          >
            {t('只看该渠道')}
          </Button>
        );
      }
      if (!(row.breakdown || []).length) return null;
      const rKey = rowKey(row);
      const options = MERGE_VIEW_OPTIONS[activeTab] || MERGE_VIEW_OPTIONS.users;
      return (
        <Select
          size='small'
          value={getMergeMode(rKey)}
          optionList={options.map((o) => ({
            label: t(o.labelKey),
            value: o.value,
          }))}
          onChange={(value) => setMergeMode(rKey, value)}
          style={{ width: '100%' }}
        />
      );
    },
  };

  const columns = [
    dimensionColumn,
    ...channelExtraColumns,
    ...buildMetricColumns(t, exchangeRate, primaryCurrency, activeTab, openRatioModal),
    actionColumn,
  ];

  // pageData 三个 tab 共用一份状态。切 tab 时新请求还在飞行中，若直接渲染上一个
  // 维度的 items，就会用新维度的列去读旧维度的行（用户列里出现渠道数据）。
  // dim 不匹配即视为陈旧，渲染空表 + loading。
  const dataIsStale = (pageData?.dim || activeTab) !== activeTab;
  const items = dataIsStale ? [] : pageData?.items || [];
  const total = dataIsStale ? 0 : pageData?.total || 0;

  // ========== 展平：把展开行的合并明细直接插入父行之后，成为同一张表的普通
  // 行（打 __isChild 标记）。单表单 colgroup，列宽由列定义唯一决定，父子行
  // 天然对齐，不再需要第二张表 + 宽度预算凑对齐。 ==========
  const buildFlattenedRows = () => {
    const result = [];
    for (const record of items) {
      const rKey = rowKey(record);
      result.push({ ...record, __rowKey: rKey });

      const rawRows = record.breakdown || [];
      if (rawRows.length === 0 || !expandedKeys.has(rKey)) continue;

      const options = MERGE_VIEW_OPTIONS[activeTab] || MERGE_VIEW_OPTIONS.users;
      const mode = getMergeMode(rKey);
      const activeOption = options.find((o) => o.value === mode) || options[0];
      const mergedRows = mergeBreakdown(rawRows, activeOption.keyFields);

      // 明细行补上自身字段带不动的身份挂载信息：
      //  - 供应商维度：渠道身份折叠进了父行，把父行的渠道计价配置注入每个明细行
      //    （成本倍率/折扣列因此能显示 2.5/倍率 而不是空）；
      //  - 用户维度：明细行都属于父行用户，注入父行的用户折扣字段（顺带覆盖
      //    合并视图丢掉挂载字段的情况）。
      const enrichChild = (bRow) => {
        if (activeTab === 'channels') {
          return {
            ...bRow,
            channel_id: record.channel_id,
            channel_name: record.channel_name,
            cost_mode: record.cost_mode,
            cost_ratio: record.cost_ratio,
            cost_discount: record.cost_discount,
            effective_ratio: record.effective_ratio,
          };
        }
        if (activeTab === 'users') {
          return {
            ...bRow,
            user_group: record.user_group,
            group_ratio: record.group_ratio,
            group_ratio_known: record.group_ratio_known,
            group_ratio_special: record.group_ratio_special,
            group_ratio_mixed: record.group_ratio_mixed,
          };
        }
        return bRow;
      };

      mergedRows.forEach((bRow, idx) => {
        result.push({
          ...enrichChild(bRow),
          __isChild: true,
          __parentKey: rKey,
          __rowKey: `${rKey}::${idx}`,
          __mode: mode,
          __keyFields: activeOption.keyFields,
        });
      });

      if (record.breakdown_truncated > 0) {
        result.push({
          __isChild: true,
          __isNote: true,
          __parentKey: rKey,
          __rowKey: `${rKey}::note`,
          __noteText: t('还有 {{count}} 项未展示', {
            count: record.breakdown_truncated,
          }),
        });
      }
    }
    return result;
  };

  const dataSource = buildFlattenedRows();

  /**
   * 行分组视觉：多个用户/模型/渠道展开后，父子行会糊成一片，靠这三条线区分：
   *  - 父行上边一条 2px 强分隔线 —— 组与组之间的切分；
   *  - 子行首列一条 2px 左导轨 —— 表明"这些行属于上面那个父行"；
   *  - 展开中的父行加底色 —— 长列表里一眼找到组头。
   *
   * 这里不用 web/default 端的 border-separate 卡片式留白：Semi 的 Table 自带
   * colgroup 与 sticky 滚动容器，改表格的 border-collapse 会连带影响列宽计算与
   * 表头吸顶，收益不值这个风险。
   */
  const rowClassName = (record) => {
    if (!record) return '';
    if (record.__isChild) {
      return '[&>td:first-child]:border-l-2 [&>td:first-child]:border-l-[var(--semi-color-primary)]';
    }
    const expanded = expandedKeys.has(record.__rowKey);
    return [
      '[&>td]:border-t-2 [&>td]:border-t-[var(--semi-color-border)]',
      expanded ? '[&>td]:bg-[var(--semi-color-fill-0)]' : '',
    ]
      .filter(Boolean)
      .join(' ');
  };

  return (
    <>
      <Tabs type='line' activeKey={activeTab} onChange={setActiveTab}>
        <TabPane tab={t('用户维度')} itemKey='users' />
        <TabPane tab={t('模型维度')} itemKey='models' />
        <TabPane tab={t('供应商维度')} itemKey='channels' />
      </Tabs>

      <div className='mt-2'>
        <CardTable
          columns={columns}
          dataSource={dataSource}
          loading={tableLoading}
          rowKey={(record) => record.__rowKey}
          onRow={(record) => ({ className: rowClassName(record) })}
          scroll={{ x: 'max-content' }}
          hidePagination
          pagination={{
            currentPage: activePage,
            pageSize,
            total,
            pageSizeOpts: [10, 20, 50, 100],
            showSizeChanger: true,
            onPageSizeChange: handlePageSizeChange,
            onPageChange: handlePageChange,
          }}
        />
      </div>

      <Modal
        centered
        visible={ratioModal.visible}
        onCancel={closeRatioModal}
        onOk={submitRatio}
        confirmLoading={submitting}
        okButtonProps={{ disabled: !isValidRatio }}
        title={
          <div className='flex items-center'>
            <IconEdit className='mr-2' />
            {t('计价方式')}
          </div>
        }
      >
        <div className='mb-2'>
          <Text type='secondary'>{ratioModal.channelName}</Text>
        </div>

        <Select
          className='w-full mb-2'
          value={ratioModal.mode}
          optionList={[
            { label: t('成本倍率（人民币:美元）'), value: 'ratio' },
            { label: t('成本折扣'), value: 'discount' },
          ]}
          onChange={(value) => setRatioModal((s) => ({ ...s, mode: value }))}
        />

        {ratioModal.mode === 'discount' ? (
          <InputNumber
            className='w-full'
            min={0}
            step={0.01}
            value={ratioModal.discountValue}
            onChange={(value) =>
              setRatioModal((s) => ({ ...s, discountValue: value }))
            }
          />
        ) : (
          <InputNumber
            className='w-full'
            min={0}
            step={0.01}
            value={ratioModal.ratioValue}
            onChange={(value) => setRatioModal((s) => ({ ...s, ratioValue: value }))}
          />
        )}

        <div className='pt-2'>
          <Text type='tertiary' size='small'>
            {!isValidRatio
              ? t('请输入不小于 0 的倍率')
              : ratioModal.mode === 'discount'
                ? t('折扣 {{d}} × 汇率 {{rate}} → 刊例 $1 记成本 ¥{{cny}}', {
                    d: ratioModal.discountValue,
                    rate: exchangeRate,
                    cny: (
                      Number(ratioModal.discountValue) * Number(exchangeRate || 0)
                    ).toFixed(2),
                  })
                : t('倍率 {{r}} → 上游每消耗刊例 $1 记成本 ¥{{cny}}', {
                    r: ratioModal.ratioValue,
                    cny: Number(ratioModal.ratioValue).toFixed(2),
                  })}
          </Text>
        </div>
      </Modal>

      <Modal
        centered
        visible={subSuppliersModal.visible}
        onCancel={closeSubSuppliersModal}
        footer={
          <Button type='primary' onClick={closeSubSuppliersModal}>
            {t('关闭')}
          </Button>
        }
        title={t('子供应商')}
      >
        <div className='mb-2'>
          <Text type='secondary'>{subSuppliersModal.channelName}</Text>
        </div>
        <CardTable
          columns={[
            {
              key: 'name',
              title: t('名称'),
              render: (_, row) => row.name || '-',
            },
            {
              key: 'cost_ratio',
              title: t('倍率'),
              align: 'right',
              render: (_, row) => row.cost_ratio,
            },
          ]}
          dataSource={subSuppliersModal.subSuppliers}
          rowKey={(row, idx) => `sub-${idx}`}
          hidePagination
          size='small'
        />
        <div className='pt-1'>
          <Text type='tertiary' size='small'>
            {t('报表成本按渠道级计价计算')}
          </Text>
        </div>
      </Modal>
    </>
  );
};

export default CostTables;
