package controller

import (
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// costIntegrationDB 用真实的 model.InitDB() 在临时目录建一个 SQLite 库。
//
// 不手搓 gorm.Open + AutoMigrate 的原因：成本核算依赖 initCol() 初始化的保留字
// 列名变量（commonGroupCol/commonKeyCol），而 initCol 是 model 包私有、只在
// chooseDB 里触发。绕过它，migrateDB 里的 seedLoadExchangeRate 就会拼出
// "SELECT ... WHERE  = ?"（key 是三库保留字）。
//
// 这里覆盖单测覆盖不到的那段：HTTP 查询参数 → buildCostCube（含真实的
// GetAllLogsForExport 流式扫描、渠道配置映射、成本版本映射）→ 逐条定价 → 折叠。
func costIntegrationDB(t *testing.T) {
	t.Helper()

	prevPath := common.SQLitePath
	prevDB, prevLogDB := model.DB, model.LOG_DB
	prevMaster := common.IsMasterNode
	prevRedis := common.RedisEnabled
	common.SQLitePath = filepath.Join(t.TempDir(), "cost-test.db")
	common.IsMasterNode = true
	// RedisEnabled 默认 true，而 RDB 只在 InitRedisClient 里赋值——测试不调它，于是
	// 任何走用户缓存的路径（如 AddChannel 结尾的审计日志）都会对 nil RDB 解引用。
	common.RedisEnabled = false
	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.SQLitePath = prevPath
		common.IsMasterNode = prevMaster
		common.RedisEnabled = prevRedis
		model.DB, model.LOG_DB = prevDB, prevLogDB
	})

	if err := model.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	model.LOG_DB = model.DB
}

// seedCostIntegrationData 造一个"跨分组专属倍率"的真实场景：
// dave 属于 vip 分组，但用 default 分组的令牌消费 —— 专属倍率 vip→default=0.7
// 在请求当时生效，已被写进日志 other 的 user_group_ratio。折扣列读的就是这个
// 历史值（而不是 vip 的一维倍率 0.9，也不是查当前配置），日志实付 700 / 刊例 1000。
//
// 渠道计价版本必须在这里显式建：seedChannelCostVersions 跑在 InitDB 内部，
// 那时渠道行还没插进去，回填自然扫不到它。
func seedCostIntegrationData(t *testing.T, at time.Time) {
	t.Helper()
	ts := at.Unix()

	if err := model.DB.Exec(
		"INSERT INTO users (id, username, password, `group`, status) VALUES (?, ?, ?, ?, ?)",
		42, "dave", "x", "vip", 1).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := model.DB.Exec(
		"INSERT INTO channels (id, name, `key`, status, setting) VALUES (?, ?, ?, ?, ?)",
		3, "upstream-a", "sk-test", 1, `{"cost_ratio":2.5}`).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	// effective_from=0：等同迁移回填出来的"自古以来"初始版本，覆盖全部日志时间点。
	if err := model.CreateChannelCostVersion(&model.ChannelCostVersion{
		ChannelId: 3, EffectiveFrom: 0, CostRatio: 2.5,
		Note: "seeded by integration test",
	}); err != nil {
		t.Fatalf("seed cost version: %v", err)
	}

	logs := []*model.Log{
		// 09 点与 11 点各一条，中间 10 点故意留空 —— 用于验证空桶补零。
		{UserId: 42, Username: "dave", CreatedAt: ts, Type: model.LogTypeConsume,
			ChannelId: 3, ModelName: "gpt-4o", Quota: 700, PromptTokens: 100,
			CompletionTokens: 50, Group: "default",
			Other: `{"model_ratio":2,"group_ratio":0.9,"user_group_ratio":0.7}`},
		{UserId: 42, Username: "dave", CreatedAt: ts + 2*3600, Type: model.LogTypeConsume,
			ChannelId: 3, ModelName: "gpt-4o", Quota: 700, PromptTokens: 100,
			CompletionTokens: 50, Group: "default",
			Other: `{"model_ratio":2,"group_ratio":0.9,"user_group_ratio":0.7}`},
	}
	for _, l := range logs {
		if err := model.DB.Create(l).Error; err != nil {
			t.Fatalf("seed log: %v", err)
		}
	}
}

