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

// 成本报表的金额口径：与站点管理员在「系统设置 → 运营设置 → 额度展示类型」
// (quota_display_type) 里选择的口径保持一致 —— 与 helpers/render.jsx 的
// renderQuota()/getQuotaDisplayType() 读取同一个 localStorage key。
//
// 后端每个金额只是**一个数 N**，它的货币由额度展示类型声明；筛选栏汇率**不缩放
// 这个数**，只用来推导另一种货币的副行：
//
//   展示 CNY → 主行 ¥N，副行 $ (N ÷ 汇率)
//   展示 USD → 主行 $N，副行 ¥ (N × 汇率)
//
// 所以切换展示类型只是给同一个 N 换标签并翻转推导方向，绝不会把主行数值乘大。
// 图表只有一条轴，走 getCostChartCurrency() 只贴符号、同样不缩放。

import { getCurrencyConfig } from '../../helpers/render';

/** 读取管理员配置的额度展示类型，兜底 'USD'。 */
export function getQuotaDisplayType() {
  return localStorage.getItem('quota_display_type') || 'USD';
}

/** 成本报表双币种展示时，哪种货币显示在主行：仅当管理员显式选择 CNY 时才是 CNY 主行。 */
export function getMoneyPrimaryCurrency() {
  return getQuotaDisplayType() === 'CNY' ? 'cny' : 'usd';
}

export function formatUsd(value, digits = 2) {
  return `$${Number(value || 0).toFixed(digits)}`;
}

export function formatCny(value, digits = 2) {
  return `¥${Number(value || 0).toFixed(digits)}`;
}

/**
 * 成本报表的金额展示：amount 已经是展示货币下的那个数 N，主行只贴符号，
 * 副行用筛选栏汇率推导出另一种货币（见文件头注释）。
 *
 * @param {number} amount 后端返回的单一金额，单位即展示货币
 * @param {'usd'|'cny'} primary 展示类型决定的主行货币
 * @param {number} exchangeRate 筛选栏汇率，仅用于推导副行
 * @param {number} digits
 * @returns {{primary: string, secondary: string}}
 */
export function resolveDualMoney(amount, primary, exchangeRate, digits = 2) {
  const n = Number(amount || 0);
  return primary === 'cny'
    ? {
        primary: formatCny(n, digits),
        secondary: formatUsd(deriveUsdFromCny(n, exchangeRate), digits),
      }
    : {
        primary: formatUsd(n, digits),
        secondary: formatCny(deriveCnyFromUsd(n, exchangeRate), digits),
      };
}

/**
 * 展示货币下的利润：后端 profit_cny = revenue_usd × 汇率 − cost_cny，
 * 只把汇率混进了一边，汇率 ≠ 1 时三个主行数字就对不上账。
 * 统一用同一口径的两个数相减重算。
 */
export function profitAmountOf(row) {
  return (Number(row?.revenue_usd) || 0) - (Number(row?.cost_cny) || 0);
}

/** 利润率：用重算后的同口径金额相除。 */
export function profitRateOf(row) {
  const revenue = Number(row?.revenue_usd) || 0;
  if (!revenue) return 0;
  return profitAmountOf(row) / revenue;
}

/** 用当前查询生效的汇率，把仅有人民币的字段（成本/利润）换算出美元。 */
export function deriveUsdFromCny(cny, exchangeRate) {
  return Number(exchangeRate) > 0 ? Number(cny || 0) / Number(exchangeRate) : 0;
}

/** 用当前查询生效的汇率，把仅有美元的字段（刊例价）换算出人民币。 */
export function deriveCnyFromUsd(usd, exchangeRate) {
  return Number(usd || 0) * Number(exchangeRate || 0);
}

/** 倍率展示：×1.4 这种形式，最多 4 位小数且去掉尾随 0。 */
export function formatCostRatio(ratio) {
  const n = Number(ratio || 0);
  if (!n) return '-';
  return `×${Number(n.toFixed(4))}`;
}

/**
 * 用户行的实际生效成本倍率：用户跨多个渠道，没有单一配置倍率，
 * 只能用这一行自己的钱反推 —— cost_cny / (list_usd × 汇率)。
 * 与后端 cost_cny = list_usd × ratio 互为逆运算，因此结果就是加权倍率。
 * 刊例价为 0（无上游计费基数）时返回 null，由调用方渲染 '-'。
 */
export function effectiveCostRatioOf(row) {
  const listUsd = Number(row?.list_usd || 0);
  const costCny = Number(row?.cost_cny || 0);
  if (!listUsd) return null;
  return costCny / listUsd;
}

/**
 * 图表的单一展示货币，跟随「额度展示类型」。
 *
 * 表格/KPI 能并排放两种货币，图表只有一条轴，必须收敛成一种：
 * CNY → ¥；CUSTOM → 自定义符号；
 * USD / TOKENS → $（金额用 token 表达无意义，与新前端 getBillingDisplayMeta 同口径）。
 *
 * format() 接受的是**展示货币下的那个数 N**，只贴符号、不做换算 —— 后端返回的
 * 金额已经是展示货币口径（见文件头注释），再乘一次 rateFromUsd 就会把本该只是
 * 换个标签的数值放大。rateFromUsd 仍然导出，供确实需要"站点 USD→本币汇率"的
 * 调用方使用，但成本图表不属于这一类。
 *
 * @returns {{symbol: string, rateFromUsd: number, format: (amount: number) => string}}
 */
export function getCostChartCurrency() {
  const { symbol, rate, type } = getCurrencyConfig();
  // TOKENS 没有货币语义，退回美元；其余沿用 getCurrencyConfig 的符号与汇率。
  const isTokens = type === 'TOKENS';
  const displaySymbol = isTokens ? '$' : symbol;
  const rateFromUsd = isTokens ? 1 : Number(rate) || 1;
  return {
    symbol: displaySymbol,
    rateFromUsd,
    format: (amount, digits = 2) =>
      `${displaySymbol}${Number(amount || 0).toFixed(digits)}`,
  };
}
