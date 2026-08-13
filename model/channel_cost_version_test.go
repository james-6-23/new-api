package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// seedLoadExchangeRate 必须从 options 表读汇率，而不是读包级变量。
//
// 为什么这条测试存在：seedChannelCostVersions 由 migrateDB() 调用，而 migrateDB()
// 跑在 InitOptionMap() **之前**（main.go: InitDB 早于 InitOptionMap）。此时
// operation_setting.USDExchangeRate 还是包级默认 7.3，管理员配置的值只存在于
// options 表里。若这里退回读包级变量，所有 discount 模式渠道的初始版本都会被
// 冻结成错误汇率 —— 而 ExchangeRate 这一列的存在意义正是防止这件事。
func TestSeedLoadExchangeRate_ReadsOptionsTableNotPackageDefault(t *testing.T) {
	DB.Exec("DELETE FROM options")
	t.Cleanup(func() { DB.Exec("DELETE FROM options") })

	// 模拟启动时序：包级变量仍是默认值，管理员配置只在 options 表里。
	oldRate := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7.3
	t.Cleanup(func() { operation_setting.USDExchangeRate = oldRate })

	if err := DB.Create(&Option{Key: "USDExchangeRate", Value: "6.5"}).Error; err != nil {
		t.Fatalf("seed option: %v", err)
	}

	if got := seedLoadExchangeRate(); got != 6.5 {
		t.Fatalf("rate = %v, want 6.5 (must read options table, not the 7.3 package default)", got)
	}
}

// options 表无该键时回退到包级变量，而不是硬编码常量。
func TestSeedLoadExchangeRate_FallsBackToPackageVarWhenAbsent(t *testing.T) {
	DB.Exec("DELETE FROM options")
	t.Cleanup(func() { DB.Exec("DELETE FROM options") })

	oldRate := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7.1
	t.Cleanup(func() { operation_setting.USDExchangeRate = oldRate })

	if got := seedLoadExchangeRate(); got != 7.1 {
		t.Fatalf("rate = %v, want 7.1 (package fallback)", got)
	}
}

// 值不可解析或非正时同样回退，不能把 0 冻进版本行（0 倍率 = 成本记 0）。
func TestSeedLoadExchangeRate_IgnoresInvalidValue(t *testing.T) {
	DB.Exec("DELETE FROM options")
	t.Cleanup(func() { DB.Exec("DELETE FROM options") })

	oldRate := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7.2
	t.Cleanup(func() { operation_setting.USDExchangeRate = oldRate })

	for _, bad := range []string{"", "abc", "0", "-1"} {
		DB.Exec("DELETE FROM options")
		if err := DB.Create(&Option{Key: "USDExchangeRate", Value: bad}).Error; err != nil {
			t.Fatalf("seed option %q: %v", bad, err)
		}
		if got := seedLoadExchangeRate(); got != 7.2 {
			t.Fatalf("value %q: rate = %v, want 7.2 (fallback)", bad, got)
		}
	}
}

