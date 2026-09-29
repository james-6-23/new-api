# Seedance 限时折扣计费层 + 新模型接入 Skill 设计

日期:2026-09-11
范围:`relay/channel/task/seedance` 计费矩阵(三个渠道类型共用:`ChannelTypeVolcEngine` 45 / `ChannelTypeDoubaoVideo` 54 / `ChannelTypeSeedance3rd` 59)

---

## Context(为什么做这件事)

两个诉求:

1. **Seedance 2.5 需要 1080p 档位与限时折扣**。矩阵里 `doubao-seedance-2-5-260628`(`relay/channel/task/seedance/pricing.go:55`)与 `dreamina-seedance-2-5-260628`(`:36`)已存在,base 档单价(国内 70/42、海外 10.7/6.4)与官方一致,但两者都只有 base 档,且代码注释明确写着「2.5 仅支持输出 480p/720p(无 1080p/4k 档)」(`pricing.go:35`、`:53`)。新官方定价给出了 1080p 档,并对该档位施加限时折扣(国内 72折 / 海外 28% off,系数均为 0.72),与现有注释冲突。

2. **每次出新模型都要改代码,希望固化成 Skill**。当前接入一个新 seedance 模型需要人工判断改哪几处,容易漏。目标是让后续接入退化为「给模型名 + 价格」的表格编辑。

3. **折扣与活动起止时间必须可自由配置**。限时折扣是商务决策,变更节奏远快于发版:改一个系数、提前开跑或延期都不该需要开发介入。因此折扣不写死在代码里,而是后台可编辑的配置:每个模型一组**开始/结束时刻(精确到秒)**,窗口内按**档位**分别打折——2.5 只有 1080p 参与,其他档位不受影响。折扣叠加在原价之上,系数由你自行填写。

### 一个现存缺陷,正好是折扣可审计性的障碍

模型没有某档位时,单价静默回退到 base(`pricing.go:87-90`):

```go
cell, has := tiers[tier]
if !has {
    cell = tiers["base"]
}
```

但日志照旧记录请求档位(`service/task_billing.go:52` `other["video_resolution_tier"] = vb.ResolutionTier`)。结果是**账单显示 1080p、实际按 480p/720p 价扣费**。折扣一旦引入,这种偏差会让「原价 × 折扣 = 实收」在账单上算不平,因此本次一并修正。

---

## 计费链路(沿用现有,不改)

矩阵是单一事实来源,三个渠道类型共用,因此「修改所有 seedance 渠道类型」只需改这一处:

- `relay/channel/task/seedance/pricing.go:96` `PricingRatio` 算出 `video_pricing = 单元格单价 ÷ 基准单价`
- `relay/channel/task/seedance/billing.go:15` `EstimateBilling` 注入 `OtherRatios{"video_pricing": ratio}`
- `relay/relay_task.go:197` 预扣阶段乘进额度
- `service/task_billing.go:364` 结算阶段用同一快照系数重算

**折扣天然属于「单元格单价」这一层**,落在该层即自动贯穿预扣与结算,计费管线一行不动。

### 异步任务的折扣一致性(配置化带来的新要求)

视频任务是异步的:提交时预扣,轮询到终态才结算。折扣一旦可随时修改,就必须回答「任务执行期间运营改了折扣,按哪个价结算」。

答案是**按提交时的折扣结算**,且现有机制已经保证这一点:`video_pricing` 在提交时写入 `TaskBillingContext.OtherRatios`(`model/task.go:142`),随 `PrivateData` 持久化到 `tasks.private_data` 列(`model/task.go:85`);结算阶段只读这份快照的乘积(`task_billing.go:354-361`),不重新查配置。

因此本次实现有一条硬约束:**结算路径不得重新读取折扣配置**。用户按下单时看到的价格付费,中途调价不影响在途任务——这既是正确的商业语义,也避免了对账争议。需为此补一条回归测试。

最终费用公式不变:
`USD/元 = completion_tokens × modelRatio × groupRatio × video_pricing ÷ QuotaPerUnit`

---

## 一、数据模型:原价在代码,折扣在配置