func costTestContext(query string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/api/cost/users?"+query, nil)
	return c, rec
}

// costSeedBaseTime 返回一个「肯定已经过去」的整点，作为造数据的基准。
//
// 不能用「今天 09:00」：buildCostOverview 的空桶补零刻意只补到 min(end, now)
// （未来的桶尚未发生，补零会画出假的归零走势），因此 09:00 这种固定时刻在
// 11:00 之前跑测试时整段区间都在未来，补零一个桶都不补，测试随时钟红/绿。
// 从当前时间往回退，保证 at 与 at+2h 都落在过去。
func costSeedBaseTime() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.Local).
		Add(-3 * time.Hour)
}

// TestCostIntegration_UsersDimension 端到端跑通用户维度接口，验证三件事：
//  1. 折扣取的是日志里的历史专属倍率 0.7（而不是分组倍率 0.9，也不是当前配置）；
//  2. 实际加权折扣从钱反推出来（1400/2000 = 0.7）；
//  3. 版本映射确实流到了折叠层——渠道建过版本，本行必须是已定价的。
func TestCostIntegration_UsersDimension(t *testing.T) {
	costIntegrationDB(t)
	// 跨度只有几小时，落在自适应小时粒度区间内。
	at := costSeedBaseTime()
	seedCostIntegrationData(t, at)

	start := at.Add(-time.Hour).Unix()
	end := at.Add(4 * time.Hour).Unix()
	c, _ := costTestContext(
		"start_timestamp=" + strconv.FormatInt(start, 10) + "&end_timestamp=" + strconv.FormatInt(end, 10) + "&exchange_rate=7")

	data, err := buildCostCube(c)
	if err != nil {
		t.Fatalf("buildCostCube: %v", err)
	}
	if data.cube.granularity != costGranularityHour {
		t.Fatalf("granularity = %q, want hour (range is a few hours)", data.cube.granularity)
	}

	rows := foldCostCube(data.cube, costDimUser, data.channels, data.versions, data.rate, data.end)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Username != "dave" {
		t.Fatalf("username = %q", row.Username)
	}
	// 实付 1400 / 刊例 2000 = 0.7。若折扣错取 group_ratio 0.9，刊例会被还原成
	// 1555.6，这个商就成了 0.9。
	if !row.EffectiveDiscountKnown || !nearly(row.EffectiveDiscount, 0.7) {
		t.Fatalf("effective discount = %v (known=%v), want 0.7",
			row.EffectiveDiscount, row.EffectiveDiscountKnown)
	}
	// 折扣事实必须流到输出：两条日志都带 user_group_ratio，覆盖率满、命中专属倍率。
	if !row.DiscountSpecial || !nearly(row.DiscountCoverage, 1) {
		t.Fatalf("discount signals = special:%v coverage:%v, want true / 1",
			row.DiscountSpecial, row.DiscountCoverage)
	}
	// 渠道有 effective_from=0 的版本，全部刊例都定得到价。
	if !row.Priced || !nearly(row.CostCny, 0.01) {
		t.Fatalf("priced = %v cost_cny = %v, want true / 0.01", row.Priced, row.CostCny)
	}
}

