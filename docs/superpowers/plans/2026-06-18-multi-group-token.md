# 多分组令牌（跨分组调用）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让一个令牌可绑定多个分组，请求时按用户设定的顺序遍历各分组选渠道，某分组无可用渠道或失败时自动切到下一个分组（跨分组重试），无需为不同分组重复创建令牌。

**Architecture:** 复用 `Token.Group` 字段，从"单个分组名"升级为"逗号分隔的有序分组名"（零迁移）。后端在认证中间件按解析后的分组数量分流；多分组（≥2 个具体分组）时把有序列表写入新 context key `ContextKeyTokenGroups`，并复用现有 `auto` 模式的"按序遍历 + 跨分组重试"逻辑选渠道。计费天然正确，因为遍历命中分组时会写 `ContextKeyAutoGroup`，结算逻辑（`service/quota.go`）已据此覆盖 group ratio。前端两套（default/classic）分组选择改为多选，`auto` 与具体分组互斥，多选自动启用跨分组重试。

**Tech Stack:** Go 1.25 / Gin / GORM；React19+Rsbuild（web/default，TypeScript）、React18+Vite+Semi Design（web/classic）；i18n：go-i18n（后端）+ i18next（前端）。

## Global Constraints

- **JSON 操作**：业务代码禁止直接 `encoding/json`，必须用 `common.Marshal/Unmarshal/...`（CLAUDE.md Rule 1）。本计划不新增 JSON 序列化调用。
- **三库兼容**：SQLite / MySQL ≥5.7.8 / PostgreSQL ≥9.6 必须同时支持（Rule 2）。本方案不改表结构、不写裸 SQL，`Token.Group` 仍是普通 string 列。
- **前端包管理器**：web/default 用 `bun`（Rule 3）。
- **保护信息**：严禁修改/删除 `new-api`、`QuantumNous` 相关标识与版权头（Rule 5）。所有新建/修改文件保留现有版权头。
- **请求 DTO 零值**：可选标量用指针 + omitempty（Rule 6）。本计划不改 relay 上游 DTO，`Token` 是内部模型不受此约束。
- **分组名约束**：分组名不得包含逗号（与现有命名规则一致），这是逗号分隔存储的前提。
- **向后兼容**：单分组 / 空分组 / `auto` 令牌的行为必须完全不变。

---

## 文件结构

后端：
- `model/token.go` — 新增 `Token.GetGroups()` 解析方法。
- `model/token_groups_test.go`（新建）— `GetGroups()` 单测。
- `constant/context_key.go` — 新增 `ContextKeyTokenGroups`。
- `middleware/auth.go` — 认证中间件按分组数量分流，多分组写 context。
- `service/channel_select.go` — 抽取共用遍历逻辑，多分组模式接入。
- `service/channel_select_multigroup_test.go`（新建）— 多分组遍历单测。
- `controller/token.go` — 创建/更新时分组校验（越权、auto 互斥）。
- `controller/token_multigroup_test.go`（新建）— 校验单测。
- `i18n/translations/en.json`、`zh.json`（或现有后端文案文件）— 错误文案。

前端（default）：
- `web/default/src/features/keys/types.ts` — 表单 group 改数组。
- `web/default/src/features/keys/lib/api-key-form.ts` — schema/默认值/转换改数组。
- `web/default/src/features/keys/components/api-key-group-combobox.tsx` — 支持多选 + 互斥。
- `web/default/src/features/keys/components/api-keys-mutate-drawer.tsx` — 接入多选、开关显示逻辑。
- `web/default/src/i18n/locales/zh.json`、`en.json` — 文案。

前端（classic）：
- `web/classic/src/components/table/tokens/modals/EditTokenModal.jsx` — Form.Select 多选 + 互斥 + 提交转换。

---

## Task 1: 后端 `Token.GetGroups()` 分组解析

把 `Token.Group`（逗号分隔字符串）解析为有序、去空格、去空项的分组名列表。这是后续所有后端逻辑的基础。

**Files:**
- Modify: `model/token.go`（在 `Token` 结构体方法区新增函数，约第 33 行后）
- Test: `model/token_groups_test.go`（新建）

**Interfaces:**
- Produces: `func (token *Token) GetGroups() []string` — 返回解析后的有序分组名列表；空串返回空切片 `[]string{}`。

- [ ] **Step 1: 写失败测试**

新建 `model/token_groups_test.go`：

```go
package model

import (
	"reflect"
	"testing"
)

func TestTokenGetGroups(t *testing.T) {
	cases := []struct {
		name  string
		group string
		want  []string
	}{
		{"empty", "", []string{}},
		{"single", "vip", []string{"vip"}},
		{"auto", "auto", []string{"auto"}},
		{"multi", "claude,gpt", []string{"claude", "gpt"}},
		{"spaces", " claude , gpt ", []string{"claude", "gpt"}},
		{"empty-items", "claude,,gpt,", []string{"claude", "gpt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := &Token{Group: tc.group}
			got := token.GetGroups()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("GetGroups(%q) = %#v, want %#v", tc.group, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd E:/Prod_Project/other/new-api && go test ./model/ -run TestTokenGetGroups -v`