两类数据的变更节奏完全不同,因此分置:

| 数据 | 位置 | 谁改 | 为何 |
|---|---|---|---|
| **原价矩阵**(档位 × 含视频) | 代码 `pricing.go` | 开发,随上游公告 | 官方定价,新档位往往同时需要 `ClassifyResTier` 支持,天然伴随发版 |
| **限时折扣**(档位系数 + 起止时刻) | 后台配置 | 运营,随时 | 商务决策,需要自由调整,不应为改一个数字发版 |

### 原价矩阵

保留代码内定义,仅去掉折扣相关的假设:

```go
// 原价:档位 → 是否含视频 → 单价(每百万 token)
var unitPrice = map[string]map[string]map[bool]float64{ ... }
```

**关键约束:矩阵里存原价,折后价永不写入。** 折扣一律来自配置层,「原价 / 折扣 / 实收」因此始终可分离。

### 折扣配置

新建 `setting/billing_setting/video_promo.go`,沿用本仓库唯一的配置注册模式(对照 `tiered_billing.go:31`):

```go
// DB key: billing_setting.video_promo
type VideoPromo struct {
    // Factors 计价档位 → 折扣系数,如 {"1080p": 0.72}
    // 键必须是该模型在原价矩阵中真实存在的档位;未列出的档位不打折。
    Factors map[string]float64 `json:"factors"`

    // 活动窗口,Unix 秒(精确到秒)。判定为闭区间 StartAt <= now <= EndAt。
    // Factors 非空时两者都必须 > 0,详见「窗口判定」。
    StartAt int64 `json:"start_at"`
    EndAt   int64 `json:"end_at"`
}

type BillingSetting struct {
    // ...existing fields...
    VideoPromo map[string]VideoPromo `json:"video_promo"` // 模型名 → 活动
}
```

用 `map[档位]系数` 而非「档位列表 + 单一系数」,因为四个模型的**作用档位与系数各不相同**:2.5 只折 1080p、其他档不折,fast/mini 折 base。map 同时表达「哪些档位」和「各自多少」两个维度,不需要后续改结构。

**活动窗口按模型独立。** 每个模型一组 `StartAt`/`EndAt`,四个模型的活动可以各自起止、互不重叠、也可以只开其中一个。窗口是模型级而非档位级:同一模型的多个档位若需要不同起止时间,那是两场活动,当前不支持——真要如此再把窗口下移到档位,届时 `Factors` 的值从 `float64` 变结构体即可,不影响其余设计。

挂在既有 `billing_setting` 之下而非新建 package:该配置已在 `model/option.go:622-634` 的写后钩子里绑定了 `InvalidatePricingCache()` 与 `ratio_setting.InvalidateExposedDataCache()`,复用即自动获得缓存失效。

`setting/config/config.go:146-152`(写)与 `:255-268`(读)对任意嵌套结构走 `json.Marshal`/`Unmarshal`,故整张嵌套 map 存于单个 DB key,无需额外序列化代码。`reflect.Map` 字段更新时会重建新 map(`config.go:255-263`),删除的模型键能被正确清除。

读访问器返回 `lo.Assign` 副本,与 `GetBillingModeCopy`(`tiered_billing.go:50`)一致,热路径不共享 map。

### 窗口判定

`StartAt`/`EndAt` 存 **Unix 秒**,与 `common.GetTimestamp()`(`common/utils.go:261`,返回 `time.Now().Unix()`)比较。判定为**闭区间**:

```go
now := clock()                       // 注入,测试不依赖真实时间
active := start > 0 && end > 0 && now >= start && now <= end
```

闭区间与日志过滤(`model/log.go:429-432` 两端均 `>=`/`<=`)一致,不引入第二种边界口径。仓库里 `<` 与 `<=` 混用有先例(`model/token.go:212` 用 `<`、`controller/token.go:293` 用 `<=`),不必跟随这种不一致。

