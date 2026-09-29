package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

func TestExtractVolcTaskID(t *testing.T) {
	cases := map[string]string{
		"/api/v3/contents/generations/tasks/cgt-2026-abc": "cgt-2026-abc",
		"/api/v3/contents/generations/tasks/cgt-1/":       "cgt-1",
		"/api/v3/contents/generations/tasks":              "",
		"/api/v3/contents/generations/tasks/":             "",
		// 列表类子路径不是单个任务 ID，不能当成 ID 去反查渠道。
		"/api/v3/contents/generations/tasks/cgt-1/frames": "",
		"/api/v3/chat/completions":                        "",
	}
	for path, want := range cases {
		if got := extractVolcTaskID(path); got != want {
			t.Fatalf("extractVolcTaskID(%q) = %q, want %q", path, got, want)
		}
	}
}

func newGroupContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/volc/api/v3/files", nil)
	return c
}

func TestCandidateGroupsLiteralGroup(t *testing.T) {
	c := newGroupContext(t)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "vip")
	got := candidateGroups(c)
	if len(got) != 1 || got[0] != "vip" {
		t.Fatalf("got %v, want [vip]", got)
	}
}

// 多分组令牌：首个分组没有透传渠道时要能继续尝试后续分组。
func TestCandidateGroupsMultiGroupToken(t *testing.T) {
	c := newGroupContext(t)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "vip")
	common.SetContextKey(c, constant.ContextKeyTokenGroups, []string{"vip", "default"})
	got := candidateGroups(c)
	if len(got) != 2 || got[0] != "vip" || got[1] != "default" {
		t.Fatalf("got %v, want [vip default]", got)
	}
}

// auto 不是真实分组名，拿它直接查渠道永远查不到，必须展开。
func TestCandidateGroupsAutoFallsBackToUserGroup(t *testing.T) {
	c := newGroupContext(t)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "auto")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	got := candidateGroups(c)
	if len(got) == 0 {
		t.Fatal("auto must expand to at least the user group")
	}
	for _, g := range got {
		if g == "auto" {
			t.Fatal("auto must never be used as a literal group name")
		}
	}
}

func TestCandidateGroupsEmpty(t *testing.T) {
	c := newGroupContext(t)
	if got := candidateGroups(c); len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestNormalizeVolcPassthroughPath(t *testing.T) {
	cases := map[string]string{
		// 数据面挂在站点根上,路径与官方逐字一致,不做任何剥离。
		"/api/v3/chat/completions":            "/api/v3/chat/completions",
		"/api/v3/contents/generations/tasks":  "/api/v3/contents/generations/tasks",
		"/api/compatible/v1/chat/completions": "/api/compatible/v1/chat/completions",
		// /volc 前缀兼容写法,剥离后同样得到官方路径。
		"/volc/api/v3/chat/completions": "/api/v3/chat/completions",
		// 控制面官方在 host 根,站点用 /volc 承载,还原为 "/"。
		"/volc":  "/",
		"/volc/": "/",
	}
	for in, want := range cases {
		if got := normalizeVolcPassthroughPath(in); got != want {
			t.Fatalf("normalizeVolcPassthroughPath(%q) = %q, want %q", in, got, want)
		}
	}
}