Expected: FAIL，提示 `token.GetGroups undefined`（编译错误）。

- [ ] **Step 3: 实现 `GetGroups()`**

在 `model/token.go` 第 36 行（`func (token *Token) Clean()` 之后）插入：

```go
// GetGroups 解析 token.Group（逗号分隔）为有序、去空格、去空项的分组名列表。
// 空字符串返回空切片；单分组/auto 返回单元素切片，保证向后兼容。
func (token *Token) GetGroups() []string {
	groups := make([]string, 0)
	for _, g := range strings.Split(token.Group, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			groups = append(groups, g)
		}
	}
	return groups
}
```

（`strings` 包已在 `model/token.go` 第 6 行导入，无需新增 import。）

- [ ] **Step 4: 运行测试确认通过**

Run: `cd E:/Prod_Project/other/new-api && go test ./model/ -run TestTokenGetGroups -v`
Expected: PASS（6 个子用例全部通过）。

- [ ] **Step 5: 提交**

```bash
git add model/token.go model/token_groups_test.go
git commit -m "feat(token): 新增 GetGroups() 解析逗号分隔多分组

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: 新增 context key `ContextKeyTokenGroups`

新增一个 context key，用于在认证层把令牌的有序分组列表传给渠道选择层。

**Files:**
- Modify: `constant/context_key.go`（token related keys 区块，约第 21 行后）

**Interfaces:**
- Produces: `constant.ContextKeyTokenGroups`（`ContextKey` 类型，值 `"token_groups"`）— 存 `[]string`，token 绑定的有序分组列表（仅多分组模式下设置）。

- [ ] **Step 1: 新增常量**

在 `constant/context_key.go` 第 21 行 `ContextKeyTokenCrossGroupRetry ContextKey = "token_cross_group_retry"` 之后插入一行：

```go
	ContextKeyTokenGroups            ContextKey = "token_groups"
```

- [ ] **Step 2: 验证编译**

Run: `cd E:/Prod_Project/other/new-api && go build ./constant/`
Expected: 无输出（编译成功）。

- [ ] **Step 3: 提交**

```bash
git add constant/context_key.go
git commit -m "feat(context): 新增 ContextKeyTokenGroups 传递多分组列表

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: 渠道选择支持多分组遍历（service/channel_select.go）

让 `CacheGetRandomSatisfiedChannel` 在检测到 `ContextKeyTokenGroups`（≥2 元素）时，按该有序列表遍历选渠道，复用 `auto` 模式完全相同的跨分组重试机制。这是核心改动。

**Files:**
- Modify: `service/channel_select.go`（`CacheGetRandomSatisfiedChannel`，第 83-162 行）
- Test: `service/channel_select_multigroup_test.go`（新建）

**Interfaces:**
- Consumes: `constant.ContextKeyTokenGroups`（Task 2）、现有 `model.GetRandomSatisfiedChannel`、`constant.ContextKeyAutoGroupIndex`、`constant.ContextKeyAutoGroup`。
- Produces: `CacheGetRandomSatisfiedChannel` 签名不变 `(*model.Channel, string, error)`；新增内部辅助 `selectChannelAcrossGroups(param *RetryParam, groups []string, crossGroupRetry bool) (*model.Channel, string)`。

- [ ] **Step 1: 写失败测试**

新建 `service/channel_select_multigroup_test.go`。该测试验证"分组来源解析"这一纯逻辑：当 context 中存在 `ContextKeyTokenGroups` 且长度 ≥2 时，应作为遍历分组来源；否则回退。我们测试新抽取的纯函数 `resolveSelectGroups`。

```go
package service

import (
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newTestCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	return c
}

func TestResolveSelectGroups(t *testing.T) {
	t.Run("multi-group from context", func(t *testing.T) {
		c := newTestCtx()
		c.Set(string(constant.ContextKeyTokenGroups), []string{"claude", "gpt"})
		groups, isMulti := resolveSelectGroups(c, "claude")
		assert.True(t, isMulti)
		assert.True(t, reflect.DeepEqual(groups, []string{"claude", "gpt"}))
	})

	t.Run("single context group is not multi", func(t *testing.T) {
		c := newTestCtx()
		c.Set(string(constant.ContextKeyTokenGroups), []string{"claude"})
		groups, isMulti := resolveSelectGroups(c, "claude")
		assert.False(t, isMulti)
		assert.Nil(t, groups)
	})

	t.Run("no context groups is not multi", func(t *testing.T) {
		c := newTestCtx()
		groups, isMulti := resolveSelectGroups(c, "vip")
		assert.False(t, isMulti)
		assert.Nil(t, groups)
	})
}
```

（`stretchr/testify` 已是项目依赖，见其他 `*_test.go`。）

- [ ] **Step 2: 运行测试确认失败**

Run: `cd E:/Prod_Project/other/new-api && go test ./service/ -run TestResolveSelectGroups -v`
Expected: FAIL，提示 `resolveSelectGroups undefined`。

