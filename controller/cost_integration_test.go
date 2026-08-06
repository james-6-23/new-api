package controller

import (
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// costIntegrationDB 用真实的 model.InitDB() 在临时目录建一个 SQLite 库。
//
// 不手搓 gorm.Open + AutoMigrate 的原因：成本核算的查询依赖 initCol() 初始化的
// 保留字列名变量（commonGroupCol 等），而 initCol 是 model 包私有、只在 chooseDB
// 里触发。绕过它会让 GetAllUserGroups 拼出 "SELECT username,  FROM users"。
//
// 这里覆盖单测覆盖不到的那段：HTTP 查询参数 → buildCostCube（含真实的
// GetAllLogsForExport 流式扫描、渠道倍率映射、用户分组映射）→ 折叠 → 折扣解析。
func costIntegrationDB(t *testing.T) {
	t.Helper()

	prevPath := common.SQLitePath
	prevDB, prevLogDB := model.DB, model.LOG_DB
	prevMaster := common.IsMasterNode
	common.SQLitePath = filepath.Join(t.TempDir(), "cost-test.db")
	common.IsMasterNode = true
	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.SQLitePath = prevPath
		common.IsMasterNode = prevMaster
		model.DB, model.LOG_DB = prevDB, prevLogDB
	})

	if err := model.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	model.LOG_DB = model.DB
}

// seedCostIntegrationData 造一个"跨分组专属倍率"的真实场景：
// dave 属于 vip 分组，但用 default 分组的令牌消费 —— 专属倍率 vip→default=0.7
// 生效，日志实付 700 / 刊例 1000。旧实现（只查 GetGroupGroupRatio(g,g)）在这里
// 会回退到 vip 的一维倍率 0.9，与实际不符。
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

	if err := ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":0.9}`); err != nil {
		t.Fatal(err)
	}
	if err := ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"default":0.7}}`); err != nil {
		t.Fatal(err)
	}
}

func costTestContext(query string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/api/cost/users?"+query, nil)
	return c, rec
}

// TestCostIntegration_UsersDimension 端到端跑通用户维度接口，验证三件事：
//  1. 跨分组专属倍率被正确解析（0.7，而不是 vip 的一维倍率 0.9）；
//  2. 实际加权折扣从钱反推出来（700/1000 = 0.7）且与配置一致；
//  3. 使用分组额度确实流到了折扣解析（UsingGroupQuota 非空）。
func TestCostIntegration_UsersDimension(t *testing.T) {
	costIntegrationDB(t)
	// 用"今天 09:00"，让查询落在自适应小时粒度区间内。
	now := time.Now()
	at := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local)
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
	if got := data.userGroups["dave"]; got != "vip" {
		t.Fatalf("user group = %q, want vip", got)
	}

	rows := foldCostCube(data.cube, costDimUser, data.channels, data.rate)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Username != "dave" {
		t.Fatalf("username = %q", row.Username)
	}
	// 使用分组额度必须流到折扣解析，否则二维查表退化成对角线。
	if row.UsingGroupQuota["default"] != 1400 {
		t.Fatalf("using-group quota = %+v, want default:1400", row.UsingGroupQuota)
	}
	// 实付 1400 / 刊例 2000 = 0.7
	if !row.EffectiveDiscountKnown || !nearly(row.EffectiveDiscount, 0.7) {
		t.Fatalf("effective discount = %v (known=%v), want 0.7",
			row.EffectiveDiscount, row.EffectiveDiscountKnown)
	}

	attachUserGroupRatios(rows, data.userGroups)
	if !rows[0].GroupRatioKnown || !rows[0].GroupRatioSpecial {
		t.Fatalf("cross-group dedicated ratio not resolved: %+v", rows[0])
	}
	if !nearly(rows[0].GroupRatio, 0.7) {
		t.Fatalf("configured ratio = %v, want 0.7 (dedicated vip→default), not 0.9",
			rows[0].GroupRatio)
	}
}

// TestCostIntegration_OverviewFillsHourlyGaps 总览接口在真实数据上补齐空桶：
// 09 点与 11 点有消费，10 点无消费也必须出点，否则折线会直接跨过去。
func TestCostIntegration_OverviewFillsHourlyGaps(t *testing.T) {
	costIntegrationDB(t)
	now := time.Now()
	at := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local)
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
	// 渠道配了 cost_ratio=2.5 → 不应出现在未定价警示里。
	if ov.UnpricedChannelCount != 0 {
		t.Fatalf("unpriced = %d, want 0 (channel 3 has cost_ratio)", ov.UnpricedChannelCount)
	}
	// 成本 = 刊例$ × 2.5；刊例 quota 2000 → $0.004 → ¥0.01
	if !nearly(ov.Totals.CostCny, 0.01) {
		t.Fatalf("cost_cny = %v, want 0.01", ov.Totals.CostCny)
	}
}

// TestCostIntegration_JSONContract 前端按字段名读取，字段一旦漏了 json tag 或被
// omitempty 吞掉就会静默显示成 "-"。这里锁住新增字段真的会出现在响应里。
func TestCostIntegration_JSONContract(t *testing.T) {
	costIntegrationDB(t)
	now := time.Now()
	at := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local)
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

	rows := foldCostCube(data.cube, costDimUser, data.channels, data.rate)
	attachUserGroupRatios(rows, data.userGroups)
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
	if got := item["group_ratio"]; got != 0.7 {
		t.Fatalf("item.group_ratio = %v, want 0.7 (dedicated vip→default)", got)
	}
	if got := item["group_ratio_special"]; got != true {
		t.Fatalf("item.group_ratio_special = %v, want true", got)
	}
	// 内部字段不得泄漏到响应（json:"-"）。
	if _, leaked := item["UsingGroupQuota"]; leaked {
		t.Fatalf("UsingGroupQuota must not be serialized: %v", item)
	}
}

func nearly(got, want float64) bool {
	d := got - want
	return d < 1e-6 && d > -1e-6
}
