# 成本核算价格版本化设计方案

- 日期：2026-08-08
- 状态：待实现
- 权限级别：仅超级管理员（Root，role >= 100）
- 范围：后端（Go）+ 新前端（web/default）+ 老前端（web/classic）

---

## 1. 问题定义

现有成本核算存在两个正确性 bug：

### Bug 1：供应商改价后历史成本被重算

`cost_cny = list_usd × effectiveChannelRatio(当前配置)`（`cost_stat.go:302,375`）。
渠道倍率只有一个当前值存在 `ChannelSettings` 里，无时间维度。改价后，**全部历史区间成本按新价重算**，已发出的报表不可复现，历史利润数据失真。

`discount` 模式还要乘筛选栏的查询汇率（操作员可随时修改），加剧了不可复现性。

### Bug 2：用户折扣列查的是当前配置，不是历史值

`resolveUserDiscount()` 查当前 `ratio_setting` 配置（`cost_stat.go:406`）。折扣一改，报表折扣列立刻跳成新值。实际上日志 `other` 里早已存着请求当时的 `group_ratio`/`user_group_ratio`，历史值已经在库里，只是没有被读取。

---

## 2. 设计决策（已与用户确认）

| 决策点 | 结论 |
|---|---|
| 历史成本口径 | 按日志发生当时的渠道价分段算，改价 = 追加版本，历史不动 |
| 改价粒度 | 渠道级（整体倍率/折扣），不做逐模型覆盖 |
| discount 模式汇率 | 汇率随版本冻结，不再跟查询汇率浮动 |
| 用户折扣列 | 从日志 `other` 取历史值，删除 `resolveUserDiscount` 配置查询链路 |
| 历史日志迁移 | 把当前值当作「自古以来」的初始版本（`effective_from = 0`） |
| 本次范围 | 只修正确性，汇总表留作后续独立 spec |
| 版本管理入口 | 仅在成本页供应商维度的倍率弹窗里（渠道编辑页只改当前价） |
| 退款定价时刻 | 按退款日志自身 `CreatedAt` 定版本（跨版本概率极低，不引入复杂度） |

---

## 3. 数据模型

### 3.1 新表 `channel_cost_versions`（主库）

```go
// model/channel_cost_version.go
type ChannelCostVersion struct {
    Id            int     `json:"id"             gorm:"primaryKey"`
    ChannelId     int     `json:"channel_id"     gorm:"index:idx_channel_effective,priority:1;not null"`
    EffectiveFrom int64   `json:"effective_from" gorm:"index:idx_channel_effective,priority:2;not null"`
    // EffectiveFrom 为秒级 Unix 时间戳，闭区间起点。0 = 初始版本（自古以来）。
    // 版本区间 = [effective_from, 下一个版本的 effective_from)。
    CostMode      string  `json:"cost_mode"      gorm:"type:varchar(16)"`
    // CostMode: "ratio" | "discount"（空值兼容旧数据，等同 ratio）
    CostRatio     float64 `json:"cost_ratio"`
    // ratio 模式：CNY:USD 倍率（如 2.5 表示每消耗刊例 $1 记成本 ¥2.50）
    CostDiscount  float64 `json:"cost_discount"`
    // discount 模式：刊例折扣（如 0.8）
    ExchangeRate  float64 `json:"exchange_rate"`
    // discount 模式的冻结结算汇率（建版本时取快照，不随查询汇率浮动）
    Note          string  `json:"note"           gorm:"type:varchar(255)"`
    // 备注，如「8月起谈到 2.3 的采购价」
    CreatedAt     int64   `json:"created_at"`
    CreatedBy     int     `json:"created_by"`
    // 操作者 user_id，审计用
}
```

**关键设计**：

- 单点 `effective_from`，不存 `effective_to`，避免重叠/空隙一致性问题。查找时取 `effective_from <= log.CreatedAt` 的最大值（降序第一条）。
- 版本行不可变，改价只追加。误填的版本可通过 DELETE 删除后重加。
- `EffectiveFrom = 0` 专供迁移回填使用，POST API 禁止新建 `effective_from = 0` 的版本。
- 索引 `(channel_id, effective_from)` 覆盖唯一查询模式。
- 三库兼容：纯 GORM，无原生 SQL。

### 3.2 ChannelSettings 保留（降级为「当前价」）

`dto.ChannelSettings` 的 `CostRatio`/`CostMode`/`CostDiscount` 保留，语义降级为「当前价」：

- 渠道编辑页照常编辑这三个字段
- 后端 `UpdateChannel` 时，若值有变化则自动追加 `effective_from = now` 的版本
- 字段说明在 UI 上更新为「保存后自动记录为当前生效价，历史区间仍按原价核算」

`SubSuppliers`/`IsAggregator` 等非价格配置不受影响。

