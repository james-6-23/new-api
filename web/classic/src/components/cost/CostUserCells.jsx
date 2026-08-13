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
import { Button, Popover, Tag, Tooltip, Typography } from '@douyinfe/semi-ui';
import { IconEdit } from '@douyinfe/semi-icons';
import { effectiveCostRatioOf, formatCostRatio } from './costFormat';

const { Text } = Typography;

const hoverTextStyle = {
  cursor: 'help',
  textDecoration: 'underline dotted',
  textUnderlineOffset: 2,
};

/** "2.5"、"0.8" —— 配置数值，去掉尾随 0。 */
const trimRatioNumber = (value) => {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  return String(Number(n.toFixed(4)));
};

/**
 * 「2.5/倍率」「0.8/折扣」—— 单个渠道的计价配置统一展示格式。
 * 无可用配置时返回 null（由调用方决定显示 未填写 还是别的兜底）。
 */
export const configuredPricingLabel = (row, t) => {
  if (row.cost_mode === 'discount' && Number(row.cost_discount) > 0) {
    return t('{{v}}/折扣', { v: trimRatioNumber(row.cost_discount) });
  }
  if (Number(row.cost_ratio) > 0) {
    return t('{{v}}/倍率', { v: trimRatioNumber(row.cost_ratio) });
  }
  return null;
};

/**
 * 实际成本倍率 + 各渠道当前配置悬浮清单：用于用户/模型父行 —— 一行跨多个渠道、
 * 也可能跨多个计价版本，没有单一配置值可显示，只能用这一行自己的钱反推
 * （成本 ÷ 刊例价）。清单里的配置值是各渠道**当前**的价，与区间内实际生效的价
 * 可能不同（改过价就会不同），仅供对照。
 */
const WeightedCostRatio = ({ row, t }) => {
  const ratio = effectiveCostRatioOf(row);
  const breakdown = Array.isArray(row.breakdown) ? row.breakdown : [];
  const channelIds = new Set(
    breakdown.filter((b) => b?.channel_id).map((b) => b.channel_id),
  );

  if (ratio == null) {
    return <span style={{ color: 'var(--semi-color-text-2)' }}>-</span>;
  }

  // 跨渠道，或单渠道但区间内改过价 —— 两种情况下这个数都是加权值，标 ≈。
  const blended = channelIds.size > 1 || Boolean(row.ratio_mixed);
  const text = `${blended ? '≈' : ''}${formatCostRatio(ratio)}`;

  if (!breakdown.length) return <span>{text}</span>;

  return (
    <Popover
      showArrow
      trigger='hover'
      position='left'
      content={
        <div style={{ maxWidth: 300, padding: '8px 4px', lineHeight: 1.6 }}>
          <div style={{ color: 'var(--semi-color-text-2)', marginBottom: 6 }}>
            {t('实际成本倍率 = 区间内成本 ÷ 刊例价；下列为各渠道当前配置')}
          </div>
          {breakdown.map((b, index) => (
            <div
              key={`${b.channel_id || 'none'}-${index}`}
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                gap: 16,
                padding: '2px 0',
              }}
            >
              <span
                style={{
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {b.channel_name || t('未知渠道')}
              </span>
              <span style={{ whiteSpace: 'nowrap' }}>
                {configuredPricingLabel(b, t) || t('未填写')}
              </span>
            </div>
          ))}
        </div>
      }
    >
      <span style={hoverTextStyle}>{text}</span>
    </Popover>
  );
};

/**
 * 统一的「成本倍率/折扣」单元格，三个维度、父行与明细行共用：
 *  - 供应商维度父行：渠道当前配置（2.5/倍率、0.8/折扣）+ 未填写标签 + 区间内改过
 *    价时的 ≈ 标记 + 编辑按钮；
 *  - 携带渠道计价字段的行（明细行；供应商维度明细行由展平时从父行注入）：配置标签；
 *  - 用户/模型父行：加权倍率 + 各渠道悬浮清单；
 *  - 合并后的明细行：有刊例价基数时显示加权值，否则 '-'。
 */
