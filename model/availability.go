/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package model

import (
	"sort"
	"time"
)

const (
	// MaxAvailabilityEntities 限制返回的实体数量，按 24h 请求数取前 N。
	MaxAvailabilityEntities = 12
	// MaxAvailabilityLines 限制每个实体下的子线数量。
	MaxAvailabilityLines = 6
	// availabilityWindowHours 是固定的统计窗口。
	availabilityWindowHours = 24
	// availabilityCurrentBuckets 是 current 聚合覆盖的最近桶数。
	//
	// 取 3 而非 1：刚开始的小时桶可能只有一两个请求，
	// 一次失败就会把总体状态翻成 incident。
	availabilityCurrentBuckets = 3
	// availabilityBucketSeconds 是小时桶的长度，与 perf_metrics 的默认分桶一致。
	availabilityBucketSeconds = 3600
)

// availabilityMetricKeys 是响应中 metrics map 的键，顺序即前端展示顺序。
var availabilityMetricKeys = []string{"successRate", "ttft", "tps", "latency"}

// availabilityLowerIsBetter 标记哪些指标的 best 包络线应取 min。
var availabilityLowerIsBetter = map[string]bool{"ttft": true, "latency": true}

// AvailabilityRow 是聚合查询的一行：某模型 × 某分组 × 某小时桶。
type AvailabilityRow struct {
	ModelName      string
	GroupName      string
	BucketTs       int64
	RequestCount   int64
	SuccessCount   int64
	TotalLatencyMs int64
	TtftSumMs      int64
	TtftCount      int64
	OutputTokens   int64
	GenerationMs   int64
}

// AvailabilityCurrent 是实体的当前状态，取最近若干个桶的合计。
// 指针类型用于表达「无数据」——nil 序列化为 JSON null。
type AvailabilityCurrent struct {
	SuccessRatePct *float64 `json:"successRatePct"`
	TtftMs         *float64 `json:"ttftMs"`
	Tps            *float64 `json:"tps"`
	LatencyMs      *float64 `json:"latencyMs"`
}

// AvailabilityLine 是图表中的一条子线。
type AvailabilityLine struct {
	Id     string     `json:"id"`
	Name   string     `json:"name"`
	Points []*float64 `json:"points"`
}

// AvailabilityMetric 是单个指标的图表数据：best 包络线 + 各子线。
type AvailabilityMetric struct {
	Best  []*float64         `json:"best"`
	Lines []AvailabilityLine `json:"lines"`
}

// AvailabilityEntity 是一个分组或一个模型。
type AvailabilityEntity struct {
	Id       string                        `json:"id"`
	Name     string                        `json:"name"`
	Requests int64                         `json:"requests"`
	Hours    []string                      `json:"hours"`
	Current  AvailabilityCurrent           `json:"current"`
	Metrics  map[string]AvailabilityMetric `json:"metrics"`
}

// AvailabilityResponse 是 /api/status/availability 的响应载荷。
type AvailabilityResponse struct {
	GeneratedAt     int64                `json:"generatedAt"`
	Dimension       string               `json:"dimension"`
	Truncated       bool                 `json:"truncated"`
	MetricsDisabled bool                 `json:"metricsDisabled"`
	Entities        []AvailabilityEntity `json:"entities"`
}

// QueryAvailabilityRows 读取 startTs 之后的全部聚合行。
//
// 只用 GORM 构造器与 SUM/GROUP BY，三库通用；group 是保留字，
// 经 commonGroupCol 引用并改名为 group_name 以避开扫描期的再次转义。
func QueryAvailabilityRows(startTs int64) ([]AvailabilityRow, error) {
	var rows []AvailabilityRow
	err := DB.Model(&PerfMetric{}).
		Select("model_name, "+commonGroupCol+" as group_name, bucket_ts, "+
			"SUM(request_count) as request_count, "+
			"SUM(success_count) as success_count, "+
			"SUM(total_latency_ms) as total_latency_ms, "+
			"SUM(ttft_sum_ms) as ttft_sum_ms, "+
			"SUM(ttft_count) as ttft_count, "+
			"SUM(output_tokens) as output_tokens, "+
			"SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ?", startTs).
		Group("model_name, " + commonGroupCol + ", bucket_ts").
		Find(&rows).Error
	return rows, err
}

