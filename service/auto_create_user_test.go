package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// fakeRandomFn returns a deterministic, inspectable random replacement.
// The N-th call returns "fake-<charset>-<N>".
type fakeRandomFn struct {
	Calls []struct {
		N       int
		Charset string
	}
}

func (f *fakeRandomFn) Fn() func(int, string) string {
	return func(n int, charset string) string {
		f.Calls = append(f.Calls, struct {
			N       int
			Charset string
		}{N: n, Charset: charset})
		return "fake-" + charset + "-" + itoa(len(f.Calls))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// existsScript is a fake UsernameExistsFunc that returns the values in `script`
// in order. If the script runs out, it returns (false, nil) — i.e. "not taken".
type existsScript struct {
	calls   []string
	results []struct {
		taken bool
		err   error
	}
	cursor int
}

func (e *existsScript) Fn() UsernameExistsFunc {
	return func(username string) (bool, error) {
		e.calls = append(e.calls, username)
		if e.cursor >= len(e.results) {
			return false, nil
		}
		r := e.results[e.cursor]
		e.cursor++
		return r.taken, r.err
	}
}

func TestPreviewAutoCreateUser_GeneratesFromSettings(t *testing.T) {
	defer operation_setting.ResetAutoCreateUserSettingForTest()
	operation_setting.SetAutoCreateUserSettingForTest(operation_setting.AutoCreateUserSetting{
		UsernamePrefix:        "User-",
		UsernameSuffixLength:  4,
		UsernameSuffixCharset: operation_setting.AutoCreateUserCharsetAlphanumeric,
		PasswordMode:          operation_setting.AutoCreateUserPasswordSameAsUsername,
		DefaultQuota:          500,
		DefaultGroup:          "vip",
	})
	random := &fakeRandomFn{}
	exists := &existsScript{}

	resp, err := PreviewAutoCreateUser(
		operation_setting.GetAutoCreateUserSetting(),
		random.Fn(),
		exists.Fn(),
	)

	require.NoError(t, err)
	require.Equal(t, "User-fake-alphanumeric-1", resp.Username,
		"username = prefix + randomFn(suffix_length, suffix_charset)")
	require.Equal(t, "User-fake-alphanumeric-1", resp.Password,
		"same_as_username mode echoes the username")
	require.Equal(t, "vip", resp.Group)
	require.Equal(t, 500, resp.Quota)
	require.Len(t, exists.calls, 1, "no collision → exactly one existence check")
}

func TestPreviewAutoCreateUser_RetriesOnCollision(t *testing.T) {
	defer operation_setting.ResetAutoCreateUserSettingForTest()
	operation_setting.SetAutoCreateUserSettingForTest(operation_setting.AutoCreateUserSetting{
		UsernamePrefix:        "u",
		UsernameSuffixLength:  3,
		UsernameSuffixCharset: operation_setting.AutoCreateUserCharsetDigits,
		PasswordMode:          operation_setting.AutoCreateUserPasswordSameAsUsername,
		DefaultGroup:          "default",
	})
	random := &fakeRandomFn{}
	exists := &existsScript{
		results: []struct {
			taken bool
			err   error
		}{{taken: true}, {taken: true}, {taken: false}},
	}

	resp, err := PreviewAutoCreateUser(
		operation_setting.GetAutoCreateUserSetting(),
		random.Fn(),
		exists.Fn(),
	)

	require.NoError(t, err)
	require.Equal(t, []string{"ufake-digits-1", "ufake-digits-2", "ufake-digits-3"}, exists.calls,
		"each collision should produce a fresh random suffix and a fresh existence check")
	require.Equal(t, "ufake-digits-3", resp.Username,
		"the surviving candidate (3rd) is returned")
}

func TestPreviewAutoCreateUser_ErrorsAfterFiveCollisions(t *testing.T) {
	defer operation_setting.ResetAutoCreateUserSettingForTest()
	operation_setting.ResetAutoCreateUserSettingForTest()
	random := &fakeRandomFn{}
	exists := &existsScript{
		results: []struct {
			taken bool
			err   error
		}{
			{taken: true}, {taken: true}, {taken: true}, {taken: true}, {taken: true},
		},
	}

	_, err := PreviewAutoCreateUser(
		operation_setting.GetAutoCreateUserSetting(),
		random.Fn(),
		exists.Fn(),
	)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUsernameCollisionExhausted),
		"5 consecutive collisions must surface ErrUsernameCollisionExhausted (got: %v)", err)
	require.Len(t, exists.calls, 5, "exactly 5 attempts before giving up")
}

