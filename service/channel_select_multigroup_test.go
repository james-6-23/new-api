package service

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newTestCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
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
