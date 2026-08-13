package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
)

func TestBuildCostOverview_TrendAndStackAndWarning(t *testing.T) {
	cube := seedCube() // 2 users, 2 channels(3 priced, 7 unpriced), same day
	// start/end 传 0：本用例只验证折叠结果，不触发空桶补零（补零单独在
	// TestBuildCostOverviewFillsGaps 覆盖）。
	ov := buildCostOverview(cube, testChannels(), 7.0, 0, 0)
	if len(ov.Trend) != 1 || ov.Trend[0].Date != "2026-06-01" {
		t.Fatalf("trend: %+v", ov.Trend)
	}
	// stack: 每渠道一条（同一天）
	if len(ov.CostStack) != 2 {
		t.Fatalf("stack: %+v", ov.CostStack)
	}
	if ov.UnpricedChannelCount != 1 {
		t.Fatalf("unpriced = %d, want 1", ov.UnpricedChannelCount)
	}
	// 总收入 = (800+500)/5e5*7
	if ov.Totals.RevenueCny != 0.0182 {
		t.Fatalf("revenue_cny = %v", ov.Totals.RevenueCny)
	}
	// 成本 = 1000/5e5*2.5 = 0.005（ch7 为 0）
	if ov.Totals.CostCny != 0.005 {
		t.Fatalf("cost_cny = %v", ov.Totals.CostCny)
	}
}

// TestBuildCostOverview_ChannelZeroExcludedFromUnpriced 覆盖 I-1：channel_id=0
// （日志未选择渠道，非真实渠道）的错误行不应计入 unpriced 渠道数——0 只是
// "未选择渠道"的兜底值，把它算作未定价渠道会让告警横幅误报。
func TestBuildCostOverview_ChannelZeroExcludedFromUnpriced(t *testing.T) {
	// 主角必须是 channel 0 的**消费**行：错误行在累加 UnpricedListQuota 之前就 continue
	// 了，用错误行做用例时 `UnpricedListQuota > 0` 这一半条件本身就为假，把
	// `&& k.ChannelId != 0` 整个删掉断言照样通过——那样这条测试就什么都没钉住。
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
			ChannelId: 0, ModelName: "gpt-4o", Quota: 100, Other: `{"group_ratio":1}`},
	}, testVersions())
	ov := buildCostOverview(c, testChannels(), 7.0, 0, 0)
	if ov.UnpricedChannelCount != 0 {
		t.Fatalf("unpriced = %d, want 0 (channel_id=0 must be excluded)", ov.UnpricedChannelCount)
	}

	// 混合场景：channel 0 的错误行 + channel 7（testVersions 中没有版本，
	// 真实未定价渠道）的消费行 → 只有渠道 7 应计入 unpriced。
	c2 := newCostCube()
	c2.addBatch([]*model.Log{
		{Type: model.LogTypeError, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
			ChannelId: 0, ModelName: "gpt-4o"},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 7, ModelName: "gpt-4o", Quota: 100, Other: `{"group_ratio":1}`},
	}, testVersions())
	ov2 := buildCostOverview(c2, testChannels(), 7.0, 0, 0)
	if ov2.UnpricedChannelCount != 1 {
		t.Fatalf("unpriced = %d, want 1 (only real unpriced channel 7 counted)", ov2.UnpricedChannelCount)
	}
	// 警示条下钻：未定价渠道清单应带上渠道身份，且只含真实渠道 7。
	if len(ov2.UnpricedChannels) != 1 || ov2.UnpricedChannels[0].ChannelId != 7 {
		t.Fatalf("unpriced channels = %+v, want [{7 ...}]", ov2.UnpricedChannels)
	}
}

