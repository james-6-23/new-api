package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// lastPoint 取序列末位。时间轴固定 24 个桶且右端对齐当前小时，
// 因此当 nowTs 与 bucketTs 同属一个小时时，数据落在最后一个桶。
func lastPoint(points []*float64) *float64 {
	return points[len(points)-1]
}

// availRow 是构造测试数据的便捷函数。
func availRow(modelName, group string, bucketTs, req, ok, lat, ttftSum, ttftN, out, gen int64) AvailabilityRow {
	return AvailabilityRow{
		ModelName:      modelName,
		GroupName:      group,
		BucketTs:       bucketTs,
		RequestCount:   req,
		SuccessCount:   ok,
		TotalLatencyMs: lat,
		TtftSumMs:      ttftSum,
		TtftCount:      ttftN,
		OutputTokens:   out,
		GenerationMs:   gen,
	}
}

func TestBuildAvailabilityGroupDimension(t *testing.T) {
	rows := []AvailabilityRow{
		availRow("gpt-4", "default", 3600, 100, 99, 100000, 20000, 100, 5000, 10000),
	}

	got := BuildAvailability(rows, "group", 7200)

	require.Equal(t, "group", got.Dimension)
	require.Len(t, got.Entities, 1)
	require.Equal(t, "default", got.Entities[0].Id)
	require.Equal(t, int64(100), got.Entities[0].Requests)
	// 分组维度下，子线是模型
	require.Len(t, got.Entities[0].Metrics["successRate"].Lines, 1)
	require.Equal(t, "gpt-4", got.Entities[0].Metrics["successRate"].Lines[0].Name)
}

func TestBuildAvailabilityModelDimension(t *testing.T) {
	rows := []AvailabilityRow{
		availRow("gpt-4", "default", 3600, 100, 99, 100000, 20000, 100, 5000, 10000),
	}

	got := BuildAvailability(rows, "model", 7200)

	require.Len(t, got.Entities, 1)
	require.Equal(t, "gpt-4", got.Entities[0].Id)
	// 模型维度下，子线是分组
	require.Equal(t, "default", got.Entities[0].Metrics["successRate"].Lines[0].Name)
}

func TestBuildAvailabilityHoursAxisIsFixed24(t *testing.T) {
	got := BuildAvailability(nil, "group", 3600*50)

	// 无实体时不下发 hours；有实体时长度恒为 24
	rows := []AvailabilityRow{
		availRow("a", "default", 3600*50, 1, 1, 10, 1, 1, 1, 10),
	}
	got = BuildAvailability(rows, "group", 3600*50)

	require.Len(t, got.Entities[0].Hours, 24)
	require.Len(t, got.Entities[0].Metrics["successRate"].Best, 24)
	require.Len(t, got.Entities[0].Metrics["successRate"].Lines[0].Points, 24)
}

func TestBuildAvailabilityDerivedMetrics(t *testing.T) {
	// 100 请求 / 99 成功 → 99%；总时延 100000ms/100 → 1000ms
	// ttft 20000ms/100 → 200ms；5000 tokens / 10s → 500 tps
	rows := []AvailabilityRow{
		availRow("gpt-4", "default", 3600, 100, 99, 100000, 20000, 100, 5000, 10000),
	}

	got := BuildAvailability(rows, "group", 3600)
	cur := got.Entities[0].Current

	require.NotNil(t, cur.SuccessRatePct)
	require.InDelta(t, 99.0, *cur.SuccessRatePct, 0.001)
	require.NotNil(t, cur.LatencyMs)
	require.InDelta(t, 1000.0, *cur.LatencyMs, 0.001)
	require.NotNil(t, cur.TtftMs)
	require.InDelta(t, 200.0, *cur.TtftMs, 0.001)
	require.NotNil(t, cur.Tps)
	require.InDelta(t, 500.0, *cur.Tps, 0.001)
}