### 3.3 迁移（幂等）

`InitDB()` 里 `AutoMigrate` 加 `&ChannelCostVersion{}`，随后执行一次性回填：

```
对每个渠道：
  若版本表中该渠道尚无任何记录 AND ChannelSettings 里有非零成本配置：
    插入一条 effective_from = 0 的初始版本
    ExchangeRate 取当时的 operation_setting.USDExchangeRate
```

幂等：下次启动检测到已有记录则跳过。

---

## 4. 后端改造

### 4.1 版本查找（`model/channel_cost_version.go`）

```go
// GetAllChannelCostVersions 一次性载入全部渠道的版本列表（主库）。
// 返回 map[channelId] → []ChannelCostVersion（按 effective_from 升序）。
func GetAllChannelCostVersions() (map[int][]ChannelCostVersion, error)

// VersionMap 提供 O(log n) 的版本查找，供 addBatch 热路径使用。
type VersionMap map[int][]ChannelCostVersion

// RatioAt 返回 channelId 在 ts 时刻生效的有效倍率（CNY:USD）。
// discount 模式：返回 CostDiscount × 版本自带 ExchangeRate（不碰查询汇率）。
// 无版本 / 版本值为 0：返回 0, false（调用方按未定价处理）。
func (v VersionMap) RatioAt(channelId int, ts int64) (ratio float64, ok bool)
```

### 4.2 `costCubeRow` 改造（`controller/cost_stat.go`）

**增加字段**：

```go
type costCubeRow struct {
    Quota               float64
    ListQuota           float64
    RefundQuota         float64
    CostCny             float64  // 新增：逐条定价后累加，fold 时直接加和
    UnpricedListQuota   float64  // 新增：无版本可用的刊例敞口
    PromptTokens        int
    CompletionTokens    int
    RequestCount        int
    ErrorCount          int
    CacheReadTokens     int
    CacheCreationTokens int
    FrtSumMs            float64
    FrtCount            int
    // 折扣信号（替换 QuotaByGroup）
    DiscountWeightedSum float64  // Σ(历史折扣 × list_quota)
    DiscountListBasis   float64  // 有有效折扣信息的 list_quota 之和
    DiscountSpecialSum  float64  // 命中专属倍率的 list_quota 之和
    DiscountFirstRatio  float64  // 第一个出现的折扣值（判断 mixed 用）
    DiscountMixed       bool     // 区间内出现过 >1 个不同折扣值（避免 float64 map key 精度问题）
}
```

**`addBatch()` 改造核心**：

```go
// 在 addBatch 内，取代原先的 ListQuota 累加 + 末尾统一乘倍率
listQ := logListQuota(log, info)
ratio, priced := versions.RatioAt(k.ChannelId, log.CreatedAt)
if priced && ratio > 0 {
    row.CostCny += listQ / common.QuotaPerUnit * ratio
} else {
    row.UnpricedListQuota += listQ
}

// 用户折扣：从日志 other 取历史值
histDiscount := historicalDiscount(info)  // UserGroupRatio 有效则取，否则 GroupRatio
if histDiscount > 0 {
    row.DiscountWeightedSum += histDiscount * listQ
    row.DiscountListBasis   += listQ
    if isSpecialRatio(info) { row.DiscountSpecialSum += listQ }
    row.DiscountRatioSeen[histDiscount] = struct{}{}
}
```

退款行：同样调用 `versions.RatioAt(channelId, refundLog.CreatedAt)`，CostCny 取负。

### 4.3 `costMoneyFromRow()` 改造

去掉 `ratio` 参数，直接用 `r.CostCny`：

```go
func costMoneyFromRow(r *costCubeRow, exchangeRate float64) costMoney {
    m := costMoney{
        RevenueUsd: roundTo6(r.Quota / common.QuotaPerUnit),
        ListUsd:    roundTo6(r.ListQuota / common.QuotaPerUnit),
        CostCny:    roundTo6(r.CostCny),   // 直接取，无需乘倍率
        ...
    }
    ...
}
```

`add()` 的 `CostCny` 字段变成纯加法，正确。

### 4.4 `costMoney` 新增字段

```go
type costMoney struct {
    ...原有字段不变...
    // 有效倍率（加权真实值）= CostCny / ListUsd；跨版本时为加权均值
    EffectiveRatio      float64 `json:"effective_ratio"`
    EffectiveRatioKnown bool    `json:"effective_ratio_known"`
    RatioMixed          bool    `json:"ratio_mixed"`  // 区间内改过价（>1 个版本生效）

    // 用户折扣（替换 GroupRatio*/EffectiveDiscount* 中的配置查询部分）
    // EffectiveDiscount 保留（收入÷刊例），含义不变
    DiscountMixed    bool    `json:"discount_mixed"`    // 区间内折扣变更过
    DiscountSpecial  bool    `json:"discount_special"`  // 命中了专属倍率
    DiscountCoverage float64 `json:"discount_coverage"` // 有折扣信息的额度占比
}
```