export const CostRatioDiscountCell = ({ row, dim, t, onEditRatio }) => {
  const isParent = !row.__isChild;

  if (dim === 'channels' && isParent) {
    if (!row.channel_id) {
      return <span style={{ color: 'var(--semi-color-text-2)' }}>-</span>;
    }
    const label = configuredPricingLabel(row, t);
    return (
      <div className='flex items-center justify-end gap-1'>
        {!row.priced || !label ? (
          <Tag color='yellow' shape='circle'>
            {t('未填写')}
          </Tag>
        ) : (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'flex-end',
              lineHeight: 1.3,
            }}
          >
            <span
              style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}
            >
              {label}
              {row.ratio_mixed && (
                <Tooltip
                  content={t(
                    '区间内成本倍率有变更：成本已按各计价版本分段计算，标签显示的是当前生效价。',
                  )}
                >
                  <Tag color='orange' shape='circle' size='small'>
                    ≈
                  </Tag>
                </Tooltip>
              )}
            </span>
            {/* 折扣模式的等效倍率来自这一行自己的钱（成本 ÷ 刊例价），
                没有可定价用量时它是 0，写出来就成了「≈¥0.00/$1」—— 那不是该渠道
                的价，只是这段区间没得算。 */}
            {row.cost_mode === 'discount' && row.effective_ratio_known && (
              <Text type='tertiary' size='small'>
                ≈¥{Number(row.effective_ratio || 0).toFixed(2)}/$1
              </Text>
            )}
          </div>
        )}
        <Button
          icon={<IconEdit />}
          type='tertiary'
          theme='borderless'
          size='small'
          onClick={() => onEditRatio && onEditRatio(row)}
        />
      </div>
    );
  }

  const label = configuredPricingLabel(row, t);
  if (label) return <span>{label}</span>;
  // 有渠道身份但未配置计价 —— 与「无计算基数」区分开。
  if (row.channel_id) {
    return <Text type='tertiary'>{t('未填写')}</Text>;
  }
  if (isParent && (row.breakdown || []).length > 0) {
    return <WeightedCostRatio row={row} t={t} />;
  }
  if (Number(row.list_usd) > 0) {
    return <span>≈{formatCostRatio(effectiveCostRatioOf(row))}</span>;
  }
  return <span style={{ color: 'var(--semi-color-text-2)' }}>-</span>;
};

/**
 * 「用户折扣」= 区间内**实际生效**的折扣（effective_discount = 收入 ÷ 刊例价，
 * 按额度加权）。这是客户真正付的价：专属倍率、跨分组混用、区间内改过倍率，全都
 * 已经落在这个商里，不需要（也不再能）跟配置对照 —— 报表不再下发配置倍率，改价
 * 历史由渠道计价版本承载，配置值只代表"现在的价"，拿它跟一段历史区间比本就不对。
 *
 * 刊例价为 0（免费/未定价模型，或区间内没有可定价用量）时商无意义，显示 '-'。
 */