// TestNormalizeCostGranularity 自适应粒度：短区间给小时桶（页面默认筛选就是
// 「今天」，日粒度下只有一个点、根本画不出趋势），长区间给日桶；显式指定的
// hour/day 覆盖自适应。
func TestNormalizeCostGranularity(t *testing.T) {
	const day = int64(24 * 3600)
	cases := []struct {
		name       string
		raw        string
		start, end int64
		want       string
	}{
		{"single day auto -> hour", "", 0, day, costGranularityHour},
		{"one hour auto -> hour", "auto", 0, 3600, costGranularityHour},
		{"exactly 2 days auto -> hour", "", 0, 2 * day, costGranularityHour},
		{"over 2 days auto -> day", "", 0, 2*day + 1, costGranularityDay},
		{"7 days auto -> day", "", 0, 7 * day, costGranularityDay},
		{"explicit day on short range", costGranularityDay, 0, 3600, costGranularityDay},
		{"explicit hour on long range", costGranularityHour, 0, 30 * day, costGranularityHour},
		{"unknown value falls back to auto", "week", 0, 30 * day, costGranularityDay},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCostGranularity(tc.raw, tc.start, tc.end); got != tc.want {
				t.Fatalf("normalizeCostGranularity(%q, %d, %d) = %q, want %q",
					tc.raw, tc.start, tc.end, got, tc.want)
			}
		})
	}
}

// TestCostCube_HourlyBucketing 小时粒度下同一天的不同小时必须落进不同的桶
// （日粒度下它们会被合成一个点）。
func TestCostCube_HourlyBucketing(t *testing.T) {
	c := newCostCubeWithGranularity(costGranularityHour)
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 100, Other: `{"group_ratio":1}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 14), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 100, Other: `{"group_ratio":1}`},
	}, testVersions())
	if len(c.rows) != 2 {
		t.Fatalf("hourly cube rows = %d, want 2 (09 and 14 are distinct buckets)", len(c.rows))
	}
	for k := range c.rows {
		if k.Bucket != "2026-06-01 09" && k.Bucket != "2026-06-01 14" {
			t.Fatalf("unexpected bucket label %q", k.Bucket)
		}
	}
}

// TestBuildCostOverviewFillsGaps 空桶补零：区间内没有消费的时段也要出点，
// 否则折线直接跨过去，读起来像那段时间在持续盈利/亏损。补零不超过 now——
// 未来的桶尚未发生，补出来会画出一段假的归零走势。
func TestBuildCostOverviewFillsGaps(t *testing.T) {
	// 小时粒度：09 点与 12 点有消费，10/11 点应被补零 → 共 4 个点（09..12）。
	c := newCostCubeWithGranularity(costGranularityHour)
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 100, Other: `{"group_ratio":1}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 100, Other: `{"group_ratio":1}`},
	}, testVersions())
	start, end := tsOn("2026-06-01", 9), tsOn("2026-06-01", 12)
	ov := buildCostOverview(c, testChannels(), 7.0, start, end)
	if len(ov.Trend) != 4 {
		t.Fatalf("trend points = %d, want 4 (09,10,11,12): %+v", len(ov.Trend), ov.Trend)
	}
	if ov.Granularity != costGranularityHour {
		t.Fatalf("granularity = %q, want hour", ov.Granularity)
	}
	// 序列必须有序且中间两点为补零点。
	wantDates := []string{"2026-06-01 09", "2026-06-01 10", "2026-06-01 11", "2026-06-01 12"}
	for i, want := range wantDates {
		if ov.Trend[i].Date != want {
			t.Fatalf("trend[%d].Date = %q, want %q", i, ov.Trend[i].Date, want)
		}
	}
	if ov.Trend[1].RevenueCny != 0 || ov.Trend[2].RevenueCny != 0 {
		t.Fatalf("gap buckets must be zero-filled: %+v", ov.Trend)
	}
	if ov.Trend[0].RevenueCny == 0 || ov.Trend[3].RevenueCny == 0 {
		t.Fatalf("real buckets must keep their money: %+v", ov.Trend)
	}
	// 堆叠图不补零（桶×渠道笛卡尔积会放大数据量），只有真实消费的两个桶。
	if len(ov.CostStack) != 2 {
		t.Fatalf("cost stack = %d, want 2 (gaps are not filled for stacks)", len(ov.CostStack))
	}
}