**`0` 不表示「永久有效」,而是「未配置」。** 这与 `Redemption.ExpiredTime`(`model/redemption.go:26`,`0` = 永不过期)相反,是刻意的:限时活动没有「永久」语义,一个填了系数却漏填结束时间的配置是事故而非永久活动。`Factors` 非空但任一端为 0 时,整个模型的折扣不生效并记 warning——宁可少收折扣,不可无限期打折。

时间戳不带时区,管理员在选择器里挑的就是一个确定时刻,不存在「国内按东八区、海外按 UTC」的规则要向客服解释。类型与单位沿用 `ChannelCostVersion.EffectiveFrom`(`model/channel_cost_version.go:22`,`int64` 秒,管理员可设的生效时刻);那里靠版本排序推导区间末端,本方案需要显式 `EndAt`,因为活动结束后是回到原价,而不是切到下一场活动。

### 秒级精度:前端必须新建控件

**仓库现有控件全部做不到秒。** 这是本次改动里唯一需要新写 UI 基础件的地方:

- `components/datetime-picker.tsx:82`、`:101` 用 `setHours(hours, minutes, 0, 0)` 把秒**硬置零**,内部状态是 `'00:00'`(`:66`)
- 其 `<Input type='time'>`(`:144-150`)未设 `step`,浏览器只渲染 HH:mm
- `features/usage-logs/.../compact-date-time-range-picker.tsx:62-65` 的源码注释明写此限制
- 全 `web/default/src` 共 30 处 `step=`,无一落在 time / datetime-local 输入上
- classic 主题的 Semi `type='dateTimeRange'`(如 `UsageLogsFilters.jsx:61-64`)未传 `format`,默认同样是分钟精度

因此扩展 `DateTimePicker` 支持秒:`<Input type='time' step={1}>` 让浏览器显示 HH:mm:ss,并把 `setHours(h, m, s, 0)` 的秒位改为透传。加可选 prop(如 `withSeconds`,默认 `false`)以免影响 `edit-ratio-dialog.tsx:521-526` 等既有调用点的行为。

下游转换已经支持秒,无需改动:`dateToUnixTimestamp`(`lib/time.ts:32`)是 `Math.floor(date.getTime() / 1000)`,秒位如实保留——今天秒总是 0 纯粹是控件所致。仅 `formatTimestampForInput`(`lib/format.ts:192-197`)输出 `'YYYY-MM-DDTHH:mm'`,回填时需要一个带秒的变体。

---

## 二、2.5 的 1080p 档位与折扣

| 模型 | 档位 | 不含视频 | 含视频 |
|---|---|---|---|
| `doubao-seedance-2-5-260628` | 480p/720p (base) | 70.00 | 42.00 |
| | 1080p 原价 | 77.00 | 46.00 |
| | 1080p 折后(×0.72) | 55.44 | 33.12 |
| `dreamina-seedance-2-5-260628` | 480p/720p (base) | 10.70 | 6.40 |
| | 1080p 原价 | 11.70 | 7.00 |
| | 1080p 折后(×0.72) | 8.424 | 5.04 |

国内 72折 与海外 28% off 是同一系数 0.72。**矩阵只填原价那两行**,折后价由配置产生,不写进代码。

`pricing.go:35`、`:53` 的「无 1080p 档」注释随之更正。

### 代码不写任何折扣数字,迁移不写任何初始值

0.72 这个系数**不出现在代码里**,也不由迁移预置。上线时 `video_promo` 为空,四个模型的档位集合、系数、起止时刻全部由你在后台填写,各自独立。

这带来一条比「初始值正确」更强的验收标准:**空配置下系统行为与今天逐字节一致**。折扣纯属叠加层,没配就等于不存在,回归风险因此收敛为一条可断言的测试(见测试章节)。

上表的折后价仅为算例,用于说明「原价 × 系数」的口径,不是要写入的数据。

### fast / mini

矩阵只保留 base 档:两地官方文档均写明 fast/mini 不支持 1080p 输出(`接口文档/海外byteplus-seedance文档/模型计费定价.md:117-118`、`接口文档/seedance2视频文档/Seedance 2.0 计费规则说明.md:34,39`),不为其编造 1080p 价格。