// 回填的三条不变量：只为有成本配置的渠道建版本、重复调用不新增行、discount
// 渠道必须带非零冻结汇率。
//
// 注意：本测试不覆盖"单渠道插入失败不中断整批"那条错误隔离逻辑——在 SQLite 上
// 无法不依赖实现细节地稳定制造一次中途插入失败（Id 由 GORM 自增，冲突不可控），
// 该行为靠 seedChannelCostVersions 内的 SysError+continue 保证。
func TestSeedChannelCostVersions_SkipsSeededChannelsIdempotently(t *testing.T) {
	DB.Exec("DELETE FROM channel_cost_versions")
	DB.Exec("DELETE FROM channels")
	t.Cleanup(func() {
		DB.Exec("DELETE FROM channel_cost_versions")
		DB.Exec("DELETE FROM channels")
	})

	ratioSetting := `{"cost_mode":"ratio","cost_ratio":2.5}`
	discSetting := `{"cost_mode":"discount","cost_discount":0.8}`
	noCostSetting := `{"force_format":true}`
	for _, ch := range []Channel{
		{Id: 1, Name: "a", Setting: &ratioSetting},
		{Id: 2, Name: "b", Setting: &discSetting},
		{Id: 3, Name: "c", Setting: &noCostSetting},
	} {
		if err := DB.Create(&ch).Error; err != nil {
			t.Fatalf("create channel %d: %v", ch.Id, err)
		}
	}

	if err := seedChannelCostVersions(); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	var afterFirst int64
	DB.Model(&ChannelCostVersion{}).Count(&afterFirst)
	if afterFirst != 2 { // 渠道 3 无成本配置，不建版本
		t.Fatalf("after first seed: %d versions, want 2", afterFirst)
	}

	// 幂等：重复调用不新增行
	if err := seedChannelCostVersions(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	var afterSecond int64
	DB.Model(&ChannelCostVersion{}).Count(&afterSecond)
	if afterSecond != afterFirst {
		t.Fatalf("second seed added rows: %d -> %d, want idempotent", afterFirst, afterSecond)
	}

	// discount 模式渠道必须带上非零冻结汇率，否则 EffectiveRatio 返回 false（成本记 0）
	var discVer ChannelCostVersion
	if err := DB.Where("channel_id = ?", 2).First(&discVer).Error; err != nil {
		t.Fatalf("load discount version: %v", err)
	}
	if discVer.ExchangeRate <= 0 {
		t.Fatalf("discount version exchange rate = %v, want > 0", discVer.ExchangeRate)
	}
	if _, ok := discVer.EffectiveRatio(); !ok {
		t.Fatal("seeded discount version must yield a usable ratio")
	}
}

// 最后一条版本不可删：删光后该渠道全部历史日志失去成本基准，且版本行不可更新，
// 损失不可逆。
//
// 这里刻意全部用非零 effective_from：基线（effective_from=0）由
// TestDeleteChannelCostVersion_BaselineIsProtected 单独覆盖，且基线保护会先于计数
// 校验命中。用非基线行才能把「仅剩一条」这道计数校验单独钉住——它正是历史遗留的
// 无基线渠道所依赖的第二道防线。
func TestDeleteChannelCostVersionIfNotLast(t *testing.T) {
	DB.Exec("DELETE FROM channel_cost_versions")
	t.Cleanup(func() { DB.Exec("DELETE FROM channel_cost_versions") })

	mk := func(channelId int, effectiveFrom int64) *ChannelCostVersion {
		v := &ChannelCostVersion{
			ChannelId: channelId, EffectiveFrom: effectiveFrom,
			CostMode: "ratio", CostRatio: 2.5,
		}
		if err := CreateChannelCostVersion(v); err != nil {
			t.Fatalf("create version: %v", err)
		}
		return v
	}

	only := mk(1, 1000)
	if err := DeleteChannelCostVersionIfNotLast(1, only.Id); !errors.Is(err, ErrLastVersion) {
		t.Fatalf("deleting sole version: err = %v, want ErrLastVersion", err)
	}
	var stillThere int64
	DB.Model(&ChannelCostVersion{}).Where("channel_id = ?", 1).Count(&stillThere)
	if stillThere != 1 {
		t.Fatalf("sole version was deleted despite the guard: count = %d", stillThere)
	}

	// 两条时可删，且删的是指定那条
	second := mk(1, 2000)
	if err := DeleteChannelCostVersionIfNotLast(1, second.Id); err != nil {
		t.Fatalf("deleting one of two: %v", err)
	}
	var remaining []ChannelCostVersion
	DB.Where("channel_id = ?", 1).Find(&remaining)
	if len(remaining) != 1 || remaining[0].Id != only.Id {
		t.Fatalf("wrong row deleted: remaining = %+v", remaining)
	}

	// 计数按渠道隔离：别的渠道有多条，不能让本渠道的最后一条变得可删
	mk(2, 1000)
	mk(2, 3000)
	if err := DeleteChannelCostVersionIfNotLast(1, only.Id); !errors.Is(err, ErrLastVersion) {
		t.Fatalf("count must scope to the channel: err = %v, want ErrLastVersion", err)
	}
}

// effective_from=0 的基线版本不可删——这是整条历史的成本地基，且删掉无法补回。
//
// 为什么单靠 count>1 不够：渠道有 [0, T1] 两条时 count=2，校验放行，于是基线被删。
// 此后 T1 之前的全部日志 VersionAt 解析不到版本，成本永久记 0（显示成 100% 毛利）。
// 补救路径全部堵死：POST 硬拒 effective_from=0；appendCostVersionIfChanged 只在
// 渠道「零版本」时才写 0，而此时还剩 T1；重启时 seedChannelCostVersions 的 seeded
// 集合按「该渠道是否存在任意版本」判定，同样跳过。版本行又不可更新。
//
// 两个前端都已隐藏基线行的删除按钮，并注明理由是"接口会拒绝"——这条测试确保
// 那句注释成立，而不是把不变量只寄托在客户端。
func TestDeleteChannelCostVersion_BaselineIsProtected(t *testing.T) {
	DB.Exec("DELETE FROM channel_cost_versions")
	t.Cleanup(func() { DB.Exec("DELETE FROM channel_cost_versions") })

	mk := func(channelId int, effectiveFrom int64) *ChannelCostVersion {
		v := &ChannelCostVersion{
			ChannelId: channelId, EffectiveFrom: effectiveFrom,
			CostMode: "ratio", CostRatio: 2.5,
		}
		if err := CreateChannelCostVersion(v); err != nil {
			t.Fatalf("create version: %v", err)
		}
		return v
	}

	baseline := mk(7, 0)
	later := mk(7, 1754000000)

	// count=2 会让 count<=1 那道校验放行，所以这里必须由基线保护挡住。
	if err := DeleteChannelCostVersionIfNotLast(7, baseline.Id); !errors.Is(err, ErrBaselineVersion) {
		t.Fatalf("deleting the effective_from=0 baseline: err = %v, want ErrBaselineVersion", err)
	}
	var survives int64
	DB.Model(&ChannelCostVersion{}).Where("id = ?", baseline.Id).Count(&survives)
	if survives != 1 {
		t.Fatal("baseline row was deleted; every log before the next version now prices at 0")
	}

	// 非基线版本仍然可删：它可以用 POST 原样重建，删除不造成不可逆损失。
	if err := DeleteChannelCostVersionIfNotLast(7, later.Id); err != nil {
		t.Fatalf("deleting a non-baseline version: %v", err)
	}

	// 只剩基线时，两道校验都该挡住（此时 count<=1 也生效）。
	if err := DeleteChannelCostVersionIfNotLast(7, baseline.Id); err == nil {
		t.Fatal("deleting the sole remaining baseline must fail")
	}
}

// BatchInsertChannels 必须把自增主键回填进调用方的切片。
//
// 新建渠道的追版本逻辑依赖这一点：AddChannel 在批量插入之后遍历同一个切片取 Id 去写
// 版本行。若 Id 没有回填，写进去的会是 channel_id=0 —— 一批指向不存在渠道的孤儿版本，
// 而真正的新渠道仍然没有任何定价，成本永久记 0，且现象与"忘了写版本"完全一样，难查。
//
// 这条回填**不是**自动发生的，所以必须钉住：GORM 确实会把自增主键写回它收到的那个
// 切片，但 samber/lo v1.52 的 Chunk 返回的是**拷贝**而非共享底层数组的子切片
// （已实测：改 chunk[0][0] 不影响原切片），于是 GORM 只填到了那份拷贝上，函数内部
// 的 AddAbilities 能拿到正确 Id，调用方却只看到 0。BatchInsertChannels 因此显式把 Id
// 拷回原切片。升级 lo 或改写分片逻辑时，这条测试会挡住回归。
func TestBatchInsertChannels_BackfillsIds(t *testing.T) {
	DB.Exec("DELETE FROM channels")
	t.Cleanup(func() { DB.Exec("DELETE FROM channels") })

	setting := `{"cost_mode":"ratio","cost_ratio":2.5}`
	channels := []Channel{
		{Name: "batch-a", Key: "ka", Setting: &setting},
		{Name: "batch-b", Key: "kb", Setting: &setting},
	}
	if err := BatchInsertChannels(channels); err != nil {
		t.Fatalf("batch insert: %v", err)
	}
	for i, ch := range channels {
		if ch.Id <= 0 {
			t.Fatalf("channels[%d].Id = %d, want a backfilled positive id "+
				"(the add-channel cost version hook reads it straight from this slice)", i, ch.Id)
		}
	}
	if channels[0].Id == channels[1].Id {
		t.Fatalf("both channels share id %d", channels[0].Id)
	}
}
