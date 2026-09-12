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

// 展开行客户端合并工具：与后端 controller/cost_stat.go 的 costMoney.add()/
// deriveRates() 口径保持一致 —— 只对"原始可加字段"求和，派生指标（成功率/
// 缓存率/平均TTFT/利润率）在求和后统一重新计算，绝不对派生字段直接求平均。

// 原始可加字段：来自 costMoney（后端 dto），breakdown 行内嵌展开为同级字段。
export const RAW_ADDITIVE_FIELDS = [
  'revenue_usd',
  'revenue_cny',
  'list_usd',
  'cost_cny',
  'profit_cny',
  'refund_usd',
  'prompt_tokens',
  'completion_tokens',
  'request_count',
  'cache_read_tokens',
  'cache_creation_tokens',
  'error_count',
  'frt_sum_ms',
  'frt_count',
];

// 重新派生 v2 指标，公式与零分母兜底规则与后端 costMoney.deriveRates() 一致：
// - total_tokens：非缓存输入 + 输出 + 缓存读取 + 缓存创建（后端已把 prompt_tokens
//   归一化为「非缓存输入」，四项互不重叠）
// - cache_rate：缓存读取 / 总输入（分母含缓存创建，不含输出；分母 0 → 0）
// - success_rate：request_count+error_count 为 0 时兜底为 1
// - avg_ttft_ms：frt_count 为 0 时兜底为 0
// - profit_rate：用展示口径的「收入 − 成本」重算（收入 0 → 0），与页面各列的
//   profitRateOf() 一致；后端 profit_cny 混入了筛选汇率，不能直接拿来相除
// - effective_discount：收入$ ÷ 刊例$（按合并后的总额重算，非各行取平均）
// - effective_ratio：成本¥ ÷ 刊例$（同上，跨计价版本时天然是加权均值）
export function deriveCostRates(row) {
  const promptTokens = Number(row.prompt_tokens) || 0;
  const completionTokens = Number(row.completion_tokens) || 0;
  const requestCount = Number(row.request_count) || 0;
  const errorCount = Number(row.error_count) || 0;
  const cacheReadTokens = Number(row.cache_read_tokens) || 0;
  const cacheCreationTokens = Number(row.cache_creation_tokens) || 0;
  const frtSumMs = Number(row.frt_sum_ms) || 0;
  const frtCount = Number(row.frt_count) || 0;
  const revenueUsd = Number(row.revenue_usd) || 0;
  const costCny = Number(row.cost_cny) || 0;

  const inputTokens = promptTokens + cacheReadTokens + cacheCreationTokens;
  row.total_tokens = inputTokens + completionTokens;
  row.success_rate =
    requestCount + errorCount === 0
      ? 1
      : requestCount / (requestCount + errorCount);
  row.cache_rate = inputTokens === 0 ? 0 : cacheReadTokens / inputTokens;
  row.avg_ttft_ms = frtCount === 0 ? 0 : frtSumMs / frtCount;
  row.profit_rate = revenueUsd === 0 ? 0 : (revenueUsd - costCny) / revenueUsd;
  const listUsd = Number(row.list_usd) || 0;
  row.effective_discount_known = listUsd !== 0;
  row.effective_discount =
    listUsd === 0 ? 0 : (Number(row.revenue_usd) || 0) / listUsd;
  // 真实成本倍率：渠道可能在区间内改过价，配置值只代表"现在的价"，只有用这一行
  // 自己的钱反推才是区间内实际付出的倍率。门槛沿用后端的 ListUsd == 0
  // （cost_stat.go deriveRates）——不是 > 0：只含退款的区间刊例价为负，此时商仍
  // 有意义，改用 > 0 会让本地折叠的父行与同一行从服务端取回的结果不一致。
  row.effective_ratio_known = listUsd !== 0;
  row.effective_ratio =
    listUsd === 0 ? 0 : (Number(row.cost_cny) || 0) / listUsd;
  return row;
}

// 布尔信号字段：任一子行命中，合并后的父行即命中（与后端 costMoney.add() 取或
// 的口径一致）。不参与求和，也不能重新派生 —— 它们是采集到的版本身份事实，
// 不是金额的函数。
const SIGNAL_FLAG_FIELDS = [
  'ratio_mixed',
  'discount_mixed',
  'discount_special',
];