**矩阵现有数字确认为原价**(用户 2026-09-11 确认:「原价然后过期时间之前可以设置折扣」)。因此 37/22(fast)、23/14(mini)、以及 2.0/2.5 各档一律保持不动,不存在重复打折隐患。原先阻塞实施的那项确认到此关闭。

fast/mini 同样参与限时活动,折扣与档位由你按模型分别配置。这暴露了一个必须改的机制缺陷,见下节。

---

## 三、单价求值

`CellUnit` 两项行为变更:

### 1. 显式返回实际计价档位

新增返回值 `tierHit string`。调用方据此把日志的 `video_resolution_tier` 改为记录**真实计价档位**而非请求档位。1080p 请求落到 base 计价时,账单如实显示 base,不再自欺。

`CellUnit` 与 `PricingRatio` 当前均为三返回值(`unit, base, ok` / `ratio, base, ok`),加 `tierHit` 后变四返回值,调用方需同步:`ResolveVideoBilling`(`billing.go:37`)是唯一生产调用点,其余为测试(`pricing_test.go:72`、`:119`)。

### 2. 折扣不进矩阵,而是独立乘子

`CellUnit` / `PricingRatio` **不感知折扣**,继续只返回原价与相对倍率。折扣由新函数解析,作为并列的 `OtherRatios` 键注入:

```go
// billing.go — EstimateBilling
ratios := map[string]float64{}
if ratio != 1.0 {
    ratios["video_pricing"] = ratio                  // 档位倍率,纯原价
}
if f := promo.Resolve(model, tierHit, now); f < 1.0 {
    ratios["video_promo"] = f                        // 折扣,独立键
}
```

匹配用 `tierHit`(实际计价档位)而非请求档位。1080p 请求在只有 base 档的模型上回退到 base 计价,若按请求档位判断,会把 1080p 的折扣施加到 base 单价上。配置由人手填、无迁移校准,这个错配更容易发生,因此匹配基准必须是真实计价档位。

### 配置健壮性

配置由人手填,坏值必须安全降级而非放大扣费或崩溃:

| 情形 | 处理 |
|---|---|
| 系数 `<= 0` 或 `> 1` | 忽略**该档位**折扣,按原价计;记 warning |
| `Factors` 为空或 nil | 无折扣,原价 |
| 档位键不在该模型矩阵中 | 该键不生效(不误伤同模型其他档位) |
| 模型名未出现在配置中 | 无折扣,原价 |
| 窗口未开始 / 已结束 | 无折扣,原价 |
| `StartAt` 或 `EndAt` 为 0 而 `Factors` 非空 | **整个模型**折扣不生效;记 warning |
| `StartAt >= EndAt` | 整个模型折扣不生效;记 warning |

一律向**原价**降级,不放大扣费、不 panic。系数 `> 1` 视为坏值而非加价:此配置项语义是限时优惠,加价应走 `modelRatio`。

注意两种降级粒度的区别:坏系数只废掉那一个档位,坏窗口废掉整个模型。窗口是模型级的,一个说不清起止的活动整场都不该生效。

保存前校验(参照 `controller/option.go:306-341` 的 `case` 分支模式,本仓库唯一的存前校验机制):每个系数落在 `(0, 1]`,档位键属于该模型矩阵的真实档位,`Factors` 非空时 `StartAt > 0 && EndAt > 0 && StartAt < EndAt`。后台直接拒绝坏值,运行时降级只是兜底。

### 为何必须独立成键:base 档折扣会被约掉

这是 fast/mini 参与活动后暴露的机制缺陷,也是本方案相对初版的核心修正。

`PricingRatio` 是**相对倍率**,分母就是 base/无视频那一格(`pricing.go:101`,`base = tiers["base"][false]`)。若把折扣乘进矩阵单价,base 档折扣的分子分母会同时缩放,直接约掉:

```
(37 × 0.72) / (37 × 0.72) = 1.0     // 与不打折完全相同
```

更糟的是 `billing.go:22` 恰在 `ratio == 1.0` 时返回 `nil`,连键都不写;而 `TestBaseRatioIsOne`(`pricing_test.go:129-137`)会把这个 1.0 断言为正确。**折扣静默失效,测试全绿。** 2.5 的 1080p 之所以不受影响,只是因为它折的是分子(77→55.44)、分母 70 不动——「折扣乘进矩阵」仅在非基准档位上碰巧可用。

