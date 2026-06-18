package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ErrUsernameCollisionExhausted 表示自动生成的用户名连续 5 次都已存在，放弃。
// 控制器层应将其转换为 i18n.MsgAutoCreateUsernameCollision 之后返回，让前端提示
// 管理员"重新生成"再试。其他错误（典型如数据库异常）应原样上报。
var ErrUsernameCollisionExhausted = errors.New("auto-create: username collision retries exhausted")

// previewMaxAttempts 是 PreviewAutoCreateUser 的硬上限。提到 5 已经足以让 4 位
// 字母数字（62^4 ≈ 1480 万空间）几乎不可能连续重复；调高没有任何收益，调低又
// 会在 default group 名字模式上误伤合法用户。
const previewMaxAttempts = 5

// UsernameExistsFunc 抽象掉数据库依赖，让 PreviewAutoCreateUser 可以在不引导
// GORM/SQLite 的情况下走通完整的单测覆盖。生产路径里由 controller 注入
// model.CheckUserExistOrDeleted 的包装。
type UsernameExistsFunc func(username string) (bool, error)

// PreviewAutoCreateUser 按当前 AutoCreateUserSetting 推演一次"自动创建用户"
// 弹窗的初始值。它本身只读，不会创建任何用户；落库由调用方在拿到管理员确认
// 后通过 POST /api/user/ 完成。
//
// 调用顺序：
//  1. quota: 优先用 setting.DefaultQuota；为 0 时回退到 common.QuotaForNewUser。
//  2. group: 优先用 setting.DefaultGroup；空字符串回退到 "default"。
//  3. username: 最多重试 previewMaxAttempts 次；每次生成新候选并查询 exists。
//     全部命中即返回 ErrUsernameCollisionExhausted。
//  4. password: BuildPassword 按 PasswordMode 派生（要么照抄 username，要么
//     用 alphanumeric 随机串）。
func PreviewAutoCreateUser(
	s operation_setting.AutoCreateUserSetting,
	randomFn func(int, string) string,
	exists UsernameExistsFunc,
) (dto.AutoCreateUserPreviewResponse, error) {
	quota := s.DefaultQuota
	if quota == 0 {
		quota = common.QuotaForNewUser
	}
	group := s.DefaultGroup
	if group == "" {
		group = "default"
	}

	var username string
	for attempt := 0; attempt < previewMaxAttempts; attempt++ {
		candidate := s.BuildUsername(randomFn)
		taken, err := exists(candidate)
		if err != nil {
			return dto.AutoCreateUserPreviewResponse{}, err
		}
		if !taken {
			username = candidate
			break
		}
	}
	if username == "" {
		return dto.AutoCreateUserPreviewResponse{}, ErrUsernameCollisionExhausted
	}

	password := s.BuildPassword(username, randomFn)
	return dto.AutoCreateUserPreviewResponse{
		Username: username,
		Password: password,
		Group:    group,
		Quota:    quota,
	}, nil
}

// RenderCopyItem 是"创建成功"弹窗中单行复制内容的占位符替换函数。
// 设计文档要求前端在渲染时完成替换（让管理员在编辑模板时能看到实时预览），
// 后端保留这个实现仅作单测真值和未来服务端渲染（如发邮件）的契约锚点。
//
// 已识别的占位符：{{username}} {{password}} {{site}}。
// 任何其它形如 {{...}} 的写法都按字面量保留，便于管理员后续临时插入自定义
// 占位符，再决定升级到一等公民。
func RenderCopyItem(tmpl, username, password, siteURL string) string {
	out := tmpl
	out = strings.ReplaceAll(out, "{{username}}", username)
	out = strings.ReplaceAll(out, "{{password}}", password)
	out = strings.ReplaceAll(out, "{{site}}", siteURL)
	return out
}
