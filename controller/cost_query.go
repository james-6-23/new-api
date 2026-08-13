package controller

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type costTrendPoint struct {
	Date       string  `json:"date"`
	RevenueCny float64 `json:"revenue_cny"`
	CostCny    float64 `json:"cost_cny"`
	ProfitCny  float64 `json:"profit_cny"`
}

type costStackPoint struct {
	Date        string  `json:"date"`
	ChannelId   int     `json:"channel_id"`
	ChannelName string  `json:"channel_name"`
	CostCny     float64 `json:"cost_cny"`
}

// costUnpricedChannel 未配置成本计价的渠道身份，供前端警示条下钻（点进供应商
// 维度直接定位）。只下发 id/name，倍率本身在维度接口里。
type costUnpricedChannel struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name,omitempty"`
}

type costOverviewDTO struct {
	Totals               costMoney             `json:"totals"`
	UnpricedChannelCount int                   `json:"unpriced_channel_count"`
	UnpricedChannels     []costUnpricedChannel `json:"unpriced_channels,omitempty"`
	ExchangeRate         float64               `json:"exchange_rate"`
	// Granularity 本次趋势/堆叠序列的时间桶粒度（hour|day），前端据此切换
	// 轴标签格式（HH:mm / MM-DD）。
	Granularity string           `json:"granularity"`
	Trend       []costTrendPoint `json:"trend"`
	CostStack   []costStackPoint `json:"cost_stack"`
}

