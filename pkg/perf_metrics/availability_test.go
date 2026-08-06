package perfmetrics

import (
	"testing"
	"time"
)

func TestHotAvailabilityRowsFiltersAndMaps(t *testing.T) {
	hotBuckets.Range(func(k, _ any) bool {
		hotBuckets.Delete(k)
		return true
	})

	now := time.Now().Unix()
	current := bucketStart(now)
	stale := current - 48*3600

	inWindow := bucketKey{model: "gpt-4o", group: "default", bucketTs: current}
	hotBuckets.Store(inWindow, &atomicBucket{})
	b, _ := hotBuckets.Load(inWindow)
	b.(*atomicBucket).add(Sample{
		Model: "gpt-4o", Group: "default", LatencyMs: 200,
		TtftMs: 50, HasTtft: true, Success: true,
		OutputTokens: 100, GenerationMs: 1000,
	})

	// 窗口外的桶必须被过滤掉。
	hotBuckets.Store(bucketKey{model: "old", group: "default", bucketTs: stale}, &atomicBucket{})
	old, _ := hotBuckets.Load(bucketKey{model: "old", group: "default", bucketTs: stale})
	old.(*atomicBucket).add(Sample{Model: "old", Group: "default", Success: true})

	// 已落库后被 drain 归零的桶不应重复计入。
	drained := bucketKey{model: "drained", group: "default", bucketTs: current}
	hotBuckets.Store(drained, &atomicBucket{})
	d, _ := hotBuckets.Load(drained)
	d.(*atomicBucket).add(Sample{Model: "drained", Group: "default", Success: true})
	d.(*atomicBucket).drain()

	rows := HotAvailabilityRows(stale + 3600)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(rows), rows)
	}

	r := rows[0]
	if r.ModelName != "gpt-4o" || r.GroupName != "default" || r.BucketTs != current {
		t.Fatalf("unexpected identity: %+v", r)
	}
	if r.RequestCount != 1 || r.SuccessCount != 1 || r.TotalLatencyMs != 200 {
		t.Fatalf("unexpected counters: %+v", r)
	}
	if r.TtftSumMs != 50 || r.TtftCount != 1 {
		t.Fatalf("unexpected ttft: %+v", r)
	}
	if r.OutputTokens != 100 || r.GenerationMs != 1000 {
		t.Fatalf("unexpected throughput: %+v", r)
	}

	hotBuckets.Range(func(k, _ any) bool {
		hotBuckets.Delete(k)
		return true
	})
}
