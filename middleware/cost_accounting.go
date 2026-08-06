/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

// CostAccountingEnabled 拦截成本核算相关接口。
//
// 该中间件挂载在 RootAuth 之前：功能关闭时，任何调用方（包括未登录的探测）
// 得到的都是一致的「功能未启用」，而不是先暴露一个权限错误。
func CostAccountingEnabled() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.CostAccountingEnabled {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgFeatureDisabled),
			})
			return
		}
		c.Next()
	}
}
