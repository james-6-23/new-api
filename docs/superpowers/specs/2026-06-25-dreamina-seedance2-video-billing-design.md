# Dreamina Seedance 2.0 海外视频计费优化设计

日期:2026-06-25
范围:渠道类型「字节火山豆包通用协议」(`ChannelTypeVolcEngine` 45 / `ChannelTypeDoubaoVideo` 54 → `relay/channel/task/doubao` 适配器)
仅针对三个海外视频模型:
- `dreamina-seedance-2-0-260128`
- `dreamina-seedance-2-0-fast-260128`
- `dreamina-seedance-2-0-mini-260615`

---

## Context(为什么做这件事)

当前豆包视频适配器(`relay/channel/task/doubao`)对 Seedance 2.0 的计费有三个问题:

1. **缺模型**:`ModelList` 只有国内 `volces` 命名的 `doubao-seedance-2-0-260128` / `-fast-260128`,没有文档里的海外 `dreamina-*` 命名,也没有 `mini-260615`。
2. **定价不准**:现用「两个独立粗略倍率相乘」(`video_input` 28/46、`resolution` 51/46),只是对官方二维矩阵的近似;且没有 4k 档、没有 mini、`fast` 没有视频折扣。官方矩阵里「含/不含视频」的折扣在不同分辨率档位下并不相同,相乘会产生误差。
3. **价格不透明**:使用记录里看不到"这次视频是按什么单价、多少 token、什么分组倍率算出来的",用户无法自行核价。

官方定价(`接口文档/海外byteplus-seedance文档/模型计费定价.md`)本质是一张二维矩阵:**单价(USD/百万 token) = f(模型, 分辨率档位, 是否含视频输入)**。计费按 token(`usage.completion_tokens`)结算,token 数已编码分辨率/时长/帧率以及文档所述「最低 token 下限」,因此只需选对单价格子并在日志中讲清楚即可。

**目标产出**:三个模型按官方矩阵精确计费;使用记录里展示明确的计费公式(单价 × token ÷ 1e6 × 分组倍率 = 费用),用户能算清价格。

---

## 计费引擎与换算(沿用现有,不改)

- `common.QuotaPerUnit = 500000`,即 `1 USD = 500000 quota`。
- 前端既有约定:`USD/百万 token = model_ratio × 2`。
- 任务两阶段计费(均已存在):
  - **提交阶段**(`relay/relay_task.go` → `controller/relay.go`):预扣估算额度,持久化 `TaskBillingContext{ModelRatio, GroupRatio, OtherRatios}` 快照,写一条预扣消费日志(`service.LogTaskConsumption`)。
  - **轮询完成**(`service/task_billing.go: RecalculateTaskQuotaByTokens`):`actualQuota = completion_tokens × modelRatio × groupRatio × ∏(OtherRatios)`,与预扣做差额结算,写结算/退款日志(`taskBillingOther` 构建 `Other`)。

**最终费用公式(不变)**:
`USD = completion_tokens × modelRatio × groupRatio × video_pricing ÷ 500000`

---

## 定价矩阵(官方,USD / 百万 token,格式 `不含视频 / 含视频`)

| 模型 | 480p/720p | 1080p | 4k |
|---|---|---|---|
| `dreamina-seedance-2-0-260128` | 7.0 / 4.3 | 7.7 / 4.7 | 4.0 / 2.4 |
| `dreamina-seedance-2-0-fast-260128` | 5.6 / 3.3 | 不支持 | 不支持 |
| `dreamina-seedance-2-0-mini-260615` | 3.5 / 2.1 | 不支持 | 不支持 |

注:`fast` / `mini` 单价不随分辨率变化(只分含/不含视频);`260128` 随分辨率档位与含/不含视频两个维度变化。`4k` 单价反而更低(因为 token 消耗更高)。

### 基准 = 「不含视频 + 基础分辨率(480p/720p)」格子

管理员把这个基准设为 `modelRatio`(= 基准单价 ÷ 2)。推荐默认值:

