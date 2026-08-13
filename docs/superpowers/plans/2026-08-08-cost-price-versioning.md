# 成本核算价格版本化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复成本核算两个正确性 bug——供应商改价后历史成本被重算、用户折扣列查的是当前配置而非历史值。

**Architecture:** 新建 `channel_cost_versions` 表记录渠道历史价格版本；`addBatch()` 热路径逐条日志按 `log.CreatedAt` 二分查版本并当场算 `CostCny`；用户折扣改从日志 `other.group_ratio`/`user_group_ratio` 取历史值，删除 `resolveUserDiscount` 配置查询链路。

**Tech Stack:** Go 1.22+, Gin, GORM v2, SQLite/MySQL/PostgreSQL, React 19, TypeScript, TanStack Query, i18next, Semi Design (classic)

## Global Constraints

- JSON marshal/unmarshal 必须用 `common.Marshal`/`common.Unmarshal`（Rule 1）
- 所有 DB 代码必须兼容 SQLite / MySQL >= 5.7.8 / PostgreSQL >= 9.6；纯 GORM，无裸 SQL，无保留字列名（Rule 2）
- 前端包管理器用 bun（Rule 3）
- 不修改、不删除任何 new-api / QuantumNous 相关品牌信息（Rule 5）
- 上游转发 DTO 可选标量字段用指针 + omitempty（Rule 6）；本次改动不涉及 relay 路径

---

## File Map

| 文件 | 动作 | 说明 |
|---|---|---|
| `model/channel_cost_version.go` | 新建 | `ChannelCostVersion` struct + GORM model + `VersionMap` + `RatioAt` + `GetAllChannelCostVersions` + CRUD helpers |
| `model/main.go` | 修改 | `AutoMigrate` 加 `&ChannelCostVersion{}` + 调用 `seedChannelCostVersions` |
| `model/channel.go` | 修改 | 删除 `GetAllUserGroups`；`GetAllChannelCostInfos` 不变 |
| `controller/cost_stat.go` | 修改 | `costCubeRow` 新字段；`addBatch` 下沉定价；删除 `resolveUserDiscount`/`attachUserGroupRatios`/`effectiveChannelRatio`；`costMoneyFromRow` 去掉 ratio 参数；`costMoney` 新增字段；`foldCostCube` 改造 |
| `controller/cost_query.go` | 修改 | `buildCostCube` 换 versions；`costCubeCacheEntry` 去 userGroups 加 versions；缓存键不变 |
| `controller/cost_versions.go` | 新建 | 版本管理 API handlers（GET/POST/DELETE） |
| `controller/channel.go` | 修改 | `UpdateChannel` 保存时检测价格变化并追加版本 |
| `router/api-router.go` | 修改 | `costRoute` 新增 3 条版本路由 |
| `controller/cost_stat_test.go` | 修改 | 删除旧 `TestEffectiveChannelRatio`；更新所有依赖 `testChannels()`/`addBatch` 的测试 |
| `controller/cost_stat_version_test.go` | 新建 | `VersionMap.RatioAt` 单测 + 跨版本 `addBatch` 单测 + discount 冻结汇率单测 |
| `controller/cost_integration_test.go` | 修改 | 更新集成测试以适配新签名 |
| `web/default/src/features/cost/types.ts` | 修改 | 删旧 group_ratio* 字段；加 ratio_mixed/discount_mixed/discount_special/discount_coverage/effective_ratio_known |
| `web/default/src/features/cost/api.ts` | 修改 | 加版本管理 API 函数 |
| `web/default/src/features/cost/lib.ts` | 修改 | `mergeBreakdown` 删 group_ratio* carry；`ZERO_MONEY` 加新字段 |
| `web/default/src/features/cost/components/edit-ratio-dialog.tsx` | 修改 | 扩为两段：当前价 + 版本历史列表 + 补录入口 |
| `web/default/src/features/cost/components/cost-user-cells.tsx` | 修改 | `UserDiscountCell` hover 改写；`PricingCellRow` 类型更新；`CostRatioDiscountCell` 加 ratio_mixed 展示 |
| `web/default/src/i18n/locales/en.json` | 修改 | 补约 12 个新键 |
| `web/default/src/i18n/locales/zh.json` | 修改 | 补对应中文 |
| `web/classic/src/components/cost/costMerge.js` | 修改 | 删 group_ratio* carry fields |
| `web/classic/src/components/cost/CostUserCells.jsx` | 修改 | `UserDiscountCell` hover 改写 |
| `web/classic/src/components/cost/CostTables.jsx` | 修改 | 倍率列加 ratio_mixed 展示 |
| `web/classic/src/components/cost/CostVersionModal.jsx` | 新建 | 版本历史 Semi Modal（在成本页供应商维度倍率编辑处触发） |

---

### Task 1: ChannelCostVersion 模型与 VersionMap

**Files:**
- Create: `model/channel_cost_version.go`
- Modify: `model/main.go`

**Interfaces:**
- Produces:
  - `type ChannelCostVersion struct`
  - `type VersionMap map[int][]ChannelCostVersion`
  - `func (v VersionMap) RatioAt(channelId int, ts int64) (float64, bool)`
  - `func GetAllChannelCostVersions() (VersionMap, error)`
  - `func GetChannelCostVersions(channelId int) ([]ChannelCostVersion, error)`
  - `func CreateChannelCostVersion(v *ChannelCostVersion) error`
  - `func DeleteChannelCostVersion(id int) error`
  - `func VersionExists(channelId int, effectiveFrom int64) (bool, error)`

- [ ] **Step 1: 新建 `model/channel_cost_version.go`（struct + RatioAt）**

```go
package model

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ChannelCostVersion 渠道成本计价历史版本。
// EffectiveFrom 秒级 Unix 时间戳（闭区间起点）；0 = 初始版本（自古以来）。
// 版本区间 = [EffectiveFrom, 下一版本.EffectiveFrom)。
// 版本行不可变：改价追加新行，误填 DELETE 后重加。
type ChannelCostVersion struct {
	Id            int     `json:"id"             gorm:"primaryKey"`
	ChannelId     int     `json:"channel_id"     gorm:"index:idx_channel_effective,priority:1;not null"`
	EffectiveFrom int64   `json:"effective_from" gorm:"index:idx_channel_effective,priority:2;not null"`
	// CostMode: "ratio"（CNY:USD 倍率）| "discount"（刊例折扣）。空值等同 ratio。
	CostMode     string  `json:"cost_mode"     gorm:"type:varchar(16)"`
	CostRatio    float64 `json:"cost_ratio"`
	CostDiscount float64 `json:"cost_discount"`
	// ExchangeRate discount 模式冻结结算汇率，不随查询汇率浮动。
	ExchangeRate float64 `json:"exchange_rate"`
	Note         string  `json:"note"       gorm:"type:varchar(255)"`
	CreatedAt    int64   `json:"created_at"`
	CreatedBy    int     `json:"created_by"`
}

// VersionMap channelId → 按 EffectiveFrom 升序排列的版本切片。
type VersionMap map[int][]ChannelCostVersion

// RatioAt 返回 channelId 在时间戳 ts 时刻的有效 CNY:USD 倍率。
// discount 模式返回 CostDiscount × 版本自带 ExchangeRate（不碰查询汇率）。
// 无版本或值为 0 → 0, false。
func (v VersionMap) RatioAt(channelId int, ts int64) (float64, bool) {
	versions := v[channelId]
	if len(versions) == 0 {
		return 0, false
	}
	// versions 升序；找最后一个 EffectiveFrom <= ts 的版本。
	idx := sort.Search(len(versions), func(i int) bool {
		return versions[i].EffectiveFrom > ts
	}) - 1
	if idx < 0 {
		return 0, false
	}
	ver := versions[idx]
	if ver.CostMode == "discount" {
		r := ver.CostDiscount * ver.ExchangeRate
		if r <= 0 {
			return 0, false
		}
		return r, true
	}
	if ver.CostRatio <= 0 {
		return 0, false
	}
	return ver.CostRatio, true
}
```

- [ ] **Step 2: 继续追加 `model/channel_cost_version.go`（CRUD helpers）**

在文件末尾追加：

