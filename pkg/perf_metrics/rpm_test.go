package perfmetrics

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrentRpmCountsWithinWindow(t *testing.T) {
	r := newRpmCounter()

	// 同一秒内三次
	r.incrAt(1000)
	r.incrAt(1000)
	r.incrAt(1000)
	// 相邻秒一次
	r.incrAt(1001)

	require.Equal(t, int64(4), r.currentAt(1001))
}

func TestCurrentRpmEvictsOlderThan60s(t *testing.T) {
	r := newRpmCounter()

	r.incrAt(1000) // 将在 t=1060 时正好滑出窗口
	r.incrAt(1030)

	// t=1059：1000 与 1030 都还在最近 60 秒内
	require.Equal(t, int64(2), r.currentAt(1059))
	// t=1060：1000 已滑出，仅剩 1030
	require.Equal(t, int64(1), r.currentAt(1060))
	// t=1200：全部滑出
	require.Equal(t, int64(0), r.currentAt(1200))
}

func TestRpmIncrAtReusesSlotAfterFullLap(t *testing.T) {
	r := newRpmCounter()

	r.incrAt(1000)
	// 1060 与 1000 落在同一个槽（对 60 取模相同），必须覆盖而非累加
	r.incrAt(1060)

	require.Equal(t, int64(1), r.currentAt(1060))
}

func TestRpmCounterIsConcurrencySafe(t *testing.T) {
	r := newRpmCounter()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.incrAt(2000)
		}()
	}
	wg.Wait()

	require.Equal(t, int64(100), r.currentAt(2000))
}

func TestRpmCounterIgnoresFutureSlots(t *testing.T) {
	r := newRpmCounter()

	// 时钟回拨等异常导致的「未来」样本不应计入当前窗口
	r.incrAt(3000)

	require.Equal(t, int64(0), r.currentAt(2900))
}
