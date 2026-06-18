package controller

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

// ptr is a tiny generic helper to take the address of a literal.
func ptr[T any](v T) *T { return &v }

func TestApplyCreateUserRequest_RejectsEmptyUsername(t *testing.T) {
	_, code, vErr, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "",
		Password: "longenoughpw",
	}, 10)

	require.False(t, ok)
	require.Equal(t, i18n.MsgInvalidParams, code)
	require.NoError(t, vErr, "validatorErr is reserved for the MsgUserInputInvalid path")
}

func TestApplyCreateUserRequest_TrimsUsernameWhitespace(t *testing.T) {
	// Username = "   " → trimmed to "" → MsgInvalidParams (matches the existing
	// controller's `strings.TrimSpace` + empty check).
	_, code, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "   ",
		Password: "longenoughpw",
	}, 10)

	require.False(t, ok)
	require.Equal(t, i18n.MsgInvalidParams, code)
}

func TestApplyCreateUserRequest_RejectsEmptyPassword(t *testing.T) {
	_, code, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "",
	}, 10)

	require.False(t, ok)
	require.Equal(t, i18n.MsgInvalidParams, code)
}

func TestApplyCreateUserRequest_RejectsRoleAtOrAboveCaller(t *testing.T) {
	for _, callerRole := range []int{10, 100} {
		_, code, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
			Username: "alice",
			Password: "longenoughpw",
			Role:     callerRole, // tries to create at caller's level
		}, callerRole)
		require.False(t, ok, "callerRole=%d", callerRole)
		require.Equal(t, i18n.MsgUserCannotCreateHigherLevel, code,
			"callerRole=%d", callerRole)
	}
}

func TestApplyCreateUserRequest_RejectsNegativeQuota(t *testing.T) {
	_, code, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "longenoughpw",
		Role:     1,
		Quota:    ptr(-1),
	}, 10)

	require.False(t, ok)
	require.Equal(t, i18n.MsgInvalidParams, code)
}

func TestApplyCreateUserRequest_HonorsGroupPointer(t *testing.T) {
	u, code, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "longenoughpw",
		Role:     1,
		Group:    ptr("vip"),
	}, 10)

	require.True(t, ok, "code=%q", code)
	require.Equal(t, "vip", u.Group)
}

func TestApplyCreateUserRequest_NilGroupKeepsZeroValue(t *testing.T) {
	// Nil group must NOT default to "vip" or anything else — leave it zero so the
	// GORM column default ('default') is what actually populates the row.
	u, _, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "longenoughpw",
		Role:     1,
		Group:    nil,
	}, 10)

	require.True(t, ok)
	require.Equal(t, "", u.Group,
		"a nil Group pointer must not be touched — GORM default 'default' applies at Insert time")
}

func TestApplyCreateUserRequest_HonorsQuotaPointerExplicitZero(t *testing.T) {
	// This is the Rule 6 contract:
	//   nil pointer → don't touch the field (GORM default 0 applies)
	//   *0 pointer  → explicit zero is HONORED (admin really wants 0 quota)
	// Test both paths so a future refactor can't quietly conflate them.

	u, _, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "longenoughpw",
		Role:     1,
		Quota:    ptr(0),
	}, 10)
	require.True(t, ok)
	require.Equal(t, 0, u.Quota, "explicit *int(0) must be honored, not dropped")

	u2, _, _, ok2 := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "longenoughpw",
		Role:     1,
		Quota:    ptr(500),
	}, 10)
	require.True(t, ok2)
	require.Equal(t, 500, u2.Quota)
}

func TestApplyCreateUserRequest_NilQuotaKeepsZeroValue(t *testing.T) {
	u, _, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "longenoughpw",
		Role:     1,
		Quota:    nil,
	}, 10)

	require.True(t, ok)
	require.Equal(t, 0, u.Quota)
}

func TestApplyCreateUserRequest_DisplayNameFallsBackToUsername(t *testing.T) {
	u, _, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "longenoughpw",
		Role:     1,
	}, 10)

	require.True(t, ok)
	require.Equal(t, "alice", u.DisplayName)
}