// TestCostIntegration_OverviewFillsHourlyGaps 总览接口在真实数据上补齐空桶：
// 09 点与 11 点有消费，10 点无消费也必须出点，否则折线会直接跨过去。
func TestCostIntegration_OverviewFillsHourlyGaps(t *testing.T) {
	costIntegrationDB(t)
	at := costSeedBaseTime()
	seedCostIntegrationData(t, at)

	start := at.Unix()
	end := at.Add(2 * time.Hour).Unix()
	c, _ := costTestContext(
		"start_timestamp=" + strconv.FormatInt(start, 10) + "&end_timestamp=" + strconv.FormatInt(end, 10) + "&exchange_rate=7")

	data, err := buildCostCube(c)
	if err != nil {
		t.Fatalf("buildCostCube: %v", err)
	}
	ov := buildCostOverview(data.cube, data.channels, data.rate, data.start, data.end)

	if ov.Granularity != costGranularityHour {
		t.Fatalf("granularity = %q, want hour", ov.Granularity)
	}
	if len(ov.Trend) != 3 {
		t.Fatalf("trend = %d points, want 3 (09,10,11 with 10 zero-filled): %+v",
			len(ov.Trend), ov.Trend)
	}
	if ov.Trend[1].RevenueCny != 0 {
		t.Fatalf("gap hour must be zero-filled: %+v", ov.Trend[1])
	}
	if ov.Trend[0].RevenueCny == 0 || ov.Trend[2].RevenueCny == 0 {
		t.Fatalf("real hours must carry money: %+v", ov.Trend)
	}
	// 渠道建过 effective_from=0 的版本 → 不应出现在未定价警示里。
	if ov.UnpricedChannelCount != 0 {
		t.Fatalf("unpriced = %d, want 0 (channel 3 has a cost version)", ov.UnpricedChannelCount)
	}
	// 成本按版本倍率逐条累加 = 刊例$ × 2.5；刊例 quota 2000 → $0.004 → ¥0.01
	if !nearly(ov.Totals.CostCny, 0.01) {
		t.Fatalf("cost_cny = %v, want 0.01", ov.Totals.CostCny)
	}
}

// TestCostIntegration_JSONContract 前端按字段名读取，字段一旦漏了 json tag 或被
// omitempty 吞掉就会静默显示成 "-"。这里锁住新增字段真的会出现在响应里。
func TestCostIntegration_JSONContract(t *testing.T) {
	costIntegrationDB(t)
	at := costSeedBaseTime()
	seedCostIntegrationData(t, at)

	start := at.Unix()
	end := at.Add(2 * time.Hour).Unix()
	c, _ := costTestContext(
		"start_timestamp=" + strconv.FormatInt(start, 10) + "&end_timestamp=" + strconv.FormatInt(end, 10) + "&exchange_rate=7")

	data, err := buildCostCube(c)
	if err != nil {
		t.Fatalf("buildCostCube: %v", err)
	}

	ov := buildCostOverview(data.cube, data.channels, data.rate, data.start, data.end)
	ovJSON, err := common.Marshal(ov)
	if err != nil {
		t.Fatalf("marshal overview: %v", err)
	}
	var ovMap map[string]any
	if err := common.Unmarshal(ovJSON, &ovMap); err != nil {
		t.Fatalf("unmarshal overview: %v", err)
	}
	if ovMap["granularity"] != costGranularityHour {
		t.Fatalf("overview.granularity = %v, want %q", ovMap["granularity"], costGranularityHour)
	}
	// effective_discount / _known 在 totals 里（costMoney 内嵌）
	totals, _ := ovMap["totals"].(map[string]any)
	if totals == nil {
		t.Fatalf("overview.totals missing: %s", ovJSON)
	}
	for _, k := range []string{"effective_discount", "effective_discount_known"} {
		if _, ok := totals[k]; !ok {
			t.Fatalf("totals.%s missing from JSON: %v", k, totals)
		}
	}

	rows := foldCostCube(data.cube, costDimUser, data.channels, data.versions, data.rate, data.end)
	page := paginateCostRows(rows, 1, 20)
	pageJSON, err := common.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	var pageMap map[string]any
	if err := common.Unmarshal(pageJSON, &pageMap); err != nil {
		t.Fatalf("unmarshal page: %v", err)
	}
	items, _ := pageMap["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item, _ := items[0].(map[string]any)
	if got := item["effective_discount"]; got != 0.7 {
		t.Fatalf("item.effective_discount = %v, want 0.7", got)
	}
	// 折扣信号取自日志历史值：命中专属倍率、且全部刊例都带折扣信息。
	if got := item["discount_special"]; got != true {
		t.Fatalf("item.discount_special = %v, want true", got)
	}
	if got := item["discount_coverage"]; got != 1.0 {
		t.Fatalf("item.discount_coverage = %v, want 1", got)
	}
	// priced 无 omitempty：false 时也必须下发，前端靠它提示"成本被低估"。
	// effective_ratio 为区间内加权实付倍率（0.01 ÷ $0.004 = 2.5）。
	if got, ok := item["priced"]; !ok || got != true {
		t.Fatalf("item.priced = %v (present=%v), want true", got, ok)
	}
	if got := item["effective_ratio"]; got != 2.5 {
		t.Fatalf("item.effective_ratio = %v, want 2.5", got)
	}
}

func nearly(got, want float64) bool {
	d := got - want
	return d < 1e-6 && d > -1e-6
}

// 渠道的**首个**自动版本必须落在 effective_from=0，而不是 now。
//
// 用 now 会同时挖两个坑：渠道创建到首次编辑之间的日志永远落在版本区间之前，
// VersionAt 解析不到版本，那段成本永久记 0（显示成 100% 毛利）；而且一旦写入，
// seedChannelCostVersions 的 seeded 集合就认为该渠道"已回填"，重启补种也不再
// 管它。版本行不可变、创建接口又硬拒 effective_from=0，事后无从补救。
func TestAppendCostVersion_FirstVersionCoversHistory(t *testing.T) {
	costIntegrationDB(t)

	const chID = 4242
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	appendCostVersionIfChanged(c, chID, &dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.5})

	versions, err := model.GetChannelCostVersions(chID)
	if err != nil {
		t.Fatalf("load versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(versions))
	}
	if versions[0].EffectiveFrom != 0 {
		t.Fatalf("first version effective_from = %d, want 0 (must cover logs written before the first edit)",
			versions[0].EffectiveFrom)
	}

	// 第二次改价才用 now：历史区间归首版，新价从改动时刻起生效。
	appendCostVersionIfChanged(c, chID, &dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.0})
	versions, _ = model.GetChannelCostVersions(chID) // 降序，[0] 为最新
	if len(versions) != 2 {
		t.Fatalf("versions after repricing = %d, want 2", len(versions))
	}
	if versions[0].EffectiveFrom == 0 {
		t.Fatal("second version must start at the moment of change, not 0")
	}
}

