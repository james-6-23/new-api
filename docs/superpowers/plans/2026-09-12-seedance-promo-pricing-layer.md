# Seedance 限时折扣计费层 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 seedance 2.5 补上 1080p 原价档位,并新增一个后台可配置、按模型独立、精确到秒的限时折扣层,使账单能分列「原价 / 折扣 / 实收」。

**Architecture:** 原价留在代码矩阵 `relay/channel/task/seedance/pricing.go`(三个渠道类型共用的单一事实来源);折扣落在新配置 `billing_setting.video_promo`。折扣**不乘进矩阵单价**,而是作为与 `video_pricing` 并列的独立 `OtherRatios` 键 `video_promo` 注入 —— 因为 `PricingRatio` 是相对倍率,分母就是 base 格,折扣乘进矩阵会让 base 档折扣被分子分母同时缩放而静默约掉。预扣与结算对 `OtherRatios` 无差别连乘且无键白名单,故新键自动贯穿全链路,计费管线一行不动。

**Tech Stack:** Go 1.25.1 (Gin, GORM)、React 19 + TypeScript + Rsbuild + Base UI + Tailwind、Bun。

## Global Constraints

- **模块路径**:`github.com/QuantumNous/new-api`。
- **JSON**:禁止直接调用 `encoding/json` 的 marshal/unmarshal,必须走 `common.Marshal` / `common.Unmarshal` / `common.UnmarshalJsonStr`(CLAUDE.md Rule 1)。类型引用 `json.RawMessage` 仍允许。
- **受保护标识**:`new-api`、`QuantumNous` 的任何引用/版权头/元数据一律不得修改或删除(CLAUDE.md Rule 5)。
- **空配置 = 零行为变化**:`video_promo` 为空时,所有模型的 `OtherRatios` 必须与改动前逐键一致,且不出现 `video_promo` 键。这是本次最高优先级的验收标准。
- **结算路径不得重新读取折扣配置**:折扣在提交时写入 `TaskBillingContext.OtherRatios` 快照并持久化;结算只读快照连乘。在途任务不受调价影响。
- **矩阵只存原价**,折后价永不写入代码;`0.72` 这个系数不得出现在任何 Go 文件里,也不由迁移预置。
- **窗口判定为闭区间** `StartAt <= now <= EndAt`,单位 Unix 秒。`0` 表示「未配置」而非「永久有效」。
- **坏配置一律向原价降级**,不放大扣费、不 panic。坏系数只废该档位;坏窗口废整个模型。
- 后端测试命令:`go test ./...`(CI 不跑测试,这是事实标准命令)。
- 前端:包管理器 `bun`,工作区根为 `web/`。校验命令 `bun run typecheck`、`bun run lint`、`bun run format:check`、`bun run copyright:check`。
- **前端无测试框架**:无 vitest/jest/jsdom/testing-library。唯一先例是把纯逻辑抽到非 `.tsx` 模块用 `node:test` 测(`components/ui/dropdown-menu.test.tsx`)。不要写 `render()`/`screen` 测试。
- **前端新文件必须带 AGPL 版权头**(1–18 行),`bun run copyright` 可自动补齐。
- **前端所有用户可见文案必须走 `t('English source string')`**,React 组件内必须 `const { t } = useTranslation()`(子组件也要自己调,不能靠父组件)。新增 key 后跑 `bun run i18n:sync`,不要手改 6 个 locale JSON。
- **Prettier 严格**:`semi: false`、`singleQuote: true`、`jsxSingleQuote: true`、`printWidth: 80`、`trailingComma: "es5"`,且 `@trivago/prettier-plugin-sort-imports` 有 33 条 importOrder(`@/lib/*` → `@/components/ui/*` → `@/components/*` → `@/features/*` → 相对路径)。

---

## File Structure

**新建(Go)**
- `setting/billing_setting/video_promo.go` — `VideoPromo` 类型、读访问器、窗口判定(含可注入时钟 `nowFn`)、保存前结构校验。
- `setting/billing_setting/video_promo_test.go` — 窗口边界五点、秒级精度、坏配置降级。
- `relay/channel/task/seedance/promo_test.go` — 空配置不变量、base 档折扣不被约掉、`tierHit` 匹配、多模型互不串扰。

**修改(Go)**
- `relay/channel/task/seedance/pricing.go` — 两个 2.5 模型加 1080p 原价行;`CellUnit`/`PricingRatio` 增加 `tierHit` 返回值;新增 `TiersForModel` / `AllModelTiers`。
- `relay/channel/task/seedance/pricing_test.go` — 更新 2.5 的 1080p 断言与过时注释,适配四返回值。
- `relay/channel/task/seedance/billing.go` — `ResolveVideoBilling` 解析折扣并填快照;`EstimateBilling` 注入 `video_promo` 独立键。
- `relay/channel/task/sora/seedance2_test.go` — 仅适配签名,期望值不变。
- `setting/billing_setting/tiered_billing.go` — `BillingSetting` 加 `VideoPromo` 字段。
- `types/price_data.go` — `VideoBillingDisplay` 加 `PromoFactor`/`PromoStartAt`/`PromoEndAt`。
- `service/task_billing.go` — 预扣(:51-59)与结算(:147-160)两处日志追加折扣字段。
- `controller/option.go` — `billing_setting.video_promo` 存前校验 case;新增 `GetVideoPromoTiers` handler。
- `router/api-router.go` — 注册 `GET /api/option/video_promo_tiers`。

**新建(前端)**
- `web/default/src/components/datetime-picker-time.ts` — 纯时间字符串 ↔ Date 换算(可测)。
- `web/default/src/components/datetime-picker-time.test.ts` — `node:test`。
- `web/default/src/features/system-settings/models/video-promo-editor.tsx` — 单模型折扣编辑器(visual + JSON 双模式)。

**修改(前端)**
- `web/default/src/components/datetime-picker.tsx` — 加 `withSeconds` 可选 prop(默认 `false`,8 个既有调用点行为不变)。
- `web/default/src/lib/format.ts` — `formatTimestampForInput` 加带秒变体。
- `web/default/src/features/system-settings/models/model-pricing-sheet.tsx` — 嵌入折扣编辑器。
- `.../models/model-pricing-core.ts`、`.../models/model-pricing-snapshots.ts` — `ModelRatioData` / 快照增加 `videoPromo`。
- `.../models/model-ratio-visual-editor.tsx` — 透传折扣 map。
- `.../models/ratio-settings-card.tsx` — `apiKeyMap` 登记 `billing_setting.video_promo`。
- `web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx` — 账单补原价/折扣/实收行。
- `web/default/src/features/usage-logs/types.ts` — 补 `video_promo_*` 字段。

---

### Task 1: 2.5 补 1080p 原价档位

**Files:**
- Modify: `relay/channel/task/seedance/pricing.go:34-37`(dreamina 2.5)、`:57-60`(doubao 2.5)
- Test: `relay/channel/task/seedance/pricing_test.go`

**Interfaces:**
- Consumes: 无。
- Produces: `unitPrice` 中两个 2.5 模型各多出 `"1080p"` 档。后续任务依赖 `dreamina-seedance-2-5-260628` 的 1080p 为 `{false: 11.7, true: 7.0}`,`doubao-seedance-2-5-260628` 的 1080p 为 `{false: 77.0, true: 46.0}`。

- [ ] **Step 1: 先改测试(TDD)** —— 在 `pricing_test.go` 的 `TestDreaminaCellUnit` 里,把 2.5 那 4 行(现为 `:65-69` 区域)替换成:

```go
		// 2.5 支持 480p/720p 与 1080p 两档;4k 回退 base
		{"dreamina-seedance-2-5-260628", "base", false, 10.7},
		{"dreamina-seedance-2-5-260628", "base", true, 6.4},
		{"dreamina-seedance-2-5-260628", "1080p", false, 11.7},
		{"dreamina-seedance-2-5-260628", "1080p", true, 7.0},
		{"dreamina-seedance-2-5-260628", "4k", false, 10.7},
```

同时在 `TestDoubaoRatioEquivalence` 里把 2.5 那 5 行(现为 `:111-116` 区域)替换成:

```go
		// 2.5: base 70/42、1080p 原价 77/46;4k 无此档,回退 base
		{"doubao-seedance-2-5-260628", "base", false, 1.0},
		{"doubao-seedance-2-5-260628", "base", true, 42.0 / 70.0},
		{"doubao-seedance-2-5-260628", "1080p", false, 77.0 / 70.0},
		{"doubao-seedance-2-5-260628", "1080p", true, 46.0 / 70.0},
		{"doubao-seedance-2-5-260628", "4k", true, 42.0 / 70.0},
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay/channel/task/seedance/ -run 'TestDreaminaCellUnit|TestDoubaoRatioEquivalence' -v`
Expected: FAIL —— `unit(...)=6.4 want 7` 与 `PricingRatio(...)=1 want 1.1`。

- [ ] **Step 3: 加 1080p 原价行**

`pricing.go` 中把 dreamina 2.5 条目(含其上方注释)替换为:

```go
	// 2.5 支持 480p/720p(base)与 1080p 两档,按含/不含视频分别定价。
	// 1080p 存的是原价;限时折扣走后台配置 billing_setting.video_promo,永不写入本矩阵。
	"dreamina-seedance-2-5-260628": {
		"base":  {false: 10.7, true: 6.4},
		"1080p": {false: 11.7, true: 7.0},
	},
```

把 doubao 2.5 条目(含其上方注释)替换为:

```go
	// 2.5 支持 480p/720p(base)与 1080p 两档:
	// base 输入不含视频 70.00 元/M、包含视频 42.00 元/M;
	// 1080p 原价 输入不含视频 77.00 元/M、包含视频 46.00 元/M。
	// 1080p 存的是原价;限时折扣走后台配置 billing_setting.video_promo,永不写入本矩阵。
	"doubao-seedance-2-5-260628": {
		"base":  {false: 70.0, true: 42.0},
		"1080p": {false: 77.0, true: 46.0},
	},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relay/channel/task/seedance/... ./relay/channel/task/sora/...`
Expected: PASS(两个包均 ok)。`sora` 包期望值不变,因为它只测 mini。

- [ ] **Step 5: Commit**

```bash
git add relay/channel/task/seedance/pricing.go relay/channel/task/seedance/pricing_test.go
git commit -m "feat(seedance): 2.5 补 1080p 原价档位"
```

---

### Task 2: `CellUnit`/`PricingRatio` 返回真实计价档位