/**
 * 按 keyFields 合并 breakdown 明细行：对 RAW_ADDITIVE_FIELDS 求和，重新派生比率指标。
 * keyFields 为空（null/undefined/[]）时视为"明细"模式，原样返回（不合并）。
 *
 * @param {Array<object>} rows 展开行明细（costBreakdownRow[]，字段已在 JSON 中展平）
 * @param {string[]|null} keyFields 保留分组的身份字段，如 ['channel_id','channel_name']
 * @returns {Array<object>} 合并后的行（新对象，不修改入参）
 */
export function mergeBreakdown(rows, keyFields) {
  if (!Array.isArray(rows) || rows.length === 0) return [];
  if (!keyFields || keyFields.length === 0) return rows;

  // 随身份字段一起保留的挂载字段：合并仍保留渠道身份时，渠道当前的计价配置对
  // 整组生效（组内各行同一身份 → 同一取值）。
  // 用户侧没有挂载字段：用户折扣不再由后端下发配置值，改由 deriveCostRates 用
  // 这一行自己的钱反推（effective_discount），挂载无从可挂。
  // effective_ratio 同理不挂载：它由 deriveCostRates 按合并后的总额重算，挂载
  // 过来的父行值会被直接覆盖。
  const carryFields = [];
  if (keyFields.includes('channel_id')) {
    carryFields.push('cost_mode', 'cost_ratio', 'cost_discount');
  }

  const groups = new Map();
  const order = [];
  for (const row of rows) {
    const key = keyFields.map((f) => row[f] ?? '').join('␟');
    let g = groups.get(key);
    if (!g) {
      g = {};
      keyFields.forEach((f) => {
        g[f] = row[f];
      });
      carryFields.forEach((f) => {
        g[f] = row[f];
      });
      RAW_ADDITIVE_FIELDS.forEach((f) => {
        g[f] = 0;
      });
      SIGNAL_FLAG_FIELDS.forEach((f) => {
        g[f] = false;
      });
      // 覆盖率是占比，不能直接相加：先按 list_usd 加权累计成绝对基数，
      // 汇总完再还原成占比（见下方 order.map）。
      g.discount_coverage = 0;
      groups.set(key, g);
      order.push(key);
    }
    RAW_ADDITIVE_FIELDS.forEach((f) => {
      g[f] = (Number(g[f]) || 0) + (Number(row[f]) || 0);
    });
    SIGNAL_FLAG_FIELDS.forEach((f) => {
      g[f] = g[f] || Boolean(row[f]);
    });
    g.discount_coverage +=
      (Number(row.discount_coverage) || 0) * (Number(row.list_usd) || 0);
  }
  return order.map((key) => {
    const g = groups.get(key);
    // 门槛用 > 0（与后端 cost_stat.go 的覆盖率门槛一致，而非派生比率用的 !== 0）：
    // 区间只含退款时 list_usd 为负，Math.min 没有下界，负分母会算出 "-320% 的消费
    // 带定价信息" 这种话。
    const listUsd = Number(g.list_usd) || 0;
    g.discount_coverage =
      listUsd > 0 ? Math.min(g.discount_coverage / listUsd, 1) : 0;
    return deriveCostRates(g);
  });
}

// 每个维度 Tab 下「查看方式」Select 的可选项：value 唯一标识，keyFields 传给
// mergeBreakdown（null = 明细，不合并）。中文 label 走 i18n key 原样翻译。
export const MERGE_VIEW_OPTIONS = {
  users: [
    { value: 'detail', labelKey: '明细', keyFields: null },
    {
      value: 'merge_model',
      labelKey: '合并模型',
      keyFields: ['channel_id', 'channel_name'],
    },
    {
      value: 'merge_channel',
      labelKey: '合并渠道',
      keyFields: ['model_name'],
    },
  ],
  models: [
    { value: 'detail', labelKey: '明细', keyFields: null },
    {
      value: 'merge_user',
      labelKey: '合并用户',
      keyFields: ['channel_id', 'channel_name'],
    },
    { value: 'merge_channel', labelKey: '合并渠道', keyFields: ['username'] },
  ],
  channels: [
    { value: 'detail', labelKey: '明细', keyFields: null },
    { value: 'merge_user', labelKey: '合并用户', keyFields: ['model_name'] },
    { value: 'merge_model', labelKey: '合并模型', keyFields: ['username'] },
  ],
};
