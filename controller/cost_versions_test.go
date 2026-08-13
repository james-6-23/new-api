package controller

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// costVersionPostContext 组装一次 POST /api/cost/channels/:id/versions 的调用现场。
// 路由参数必须手工塞进 c.Params：CreateChannelCostVersion 从 c.Param("id") 取渠道。
func costVersionPostContext(channelID int, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST",
		"/api/cost/channels/"+strconv.Itoa(channelID)+"/versions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channelID)}}
	c.Set("id", 1) // 中间件已验证的登录用户，落到 CreatedBy
	return c, rec
}

type costVersionResponse struct {
	Success bool                     `json:"success"`
	Message string                   `json:"message"`
	Data    model.ChannelCostVersion `json:"data"`
}

func decodeCostVersionResponse(t *testing.T, rec *httptest.ResponseRecorder) costVersionResponse {
	t.Helper()
	var resp costVersionResponse
	if err := common.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", rec.Body.String(), err)
	}
	return resp
}

// seedCostVersionChannel 建一个渠道行：CreateChannelCostVersion 会拒绝孤儿版本。
//
// 用 GORM 而非手写 INSERT：`key` 是保留字，反引号只在 MySQL/SQLite 成立，PostgreSQL
// 要双引号（CLAUDE.md 规则 2）。当前测试跑在 SQLite 上，但把不可移植的 SQL 留在测试里
// 等于给"换库跑测试"埋一颗雷。GORM 自己会按方言引用列名。
func seedCostVersionChannel(t *testing.T, channelID int) {
	t.Helper()
	ch := model.Channel{
		Id:     channelID,
		Name:   "upstream-" + strconv.Itoa(channelID),
		Key:    "sk-test",
		Status: 1,
	}
	if err := model.DB.Create(&ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

// withUSDExchangeRate 临时改写服务端汇率，测完还原（包级变量，会串到别的测试）。
func withUSDExchangeRate(t *testing.T, rate float64) {
	t.Helper()
	prev := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = rate
	t.Cleanup(func() { operation_setting.USDExchangeRate = prev })
}

// 回填历史价时，客户端传来的结算汇率必须被原样冻结。
//
// 这是整个补录功能存在的理由：六月那一版该按六月的汇率结算，而服务端只知道今天的
// 汇率。若这里用服务端汇率覆盖传入值，"冻结汇率、历史成本不漂移"这条不变量恰好在
// 它最该生效的地方被反过来用，且版本行不可变，事后无从修正。
func TestCreateChannelCostVersion_DiscountFreezesClientRate(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 7.3) // 今天的汇率，不该出现在结果里
	const chID = 5101
	seedCostVersionChannel(t, chID)

	c, rec := costVersionPostContext(chID,
		`{"effective_from":1717200000,"cost_mode":"discount","cost_discount":0.8,"exchange_rate":6.4,"note":"june"}`)
	CreateChannelCostVersion(c)

	resp := decodeCostVersionResponse(t, rec)
	if !resp.Success {
		t.Fatalf("create failed: %s", resp.Message)
	}
	if resp.Data.ExchangeRate != 6.4 {
		t.Fatalf("response exchange_rate = %v, want 6.4 (client value must win)", resp.Data.ExchangeRate)
	}

	versions, err := model.GetChannelCostVersions(chID)
	if err != nil {
		t.Fatalf("load versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(versions))
	}
	if versions[0].ExchangeRate != 6.4 {
		t.Fatalf("stored exchange_rate = %v, want 6.4", versions[0].ExchangeRate)
	}
	// 冻结的汇率必须真的进入定价：0.8 × 6.4 = 5.12。
	ratio, ok := versions[0].EffectiveRatio()
	if !ok || !nearly(ratio, 5.12) {
		t.Fatalf("EffectiveRatio() = %v (ok=%v), want 5.12", ratio, ok)
	}
}

// 不传 exchange_rate 的语义是「按当下的价记一版」，此时服务端汇率就是正确答案。
func TestCreateChannelCostVersion_DiscountFallsBackToServerRate(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 7.3)
	const chID = 5102
	seedCostVersionChannel(t, chID)

	c, rec := costVersionPostContext(chID,
		`{"effective_from":1717200000,"cost_mode":"discount","cost_discount":0.8}`)
	CreateChannelCostVersion(c)

	resp := decodeCostVersionResponse(t, rec)
	if !resp.Success {
		t.Fatalf("create failed: %s", resp.Message)
	}
	if resp.Data.ExchangeRate != 7.3 {
		t.Fatalf("exchange_rate = %v, want 7.3 (server rate is the default)", resp.Data.ExchangeRate)
	}
}

// 两边都拿不到正数汇率时拒绝写入：把 0 冻进不可变的版本行，该版本覆盖的日志会
// 永久算不出成本（EffectiveRatio 返回 ok=false），只能靠再追加一版绕过。
func TestCreateChannelCostVersion_DiscountRefusesNonPositiveRate(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 0)
	const chID = 5103
	seedCostVersionChannel(t, chID)

	for _, body := range []string{
		`{"effective_from":1717200000,"cost_mode":"discount","cost_discount":0.8}`,
		`{"effective_from":1717200000,"cost_mode":"discount","cost_discount":0.8,"exchange_rate":-1}`,
	} {
		c, rec := costVersionPostContext(chID, body)
		CreateChannelCostVersion(c)

		resp := decodeCostVersionResponse(t, rec)
		if resp.Success {
			t.Fatalf("create succeeded for %s, want refusal", body)
		}
		if !strings.Contains(resp.Message, "positive exchange rate") {
			t.Fatalf("message = %q, want it to name the missing exchange rate", resp.Message)
		}
	}

	versions, err := model.GetChannelCostVersions(chID)
	if err != nil {
		t.Fatalf("load versions: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("versions = %d, want 0 (nothing may be written on refusal)", len(versions))
	}
}

// 新建渠道时就必须落一条计价版本，而不是等下一次编辑或下一次重启。
//
// 为什么这条测试存在：追版本的钩子原先只挂在 UpdateChannel 上。于是"新建一个带价的
// 渠道 → 流量立刻进来"这条最普通的路径下，渠道全程无版本，成本报表把它算成 0 成本、
// 100% 毛利。自愈只发生在下次编辑或进程重启（两者都写 effective_from=0，会追溯覆盖），
// 所以在长期不重启的实例上，错误数字能挂满整个运行周期——且看不出任何异常。
//
// 刻意驱动真正的 AddChannel handler，而不是直接调 appendNewChannelCostVersions：
// 缺陷本身就是"钩子没挂上"，而只调那个函数的测试在钩子被摘掉后依然全绿——那样只钉住了
// 函数行为，钉不住接线。这里连 batch 模式一起走，覆盖"一次建多个渠道，每个都要有版本"。
func TestAddChannel_SeedsCostVersionForNewChannel(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 7.0)

	// batch 模式按换行拆 key，一次建出两个带价渠道 + 用另一次请求建一个无价渠道。
	addChannel(t, `{"mode":"batch","channel":{"name":"priced","type":1,"key":"ka\nkb",`+
		`"models":"gpt-4","group":"default","setting":"{\"cost_mode\":\"ratio\",\"cost_ratio\":2.5}"}}`)
	addChannel(t, `{"mode":"single","channel":{"name":"unpriced","type":1,"key":"kc",`+
		`"models":"gpt-4","group":"default","setting":"{\"force_format\":true}"}}`)

	var channels []model.Channel
	if err := model.DB.Order("id asc").Find(&channels).Error; err != nil {
		t.Fatalf("load channels: %v", err)
	}
	if len(channels) != 3 {
		t.Fatalf("channels = %d, want 3 (batch of 2 + single of 1)", len(channels))
	}

	// 走成本查询真正用的那条读路径：GetAllChannelCostVersions 返回按 effective_from
	// 升序的 VersionMap，而 VersionAt 的二分查找依赖这个升序。用 GetChannelCostVersions
	// （降序，供前端展示）拼 VersionMap 在只有一条版本时也能"通过"，却是错的用法。
	vm, err := model.GetAllChannelCostVersions()
	if err != nil {
		t.Fatalf("load version map: %v", err)
	}

	for _, ch := range channels[:2] {
		v, ok := vm.VersionAt(ch.Id, common.GetTimestamp())
		if !ok {
			t.Fatalf("channel %d (%s) has no version in effect; cost reports would show 0 cost / 100%% margin",
				ch.Id, ch.Name)
		}
		if v.EffectiveFrom != 0 {
			t.Fatalf("channel %d: effective_from = %d, want 0 so the price covers logs written before the row existed",
				ch.Id, v.EffectiveFrom)
		}
		if r, ok := v.EffectiveRatio(); !ok || r != 2.5 {
			t.Fatalf("channel %d: ratio = %v (ok=%v), want 2.5", ch.Id, r, ok)
		}
	}

	// 无成本配置的渠道不该凭空得到一个版本
	if got := mustVersions(t, channels[2].Id); len(got) != 0 {
		t.Fatalf("unpriced channel got %d versions, want 0", len(got))
	}
}