| 模型 | 基准单价 USD/M | 推荐默认 `modelRatio` |
|---|---|---|
| `dreamina-seedance-2-0-260128` | 7.0 | 3.5 |
| `dreamina-seedance-2-0-fast-260128` | 5.6 | 2.8 |
| `dreamina-seedance-2-0-mini-260615` | 3.5 | 1.75 |

### 每格相对基准的合并倍率 → `OtherRatios["video_pricing"]`

`video_pricing = 单元格单价 ÷ 基准单价`(单一精确倍率,取代现有两倍率相乘)。

`260128`:

| 档位 | 不含视频 | 含视频 |
|---|---|---|
| 480p/720p | 1.0 | 4.3/7.0 = 0.614286 |
| 1080p | 7.7/7.0 = 1.1 | 4.7/7.0 = 0.671429 |
| 4k | 4.0/7.0 = 0.571429 | 2.4/7.0 = 0.342857 |

`fast`:不含 1.0,含视频 3.3/5.6 = 0.589286
`mini`:不含 1.0,含视频 2.1/3.5 = 0.6

代码里用精确分数(`4.3/7.0` 等)书写,自带文档性,与既有 `seedance2.go` 风格一致。

**核验**:`260128` 1080p 含视频有效单价 = `modelRatio×2×0.671429 = 3.5×2×0.671429 = 4.7 ✓`。

---

## 组件设计

### 1. 新增定价模块(单一事实来源)
新文件 `relay/channel/task/doubao/seedance2_pricing.go`:
- `dreaminaUnitPrice map[string]map[resTier]map[bool]float64` —— 官方 USD/M 矩阵(以分数写)。
- `IsDreaminaSeedance2(model string) bool` —— 命中三个模型名。
- `DreaminaVideoBilling(c, info) (ratio float64, tier string, hasVideo bool, baseUnitUSD float64, ok bool)`:
  - 复用现有探测:视频输入用 `hasVideoInMetadata`(`doubao/adaptor.go` 已有)+ raw body 兜底;分辨率档位扩展现有 `is1080pString` 逻辑,新增 `4k`/`2160p`/`3840x2160` 与 `720p`/`480p` 识别,归一到 `{base, 1080p, 4k}`。
  - `fast`/`mini` 不支持的档位回退到 `base`。
  - 返回 `ratio = cellUnit / baseUnit`,以及展示用 `tier`、`hasVideo`、`baseUnitUSD`。

### 2. `relay/channel/task/doubao/constants.go`
- `ModelList` 追加三个 `dreamina-*` 模型(保留现有 `doubao-*` 不动)。
- 现有 `videoInputRatioMap` / `GetVideoInputRatio` 保持不变,仅服务旧 `doubao-*`。

### 3. `relay/channel/task/doubao/adaptor.go` —— `EstimateBilling`
分支:
```
if IsDreaminaSeedance2(info.OriginModelName) {
    ratio, tier, hasVideo, baseUnit, ok := DreaminaVideoBilling(c, info)
    if ok {
        if ratio != 1.0 { ratios["video_pricing"] = ratio }
        // 展示字段挂到 info.PriceData,供两处日志使用
        info.PriceData.VideoBilling = &types.VideoBillingDisplay{
            ResolutionTier: tier, HasVideoInput: hasVideo,
            BaseUnitUSDPerM: baseUnit, PricingRatio: ratio,
        }
    }
    return ratios
}
// 旧 doubao-* 走原 GetVideoInputRatio 路径
```

### 4. `types/price_data.go`
新增展示用结构(仅内部使用,不参与上游 marshal,故无需指针约束 Rule 6):
```
type VideoBillingDisplay struct {
    ResolutionTier  string
    HasVideoInput   bool
    BaseUnitUSDPerM float64
    PricingRatio    float64
}
```
`PriceData` 增加字段 `VideoBilling *VideoBillingDisplay`。

### 5. `model/task.go` —— `TaskBillingContext`
新增 `VideoBilling *VideoBillingDisplay`(同结构,复用 types 包),让轮询结算阶段也能渲染。

### 6. `controller/relay.go`(约 584 行)
组装 `TaskBillingContext` 时把 `relayInfo.PriceData.VideoBilling` 拷入。