修正一个现存缺陷:模型没有某档位时单价静默回退 base,但日志照旧记录请求档位,导致账单显示 1080p、实际按 base 价扣费。折扣引入后这会让「原价 × 折扣 = 实收」算不平。

**Files:**
- Modify: `relay/channel/task/seedance/pricing.go:82-102`
- Modify: `relay/channel/task/seedance/billing.go:37`
- Test: `relay/channel/task/seedance/pricing_test.go`(调用点 `:72`、`:119`、`:132`、`:141`)
- Test: `relay/channel/task/sora/seedance2_test.go`(如有调用点)

**Interfaces:**
- Consumes: Task 1 的矩阵。
- Produces:
  - `func CellUnit(model, tier string, hasVideo bool) (unit, base float64, tierHit string, ok bool)`
  - `func PricingRatio(model, tier string, hasVideo bool) (ratio, base float64, tierHit string, ok bool)`
  - `func TiersForModel(model string) []string` —— 固定顺序 `base`,`1080p`,`4k`,只返回该模型真实存在的档位;未知模型返回 `nil`。
  - `func AllModelTiers() map[string][]string`

- [ ] **Step 1: 写失败测试** —— 在 `pricing_test.go` 末尾追加:

```go
// TestTierHit 校验实际计价档位如实返回:模型无该档位时回退 base 并如实报告。
func TestTierHit(t *testing.T) {
	cases := []struct {
		model    string
		tier     string
		wantHit  string
	}{
		{"doubao-seedance-2-5-260628", "1080p", "1080p"},
		{"doubao-seedance-2-5-260628", "4k", "base"},
		{"doubao-seedance-2-0-fast-260128", "1080p", "base"},
		{"doubao-seedance-2-0-mini-260615", "1080p", "base"},
		{"dreamina-seedance-2-0-260128", "4k", "4k"},
		{"doubao-seedance-2-0-260128", "base", "base"},
	}
	for _, tc := range cases {
		if _, _, hit, ok := CellUnit(tc.model, tc.tier, false); !ok || hit != tc.wantHit {
			t.Fatalf("CellUnit(%s,%s) hit=%q ok=%v want %q", tc.model, tc.tier, hit, ok, tc.wantHit)
		}
		if _, _, hit, ok := PricingRatio(tc.model, tc.tier, true); !ok || hit != tc.wantHit {
			t.Fatalf("PricingRatio(%s,%s) hit=%q ok=%v want %q", tc.model, tc.tier, hit, ok, tc.wantHit)
		}
	}
}

// TestTiersForModel 后台配置界面据此渲染档位行,顺序必须稳定。
func TestTiersForModel(t *testing.T) {
	if got := TiersForModel("doubao-seedance-2-5-260628"); len(got) != 2 || got[0] != "base" || got[1] != "1080p" {
		t.Fatalf("2.5 tiers=%v want [base 1080p]", got)
	}
	if got := TiersForModel("doubao-seedance-2-0-fast-260128"); len(got) != 1 || got[0] != "base" {
		t.Fatalf("fast tiers=%v want [base]", got)
	}
	if got := TiersForModel("dreamina-seedance-2-0-260128"); len(got) != 3 {
		t.Fatalf("2.0 tiers=%v want 3 entries", got)
	}
	if got := TiersForModel("sora-2"); got != nil {
		t.Fatalf("unknown model tiers=%v want nil", got)
	}
	if all := AllModelTiers(); len(all) != len(unitPrice) {
		t.Fatalf("AllModelTiers len=%d want %d", len(all), len(unitPrice))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay/channel/task/seedance/ -run 'TestTierHit|TestTiersForModel'`
Expected: 编译失败 —— `assignment mismatch: 4 variables but CellUnit returns 3 values` 与 `undefined: TiersForModel`。

- [ ] **Step 3: 改实现** —— `pricing.go` 里把 `CellUnit` 与 `PricingRatio` 整体替换为:

```go
// CellUnit 返回某格(model,tier,hasVideo)的原单价、该模型基准单价(base 档不含视频),
// 以及 tierHit —— 实际用于计价的档位。模型不支持请求档位时回退 base,tierHit 如实为 "base",
// 使日志能记录真实计价档位而非请求档位。
func CellUnit(model, tier string, hasVideo bool) (unit, base float64, tierHit string, ok bool) {
	tiers, exists := unitPrice[model]
	if !exists {
		return 0, 0, "", false
	}
	cell, has := tiers[tier]
	if has {
		tierHit = tier
	} else {
		cell = tiers["base"]
		tierHit = "base"
	}
	return cell[hasVideo], tiers["base"][false], tierHit, true
}

// PricingRatio 返回相对基准的单一合并倍率 video_pricing = 单元格原单价 ÷ 基准原单价,
// 基准单价(供展示回退),以及实际计价档位。该倍率**不含任何折扣**。
func PricingRatio(model, tier string, hasVideo bool) (ratio, base float64, tierHit string, ok bool) {
	unit, base, tierHit, ok := CellUnit(model, tier, hasVideo)
	if !ok || base <= 0 {
		return 0, 0, "", false
	}
	return unit / base, base, tierHit, true
}

// tierOrder 是档位的稳定展示顺序,后台配置界面按此渲染。
var tierOrder = []string{"base", "1080p", "4k"}

// TiersForModel 返回该模型原价矩阵中真实存在的档位(稳定顺序),供后台按模型渲染折扣行。
// 这从源头消掉「给只有 base 档的 fast 配 1080p 折扣」这类无效配置。
func TiersForModel(model string) []string {
	tiers, ok := unitPrice[model]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(tiers))
	for _, t := range tierOrder {
		if _, has := tiers[t]; has {
			out = append(out, t)
		}
	}
	return out
}

// AllModelTiers 返回所有 Seedance 模型的可用档位,供后台一次性拉取。
func AllModelTiers() map[string][]string {
	out := make(map[string][]string, len(unitPrice))
	for m := range unitPrice {
		out[m] = TiersForModel(m)
	}
	return out
}
```

- [ ] **Step 4: 修所有调用点** —— `billing.go:37` 改为四返回值(暂不接折扣,Task 4 再接),并把快照的 `ResolutionTier` 改为记录 `tierHit`:

```go
	ratio, base, tierHit, ok := PricingRatio(model, tier, hasVideo)
	if !ok {
		return 0, types.VideoBillingDisplay{}, false
	}
	return ratio, types.VideoBillingDisplay{
		ResolutionTier:  tierHit,
		HasVideoInput:   hasVideo,
		BaseUnitUSDPerM: base,
		PricingRatio:    ratio,
	}, true
```

`pricing_test.go` 的 4 处调用点改为丢弃新返回值:`got, _, _, ok := CellUnit(...)`、`ratio, _, _, ok := PricingRatio(...)`、`r, _, _, ok := PricingRatio(...)`、`r, base, _, ok := PricingRatio(...)`。

- [ ] **Step 5: 跑全量测试**

Run: `go build ./... && go test ./relay/... ./service/... ./types/...`
Expected: PASS。`sora/seedance2_test.go` 的 14/23 期望值不变;`TestEstimateSeedance2Ratios_Mini1080pNoResolutionRatio` 仍绿(mini 无 1080p → 回退 base → 倍率 1.0 → 不写键)。

- [ ] **Step 6: Commit**

```bash
git add relay/channel/task/seedance/ relay/channel/task/sora/
git commit -m "fix(seedance): 计费档位如实返回,日志不再记录未生效的请求档位"
```

---

### Task 3: `billing_setting.video_promo` 配置 + 窗口判定

**Files:**
- Create: `setting/billing_setting/video_promo.go`
- Create: `setting/billing_setting/video_promo_test.go`
- Modify: `setting/billing_setting/tiered_billing.go:20-28`

**Interfaces:**
- Consumes: 无(纯配置层,不 import seedance,避免循环依赖)。
- Produces:
  - `type VideoPromo struct { Factors map[string]float64; StartAt, EndAt int64 }`
  - `func ResolveVideoPromo(model, tier string) (factor float64, startAt, endAt int64)` —— 无折扣时返回 `(1, 0, 0)`。
  - `func GetVideoPromoCopy() map[string]VideoPromo`
  - `func ValidateVideoPromo(jsonStr string) error` —— 只做结构校验,**不校验档位键**(档位属于 seedance 包,在此校验会造成 `billing_setting → seedance → billing_setting` 循环导入;档位键校验放在 Task 6 的 controller 层)。
  - 测试钩子 `var nowFn = common.GetTimestamp`。

> **注意:仓库里不存在任何时钟注入先例**(全仓 `nowFunc`/`clock`/`func() time.Time` 零命中)。此处刻意用**包内私有 `var nowFn`** 而非把 `common.GetTimestamp` 改成 `var` —— 后者有约 50 个调用点,爆炸半径过大。

- [ ] **Step 1: 给 `BillingSetting` 加字段** —— `tiered_billing.go` 中把 struct 与初始值替换为:

```go
// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr, billing_setting.video_promo
type BillingSetting struct {
	BillingMode map[string]string     `json:"billing_mode"`
	BillingExpr map[string]string     `json:"billing_expr"`
	VideoPromo  map[string]VideoPromo `json:"video_promo"` // 模型名 → 限时活动
}

var billingSetting = BillingSetting{
	BillingMode: make(map[string]string),
	BillingExpr: make(map[string]string),
	VideoPromo:  make(map[string]VideoPromo),
}
```

- [ ] **Step 2: 写失败测试** —— 新建 `setting/billing_setting/video_promo_test.go`:

