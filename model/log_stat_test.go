package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 使用日志「消耗额度」统计应为净消费:sum(消费) − sum(退款)。
// 退款日志 Quota 存正数(见 service/task_billing.go),需从消费额度中扣减。
func TestSumUsedQuota_SubtractsRefund(t *testing.T) {
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	t.Cleanup(func() { LOG_DB.Exec("DELETE FROM logs") })

	require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeConsume, Quota: 1000, PromptTokens: 10, CompletionTokens: 5, CreatedAt: 1000, Username: "alice"}).Error)
	require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeConsume, Quota: 500, PromptTokens: 4, CompletionTokens: 2, CreatedAt: 1001, Username: "alice"}).Error)
	// 退款冲抵:正数 Quota,需被减去
	require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeRefund, Quota: 300, CreatedAt: 1002, Username: "alice"}).Error)
	// 噪声:充值/管理/系统等不应计入消耗额度
	require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeTopup, Quota: 99999, CreatedAt: 1003, Username: "alice"}).Error)
	require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeManage, Quota: 88888, CreatedAt: 1004, Username: "alice"}).Error)

	stat, err := SumUsedQuota(0, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 1000+500-300, stat.Quota, "净消费 = 消费 − 退款")
}

// 无退款时,净消费等于消费总额(回归:不改变原有行为)。
func TestSumUsedQuota_NoRefundUnchanged(t *testing.T) {
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	t.Cleanup(func() { LOG_DB.Exec("DELETE FROM logs") })

	require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeConsume, Quota: 700, CreatedAt: 2000, Username: "bob"}).Error)
	require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeConsume, Quota: 300, CreatedAt: 2001, Username: "bob"}).Error)

	stat, err := SumUsedQuota(0, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 1000, stat.Quota)
}