```go
// GetAllChannelCostVersions 一次性载入全部版本（主库），升序，供 buildCostCube 缓存。
func GetAllChannelCostVersions() (VersionMap, error) {
	var rows []ChannelCostVersion
	if err := DB.Order("effective_from asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(VersionMap, len(rows))
	for _, r := range rows {
		m[r.ChannelId] = append(m[r.ChannelId], r)
	}
	return m, nil
}

// GetChannelCostVersions 查单渠道版本，降序（最新在前），供 UI 展示。
func GetChannelCostVersions(channelId int) ([]ChannelCostVersion, error) {
	var rows []ChannelCostVersion
	err := DB.Where("channel_id = ?", channelId).
		Order("effective_from desc").Find(&rows).Error
	return rows, err
}

// CreateChannelCostVersion 追加新版本（CreatedAt 由函数填充）。
func CreateChannelCostVersion(v *ChannelCostVersion) error {
	v.CreatedAt = common.GetTimestamp()
	return DB.Create(v).Error
}

// DeleteChannelCostVersion 删除指定版本（幂等）。
func DeleteChannelCostVersion(id int) error {
	return DB.Where("id = ?", id).Delete(&ChannelCostVersion{}).Error
}

// VersionExists 检查同渠道同 effective_from 是否已有版本，用于 409 冲突检测。
func VersionExists(channelId int, effectiveFrom int64) (bool, error) {
	var count int64
	err := DB.Model(&ChannelCostVersion{}).
		Where("channel_id = ? AND effective_from = ?", channelId, effectiveFrom).
		Count(&count).Error
	return count > 0, err
}

// seedChannelCostVersions 迁移回填：对已配置成本计价但版本表无记录的渠道，
// 插入 effective_from=0 的初始版本。幂等，重复调用跳过已有渠道。
func seedChannelCostVersions() error {
	type cid struct{ ChannelId int }
	var existing []cid
	if err := DB.Model(&ChannelCostVersion{}).Select("channel_id").
		Group("channel_id").Find(&existing).Error; err != nil {
		return err
	}
	seeded := make(map[int]bool, len(existing))
	for _, r := range existing {
		seeded[r.ChannelId] = true
	}
	var channels []Channel
	if err := DB.Select("id", "setting").Find(&channels).Error; err != nil {
		return err
	}
	er := operation_setting.USDExchangeRate
	if er <= 0 {
		er = 7.3
	}
	for _, ch := range channels {
		if seeded[ch.Id] || ch.Setting == nil || *ch.Setting == "" {
			continue
		}
		var s dto.ChannelSettings
		if err := common.UnmarshalJsonStr(*ch.Setting, &s); err != nil {
			continue
		}
		hasCost := (s.CostMode == "discount" && s.CostDiscount > 0) ||
			(s.CostMode != "discount" && s.CostRatio > 0)
		if !hasCost {
			continue
		}
		v := &ChannelCostVersion{
			ChannelId: ch.Id, EffectiveFrom: 0,
			CostMode: s.CostMode, CostRatio: s.CostRatio,
			CostDiscount: s.CostDiscount, ExchangeRate: er,
			Note: "migrated from channel settings",
		}
		if err := CreateChannelCostVersion(v); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: 修改 `model/main.go` — AutoMigrate + seed**

在 `model/main.go` 第 266 行附近的 `DB.AutoMigrate(` 列表末尾（`&PerfMetric{}` 行后）加：

```go
		&ChannelCostVersion{},
```

在 AutoMigrate 成功后（第 293 行附近，`common.UsingSQLite` 判断上方）加：

```go
	if err := seedChannelCostVersions(); err != nil {
		common.SysError("seedChannelCostVersions: " + err.Error())
	}
```

- [ ] **Step 4: 编译验证**

```bash
cd D:/Project/new-api && go build ./model/... 2>&1
```

期望：无错误。

- [ ] **Step 5: Commit**

```bash
cd D:/Project/new-api
git add model/channel_cost_version.go model/main.go
git commit -m "feat(cost): ChannelCostVersion model, VersionMap.RatioAt, seed migration"
```
---

### Task 2: VersionMap 单元测试

**Files:**
- Create: `controller/cost_stat_version_test.go`

**Interfaces:**
- Consumes: `model.VersionMap.RatioAt`（Task 1）

- [ ] **Step 1: 新建 `controller/cost_stat_version_test.go`**

```go
package controller

import (
	"testing"
	"github.com/QuantumNous/new-api/model"
)

func mkVersionMap(versions []model.ChannelCostVersion) model.VersionMap {
	m := make(model.VersionMap)
	for _, v := range versions {
		m[v.ChannelId] = append(m[v.ChannelId], v)
	}
	return m
}

func TestVersionMap_NoVersions(t *testing.T) {
	if _, ok := mkVersionMap(nil).RatioAt(1, 1000); ok {
		t.Fatal("want false")
	}
}

func TestVersionMap_SingleRatio(t *testing.T) {
	vm := mkVersionMap([]model.ChannelCostVersion{
		{ChannelId: 1, EffectiveFrom: 0, CostMode: "ratio", CostRatio: 2.5},
	})
	r, ok := vm.RatioAt(1, 9999999999)
	if !ok || r != 2.5 {
		t.Fatalf("want (2.5,true), got (%v,%v)", r, ok)
	}
}

func TestVersionMap_BeforeFirstVersion(t *testing.T) {
	vm := mkVersionMap([]model.ChannelCostVersion{
		{ChannelId: 1, EffectiveFrom: 1000, CostMode: "ratio", CostRatio: 2.5},
	})
	if _, ok := vm.RatioAt(1, 500); ok {
		t.Fatal("before first version must return false")
	}
}

func TestVersionMap_CrossVersionBoundary(t *testing.T) {
	vm := mkVersionMap([]model.ChannelCostVersion{
		{ChannelId: 3, EffectiveFrom: 0, CostMode: "ratio", CostRatio: 2.5},
		{ChannelId: 3, EffectiveFrom: 2000, CostMode: "ratio", CostRatio: 2.3},
	})
	if r, _ := vm.RatioAt(3, 1999); r != 2.5 {
		t.Fatalf("before boundary: want 2.5, got %v", r)
	}
	if r, _ := vm.RatioAt(3, 2000); r != 2.3 {
		t.Fatalf("at boundary: want 2.3, got %v", r)
	}
}

func TestVersionMap_DiscountFrozenRate(t *testing.T) {
	vm := mkVersionMap([]model.ChannelCostVersion{
		{ChannelId: 5, EffectiveFrom: 0, CostMode: "discount", CostDiscount: 0.8, ExchangeRate: 6.8},
	})
	r, ok := vm.RatioAt(5, 9999)
	if !ok {
		t.Fatal("want ok=true")
	}
	want := 0.8 * 6.8
	if d := r - want; d > 1e-9 || d < -1e-9 {
		t.Fatalf("want %v, got %v", want, r)
	}
}

func TestVersionMap_ZeroRatioUnpriced(t *testing.T) {
	if _, ok := mkVersionMap([]model.ChannelCostVersion{
		{ChannelId: 7, EffectiveFrom: 0, CostMode: "ratio", CostRatio: 0},
	}).RatioAt(7, 9999); ok {
		t.Fatal("zero ratio must return ok=false")
	}
}

// VersionAt 返回版本本体，供 addBatch 记录"命中了哪个版本"以判定跨版本。
// 与 RatioAt 必须选中同一个版本——两者共用查找逻辑，此测试锁死这一点。
func TestVersionMap_VersionAt_IdentifiesVersion(t *testing.T) {
	vm := mkVersionMap([]model.ChannelCostVersion{
		{Id: 1, ChannelId: 3, EffectiveFrom: 0, CostMode: "ratio", CostRatio: 2.5},
		{Id: 2, ChannelId: 3, EffectiveFrom: 2000, CostMode: "ratio", CostRatio: 2.3},
	})
	v1, ok := vm.VersionAt(3, 1999)
	if !ok || v1.Id != 1 {
		t.Fatalf("at 1999: want version id 1, got id=%d ok=%v", v1.Id, ok)
	}
	v2, ok := vm.VersionAt(3, 2000)
	if !ok || v2.Id != 2 {
		t.Fatalf("at 2000: want version id 2, got id=%d ok=%v", v2.Id, ok)
	}
	if _, ok := vm.VersionAt(3, -1); ok {
		t.Fatal("before first version must return ok=false")
	}
}

// EffectiveRatio 是 RatioAt 的底层换算；两条路径必须给出同一个数。
func TestChannelCostVersion_EffectiveRatio(t *testing.T) {
	ratioVer := model.ChannelCostVersion{CostMode: "ratio", CostRatio: 2.5}
	if r, ok := ratioVer.EffectiveRatio(); !ok || r != 2.5 {
		t.Fatalf("ratio mode: want (2.5,true), got (%v,%v)", r, ok)
	}
	discVer := model.ChannelCostVersion{CostMode: "discount", CostDiscount: 0.8, ExchangeRate: 6.8}
	r, ok := discVer.EffectiveRatio()
	if !ok {
		t.Fatal("discount mode: want ok=true")
	}
	if d := r - 0.8*6.8; d > 1e-9 || d < -1e-9 {
		t.Fatalf("discount mode: want %v, got %v", 0.8*6.8, r)
	}
	// discount 模式但汇率为 0 → 无法定价
	if _, ok := (model.ChannelCostVersion{
		CostMode: "discount", CostDiscount: 0.8, ExchangeRate: 0,
	}).EffectiveRatio(); ok {
		t.Fatal("discount with zero exchange rate must return ok=false")
	}
}
```

- [ ] **Step 2: 运行测试（先失败，Task 1 完成后再过）**

```bash
cd D:/Project/new-api && go test ./controller/... -run TestVersionMap -v 2>&1
```

Task 1 完成后期望：6 个 `TestVersionMap_*` 全 PASS。

- [ ] **Step 3: Commit**

```bash
cd D:/Project/new-api
git add controller/cost_stat_version_test.go
git commit -m "test(cost): VersionMap.RatioAt unit tests"
```

---

### Task 3: costCubeRow 改造 + addBatch 下沉定价 + 删旧链路

**Files:**
- Modify: `controller/cost_stat.go`

**Interfaces:**
- Consumes: `model.VersionMap.RatioAt`（Task 1）
- Produces:
  - `costCubeRow` 新增 `CostCny float64`, `UnpricedListQuota float64`, `DiscountWeightedSum float64`, `DiscountListBasis float64`, `DiscountSpecialSum float64`, `DiscountFirstRatio float64`, `DiscountMixed bool`
  - `costCubeRow.QuotaByGroup` **删除**
  - `addGroupQuota` / `mergeGroupQuota` **删除**
  - `resolveUserDiscount` / `userDiscount` / `attachUserGroupRatios` **删除**
  - `effectiveChannelRatio` **删除**
  - `costDimensionRow` / `costBreakdownRow` 的 `UserGroup/GroupRatio/GroupRatioKnown/GroupRatioSpecial/GroupRatioMixed/UsingGroupQuota` **删除**
  - `func (c *costCube) addBatch(logs []*model.Log, versions model.VersionMap)` 签名改变（加 versions 参数）

- [ ] **Step 1: 修改 `controller/cost_stat.go` — costCubeRow 结构体**

将 `costCubeRow` 定义（约第 25-50 行）改为：

```go
type costCubeRow struct {
    Quota               float64
    ListQuota           float64
    RefundQuota         float64
    CostCny             float64 // 按版本逐条定价后累加
    UnpricedListQuota   float64 // 无版本可用的刊例敞口（用于 Priced 判定）
    PromptTokens        int
    CompletionTokens    int
    RequestCount        int
    ErrorCount          int
    CacheReadTokens     int
    CacheCreationTokens int
    FrtSumMs            float64
    FrtCount            int
    // 用户折扣信号（从日志 other 取历史值，替代 QuotaByGroup 配置查询链路）
    DiscountWeightedSum float64 // Σ(历史折扣 × listQ)
    DiscountListBasis   float64 // 有有效折扣信息的 listQ 之和（退款同步冲减）
    DiscountSpecialSum  float64 // 命中专属倍率（user_group_ratio 有效）的 listQ 之和
    DiscountFirstRatio  float64 // 第一个出现的折扣值（避免 float64 map key 精度问题）
    DiscountMixed       bool    // 区间内出现过 >1 个不同折扣值
    // DiscountTotalBasis 覆盖率分母：所有消费/退款行的 listQ 净额（无论有无折扣信息）。
    // 不能直接用 ListQuota 当分母——退款会让 ListQuota 减小而 DiscountListBasis
    // 若不同步冲减，覆盖率会 > 1。两者必须走同一套加减规则。
    DiscountTotalBasis float64
    // RatioVersionSeen 记录本格子命中过的版本 EffectiveFrom 集合；len > 1 即区间内改过价。
    // 用 int64 作 map key 安全（EffectiveFrom 是精确整数，无浮点精度问题）。
    RatioVersionSeen map[int64]struct{}
}
```

删除 `addGroupQuota` 方法和 `mergeGroupQuota` 内联函数（后者在 `foldCostCube` 里）。

**关于 `RatioVersionSeen`**：`VersionMap.RatioAt` 目前只返回 `(ratio, ok)`，拿不到版本身份。需要在 Task 1 的 `model/channel_cost_version.go` 里**追加**一个方法（不改 `RatioAt`，避免破坏 Task 2 的测试）：

```go
// VersionAt 返回 channelId 在 ts 时刻生效的版本本体，供调用方识别"跨了哪几个版本"。
// 与 RatioAt 共用同一套查找逻辑；ok=false 时第一个返回值无意义。
func (v VersionMap) VersionAt(channelId int, ts int64) (ChannelCostVersion, bool) {
	versions := v[channelId]
	if len(versions) == 0 {
		return ChannelCostVersion{}, false
	}
	idx := sort.Search(len(versions), func(i int) bool {
		return versions[i].EffectiveFrom > ts
	}) - 1
	if idx < 0 {
		return ChannelCostVersion{}, false
	}
	return versions[idx], true
}

// EffectiveRatio 返回该版本换算后的 CNY:USD 倍率（discount 模式乘冻结汇率）。
// RatioAt 应改为调用 VersionAt + 本方法，避免两处查找逻辑漂移。
func (v ChannelCostVersion) EffectiveRatio() (float64, bool) {
	if v.CostMode == "discount" {
		r := v.CostDiscount * v.ExchangeRate
		if r <= 0 {
			return 0, false
		}
		return r, true
	}
	if v.CostRatio <= 0 {
		return 0, false
	}
	return v.CostRatio, true
}
```

`RatioAt` 相应简化为：

```go
func (v VersionMap) RatioAt(channelId int, ts int64) (float64, bool) {
	ver, ok := v.VersionAt(channelId, ts)
	if !ok {
		return 0, false
	}
	return ver.EffectiveRatio()
}
```

- [ ] **Step 2: 修改 `addBatch` 签名和实现**

```go
// addBatch 处理一批日志，按 versions 逐条定价并累加到立方体。
// versions 为 nil 时退化为旧行为（成本按 0 计），但正常调用应始终传入。
func (c *costCube) addBatch(logs []*model.Log, versions model.VersionMap) {
    for _, log := range logs {
        if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund && log.Type != model.LogTypeError {
            continue
        }
        key := costCubeKey{
            UserId: log.UserId, Username: log.Username,
            ModelName: log.ModelName, ChannelId: log.ChannelId,
            Bucket: costBucketLabel(time.Unix(log.CreatedAt, 0), c.granularity),
        }
        row := c.rows[key]
        if row == nil {
            row = &costCubeRow{}
            c.rows[key] = row
        }
        if log.Type == model.LogTypeError {
            row.ErrorCount++
            continue
        }
        info := parseLogPricingInfo(log)
        listQ := logListQuota(log, info)

        // 版本定价：逐条按 log.CreatedAt 查当时生效版本。
        // 用 VersionAt 而非 RatioAt，以便记录命中的版本身份（跨版本判定）。
        ratio, priced := float64(0), false
        if versions != nil {
            if ver, ok := versions.VersionAt(log.ChannelId, log.CreatedAt); ok {
                ratio, priced = ver.EffectiveRatio()
                if priced {
                    if row.RatioVersionSeen == nil {
                        row.RatioVersionSeen = make(map[int64]struct{}, 1)
                    }
                    row.RatioVersionSeen[ver.EffectiveFrom] = struct{}{}
                }
            }
        }

        // 折扣覆盖率分母与分子必须走同一套加减规则，否则退款会把比值推过 1。
        histDiscount := historicalDiscount(info)
        isSpecial := info != nil && isValidGroupRatio(info.UserGroupRatio) && info.UserGroupRatio > 0

        if log.Type == model.LogTypeRefund {
            row.Quota -= float64(log.Quota)
            row.ListQuota -= listQ
            row.RefundQuota += float64(log.Quota)
            if priced && ratio > 0 {
                row.CostCny -= listQ / common.QuotaPerUnit * ratio
            } else {
                row.UnpricedListQuota -= listQ
            }
            row.DiscountTotalBasis -= listQ
            if histDiscount > 0 {
                row.DiscountWeightedSum -= histDiscount * listQ
                row.DiscountListBasis -= listQ
                if isSpecial {
                    row.DiscountSpecialSum -= listQ
                }
            }
            continue
        }

        if !isSettleStageLog(info) {
            row.RequestCount++
        }
        row.Quota += float64(log.Quota)
        row.ListQuota += listQ
        if priced && ratio > 0 {
            row.CostCny += listQ / common.QuotaPerUnit * ratio
        } else {
            row.UnpricedListQuota += listQ
        }

        // 用户折扣：从 other 取请求当时的历史值（UserGroupRatio 有效则取，否则 GroupRatio）
        row.DiscountTotalBasis += listQ
        if histDiscount > 0 {
            row.DiscountWeightedSum += histDiscount * listQ
            row.DiscountListBasis += listQ
            if isSpecial {
                row.DiscountSpecialSum += listQ
            }
            if row.DiscountFirstRatio == 0 {
                row.DiscountFirstRatio = histDiscount
            } else if !row.DiscountMixed {
                d := row.DiscountFirstRatio - histDiscount
                if d > 0.001 || d < -0.001 {
                    row.DiscountMixed = true
                }
            }
        }

        cacheRead := 0
        if info != nil {
            cacheRead = info.CacheTokens
        }
        row.PromptTokens += promptTokensExcludingCache(log.PromptTokens, cacheRead, info)
        row.CompletionTokens += log.CompletionTokens
        row.CacheReadTokens += cacheRead
        row.CacheCreationTokens += cacheCreationTokensOf(info)
        if info != nil && info.Frt > 0 {
            row.FrtSumMs += info.Frt
            row.FrtCount++
        }
    }
}
```

- [ ] **Step 3: 在 `controller/cost_stat.go` 末尾添加 `historicalDiscount` 辅助函数**

```go
// historicalDiscount 从日志 other 取请求当时的生效折扣：
// user_group_ratio 有效（专属倍率）优先，其次 group_ratio。
// 返回 0 表示无法确定（旧日志无 other 或值无效）。
func historicalDiscount(info *logPricingInfo) float64 {
    if info == nil {
        return 0
    }
    if isValidGroupRatio(info.UserGroupRatio) && info.UserGroupRatio > 0 {
        return info.UserGroupRatio
    }
    if info.GroupRatio > 0 {
        return info.GroupRatio
    }
    return 0
}
```

- [ ] **Step 4: 删除以下函数（直接删除，无替代）**

- `addGroupQuota` 方法（约 `cost_stat.go:105-113`）
- `resolveUserDiscount` 函数及 `userDiscount` 类型（约 `:386-457`）
- `attachUserGroupRatios` 函数（约 `:459-516`）
- `effectiveChannelRatio` 函数（约 `:373-384`）

删除 `costBreakdownRow` 中的字段：`UserGroup`, `GroupRatio`, `GroupRatioKnown`, `GroupRatioSpecial`, `GroupRatioMixed`, `UsingGroupQuota`

删除 `costDimensionRow` 中的字段：`UserGroup`, `GroupRatio`, `GroupRatioKnown`, `GroupRatioSpecial`, `GroupRatioMixed`, `UsingGroupQuota`

- [ ] **Step 5: 编译验证（会有错误，记录后在 Task 4 中修复）**

```bash
cd D:/Project/new-api && go build ./controller/... 2>&1
```

期望：有编译错误（`addBatch` 调用方还没更新），记录错误，继续 Task 4。

- [ ] **Step 6: Commit**

```bash
cd D:/Project/new-api
git add controller/cost_stat.go
git commit -m "feat(cost): costCubeRow pricing fields, addBatch version-aware, delete old discount chain"
```


---

### Task 4: costMoney 新字段 + costMoneyFromRow + deriveRates + foldCostCube

**Files:**
- Modify: `controller/cost_stat.go`

**Interfaces:**
- Consumes: `costCubeRow.CostCny`, `costCubeRow.UnpricedListQuota`（Task 3）
- Produces:
  - `costMoney` 新增 `EffectiveRatio float64`, `EffectiveRatioKnown bool`, `RatioMixed bool`, `DiscountMixed bool`, `DiscountSpecial bool`, `DiscountCoverage float64`
  - `func costMoneyFromRow(r *costCubeRow, exchangeRate float64) costMoney`（去掉 ratio 参数）
  - `foldCostCube` 签名改为 `func foldCostCube(cube *costCube, dim string, channels map[int]*model.ChannelCostInfo, versions model.VersionMap, exchangeRate float64, end int64) []costDimensionRow`

- [ ] **Step 1: 在 `costMoney` struct 里加新字段（约第 183 行后）**

在 `EffectiveDiscountKnown bool` 之后加：

```go
    // EffectiveRatio 加权真实成本倍率 = CostCny / ListUsd。
    // 跨版本时为加权均值；RatioMixed=true 表示区间内改过价。
    EffectiveRatio      float64 `json:"effective_ratio,omitempty"`
    EffectiveRatioKnown bool    `json:"effective_ratio_known,omitempty"`
    RatioMixed          bool    `json:"ratio_mixed,omitempty"`
    // 用户折扣信号（从日志历史值派生，替代查配置）
    DiscountMixed    bool    `json:"discount_mixed,omitempty"`
    DiscountSpecial  bool    `json:"discount_special,omitempty"`
    DiscountCoverage float64 `json:"discount_coverage,omitempty"`
```

- [ ] **Step 2: 修改 `costMoneyFromRow`（去掉 ratio 参数，直接用 r.CostCny）**

这是本步骤唯一的目标代码——照抄，不要另写变体：

```go
func costMoneyFromRow(r *costCubeRow, exchangeRate float64) costMoney {
    m := costMoney{
        RevenueUsd:          roundTo6(r.Quota / common.QuotaPerUnit),
        ListUsd:             roundTo6(r.ListQuota / common.QuotaPerUnit),
        CostCny:             roundTo6(r.CostCny),
        RefundUsd:           roundTo6(r.RefundQuota / common.QuotaPerUnit),
        PromptTokens:        r.PromptTokens,
        CompletionTokens:    r.CompletionTokens,
        RequestCount:        r.RequestCount,
        CacheReadTokens:     r.CacheReadTokens,
        CacheCreationTokens: r.CacheCreationTokens,
        ErrorCount:          r.ErrorCount,
        FrtSumMs:            r.FrtSumMs,
        FrtCount:            r.FrtCount,
    }
    m.RevenueCny = roundTo6(m.RevenueUsd * exchangeRate)
    m.ProfitCny = roundTo6(m.RevenueCny - m.CostCny)
    if m.RevenueCny != 0 {
        m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
    }
    // 折扣信号。分母用 DiscountTotalBasis（与分子同套加减规则，退款已同步冲减），
    // 不能用 ListQuota——两者加减规则不同会让覆盖率越过 1。
    if r.DiscountTotalBasis > 0 && r.DiscountListBasis > 0 {
        m.DiscountCoverage = roundTo6(r.DiscountListBasis / r.DiscountTotalBasis)
        m.DiscountMixed = r.DiscountMixed
        m.DiscountSpecial = r.DiscountSpecialSum > 0
    }
    // 跨版本标记：本格子命中过 >1 个价格版本。
    m.RatioMixed = len(r.RatioVersionSeen) > 1
    m.deriveRates()
    return m
}
```

- [ ] **Step 3: 修改 `deriveRates` — 加 EffectiveRatio 派生**

在 `deriveRates()` 末尾（`EffectiveDiscount` 派生之后）加：

```go
    if m.ListUsd == 0 {
        m.EffectiveRatio, m.EffectiveRatioKnown = 0, false
    } else {
        m.EffectiveRatio = roundTo6(m.CostCny / m.ListUsd)
        m.EffectiveRatioKnown = true
    }
```

**注意 `RatioMixed` 不在 `deriveRates` 里算** —— 它不是从金额派生的，而是 `costCubeRow` 采集时记录的事实。`deriveRates` 被 `add()` 调用，若在此处赋值会把 `add()` 累加起来的 mixed 状态覆盖掉。

- [ ] **Step 4: 修改 `add()` — 让布尔信号在折叠时正确合并**

`add()` 末尾调用 `deriveRates()` 之前，加布尔字段的 OR 合并与覆盖率的加权合并：

```go
func (m *costMoney) add(o costMoney) {
    // ...原有的数值累加保持不变...

    // 布尔信号取或：任一子行跨版本/跨折扣，父行即为 mixed。
    m.RatioMixed = m.RatioMixed || o.RatioMixed
    m.DiscountMixed = m.DiscountMixed || o.DiscountMixed
    m.DiscountSpecial = m.DiscountSpecial || o.DiscountSpecial
    // 覆盖率按 ListUsd 加权合并（近似：两行折扣口径不同时以金额占比为准）。
    // 任一侧为 0 覆盖率时按 0 参与加权，保证"有缺失就拉低"。
    if totalList := m.ListUsd + o.ListUsd; totalList > 0 {
        m.DiscountCoverage = roundTo6(
            (m.DiscountCoverage*m.ListUsd + o.DiscountCoverage*o.ListUsd) / totalList)
    }

    if m.RevenueCny != 0 {
        m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
    } else {
        m.ProfitRate = 0
    }
    m.deriveRates()
}
```

**注意加权顺序**：`m.ListUsd` 在函数开头已经被 `m.ListUsd = roundTo6(m.ListUsd + o.ListUsd)` 累加过了。必须在**累加之前**先算覆盖率加权，或用累加前的旧值。实现时把覆盖率合并放在数值累加**之前**：

```go
func (m *costMoney) add(o costMoney) {
    // 先用累加前的 ListUsd 做覆盖率加权
    if totalList := m.ListUsd + o.ListUsd; totalList > 0 {
        m.DiscountCoverage = roundTo6(
            (m.DiscountCoverage*m.ListUsd + o.DiscountCoverage*o.ListUsd) / totalList)
    }
    m.RatioMixed = m.RatioMixed || o.RatioMixed
    m.DiscountMixed = m.DiscountMixed || o.DiscountMixed
    m.DiscountSpecial = m.DiscountSpecial || o.DiscountSpecial

    // ...然后是原有的全部数值累加（RevenueUsd/ListUsd/CostCny/... ）...

    if m.RevenueCny != 0 {
        m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
    } else {
        m.ProfitRate = 0
    }
    m.deriveRates()
}
```

- [ ] **Step 5: 修改 `foldCostCube` 签名和实现**

函数签名改为：

```go
func foldCostCube(cube *costCube, dim string, channels map[int]*model.ChannelCostInfo,
    versions model.VersionMap, exchangeRate float64, end int64) []costDimensionRow {
```

函数内的改动：

1. 删除 `mergeGroupQuota` 内联函数及其所有调用
2. `userSets` 逻辑保持不变（渠道维度的 `UserCount` 仍需要它）
3. `costMoneyFromRow(r, ratio, exchangeRate)` 调用改为 `costMoneyFromRow(r, exchangeRate)`
4. 删除 `effectiveChannelRatio(channels, k.ChannelId, exchangeRate)` 调用；渠道名改为直接从 `channels[k.ChannelId].Name` 取：

```go
// 循环开头，取代原来的 ratio, chName := effectiveChannelRatio(...)
chName := ""
if ci := channels[k.ChannelId]; ci != nil {
    chName = ci.Name
}
m := costMoneyFromRow(r, exchangeRate)
```

5. `Priced` 判定：在 `costDimensionRow` 加一个不下发的内部字段，折叠时累加，最后派生：

```go
// costDimensionRow 结构体里加（json:"-" 确保不下发）：
unpricedListQuota float64 `json:"-"`

// 折叠循环内，row.costMoney.add(m) 之后加：
row.unpricedListQuota += r.UnpricedListQuota

// 循环结束、生成 rows 切片时（for gk, row := range groups 里）加：
row.Priced = row.unpricedListQuota == 0
```

6. 渠道维度父行的配置展示：改为取**区间末尾生效版本**，回退到 `ChannelSettings`：

```go
if dim == costDimChannel {
    row.ChannelName = chName
    if ci := channels[gk.ChannelId]; ci != nil {
        row.IsAggregator = ci.IsAggregator
        row.SubSuppliers = ci.SubSuppliers
    }
    // 配置展示优先取区间末尾版本（供 hover 对照"现在是什么价"）
    if ver, ok := versions.VersionAt(gk.ChannelId, end); ok {
        row.CostMode = ver.CostMode
        row.CostRatio = ver.CostRatio
        row.CostDiscount = ver.CostDiscount
    } else if ci := channels[gk.ChannelId]; ci != nil {
        row.CostMode = ci.CostMode
        row.CostRatio = ci.CostRatio
        row.CostDiscount = ci.CostDiscount
    }
    userSets[gk] = make(map[int]bool)
}
```

注意 `row.EffectiveRatio` **不要**在这里赋值——它是 `costMoney` 的派生字段，由 `deriveRates()` 从 `CostCny/ListUsd` 算出（加权真实值）。

7. breakdown 子行的计价配置同样改为按版本取：

```go
if b.key.ChannelId != 0 {
    if ver, ok := versions.VersionAt(b.key.ChannelId, end); ok {
        br.CostMode = ver.CostMode
        br.CostRatio = ver.CostRatio
        br.CostDiscount = ver.CostDiscount
    } else if ci := channels[b.key.ChannelId]; ci != nil {
        br.CostMode = ci.CostMode
        br.CostRatio = ci.CostRatio
        br.CostDiscount = ci.CostDiscount
    }
}
```

8. 删除 `attachUserGroupRatios` 调用（在 `getCostByDimension` 里，Task 5 处理）

- [ ] **Step 5: 修改 `costDimensionRow`（加内部字段，删旧字段）**

```go
type costDimensionRow struct {
    UserId      int    `json:"user_id,omitempty"`
    Username    string `json:"username,omitempty"`
    ModelName   string `json:"model_name,omitempty"`
    ChannelId   int    `json:"channel_id,omitempty"`
    ChannelName string `json:"channel_name,omitempty"`
    CostRatio   float64 `json:"cost_ratio,omitempty"`
    Priced      bool   `json:"priced"`
    UserCount   int    `json:"user_count,omitempty"`
    costMoney
    Breakdown          []costBreakdownRow `json:"breakdown,omitempty"`
    BreakdownTruncated int                `json:"breakdown_truncated,omitempty"`
    CostMode       string                   `json:"cost_mode,omitempty"`
    CostDiscount   float64                  `json:"cost_discount,omitempty"`
    IsAggregator   bool                     `json:"is_aggregator,omitempty"`
    SubSuppliers   []dto.ChannelSubSupplier `json:"sub_suppliers,omitempty"`
    // 内部使用，不下发：累计无版本可用的刊例敞口，最终派生 Priced
    unpricedListQuota float64 `json:"-"`
}
```

- [ ] **Step 6: 编译验证**

```bash
cd D:/Project/new-api && go build ./controller/... 2>&1
```

期望：无错误。

- [ ] **Step 7: 运行现有测试，记录失败项（下一步修复）**

```bash
cd D:/Project/new-api && go test ./controller/... -v 2>&1 | head -80
```

- [ ] **Step 8: Commit**

```bash
cd D:/Project/new-api
git add controller/cost_stat.go
git commit -m "feat(cost): costMoney version fields, costMoneyFromRow no-ratio, foldCostCube refactor"
```


---

### Task 5: buildCostCube 更新 + 版本管理 API + 路由

**Files:**
- Modify: `controller/cost_query.go`
- Create: `controller/cost_versions.go`
- Modify: `router/api-router.go`

**Interfaces:**
- Consumes: `model.GetAllChannelCostVersions`（Task 1）；`foldCostCube` 新签名（Task 4）
- Produces:
  - `GET /api/cost/channels/:id/versions`
  - `POST /api/cost/channels/:id/versions`
  - `DELETE /api/cost/versions/:vid`

- [ ] **Step 1: 修改 `controller/cost_query.go` — costCubeCacheEntry + buildCostCube**

将 `costCubeCacheEntry` 的 `userGroups map[string]string` 字段替换为 `versions model.VersionMap`：

```go
type costCubeCacheEntry struct {
    cube     *costCube
    channels map[int]*model.ChannelCostInfo
    versions model.VersionMap
    rate     float64
    at       time.Time
}
```

同步修改 `costCubeData`：

```go
type costCubeData struct {
    cube     *costCube
    channels map[int]*model.ChannelCostInfo
    versions model.VersionMap
    rate     float64
    start    int64
    end      int64
}
```

修改 `buildCostCube`，替换 `GetAllUserGroups` 为 `GetAllChannelCostVersions`，并把 `versions` 传给 `cube.addBatch`：

```go
func buildCostCube(c *gin.Context) (*costCubeData, error) {
    start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
    end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
    start, end = clampCostRange(start, end, time.Now().Unix())

    modelName := c.Query("model_name")
    username := c.Query("username")
    rate := billSummaryRate(c)
    channel, _ := strconv.Atoi(c.Query("channel"))
    granularity := normalizeCostGranularity(c.Query("granularity"), start, end)

    cacheKey := costCubeCacheKey(start, end, modelName, username, rate, channel, granularity)
    if entry, ok := costCubeCacheGet(cacheKey); ok {
        return &costCubeData{cube: entry.cube, channels: entry.channels,
            versions: entry.versions, rate: entry.rate, start: start, end: end}, nil
    }

    versions, err := model.GetAllChannelCostVersions()
    if err != nil {
        return nil, err
    }
    cube := newCostCubeWithGranularity(granularity)
    maxRows := model.LogExportMaxRows("xlsx")
    _, err = model.GetAllLogsForExport(model.LogTypeUnknown, start, end,
        modelName, username, "", channel, "", "", maxRows,
        func(batch []*model.Log) error {
            cube.addBatch(batch, versions)
            return nil
        })
    if err != nil {
        return nil, err
    }
    channels, err := model.GetAllChannelCostInfos()
    if err != nil {
        return nil, err
    }
    costCubeCachePut(cacheKey, &costCubeCacheEntry{cube: cube, channels: channels,
        versions: versions, rate: rate, at: time.Now()})
    return &costCubeData{cube: cube, channels: channels, versions: versions,
        rate: rate, start: start, end: end}, nil
}
```

修改 `getCostByDimension`：传新参数给 `foldCostCube`，删除 `attachUserGroupRatios` 调用：

```go
func getCostByDimension(c *gin.Context, dim string) {
    data, err := buildCostCube(c)
    if err != nil {
        common.ApiError(c, err)
        return
    }
    page, pageSize := parseBillSummaryPaging(c)
    rows := foldCostCube(data.cube, dim, data.channels, data.versions, data.rate, data.end)
    common.ApiSuccess(c, paginateCostRows(rows, page, pageSize))
}
```

- [ ] **Step 2: 新建 `controller/cost_versions.go`**

```go
package controller

import (
    "net/http"
    "strconv"
    "time"

    "github.com/QuantumNous/new-api/common"
    "github.com/QuantumNous/new-api/model"
    "github.com/gin-gonic/gin"
)

// GetChannelCostVersions GET /api/cost/channels/:id/versions
func GetChannelCostVersions(c *gin.Context) {
    id, err := strconv.Atoi(c.Param("id"))
    if err != nil {
        common.ApiError(c, err)
        return
    }
    versions, err := model.GetChannelCostVersions(id)
    if err != nil {
        common.ApiError(c, err)
        return
    }
    common.ApiSuccess(c, versions)
}

type createVersionRequest struct {
    EffectiveFrom int64   `json:"effective_from" binding:"required"`
    CostMode      string  `json:"cost_mode"`
    CostRatio     float64 `json:"cost_ratio"`
    CostDiscount  float64 `json:"cost_discount"`
    ExchangeRate  float64 `json:"exchange_rate"`
    Note          string  `json:"note"`
}

// CreateChannelCostVersion POST /api/cost/channels/:id/versions
func CreateChannelCostVersion(c *gin.Context) {
    id, err := strconv.Atoi(c.Param("id"))
    if err != nil {
        common.ApiError(c, err)
        return
    }
    var req createVersionRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        common.ApiError(c, err)
        return
    }
    if req.EffectiveFrom == 0 {
        c.JSON(http.StatusOK, gin.H{"success": false,
            "message": "effective_from cannot be 0 (reserved for migration)"})
        return
    }
    exists, err := model.VersionExists(id, req.EffectiveFrom)
    if err != nil {
        common.ApiError(c, err)
        return
    }
    if exists {
        c.JSON(http.StatusOK, gin.H{"success": false,
            "message": "a version with this effective_from already exists for this channel"})
        return
    }
    userId := c.GetInt("id")
    v := &model.ChannelCostVersion{
        ChannelId:     id,
        EffectiveFrom: req.EffectiveFrom,
        CostMode:      req.CostMode,
        CostRatio:     req.CostRatio,
        CostDiscount:  req.CostDiscount,
        ExchangeRate:  req.ExchangeRate,
        Note:          req.Note,
        CreatedBy:     userId,
    }
    if err := model.CreateChannelCostVersion(v); err != nil {
        common.ApiError(c, err)
        return
    }
    common.ApiSuccess(c, v)
}

// DeleteChannelCostVersion DELETE /api/cost/versions/:vid
func DeleteChannelCostVersion(c *gin.Context) {
    vid, err := strconv.Atoi(c.Param("vid"))
    if err != nil {
        common.ApiError(c, err)
        return
    }
    if err := model.DeleteChannelCostVersion(vid); err != nil {
        common.ApiError(c, err)
        return
    }
    common.ApiSuccess(c, nil)
}
```

忽略未使用的 `time` import（可以用 `_ "time"` 或删掉）。`c.GetInt("id")` 取当前登录用户 id（`middleware.RootAuth` 已填充）。

- [ ] **Step 3: 修改 `router/api-router.go` — 在 costRoute 组加 3 条路由**

在 `costRoute.GET("/channels", ...)` 之后加：

```go
costRoute.GET("/channels/:id/versions", controller.GetChannelCostVersions)
costRoute.POST("/channels/:id/versions", controller.CreateChannelCostVersion)
costRoute.DELETE("/versions/:vid", controller.DeleteChannelCostVersion)
```

- [ ] **Step 4: 编译验证**

```bash
cd D:/Project/new-api && go build ./... 2>&1
```

期望：无错误。

- [ ] **Step 5: 运行所有测试**

```bash
cd D:/Project/new-api && go test ./controller/... -v 2>&1 | tail -40
```

期望：`TestVersionMap_*` 全 PASS，其余测试无新增 FAIL（旧 `TestEffectiveChannelRatio` 因 `effectiveChannelRatio` 已删，需同步从测试文件中删除）。

- [ ] **Step 6: Commit**

```bash
cd D:/Project/new-api
git add controller/cost_query.go controller/cost_versions.go router/api-router.go
git commit -m "feat(cost): wire VersionMap into buildCostCube, add version management API"
```


---

### Task 6: UpdateChannel 自动追版本 + 测试修复

**Files:**
- Modify: `controller/channel.go`
- Modify: `controller/cost_stat_test.go`
- Modify: `controller/cost_integration_test.go`

**Interfaces:**
- Consumes: `model.CreateChannelCostVersion`, `model.VersionExists`（Task 1）；`addBatch` 新签名（Task 3）

- [ ] **Step 1: 修改 `controller/channel.go` — UpdateChannel 价格变更时追版本**

在 `UpdateChannel` 函数内，`model.UpdateChannel` 调用成功后，加以下逻辑。

**关键：必须先与当前最新版本比对，只有价格真的变了才追加**（spec §3.2）。否则每次改渠道 API key 之类的无关字段都会插一条重复版本，价格历史很快就没法看了。

```go
// 成本计价字段变化时追加一条 effective_from=now 的版本。
// 与"当前最新版本"比对，无变化则不追加——否则每次保存渠道（改 key、改模型列表…）
// 都会插一条重复版本，价格历史会被噪声淹没。
if channel.Setting != nil {
    var newSetting dto.ChannelSettings
    if err := common.UnmarshalJsonStr(*channel.Setting, &newSetting); err == nil {
        appendCostVersionIfChanged(c, channel.Id, &newSetting)
    }
}
```

在 `controller/channel.go` 末尾（或 `controller/cost_versions.go`）加这个辅助函数：

```go
// appendCostVersionIfChanged 比对渠道当前最新价格版本与新设置，仅在计价字段
// 真的变化时追加新版本。价格未配置（两种模式的值都是 0）时不追加。
// 失败只记日志，不阻断渠道保存——版本记录是核算辅助，不该让渠道编辑失败。
func appendCostVersionIfChanged(c *gin.Context, channelId int, s *dto.ChannelSettings) {
    hasCost := (s.CostMode == "discount" && s.CostDiscount > 0) ||
        (s.CostMode != "discount" && s.CostRatio > 0)
    if !hasCost {
        return
    }
    versions, err := model.GetChannelCostVersions(channelId) // 降序，[0] 为最新
    if err != nil {
        common.SysError("load cost versions failed: " + err.Error())
        return
    }
    if len(versions) > 0 {
        latest := versions[0]
        // 归一化：空 CostMode 等同 "ratio"，否则 ""→"ratio" 的保存会被误判为变化。
        normMode := func(m string) string {
            if m == "" {
                return "ratio"
            }
            return m
        }
        sameFloat := func(a, b float64) bool {
            d := a - b
            return d < 1e-9 && d > -1e-9
        }
        unchanged := normMode(latest.CostMode) == normMode(s.CostMode) &&
            sameFloat(latest.CostRatio, s.CostRatio) &&
            sameFloat(latest.CostDiscount, s.CostDiscount)
        if unchanged {
            return
        }
    }
    now := time.Now().Unix()
    if exists, _ := model.VersionExists(channelId, now); exists {
        return // 同一秒内重复保存，跳过
    }
    v := &model.ChannelCostVersion{
        ChannelId:     channelId,
        EffectiveFrom: now,
        CostMode:      s.CostMode,
        CostRatio:     s.CostRatio,
        CostDiscount:  s.CostDiscount,
        ExchangeRate:  operation_setting.USDExchangeRate,
        Note:          "auto from channel update",
        CreatedBy:     c.GetInt("id"),
    }
    if err := model.CreateChannelCostVersion(v); err != nil {
        common.SysError("auto append cost version failed: " + err.Error())
    }
}
```

确保 import 有 `"github.com/QuantumNous/new-api/setting/operation_setting"` 和 `"time"`。

- [ ] **Step 1b: 为 `appendCostVersionIfChanged` 的比对逻辑写单测**

在 `controller/cost_stat_version_test.go` 追加。比对逻辑是本任务唯一有分支的部分，且"改 key 不该产生版本"正是本步骤要防的回归：

```go
// 把比对逻辑抽成纯函数以便测试（实现时 appendCostVersionIfChanged 调用它）：
//   func costVersionChanged(latest model.ChannelCostVersion, s *dto.ChannelSettings) bool
func TestCostVersionChanged(t *testing.T) {
	cases := []struct {
		name   string
		latest model.ChannelCostVersion
		s      dto.ChannelSettings
		want   bool
	}{
		{"identical-ratio", model.ChannelCostVersion{CostMode: "ratio", CostRatio: 2.5},
			dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.5}, false},
		{"empty-mode-equals-ratio", model.ChannelCostVersion{CostMode: "", CostRatio: 2.5},
			dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.5}, false},
		{"ratio-changed", model.ChannelCostVersion{CostMode: "ratio", CostRatio: 2.5},
			dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.3}, true},
		{"mode-switched", model.ChannelCostVersion{CostMode: "ratio", CostRatio: 2.5},
			dto.ChannelSettings{CostMode: "discount", CostDiscount: 0.8}, true},
		{"discount-changed", model.ChannelCostVersion{CostMode: "discount", CostDiscount: 0.8},
			dto.ChannelSettings{CostMode: "discount", CostDiscount: 0.75}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := costVersionChanged(tc.latest, &tc.s); got != tc.want {
				t.Fatalf("changed = %v, want %v", got, tc.want)
			}
		})
	}
}
```

运行：`go test ./controller/... -run TestCostVersionChanged -v` — 期望 5 个子测试全 PASS。

- [ ] **Step 2: 修改 `controller/cost_stat_test.go` — 更新受影响测试**

`TestEffectiveChannelRatio` 测试 `effectiveChannelRatio` 函数，该函数已删除，整个测试删除。

所有调用 `c.addBatch([]*model.Log{...})` 的测试（无 versions 参数）改为传 nil：

```go
c.addBatch([]*model.Log{...}, nil)
```

全局搜索替换：`c.addBatch([]*model.Log{` → `c.addBatch([]*model.Log{`（签名已变，编译会提示，按提示修改即可）。

删除 `testChannels()` 函数中不再被任何测试引用的 `effectiveChannelRatio` 调用。

删除 `TestFoldCostCube_*` 系列里 `foldCostCube(c, dim, testChannels(), rate)` 的调用，改为：

```go
foldCostCube(c, dim, testChannels(), nil, rate, end)
```

其中 `nil` 为 versions（测试不需要跨版本验证，传 nil 时 `RatioAt` 全返回 false，成本按 0 计）。

- [ ] **Step 3: 修改 `controller/cost_integration_test.go`**

集成测试里的 `foldCostCube` 调用同步加 `versions, end` 参数。
`attachUserGroupRatios` 调用删除。
`model.GetAllUserGroups()` 调用删除。

- [ ] **Step 4: 运行全量测试**

```bash
cd D:/Project/new-api && go test ./controller/... -v 2>&1 | tail -60
```

期望：所有测试 PASS，无 FAIL。

- [ ] **Step 5: Commit**

```bash
cd D:/Project/new-api
git add controller/channel.go controller/cost_stat_test.go controller/cost_integration_test.go
git commit -m "feat(cost): UpdateChannel auto-appends cost version; fix tests for new addBatch/foldCostCube signatures"
```


---

### Task 7: 新前端（types / api / lib / edit-ratio-dialog / cost-user-cells / i18n）

**Files:**
- Modify: `web/default/src/features/cost/types.ts`
- Modify: `web/default/src/features/cost/api.ts`
- Modify: `web/default/src/features/cost/lib.ts`
- Modify: `web/default/src/features/cost/components/edit-ratio-dialog.tsx`
- Modify: `web/default/src/features/cost/components/cost-user-cells.tsx`
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`

**Interfaces:**
- Consumes: 后端新字段 `ratio_mixed`, `discount_mixed`, `discount_special`, `discount_coverage`, `effective_ratio_known`；版本管理 API（Task 5）
- Produces: 编译通过的前端，`UserDiscountCell` 用历史信号，`EditRatioDialog` 含版本历史面板

- [ ] **Step 1: 修改 `web/default/src/features/cost/types.ts`**

在 `CostMoney` interface 里 `effective_discount_known: boolean` 之后加：

```ts
  effective_ratio?: number
  effective_ratio_known?: boolean
  ratio_mixed?: boolean
  discount_mixed?: boolean
  discount_special?: boolean
  discount_coverage?: number
```

在 `CostBreakdownRow` 和 `CostDimensionRow` 里删除以下字段：
`user_group`, `group_ratio`, `group_ratio_known`, `group_ratio_special`, `group_ratio_mixed`

在文件末尾加版本类型：

```ts
export interface ChannelCostVersion {
  id: number
  channel_id: number
  effective_from: number
  cost_mode: '' | 'ratio' | 'discount'
  cost_ratio: number
  cost_discount: number
  exchange_rate: number
  note?: string
  created_at: number
  created_by: number
}
```

- [ ] **Step 2: 修改 `web/default/src/features/cost/api.ts` — 加版本管理函数**

```ts
import type { ChannelCostVersion } from './types'

export async function getChannelCostVersions(channelId: number): Promise<ChannelCostVersion[]> {
  const res = await api.get(`/api/cost/channels/${channelId}/versions`)
  return res.data.data
}

export async function createChannelCostVersion(
  channelId: number,
  body: Omit<ChannelCostVersion, 'id' | 'channel_id' | 'created_at' | 'created_by'>
): Promise<ChannelCostVersion> {
  const res = await api.post(`/api/cost/channels/${channelId}/versions`, body)
  return res.data.data
}

export async function deleteChannelCostVersion(versionId: number): Promise<void> {
  await api.delete(`/api/cost/versions/${versionId}`)
}
```

- [ ] **Step 3: 修改 `web/default/src/features/cost/lib.ts` — 删 group_ratio* carry**

在 `mergeBreakdown` 函数的 `if (groupBy === 'username')` 块（约第 254-260 行）中，删除：

```ts
      if (groupBy === 'username') {
        acc.user_group = row.user_group
        acc.group_ratio = row.group_ratio
        acc.group_ratio_known = row.group_ratio_known
        acc.group_ratio_special = row.group_ratio_special
        acc.group_ratio_mixed = row.group_ratio_mixed
      }
```

整个块删掉即可（字段已从类型中删除）。

在 `ZERO_MONEY` 对象里加新字段（位于 `effective_discount_known: false,` 之后）：

```ts
  effective_ratio: 0,
  effective_ratio_known: false,
  ratio_mixed: false,
  discount_mixed: false,
  discount_special: false,
  discount_coverage: 0,
```

- [ ] **Step 4: 修改 `web/default/src/features/cost/components/cost-user-cells.tsx`**

**4a. 更新 `PricingCellRow` interface**：删除 `user_group`, `group_ratio`, `group_ratio_known`, `group_ratio_special`, `group_ratio_mixed`；加新字段：

```ts
  ratio_mixed?: boolean
  discount_mixed?: boolean
  discount_special?: boolean
  discount_coverage?: number
  effective_ratio_known?: boolean
```

**4b. 重写 `UserDiscountCell`**：

```tsx
export function UserDiscountCell({ row }: { row: PricingCellRow }) {
  const { t } = useTranslation()

  const actualKnown = Boolean(row.effective_discount_known)
  const actual = row.effective_discount
  const mixed = Boolean(row.discount_mixed)
  const special = Boolean(row.discount_special)
  const coverage = row.discount_coverage ?? 1

  if (!actualKnown) {
    return <span className='text-muted-foreground'>-</span>
  }

  return (
    <HoverCard>
      <HoverCardTrigger delay={100} closeDelay={80} tabIndex={0} className={hoverTriggerClass}>
        <span className={mixed ? 'text-warning inline-flex items-center gap-1' : undefined}>
          {trimRatioNumber(actual)}
        </span>
      </HoverCardTrigger>
      <HoverCardContent align='end' className='w-80'>
        <div className='flex flex-col gap-1.5 text-xs'>
          <div className='flex items-center justify-between gap-4'>
            <span className='text-muted-foreground'>{t('Actual discount (this range)')}</span>
            <span className='tabular-nums'>{trimRatioNumber(actual)}</span>
          </div>
          <p className='text-muted-foreground'>
            {t('Actual discount is revenue divided by list price over the selected range.')}
          </p>
          {special && (
            <p className='text-muted-foreground'>
              {t('Dedicated ratio applied: a user-group × token-group ratio was active.')}
            </p>
          )}
          {mixed && (
            <p className='text-warning'>
              {t('Discount changed during this range — the user may have changed groups or a ratio was edited.')}
            </p>
          )}
          {coverage < 0.99 && (
            <p className='text-muted-foreground'>
              {t('Partial discount coverage: {{pct}}% of spend has pricing info; older logs may lack it.', {
                pct: Math.round(coverage * 100),
              })}
            </p>
          )}
        </div>
      </HoverCardContent>
    </HoverCard>
  )
}
```

**4c. 在 `CostRatioDiscountCell` 里加 `ratio_mixed` 展示**：

在 `configuredPricingLabel(row, t)` 返回 label 后，加一个条件：若 `row.ratio_mixed` 为 true 且当前行非渠道父行时，显示加权值：

```tsx
// 在 WeightedCostRatio 组件内，blended 已经处理多渠道；
// ratio_mixed 表示单渠道内跨版本，text 前加 ≈ 并 hover 说明
```

在渠道维度父行（`dim === 'channels' && isParent`）内，`cost_mode === 'discount'` 小字后加：

```tsx
{row.ratio_mixed && (
  <span className='text-warning text-[11px]'>
    {t('Cost ratio changed during this range')}
  </span>
)}
```

- [ ] **Step 5: 修改 `web/default/src/features/cost/components/edit-ratio-dialog.tsx` — 加版本历史面板**

扩展 `EditRatioDialogProps`：

```ts
interface EditRatioDialogProps {
  channelId: number
  channelName: string
  currentRatio: number
  currentMode?: string
  currentDiscount?: number
  exchangeRate: number
}
```

Props 不变，但在 Dialog 内容区加第二段（版本历史）。在现有表单下方加：

```tsx
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getChannelCostVersions, createChannelCostVersion, deleteChannelCostVersion } from '../api'
import { formatDate } from '@/lib/format' // 或用 dayjs/date-fns，按项目已有

// Dialog 内容结构：
<div className='flex flex-col gap-6'>
  {/* 当前价（原有表单，不变）*/}
  <div className='flex flex-col gap-4'>
    {/* ... 原有 Label/Select/Input */}
    <p className='text-muted-foreground text-xs'>
      {t('Saving records the current price as a new version; historical ranges keep their original price.')}
    </p>
  </div>

  {/* 分隔线 */}
  <div className='border-t pt-4'>
    <VersionHistoryPanel channelId={channelId} />
  </div>
</div>
```

新建内联组件 `VersionHistoryPanel`：

```tsx
function VersionHistoryPanel({ channelId }: { channelId: number }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data: versions, isLoading } = useQuery({
    queryKey: ['cost-versions', channelId],
    queryFn: () => getChannelCostVersions(channelId),
  })
  const deleteMut = useMutation({
    mutationFn: deleteChannelCostVersion,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['cost-versions', channelId] }),
  })
  const [showAdd, setShowAdd] = useState(false)

  return (
    <div className='flex flex-col gap-2'>
      <div className='flex items-center justify-between'>
        <p className='text-sm font-medium'>{t('Version history')}</p>
        <Button type='button' variant='outline' size='sm' onClick={() => setShowAdd(true)}>
          {t('Add historical price')}
        </Button>
      </div>
      {isLoading ? (
        <p className='text-muted-foreground text-xs'>{t('Loading...')}</p>
      ) : (
        <div className='flex flex-col divide-y text-xs'>
          {(versions ?? []).map((v) => (
            <div key={v.id} className='flex items-center justify-between gap-2 py-1.5'>
              <span className='text-muted-foreground tabular-nums'>
                {v.effective_from === 0 ? t('Initial') : new Date(v.effective_from * 1000).toLocaleDateString()}
              </span>
              <span className='tabular-nums'>
                {v.cost_mode === 'discount'
                  ? t('{{v}} / discount', { v: v.cost_discount })
                  : t('{{v}} / ratio', { v: v.cost_ratio })}
              </span>
              <span className='text-muted-foreground truncate max-w-24'>{v.note}</span>
              {v.effective_from !== 0 && (
                <Button type='button' variant='ghost' size='icon' className='size-5'
                  onClick={() => deleteMut.mutate(v.id)}
                  disabled={deleteMut.isPending}
                >
                  ×
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
      {showAdd && (
        <AddVersionForm channelId={channelId} onDone={() => {
          setShowAdd(false)
          queryClient.invalidateQueries({ queryKey: ['cost-versions', channelId] })
        }} />
      )}
    </div>
  )
}
```

`AddVersionForm` 是一个简单的内联表单：日期选择器（转为 Unix 时间戳）+ 模式选择 + 倍率/折扣输入 + 备注 + 提交按钮。提交调用 `createChannelCostVersion`。

- [ ] **Step 5b: 修改筛选栏汇率标签（spec §5.4）**

文件：`web/default/src/features/cost/components/cost-filter.tsx`

汇率输入框的含义变了——它现在**只**影响收入折算（`revenue_cny`），不再影响成本（成本按各版本自带的冻结汇率算）。标签必须跟着改，否则操作员会以为调它能改成本。

把汇率字段的 `Label` 文案从 `t('Exchange Rate')` 改为 `t('Revenue exchange rate')`，并在输入框下方加一行说明：

```tsx
<p className='text-muted-foreground text-xs'>
  {t('Converts USD revenue to CNY for display. Cost uses each price version\'s own settlement rate.')}
</p>
```

（若现有实现用的是别的组件结构，保持结构不变，只改标签文案并加这一行说明。）

- [ ] **Step 6: 修改 i18n 文件**

在 `web/default/src/i18n/locales/en.json` 加：

```json
"Version history": "Version history",
"Add historical price": "Add historical price",
"Price effective from": "Price effective from",
"Initial": "Initial",
"Saving records the current price as a new version; historical ranges keep their original price.": "Saving records the current price as a new version; historical ranges keep their original price.",
"Revenue exchange rate": "Revenue exchange rate",
"Does not affect cost accounting": "Does not affect cost accounting",
"Discount changed during this range — the user may have changed groups or a ratio was edited.": "Discount changed during this range — the user may have changed groups or a ratio was edited.",
"Dedicated ratio applied: a user-group × token-group ratio was active.": "Dedicated ratio applied: a user-group × token-group ratio was active.",
"Partial discount coverage: {{pct}}% of spend has pricing info; older logs may lack it.": "Partial discount coverage: {{pct}}% of spend has pricing info; older logs may lack it.",
"Cost ratio changed during this range": "Cost ratio changed during this range",
"Weighted across price versions": "Weighted across price versions",
"Converts USD revenue to CNY for display. Cost uses each price version's own settlement rate.": "Converts USD revenue to CNY for display. Cost uses each price version's own settlement rate."
```

在 `web/default/src/i18n/locales/zh.json` 加对应中文翻译：

```json
"Version history": "价格历史",
"Add historical price": "补录历史价",
"Price effective from": "生效日期",
"Initial": "初始",
"Saving records the current price as a new version; historical ranges keep their original price.": "保存后自动记录为当前生效价，历史区间仍按原价核算。",
"Revenue exchange rate": "收入折算汇率",
"Does not affect cost accounting": "不影响成本核算",
"Discount changed during this range — the user may have changed groups or a ratio was edited.": "区间内折扣有变更——用户可能换过分组，或倍率曾被调整。",
"Dedicated ratio applied: a user-group × token-group ratio was active.": "命中专属倍率：期间存在用户分组 × 令牌分组的专属倍率配置。",
"Partial discount coverage: {{pct}}% of spend has pricing info; older logs may lack it.": "折扣信息覆盖率 {{pct}}%，部分旧日志缺少定价字段。",
"Cost ratio changed during this range": "区间内成本倍率有变更",
"Weighted across price versions": "跨版本加权",
"Converts USD revenue to CNY for display. Cost uses each price version's own settlement rate.": "用于将美元收入折算成人民币展示；成本按各价格版本自带的结算汇率计算，不受此项影响。"
```

同时在 `fr.json`, `ru.json`, `ja.json`, `vi.json` 里加相同 key（值暂用英文，运行 `bun run i18n:sync` 后再补译）。

- [ ] **Step 7: 编译验证**

```bash
cd D:/Project/new-api/web/default && bun run build 2>&1 | tail -20
```

期望：无 TypeScript 错误，build 成功。

- [ ] **Step 8: Commit**

```bash
cd D:/Project/new-api
git add web/default/src/features/cost/
git add web/default/src/i18n/locales/
git commit -m "feat(cost): default frontend — version history panel, UserDiscountCell history signals, ratio_mixed"
```


---

### Task 8: Classic 前端（costMerge / CostUserCells / CostTables / CostVersionModal）

**Files:**
- Modify: `web/classic/src/components/cost/costMerge.js`
- Modify: `web/classic/src/components/cost/CostUserCells.jsx`
- Modify: `web/classic/src/components/cost/CostTables.jsx`
- Create: `web/classic/src/components/cost/CostVersionModal.jsx`

**Interfaces:**
- Consumes: 后端新字段（同 Task 7）；版本管理 API（Task 5）

- [ ] **Step 1: 修改 `web/classic/src/components/cost/costMerge.js`**

在 `mergeBreakdown` 函数的 `carryFields` 初始化块中，删除 `user_group` 相关字段：

```js
// 删除这段：
if (keyFields.includes('username')) {
  carryFields.push(
    'user_group',
    'group_ratio',
    'group_ratio_known',
    'group_ratio_special',
    'group_ratio_mixed',
  );
}
```

整个 `if (keyFields.includes('username'))` 块删掉。

在 `deriveCostRates` 函数里 `effective_discount` 派生之后加：

```js
// effective_ratio 派生
row.effective_ratio_known = row.list_usd !== 0;
row.effective_ratio = row.list_usd === 0 ? 0 : (Number(row.cost_cny) || 0) / (Number(row.list_usd) || 1);
```

- [ ] **Step 2: 修改 `web/classic/src/components/cost/CostUserCells.jsx` — UserDiscountCell**

找到 `UserDiscountCell` 组件（使用 `group_ratio`、`drifted` 等），整体替换为：

```jsx
export const UserDiscountCell = ({ row, t }) => {
  const actualKnown = Boolean(row.effective_discount_known);
  const actual = Number(row.effective_discount) || 0;
  const mixed = Boolean(row.discount_mixed);
  const special = Boolean(row.discount_special);
  const coverage = Number(row.discount_coverage) || 1;

  if (!actualKnown) {
    return <span style={{ color: 'var(--semi-color-text-2)' }}>-</span>;
  }

  const content = (
    <div style={{ maxWidth: 320, padding: '8px 4px', lineHeight: 1.6, fontSize: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
        <span style={{ color: 'var(--semi-color-text-2)' }}>{t('区间实际折扣')}</span>
        <span>{trimRatioNumber(actual)}</span>
      </div>
      <p style={{ color: 'var(--semi-color-text-2)', marginTop: 4 }}>
        {t('实际折扣 = 收入 ÷ 刊例价（区间内按额度加权）')}
      </p>
      {special && (
        <p style={{ color: 'var(--semi-color-text-2)', marginTop: 4 }}>
          {t('命中专属倍率：期间存在用户分组 × 令牌分组的专属倍率配置。')}
        </p>
      )}
      {mixed && (
        <p style={{ color: 'var(--semi-color-warning)', marginTop: 4 }}>
          {t('区间内折扣有变更——用户可能换过分组，或倍率曾被调整。')}
        </p>
      )}
      {coverage < 0.99 && (
        <p style={{ color: 'var(--semi-color-text-2)', marginTop: 4 }}>
          {t('折扣信息覆盖率 {{pct}}%，部分旧日志缺少定价字段。', { pct: Math.round(coverage * 100) })}
        </p>
      )}
    </div>
  );

  return (
    <Popover showArrow trigger='hover' position='left' content={content}>
      <span
        style={{
          ...hoverTextStyle,
          color: mixed ? 'var(--semi-color-warning)' : undefined,
        }}
      >
        {trimRatioNumber(actual)}
      </span>
    </Popover>
  );
};
```

- [ ] **Step 3: 修改 `web/classic/src/components/cost/CostTables.jsx` — ratio_mixed 展示**

找到渠道维度父行的倍率单元格渲染逻辑（通常是 `configuredPricingLabel` 调用处）。在渲染 label 之后加：

```jsx
{row.ratio_mixed && (
  <Tooltip content={t('区间内成本倍率有变更')}>
    <Tag color='orange' size='small' style={{ marginLeft: 4 }}>≈</Tag>
  </Tooltip>
)}
```

import 确保有 `Tooltip` 和 `Tag`（Semi Design 已有）。

- [ ] **Step 4: 新建 `web/classic/src/components/cost/CostVersionModal.jsx`**

```jsx
import React, { useState } from 'react';
import { Modal, Table, Button, Form, Toast } from '@douyinfe/semi-ui';
import { API } from '../../../../helpers/api';

const { DatePicker, Select, InputNumber, Input } = Form;

export function CostVersionModal({ channelId, channelName, visible, onClose }) {
  const [versions, setVersions] = useState([]);
  const [loading, setLoading] = useState(false);
  const [showAdd, setShowAdd] = useState(false);

  React.useEffect(() => {
    if (visible) loadVersions();
  }, [visible, channelId]);

  const loadVersions = async () => {
    setLoading(true);
    try {
      const res = await API.get(`/api/cost/channels/${channelId}/versions`);
      if (res.data.success) setVersions(res.data.data || []);
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (id) => {
    const res = await API.delete(`/api/cost/versions/${id}`);
    if (res.data.success) {
      Toast.success('已删除');
      loadVersions();
    }
  };

  const columns = [
    {
      title: '生效日期',
      dataIndex: 'effective_from',
      render: (v) => v === 0 ? '初始' : new Date(v * 1000).toLocaleDateString(),
    },
    {
      title: '计价方式',
      dataIndex: 'cost_mode',
      render: (v, row) =>
        v === 'discount'
          ? `${row.cost_discount} / 折扣`
          : `${row.cost_ratio} / 倍率`,
    },
    { title: '备注', dataIndex: 'note' },
    {
      title: '操作',
      render: (_, row) =>
        row.effective_from !== 0 ? (
          <Button type='danger' size='small' onClick={() => handleDelete(row.id)}>
            删除
          </Button>
        ) : null,
    },
  ];

  return (
    <Modal
      title={`价格历史 — ${channelName}`}
      visible={visible}
      onCancel={onClose}
      footer={null}
      width={600}
    >
      <Button onClick={() => setShowAdd(true)} style={{ marginBottom: 12 }}>
        补录历史价
      </Button>
      <Table
        columns={columns}
        dataSource={versions}
        loading={loading}
        pagination={false}
        size='small'
      />
      {showAdd && (
        <AddVersionForm
          channelId={channelId}
          onDone={() => { setShowAdd(false); loadVersions(); }}
        />
      )}
    </Modal>
  );
}

function AddVersionForm({ channelId, onDone }) {
  const [mode, setMode] = useState('ratio');
  const [ratio, setRatio] = useState('');
  const [discount, setDiscount] = useState('');
  const [date, setDate] = useState(null);
  const [note, setNote] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async () => {
    if (!date) return;
    setSubmitting(true);
    try {
      const body = {
        effective_from: Math.floor(new Date(date).getTime() / 1000),
        cost_mode: mode,
        cost_ratio: mode === 'ratio' ? Number(ratio) : 0,
        cost_discount: mode === 'discount' ? Number(discount) : 0,
        exchange_rate: 0,
        note,
      };
      const res = await API.post(`/api/cost/channels/${channelId}/versions`, body);
      if (res.data.success) {
        Toast.success('已添加');
        onDone();
      } else {
        Toast.error(res.data.message || '添加失败');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ marginTop: 16, padding: 12, border: '1px solid var(--semi-color-border)', borderRadius: 6 }}>
      <p style={{ marginBottom: 8, fontWeight: 500 }}>补录历史价</p>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'flex-end' }}>
        <div>
          <label style={{ fontSize: 12 }}>生效日期</label>
          <input type='date' value={date || ''} onChange={e => setDate(e.target.value)}
            style={{ display: 'block', marginTop: 4 }} />
        </div>
        <div>
          <label style={{ fontSize: 12 }}>计价方式</label>
          <select value={mode} onChange={e => setMode(e.target.value)}
            style={{ display: 'block', marginTop: 4 }}>
            <option value='ratio'>倍率</option>
            <option value='discount'>折扣</option>
          </select>
        </div>
        {mode === 'ratio' ? (
          <div>
            <label style={{ fontSize: 12 }}>倍率 (CNY/USD)</label>
            <input type='number' value={ratio} onChange={e => setRatio(e.target.value)}
              style={{ display: 'block', marginTop: 4, width: 80 }} />
          </div>
        ) : (
          <div>
            <label style={{ fontSize: 12 }}>折扣</label>
            <input type='number' value={discount} onChange={e => setDiscount(e.target.value)}
              style={{ display: 'block', marginTop: 4, width: 80 }} />
          </div>
        )}
        <div>
          <label style={{ fontSize: 12 }}>备注</label>
          <input type='text' value={note} onChange={e => setNote(e.target.value)}
            style={{ display: 'block', marginTop: 4, width: 120 }} />
        </div>
        <button onClick={handleSubmit} disabled={submitting || !date}>
          {submitting ? '保存中…' : '保存'}
        </button>
      </div>
    </div>
  );
}
```

在成本页供应商维度的倍率列（`CostTables.jsx`）的编辑按钮点击处，把原来的内联编辑改为打开 `CostVersionModal`：

```jsx
import { CostVersionModal } from './CostVersionModal';

// 在组件 state 里加：
const [versionModalChannel, setVersionModalChannel] = useState(null);

// 渠道维度倍率列的编辑图标 onClick：
onClick={() => setVersionModalChannel({ id: row.channel_id, name: row.channel_name })}

// 在 return 里加：
<CostVersionModal
  channelId={versionModalChannel?.id}
  channelName={versionModalChannel?.name}
  visible={Boolean(versionModalChannel)}
  onClose={() => setVersionModalChannel(null)}
/>
```

- [ ] **Step 5: 编译验证**

```bash
cd D:/Project/new-api/web/classic && bun install && bun run build 2>&1 | tail -20
```

期望：无错误。如果 classic 用 npm，换 `npm run build`。

- [ ] **Step 6: 运行后端测试（最终回归）**

```bash
cd D:/Project/new-api && go test ./... 2>&1 | grep -E "FAIL|ok"
```

期望：全部 `ok`，无 `FAIL`。

- [ ] **Step 7: Commit**

```bash
cd D:/Project/new-api
git add web/classic/src/components/cost/
git commit -m "feat(cost): classic frontend — CostVersionModal, UserDiscountCell history signals, ratio_mixed"
```