```go
package billing_setting

import "testing"

// withPromo 临时替换折扣配置与时钟,返回还原函数。
func withPromo(t *testing.T, cfg map[string]VideoPromo, now int64) {
	t.Helper()
	oldCfg, oldNow := billingSetting.VideoPromo, nowFn
	billingSetting.VideoPromo = cfg
	nowFn = func() int64 { return now }
	t.Cleanup(func() {
		billingSetting.VideoPromo = oldCfg
		nowFn = oldNow
	})
}

const (
	winStart = int64(1_800_000_000)
	winEnd   = int64(1_800_003_600)
)

func onePromo(factors map[string]float64, start, end int64) map[string]VideoPromo {
	return map[string]VideoPromo{"m": {Factors: factors, StartAt: start, EndAt: end}}
}

// TestWindowIsClosedInterval 闭区间五点:start-1 不折、start 折、区间中折、end 折、end+1 不折。
func TestWindowIsClosedInterval(t *testing.T) {
	cases := []struct {
		now    int64
		want   float64
	}{
		{winStart - 1, 1.0},
		{winStart, 0.5},
		{winStart + 1800, 0.5},
		{winEnd, 0.5},
		{winEnd + 1, 1.0},
	}
	for _, tc := range cases {
		withPromo(t, onePromo(map[string]float64{"base": 0.5}, winStart, winEnd), tc.now)
		got, _, _ := ResolveVideoPromo("m", "base")
		if got != tc.want {
			t.Fatalf("now=%d factor=%v want %v", tc.now, got, tc.want)
		}
	}
}

// TestSecondPrecision 窗口 10:00:00~10:00:59,10:00:30 命中、10:01:00 不命中。
func TestSecondPrecision(t *testing.T) {
	s, e := int64(1_800_000_000), int64(1_800_000_059)
	withPromo(t, onePromo(map[string]float64{"base": 0.72}, s, e), s+30)
	if f, _, _ := ResolveVideoPromo("m", "base"); f != 0.72 {
		t.Fatalf("mid-window factor=%v want 0.72", f)
	}
	withPromo(t, onePromo(map[string]float64{"base": 0.72}, s, e), s+60)
	if f, _, _ := ResolveVideoPromo("m", "base"); f != 1.0 {
		t.Fatalf("after-window factor=%v want 1.0", f)
	}
}

// TestBadConfigFallsBackToListPrice 坏值一律降级到原价,且不 panic。
func TestBadConfigFallsBackToListPrice(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]VideoPromo
	}{
		{"factor zero", onePromo(map[string]float64{"base": 0}, winStart, winEnd)},
		{"factor negative", onePromo(map[string]float64{"base": -0.5}, winStart, winEnd)},
		{"factor above one", onePromo(map[string]float64{"base": 1.5}, winStart, winEnd)},
		{"nil factors", onePromo(nil, winStart, winEnd)},
		{"empty factors", onePromo(map[string]float64{}, winStart, winEnd)},
		{"missing start", onePromo(map[string]float64{"base": 0.5}, 0, winEnd)},
		{"missing end", onePromo(map[string]float64{"base": 0.5}, winStart, 0)},
		{"start equals end", onePromo(map[string]float64{"base": 0.5}, winStart, winStart)},
		{"start after end", onePromo(map[string]float64{"base": 0.5}, winEnd, winStart)},
	}
	for _, tc := range cases {
		withPromo(t, tc.cfg, winStart+10)
		f, s, e := ResolveVideoPromo("m", "base")
		if f != 1.0 || s != 0 || e != 0 {
			t.Fatalf("%s: got (%v,%d,%d) want (1,0,0)", tc.name, f, s, e)
		}
	}
}

// TestDegradationGranularity 坏系数只废该档位;好档位不受牵连。
func TestDegradationGranularity(t *testing.T) {
	withPromo(t, onePromo(map[string]float64{"base": 5.0, "1080p": 0.72}, winStart, winEnd), winStart+10)
	if f, _, _ := ResolveVideoPromo("m", "base"); f != 1.0 {
		t.Fatalf("bad base factor should degrade, got %v", f)
	}
	if f, _, _ := ResolveVideoPromo("m", "1080p"); f != 0.72 {
		t.Fatalf("good 1080p factor should survive, got %v", f)
	}
}

// TestUnconfiguredModelAndTier 未配置的模型/档位无折扣。
func TestUnconfiguredModelAndTier(t *testing.T) {
	withPromo(t, onePromo(map[string]float64{"1080p": 0.72}, winStart, winEnd), winStart+10)
	if f, _, _ := ResolveVideoPromo("other-model", "1080p"); f != 1.0 {
		t.Fatalf("unconfigured model should not discount")
	}
	if f, _, _ := ResolveVideoPromo("m", "base"); f != 1.0 {
		t.Fatalf("unconfigured tier should not discount")
	}
}

// TestEmptyConfigIsNoOp 空配置下任何查询都返回不打折 —— 对应最高优先级验收标准。
func TestEmptyConfigIsNoOp(t *testing.T) {
	withPromo(t, map[string]VideoPromo{}, winStart+10)
	for _, tier := range []string{"base", "1080p", "4k", ""} {
		if f, s, e := ResolveVideoPromo("m", tier); f != 1.0 || s != 0 || e != 0 {
			t.Fatalf("empty config tier=%q got (%v,%d,%d) want (1,0,0)", tier, f, s, e)
		}
	}
}

// TestWindowsAreIndependentPerModel A 进行中、B 未开始、C 已结束同时成立时只有 A 打折。
func TestWindowsAreIndependentPerModel(t *testing.T) {
	now := winStart + 10
	withPromo(t, map[string]VideoPromo{
		"a": {Factors: map[string]float64{"base": 0.8}, StartAt: winStart, EndAt: winEnd},
		"b": {Factors: map[string]float64{"base": 0.8}, StartAt: now + 100, EndAt: now + 200},
		"c": {Factors: map[string]float64{"base": 0.8}, StartAt: now - 200, EndAt: now - 100},
	}, now)
	if f, _, _ := ResolveVideoPromo("a", "base"); f != 0.8 {
		t.Fatalf("a should be active, got %v", f)
	}
	if f, _, _ := ResolveVideoPromo("b", "base"); f != 1.0 {
		t.Fatalf("b not started yet, got %v", f)
	}
	if f, _, _ := ResolveVideoPromo("c", "base"); f != 1.0 {
		t.Fatalf("c already ended, got %v", f)
	}
}

// TestResolveReturnsWindowForAudit 命中时必须回传窗口,供账单自证。
func TestResolveReturnsWindowForAudit(t *testing.T) {
	withPromo(t, onePromo(map[string]float64{"base": 0.72}, winStart, winEnd), winStart+10)
	f, s, e := ResolveVideoPromo("m", "base")
	if f != 0.72 || s != winStart || e != winEnd {
		t.Fatalf("got (%v,%d,%d) want (0.72,%d,%d)", f, s, e, winStart, winEnd)
	}
}

func TestValidateVideoPromo(t *testing.T) {
	ok := []string{
		``, `{}`,
		`{"m":{"factors":{"1080p":0.72},"start_at":1800000000,"end_at":1800003600}}`,
		`{"m":{"factors":{"base":1},"start_at":1,"end_at":2}}`,
		`{"m":{"factors":{},"start_at":0,"end_at":0}}`,
	}
	for _, s := range ok {
		if err := ValidateVideoPromo(s); err != nil {
			t.Fatalf("ValidateVideoPromo(%s) unexpected err: %v", s, err)
		}
	}
	bad := []string{
		`not json`,
		`{"m":{"factors":{"base":0},"start_at":1,"end_at":2}}`,
		`{"m":{"factors":{"base":1.5},"start_at":1,"end_at":2}}`,
		`{"m":{"factors":{"base":-1},"start_at":1,"end_at":2}}`,
		`{"m":{"factors":{"base":0.5},"start_at":0,"end_at":2}}`,
		`{"m":{"factors":{"base":0.5},"start_at":1,"end_at":0}}`,
		`{"m":{"factors":{"base":0.5},"start_at":2,"end_at":1}}`,
		`{"m":{"factors":{"base":0.5},"start_at":1,"end_at":1}}`,
	}
	for _, s := range bad {
		if err := ValidateVideoPromo(s); err == nil {
			t.Fatalf("ValidateVideoPromo(%s) expected error, got nil", s)
		}
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./setting/billing_setting/`
Expected: 编译失败 —— `undefined: VideoPromo`、`undefined: ResolveVideoPromo`、`undefined: nowFn`、`undefined: ValidateVideoPromo`。

- [ ] **Step 4: 写实现** —— 新建 `setting/billing_setting/video_promo.go`(第一段):

```go
package billing_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"github.com/samber/lo"
)

// VideoPromo 单个模型的限时折扣活动。DB key: billing_setting.video_promo(模型名 → 本结构)。
//
// 设计要点:
//   - 折扣**不写入原价矩阵**。矩阵只存原价,折扣是独立乘子,详见
//     docs/superpowers/specs/2026-09-11-seedance-promo-pricing-layer-design.md 第三节。
//   - 窗口是**模型级**而非档位级:同一模型多个档位若需不同起止时间,视为两场活动,当前不支持。
type VideoPromo struct {
	// Factors 计价档位 → 折扣系数,如 {"1080p": 0.72}。
	// 键必须是该模型在原价矩阵中真实存在的档位;未列出的档位不打折。
	Factors map[string]float64 `json:"factors"`

	// StartAt / EndAt 活动窗口,Unix 秒(精确到秒),判定为闭区间 StartAt <= now <= EndAt。
	// 0 表示「未配置」而非「永久有效」:Factors 非空却漏填任一端是事故而非永久活动,
	// 此时整个模型的折扣不生效 —— 宁可少收折扣,不可无限期打折。
	StartAt int64 `json:"start_at"`
	EndAt   int64 `json:"end_at"`
}

// nowFn 是可注入的时钟,仅测试会替换。仓库无既有时钟注入约定,
// 刻意用包内私有变量而非把 common.GetTimestamp 改成 var(后者约 50 个调用点,爆炸半径过大)。
var nowFn = common.GetTimestamp

// GetVideoPromoCopy 返回折扣配置副本,与 GetBillingModeCopy(tiered_billing.go:50)一致,
// 热路径不共享 map。注意沿用仓库既有的单参 lo.Assign 写法。
func GetVideoPromoCopy() map[string]VideoPromo {
	return lo.Assign(billingSetting.VideoPromo)
}

// ResolveVideoPromo 返回指定模型在指定**实际计价档位**上当前生效的折扣系数与活动窗口。
// 无活动 / 未开始 / 已结束 / 配置非法时返回 (1, 0, 0),调用方据此判定「不打折」。
//
// 坏值一律向原价降级,不放大扣费、不 panic。两种降级粒度:
// 坏系数只废该档位;坏窗口废整个模型(窗口是模型级的,说不清起止的活动整场都不该生效)。
func ResolveVideoPromo(model, tier string) (factor float64, startAt, endAt int64) {
	promo, ok := billingSetting.VideoPromo[model]
	if !ok || len(promo.Factors) == 0 {
		return 1.0, 0, 0
	}
	if promo.StartAt <= 0 || promo.EndAt <= 0 || promo.StartAt >= promo.EndAt {
		common.SysLog(fmt.Sprintf(
			"video_promo: model %s has factors but invalid window (start=%d end=%d), discount disabled for the whole model",
			model, promo.StartAt, promo.EndAt))
		return 1.0, 0, 0
	}
	if now := nowFn(); now < promo.StartAt || now > promo.EndAt {
		return 1.0, 0, 0
	}
	f, has := promo.Factors[tier]
	if !has {
		return 1.0, 0, 0
	}
	if f <= 0 || f > 1 {
		common.SysLog(fmt.Sprintf(
			"video_promo: model %s tier %s has invalid factor %v (want 0 < f <= 1), this tier falls back to list price",
			model, tier, f))
		return 1.0, 0, 0
	}
	return f, promo.StartAt, promo.EndAt
}

// ValidateVideoPromo 保存前校验(参照 controller/option.go 的 case 分支模式)。
// 只做结构校验;档位键是否属于该模型矩阵由 controller 层校验 —— 在此 import seedance
// 会造成 billing_setting → seedance → billing_setting 循环导入。
func ValidateVideoPromo(jsonStr string) error {
	if jsonStr == "" {
		return nil
	}
	var cfg map[string]VideoPromo
	if err := common.UnmarshalJsonStr(jsonStr, &cfg); err != nil {
		return fmt.Errorf("invalid video_promo JSON: %w", err)
	}
	for model, p := range cfg {
		if len(p.Factors) == 0 {
			continue
		}
		if p.StartAt <= 0 || p.EndAt <= 0 {
			return fmt.Errorf("model %s: start_at and end_at are both required when factors is set", model)
		}
		if p.StartAt >= p.EndAt {
			return fmt.Errorf("model %s: start_at must be earlier than end_at", model)
		}
		for tier, f := range p.Factors {
			if f <= 0 || f > 1 {
				return fmt.Errorf("model %s tier %s: factor %v must be within (0, 1]", model, tier, f)
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./setting/billing_setting/ -v`
Expected: 全部 PASS(10 个新测试)。若 `common.SysLog` 签名不符,先 `grep -n "func SysLog" common/logger.go` 确认。

