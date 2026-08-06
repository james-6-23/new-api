# Sora 端点 doubao-seedance-2-0-mini-260615 含视频输入折扣计费设计

日期:2026-07-12
范围:渠道类型「Sora / OpenAI」(`ChannelTypeSora` / `ChannelTypeOpenAI` → `relay/channel/task/sora` 适配器),仅补齐一个海外视频模型:
- `doubao-seedance-2-0-mini-260615`

---

## Context(为什么做这件事)

`doubao-seedance-2-0-mini-260615` 走 OpenAI `/v1/videos`(sora 端点),但**含视频输入时没有按折扣计费**,被按满价扣费。

根因(两条,均已核实):

1. **路由由渠道类型决定,与模型名无关**(`relay/relay_adaptor.go:155-158`)。该模型配在 Sora/OpenAI 渠道 → 命中 `sora.TaskAdaptor` → 打 `/v1/videos`。sora 适配器的阶梯/折扣计费只对 `seedance2ModelSet` 里的模型生效。
2. **`seedance2ModelSet` 与 `seedance2VideoInputRatio` 里没有 mini**(`relay/channel/task/sora/seedance2.go:10-13, 43-46`),只有 `doubao-seedance-2-0-260128` 与 `-fast-260128`。所以 mini 请求含视频输入时 `EstimateBilling` 不产生任何折扣。

> 注:阶梯定价的完整二维矩阵(base/1080p/4k × 含视频)另有一套,存在于 `doubao` 适配器(`relay/channel/task/doubao`,渠道类型 45/54),键名为 `dreamina-*`,见 `docs/superpowers/specs/2026-06-25-dreamina-seedance2-video-billing-design.md`。该矩阵**本次不动**——mini 走的是 sora 端点,与 doubao 适配器是两条独立链路。

---

## 计费口径(人民币定价惯例)

管理员对整个 `doubao-seedance` 系列按**人民币**定价。系统内部一律以美元计价,人民币仅为显示层换算(`USDExchangeRate`,默认 7.3,`setting/operation_setting/payment_setting_old.go:18`),**不影响内部计费**。

- 有效单价(USD / 百万 token) = `modelRatio × 2`
- 显示人民币单价 = `modelRatio × 2 × USDExchangeRate`
- 结算公式(不变):`USD = completion_tokens × modelRatio × groupRatio × video_input ÷ 500000`

`sora/seedance2.go` 现有的 `video_input` 折扣本质是**人民币两价之比**(含视频 ÷ 不含视频),与汇率无关,这也解释了现有值:

| 模型 | 不含视频 (CNY/M) | 含视频 (CNY/M) | video_input 折扣 |
|---|---|---|---|
| `doubao-seedance-2-0-260128` | 46 | 28 | `28.0 / 46.0`(现有,不动) |
| `doubao-seedance-2-0-fast-260128` | 37 | 22 | `22.0 / 37.0`(现有,不动) |
| **`doubao-seedance-2-0-mini-260615`** | **23** | **14** | **`14.0 / 23.0`(本次新增)** |

mini 官方只有 base 档(480p/720p),**不支持 1080p / 4k**,因此不需要 resolution 折扣条目。

---

## 组件设计

### 唯一代码改动:`relay/channel/task/sora/seedance2.go`

```go
var seedance2ModelSet = map[string]struct{}{
	"doubao-seedance-2-0-260128":      {},
	"doubao-seedance-2-0-fast-260128": {},
	"doubao-seedance-2-0-mini-260615": {}, // 新增
}

var seedance2VideoInputRatio = map[string]float64{
	"doubao-seedance-2-0-260128":      28.0 / 46.0,
	"doubao-seedance-2-0-fast-260128": 22.0 / 37.0,
	"doubao-seedance-2-0-mini-260615": 14.0 / 23.0, // 新增,= 0.6087,含视频输入折扣
}
// seedance2Resolution1080pRatio 不改 —— mini 不支持 1080p/4k
```

生效链路(全部沿用现有,不新增分支):
`sora/adaptor.go:117 EstimateBilling` → `IsSeedance2Model(mini)` 现在返回 true → `estimateSeedance2Ratios` → 检测到含视频输入时产出 `ratios["video_input"] = 0.6087`;若请求带 1080p,`Get1080pRatio(mini, …)` 因无条目返回 false,自动跳过 → 结算按 `completion_tokens × modelRatio × groupRatio × 0.6087 ÷ 500000`。

### 不改动的部分(刻意)

- **`sora/constants.go` 的 `ModelList`**:仍为 `sora-2` / `sora-2-pro`。260128/fast 也不在其中——模型靠管理员在渠道里手动添加、按渠道类型路由,`ModelList` 不是网关。
- **`setting/ratio_setting/model_ratio.go`**:**不加** `doubao-seedance-2-0-mini-260615` 的代码默认 `modelRatio`,与现有 260128/fast 保持一致(它们也没有代码默认),基准价由管理员在后台配置。
- **`doubao` 适配器与 `dreamina-*` 矩阵**:与本链路无关,不动。

### 管理员侧配置(不属于本次代码,记录以便验收)

后台给 `doubao-seedance-2-0-mini-260615` 配 `modelRatio`:
`modelRatio = 不含视频人民币单价 ÷ 2 ÷ USDExchangeRate`
汇率 7.3 时:`23 ÷ 2 ÷ 7.3 ≈ 1.5753`。
生效后:不含视频显示 23 CNY/M,含视频 `× 14/23` = 14 CNY/M。

---

## 验证方案

1. **单元测试**(Go,`relay/channel/task/sora/`,表驱动):
   - mini + 含视频输入 → `ratios["video_input"] == 14.0/23.0`
   - mini + 无视频输入 → 返回 nil(无任何 ratio)
   - mini + 请求 1080p(含/不含视频)→ 不产生 `resolution` ratio(mini 不支持)
   - 回归:`260128` / `fast` 现有 `video_input`、`260128` 的 1080p ratio 完全不变
2. **构建**:`go build ./...` 通过;`go vet ./relay/channel/task/sora/...` 无新增告警。
3. **端到端**(手动):Sora/OpenAI 渠道配 `doubao-seedance-2-0-mini-260615`,后台配 `modelRatio≈1.5753`;分别提交「无视频输入」与「含视频输入」生成任务,轮询完成后核对扣费 quota ≈ `completion_tokens × modelRatio × groupRatio × video_input ÷ 500000`,且含视频显示价为 14 CNY/M。

---

## 取舍与后续增强

- **本次选择硬编码(方案 B)**:改动最小(一个文件两行 + 测试),与现有 260128/fast 同构。
- **已评估但本次不做的可配置化(方案 A1)**:把 `video_input` 折扣接入 `setting/ratio_setting` 的既有可配置机制(仿 `imageRatio`:`defaultVideoInputRatio` + `videoInputRatioMap` + `GetVideoInputRatio` + `UpdateVideoInputRatioByJSONString` + 前端 JSON 编辑器 + `model/option.go` 注册),使以后加 seedance 模型仅需后台改 JSON、零代码改动。如未来加模型频繁,可作为独立任务落地,`IsSeedance2Model` 届时改为「配置里是否存在该模型的 video_input 条目」。
- **1080p / 4k 折扣**:mini 不支持,无需处理;若未来有支持多分辨率档的新模型走 sora 端点,再按现有 `seedance2Resolution1080pRatio` 模式扩展。
