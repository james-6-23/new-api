package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearQuotaData(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
		CacheQuotaDataLock.Lock()
		CacheQuotaData = make(map[string]*QuotaData)
		CacheQuotaDataLock.Unlock()
	})
}

// 结算/退款镜像：只调金额，不动请求计数（count），负值可冲抵。
func TestLogQuotaDataAdjustment_NetsWithoutCount(t *testing.T) {
	clearQuotaData(t)
	ts := int64(7200) // 整点
	// 预扣：常规路径 count+1, quota+1000
	LogQuotaData(1, "u1", "doubao-seedance-1-0", 1000, ts, 50)
	// 结算补扣镜像 +200；退款镜像 -1200
	LogQuotaDataAdjustment(1, "u1", "doubao-seedance-1-0", 200, ts)
	LogQuotaDataAdjustment(1, "u1", "doubao-seedance-1-0", -1200, ts)
	SaveQuotaDataCache()

	var rows []*QuotaData
	require.NoError(t, DB.Table("quota_data").Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Count, "调整镜像不得增加请求计数")
	assert.Equal(t, 0, rows[0].Quota, "1000+200-1200 净额为 0")
	assert.Equal(t, 50, rows[0].TokenUsed)
}

// 无预扣缓存时（跨小时/重启后）调整镜像应独立成行，count 恒 0。
func TestLogQuotaDataAdjustment_StandaloneRow(t *testing.T) {
	clearQuotaData(t)
	LogQuotaDataAdjustment(2, "u2", "dreamina-seedance-2-0", -300, 3600)
	SaveQuotaDataCache()

	var rows []*QuotaData
	require.NoError(t, DB.Table("quota_data").Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 0, rows[0].Count)
	assert.Equal(t, -300, rows[0].Quota)
}

// UpdateUserUsedQuotaDelta：只调 used_quota，不动 request_count；负值可冲抵。
func TestUpdateUserUsedQuotaDelta(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM users") })
	u := &User{Username: "stat-u", Password: "12345678", Role: 1, Status: 1}
	require.NoError(t, DB.Create(u).Error)

	UpdateUserUsedQuotaAndRequestCount(u.Id, 1000) // 预扣：quota+1000, count+1
	UpdateUserUsedQuotaDelta(u.Id, -400)           // 退款冲抵

	var got User
	require.NoError(t, DB.First(&got, u.Id).Error)
	assert.Equal(t, 600, got.UsedQuota)
	assert.Equal(t, 1, got.RequestCount, "退款不得改变请求计数")
}