- [ ] **Step 3: 实现 `resolveSelectGroups` 并接入遍历**

在 `service/channel_select.go` 文件末尾新增辅助函数：

```go
// resolveSelectGroups 判断本次请求是否为"多分组令牌"模式。
// 当 context 中 ContextKeyTokenGroups 存在且元素 >=2 时，返回该有序列表与 true；
// 否则返回 nil, false（走原有 auto / 单分组逻辑）。
func resolveSelectGroups(ctx *gin.Context, tokenGroup string) ([]string, bool) {
	v, exists := common.GetContextKey(ctx, constant.ContextKeyTokenGroups)
	if !exists {
		return nil, false
	}
	groups, ok := v.([]string)
	if !ok || len(groups) < 2 {
		return nil, false
	}
	return groups, true
}
```

然后把 `CacheGetRandomSatisfiedChannel` 中现有 `auto` 分支的遍历主体抽取为可复用函数。修改第 83-162 行的函数体为：

```go
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	var channel *model.Channel
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)

	// 多分组令牌模式：按 token 自定义的有序分组列表遍历（始终启用跨分组重试）
	if groups, isMulti := resolveSelectGroups(param.Ctx, param.TokenGroup); isMulti {
		channel, selectGroup = selectChannelAcrossGroups(param, groups, true)
		return channel, selectGroup, nil
	}

	if param.TokenGroup == "auto" {
		if len(setting.GetAutoGroups()) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}
		autoGroups := GetUserAutoGroup(userGroup)
		crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)
		channel, selectGroup = selectChannelAcrossGroups(param, autoGroups, crossGroupRetry)
	} else {
		channel, err = model.GetRandomSatisfiedChannel(param.TokenGroup, param.ModelName, param.GetRetry())
		if err != nil {
			return nil, param.TokenGroup, err
		}
	}
	return channel, selectGroup, nil
}

// selectChannelAcrossGroups 按有序分组列表遍历选渠道，复用跨分组重试状态机。
// 这是 auto 模式与多分组令牌模式的共用逻辑，二者仅分组列表来源不同。
func selectChannelAcrossGroups(param *RetryParam, groups []string, crossGroupRetry bool) (*model.Channel, string) {
	var channel *model.Channel
	selectGroup := param.TokenGroup

	startGroupIndex := 0
	if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
		if idx, ok := lastGroupIndex.(int); ok {
			startGroupIndex = idx
		}
	}

	for i := startGroupIndex; i < len(groups); i++ {
		group := groups[i]
		priorityRetry := param.GetRetry()
		if i > startGroupIndex {
			priorityRetry = 0
		}
		logger.LogDebug(param.Ctx, "Cross-group selecting group: %s, priorityRetry: %d", group, priorityRetry)

		channel, _ = model.GetRandomSatisfiedChannel(group, param.ModelName, priorityRetry)
		if channel == nil {
			logger.LogDebug(param.Ctx, "No available channel in group %s for model %s at priorityRetry %d, trying next group", group, param.ModelName, priorityRetry)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
			param.SetRetry(0)
			continue
		}
		common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, group)
		selectGroup = group
		logger.LogDebug(param.Ctx, "Cross-group selected group: %s", group)

		if crossGroupRetry && priorityRetry >= common.RetryTimes {
			logger.LogDebug(param.Ctx, "Current group %s retries exhausted (priorityRetry=%d >= RetryTimes=%d), preparing switch to next group for next retry", group, priorityRetry, common.RetryTimes)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
			param.SetRetry(0)
			param.ResetRetryNextTry()
		} else {
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
		}
		break
	}
	return channel, selectGroup
}
```

> 注意：抽取后行为与原 `auto` 分支逐行等价（仅日志文案从 "Auto" 改为 "Cross-group"），保证 auto 模式回归不变。`resolveSelectGroups` 的 `tokenGroup` 形参暂未使用但保留以备扩展——为避免 unused 警告，函数内不引用它即可（Go 允许未使用的函数形参）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd E:/Prod_Project/other/new-api && go test ./service/ -run TestResolveSelectGroups -v`
Expected: PASS（3 个子用例通过）。

- [ ] **Step 5: 编译全包并跑 service 既有测试，确认无回归**

Run: `cd E:/Prod_Project/other/new-api && go build ./... && go test ./service/ -run TestResolveSelectGroups -count=1`
Expected: 编译成功，测试 PASS。

- [ ] **Step 6: 提交**

```bash
git add service/channel_select.go service/channel_select_multigroup_test.go
git commit -m "feat(channel): 抽取跨分组遍历逻辑，支持多分组令牌

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: 认证中间件按分组数量分流（middleware/auth.go）

在 token 认证时解析 `token.GetGroups()`，按数量分流：0 个走原逻辑；含 `auto` 或 1 个具体分组走原逻辑；≥2 个具体分组写入 `ContextKeyTokenGroups` 并设初始 using group。每个分组都校验在用户可用分组内。

**Files:**
- Modify: `middleware/auth.go`（第 392-409 行的分组处理段）

