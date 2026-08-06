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

// 成本报表的双币种展示：与站点管理员在「系统设置 → 运营设置 → 额度展示类型」
// (quota_display_type) 里选择的口径保持一致 —— 与 helpers/render.jsx 的
// renderQuota()/getQuotaDisplayType() 读取同一个 localStorage key。
// 表格与 KPI 始终展示美元(USD)与人民币(CNY)两条线（报表口径本身就是"上游计价
// 美元 + 结算成本人民币"），仅用展示类型决定"哪种货币在前"；
// 图表只有一条轴，走 getCostChartCurrency() 解析出单一展示货币。

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
 * 按 primary 货币把一对 USD/CNY 金额格式化为 {primary, secondary} 两行文本。
 * @param {number} usd
 * @param {number} cny
 * @param {'usd'|'cny'} primary
 * @param {number} digits
 * @returns {{primary: string, secondary: string}}
 */
export function formatDualMoney(usd, cny, primary, digits = 2) {
  return primary === 'cny'
    ? { primary: formatCny(cny, digits), secondary: formatUsd(usd, digits) }
    : { primary: formatUsd(usd, digits), secondary: formatCny(cny, digits) };
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
 * CNY → ¥ + 站点汇率；CUSTOM → 自定义符号 + 自定义汇率；
 * USD / TOKENS → $（金额用 token 表达无意义，与新前端 getBillingDisplayMeta 同口径）。
 *
 * ## 两个汇率不能混用
 * - **查询汇率**（筛选栏输入，默认 6.8）：成本核算口径，后端已用它算出全部
 *   *_cny 字段；
 * - **展示汇率**（getCurrencyConfig().rate）：全站展示口径。
 *
 * 因此 format() 只接受**美元**。持有 *_cny 的调用方必须先用查询汇率除回美元
 * （deriveUsdFromCny），保证只换算一次——直接喂 CNY 会叠加两个汇率。
 *
 * @returns {{symbol: string, rateFromUsd: number, format: (usd: number) => string}}
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
    format: (usd, digits = 2) =>
      `${displaySymbol}${(Number(usd || 0) * rateFromUsd).toFixed(digits)}`,
  };
}