type costPageDTO struct {
	Items    []costDimensionRow `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Summary  costMoney          `json:"summary"`
}

// costCubeMaxRangeSeconds 时间跨度上限：370 天。超出的查询会把 start 钳制到
// end - 370 天，避免一次请求把全部历史日志流式扫描一遍（spec §5.2(4)）。
const costCubeMaxRangeSeconds = int64(370 * 24 * 3600)

// clampCostRange 归一化查询时间范围：
//   - end<=0（未传）时取当前时间；
//   - start<=0（未传）或跨度超过 370 天时，把 start 钳制为 end-370天。
//
// 提取成纯函数便于单元测试，不依赖 gin.Context / time.Now。
func clampCostRange(start, end, now int64) (int64, int64) {
	if end <= 0 {
		end = now
	}
	if start <= 0 || end-start > costCubeMaxRangeSeconds {
		start = end - costCubeMaxRangeSeconds
	}
	return start, end
}

// costCubeCacheEntry 缓存一次 buildCostCube 的完整产出（立方体 + 渠道配置映射 +
// 成本版本映射 + 汇率）。版本映射随立方体一起缓存：立方体里的成本已按这份版本
// 算好，分开加载会让两者不同步。
type costCubeCacheEntry struct {
	cube     *costCube
	channels map[int]*model.ChannelCostInfo
	versions model.VersionMap
	rate     float64
	at       time.Time
}

// costCubeCache 一期护栏：60 秒内相同查询参数直接复用结果，避免同一页面（总览 +
// 三个维度表）多次点击重复流式扫描整段日志。一期仅内存缓存（不接 Redis），
// 使用 sync.Map 做简单的进程内存储；写入时顺带清理过期项并在条目数超过上限时
// 淘汰最旧的一条，避免无界增长。
var costCubeCache sync.Map

const (
	costCubeCacheTTL        = 60 * time.Second
	costCubeCacheMaxEntries = 32
)

// costCubeCacheKey 由归一化后的查询参数拼接生成缓存键（含 channel 过滤条件与
// 时间桶粒度，不同筛选/粒度不应互相命中彼此的缓存——粒度决定了立方体的分桶键，
// 复用会直接串出错误的趋势序列）。
func costCubeCacheKey(start, end int64, modelName, username string, rate float64, channel int, granularity string) string {
	return fmt.Sprintf("%d|%d|%s|%s|%.6f|%d|%s", start, end, modelName, username, rate, channel, granularity)
}

func costCubeCacheGet(key string) (*costCubeCacheEntry, bool) {
	v, ok := costCubeCache.Load(key)
	if !ok {
		return nil, false
	}
	entry := v.(*costCubeCacheEntry)
	if time.Since(entry.at) > costCubeCacheTTL {
		costCubeCache.Delete(key)
		return nil, false
	}
	return entry, true
}

func costCubeCachePut(key string, entry *costCubeCacheEntry) {
	// 清理过期项，同时统计存活条目数，超过上限时淘汰其中最旧的一条
	// （简单策略，非严格 LRU，够用即可）。
	var oldestKey any
	var oldestAt time.Time
	count := 0
	costCubeCache.Range(func(k, v any) bool {
		e, _ := v.(*costCubeCacheEntry)
		if e == nil || time.Since(e.at) > costCubeCacheTTL {
			costCubeCache.Delete(k)
			return true
		}
		count++
		if oldestKey == nil || e.at.Before(oldestAt) {
			oldestKey = k
			oldestAt = e.at
		}
		return true
	})
	if count >= costCubeCacheMaxEntries && oldestKey != nil {
		costCubeCache.Delete(oldestKey)
	}
	costCubeCache.Store(key, entry)
}

// costCubeCacheClear 丢弃全部缓存条目，在计价版本发生变化时调用。
//
// 为什么必须全清而不是按渠道/时间清：缓存里的 cost_cny 是用当时那份 VersionMap
// 逐条日志算出来的，一条新版本行会改写它覆盖区间内所有日志的成本，而缓存键只含
// 查询参数、不含版本指纹，无法判断哪些条目受影响（补录一条 6 月的历史价甚至会
// 改到"上半年"这种早已缓存的区间）。
//
// 不清的后果不是"数字晚 60 秒更新"这么轻——「改完价立刻看报表」正是这个功能最
// 自然的操作顺序，管理员会看到旧价、以为没生效，于是重复追加版本。
func costCubeCacheClear() {
	costCubeCache.Range(func(k, _ any) bool {
		costCubeCache.Delete(k)
		return true
	})
}

// costCubeData 一次 buildCostCube 的产出集合，避免多返回值随字段增长继续膨胀。
type costCubeData struct {
	cube     *costCube
	channels map[int]*model.ChannelCostInfo
	versions model.VersionMap
	rate     float64
	start    int64
	end      int64
}

// buildCostCube 流式扫描日志构建立方体，同时载入渠道配置映射、成本版本映射与汇率。
// 版本必须在扫描前载入——逐条定价发生在 addBatch 内。
// 命中 60 秒内相同参数的缓存时直接复用，否则重新扫描并写入缓存。
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

// buildCostOverview 从立方体折出总览：总计 + 趋势序列 + 按渠道的成本堆叠。
// start/end 为归一化后的查询区间，用于补齐无消费的空桶——不补的话折线会直接
// 跨过没有消费的时段，视觉上读成"那段时间在盈利/亏损"。
func buildCostOverview(cube *costCube, channels map[int]*model.ChannelCostInfo, rate float64, start, end int64) costOverviewDTO {
	ov := costOverviewDTO{ExchangeRate: rate, Granularity: cube.granularity}
	trend := make(map[string]*costTrendPoint)
	stack := make(map[string]*costStackPoint) // key: bucket|channelId
	unpriced := make(map[int]string)
	for k, r := range cube.rows {
		chName := ""
		if ci := channels[k.ChannelId]; ci != nil {
			chName = ci.Name
		}
		m := costMoneyFromRow(r, rate)
		ov.Totals.add(m)
		// 未定价的判据是"这些刊例金额找不到生效版本"，不是"当前倍率为 0"——渠道
		// 今天配了价也不代表历史每一条日志都有版本可查。
		// channel_id == 0 表示日志未选择任何渠道（旧日志/兜底值），不是一个
		// "未定价"的真实渠道，计入 unpriced 会让告警横幅误报，故跳过。
		if r.UnpricedListQuota > 0 && k.ChannelId != 0 {
			unpriced[k.ChannelId] = chName
		}
		tp := trend[k.Bucket]
		if tp == nil {
			tp = &costTrendPoint{Date: k.Bucket}
			trend[k.Bucket] = tp
		}
		tp.RevenueCny = roundTo6(tp.RevenueCny + m.RevenueCny)
		tp.CostCny = roundTo6(tp.CostCny + m.CostCny)
		tp.ProfitCny = roundTo6(tp.ProfitCny + m.ProfitCny)

		sk := k.Bucket + "|" + strconv.Itoa(k.ChannelId)
		sp := stack[sk]
		if sp == nil {
			sp = &costStackPoint{Date: k.Bucket, ChannelId: k.ChannelId, ChannelName: chName}
			stack[sk] = sp
		}
		sp.CostCny = roundTo6(sp.CostCny + m.CostCny)
	}
	ov.UnpricedChannelCount = len(unpriced)
	for id, name := range unpriced {
		ov.UnpricedChannels = append(ov.UnpricedChannels, costUnpricedChannel{ChannelId: id, ChannelName: name})
	}
	sort.Slice(ov.UnpricedChannels, func(i, j int) bool {
		return ov.UnpricedChannels[i].ChannelId < ov.UnpricedChannels[j].ChannelId
	})
	// 补齐区间内没有任何消费的桶（补零），让趋势线连续。只补到 min(end, now)：
	// 未来的桶尚未发生，补零会画出一段假的归零走势。
	for _, bucket := range costBucketRange(start, end, cube.granularity, time.Now().Unix()) {
		if trend[bucket] == nil {
			trend[bucket] = &costTrendPoint{Date: bucket}
		}
	}
	for _, tp := range trend {
		ov.Trend = append(ov.Trend, *tp)
	}
	sort.Slice(ov.Trend, func(i, j int) bool { return ov.Trend[i].Date < ov.Trend[j].Date })
	for _, sp := range stack {
		ov.CostStack = append(ov.CostStack, *sp)
	}
	sort.Slice(ov.CostStack, func(i, j int) bool {
		if ov.CostStack[i].Date != ov.CostStack[j].Date {
			return ov.CostStack[i].Date < ov.CostStack[j].Date
		}
		return ov.CostStack[i].ChannelId < ov.CostStack[j].ChannelId
	})
	return ov
}

// costBucketRange 枚举 [start, min(end, now)] 覆盖的全部桶标签。
// 堆叠图不在这里补零：那是「桶 × 渠道」的笛卡尔积，补齐会让数据量按渠道数放大，
// 而堆叠柱本身缺失即为 0 高度，视觉上无歧义。
func costBucketRange(start, end int64, granularity string, now int64) []string {
	if start <= 0 || end <= start {
		return nil
	}
	if end > now {
		end = now
	}
	if end < start {
		return nil
	}
	step := costBucketStep(granularity)
	cursor := costBucketTruncate(time.Unix(start, 0), granularity)
	last := time.Unix(end, 0)
	buckets := make([]string, 0, 32)
	// 上限护栏：370 天日粒度约 371 个桶；小时粒度受 2 天自适应上限约束约 49 个。
	// 手动指定 hour + 长区间时这里会被 maxBuckets 截断，避免生成上万个点。
	const maxBuckets = 800
	for !cursor.After(last) && len(buckets) < maxBuckets {
		buckets = append(buckets, costBucketLabel(cursor, granularity))
		cursor = cursor.Add(step)
	}
	return buckets
}

func paginateCostRows(rows []costDimensionRow, page, pageSize int) costPageDTO {
	var summary costMoney
	for i := range rows {
		summary.add(rows[i].costMoney)
	}
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	items := []costDimensionRow{}
	if start < len(rows) {
		if end > len(rows) {
			end = len(rows)
		}
		items = rows[start:end]
	}
	return costPageDTO{Items: items, Total: len(rows), Page: page, PageSize: pageSize, Summary: summary}
}

// GetCostOverview GET /api/cost/overview — Root only.
func GetCostOverview(c *gin.Context) {
	data, err := buildCostCube(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildCostOverview(data.cube, data.channels, data.rate, data.start, data.end))
}

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

// GetCostByUsers GET /api/cost/users — Root only.
func GetCostByUsers(c *gin.Context) { getCostByDimension(c, costDimUser) }

// GetCostByModels GET /api/cost/models — Root only.
func GetCostByModels(c *gin.Context) { getCostByDimension(c, costDimModel) }

// GetCostByChannels GET /api/cost/channels — Root only.
func GetCostByChannels(c *gin.Context) { getCostByDimension(c, costDimChannel) }
