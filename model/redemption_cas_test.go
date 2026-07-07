package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

// lockForUpdate must emit FOR UPDATE on databases that support row locking and
// skip it on SQLite, where the syntax does not exist.
//
// The dummy dialector is used because the SQLite driver strips locking clauses
// from the generated SQL, which would mask what the helper itself does.
func TestLockForUpdate_EmitsRowLock(t *testing.T) {
	original := common.UsingSQLite
	t.Cleanup(func() { common.UsingSQLite = original })

	dummyDB, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	buildSQL := func() string {
		var rows []Redemption
		return lockForUpdate(dummyDB).Where("id = ?", 1).Find(&rows).Statement.SQL.String()
	}

	common.UsingSQLite = false
	assert.Contains(t, buildSQL(), "FOR UPDATE")

	common.UsingSQLite = true
	assert.NotContains(t, buildSQL(), "FOR UPDATE")
}

// Redeem must credit a code exactly once even under concurrent redemption.
// On SQLite (no row lock) the compare-and-swap on status is what serializes
// the winners, so this guards the double-credit vulnerability directly.
func TestRedeem_ConcurrentSingleCredit(t *testing.T) {
	truncateTables(t)

	const key = "abcdef0123456789abcdef0123456789"
	const codeQuota = 500
	require.NoError(t, DB.Create(&Redemption{
		Id:     1,
		Key:    key,
		Status: common.RedemptionCodeStatusEnabled,
		Quota:  codeQuota,
	}).Error)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "redeemer", Quota: 0}).Error)

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	successes := make([]bool, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, 1); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, s := range successes {
		if s {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, codeQuota, user.Quota, "quota credited exactly once")

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, 1).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, 1, redemption.UsedUserId)
}