export const UserDiscountCell = ({ row, t }) => {
  const actualKnown = Boolean(row.effective_discount_known);
  const mixed = Boolean(row.discount_mixed);
  const special = Boolean(row.discount_special);
  // 兜底取 0 而不是 1：discount_coverage 是 omitempty，后端只在有定价基数时才赋值
  // （cost_stat.go 的 DiscountTotalBasis > 0 门槛），字段缺席的含义正是"没有任何
  // 消费带定价信息"。兜底成 1 会在覆盖率最差时恰好把提示藏起来。
  const coverage = Number(row.discount_coverage) || 0;

  if (!actualKnown) {
    return <span style={{ color: 'var(--semi-color-text-2)' }}>-</span>;
  }

  const actual = Number(row.effective_discount) || 0;
  const subtle = { color: 'var(--semi-color-text-2)' };
  const spread = { display: 'flex', justifyContent: 'space-between', gap: 16 };

  return (
    <Popover
      showArrow
      trigger='hover'
      position='left'
      content={
        <div style={{ maxWidth: 320, padding: '8px 4px', lineHeight: 1.6 }}>
          <div style={spread}>
            <span style={subtle}>{t('实际折扣（本区间）')}</span>
            <span>{trimRatioNumber(actual)}</span>
          </div>
          <div style={{ ...subtle, marginTop: 6 }}>
            {t('实际折扣 = 所选区间内的收入 ÷ 刊例价（按额度加权）。')}
          </div>
          {special && (
            <div style={{ ...subtle, marginTop: 6 }}>
              {t(
                '命中专属倍率：区间内存在「用户分组×令牌分组」的专属倍率配置。',
              )}
            </div>
          )}
          {mixed && (
            <div style={{ color: 'var(--semi-color-warning)', marginTop: 6 }}>
              {t('区间内折扣有变更 —— 用户可能换过分组，或倍率被调整过。')}
            </div>
          )}
          {coverage < 0.99 && (
            <div style={{ ...subtle, marginTop: 6 }}>
              {t(
                '折扣信息覆盖率 {{pct}}%，部分日志缺少定价字段，折扣仅含已知部分。',
                {
                  pct: Math.round(coverage * 100),
                },
              )}
            </div>
          )}
        </div>
      }
    >
      <span style={hoverTextStyle}>
        <span
          style={mixed ? { color: 'var(--semi-color-warning)' } : undefined}
        >
          {trimRatioNumber(actual)}
        </span>
      </span>
    </Popover>
  );
};

/** 请求数按 成功/失败 + 成功率 合成一列，替代原先分开的「请求数」「成功率」。 */
export const RequestOutcomeCell = ({
  requestCount,
  errorCount,
  successRate,
  percent,
}) => (
  <div
    style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'flex-end',
      lineHeight: 1.3,
    }}
  >
    <span>
      <span
        style={{
          color: requestCount
            ? 'var(--semi-color-success)'
            : 'var(--semi-color-text-2)',
        }}
      >
        {Number(requestCount || 0)}
      </span>
      <span style={{ color: 'var(--semi-color-text-2)' }}> / </span>
      <span
        style={{
          color: errorCount
            ? 'var(--semi-color-danger)'
            : 'var(--semi-color-text-2)',
        }}
      >
        {Number(errorCount || 0)}
      </span>
    </span>
    <span style={{ color: 'var(--semi-color-text-2)', fontSize: 11 }}>
      {percent(successRate ?? 1)}
    </span>
  </div>
);

/**
 * 总 tokens 悬浮明细：数字 + 悬浮列出 非缓存输入/输出/缓存读取/缓存创建 四项。
 * 四项互不重叠，相加恒等于总 tokens。
 */
export const TokensHoverCell = ({ row, t }) => (
  <Popover
    showArrow
    trigger='hover'
    position='left'
    content={
      <div style={{ minWidth: 200, padding: '8px 4px', lineHeight: 1.8 }}>
        {[
          ['非缓存输入 tokens', row.prompt_tokens],
          ['输出 tokens', row.completion_tokens],
          ['缓存读取 tokens', row.cache_read_tokens],
          ['缓存创建 tokens', row.cache_creation_tokens],
        ].map(([labelKey, value]) => (
          <div
            key={labelKey}
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              gap: 16,
            }}
          >
            <span style={{ color: 'var(--semi-color-text-2)' }}>
              {t(labelKey)}
            </span>
            <span className='tabular-nums'>{Number(value || 0)}</span>
          </div>
        ))}
      </div>
    }
  >
    <span style={hoverTextStyle}>{Number(row.total_tokens || 0)}</span>
  </Popover>
);