// addChannel 驱动一次 POST /api/channel/，请求失败即 Fatal（测试的前置条件，不是被测行为）。
func addChannel(t *testing.T, body string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/api/channel/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)

	AddChannel(c)

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := common.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal AddChannel response %q: %v", rec.Body.String(), err)
	}
	if !resp.Success {
		t.Fatalf("AddChannel failed: %s", resp.Message)
	}
}

func mustVersions(t *testing.T, channelID int) []model.ChannelCostVersion {
	t.Helper()
	vs, err := model.GetChannelCostVersions(channelID)
	if err != nil {
		t.Fatalf("load versions for channel %d: %v", channelID, err)
	}
	return vs
}

// 超长 note 必须在入口被拒，而不是丢给数据库去处理。
//
// 为什么这条测试存在：note 列是 varchar(255)，三个数据库对超长写入的反应完全不同
// —— PG 报 22001 直接失败，MySQL 非严格模式静默截断，SQLite 原样存下（它不强制
// varchar 长度）。同一个请求在三库上三种结果，其中两种还会让"写成功了但内容不是
// 我填的"这件事无声发生。项目要求三库行为一致（CLAUDE.md 规则 2），所以边界必须
// 由应用层划定。
//
// 用 rune 而非 byte 计数：255 个汉字是 765 字节，若按字节校验会把一条合法备注拒掉。
func TestCreateChannelCostVersion_RejectsOverlongNote(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 7.3)
	const chID = 5104
	seedCostVersionChannel(t, chID)

	// 边界内：正好 255 个汉字（765 字节）必须通过，证明校验按字符而非字节计数。
	okNote := strings.Repeat("汉", costVersionNoteMaxRunes)
	c, rec := costVersionPostContext(chID,
		`{"effective_from":1717200000,"cost_ratio":2.5,"note":"`+okNote+`"}`)
	CreateChannelCostVersion(c)
	if resp := decodeCostVersionResponse(t, rec); !resp.Success {
		t.Fatalf("255-rune note was refused: %s", resp.Message)
	}

	// 超一个字符即拒
	tooLong := strings.Repeat("a", costVersionNoteMaxRunes+1)
	c, rec = costVersionPostContext(chID,
		`{"effective_from":1717300000,"cost_ratio":2.5,"note":"`+tooLong+`"}`)
	CreateChannelCostVersion(c)
	resp := decodeCostVersionResponse(t, rec)
	if resp.Success {
		t.Fatal("256-char note was accepted; the DB would decide the outcome instead")
	}
	if !strings.Contains(resp.Message, "note") {
		t.Fatalf("message = %q, want it to name the offending field", resp.Message)
	}

	// 被拒的请求不能留下半条记录
	if got := mustVersions(t, chID); len(got) != 1 {
		t.Fatalf("versions = %d, want 1 (only the accepted one)", len(got))
	}
}