折扣独立成键后,三处自然对齐,且无需改动任何计费代码:

- 预扣 `relay_task.go:199` 对所有键无差别连乘
- 结算 `task_billing.go:357` 连乘同一份持久化快照
- 两处都没有键白名单,新键自动生效

附带三个好处:base 档折扣不再被约掉(`video_pricing` 保持 1.0 不写,`video_promo` 独立生效);`TestBaseRatioIsOne` 这条不变量无需推翻,因为它本身是对的,只是不该被折扣污染;账单天然可拆(见下节)。

---

## 四、账单与日志

`types.VideoBillingDisplay`(`types/price_data.go:31`)新增三字段:

| 字段 | 含义 |
|---|---|
| `PromoFactor` | 折扣系数;未打折为 1 |
| `PromoStartAt` | 活动开始 Unix 秒;未打折为 0 |
| `PromoEndAt` | 活动结束 Unix 秒;未打折为 0 |

起止都落库,是为了让历史账单**自证**:事后核查一笔扣费时,当时的活动窗口与系数都在日志里,不必去翻「现在的配置」反推——配置随时可改,改完旧账单就解释不清了。这也是把两个时刻都存下来而非只存系数的唯一理由。

既有 `PricingRatio` 语义**保持不变**(纯原价的相对倍率),因此 `video_unit_price = ModelRatio × 2 × PricingRatio`(`task_billing.go:55`)自动仍是**原价**,那行代码不用改,历史日志的含义也不发生漂移。实收价由账单侧乘 `PromoFactor` 导出。

这是折扣独立成键带来的直接收益:初版方案要引入 `ListPricingRatio` 来保住原价,是因为折扣污染了 `PricingRatio`;折扣移出后该字段不再需要。

`service/task_billing.go` 两处写日志处(`:51-59` 预扣、`:147-160` 结算)追加:

```
video_resolution_tier  = "1080p"   // 真实计价档位(tierHit)
video_unit_price       = 77.00     // 原价,已有字段,语义不变
video_promo_factor     = 0.72      // 限时72折
video_net_unit_price   = 55.44     // 实收 = 原价 × 折扣
```

`video_promo` 这个倍率键还会**自动**出现在日志的「计算参数」文本里——`task_billing.go:29-33` 遍历 `OtherRatios` 且跳过 1.0,无需为此写代码。这正是你要的「账单上给他做折扣」:原价与折扣分列,`原价 × 折扣 = 实收` 可当场核对。反之若把折后价塞进单价,账单上只剩一个数字,事后无法证明打了折。

前端详情弹窗(`web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx:406-433`)已在展示 `video_unit_price` 与档位,按同样模式补原价与折扣两行;类型定义在 `types.ts:201-204`、`data/schema.ts:36-37`。

未打折时(`PromoFactor == 1`)不渲染折扣行,避免给全价请求增加噪音。

---

## 五、后台折扣编辑界面

复用既有模型定价表,不新建页面:折扣是模型的属性,和倍率、计费模式同属一处心智模型。

- 每模型一行的表格在 `web/default/src/features/system-settings/models/model-ratio-visual-editor.tsx`,列定义在 `model-ratio-table-columns.tsx`
- 逐模型编辑在 `model-pricing-sheet.tsx`;其中 `tiered-pricing-editor.tsx`(`model-pricing-sheet.tsx:640`)已证明嵌套的档位列表能在该抽屉内编辑,折扣编辑器沿用同一位置与形态
- 保存需在 `ratio-settings-card.tsx:315-318` 的 `apiKeyMap` 中登记 `billing_setting.video_promo`;该文件已有按字段脏检查(`:320-334`),未改动的键不会被写入

每个模型的编辑区两部分:

**活动时间**——开始与结束各一个秒级 `DateTimePicker`(上节的 `withSeconds`),提交为 Unix 秒。就近参照 `edit-ratio-dialog.tsx:521-526` 绑定 `effectiveFrom` 的写法。

