package perfmetrics

import (
	"sync"
	"time"
)

// rpmWindowSeconds 是实时 RPM 的统计窗口，也是环形缓冲的槽位数。
const rpmWindowSeconds = 60

// rpmCounter 是一个 60 槽的环形缓冲，每槽代表一秒。
//
// 每个槽同时保存它所属的秒级时间戳；写入时若槽上的时间戳与当前秒不符，
// 说明该槽是上一圈留下的陈旧数据，先清零再累加。淘汰因此是写时惰性完成的，
// 不需要后台清理协程。
type rpmCounter struct {
	mu    sync.Mutex
	slots [rpmWindowSeconds]rpmSlot
}

type rpmSlot struct {
	ts    int64
	count int64
}

func newRpmCounter() *rpmCounter {
	return &rpmCounter{}
}

func (r *rpmCounter) incrAt(ts int64) {
	// 双重取模保证负时间戳也落在合法下标上。
	idx := int(((ts % rpmWindowSeconds) + rpmWindowSeconds) % rpmWindowSeconds)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.slots[idx].ts != ts {
		r.slots[idx].ts = ts
		r.slots[idx].count = 0
	}
	r.slots[idx].count++
}

// currentAt 返回 (ts-60, ts] 窗口内的请求总数。
//
// 上界判断不可省略：时钟回拨会让环上残留「未来」的槽，
// 若只判下界，这些样本会被错误地计入当前窗口。
func (r *rpmCounter) currentAt(ts int64) int64 {
	cutoff := ts - rpmWindowSeconds

	r.mu.Lock()
	defer r.mu.Unlock()

	var total int64
	for _, slot := range r.slots {
		if slot.ts > cutoff && slot.ts <= ts {
			total += slot.count
		}
	}
	return total
}

var globalRpm = newRpmCounter()

// IncrRpm 记录一次中转请求。
func IncrRpm() {
	globalRpm.incrAt(time.Now().Unix())
}

// CurrentRpm 返回当前节点最近 60 秒的请求数。
//
// 多副本部署下这是「当前节点 RPM」，不做跨节点求和——
// 跨节点求和需要共享存储，且各节点采样时刻不一致时结果并不可加。
func CurrentRpm() int64 {
	return globalRpm.currentAt(time.Now().Unix())
}