// 写入版本后必须让成本立方体缓存失效。
//
// 为什么这条测试存在：缓存里的 cost_cny 是用写入前那份 VersionMap 逐条日志算出来的。
// 不清缓存的话，「改完价立刻看报表」——这个功能最自然的操作顺序——会在最长 60 秒内
// 显示旧价，管理员据此判断"没生效"，于是重复追加版本，把价格历史搞脏。
//
// 缓存键只含查询参数、不含版本指纹，所以无法按渠道或时间局部失效：补录一条 6 月的
// 历史价会改到"上半年"这类早已缓存的区间。这条测试同时钉住"全清"这个语义。
func TestCreateChannelCostVersion_InvalidatesCostCubeCache(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 7.3)
	const chID = 5105
	seedCostVersionChannel(t, chID)

	// 塞两条不相关的缓存条目：全清语义要求它们也被丢弃（补录历史价能影响任意区间）。
	costCubeCachePut("stale-a", &costCubeCacheEntry{at: time.Now()})
	costCubeCachePut("stale-b", &costCubeCacheEntry{at: time.Now()})
	t.Cleanup(costCubeCacheClear)
	if _, ok := costCubeCacheGet("stale-a"); !ok {
		t.Fatal("precondition failed: cache entry did not land")
	}

	c, rec := costVersionPostContext(chID,
		`{"effective_from":1717200000,"cost_ratio":2.5}`)
	CreateChannelCostVersion(c)
	if resp := decodeCostVersionResponse(t, rec); !resp.Success {
		t.Fatalf("create failed: %s", resp.Message)
	}

	for _, k := range []string{"stale-a", "stale-b"} {
		if _, ok := costCubeCacheGet(k); ok {
			t.Fatalf("cache entry %q survived a version write; reports would show the old price for up to 60s", k)
		}
	}
}