**档位系数**——**该模型在原价矩阵中真实存在的档位各占一行**,每行填一个系数,留空即该档不打折。2.5 就是「1080p 填 0.72,base 留空」;fast/mini 只有 base 一行。

档位行由后端按模型给出,不让运营自己输入档位名。这从源头消掉「给只有 base 档的 fast 配 1080p 折扣」这类无效配置——该档位在界面上根本不出现。为此 `pricing.go` 需导出一个按模型列可用档位的查询函数。

每行实时显示 `原价 → 折后价` 预览,避免心算。窗口未开始或已结束时,整块加一条状态提示(未开始 / 进行中 / 已结束),否则运营无法判断「填了系数为何账单没折」。

本仓库没有通用嵌套表单渲染器,每个嵌套配置都是手写编辑器,且均配 JSON 模式作为兜底(最佳参照:`features/system-settings/general/channel-affinity/index.tsx`,visual/json 双模式在 `:146`、`:541`)。折扣编辑器同样提供 JSON 模式,以便批量配置多个模型。

---

## 六、测试

现有测试编码了「2.5 无 1080p」的旧假设,必须随本次变更更新:

| 位置 | 现断言 | 变更后 |
|---|---|---|
| `pricing_test.go:68` | `dreamina` 2.5 `1080p` 含视频 → 6.4(回退 base) | 7.0 原价 / 5.04 折后 |
| `pricing_test.go:114` | `doubao` 2.5 `1080p` 不含视频 → 倍率 1.0 | 77/70 原价 → 折后 0.72×77/70 |
| `pricing_test.go:115` | `doubao` 2.5 `1080p` 含视频 → 42/70 | 46/70 原价 → 折后 0.72×46/70 |

上表**只涉及新增 1080p 档带来的原价变化**,与折扣无关。`:65`、`:69`、`:111`、`:116` 的 `4k` 断言行为不变(仍回退 base),但声明「2.5 不支持 1080p/4k」的注释需更正。

**fast/mini 的现有断言全部不动**(`:61-64`、`:105-110`):矩阵数字确认为原价、折扣纯走配置且默认为空,这些用例的期望值不受影响。同理 `relay/channel/task/sora/seedance2_test.go` 的 14/23 期望值(`:64`、`:73`、`:91`)与 `TestEstimateSeedance2Ratios_Mini1080pNoResolutionRatio`(`:85-95`)也保持不变,仅需随 `tierHit` 新返回值调整调用签名。初版方案里「取决于待补 factor」的不确定性到此消除。

新增覆盖:

- **空配置 = 零行为变化**(最重要的一条):`video_promo` 为空时,所有模型的 `OtherRatios` 与今天逐键一致,不出现 `video_promo` 键。这条把「无初始值」从省事变成了可断言的安全网
- **base 档折扣不被分母约掉** —— 对应第三节。给 fast 配 `{"base": 0.72}`,断言 `OtherRatios["video_promo"] == 0.72` 且最终扣费为原价的 72%。这是初版方案会静默失败的用例,必须先写
- 1080p 档折扣:`video_pricing` 保持原价倍率(77/70),`video_promo` 独立为 0.72,两键相乘等于折后有效倍率
- **窗口边界五点**(注入固定时钟):`start-1s` 不折、`start` 整秒折、区间中折、`end` 整秒折、`end+1s` 不折。闭区间语义靠这五点钉住,尤其是两个端点整秒必须命中
- 秒级精度真实生效:窗口设为 `10:00:00`~`10:00:59`,`10:00:30` 命中、`10:01:00` 不命中(证明配置未被截断到分钟)
- **在途任务不受调价影响**:提交后修改配置,结算仍按快照计费 —— 对应第二节的异步一致性约束
- 坏配置降级:系数为 0 / 负数 / 大于 1,`Factors` 为 nil,档位键不存在于该模型——均按原价计且不 panic
- **窗口缺失不等于永久折扣**:`Factors` 填了但 `StartAt` 或 `EndAt` 为 0,以及 `StartAt >= EndAt`,均按原价计。这条守的是钱,必须显式覆盖
- 降级粒度:同一模型里一个档位系数坏、另一个正常,只有坏的那个失效;窗口坏则整模型失效
- 2.5 的档位隔离:只给 1080p 配系数,base 请求扣费与配置前完全一致
- 折扣按 `tierHit` 而非请求档位判断:1080p 请求落到 base 计价时,配在 1080p 上的折扣不生效
- 四个模型各配不同档位与不同系数,互不串扰(对应「各自独立」的配置语义)
- fast/mini 未配 1080p 时 `tierHit` 如实返回 `base`
- 四个模型的活动窗口互相独立:A 进行中、B 未开始、C 已结束同时成立时,只有 A 打折
- 落库的 `PromoStartAt`/`PromoEndAt` 与命中时的配置一致(历史账单自证)
- `PromoFactor == 1` 时账单不渲染折扣行,且 `video_net_unit_price == video_unit_price`

