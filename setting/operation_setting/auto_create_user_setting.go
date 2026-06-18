package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// 自动创建用户后缀字符集
const (
	AutoCreateUserCharsetAlphanumeric = "alphanumeric"
	AutoCreateUserCharsetDigits       = "digits"
	AutoCreateUserCharsetLetters      = "letters"
)

// 自动创建用户密码生成模式
const (
	AutoCreateUserPasswordSameAsUsername = "same_as_username"
	AutoCreateUserPasswordRandom         = "random"
)

// AutoCreateUserCopyItem 描述"创建成功"弹窗中可一键复制的单行内容。
// Template 支持以下占位符（在前端渲染时替换；后端 RenderCopyItem 也走同一套规则）：
//   - {{username}} 实际创建的用户名
//   - {{password}} 实际创建的密码
//   - {{site}}     来自 AutoCreateUserSetting.SiteURL
type AutoCreateUserCopyItem struct {
	Label    string `json:"label"`
	Template string `json:"template"`
}

// AutoCreateUserSetting 集中描述"自动创建用户"功能的全部可配置规则。
// 通过 config.GlobalConfig.Register 自动持久化为
// "auto_create_user_setting.<field>" 形式的 option key。
type AutoCreateUserSetting struct {
	UsernamePrefix        string                   `json:"username_prefix"`
	UsernameSuffixLength  int                      `json:"username_suffix_length"`
	UsernameSuffixCharset string                   `json:"username_suffix_charset"`
	PasswordMode          string                   `json:"password_mode"`
	RandomPasswordLength  int                      `json:"random_password_length"`
	// DefaultQuota 为 0 时表示"未设定，按 common.QuotaForNewUser 兜底"，
	// 这一兜底逻辑发生在 service.PreviewAutoCreateUser，而不是落库时。
	DefaultQuota  int                      `json:"default_quota"`
	DefaultGroup  string                   `json:"default_group"`
	SiteURL       string                   `json:"site_url"`
	CopyTemplates []AutoCreateUserCopyItem `json:"copy_templates"`
}

// defaultAutoCreateUserSetting 是包初始化时的出厂默认；
// Reset 测试辅助函数依赖它来回滚自身。
func defaultAutoCreateUserSetting() AutoCreateUserSetting {
	return AutoCreateUserSetting{
		UsernamePrefix:        "User-",
		UsernameSuffixLength:  4,
		UsernameSuffixCharset: AutoCreateUserCharsetAlphanumeric,
		PasswordMode:          AutoCreateUserPasswordSameAsUsername,
		RandomPasswordLength:  12,
		DefaultQuota:          0,
		DefaultGroup:          "default",
		SiteURL:               "",
		CopyTemplates: []AutoCreateUserCopyItem{
			{Label: "站点", Template: "{{site}}"},
			{Label: "用户名", Template: "{{username}}"},
			{Label: "密码", Template: "{{password}}"},
		},
	}
}

var autoCreateUserSetting = defaultAutoCreateUserSetting()

func init() {
	config.GlobalConfig.Register("auto_create_user_setting", &autoCreateUserSetting)
}

// GetAutoCreateUserSetting 返回当前生效的配置快照。
// 返回值是按值拷贝，调用方对其的修改不会影响后续读取。
func GetAutoCreateUserSetting() AutoCreateUserSetting {
	return autoCreateUserSetting
}

// SetAutoCreateUserSettingForTest 仅供测试覆盖包级配置。
// 生产代码请通过 config.GlobalConfig + 管理后台 PUT /api/option 触发更新。
// 配合 defer ResetAutoCreateUserSettingForTest() 使用，避免测试间污染。
func SetAutoCreateUserSettingForTest(s AutoCreateUserSetting) {
	autoCreateUserSetting = s
}

// ResetAutoCreateUserSettingForTest 回滚到 defaultAutoCreateUserSetting()。
func ResetAutoCreateUserSettingForTest() {
	autoCreateUserSetting = defaultAutoCreateUserSetting()
}

// BuildUsername 按当前配置生成一个候选用户名。
// randomFn 是注入项，便于测试用确定性桩替代真实随机。
// 当 UsernameSuffixLength <= 0 时退化为 1，避免静默生成仅含前缀的用户名。
func (s AutoCreateUserSetting) BuildUsername(randomFn func(n int, charset string) string) string {
	n := s.UsernameSuffixLength
	if n <= 0 {
		n = 1
	}
	return s.UsernamePrefix + randomFn(n, s.UsernameSuffixCharset)
}

// BuildPassword 按当前 PasswordMode 派生密码。
//   - same_as_username：直接返回用户名。
//   - random：用 alphanumeric 字符集生成 RandomPasswordLength 长度的随机串；
//     若 RandomPasswordLength <= 0，回退到 8（匹配 model.User 的 password min=8 校验）。
func (s AutoCreateUserSetting) BuildPassword(username string, randomFn func(n int, charset string) string) string {
	if s.PasswordMode == AutoCreateUserPasswordRandom {
		n := s.RandomPasswordLength
		if n <= 0 {
			n = 8
		}
		return randomFn(n, AutoCreateUserCharsetAlphanumeric)
	}
	return username
}