// 追版本的比对基准必须是「此刻生效中」的版本，而不是 effective_from 最大的那条。
//
// GetChannelCostVersions 按 effective_from 降序返回，[0] 是时间上最靠后的一条——
// 它可能尚未生效（创建接口只拒 effective_from=0，未来时间点是允许的）。拿未来那条
// 做比对，会把「当前价确实变了」误判成没变：
//
//   基线 2.5 生效中 → 管理员排期一条明天生效的 3.0 → 再把渠道当前价改成 3.0
//   → 与未来那条一比「相等」→ 不追版本
//
// 结果是今天的流量继续按 2.5 计成本，而渠道设置里写着 3.0，两边对不上且无人察觉。
func TestAppendCostVersion_ComparesAgainstVersionInEffectNotFuture(t *testing.T) {
	costIntegrationDB(t)

	const chID = 4343
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	// 基线 2.5（自古以来生效中）
	appendCostVersionIfChanged(c, chID, &dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.5})

	// 排期一条明天才生效的 3.0
	future := &model.ChannelCostVersion{
		ChannelId: chID, EffectiveFrom: time.Now().Add(24 * time.Hour).Unix(),
		CostMode: "ratio", CostRatio: 3.0,
	}
	if err := model.CreateChannelCostVersion(future); err != nil {
		t.Fatalf("create future version: %v", err)
	}

	// 把渠道当前价改成 3.0：相对「此刻生效中」的 2.5 是改价，必须追一条版本
	appendCostVersionIfChanged(c, chID, &dto.ChannelSettings{CostMode: "ratio", CostRatio: 3.0})

	versions, err := model.GetChannelCostVersions(chID)
	if err != nil {
		t.Fatalf("load versions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("versions = %d, want 3 (baseline 2.5 + scheduled 3.0 + new 3.0 effective now); "+
			"comparing against the future row hides a real repricing", len(versions))
	}
	// 新版本必须从此刻生效，让今天的流量立刻按 3.0 计价
	now := time.Now().Unix()
	var foundNow bool
	for _, v := range versions {
		if v.EffectiveFrom > 0 && v.EffectiveFrom <= now && v.CostRatio == 3.0 {
			foundNow = true
		}
	}
	if !foundNow {
		t.Fatalf("no version priced 3.0 is in effect now; versions = %+v", versions)
	}
}