func TestPreviewAutoCreateUser_PropagatesExistsLookupError(t *testing.T) {
	defer operation_setting.ResetAutoCreateUserSettingForTest()
	operation_setting.ResetAutoCreateUserSettingForTest()
	random := &fakeRandomFn{}
	sentinel := errors.New("db is on fire")
	exists := &existsScript{
		results: []struct {
			taken bool
			err   error
		}{{err: sentinel}},
	}

	_, err := PreviewAutoCreateUser(
		operation_setting.GetAutoCreateUserSetting(),
		random.Fn(),
		exists.Fn(),
	)

	require.ErrorIs(t, err, sentinel,
		"a real DB lookup error must NOT be confused with the collision-exhausted path")
}

func TestPreviewAutoCreateUser_DefaultQuotaZeroFallsBackToCommonQuotaForNewUser(t *testing.T) {
	// Save the global so we can restore it afterward — pattern straight out of
	// common/url_validator_test.go.
	orig := common.QuotaForNewUser
	t.Cleanup(func() { common.QuotaForNewUser = orig })
	common.QuotaForNewUser = 12345

	defer operation_setting.ResetAutoCreateUserSettingForTest()
	operation_setting.SetAutoCreateUserSettingForTest(operation_setting.AutoCreateUserSetting{
		UsernamePrefix:       "X",
		UsernameSuffixLength: 1,
		PasswordMode:         operation_setting.AutoCreateUserPasswordSameAsUsername,
		DefaultQuota:         0,
		DefaultGroup:         "default",
	})

	resp, err := PreviewAutoCreateUser(
		operation_setting.GetAutoCreateUserSetting(),
		(&fakeRandomFn{}).Fn(),
		(&existsScript{}).Fn(),
	)
	require.NoError(t, err)
	require.Equal(t, 12345, resp.Quota,
		"DefaultQuota==0 must fall back to common.QuotaForNewUser at preview time")
}

func TestPreviewAutoCreateUser_ExplicitDefaultQuotaSurvives(t *testing.T) {
	orig := common.QuotaForNewUser
	t.Cleanup(func() { common.QuotaForNewUser = orig })
	common.QuotaForNewUser = 99999

	defer operation_setting.ResetAutoCreateUserSettingForTest()
	operation_setting.SetAutoCreateUserSettingForTest(operation_setting.AutoCreateUserSetting{
		UsernamePrefix:       "X",
		UsernameSuffixLength: 1,
		PasswordMode:         operation_setting.AutoCreateUserPasswordSameAsUsername,
		DefaultQuota:         7,
		DefaultGroup:         "default",
	})

	resp, err := PreviewAutoCreateUser(
		operation_setting.GetAutoCreateUserSetting(),
		(&fakeRandomFn{}).Fn(),
		(&existsScript{}).Fn(),
	)
	require.NoError(t, err)
	require.Equal(t, 7, resp.Quota,
		"a non-zero DefaultQuota must NOT be overridden by common.QuotaForNewUser")
}

func TestPreviewAutoCreateUser_EmptyDefaultGroupFallsBackToDefault(t *testing.T) {
	defer operation_setting.ResetAutoCreateUserSettingForTest()
	operation_setting.SetAutoCreateUserSettingForTest(operation_setting.AutoCreateUserSetting{
		UsernamePrefix:       "X",
		UsernameSuffixLength: 1,
		PasswordMode:         operation_setting.AutoCreateUserPasswordSameAsUsername,
		DefaultGroup:         "",
	})
	resp, err := PreviewAutoCreateUser(
		operation_setting.GetAutoCreateUserSetting(),
		(&fakeRandomFn{}).Fn(),
		(&existsScript{}).Fn(),
	)
	require.NoError(t, err)
	require.Equal(t, "default", resp.Group)
}

func TestRenderCopyItem_AllPlaceholders(t *testing.T) {
	got := RenderCopyItem(
		"站点：{{site}}\n用户名：{{username}}\n密码：{{password}}",
		"User-AB12",
		"hunter2",
		"https://acme.example.com",
	)
	require.Equal(t,
		"站点：https://acme.example.com\n用户名：User-AB12\n密码：hunter2",
		got,
	)
}

func TestRenderCopyItem_PartialPlaceholders(t *testing.T) {
	got := RenderCopyItem(
		"hi {{username}}",
		"alice",
		"unused",
		"unused",
	)
	require.Equal(t, "hi alice", got)
}

func TestRenderCopyItem_UnknownPlaceholderPreserved(t *testing.T) {
	got := RenderCopyItem(
		"site={{site}} mystery={{email}}",
		"u",
		"p",
		"S",
	)
	require.Equal(t,
		"site=S mystery={{email}}",
		got,
		"unknown placeholders pass through literally — they are NOT substituted with empty string",
	)
}

func TestRenderCopyItem_RepeatedPlaceholders(t *testing.T) {
	got := RenderCopyItem(
		"{{username}} and {{username}} again",
		"alice",
		"p",
		"S",
	)
	require.Equal(t, "alice and alice again", got,
		"all occurrences of a placeholder are substituted, not just the first")
}

func TestRenderCopyItem_EmptyTemplate(t *testing.T) {
	require.Equal(t, "", RenderCopyItem("", "u", "p", "s"))
}