// AvailabilityStartTime 返回 24 小时窗口的起点（对齐到整点）。
func AvailabilityStartTime(nowTs int64) int64 {
	return alignAvailabilityBucket(nowTs) - (availabilityWindowHours-1)*availabilityBucketSeconds
}

func alignAvailabilityBucket(ts int64) int64 {
	return ts - modFloor(ts, availabilityBucketSeconds)
}

// modFloor 是向下取整的取模，保证负时间戳也向更早的整点对齐。
func modFloor(ts, m int64) int64 {
	r := ts % m
	if r < 0 {
		r += m
	}
	return r
}

// availabilityCell 是单个 (实体, 子线, 桶) 的计数器合计。
type availabilityCell struct {
	requestCount   int64
	successCount   int64
	totalLatencyMs int64
	ttftSumMs      int64
	ttftCount      int64
	outputTokens   int64
	generationMs   int64
}

func (c *availabilityCell) addRow(r AvailabilityRow) {
	c.requestCount += r.RequestCount
	c.successCount += r.SuccessCount
	c.totalLatencyMs += r.TotalLatencyMs
	c.ttftSumMs += r.TtftSumMs
	c.ttftCount += r.TtftCount
	c.outputTokens += r.OutputTokens
	c.generationMs += r.GenerationMs
}

func (c *availabilityCell) addCell(o *availabilityCell) {
	c.requestCount += o.requestCount
	c.successCount += o.successCount
	c.totalLatencyMs += o.totalLatencyMs
	c.ttftSumMs += o.ttftSumMs
	c.ttftCount += o.ttftCount
	c.outputTokens += o.outputTokens
	c.generationMs += o.generationMs
}

// metric 按指标名计算派生值。分母为 0 时返回 nil 而非 0——
// 图表应断线，而不是塌到零造成「性能骤降」的误读。
func (c availabilityCell) metric(key string) *float64 {
	switch key {
	case "successRate":
		if c.requestCount <= 0 {
			return nil
		}
		return availabilityPtr(float64(c.successCount) / float64(c.requestCount) * 100)
	case "ttft":
		if c.ttftCount <= 0 {
			return nil
		}
		return availabilityPtr(float64(c.ttftSumMs) / float64(c.ttftCount))
	case "tps":
		if c.outputTokens <= 0 || c.generationMs <= 0 {
			return nil
		}
		return availabilityPtr(float64(c.outputTokens) / (float64(c.generationMs) / 1000))
	case "latency":
		if c.requestCount <= 0 {
			return nil
		}
		return availabilityPtr(float64(c.totalLatencyMs) / float64(c.requestCount))
	}
	return nil
}

func availabilityPtr(v float64) *float64 {
	return &v
}

type availabilityLineAgg struct {
	cells map[int]*availabilityCell
	total int64
}

type availabilityEntityAgg struct {
	lines map[string]*availabilityLineAgg
	total int64
}