func TestBuildAvailabilityBestIsMaxForHigherIsBetter(t *testing.T) {
	rows := []AvailabilityRow{
		availRow("a", "default", 3600, 100, 90, 100000, 10000, 100, 1000, 10000), // 90%, 100tps
		availRow("b", "default", 3600, 100, 99, 300000, 30000, 100, 3000, 10000), // 99%, 300tps
	}

	got := BuildAvailability(rows, "group", 3600)

	best := got.Entities[0].Metrics["successRate"].Best
	require.Len(t, best, 24)
	require.NotNil(t, lastPoint(best))
	// successRate 越高越好 → 取 max
	require.InDelta(t, 99.0, *lastPoint(best), 0.001)

	// tps 越高越好 → 取 max
	bestTps := got.Entities[0].Metrics["tps"].Best
	require.NotNil(t, lastPoint(bestTps))
	require.InDelta(t, 300.0, *lastPoint(bestTps), 0.001)
}

func TestBuildAvailabilityBestIsMinForLowerIsBetter(t *testing.T) {
	rows := []AvailabilityRow{
		availRow("a", "default", 3600, 100, 100, 100000, 10000, 100, 1000, 10000), // lat 1000, ttft 100
		availRow("b", "default", 3600, 100, 100, 300000, 30000, 100, 3000, 10000), // lat 3000, ttft 300
	}

	got := BuildAvailability(rows, "group", 3600)

	// ttft / latency 越低越好 → 取 min
	bestTtft := got.Entities[0].Metrics["ttft"].Best
	require.NotNil(t, lastPoint(bestTtft))
	require.InDelta(t, 100.0, *lastPoint(bestTtft), 0.001)

	bestLatency := got.Entities[0].Metrics["latency"].Best
	require.NotNil(t, lastPoint(bestLatency))
	require.InDelta(t, 1000.0, *lastPoint(bestLatency), 0.001)
}

func TestBuildAvailabilityEmptyBucketIsNullNotZero(t *testing.T) {
	rows := []AvailabilityRow{
		availRow("a", "default", 3600, 100, 100, 100000, 10000, 100, 1000, 10000),
		availRow("a", "default", 10800, 100, 100, 100000, 10000, 100, 1000, 10000),
	}

	got := BuildAvailability(rows, "group", 10800)

	points := got.Entities[0].Metrics["successRate"].Lines[0].Points
	// 空桶必须是 nil（JSON null），不能是 0——否则图表会读成性能塌陷
	var sawNil bool
	for _, p := range points {
		if p == nil {
			sawNil = true
		}
	}
	require.True(t, sawNil, "空桶必须产出 nil 而非 0")
	// 7200 这个桶无数据
	require.Nil(t, points[len(points)-2])
	require.NotNil(t, lastPoint(points))
}

func TestBuildAvailabilityNilTtftWhenNoStreamSamples(t *testing.T) {
	// ttftCount = 0 → ttft 无从计算
	rows := []AvailabilityRow{
		availRow("a", "default", 3600, 100, 100, 100000, 0, 0, 1000, 10000),
	}

	got := BuildAvailability(rows, "group", 3600)

	require.Nil(t, got.Entities[0].Current.TtftMs)
	require.Nil(t, lastPoint(got.Entities[0].Metrics["ttft"].Lines[0].Points))
	// best 也必须是 nil，不能因为没有候选值而退化成 0
	require.Nil(t, lastPoint(got.Entities[0].Metrics["ttft"].Best))
}

func TestBuildAvailabilityTruncatesEntities(t *testing.T) {
	var rows []AvailabilityRow
	// 15 个分组，请求数递减
	for i := 0; i < 15; i++ {
		rows = append(rows, availRow("m", string(rune('a'+i)), 3600, int64(100-i), int64(100-i), 1000, 100, 1, 10, 100))
	}

	got := BuildAvailability(rows, "group", 3600)

	require.Len(t, got.Entities, MaxAvailabilityEntities)
	require.True(t, got.Truncated)
	// 按请求数降序，最大的排第一
	require.Equal(t, "a", got.Entities[0].Id)
}