- [ ] **Step 6: Commit**

```bash
git add setting/billing_setting/
git commit -m "feat(billing): 新增 video_promo 限时折扣配置与闭区间窗口判定"
```

---

### Task 4: 折扣作为独立乘子注入 `OtherRatios`

**这是全案最容易写错的一步。** 折扣**绝不能**乘进矩阵单价:`PricingRatio` 的分母就是 base/无视频那一格,base 档折扣的分子分母会同时缩放而约掉 —— `(37×0.72)/(37×0.72) = 1.0`,与不打折完全相同;更糟的是 `EstimateBilling` 恰在 `ratio == 1.0` 时返回 `nil`,连键都不写,而 `TestBaseRatioIsOne` 会把这个 1.0 断言为正确。**折扣静默失效,测试全绿。**

**Files:**
- Modify: `types/price_data.go:33-40`
- Modify: `relay/channel/task/seedance/billing.go:15-47`
- Create: `relay/channel/task/seedance/promo_test.go`

**Interfaces:**
- Consumes: Task 2 的 `PricingRatio(...) (ratio, base, tierHit, ok)`;Task 3 的 `billing_setting.ResolveVideoPromo(model, tier)`。
- Produces: `EstimateBilling` 返回的 map 可能含两个键 —— `video_pricing`(原价倍率,仅当 `!= 1.0`)与 `video_promo`(折扣,仅当 `< 1.0`);两者皆无时返回 `nil`。`types.VideoBillingDisplay` 新增 `PromoFactor`/`PromoStartAt`/`PromoEndAt`。

- [ ] **Step 1: 给展示快照加三字段** —— `types/price_data.go` 中把 `VideoBillingDisplay` 替换为:

```go
// VideoBillingDisplay 视频计费的展示用快照(不参与上游 marshal,仅用于日志/前端核价)。
// 原价单价 = BaseUnitUSDPerM * PricingRatio(再随 modelRatio 加价整体缩放);
// 实收单价 = 原价单价 * PromoFactor。
type VideoBillingDisplay struct {
	ResolutionTier  string  // 实际计价档位:"base" / "1080p" / "4k"
	HasVideoInput   bool    // 是否含视频输入
	BaseUnitUSDPerM float64 // 基准单价(USD / 百万 token)
	PricingRatio    float64 // 相对基准的合并倍率(video_pricing),**纯原价,不含折扣**
	VideoTokens     int     // 结算阶段回填的实际 completion_tokens
	PromoFactor     float64 // 限时折扣系数;未打折为 1
	PromoStartAt    int64   // 活动开始 Unix 秒;未打折为 0
	PromoEndAt      int64   // 活动结束 Unix 秒;未打折为 0
}
```

> 起止都落库是为了让历史账单**自证**:事后核查一笔扣费时,当时的窗口与系数都在日志里,不必去翻「现在的配置」反推。

- [ ] **Step 2: 写失败测试** —— 新建 `relay/channel/task/seedance/promo_test.go`:

```go
package seedance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"

	"github.com/gin-gonic/gin"
)

// newCtx 构造一个带 body 的任务请求上下文;视频/分辨率信号由 metadata 承载。
func newCtx(model string, metadata map[string]interface{}) (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("{}"))
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    model,
		Prompt:   "a cat",
		Metadata: metadata,
	})
	return c, &relaycommon.RelayInfo{OriginModelName: model}
}

func resMeta(res string) map[string]interface{} {
	return map[string]interface{}{"resolution": res}
}

// TestEmptyPromoConfigIsByteIdentical 最高优先级验收标准:
// video_promo 为空时,所有模型所有档位的 OtherRatios 与折扣层引入前逐键一致,且不出现 video_promo 键。
func TestEmptyPromoConfigIsByteIdentical(t *testing.T) {
	billing_setting.SetVideoPromoForTest(t, nil, 0)
	for model := range unitPrice {
		for _, res := range []string{"480p", "1080p", "4k"} {
			c, info := newCtx(model, resMeta(res))
			got := EstimateBilling(c, info)
			if _, has := got["video_promo"]; has {
				t.Fatalf("model=%s res=%s leaked video_promo key with empty config: %v", model, res, got)
			}
			tier := ClassifyResTier(res)
			wantRatio, _, _, ok := PricingRatio(model, tier, false)
			if !ok {
				t.Fatalf("unexpected pricing miss for %s", model)
			}
			if wantRatio == 1.0 {
				if got != nil {
					t.Fatalf("model=%s res=%s want nil ratios, got %v", model, res, got)
				}
				continue
			}
			if len(got) != 1 || !approx(got["video_pricing"], wantRatio) {
				t.Fatalf("model=%s res=%s got %v want only video_pricing=%v", model, res, got, wantRatio)
			}
			if info.PriceData.VideoBilling.PromoFactor != 1.0 {
				t.Fatalf("model=%s PromoFactor=%v want 1.0", model, info.PriceData.VideoBilling.PromoFactor)
			}
		}
	}
}

// TestBaseTierPromoSurvivesRatioDenominator 全案核心回归:
// base 档折扣必须独立生效,不能被相对倍率的分母约掉。折扣若乘进矩阵,此用例静默失败。
func TestBaseTierPromoSurvivesRatioDenominator(t *testing.T) {
	const model = "doubao-seedance-2-0-fast-260128"
	billing_setting.SetVideoPromoForTest(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"base": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)

	c, info := newCtx(model, resMeta("480p"))
	got := EstimateBilling(c, info)
	if !approx(got["video_promo"], 0.72) {
		t.Fatalf("video_promo=%v want 0.72 (got ratios %v)", got["video_promo"], got)
	}
	if _, has := got["video_pricing"]; has {
		t.Fatalf("base/no-video ratio is 1.0 and must stay unwritten, got %v", got)
	}
	vb := info.PriceData.VideoBilling
	if !approx(vb.PricingRatio, 1.0) {
		t.Fatalf("PricingRatio=%v must stay list-price 1.0", vb.PricingRatio)
	}
	if vb.PromoStartAt != 100 || vb.PromoEndAt != 200 {
		t.Fatalf("window not snapshotted: (%d,%d)", vb.PromoStartAt, vb.PromoEndAt)
	}
}

// Test1080pPromoIsIndependentOfListRatio 1080p 折扣:两键相乘 = 折后有效倍率。
func Test1080pPromoIsIndependentOfListRatio(t *testing.T) {
	const model = "doubao-seedance-2-5-260628"
	billing_setting.SetVideoPromoForTest(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)

	c, info := newCtx(model, resMeta("1080p"))
	got := EstimateBilling(c, info)
	if !approx(got["video_pricing"], 77.0/70.0) {
		t.Fatalf("video_pricing=%v want list ratio 77/70", got["video_pricing"])
	}
	if !approx(got["video_promo"], 0.72) {
		t.Fatalf("video_promo=%v want 0.72", got["video_promo"])
	}
	if !approx(got["video_pricing"]*got["video_promo"], 0.72*77.0/70.0) {
		t.Fatalf("product mismatch")
	}
	if vb := info.PriceData.VideoBilling; vb.ResolutionTier != "1080p" || !approx(vb.PromoFactor, 0.72) {
		t.Fatalf("snapshot tier=%q factor=%v want 1080p/0.72", vb.ResolutionTier, vb.PromoFactor)
	}
}

// TestPromoTierIsolation 只给 1080p 配系数时,base 请求扣费与配置前完全一致。
func TestPromoTierIsolation(t *testing.T) {
	const model = "doubao-seedance-2-5-260628"
	billing_setting.SetVideoPromoForTest(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)

	c, info := newCtx(model, resMeta("720p"))
	got := EstimateBilling(c, info)
	if _, has := got["video_promo"]; has {
		t.Fatalf("base request must not be discounted, got %v", got)
	}
	if info.PriceData.VideoBilling.PromoFactor != 1.0 {
		t.Fatalf("PromoFactor=%v want 1.0", info.PriceData.VideoBilling.PromoFactor)
	}
}

// TestPromoMatchesTierHitNotRequestedTier 折扣按实际计价档位匹配:
// 1080p 请求在只有 base 档的模型上回退 base 计价,配在 1080p 上的折扣不得生效
// —— 否则会把 1080p 的折扣施加到 base 单价上。
func TestPromoMatchesTierHitNotRequestedTier(t *testing.T) {
	const model = "doubao-seedance-2-0-fast-260128"
	billing_setting.SetVideoPromoForTest(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)

	c, info := newCtx(model, resMeta("1080p"))
	got := EstimateBilling(c, info)
	if _, has := got["video_promo"]; has {
		t.Fatalf("1080p promo must not apply when billing fell back to base, got %v", got)
	}
	if info.PriceData.VideoBilling.ResolutionTier != "base" {
		t.Fatalf("ResolutionTier=%q want base", info.PriceData.VideoBilling.ResolutionTier)
	}
}

// TestFourModelsDoNotCrossTalk 四个模型各配不同档位与系数,互不串扰。
func TestFourModelsDoNotCrossTalk(t *testing.T) {
	billing_setting.SetVideoPromoForTest(t, map[string]billing_setting.VideoPromo{
		"doubao-seedance-2-5-260628":       {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
		"doubao-seedance-2-0-fast-260128":  {Factors: map[string]float64{"base": 0.9}, StartAt: 100, EndAt: 200},
		"doubao-seedance-2-0-mini-260615":  {Factors: map[string]float64{"base": 0.5}, StartAt: 300, EndAt: 400},
		"dreamina-seedance-2-5-260628":     {Factors: map[string]float64{"1080p": 0.6}, StartAt: 100, EndAt: 200},
	}, 150)

	cases := []struct {
		model string
		res   string
		want  float64 // 0 表示不应出现 video_promo 键
	}{
		{"doubao-seedance-2-5-260628", "1080p", 0.72},
		{"doubao-seedance-2-5-260628", "480p", 0},
		{"doubao-seedance-2-0-fast-260128", "480p", 0.9},
		{"doubao-seedance-2-0-mini-260615", "480p", 0}, // 窗口未开始
		{"dreamina-seedance-2-5-260628", "1080p", 0.6},
		{"doubao-seedance-2-0-260128", "1080p", 0}, // 未配置
	}
	for _, tc := range cases {
		c, _ := newCtx(tc.model, resMeta(tc.res))
		info := &relaycommon.RelayInfo{OriginModelName: tc.model}
		got := EstimateBilling(c, info)
		f, has := got["video_promo"]
		if tc.want == 0 {
			if has {
				t.Fatalf("%s/%s should have no promo, got %v", tc.model, tc.res, got)
			}
			continue
		}
		if !has || !approx(f, tc.want) {
			t.Fatalf("%s/%s promo=%v has=%v want %v", tc.model, tc.res, f, has, tc.want)
		}
	}
}
```