**Interfaces:**
- Consumes: `token.GetGroups()`（Task 1）、`constant.ContextKeyTokenGroups`（Task 2）、`service.GetUserUsableGroups`、`ratio_setting.ContainsGroupRatio`。
- Produces: 多分组模式下设置 `ContextKeyTokenGroups`（`[]string`）与 `ContextKeyUsingGroup`（首个分组）。

- [ ] **Step 1: 替换分组处理逻辑**

将 `middleware/auth.go` 第 392-409 行：

```go
		userGroup := userCache.Group
		tokenGroup := token.Group
		if tokenGroup != "" {
			// check common.UserUsableGroups[userGroup]
			if _, ok := service.GetUserUsableGroups(userGroup)[tokenGroup]; !ok {
				abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("无权访问 %s 分组", tokenGroup))
				return
			}
			// check group in common.GroupRatio
			if !ratio_setting.ContainsGroupRatio(tokenGroup) {
				if tokenGroup != "auto" {
					abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("分组 %s 已被弃用", tokenGroup))
					return
				}
			}
			userGroup = tokenGroup
		}
		common.SetContextKey(c, constant.ContextKeyUsingGroup, userGroup)
```

替换为：

```go
		userGroup := userCache.Group
		tokenGroups := token.GetGroups()
		usableGroups := service.GetUserUsableGroups(userGroup)
		// 逐个校验 token 绑定的每个分组都在用户可用分组内，且未被弃用
		for _, g := range tokenGroups {
			if _, ok := usableGroups[g]; !ok {
				abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("无权访问 %s 分组", g))
				return
			}
			if !ratio_setting.ContainsGroupRatio(g) && g != "auto" {
				abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("分组 %s 已被弃用", g))
				return
			}
		}
		if len(tokenGroups) >= 2 {
			// 多分组令牌：写入有序列表，初始 using group 取首个分组
			common.SetContextKey(c, constant.ContextKeyTokenGroups, tokenGroups)
			userGroup = tokenGroups[0]
		} else if len(tokenGroups) == 1 {
			// 单分组（含 auto）：等价原行为，覆盖 userGroup
			userGroup = tokenGroups[0]
		}
		common.SetContextKey(c, constant.ContextKeyUsingGroup, userGroup)
```

> 说明：`len(tokenGroups)==0`（空 group）时 `userGroup` 保持 `userCache.Group`，与原逻辑一致；单分组分支与原 `tokenGroup != ""` 行为逐项等价（校验已在循环内完成）。

- [ ] **Step 2: 验证编译**

Run: `cd E:/Prod_Project/other/new-api && go build ./middleware/`
Expected: 无输出（编译成功）。检查 `service`、`ratio_setting`、`constant`、`fmt` 均已在该文件导入（原代码已用，无需新增）。

- [ ] **Step 3: 全包编译**

Run: `cd E:/Prod_Project/other/new-api && go build ./...`
Expected: 编译成功。

- [ ] **Step 4: 提交**

```bash
git add middleware/auth.go
git commit -m "feat(auth): 认证中间件支持多分组令牌分流与校验

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: 创建/更新令牌的分组校验（controller/token.go）

在 `AddToken` / `UpdateToken` 中校验：每个分组都在用户可用分组内；`auto` 不能与其它分组同选。复用 `token.GetGroups()` 解析。

**Files:**
- Modify: `controller/token.go`（`AddToken` 第 167-234 行、`UpdateToken` 第 250-313 行）
- Test: `controller/token_multigroup_test.go`（新建）

**Interfaces:**
- Consumes: `token.GetGroups()`（Task 1）、`service.GetUserUsableGroups`。
- Produces: 新增内部校验函数 `validateTokenGroups(userGroup string, groups []string) error`，供 Add/Update 共用。

- [ ] **Step 1: 写失败测试**

新建 `controller/token_multigroup_test.go`：

```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestValidateTokenGroups(t *testing.T) {
	usable := map[string]string{"default": "默认", "claude": "c", "gpt": "g", "auto": "自动"}

	cases := []struct {
		name    string
		groups  []string
		wantErr bool
	}{
		{"empty ok", []string{}, false},
		{"single ok", []string{"claude"}, false},
		{"multi ok", []string{"claude", "gpt"}, false},
		{"auto alone ok", []string{"auto"}, false},
		{"unknown group rejected", []string{"claude", "unknown"}, true},
		{"auto with others rejected", []string{"auto", "claude"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTokenGroupsWithUsable(usable, tc.groups)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateTokenGroups(%v) err=%v, wantErr=%v", tc.groups, err, tc.wantErr)
			}
		})
	}
	_ = model.Token{}
}
```

> 为可单测（不依赖 DB/setting），把核心校验抽成接受 `usable map` 的纯函数 `validateTokenGroupsWithUsable`，外层 `validateTokenGroups` 负责取 usable map。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd E:/Prod_Project/other/new-api && go test ./controller/ -run TestValidateTokenGroups -v`
Expected: FAIL，提示 `validateTokenGroupsWithUsable undefined`。