func TestApplyCreateUserRequest_DisplayNameKeptWhenProvided(t *testing.T) {
	u, _, _, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username:    "alice",
		DisplayName: "Alice Liddell",
		Password:    "longenoughpw",
		Role:        1,
	}, 10)

	require.True(t, ok)
	require.Equal(t, "Alice Liddell", u.DisplayName)
}

func TestApplyCreateUserRequest_PasswordTooShortFailsValidator(t *testing.T) {
	// model.User.Password has validate:"min=8,max=20"; a 5-char password slips past
	// the early empty-check and is caught by Validate.Struct, surfacing the same
	// MsgUserInputInvalid as today's CreateUser.
	_, code, vErr, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "alice",
		Password: "short", // 5 chars
		Role:     1,
	}, 10)

	require.False(t, ok)
	require.Equal(t, i18n.MsgUserInputInvalid, code)
	require.Error(t, vErr,
		"validatorErr MUST be returned on the MsgUserInputInvalid path so the controller "+
			"can pass {{.Error}} to the i18n template")
}

func TestApplyCreateUserRequest_UsernameTooLongFailsValidator(t *testing.T) {
	_, code, vErr, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username: "abcdefghijklmnopqrstuvwxyz", // 26 chars, validator says max=20
		Password: "longenoughpw",
		Role:     1,
	}, 10)

	require.False(t, ok)
	require.Equal(t, i18n.MsgUserInputInvalid, code)
	require.Error(t, vErr)
}

func TestEnsureUsernameNotTaken_FreeReturnsOk(t *testing.T) {
	exists := service.UsernameExistsFunc(func(string) (bool, error) { return false, nil })

	code, lookupErr, ok := ensureUsernameNotTaken("alice", exists)

	require.True(t, ok)
	require.Empty(t, code)
	require.NoError(t, lookupErr)
}

func TestEnsureUsernameNotTaken_TakenReturnsMsgUserExists(t *testing.T) {
	exists := service.UsernameExistsFunc(func(string) (bool, error) { return true, nil })

	code, lookupErr, ok := ensureUsernameNotTaken("alice", exists)

	require.False(t, ok)
	require.Equal(t, i18n.MsgUserExists, code,
		"a duplicate username MUST surface MsgUserExists, not a raw DB error")
	require.NoError(t, lookupErr,
		"lookupErr is reserved for the DB-failure path, not the taken path")
}

func TestEnsureUsernameNotTaken_LookupErrorPropagated(t *testing.T) {
	sentinel := errors.New("db died")
	exists := service.UsernameExistsFunc(func(string) (bool, error) { return false, sentinel })

	code, lookupErr, ok := ensureUsernameNotTaken("alice", exists)

	require.False(t, ok)
	require.Empty(t, code,
		"the friendlier MsgUserExists key is NOT applied to a DB-failure — the caller "+
			"should surface the underlying error verbatim instead")
	require.ErrorIs(t, lookupErr, sentinel)
}

func TestEnsureUsernameNotTaken_PassesExactUsernameToCallback(t *testing.T) {
	// Catches a future refactor that accidentally trims/lowercases the username
	// before the check — which would diverge from what Insert actually sees.
	var received string
	exists := service.UsernameExistsFunc(func(name string) (bool, error) {
		received = name
		return false, nil
	})

	_, _, ok := ensureUsernameNotTaken("Alice-AB12", exists)

	require.True(t, ok)
	require.Equal(t, "Alice-AB12", received)
}

func TestApplyCreateUserRequest_HappyPath(t *testing.T) {
	u, code, vErr, ok := applyCreateUserRequest(dto.CreateUserRequest{
		Username:    "alice",
		Password:    "longenoughpw",
		DisplayName: "",
		Role:        1,
		Group:       ptr("default"),
		Quota:       ptr(500),
	}, 10)

	require.True(t, ok, "code=%q vErr=%v", code, vErr)
	require.NoError(t, vErr)
	require.Empty(t, code)
	require.Equal(t, "alice", u.Username)
	require.Equal(t, "alice", u.DisplayName, "DisplayName falls back to Username")
	require.Equal(t, "longenoughpw", u.Password)
	require.Equal(t, 1, u.Role)
	require.Equal(t, "default", u.Group)
	require.Equal(t, 500, u.Quota)
}