- [ ] **Step 2b: 给 `billing_setting` 加测试注入辅助** —— 追加到 `setting/billing_setting/video_promo.go` 末尾(生产代码里保留,因为跨包测试需要它;`testing.TB` 参数保证只能从测试调用):

```go
// SetVideoPromoForTest 供跨包测试注入折扣配置与固定时钟,退出时自动还原。
// 参数取 testing.TB 而非 bool,确保只有测试代码能调用。
func SetVideoPromoForTest(tb testing.TB, cfg map[string]VideoPromo, now int64) {
	tb.Helper()
	oldCfg, oldNow := billingSetting.VideoPromo, nowFn
	billingSetting.VideoPromo = cfg
	if now != 0 {
		nowFn = func() int64 { return now }
	}
	tb.Cleanup(func() {
		billingSetting.VideoPromo = oldCfg
		nowFn = oldNow
	})
}
```

并把 `import` 块加上 `"testing"`。

> 把 `testing` 引入生产包会让 `go vet` 在某些配置下告警,但这是仓库内跨包注入配置的唯一无侵入做法,且 `testing.TB` 签名使其无法被生产代码误用。若评审反对,替代方案是把这三个测试挪到 `billing_setting` 包内并改用内部 `withPromo`。

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./relay/channel/task/seedance/ -run 'Promo|Empty'`
Expected: FAIL —— `undefined: billing_setting.SetVideoPromoForTest`,以及 `PromoFactor` 未定义。

- [ ] **Step 4: 改 `billing.go`** —— 把 `EstimateBilling` 与 `ResolveVideoBilling` 整体替换为:

```go
// EstimateBilling 是三个渠道类型共用的 Seedance 2.0 计费入口:检测含视频输入 + 分辨率档位,
// 查原价矩阵得到 video_pricing 倍率,解析后台限时折扣得到 video_promo 倍率,
// 写入展示快照 info.PriceData.VideoBilling,并返回 OtherRatios。
//
// 两个倍率**并列为独立键**,折扣绝不乘进矩阵单价:PricingRatio 是相对倍率,
// 分母就是 base 格,折扣乘进矩阵会让 base 档折扣被分子分母同时缩放而静默约掉。
// 预扣与结算对 OtherRatios 无差别连乘且无键白名单,故新键自动贯穿全链路。
//
// 非 Seedance 2.0 模型返回 nil;两个倍率都不生效时也返回 nil(不追加冗余倍率)。
func EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	ratio, disp, ok := ResolveVideoBilling(c, info.OriginModelName)
	if !ok {
		return nil
	}
	d := disp
	info.PriceData.VideoBilling = &d

	ratios := make(map[string]float64, 2)
	if ratio != 1.0 {
		ratios["video_pricing"] = ratio
	}
	if d.PromoFactor > 0 && d.PromoFactor < 1.0 {
		ratios["video_promo"] = d.PromoFactor
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

// ResolveVideoBilling 检测请求并计算 (video_pricing 原价倍率, 展示快照)。
// 快照的 ResolutionTier 记录**实际计价档位**(tierHit)而非请求档位:模型不支持请求档位时
// 单价回退 base,账单必须如实显示 base,否则「原价 × 折扣 = 实收」在账单上算不平。
// 折扣按 tierHit 匹配,避免把 1080p 的折扣施加到回退后的 base 单价上。
func ResolveVideoBilling(c *gin.Context, model string) (float64, types.VideoBillingDisplay, bool) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return 0, types.VideoBillingDisplay{}, false
	}
	hasVideo := HasVideoInput(c, &req)
	tier := ClassifyResTier(DetectResolution(c, &req))
	ratio, base, tierHit, ok := PricingRatio(model, tier, hasVideo)
	if !ok {
		return 0, types.VideoBillingDisplay{}, false
	}
	factor, startAt, endAt := billing_setting.ResolveVideoPromo(model, tierHit)
	return ratio, types.VideoBillingDisplay{
		ResolutionTier:  tierHit,
		HasVideoInput:   hasVideo,
		BaseUnitUSDPerM: base,
		PricingRatio:    ratio,
		PromoFactor:     factor,
		PromoStartAt:    startAt,
		PromoEndAt:      endAt,
	}, true
}
```

并在 `billing.go` 的 import 块加入 `"github.com/QuantumNous/new-api/setting/billing_setting"`。

> **无循环导入**:`billing_setting` 只 import `common`、`setting/config`、`pkg/billingexpr`、`samber/lo`,不 import `relay/*`。

- [ ] **Step 5: 跑全量测试**

Run: `go build ./... && go test ./relay/... ./setting/... ./service/... ./types/...`
Expected: 全 PASS。特别确认 `TestBaseRatioIsOne` 与 `sora` 包 14/23 期望值仍绿。

- [ ] **Step 6: Commit**

```bash
git add types/price_data.go relay/channel/task/seedance/ setting/billing_setting/
git commit -m "feat(seedance): 限时折扣作为独立乘子注入,base 档不再被分母约掉"
```

---

### Task 5: 在途任务快照一致性回归

折扣可随时改,必须钉死「按提交时的折扣结算」。现有机制已保证:`video_promo` 在提交时写入 `TaskBillingContext.OtherRatios`(`model/task.go:142`),随 `PrivateData` 持久化到 `tasks.private_data`;结算只读快照连乘(`service/task_billing.go:354-361`)。本任务只补测试把这条约束钉住 —— **不改生产代码**。

**Files:**
- Create/Modify: `relay/channel/task/seedance/promo_test.go`(追加)

**Interfaces:**
- Consumes: Task 4 的 `EstimateBilling`。
- Produces: 无生产代码变更。

- [ ] **Step 1: 追加测试**

```go
// TestInFlightTaskUsesSubmitTimeSnapshot 提交后修改折扣配置,结算仍按提交时快照计费。
// 这条守的是对账:用户按下单时看到的价格付费,中途调价不影响在途任务。
func TestInFlightTaskUsesSubmitTimeSnapshot(t *testing.T) {
	const model = "doubao-seedance-2-5-260628"

	// 提交时:1080p 打 0.72
	billing_setting.SetVideoPromoForTest(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.72}, StartAt: 100, EndAt: 200},
	}, 150)
	c, info := newCtx(model, resMeta("1080p"))
	submitted := EstimateBilling(c, info)
	if !approx(submitted["video_promo"], 0.72) {
		t.Fatalf("submit-time promo=%v want 0.72", submitted["video_promo"])
	}

	// 快照是值拷贝,后续改配置不得回改它
	snapshot := make(map[string]float64, len(submitted))
	for k, v := range submitted {
		snapshot[k] = v
	}

	// 运营中途改成 0.5,并让窗口结束
	billing_setting.SetVideoPromoForTest(t, map[string]billing_setting.VideoPromo{
		model: {Factors: map[string]float64{"1080p": 0.5}, StartAt: 100, EndAt: 200},
	}, 999)

	if !approx(snapshot["video_promo"], 0.72) {
		t.Fatalf("snapshot mutated to %v after config change", snapshot["video_promo"])
	}
	if !approx(submitted["video_promo"], 0.72) {
		t.Fatalf("returned map mutated to %v after config change", submitted["video_promo"])
	}
	if info.PriceData.VideoBilling.PromoFactor != 0.72 {
		t.Fatalf("display snapshot mutated to %v", info.PriceData.VideoBilling.PromoFactor)
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `go test ./relay/channel/task/seedance/ -run TestInFlight -v`
Expected: PASS(现有机制已正确,此测试应一次通过。**若失败,说明结算路径重新读了配置,那是必须修的 bug**)。

- [ ] **Step 3: 人工确认结算路径不读配置**

Run: `grep -n "ResolveVideoPromo\|GetVideoPromoCopy" service/task_billing.go relay/relay_task.go`
Expected: **零命中**。结算路径只能连乘持久化快照。若有命中即违反硬约束。

- [ ] **Step 4: Commit**

```bash
git add relay/channel/task/seedance/promo_test.go
git commit -m "test(seedance): 钉住在途任务按提交时折扣快照结算"
```

---

### Task 6: 日志落库原价 / 折扣 / 实收

**Files:**
- Modify: `service/task_billing.go`(预扣 `:51-59`、结算 `:147-160`)

**Interfaces:**
- Consumes: Task 4 的 `VideoBillingDisplay.PromoFactor/PromoStartAt/PromoEndAt`。
- Produces: 日志 `other` 新增 `video_promo_factor`、`video_promo_start_at`、`video_promo_end_at`、`video_net_unit_price`。`video_unit_price` 语义**不变**(仍是原价),历史日志含义不漂移。

> `video_promo` 这个倍率键还会**自动**出现在日志「计算参数」文本里 —— `task_billing.go:29-33` 遍历 `OtherRatios` 且跳过 1.0,无需为此写代码。

- [ ] **Step 1: 抽一个共用 helper** —— 在 `service/task_billing.go` 中新增(放在两处写日志函数之前):

```go
// putVideoPromoFields 把折扣三元组与实收单价写入日志 other。
// video_unit_price 保持原价语义不变,实收由 video_net_unit_price 单列,
// 使账单上「原价 × 折扣 = 实收」可当场核对。未打折时不写任何折扣字段,避免给全价请求增加噪音。
func putVideoPromoFields(other map[string]interface{}, vb *types.VideoBillingDisplay, listUnitPrice float64) {
	if vb == nil || vb.PromoFactor <= 0 || vb.PromoFactor >= 1 {
		return
	}
	other["video_promo_factor"] = vb.PromoFactor
	other["video_promo_start_at"] = vb.PromoStartAt
	other["video_promo_end_at"] = vb.PromoEndAt
	other["video_net_unit_price"] = listUnitPrice * vb.PromoFactor
}
```

- [ ] **Step 2: 预扣点(`:51-59`)** —— 注意每处都是 **if/else 一对**赋值(共 4 处赋值,不是 2 处),必须把 if/else 折叠成一个局部变量再复用:

```go
	if vb := info.PriceData.VideoBilling; vb != nil {
		other["video_resolution_tier"] = vb.ResolutionTier
		other["video_has_input"] = vb.HasVideoInput
		listUnitPrice := vb.BaseUnitUSDPerM * vb.PricingRatio
		if info.PriceData.ModelRatio > 0 {
			listUnitPrice = info.PriceData.ModelRatio * 2.0 * vb.PricingRatio
		}
		other["video_unit_price"] = listUnitPrice
		putVideoPromoFields(other, vb, listUnitPrice)
	}
```

- [ ] **Step 2b: 结算点(`:147-160`)** —— 同样折叠,**保留原有那两行说明有效单价推导的注释**:

```go
		if vb := bc.VideoBilling; vb != nil {
			other["video_resolution_tier"] = vb.ResolutionTier
			other["video_has_input"] = vb.HasVideoInput
			// 有效单价(含管理员加价)= 基准单价 × 倍率 × (modelRatio / (基准单价/2))
			// 简化:有效单价 = modelRatio * 2 * PricingRatio
			listUnitPrice := vb.BaseUnitUSDPerM * vb.PricingRatio
			if bc.ModelRatio > 0 {
				listUnitPrice = bc.ModelRatio * 2.0 * vb.PricingRatio
			}
			other["video_unit_price"] = listUnitPrice
			putVideoPromoFields(other, vb, listUnitPrice)
			if vb.VideoTokens > 0 {
				other["video_tokens"] = vb.VideoTokens
			}
		}
```

两处的 `vb` 类型不同来源(`info.PriceData.VideoBilling` 与 `bc.VideoBilling`)但都是 `*types.VideoBillingDisplay`,helper 签名对两者通用。

- [ ] **Step 3: 验证**

Run: `go build ./... && go test ./service/... ./relay/...`
Expected: PASS。

Run: `grep -n "video_net_unit_price\|video_promo_factor" service/task_billing.go`
Expected: 两处写日志点各命中一次(经 helper)。

- [ ] **Step 4: Commit**

```bash
git add service/task_billing.go
git commit -m "feat(log): 视频账单分列原价、折扣系数与实收单价"
```

---

### Task 7: 保存前校验 + 档位查询接口

**Files:**
- Modify: `controller/option.go`
- Modify: `router/api-router.go`

**Interfaces:**
- Consumes: Task 3 的 `ValidateVideoPromo`;Task 2 的 `seedance.AllModelTiers`、`TiersForModel`。
- Produces: `GET /api/option/video_promo_tiers` → `{"success":true,"data":{"<model>":["base","1080p"]}}`(admin 权限,与其他 option 路由一致)。

- [ ] **Step 1: 加存前校验 case** —— 在 `controller/option.go` 的 `switch option.Key` 里(参照 `:306-341` 既有分支模式)加入:

```go
	case "billing_setting.video_promo":
		value, _ := option.Value.(string)
		if err := billing_setting.ValidateVideoPromo(value); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		if err := validateVideoPromoTiers(value); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
```

- [ ] **Step 2: 档位归属校验 + 查询 handler** —— 在 `controller/option.go` 末尾追加:

```go
// validateVideoPromoTiers 校验每个折扣档位键都是该模型原价矩阵中真实存在的档位。
// 这项校验放在 controller 而非 billing_setting:后者 import seedance 会造成循环导入。
func validateVideoPromoTiers(jsonStr string) error {
	if jsonStr == "" {
		return nil
	}
	var cfg map[string]billing_setting.VideoPromo
	if err := common.UnmarshalJsonStr(jsonStr, &cfg); err != nil {
		return fmt.Errorf("invalid video_promo JSON: %w", err)
	}
	for model, p := range cfg {
		if len(p.Factors) == 0 {
			continue
		}
		tiers := seedance.TiersForModel(model)
		if len(tiers) == 0 {
			return fmt.Errorf("model %s has no video pricing matrix, cannot configure a promo", model)
		}
		allowed := make(map[string]bool, len(tiers))
		for _, tier := range tiers {
			allowed[tier] = true
		}
		for tier := range p.Factors {
			if !allowed[tier] {
				return fmt.Errorf("model %s does not have tier %s (available: %v)", model, tier, tiers)
			}
		}
	}
	return nil
}

// GetVideoPromoTiers 返回各视频模型可配置的计价档位,供后台按模型渲染折扣行,
// 从源头消掉「给只有 base 档的模型配 1080p 折扣」这类无效配置。
func GetVideoPromoTiers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    seedance.AllModelTiers(),
	})
}
```

按需补 import:`fmt`、`github.com/QuantumNous/new-api/common`、`github.com/QuantumNous/new-api/setting/billing_setting`、`github.com/QuantumNous/new-api/relay/channel/task/seedance`。先 `grep -n "billing_setting\|task/seedance" controller/option.go` 确认哪些已在。

- [ ] **Step 3: 注册路由** —— 在 `router/api-router.go` 中找到 option 路由组(`grep -n "option" router/api-router.go`),在同组内加:

```go
			optionRoute.GET("/video_promo_tiers", controller.GetVideoPromoTiers)
```

保持与同组既有路由相同的中间件(admin 鉴权)。

- [ ] **Step 4: 验证**

Run: `go build ./... && go vet ./controller/... ./router/...`
Expected: 无错误。

Run: `go test ./...`
Expected: 全 PASS。

- [ ] **Step 5: 手动验证接口**

启动服务后:
```bash
curl -s -H "Authorization: Bearer <admin-token>" http://localhost:3000/api/option/video_promo_tiers
```
Expected: 返回含 `doubao-seedance-2-5-260628: ["base","1080p"]` 与 `doubao-seedance-2-0-fast-260128: ["base"]` 的 map。

- [ ] **Step 6: Commit**

```bash
git add controller/option.go router/api-router.go
git commit -m "feat(option): video_promo 存前校验与档位查询接口"
```

---

### Task 8: `DateTimePicker` 支持秒级精度

仓库现有控件**全部做不到秒**:`datetime-picker.tsx:82`/`:101` 用 `setHours(h, m, 0, 0)` 把秒硬置零,内部状态是 `'00:00'`(`:66`),`<Input type='time'>`(`:144-150`)未设 `step` 故浏览器只渲染 HH:mm。这是本次唯一需要新写 UI 基础件的地方。

**Files:**
- Create: `web/default/src/components/datetime-picker-time.ts`
- Create: `web/default/src/components/datetime-picker-time.test.ts`
- Modify: `web/default/src/components/datetime-picker.tsx`
- Modify: `web/default/src/lib/format.ts:192-197`

**Interfaces:**
- Consumes: 无。
- Produces:
  - `formatTimeValue(date: Date, withSeconds: boolean): string`
  - `parseTimeValue(value: string): { hours: number; minutes: number; seconds: number }`
  - `DateTimePicker` 新增可选 prop `withSeconds?: boolean`(默认 `false`)。
  - `formatTimestampForInput(timestamp, withSeconds?)`。

> **8 个既有调用点全部只传 `value`/`onChange`/`placeholder`/`id`/`className`**(`edit-ratio-dialog.tsx:521`、`cost-filter.tsx:236,244`、`redemptions-mutate-drawer.tsx:238`、`api-keys-mutate-drawer.tsx:359`、`models-filter-dialog.tsx:200,211`、`announcements-section.tsx:508`、`log-settings-section.tsx:211`),所以默认 `false` 的可选 prop 完全向后兼容。
>
> **下游转换无需改动**:`dateToUnixTimestamp`(`lib/time.ts:32`)是 `Math.floor(getTime()/1000)`,秒位如实保留 —— 今天秒总是 0 纯粹是控件所致。`parseTimestampFromInput`(`format.ts:202-208`)用 `new Date(value)` 已能解析 `:ss`。**注意:仓库不存在 `unixTimestampToDate` 反向 helper**(全 `web/default/src` 零命中),回填 Date 时用 `new Date(ts * 1000)`。

- [ ] **Step 1: 写失败测试** —— 新建 `web/default/src/components/datetime-picker-time.test.ts`(照抄 `components/ui/dropdown-menu.test.tsx` 的 `node:test` 形态;**不要**用 `render()`/`screen`,仓库无 jsdom / testing-library / vitest / jest):

```ts
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { formatTimeValue, parseTimeValue } from './datetime-picker-time'

describe('formatTimeValue', () => {
  test('pads to HH:mm when seconds are disabled', () => {
    assert.equal(formatTimeValue(new Date(2026, 0, 2, 9, 5, 37), false), '09:05')
  })

  test('includes zero-padded seconds when enabled', () => {
    assert.equal(
      formatTimeValue(new Date(2026, 0, 2, 9, 5, 7), true),
      '09:05:07'
    )
  })

  test('keeps midnight stable', () => {
    assert.equal(formatTimeValue(new Date(2026, 0, 2, 0, 0, 0), true), '00:00:00')
  })
})

describe('parseTimeValue', () => {
  test('parses HH:mm with seconds defaulted to zero', () => {
    assert.deepEqual(parseTimeValue('09:05'), {
      hours: 9,
      minutes: 5,
      seconds: 0,
    })
  })

  test('parses HH:mm:ss', () => {
    assert.deepEqual(parseTimeValue('23:59:59'), {
      hours: 23,
      minutes: 59,
      seconds: 59,
    })
  })

  test('falls back to zeros on malformed input', () => {
    assert.deepEqual(parseTimeValue(''), { hours: 0, minutes: 0, seconds: 0 })
    assert.deepEqual(parseTimeValue('ab:cd'), {
      hours: 0,
      minutes: 0,
      seconds: 0,
    })
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd web/default && bunx tsc --noEmit src/components/datetime-picker-time.test.ts 2>&1 | head -5`
Expected: `Cannot find module './datetime-picker-time'`。

- [ ] **Step 3: 写纯逻辑模块** —— 新建 `web/default/src/components/datetime-picker-time.ts`,**先复制任一现有源文件 1–18 行的 AGPL 版权头**(`bun run copyright:check` 对新文件强制),然后:

```ts
/**
 * 时间输入框(`<input type='time'>`)的值与 Date 之间的换算。
 * 抽成独立模块以便用 node:test 覆盖 —— 仓库无 jsdom/testing-library。
 */