// TestCostBucketRangeStopsAtNow 补零序列不越过当前时刻。
func TestCostBucketRangeStopsAtNow(t *testing.T) {
	start := tsOn("2026-06-01", 9)
	now := tsOn("2026-06-01", 11)
	end := tsOn("2026-06-01", 20) // 查询区间延伸到未来
	got := costBucketRange(start, end, costGranularityHour, now)
	want := []string{"2026-06-01 09", "2026-06-01 10", "2026-06-01 11"}
	if len(got) != len(want) {
		t.Fatalf("buckets = %v, want %v (must not run past now)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buckets = %v, want %v", got, want)
		}
	}
	// 未设置区间时不补零（沿用旧行为，供只验证折叠结果的用例）。
	if b := costBucketRange(0, 0, costGranularityDay, now); b != nil {
		t.Fatalf("zero range must produce no buckets, got %v", b)
	}
}

func TestPaginateCostRows(t *testing.T) {
	rows := foldCostCube(seedCube(), costDimUser, testChannels(), testVersions(), 7.0, testFoldEnd())
	page := paginateCostRows(rows, 1, 1)
	if page.Total != 2 || len(page.Items) != 1 {
		t.Fatalf("page: total=%d items=%d", page.Total, len(page.Items))
	}
	// 排序：收入降序 → alice(0.0112) 先于 bob(0.007)
	if page.Items[0].Username != "alice" {
		t.Fatalf("first = %q", page.Items[0].Username)
	}
	page2 := paginateCostRows(rows, 2, 1)
	if len(page2.Items) != 1 || page2.Items[0].Username != "bob" {
		t.Fatalf("page2: %+v", page2.Items)
	}
	// 总计行独立于分页
	if page.Summary.RevenueCny != page2.Summary.RevenueCny {
		t.Fatal("summary must be page-independent")
	}
}

// TestClampCostRange 覆盖 spec §5.2(4) 的时间跨度护栏：370 天上限，
// start/end 缺省（<=0）时的兜底行为。
func TestClampCostRange(t *testing.T) {
	const day = int64(24 * 3600)
	now := int64(2000000000) // 固定基准时间，避免测试依赖真实 time.Now()

	cases := []struct {
		name       string
		start, end int64
		wantStart  int64
		wantEnd    int64
	}{
		{
			name:  "zero end falls back to now",
			start: now - 10*day, end: 0,
			wantStart: now - 10*day, wantEnd: now,
		},
		{
			name:  "zero start clamps to end-370d",
			start: 0, end: now,
			wantStart: now - costCubeMaxRangeSeconds, wantEnd: now,
		},
		{
			name:  "negative start clamps to end-370d",
			start: -1, end: now,
			wantStart: now - costCubeMaxRangeSeconds, wantEnd: now,
		},
		{
			name:  "span over 370d clamps start",
			start: now - 400*day, end: now,
			wantStart: now - costCubeMaxRangeSeconds, wantEnd: now,
		},
		{
			name:  "normal span within 370d is untouched",
			start: now - 30*day, end: now,
			wantStart: now - 30*day, wantEnd: now,
		},
		{
			name:  "both zero falls back to now and 370d window",
			start: 0, end: 0,
			wantStart: now - costCubeMaxRangeSeconds, wantEnd: now,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd := clampCostRange(tc.start, tc.end, now)
			if gotStart != tc.wantStart || gotEnd != tc.wantEnd {
				t.Fatalf("clampCostRange(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.start, tc.end, now, gotStart, gotEnd, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

// TestCostCubeCacheKey_ChannelDiffers 验证 channel 过滤参数参与缓存键生成：
// 不同 channel 值必须落在不同的缓存条目上，否则切换渠道筛选会误命中旧缓存。
func TestCostCubeCacheKey_ChannelDiffers(t *testing.T) {
	base := costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 0, costGranularityDay)
	withCh3 := costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 3, costGranularityDay)
	withCh9 := costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 9, costGranularityDay)
	if base == withCh3 {
		t.Fatal("expected different cache keys for channel=0 vs channel=3")
	}
	if withCh3 == withCh9 {
		t.Fatal("expected different cache keys for channel=3 vs channel=9")
	}
	if base == withCh9 {
		t.Fatal("expected different cache keys for channel=0 vs channel=9")
	}
	// 其余参数不变时同一 channel 值必须产生同一个键（缓存才能命中）。
	if costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 3, costGranularityDay) != withCh3 {
		t.Fatal("expected identical cache key for identical params")
	}
	// 粒度必须参与键：粒度决定立方体分桶键，串用会直接产出错误的趋势序列。
	if costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 3, costGranularityHour) == withCh3 {
		t.Fatal("expected different cache keys for hour vs day granularity")
	}
}