- [ ] **Step 3: 实现校验函数**

在 `controller/token.go` 文件末尾新增：

```go
// validateTokenGroupsWithUsable 校验分组列表合法性（纯逻辑，便于单测）：
//  1. auto 不能与其它分组同时选择；
//  2. 每个分组都必须在用户可用分组内。
func validateTokenGroupsWithUsable(usable map[string]string, groups []string) error {
	for _, g := range groups {
		if g == "auto" && len(groups) > 1 {
			return fmt.Errorf("auto 分组不能与其他分组同时选择")
		}
	}
	for _, g := range groups {
		if _, ok := usable[g]; !ok {
			return fmt.Errorf("无权访问 %s 分组", g)
		}
	}
	return nil
}

// validateTokenGroups 取用户可用分组后校验 token 的分组列表。
func validateTokenGroups(userGroup string, groups []string) error {
	return validateTokenGroupsWithUsable(service.GetUserUsableGroups(userGroup), groups)
}
```

确认 `controller/token.go` 顶部已导入 `fmt` 与 `github.com/QuantumNous/new-api/service`。若 `service` 未导入则新增该 import。

- [ ] **Step 4: 在 AddToken 接入校验**

取当前用户分组用 `model.GetUserGroup(id int, fromDB bool) (string, error)`（已确认存在于 `model/user.go:822`）。

在 `controller/token.go` `AddToken` 中，`key, err := common.GenerateKey()` 之前（约第 203 行 `if int(count) >= maxTokens {...}` 之后）插入：

```go
	userGroup, _ := model.GetUserGroup(c.GetInt("id"), false)
	if err := validateTokenGroups(userGroup, token.GetGroups()); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
```

- [ ] **Step 5: 在 UpdateToken 接入校验**

在 `UpdateToken` 的 `cleanToken, err := model.GetTokenByIds(token.Id, userId)` 之后、`if token.Status == common.TokenStatusEnabled {` 之前插入：

```go
	if statusOnly == "" {
		userGroup, _ := model.GetUserGroup(userId, false)
		if err := validateTokenGroups(userGroup, token.GetGroups()); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
```

（`userId` 已是 `UpdateToken` 第 251 行的局部变量。）

- [ ] **Step 6: 运行测试确认通过 + 全包编译**

Run: `cd E:/Prod_Project/other/new-api && go build ./... && go test ./controller/ -run TestValidateTokenGroups -v`
Expected: 编译成功；测试 PASS（6 个子用例）。

- [ ] **Step 7: 提交**

```bash
git add controller/token.go controller/token_multigroup_test.go
git commit -m "feat(token): 创建/更新令牌校验多分组合法性与 auto 互斥

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: 后端错误文案 i18n

为新增的中文硬编码错误信息补 i18n（与现有 `auth.go` 里"无权访问 %s 分组"风格保持一致——现状是硬编码中文，本任务保持同风格即可，不强行引入 message key，除非现有 controller 校验普遍使用 i18n key）。

**Files:**
- 检查：`controller/token.go`、`middleware/auth.go` 新增文案是否需要走 `i18n.MsgXxx`。

- [ ] **Step 1: 判断现有约定**

Run: `cd E:/Prod_Project/other/new-api && grep -rn "无权访问" middleware/ controller/`
Expected: 观察现有"无权访问 %s 分组"是否硬编码。**现状（auth.go:397）为硬编码中文**，因此本功能新增文案沿用硬编码中文即可，保持一致性，无需新增 i18n key。

- [ ] **Step 2: 确认无遗留**

若上一步发现项目已统一用 i18n key，则把 Task 4/5 中的中文串替换为新增的 `i18n.MsgTokenGroupXxx` 并在 `i18n/translations/{en,zh}.json` 添加条目。否则跳过。

- [ ] **Step 3: 提交（若有改动）**

```bash
git add -A && git commit -m "i18n(token): 多分组错误文案对齐现有约定

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

> 若 Step 1 判定无需改动，本任务无提交，直接进入下一任务。

---

## Task 7: default 前端类型与表单转换改为多分组

把 web/default 令牌表单的 `group` 从字符串改为字符串数组，并处理提交（join）/回显（split）与跨分组重试自动启用。

**Files:**
- Modify: `web/default/src/features/keys/types.ts`（`ApiKeyFormData.group`）
- Modify: `web/default/src/features/keys/lib/api-key-form.ts`（schema/默认值/转换）

**Interfaces:**
- Produces: 表单字段 `groups: string[]`（替代单值 `group`）；payload 仍发送 `group: string`（逗号拼接）。

- [ ] **Step 1: 修改 schema 与默认值（api-key-form.ts）**

将 `getApiKeyFormSchema` 中 `group: z.string().optional(),`（第 38 行）改为：

```ts
      groups: z.array(z.string()),
```

将 `API_KEY_FORM_DEFAULT_VALUES`（第 66-76 行）中 `group: DEFAULT_GROUP,` 改为 `groups: [DEFAULT_GROUP],`。