// BuildAvailability 把聚合行透视成前端结构。纯函数，不触碰数据库。
//
// dimension = "group"：实体为分组，子线为模型；
// dimension = "model"：实体为模型，子线为分组。
func BuildAvailability(rows []AvailabilityRow, dimension string, nowTs int64) AvailabilityResponse {
	resp := AvailabilityResponse{
		GeneratedAt: nowTs,
		Dimension:   dimension,
		Entities:    []AvailabilityEntity{},
	}

	// 时间轴：固定 24 个小时桶，右端对齐到当前小时。
	endBucket := alignAvailabilityBucket(nowTs)
	startBucket := endBucket - (availabilityWindowHours-1)*availabilityBucketSeconds
	buckets := make([]int64, 0, availabilityWindowHours)
	bucketIndex := make(map[int64]int, availabilityWindowHours)
	for ts := startBucket; ts <= endBucket; ts += availabilityBucketSeconds {
		bucketIndex[ts] = len(buckets)
		buckets = append(buckets, ts)
	}

	hours := make([]string, len(buckets))
	for i, ts := range buckets {
		hours[i] = time.Unix(ts, 0).Format("15:04")
	}

	entities := map[string]*availabilityEntityAgg{}

	for _, r := range rows {
		idx, ok := bucketIndex[alignAvailabilityBucket(r.BucketTs)]
		if !ok {
			continue // 窗口之外
		}

		entityId, lineId := r.GroupName, r.ModelName
		if dimension == "model" {
			entityId, lineId = r.ModelName, r.GroupName
		}
		if entityId == "" || lineId == "" {
			continue
		}

		e, ok := entities[entityId]
		if !ok {
			e = &availabilityEntityAgg{lines: map[string]*availabilityLineAgg{}}
			entities[entityId] = e
		}
		e.total += r.RequestCount

		l, ok := e.lines[lineId]
		if !ok {
			l = &availabilityLineAgg{cells: map[int]*availabilityCell{}}
			e.lines[lineId] = l
		}
		l.total += r.RequestCount

		c, ok := l.cells[idx]
		if !ok {
			c = &availabilityCell{}
			l.cells[idx] = c
		}
		c.addRow(r)
	}

	// 实体按 24h 请求数降序，截断到上限
	entityIds := make([]string, 0, len(entities))
	for id := range entities {
		entityIds = append(entityIds, id)
	}
	sort.Slice(entityIds, func(i, j int) bool {
		a, b := entities[entityIds[i]], entities[entityIds[j]]
		if a.total != b.total {
			return a.total > b.total
		}
		return entityIds[i] < entityIds[j] // 请求数相同时按名称稳定排序
	})
	if len(entityIds) > MaxAvailabilityEntities {
		entityIds = entityIds[:MaxAvailabilityEntities]
		resp.Truncated = true
	}

	currentStartIdx := len(buckets) - availabilityCurrentBuckets
	if currentStartIdx < 0 {
		currentStartIdx = 0
	}

	for _, entityId := range entityIds {
		e := entities[entityId]

		lineIds := make([]string, 0, len(e.lines))
		for id := range e.lines {
			lineIds = append(lineIds, id)
		}
		sort.Slice(lineIds, func(i, j int) bool {
			a, b := e.lines[lineIds[i]], e.lines[lineIds[j]]
			if a.total != b.total {
				return a.total > b.total
			}
			return lineIds[i] < lineIds[j]
		})
		if len(lineIds) > MaxAvailabilityLines {
			lineIds = lineIds[:MaxAvailabilityLines]
			resp.Truncated = true
		}

		// current：最近 N 桶跨全部子线合计（含被截断的子线，
		// 否则总体状态会漏掉尾部流量的失败）
		currentCell := availabilityCell{}
		for _, l := range e.lines {
			for idx, c := range l.cells {
				if idx >= currentStartIdx {
					currentCell.addCell(c)
				}
			}
		}

		metrics := make(map[string]AvailabilityMetric, len(availabilityMetricKeys))
		for _, key := range availabilityMetricKeys {
			lines := make([]AvailabilityLine, 0, len(lineIds))
			for _, lineId := range lineIds {
				points := make([]*float64, len(buckets))
				for idx, c := range e.lines[lineId].cells {
					points[idx] = c.metric(key)
				}
				lines = append(lines, AvailabilityLine{
					Id:     lineId,
					Name:   lineId,
					Points: points,
				})
			}

			// best 包络线：越低越好的取 min，其余取 max。
			// 全部子线该桶均无数据时保持 nil，不退化成 0。
			best := make([]*float64, len(buckets))
			for i := range buckets {
				for _, line := range lines {
					v := line.Points[i]
					if v == nil {
						continue
					}
					if best[i] == nil {
						best[i] = availabilityPtr(*v)
						continue
					}
					if availabilityLowerIsBetter[key] {
						if *v < *best[i] {
							best[i] = availabilityPtr(*v)
						}
					} else if *v > *best[i] {
						best[i] = availabilityPtr(*v)
					}
				}
			}

			metrics[key] = AvailabilityMetric{Best: best, Lines: lines}
		}

		resp.Entities = append(resp.Entities, AvailabilityEntity{
			Id:       entityId,
			Name:     entityId,
			Requests: e.total,
			Hours:    hours,
			Current: AvailabilityCurrent{
				SuccessRatePct: currentCell.metric("successRate"),
				TtftMs:         currentCell.metric("ttft"),
				Tps:            currentCell.metric("tps"),
				LatencyMs:      currentCell.metric("latency"),
			},
			Metrics: metrics,
		})
	}

	return resp
}