`deriveRates()` 内从 `costCubeRow` 派生以上字段。

### 4.5 删除的代码

- `costCubeRow.QuotaByGroup` 及其相关操作（`addGroupQuota`/`mergeGroupQuota`）
- `resolveUserDiscount()` 及 `userDiscount` 类型
- `attachUserGroupRatios()` 及相关调用
- `model.GetAllUserGroups()`（无其他调用方）
- `foldCostCube` 里的 `userSets`/`userGroups` 参数
- `buildCostCube` 里的 `GetAllUserGroups()` 调用
- `costDimensionRow`/`costBreakdownRow` 里的 `UserGroup/GroupRatio/GroupRatioKnown/GroupRatioSpecial/GroupRatioMixed/UsingGroupQuota`

`costCubeCacheEntry` 去掉 `userGroups` 字段，加入 `versions VersionMap`。

### 4.6 `foldCostCube` 改造

- `effectiveChannelRatio` 函数删除（倍率已在 addBatch 消费）
- `Priced` 判定改为 `row.UnpricedListQuota == 0`（而非当前值 > 0）
- `EffectiveRatio`/`RatioMixed` 从聚合后的 `CostCny/ListUsd` 派生
- 渠道行的 `CostRatio/CostMode/CostDiscount` 改为取**区间末尾版本**（`versions.RatioAt(channelId, end)`），供 hover 对照用

### 4.7 新增版本管理 API

```
GET    /api/cost/channels/:id/versions
    → []ChannelCostVersion（降序）

POST   /api/cost/channels/:id/versions
    body: {effective_from, cost_mode, cost_ratio, cost_discount, exchange_rate, note}
    校验：effective_from 不得为 0（该值保留给迁移回填的基线版本）；
         cost_mode 只接受空/ratio/discount；对应模式的价必须 > 0；
         渠道必须存在；同渠道同 effective_from 已存在时拒绝；
         note 上限 255 字符（与列宽对齐，按字符数而非字节数算）。
    discount 模式：汇率非正时拒绝写入，不兜底默认值——把 0 冻进不可变的版本行会让
         该版本覆盖的日志永久算不出成本，且只能靠再追加一版绕过。

DELETE /api/cost/versions/:vid
    幂等（目标已不存在时返回成功）。两种拒绝：
      - effective_from=0 的基线版本不可删：它覆盖「自古以来到下一版本」的全部日志，
        删除后那段区间永久无价，且补回路径全部堵死（POST 硬拒 0、自动追版本只在零
        版本时用 0、重启回填按「是否存在任意版本」跳过、版本行不可更新）。
      - 渠道仅剩一条版本时不可删。
    校验与删除在同一事务内并对该渠道的版本行加行锁（SQLite 除外，其写事务本身互斥）：
    事务只给原子性，不给互斥，裸 COUNT 下两个并发 DELETE 能同时通过计数校验。
```

**错误返回口径**：全部走项目统一信封（HTTP 200 + `{success:false, message}`，见
`common.ApiErrorMsg`），不使用 4xx 状态码。前端各处已按 `success` 字段判定，单独为这
几个接口改用 HTTP 语义会让它成为全站唯一的例外。

渠道编辑页保存时（`UpdateChannel`）：若 `CostRatio`/`CostMode`/`CostDiscount` 有变化，后端自动 `POST /api/cost/channels/:id/versions` 追加 `effective_from = now`，无需前端感知。

### 4.8 缓存键与失效

`costCubeCacheKey` 不需改动（筛选栏查询汇率不再影响成本；版本数据随 `VersionMap` 整体进缓存，不参与键）。

但正因为版本不进键，**版本一旦变化必须整体清空 `costCubeCache`**（`costCubeCacheClear`）。
缓存里的 `cost_cny` 是用当时那份 `VersionMap` 逐条日志算出来的，一条新版本行会改写它
覆盖区间内所有日志的成本，而键里没有版本指纹，无法判断哪些条目受影响——补录一条 6 月
的历史价甚至会改到「上半年」这类早已缓存的区间。

不清的后果不止「晚 60 秒更新」：「改完价立刻看报表」正是这个功能最自然的操作顺序，管理员
会看到旧价、以为没生效，于是重复追加版本。四个写入点全部挂钩：API 创建、API 删除、渠道
编辑自动追版本、新建渠道自动追版本（后两者合并在 `appendCostVersionIfChanged` 成功后统一清）。

### 4.9 测试

新增单元测试：