func TestBuildAvailabilityTruncatesLines(t *testing.T) {
	var rows []AvailabilityRow
	// 一个分组下 10 个模型
	for i := 0; i < 10; i++ {
		rows = append(rows, availRow(string(rune('a'+i)), "default", 3600, int64(100-i), int64(100-i), 1000, 100, 1, 10, 100))
	}

	got := BuildAvailability(rows, "group", 3600)

	require.Len(t, got.Entities, 1)
	require.Len(t, got.Entities[0].Metrics["successRate"].Lines, MaxAvailabilityLines)
	require.True(t, got.Truncated)
}

func TestBuildAvailabilityNotTruncatedAtExactCap(t *testing.T) {
	var rows []AvailabilityRow
	for i := 0; i < MaxAvailabilityEntities; i++ {
		rows = append(rows, availRow("m", string(rune('a'+i)), 3600, int64(100-i), int64(100-i), 1000, 100, 1, 10, 100))
	}

	got := BuildAvailability(rows, "group", 3600)

	require.Len(t, got.Entities, MaxAvailabilityEntities)
	require.False(t, got.Truncated, "恰好等于上限时不应置位 truncated")
}

func TestBuildAvailabilityCurrentUsesLastThreeBuckets(t *testing.T) {
	// 老桶全失败，最近三桶全成功。current 只应反映最近三桶。
	rows := []AvailabilityRow{
		availRow("a", "default", 3600, 100, 0, 1000, 100, 1, 10, 100),
		availRow("a", "default", 3600*10, 10, 10, 100, 10, 1, 10, 100),
		availRow("a", "default", 3600*11, 10, 10, 100, 10, 1, 10, 100),
		availRow("a", "default", 3600*12, 10, 10, 100, 10, 1, 10, 100),
	}

	got := BuildAvailability(rows, "group", 3600*12)

	require.NotNil(t, got.Entities[0].Current.SuccessRatePct)
	require.InDelta(t, 100.0, *got.Entities[0].Current.SuccessRatePct, 0.001)
	// 但 24h 请求总数包含全部
	require.Equal(t, int64(130), got.Entities[0].Requests)
}

func TestBuildAvailabilityCurrentAggregatesAcrossLines(t *testing.T) {
	// 同一桶两条子线：一条全成功、一条全失败 → current 应是合计 50%
	rows := []AvailabilityRow{
		availRow("a", "default", 3600, 100, 100, 1000, 100, 1, 10, 100),
		availRow("b", "default", 3600, 100, 0, 1000, 100, 1, 10, 100),
	}

	got := BuildAvailability(rows, "group", 3600)

	require.InDelta(t, 50.0, *got.Entities[0].Current.SuccessRatePct, 0.001)
}

func TestBuildAvailabilityDropsRowsOutsideWindow(t *testing.T) {
	rows := []AvailabilityRow{
		availRow("a", "default", 3600, 100, 100, 1000, 100, 1, 10, 100),
		// 远早于 24h 窗口起点，应被丢弃且不计入 requests
		availRow("a", "default", 3600-3600*100, 999, 999, 1000, 100, 1, 10, 100),
	}

	got := BuildAvailability(rows, "group", 3600)

	require.Len(t, got.Entities, 1)
	require.Equal(t, int64(100), got.Entities[0].Requests)
}

func TestBuildAvailabilitySkipsBlankIdentifiers(t *testing.T) {
	rows := []AvailabilityRow{
		availRow("", "default", 3600, 100, 100, 1000, 100, 1, 10, 100),
		availRow("a", "", 3600, 100, 100, 1000, 100, 1, 10, 100),
		availRow("a", "default", 3600, 50, 50, 1000, 100, 1, 10, 100),
	}

	got := BuildAvailability(rows, "group", 3600)

	require.Len(t, got.Entities, 1)
	require.Equal(t, int64(50), got.Entities[0].Requests)
}

func TestBuildAvailabilityEmptyInput(t *testing.T) {
	got := BuildAvailability(nil, "group", 3600)

	require.Empty(t, got.Entities)
	require.False(t, got.Truncated)
	require.Equal(t, int64(3600), got.GeneratedAt)
	// 必须是空数组而非 nil，避免序列化成 JSON null
	require.NotNil(t, got.Entities)
}