// TestCostCubeCacheGetPut 验证缓存命中/未命中/过期逻辑。
// 通过手工构造 entry.at（而非 time.Sleep）注入"过去"的时间戳来模拟过期，
// 保持测试快速且确定。
func TestCostCubeCacheGetPut(t *testing.T) {
	key := "test-key-" + t.Name()
	defer costCubeCache.Delete(key)

	if _, ok := costCubeCacheGet(key); ok {
		t.Fatal("expected miss before any put")
	}

	fresh := &costCubeCacheEntry{cube: newCostCube(), channels: map[int]*model.ChannelCostInfo{}, rate: 7.0, at: time.Now()}
	costCubeCachePut(key, fresh)
	got, ok := costCubeCacheGet(key)
	if !ok {
		t.Fatal("expected hit right after put")
	}
	if got.rate != 7.0 {
		t.Fatalf("rate = %v, want 7.0", got.rate)
	}

	// 模拟过期：直接把时间戳写到 61 秒前（超过 60 秒 TTL）
	expired := &costCubeCacheEntry{cube: newCostCube(), channels: map[int]*model.ChannelCostInfo{}, rate: 7.0, at: time.Now().Add(-61 * time.Second)}
	costCubeCache.Store(key, expired)
	if _, ok := costCubeCacheGet(key); ok {
		t.Fatal("expected miss for expired entry")
	}
	// costCubeCacheGet 命中过期项后应主动清理
	if _, ok := costCubeCache.Load(key); ok {
		t.Fatal("expired entry should have been evicted from the map")
	}
}

// TestCostCubeCachePutEvictsOldestWhenFull 验证条目数超过上限时会淘汰最旧的一条，
// 防止无界增长。
func TestCostCubeCachePutEvictsOldestWhenFull(t *testing.T) {
	prefix := "evict-test-" + t.Name() + "-"
	defer func() {
		for i := 0; i < costCubeCacheMaxEntries+2; i++ {
			costCubeCache.Delete(prefix + string(rune('a'+i)))
		}
	}()

	base := time.Now()
	var oldestKey string
	for i := 0; i < costCubeCacheMaxEntries; i++ {
		k := prefix + string(rune('a'+i))
		if i == 0 {
			oldestKey = k
		}
		entry := &costCubeCacheEntry{
			cube: newCostCube(), channels: map[int]*model.ChannelCostInfo{}, rate: 1.0,
			// 递增时间戳，确保第一个是最旧的
			at: base.Add(time.Duration(i) * time.Millisecond),
		}
		costCubeCachePut(k, entry)
	}

	// 再插入一条，触发对已满缓存的淘汰
	newKey := prefix + string(rune('a'+costCubeCacheMaxEntries))
	costCubeCachePut(newKey, &costCubeCacheEntry{
		cube: newCostCube(), channels: map[int]*model.ChannelCostInfo{}, rate: 1.0,
		at: base.Add(time.Duration(costCubeCacheMaxEntries) * time.Millisecond),
	})

	if _, ok := costCubeCache.Load(oldestKey); ok {
		t.Fatal("oldest entry should have been evicted once cache was full")
	}
	if _, ok := costCubeCache.Load(newKey); !ok {
		t.Fatal("newly inserted entry should be present")
	}
}