### 7. `service/task_billing.go`
- `taskBillingOther(task)`:若 `bc.VideoBilling != nil`,写入
  `video_resolution_tier`、`video_has_input`、`video_unit_price`(= `modelRatio×2×PricingRatio`,含管理员加价的有效单价)。
- `RecalculateTaskQuota(...)`:新增可选入参 `extraOther map[string]interface{}` 合并进 `Other`;`RecalculateTaskQuotaByTokens` 调用时传入 `{"video_tokens": totalTokens}`(因结算日志行不写 `CompletionTokens` 列)。另一调用点(adaptor 调整路径)传 `nil`。
- `LogTaskConsumption(c, info)`:预扣消费日志里同样从 `info.PriceData.VideoBilling` 写入上述展示字段(估算阶段无 token,故不写 `video_tokens`)。

### 8. `setting/ratio_setting/model_ratio.go`
`defaultModelRatio` 追加:
```
"dreamina-seedance-2-0-260128":      3.5,
"dreamina-seedance-2-0-fast-260128": 2.8,
"dreamina-seedance-2-0-mini-260615": 1.75,
```

### 9. 前端
- `web/default/src/features/usage-logs/types.ts`:`LogOtherData` 增加
  `video_resolution_tier?: string`、`video_has_input?: boolean`、`video_unit_price?: number`、`video_tokens?: number`。
- `web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx`:
  新增「视频计费 / Video Pricing」`DetailSection`(当 `other.video_unit_price != null` 时显示),逐行展示:
  - 分辨率档位、是否含视频输入
  - 单价:`$U/M tokens`
  - Token 数:`video_tokens`(或 `log.completion_tokens` 兜底)
  - 分组倍率:`G×`
  - **计费公式**:`$U/M × N tokens ÷ 1,000,000 × G = $cost`
  - 实际扣费:`formatLogQuota(log.quota)`(权威值)
- i18n:在 `web/default/src/i18n/locales/zh.json` / `en.json` 增加新标签键(键为英文源串)。

---

## 取舍与已知限制

- **分辨率档位取提交时请求值**(存进 `BillingContext`),复用现有 `RecalculateTaskQuotaByTokens` 结算路径,改动面最小。上游 `responseTask.Resolution` 虽返回真实分辨率,但结算路径不便拿到;若用户不传分辨率而上游用了非默认档位,可能产生档位单价偏差。作为**可选后续增强**:在 doubao 的 `AdjustBillingOnComplete` 里用上游返回分辨率重算 `video_pricing`,本次不纳入范围(待确认)。
- **最低 token 下限**(文档对 260128/fast 含视频场景)无需自行实现:上游返回的 `completion_tokens` 已包含该下限。
- **`sora` 适配器**(OpenAI `/v1/videos` 风格,`relay/channel/task/sora/seedance2.go`)有一套并行的 seedance2 计费,**本次不动**;如需双适配器对齐为另一独立任务。
- 保留 `doubao-*` 旧命名与其原计费路径不变(符合需求「只做三个 dreamina 模型」)。

---

## 验证方案

1. **单元测试**(Go):为 `DreaminaVideoBilling` 写表驱动测试,覆盖每个模型 × 每个档位 × 含/不含视频,断言 `ratio` 与「有效单价 = modelRatio×2×ratio」等于官方表数值(7.0/4.3/7.7/4.7/4.0/2.4/5.6/3.3/3.5/2.1)。
2. **构建**:`go build ./...` 通过;`go vet ./...` 无新增告警。
3. **端到端**(手动):用渠道类型 45/54 配三个模型,分别提交「无视频输入 720p」「含视频输入 720p」「260128 1080p」「260128 4k」生成任务,轮询完成后核对:
   - 扣费 quota ≈ `completion_tokens × modelRatio × groupRatio × video_pricing ÷ 500000`;
   - 使用记录详情弹窗「视频计费」区块单价、token、分组倍率、公式、实际扣费一致。
4. **前端**:`cd web/default && bun run build` 通过;打开日志详情核对中英文标签与公式渲染。
5. **回归**:旧 `doubao-seedance-2-0-*` 任务计费与日志行为不变。