func TestAvailabilityStartTimeAlignsToHour(t *testing.T) {
	// 12:34:56 → 窗口起点应是 23 小时前的整点
	nowTs := int64(3600*12 + 34*60 + 56)

	got := AvailabilityStartTime(nowTs)

	require.Equal(t, int64(0), got%3600, "起点必须对齐整点")
	require.Equal(t, int64(3600*12)-23*3600, got)
}

// TestQueryAvailabilityRowsAgainstSQLite 验证聚合 SQL 能真实执行。
// group 是保留字，靠 commonGroupCol 转义；此处确认 SELECT 别名与
// GROUP BY 在真实数据库上都成立，而不只是拼串看起来对。
func TestQueryAvailabilityRowsAgainstSQLite(t *testing.T) {
	t.Cleanup(func() { DB.Exec("DELETE FROM perf_metrics") })

	seed := []PerfMetric{
		{ModelName: "gpt-4", Group: "default", BucketTs: 3600, RequestCount: 10, SuccessCount: 9, TotalLatencyMs: 5000, TtftSumMs: 1000, TtftCount: 10, OutputTokens: 500, GenerationMs: 2000},
		{ModelName: "gpt-4", Group: "vip", BucketTs: 3600, RequestCount: 5, SuccessCount: 5, TotalLatencyMs: 1000, TtftSumMs: 200, TtftCount: 5, OutputTokens: 100, GenerationMs: 500},
		// 窗口之外，不应被查出
		{ModelName: "gpt-4", Group: "default", BucketTs: 1800, RequestCount: 99, SuccessCount: 99},
	}
	require.NoError(t, DB.Create(&seed).Error)

	rows, err := QueryAvailabilityRows(3600)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// group 列必须正确落到 GroupName 字段
	byGroup := map[string]AvailabilityRow{}
	for _, r := range rows {
		byGroup[r.GroupName] = r
	}
	require.Contains(t, byGroup, "default")
	require.Contains(t, byGroup, "vip")
	require.Equal(t, int64(10), byGroup["default"].RequestCount)
	require.Equal(t, int64(9), byGroup["default"].SuccessCount)
	require.Equal(t, int64(5000), byGroup["default"].TotalLatencyMs)
	require.Equal(t, "gpt-4", byGroup["default"].ModelName)

	// 端到端：查询结果能被透视
	resp := BuildAvailability(rows, "group", 3600)
	require.Len(t, resp.Entities, 2)
	require.Equal(t, "default", resp.Entities[0].Id) // 请求数更多
}

// TestBuildAvailabilityAccumulatesDuplicateKeys 锁定 controller 合并热桶所依赖的前提：
// 同一 (模型, 分组, 桶) 出现多行时必须累加而非覆盖。
// 库里已落盘的行与内存热桶的行会以相同键同时传入，若是覆盖语义，
// 当前小时的数据就会互相顶掉。
func TestBuildAvailabilityAccumulatesDuplicateKeys(t *testing.T) {
	rows := []AvailabilityRow{
		{ModelName: "gpt-4", GroupName: "default", BucketTs: 3600, RequestCount: 6, SuccessCount: 6, TotalLatencyMs: 600},
		{ModelName: "gpt-4", GroupName: "default", BucketTs: 3600, RequestCount: 4, SuccessCount: 2, TotalLatencyMs: 400},
	}

	resp := BuildAvailability(rows, "group", 3600)
	require.Len(t, resp.Entities, 1)

	entity := resp.Entities[0]
	require.Equal(t, int64(10), entity.Requests, "两行必须累加为 10 次请求")

	require.NotNil(t, entity.Current.SuccessRatePct)
	require.InDelta(t, 80.0, *entity.Current.SuccessRatePct, 0.001, "成功率应按合计计算 8/10")

	require.NotNil(t, entity.Current.LatencyMs)
	require.InDelta(t, 100.0, *entity.Current.LatencyMs, 0.001, "延迟应按合计计算 1000/10")
}