将 `getApiKeyFormDefaultValues`（第 78-86 行）改为：

```ts
export function getApiKeyFormDefaultValues(
  defaultUseAutoGroup: boolean
): ApiKeyFormValues {
  return {
    ...API_KEY_FORM_DEFAULT_VALUES,
    groups: defaultUseAutoGroup ? ['auto'] : [DEFAULT_GROUP],
    cross_group_retry: defaultUseAutoGroup,
  }
}
```

- [ ] **Step 2: 修改提交转换（transformFormDataToPayload）**

将第 95-113 行 `transformFormDataToPayload` 中 group 相关行改为：

```ts
    group: (data.groups || []).join(','),
    cross_group_retry:
      data.groups.includes('auto')
        ? !!data.cross_group_retry
        : data.groups.length >= 2,
```

> 含 auto（此时长度为 1，因互斥）沿用开关值；多个具体分组自动启用跨分组重试；单个具体分组为 false。

- [ ] **Step 3: 修改回显转换（transformApiKeyToFormDefaults）**

将第 118-139 行中 `group: apiKey.group || DEFAULT_GROUP,` 改为：

```ts
    groups: apiKey.group
      ? apiKey.group.split(',').map((g) => g.trim()).filter(Boolean)
      : [DEFAULT_GROUP],
```

- [ ] **Step 4: 类型检查**

Run: `cd E:/Prod_Project/other/new-api/web/default && bun run tsc --noEmit` （若无 tsc 脚本，用 `bunx tsc --noEmit`）
Expected: 报错集中在 `api-keys-mutate-drawer.tsx` 仍引用 `group`（下个任务修复）；`api-key-form.ts`/`types.ts` 自身无错。

- [ ] **Step 5: 提交**

```bash
git add web/default/src/features/keys/lib/api-key-form.ts web/default/src/features/keys/types.ts
git commit -m "feat(web): 令牌表单分组改为多选数组(default)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: default 前端多选分组组件与互斥逻辑

把单选 `ApiKeyGroupCombobox` 升级为多选，并实现 `auto` 与具体分组互斥；在 drawer 中接入数组字段、调整跨分组重试开关显示。

**Files:**
- Modify: `web/default/src/features/keys/components/api-key-group-combobox.tsx`（改为多选）
- Modify: `web/default/src/features/keys/components/api-keys-mutate-drawer.tsx`（接入 `groups` 字段、开关显示条件、load 后校正）

**Interfaces:**
- Consumes: `ApiKeyFormValues.groups`（Task 7）。
- Produces: `ApiKeyGroupCombobox` 新 props：`value: string[]`、`onValueChange: (value: string[]) => void`（多选语义，内部处理 auto 互斥）。

- [ ] **Step 1: 改造 combobox 为多选 + 互斥**

修改 `api-key-group-combobox.tsx`。将 `ApiKeyGroupComboboxProps`（第 46-52 行）改为：

```ts
type ApiKeyGroupComboboxProps = {
  options: ApiKeyGroupOption[]
  value?: string[]
  onValueChange: (value: string[]) => void
  placeholder?: string
  disabled?: boolean
}
```

将组件主体（第 98-209 行）的选中态与 `handleSelect` 改为多选语义。关键替换：

把 `const selectedOption = options.find((option) => option.value === value)` 改为：

```ts
  const selected = value ?? []
  const selectedOptions = options.filter((o) => selected.includes(o.value))
```

把 `handleSelect`（第 125-129 行）改为多选 + 互斥切换：

```ts
  const handleSelect = (selectedValue: string) => {
    const current = value ?? []
    let next: string[]
    if (current.includes(selectedValue)) {
      next = current.filter((v) => v !== selectedValue)
    } else if (selectedValue === 'auto') {
      // 选 auto 时清空其它分组（互斥）
      next = ['auto']
    } else {
      // 选具体分组时移除 auto（互斥）
      next = [...current.filter((v) => v !== 'auto'), selectedValue]
    }
    onValueChange(next)
    setSearchValue('')
    // 多选不自动关闭弹层，便于连续勾选
  }
```

把 PopoverTrigger 内显示选中项的区块（第 145-159 行）改为显示多个标签/计数：

```tsx
        <span className='flex min-w-0 flex-1 items-center justify-between gap-2 sm:gap-3'>
          <span className='min-w-0'>
            <span className='block truncate font-medium'>
              {selectedOptions.length > 0
                ? selectedOptions.map((o) => o.label).join(', ')
                : placeholder || t('Select groups')}
            </span>
          </span>
        </span>
```

把 `CommandItem` 的勾选标记（第 184-189 行 `value === option.value`）改为 `selected.includes(option.value)`。

- [ ] **Step 2: drawer 接入数组字段**

修改 `api-keys-mutate-drawer.tsx`：

将 group `FormField`（第 298-315 行）的 `name='group'` 改为 `name='groups'`，并把 `ApiKeyGroupCombobox` 的 `value={field.value}` 保持（现在是数组）、label 文案改 `t('Groups')`、placeholder 改 `t('Select groups')`。

将 `const selectedGroup = form.watch('group')`（第 246 行）改为：

```ts
  const selectedGroups = form.watch('groups')
  const isAutoGroup = (selectedGroups ?? []).includes('auto')
  const isMultiGroup = (selectedGroups ?? []).length >= 2
