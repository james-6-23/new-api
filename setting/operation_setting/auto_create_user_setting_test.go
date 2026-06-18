package operation_setting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeFakeRandomFn returns a randomFn whose output is deterministic and trivially
// inspectable. Each call returns "<charset>:<length>:<callIndex>".
//
// The caller can also pass a non-nil capture slice to record the (length, charset)
// arguments in invocation order.
func makeFakeRandomFn(capture *[]struct {
	N       int
	Charset string
}) func(int, string) string {
	calls := 0
	return func(n int, charset string) string {
		calls++
		if capture != nil {
			*capture = append(*capture, struct {
				N       int
				Charset string
			}{N: n, Charset: charset})
		}
		// Return a value that encodes the call so assertions can pinpoint exactly
		// what randomFn was asked to do.
		return charset + ":" + itoa(n) + ":" + itoa(calls)
	}
}

func itoa(i int) string {
	// Tiny local strconv-free helper so this test file has no fmt/strconv noise
	// (matching the lean style of sibling tests).
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func TestBuildUsername_PrefixAndLengthAndCharset(t *testing.T) {
	s := AutoCreateUserSetting{
		UsernamePrefix:        "User-",
		UsernameSuffixLength:  4,
		UsernameSuffixCharset: AutoCreateUserCharsetDigits,
	}
	var calls []struct {
		N       int
		Charset string
	}
	got := s.BuildUsername(makeFakeRandomFn(&calls))

	require.Equal(t, "User-digits:4:1", got)
	require.Equal(t, []struct {
		N       int
		Charset string
	}{{N: 4, Charset: "digits"}}, calls)
}

func TestBuildUsername_SuffixLengthZeroPromotesToOne(t *testing.T) {
	s := AutoCreateUserSetting{
		UsernamePrefix:        "X",
		UsernameSuffixLength:  0,
		UsernameSuffixCharset: AutoCreateUserCharsetAlphanumeric,
	}
	var calls []struct {
		N       int
		Charset string
	}
	got := s.BuildUsername(makeFakeRandomFn(&calls))

	require.Equal(t, "Xalphanumeric:1:1", got)
	require.Len(t, calls, 1)
	require.Equal(t, 1, calls[0].N,
		"a SuffixLength of 0 must be promoted to 1, not passed through to randomFn")
}

func TestBuildUsername_NegativeSuffixLengthPromotesToOne(t *testing.T) {
	s := AutoCreateUserSetting{
		UsernamePrefix:        "X",
		UsernameSuffixLength:  -7,
		UsernameSuffixCharset: AutoCreateUserCharsetLetters,
	}
	var calls []struct {
		N       int
		Charset string
	}
	got := s.BuildUsername(makeFakeRandomFn(&calls))

	require.Equal(t, "Xletters:1:1", got)
	require.Equal(t, 1, calls[0].N)
}

func TestBuildPassword_SameAsUsername_IgnoresRandomFn(t *testing.T) {
	s := AutoCreateUserSetting{
		PasswordMode:         AutoCreateUserPasswordSameAsUsername,
		RandomPasswordLength: 12,
	}
	var calls []struct {
		N       int
		Charset string
	}
	got := s.BuildPassword("User-AB12", makeFakeRandomFn(&calls))

	require.Equal(t, "User-AB12", got)
	require.Empty(t, calls,
		"SameAsUsername must not call randomFn — it produces the password verbatim from the username")
}

func TestBuildPassword_Random_UsesAlphanumericRegardlessOfSuffixCharset(t *testing.T) {
	// Suffix charset is digits, but Random password must still be alphanumeric.
	s := AutoCreateUserSetting{
		PasswordMode:          AutoCreateUserPasswordRandom,
		RandomPasswordLength:  16,
		UsernameSuffixCharset: AutoCreateUserCharsetDigits,
	}
	var calls []struct {
		N       int
		Charset string
	}
	got := s.BuildPassword("anything", makeFakeRandomFn(&calls))

	require.Equal(t, "alphanumeric:16:1", got)
	require.Equal(t, []struct {
		N       int
		Charset string
	}{{N: 16, Charset: "alphanumeric"}}, calls)
}

func TestBuildPassword_Random_LengthBelowOnePromotesToEight(t *testing.T) {
	// Defensive: prevent a misconfigured RandomPasswordLength=0 from producing an
	// empty password (which would later fail user.Insert's bcrypt step and surface
	// a confusing validator error). 8 is the project's existing min password length.
	s := AutoCreateUserSetting{
		PasswordMode:         AutoCreateUserPasswordRandom,
		RandomPasswordLength: 0,
	}
	var calls []struct {
		N       int
		Charset string
	}
	got := s.BuildPassword("user", makeFakeRandomFn(&calls))

	require.Equal(t, "alphanumeric:8:1", got)
	require.Equal(t, 8, calls[0].N)
}

func TestDefaultAutoCreateUserSetting_HasExpectedDefaults(t *testing.T) {
	defer ResetAutoCreateUserSettingForTest()
	ResetAutoCreateUserSettingForTest()

	s := GetAutoCreateUserSetting()

	require.Equal(t, "User-", s.UsernamePrefix)
	require.Equal(t, 4, s.UsernameSuffixLength)
	require.Equal(t, AutoCreateUserCharsetAlphanumeric, s.UsernameSuffixCharset)
	require.Equal(t, AutoCreateUserPasswordSameAsUsername, s.PasswordMode)
	require.Equal(t, 12, s.RandomPasswordLength)
	require.Equal(t, 0, s.DefaultQuota, "DefaultQuota=0 means 'fall back to QuotaForNewUser at preview time'")
	require.Equal(t, "default", s.DefaultGroup)
	require.Equal(t, "", s.SiteURL)
	require.Equal(t, []AutoCreateUserCopyItem{
		{Label: "站点", Template: "{{site}}"},
		{Label: "用户名", Template: "{{username}}"},
		{Label: "密码", Template: "{{password}}"},
	}, s.CopyTemplates)
}

func TestSetAutoCreateUserSettingForTest_AndReset(t *testing.T) {
	defer ResetAutoCreateUserSettingForTest()

	custom := AutoCreateUserSetting{
		UsernamePrefix:        "Acme-",
		UsernameSuffixLength:  6,
		UsernameSuffixCharset: AutoCreateUserCharsetLetters,
		PasswordMode:          AutoCreateUserPasswordRandom,
		RandomPasswordLength:  20,
		DefaultQuota:          1000,
		DefaultGroup:          "vip",
		SiteURL:               "https://acme.example.com",
		CopyTemplates: []AutoCreateUserCopyItem{
			{Label: "Welcome", Template: "Hi {{username}}!"},
		},
	}
	SetAutoCreateUserSettingForTest(custom)

	got := GetAutoCreateUserSetting()
	require.Equal(t, custom, got, "setter must overwrite the package-level setting verbatim")

	ResetAutoCreateUserSettingForTest()
	back := GetAutoCreateUserSetting()
	require.Equal(t, "User-", back.UsernamePrefix, "Reset must restore the original defaults")
}

func TestBuildUsername_HonorsSetForTestSetting(t *testing.T) {
	// Acceptance-level sanity: when an outer test (e.g. controller) configures the
	// setting via SetAutoCreateUserSettingForTest, BuildUsername reflects that.
	defer ResetAutoCreateUserSettingForTest()

	SetAutoCreateUserSettingForTest(AutoCreateUserSetting{
		UsernamePrefix:        "ACME_",
		UsernameSuffixLength:  3,
		UsernameSuffixCharset: AutoCreateUserCharsetLetters,
	})

	got := GetAutoCreateUserSetting().BuildUsername(makeFakeRandomFn(nil))
	require.True(t, strings.HasPrefix(got, "ACME_"))
	require.True(t, strings.HasSuffix(got, ":3:1"))
}