---

## 七、新模型接入 Skill

折扣配置化后,Skill 的范围缩小了——**折扣不再需要改代码**,新模型接入只剩原价与模型名注册:

1. `relay/channel/task/seedance/pricing.go` 原价矩阵加模型条目
2. `relay/channel/task/{doubao,seedance3rd}/constants.go` 模型列表加名字
3. `dreamina-*` 另加默认倍率(`setting/ratio_setting/model_ratio.go:283`,值 = USD单价 ÷ 2);`doubao-*` 刻意不设默认值,由管理员按基准单价配置为**倍率而非固定价**(否则 `PerCallBilling` 会跳过 token 阶梯结算,见 `model_ratio.go:287-291`)
4. 有限时折扣则在后台配,不动代码

于是「给模型名 + 价格就能接入」基本成立:代码改动是三处机械编辑,商务参数走配置。

Skill 固化这几步为检查单,并附带易踩空的约束:

- 矩阵只填**原价**,折后价永不写入代码;折扣一律后台配置
- fast/mini 类精简型号通常无 1080p/4k 档,不要凭空补价——查官方文档确认
- 改矩阵必须同步更新 `pricing_test.go` **与** `sora/seedance2_test.go`(两个文件都断言倍率)
- 新增档位要确认 `ClassifyResTier`(`pricing.go:68`)能识别该分辨率字符串
- **绝不把折扣乘进矩阵单价**:base 档会因分子分母同时缩放而静默失效,且现有测试不会报错(第三节)
- 新模型名需同时确认走的是 `doubao-*` 还是 `dreamina-*` 前缀,两者默认倍率处理方式不同

**实施顺序:先完成本次改造,再写 Skill。** Skill 应描述一个已验证过的流程,而非设想的流程。

---

## 不做(YAGNI)

- 不把视频计费迁到 `pkg/billingexpr` 表达式系统。两套系统当前互不相通(任务路径的 `ModelPriceHelperPerCall` 无 `GetBillingMode` 分支;`BuildTieredTokenParams` 只读 `dto.Usage`,视频任务不产生 Usage),迁移需新增 resolution/duration 变量、改编译原型与结算入参,远超本次范围。
- 不把**原价矩阵**也搬进后台。原价随上游公告变动,且新档位往往需要 `ClassifyResTier` 同步支持,本就伴随发版;搬进配置只会让代码与配置对同一事实各存一份。可配置的边界划在折扣。
- 不做档位级的独立时间窗口。窗口是模型级的;同一模型不同档位需要不同起止时,视为两场活动,当前不支持(扩展路径见「折扣配置」一节末)。
- 不做活动的历史归档与多场排期。配置里只存**当前**一场活动;已结束的活动改成新窗口即可,旧活动的举证靠日志里落库的 `PromoStartAt`/`PromoEndAt`,而非配置侧的版本链。真需要排期时,`channel_cost_version.go` 的版本表模式可循。
- 不做按分组/令牌差异化折扣。分组倍率已是独立乘子,叠加会让账单更难解释。
- 不改 duration/fps/watermark 的计费参与方式——现状是通过 token 数隐式生效,本次不动。