```

将跨分组重试 FormField 的外层条件 `{selectedGroup === 'auto' && (`（第 317 行）改为 `{isAutoGroup && (`。并在该开关下方（或多分组时）新增只读提示：

```tsx
              {isMultiGroup && (
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Multiple groups selected: cross-group retry is enabled automatically.'
                  )}
                </p>
              )}
```

将"load 后校正分组"的 effect（第 151-164 行，针对单值 group）改为数组版：

```ts
  useEffect(() => {
    if (groups.length === 0) return
    const current = form.getValues('groups') ?? []
    const valid = current.filter((g) => groups.some((o) => o.value === g))
    if (valid.length !== current.length) {
      const fallback =
        groups.find((g) => g.value === 'default')?.value ??
        groups[0]?.value ??
        ''
      form.setValue('groups', valid.length > 0 ? valid : [fallback])
    }
  }, [groups, form])
```

- [ ] **Step 3: 类型检查 + 构建**

Run: `cd E:/Prod_Project/other/new-api/web/default && bunx tsc --noEmit`
Expected: 无类型错误。

Run: `cd E:/Prod_Project/other/new-api/web/default && bun run build`
Expected: 构建成功。

- [ ] **Step 4: 提交**

```bash
git add web/default/src/features/keys/components/api-key-group-combobox.tsx web/default/src/features/keys/components/api-keys-mutate-drawer.tsx
git commit -m "feat(web): 令牌分组多选组件与 auto 互斥(default)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9: default 前端 i18n 文案

为新增/改动的英文 key 补 zh 翻译（en 为 base，无需单独文件）。

**Files:**
- Modify: `web/default/src/i18n/locales/zh.json`

**Interfaces:**
- Consumes: Task 7/8 新增的 `t('...')` key。

- [ ] **Step 1: 补 zh 翻译**

在 `web/default/src/i18n/locales/zh.json` 新增（若已存在则跳过对应行）：

```json
  "Groups": "令牌分组",
  "Select groups": "选择分组（可多选）",
  "Multiple groups selected: cross-group retry is enabled automatically.": "已选择多个分组：将自动按顺序跨分组重试。"
```

- [ ] **Step 2: 运行 i18n 同步检查**

Run: `cd E:/Prod_Project/other/new-api/web/default && bun run i18n:sync`
Expected: 无缺失 key 报错（其余语言由工具补占位）。

- [ ] **Step 3: 提交**

```bash
git add web/default/src/i18n/locales/
git commit -m "i18n(web): 多分组令牌文案(default)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 10: classic 前端多选改造

把 classic 的 `EditTokenModal.jsx` 分组 `Form.Select` 改为多选，提交时数组 join、回显 split，并实现 auto 互斥与跨分组重试显示。

**Files:**
- Modify: `web/classic/src/components/table/tokens/modals/EditTokenModal.jsx`

**Interfaces:**
- Consumes: 后端 `group`（逗号分隔字符串）。

- [ ] **Step 1: Form.Select 改多选 + 互斥**

将分组 `Form.Select`（第 387-403 行）增加 `multiple` 并处理互斥 onChange：

```jsx
                      <Form.Select
                        field='group'
                        label={t('令牌分组')}
                        placeholder={t('令牌分组，默认为用户的分组（可多选）')}
                        optionList={groups}
                        multiple
                        renderOptionItem={renderGroupOption}
                        filter={(input, option) => {
                          const q = input.toLowerCase();
                          return (
                            option.value?.toLowerCase().includes(q) ||
                            (typeof option.label === 'string' &&
                              option.label.toLowerCase().includes(q))
                          );
                        }}
                        onChange={(val) => {
                          // auto 与具体分组互斥
                          let next = Array.isArray(val) ? val : [val];
                          if (next.includes('auto') && next.length > 1) {
                            // 若新增了 auto，则只留 auto；否则移除 auto
                            const last = next[next.length - 1];
                            next = last === 'auto' ? ['auto'] : next.filter((v) => v !== 'auto');
                          }
                          formApiRef.current?.setValue('group', next);
                        }}
                        showClear
                        style={{ width: '100%' }}
                      />
```

- [ ] **Step 2: 跨分组重试显示条件改为数组判断**

将第 413-427 行的 `Col` 显示条件 `values.group === 'auto'` 改为：

```jsx
                  <Col
                    span={24}
                    style={{
                      display:
                        Array.isArray(values.group) && values.group.includes('auto')
                          ? 'block'
                          : 'none',
                    }}
                  >
```

并在其后新增多分组提示（多个具体分组时）：

```jsx
                  <Col
                    span={24}
                    style={{
                      display:
                        Array.isArray(values.group) &&
                        values.group.length >= 2 &&
                        !values.group.includes('auto')
                          ? 'block'
                          : 'none',
                    }}
                  >
                    <Text type='tertiary'>
                      {t('已选择多个分组：将自动按顺序跨分组重试')}
                    </Text>
                  </Col>
```

- [ ] **Step 3: 回显 split（loadToken）**

在 `loadToken`（第 159-182 行）中，`formApiRef.current.setValues` 之前，把字符串 group 转数组。在第 167-171 行 `model_limits` 处理块之后插入：

```jsx
      if (typeof data.group === 'string') {
        data.group = data.group
          ? data.group.split(',').map((g) => g.trim()).filter(Boolean)
          : [];
      }
```

并把 `getInitValues()` 中 `group: '',`（第 82 行）改为 `group: [],`。

- [ ] **Step 4: 提交时 join（提交函数 `submit`）**

提交逻辑在该文件的 `const submit = async (values) => {`（第 218 行）：编辑走 `API.put('/api/token/', {...})`（第 241 行），新建走 `API.post('/api/token/', localInputs)`（第 285 行）。

在 `submit` 函数体开头，对 `values` 做分组归一化，得到 `payload`，后续 put/post 用 `payload` 替代直接展开 `values` / `localInputs` 中的分组字段：

```jsx
    const groupArr = Array.isArray(values.group)
      ? values.group
      : values.group
        ? [values.group]
        : [];
    const normalizedGroup = {
      group: groupArr.join(','),
      cross_group_retry: groupArr.includes('auto')
        ? !!values.cross_group_retry
        : groupArr.length >= 2,
    };
```

在 `API.put('/api/token/', {...})` 的 body 对象中合并 `...normalizedGroup`（放在展开 values 之后以覆盖）；在 `localInputs` 构造后、`API.post` 之前执行 `Object.assign(localInputs, normalizedGroup)`。实现者按第 241/285 行实际 body 结构就近合并，确保最终发送的 `group` 是逗号字符串、`cross_group_retry` 按规则设置。

- [ ] **Step 5: 构建验证**

Run: `cd E:/Prod_Project/other/new-api/web/classic && npm install && npm run build`（classic 用 Vite，按其 package.json 脚本）
Expected: 构建成功。

- [ ] **Step 6: 提交**

```bash
git add web/classic/src/components/table/tokens/modals/EditTokenModal.jsx
git commit -m "feat(web): 令牌分组多选与跨分组重试(classic)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 11: 端到端联调与回归验证

跑全部后端测试、构建两套前端，并手动验证关键路径。

**Files:** 无（验证任务）

- [ ] **Step 1: 后端全量测试**

Run: `cd E:/Prod_Project/other/new-api && go build ./... && go test ./model/ ./service/ ./controller/ -count=1`
Expected: 全部 PASS。

- [ ] **Step 2: 两套前端构建**

Run: `cd E:/Prod_Project/other/new-api/web/default && bun run build`
Run: `cd E:/Prod_Project/other/new-api/web/classic && npm run build`
Expected: 均构建成功。

- [ ] **Step 3: 手动验证清单（记录结果）**

启动服务，准备两个分组 `claude`、`gpt`，各配至少一个渠道与不同模型。

1. 建令牌绑定 `claude,gpt`：调用仅 claude 有的模型 → 命中 claude，日志计费按 claude group ratio。
2. 同令牌调用仅 gpt 有的模型 → 命中 gpt，计费按 gpt。
3. 两分组都有的同名模型 → 命中靠前分组（claude）。
4. 把 claude 渠道临时禁用 → 同令牌请求 claude 模型 → 自动回退到 gpt（若 gpt 也有该模型）或正确报错。
5. 回归：单分组令牌（仅 `vip`）、空分组令牌、`auto` 令牌行为均与改造前一致。
6. 前端：default 与 classic 均能多选、auto 与具体分组互斥、≥2 分组时显示自动跨分组重试提示、保存后回显正确。

- [ ] **Step 4: 提交（如有联调修复）**

```bash
git add -A && git commit -m "test: 多分组令牌端到端联调修复

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 自检备注（写计划者）

- **Spec 覆盖**：存储(Task1)、context(Task2)、渠道遍历(Task3)、认证分流(Task4)、校验(Task5)、i18n(Task6/9)、前端 default(Task7/8)、前端 classic(Task10)、测试(Task11) 均有对应任务。
- **计费正确性**：经代码核实，`service/quota.go:111` 读 `ContextKeyAutoGroup` 覆盖 group ratio；Task3 遍历命中时写该 key，故多分组计费天然正确，无需额外任务。
- **类型一致**：`GetGroups() []string`（Task1）→ `ContextKeyTokenGroups []string`（Task2/4）→ `resolveSelectGroups`/`selectChannelAcrossGroups`（Task3）一致；前端 `groups: string[]`（Task7/8）payload join 为 `group: string` 与后端契约一致。
- **已消除的不确定项**：Task5 取用户分组用已确认的 `model.GetUserGroup(id, fromDB)`（`model/user.go:822`）；Task10 classic 提交函数为 `submit`（第218行，put@241 / post@285）。