/** 把 Date 的时间部分格式化为 `HH:mm` 或 `HH:mm:ss`。 */
export function formatTimeValue(date: Date, withSeconds: boolean): string {
  const pad = (n: number) => n.toString().padStart(2, '0')
  const base = `${pad(date.getHours())}:${pad(date.getMinutes())}`
  return withSeconds ? `${base}:${pad(date.getSeconds())}` : base
}

/** 解析 `HH:mm` 或 `HH:mm:ss`;非法输入一律降级为 0,避免写出 NaN 时间。 */
export function parseTimeValue(value: string): {
  hours: number
  minutes: number
  seconds: number
} {
  const [h, m, s] = value.split(':').map((part) => Number.parseInt(part, 10))
  const safe = (n: number) => (Number.isFinite(n) ? n : 0)
  return { hours: safe(h), minutes: safe(m), seconds: safe(s) }
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd web/default && node --test src/components/datetime-picker-time.test.ts`
Expected: 6 pass, 0 fail。若 `node --test` 不认 TS,改为先 `bunx tsc` 编译或直接 `bun test`(`bun` 原生支持 TS 与 `node:test` 导入);以实际可跑通的为准并记录在提交信息里。

- [ ] **Step 5: 接入 `DateTimePicker`** —— `datetime-picker.tsx` 做 6 处改动:

1. import 加 `import { formatTimeValue, parseTimeValue } from './datetime-picker-time'`(相对导入排在最后一组)。
2. `DateTimePickerProps` 加:
```tsx
  /** 显示并保留秒(`<input type='time' step={1}>`)。默认 false,既有调用点行为不变。 */
  withSeconds?: boolean
```
3. 解构参数加 `withSeconds = false`。
4. `useState<string>('00:00')`(`:66`)改为 `useState<string>(withSeconds ? '00:00:00' : '00:00')`。
5. `useEffect`(`:68-76`)里的手写 pad 改为 `setTime(formatTimeValue(value, withSeconds))`,依赖数组加 `withSeconds`。
6. `handleDateSelect`(`:80-82`)与 `handleTimeChange`(`:99-101`)里的 `time.split(':').map(Number)` + `setHours(hours, minutes, 0, 0)` 改为:
```tsx
        const { hours, minutes, seconds } = parseTimeValue(time)
        const newDate = new Date(selectedDate)
        newDate.setHours(hours, minutes, seconds, 0)
```
7. `handleClear`(`:110`)改为 `setTime(withSeconds ? '00:00:00' : '00:00')`。
8. `<Input type='time'>`(`:144-150`)加 `step={withSeconds ? 1 : undefined}` —— 浏览器需要 `step=1` 才渲染秒位。

- [ ] **Step 6: `formatTimestampForInput` 加带秒变体** —— `lib/format.ts:192-197` 改为:

```ts
/**
 * Format timestamp to date input value (YYYY-MM-DDTHH:mm[:ss])
 */
export function formatTimestampForInput(
  timestamp: number,
  withSeconds = false
): string {
  if (timestamp === -1) {
    return ''
  }
  const mask = withSeconds ? 'YYYY-MM-DDTHH:mm:ss' : 'YYYY-MM-DDTHH:mm'
  return dayjs(timestamp * 1000).format(mask)
}
```

- [ ] **Step 7: 验证**

Run: `cd web/default && bun run typecheck && bun run lint && bun run copyright:check && bun run format:check`
Expected: 全部通过。

> **已知既有问题**:`details-dialog.tsx:433` 现为 86 字符,超过 `printWidth: 80`,`format:check` 可能**在你改动之前就已失败**。先在干净工作区跑一次 `format:check` 记录基线,只对比你引入的新增失败项。

- [ ] **Step 8: Commit**

```bash
git add web/default/src/components/datetime-picker-time.ts web/default/src/components/datetime-picker-time.test.ts web/default/src/components/datetime-picker.tsx web/default/src/lib/format.ts
git commit -m "feat(ui): DateTimePicker 支持秒级精度"
```

---

### Task 9: 后台折扣编辑器

**复用既有模型定价抽屉,不新建页面** —— 折扣是模型的属性,和倍率、计费模式同属一处心智模型。`tiered-pricing-editor.tsx`(在 `model-pricing-sheet.tsx:640` 处使用)已证明嵌套档位列表能在该抽屉内编辑,折扣编辑器沿用同一位置与形态。

**Files:**
- Create: `web/default/src/features/system-settings/models/video-promo-editor.tsx`
- Modify: `.../models/model-pricing-sheet.tsx`(state `:154` 附近、hydrate `:195` 附近、`buildSubmitData` `:437-460`、render `:638-648` 附近)
- Modify: `.../models/model-pricing-core.ts`、`.../models/model-pricing-snapshots.ts`(`ModelPricingSnapshot` / `ModelRatioData` 加 `videoPromo`)
- Modify: `.../models/model-ratio-visual-editor.tsx`(透传 saved/draft 的 `videoPromo`)
- Modify: `.../models/ratio-settings-card.tsx`(`apiKeyMap` `:315-318`)
- Modify: `web/default/src/features/system-settings/api.ts`、`./types.ts`(档位查询)

**Interfaces:**
- Consumes: `GET /api/option/video_promo_tiers`(Task 7);`DateTimePicker` 的 `withSeconds`(Task 8)。
- Produces: 表单提交时把 `billing_setting.video_promo` 整张 map 序列化为 JSON 字符串,经既有脏检查机制 PUT。

- [ ] **Step 1: 加档位查询 API** —— `features/system-settings/api.ts` 追加(照 `getUpstreamChannels` 形态):

```ts
export async function getVideoPromoTiers() {
  const res = await api.get<VideoPromoTiersResponse>(
    '/api/option/video_promo_tiers'
  )
  return res.data
}
```

`./types.ts` 加:

```ts
export type VideoPromoTiersResponse = {
  success: boolean
  message: string
  data: Record<string, string[]>
}

export type VideoPromoConfig = {
  factors: Record<string, number>
  start_at: number
  end_at: number
}
```

- [ ] **Step 2: 写折扣编辑器** —— 新建 `video-promo-editor.tsx`。**先复制 AGPL 版权头(1–18 行)**。要点:

- `const { t } = useTranslation()`(子组件必须自己调,不能靠父组件)。
- Props:
```tsx
type VideoPromoEditorProps = {
  modelName?: string
  /** 该模型在原价矩阵中真实存在的档位,由后端给出。 */
  tiers: string[]
  value: VideoPromoConfig | null
  onChange: (next: VideoPromoConfig | null) => void
}
```
- **档位行由 `tiers` 驱动**,不让运营手输档位名 —— 从源头消掉「给只有 base 档的 fast 配 1080p 折扣」。`tiers` 为空时整块渲染一条提示(`t('This model has no video pricing tiers')`)并禁用编辑。
- 档位标签复用账单侧口径:`base` 显示为 `t('480p / 720p')`,其余原样(`1080p`、`4K`)。
- 每行一个系数输入,**留空 = 该档不打折**(从 `factors` 中删键,而非写 0)。
- 每行实时显示 `原价 → 折后价` 预览,避免心算。原价从后端矩阵拿不到,故预览按**相对折扣百分比**呈现即可:`t('{{percent}}% off', { percent })`,其中 `percent = Math.round((1 - factor) * 1000) / 10`。这样无需再开一个原价查询接口。
- 活动时间两个 `DateTimePicker withSeconds`,`value={startAt ? new Date(startAt * 1000) : undefined}`,`onChange` 经 `dateToUnixTimestamp` 写回。**注意仓库无 `unixTimestampToDate`,直接 `new Date(ts * 1000)`。**
- 窗口状态提示:比较 `Math.floor(Date.now() / 1000)` 与窗口,渲染 `t('Not started')` / `t('Active')` / `t('Ended')` 三态 —— 否则运营无法判断「填了系数为何账单没折」。
- **JSON 兜底模式**:照 `channel-affinity/index.tsx` 的四段式先例(state `:146`、双向转换 `:365-395`、双 Button 切换 `:455-469`、条件渲染 `:541`)。关键是 **JSON→visual 必须校验并在失败时中止**(留在 JSON 模式 + `toast.error`),不能让非法文本静默摧毁结构化状态。`'Visual'` 走 `t()`,`JSON` 保留裸字面量(与先例一致,`sync-i18n` 的 `BRAND_AND_LITERAL_KEYS` 会放行)。

- [ ] **Step 3: 接入抽屉** —— `model-pricing-sheet.tsx`:

1. state 加 `const [videoPromo, setVideoPromo] = useState<VideoPromoConfig | null>(null)`(挨着 `:154` 的 `billingExpr`)。
2. hydrate 里加 `setVideoPromo(editData.videoPromo ?? null)`(挨着 `:195`)。
3. `buildSubmitData`(`:437-460`)加 `data.videoPromo = videoPromo ?? undefined`,并把 `videoPromo` 加进 `useCallback` 依赖数组。**注意折扣与 `pricingMode` 无关**,不要放进 `if (pricingMode === 'tiered_expr')` 分支里 —— 按 token 计费与按次计费的视频模型都可能打折。
4. 用 `useQuery` 拉 `getVideoPromoTiers()`,把 `data[watchedValues.name] ?? []` 传给编辑器。
5. 在 `TieredPricingEditor` 所在区域之外**独立成块**渲染(折扣对所有计费模式都适用,不能只挂在 `tiered_expr` tab 下)。

- [ ] **Step 4: 打通存取链路**

- `model-pricing-snapshots.ts`:`ModelPricingSnapshotInput` 加 `videoPromo: string`;`ModelPricingSnapshot` / `ModelRow` 加 `videoPromo?: VideoPromoConfig`;`buildModelSnapshots` 里 `safeJsonParse` 该 map 后按模型名取值;`getSnapshotSignature` 要把 `videoPromo` 纳入签名,否则只改折扣时脏检查不认。
- `model-ratio-visual-editor.tsx`:props 加 `savedVideoPromo` / `videoPromo` 两个字符串,传进两次 `buildModelSnapshots`,并加入那个 20 项的 `useMemo` 依赖数组。
- `ratio-settings-card.tsx`:`normalized` 加 `VideoPromo: normalizeJsonString(values.VideoPromo)`,`apiKeyMap`(`:315-318`)加 `VideoPromo: 'billing_setting.video_promo'`。既有按字段脏检查(`:320-334`)会自动跳过未改动的键。

- [ ] **Step 5: 验证**

Run: `cd web/default && bun run typecheck && bun run lint`
Expected: 通过。

- [ ] **Step 6: 手动验证(必须真跑一遍)**

Run: `cd web/default && bun run dev`

按顺序确认:
1. 打开 系统设置 → 模型定价,编辑 `doubao-seedance-2-5-260628` → 折扣区**只出现 `480p / 720p` 与 `1080p` 两行**。
2. 编辑 `doubao-seedance-2-0-fast-260128` → **只出现一行 `480p / 720p`**。
3. 1080p 填 `0.72`,时间选择器能选到**秒**(不是只有时分),保存成功。
4. 刷新页面,值正确回填(含秒)。
5. 故意填 `1.5` 保存 → 后端拒绝并弹出 `factor 1.5 must be within (0, 1]`。
6. 只填系数不填时间 → 后端拒绝并提示 `start_at and end_at are both required`。
7. 切到 JSON 模式,填非法 JSON 再切回 visual → 停在 JSON 模式并 toast 报错,结构化数据未丢。

- [ ] **Step 7: Commit**

```bash
git add web/default/src/features/system-settings/
git commit -m "feat(web): 模型定价抽屉内新增视频限时折扣编辑器"
```

---

### Task 10: 账单详情展示原价 / 折扣 / 实收

**Files:**
- Modify: `web/default/src/features/usage-logs/types.ts:201-204`
- Modify: `web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx`(`VideoPricingBreakdown` `:403-465`)
- Modify: `web/default/src/features/usage-logs/data/schema.ts:36-37`(若该处也声明了 `video_*`)

**Interfaces:**
- Consumes: Task 6 落库的 `video_promo_factor`、`video_promo_start_at`、`video_promo_end_at`、`video_net_unit_price`。
- Produces: 仅 UI。

- [ ] **Step 1: 补类型** —— `types.ts` 在 `video_*` 四字段(`:201-204`)后追加:

```ts
  video_promo_factor?: number
  video_promo_start_at?: number
  video_promo_end_at?: number
  video_net_unit_price?: number
```

- [ ] **Step 2: 顺手去掉重复定义** —— `tierLabelMap` 在 `details-dialog.tsx:423-427` 与 `:483-487` **逐字重复**。提到模块作用域一份(在 `VideoPricingBreakdown` 之前),两处改为引用。因为含 `t()`,写成工厂函数:

```tsx
// 计价档位标签,账单与任务详情两处共用。
const buildTierLabelMap = (
  t: (key: string) => string
): Record<string, string> => ({
  base: t('480p / 720p'),
  '1080p': '1080p',
  '4k': '4K',
})
```

- [ ] **Step 3: 补折扣行** —— `VideoPricingBreakdown` 里,在现有 `Unit Price` 行(`:440`)之后插入。**未打折时不渲染任何折扣行**,避免给全价请求增加噪音:

```tsx
  const promo = other.video_promo_factor
  const hasPromo = promo != null && Number.isFinite(promo) && promo > 0 && promo < 1
  if (hasPromo) {
    const net = other.video_net_unit_price ?? unit * promo
    const percent = Math.round((1 - promo) * 1000) / 10
    rows.push({
      label: t('Promo Discount'),
      value: `${promo.toFixed(4)}x (${t('{{percent}}% off', { percent })})`,
    })
    rows.push({ label: t('Net Unit Price'), value: `${fmtPrice(net)}/M` })
    if (other.video_promo_start_at && other.video_promo_end_at) {
      rows.push({
        label: t('Promo Window'),
        value: `${formatDateTimeObject(new Date(other.video_promo_start_at * 1000))} ~ ${formatDateTimeObject(new Date(other.video_promo_end_at * 1000))}`,
      })
    }
  }
```

现有 `Unit Price` 行的标签改为 `t('List Unit Price')` 以和实收区分(`video_unit_price` 语义未变,仍是原价)。计费公式行(`:457-463`)在打折时把 `fmtPrice(unit)` 换成 `fmtPrice(net)`,否则公式左右算不平。

`formatDateTimeObject` 从 `@/lib/time` 导入(已存在,`time.ts:152-154`,掩码 `'YYYY-MM-DD HH:mm:ss'`,天然带秒)。

- [ ] **Step 4: 验证**

Run: `cd web/default && bun run typecheck && bun run lint`
Expected: 通过。

- [ ] **Step 5: Commit**

```bash
git add web/default/src/features/usage-logs/
git commit -m "feat(web): 账单详情分列原价、折扣与实收单价"
```

---

### Task 11: i18n 同步与全量验证

**Files:**
- Modify: `web/default/src/i18n/locales/{en,zh,fr,ru,ja,vi}.json`(经脚本,**不要手改**)

- [ ] **Step 1: 同步 i18n**

Run: `cd web/default && bun run i18n:sync`
Expected: 新 key 落入 6 个 locale。新增 key 清单(约 12 条):`480p / 720p`(已存在)、`Promo Discount`、`Net Unit Price`、`List Unit Price`、`Promo Window`、`{{percent}}% off`、`Not started`、`Active`、`Ended`、`This model has no video pricing tiers`、`Promo Factor`、`Promo Period`。

- [ ] **Step 2: 检查 `zh.json` 已翻译** —— 脚本只保证 key 存在,中文需人工确认非英文残留:

Run: `cd web/default && node -e "const z=require('./src/i18n/locales/zh.json').translation; ['Promo Discount','Net Unit Price','List Unit Price','Promo Window','Not started','Ended'].forEach(k=>console.log(k,'=>',z[k]))"`
Expected: 均为中文。若仍是英文,手工补该几条(`限时折扣` / `实收单价` / `原价单价` / `活动窗口` / `未开始` / `已结束`)。

- [ ] **Step 3: 后端全量验证**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全 PASS。

- [ ] **Step 4: 前端全量验证**

Run: `cd web/default && bun run typecheck && bun run lint && bun run copyright:check && bun run build`
Expected: 全部通过。`format:check` 与 Task 8 记录的基线对比,不得引入新增失败项。

- [ ] **Step 5: 空配置回归确认(最高优先级验收标准)**

Run: `go test ./relay/channel/task/seedance/ -run 'TestEmptyPromoConfigIsByteIdentical|TestBaseRatioIsOne' -v`
Expected: PASS —— 证明 `video_promo` 为空时行为与改动前逐键一致。

- [ ] **Step 6: 端到端手动验证**

1. 后台给 `doubao-seedance-2-5-260628` 配 `{"1080p": 0.72}`,窗口涵盖当前时刻。
2. 提交一个 1080p 视频任务,在日志详情里确认:`Resolution Tier = 1080p`、`List Unit Price` 为原价、`Promo Discount = 0.7200x (28% off)`、`Net Unit Price = 原价 × 0.72`、计算参数里出现 `video_promo`。
3. 提交一个 480p 任务,确认**不出现任何折扣行**,扣费与配置前一致。
4. 用 `doubao-seedance-2-0-fast-260128` 配 `{"base": 0.9}` 提交 480p 任务,确认 `video_promo = 0.9` 真实生效(这是折扣若乘进矩阵就会静默失效的场景)。

- [ ] **Step 7: Commit**

```bash
git add web/default/src/i18n/
git commit -m "chore(i18n): 同步视频折扣相关文案"
```

---

## 完成后:写 Skill

**实施顺序是刻意的 —— Skill 描述一个已验证过的流程,而非设想的流程。** 上述 11 个任务全绿后,再单独起一个 session 写 `.claude/skills/seedance-new-model/SKILL.md`,固化为检查单:

1. `relay/channel/task/seedance/pricing.go` 原价矩阵加模型条目(**只填原价**)
2. `relay/channel/task/{doubao,seedance3rd}/constants.go` 模型列表加名字
3. `dreamina-*` 另加默认倍率(`setting/ratio_setting/model_ratio.go:283`,值 = USD单价 ÷ 2);`doubao-*` 刻意不设默认值,由管理员配置为**倍率而非固定价**(否则 `PerCallBilling` 会跳过 token 阶梯结算,见 `model_ratio.go:287-291`)
4. 同步更新 `pricing_test.go` **与** `sora/seedance2_test.go`(两处都断言倍率)
5. 新档位要确认 `ClassifyResTier`(`pricing.go:68`)能识别该分辨率字符串
6. 有限时折扣则**在后台配,不动代码**

易踩空的约束(写进 Skill 的警告区):

- **绝不把折扣乘进矩阵单价** —— base 档会因分子分母同时缩放而静默失效,且 `TestBaseRatioIsOne` 会把错误结果断言为正确
- fast/mini 类精简型号通常无 1080p/4k 档,不要凭空补价 —— 查官方文档确认
- 新模型名需确认走 `doubao-*` 还是 `dreamina-*` 前缀,两者默认倍率处理方式不同

