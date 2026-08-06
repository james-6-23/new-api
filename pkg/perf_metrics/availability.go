package perfmetrics

import (
	"github.com/QuantumNous/new-api/model"
)

// HotAvailabilityRows 导出尚未落库的热桶，供可用性监控合并。
//
// 当前小时的桶永远不会被 flushCompletedBuckets 写入（它只处理已完结的桶），
// 只查库会让「当前」状态完全看不到进行中的这一小时。
// 已落库的桶在 flush 时被 drain 归零，仍留在 map 里，累加 0 不会重复计数。
func HotAvailabilityRows(startTs int64) []model.AvailabilityRow {
	var rows []model.AvailabilityRow
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.bucketTs < startTs {
			return true
		}
		snap := value.(*atomicBucket).snapshot()
		if snap.requestCount == 0 {
			return true
		}
		rows = append(rows, model.AvailabilityRow{
			ModelName:      k.model,
			GroupName:      k.group,
			BucketTs:       k.bucketTs,
			RequestCount:   snap.requestCount,
			SuccessCount:   snap.successCount,
			TotalLatencyMs: snap.totalLatencyMs,
			TtftSumMs:      snap.ttftSumMs,
			TtftCount:      snap.ttftCount,
			OutputTokens:   snap.outputTokens,
			GenerationMs:   snap.generationMs,
		})
		return true
	})
	return rows
}