- `VersionMap.RatioAt`：无版本、单版本、跨版本（改价前后都命中）、discount 模式（折扣×冻结汇率）
- `addBatch` 定价路径：改价前后日志各一批，成本分别按对应版本算
- `deriveRates` 新字段：`EffectiveRatio`/`RatioMixed`/`DiscountMixed`/`DiscountCoverage`
- 三维度一致性：Σ用户成本 = Σ模型成本 = Σ渠道成本（改价不影响等式）
- `Priced` 改判定后：上线前无版本的渠道 `Priced=false`，有版本且值>0 的 `Priced=true`
- 迁移幂等：`seedChannelCostVersions` 重复调用不插重复行

---

## 5. 前端改造

### 5.1 类型更新（`web/default/src/features/cost/types.ts`）

```ts
// 删除：user_group, group_ratio, group_ratio_known, group_ratio_special, group_ratio_mixed
// 新增：
ratio_mixed?: boolean         // 区间内改过价
discount_mixed?: boolean      // 区间内折扣变更过
discount_special?: boolean    // 命中专属倍率
discount_coverage?: number    // 有折扣信息的额度占比（0-1）
```

### 5.2 `UserDiscountCell` 改造

hover 内容重写：

- 移除「当前配置对照」和 `drifted` 警告（数据源已删）
- 保留：实际折扣（`effective_discount`）主显
- 新增：
  - `discount_mixed=true` → 「区间内折扣有变更」提示（橙色）
  - `discount_special=true` → 「命中专属倍率」说明
  - `discount_coverage < 0.99` → 「部分日志缺定价信息，折扣仅含已知部分」提示
- `ListUsd=0` 时主显「—」（免费/未定价，分母为零）

### 5.3 供应商倍率列与 `EditRatioDialog` 改造

`EditRatioDialog`（`edit-ratio-dialog.tsx`）扩展为两段：

```
┌ 当前价（原有内容，不变）─────────────────────────────┐
│ 计价方式 [ratio ▾]  倍率/折扣 [2.50]  [保存]          │
│ ℹ 保存后自动记录为当前生效价，历史区间仍按原价核算      │
├ 价格历史────────────────────────────────────────────┤
│ 日期          计价        值      备注     操作         │
│ 2026-08-01   ratio       2.50    8月调价  [删除]       │
│ 2026-06-01   ratio       2.30    初始     [删除]       │
│ —（初始版本）  ratio       2.30    —       —            │
│                        [+ 补录历史价]                  │
└──────────────────────────────────────────────────────┘
```

倍率列改价展示：

- `ratio_mixed=false`：原有 `configuredPricingLabel` 展示不变
- `ratio_mixed=true`：`≈{加权值} ⓘ`，hover 列出区间内各版本生效时段

### 5.4 筛选栏

「汇率」标签改为「收入折算汇率」，加 tooltip：「用于将美元收入折算成人民币展示，不影响成本核算（成本按建版本时的结算汇率计算）」。

### 5.5 classic 前端（`web/classic`）

- `CostUserCells.jsx`：`UserDiscountCell` 同步改，移除 `drifted` 警告，加 `discount_mixed`/`discount_special`/`discount_coverage` 三条提示
- `CostTables.jsx`：倍率列加 `ratio_mixed` 展示（Semi Tooltip）
- `costMerge.js`：`CostCny` 字段变成直接加和，去掉 `ratio` 相关的重算逻辑
- 版本管理：`EditChannelModal.jsx` 不改（只管当前价）；版本历史仅在成本页 Semi Modal 里，参考 `EditRatioDialog` 的新设计实现

### 5.6 i18n

两套前端各补约 12 个键，包括：
- `"Version history"` / `"Price effective from"` / `"Add historical price"`
- `"Revenue exchange rate"` / `"Does not affect cost accounting"`
- `"Discount changed during this range"` / `"Dedicated ratio applied"` / `"Partial discount coverage"`
- `"Cost ratio changed during this range"` / `"Weighted across price versions"`

---

## 6. 实施里程碑

1. **M1 后端**：新表 + 迁移 + `VersionMap.RatioAt` + `addBatch` 改造 + 删除配置查询链路 + 版本管理 API + 测试（TDD）
2. **M2 新前端**：类型更新 + `EditRatioDialog` 版本历史 + 倍率列 `ratio_mixed` + `UserDiscountCell` 改写 + 筛选栏标签 + i18n
3. **M3 老前端**：classic 同步以上前端改动

---

## 7. 规则遵从

- JSON 一律 `common.Marshal/Unmarshal`（Rule 1）
- `ChannelCostVersion` 纯 GORM，三库兼容（Rule 2）；`varchar(16)`/`varchar(255)` 在三库均受支持
- 无原始 SQL，无保留字列名
- Rule 6（上游转发 DTO 指针语义）不涉及：版本表不参与 relay 路径
