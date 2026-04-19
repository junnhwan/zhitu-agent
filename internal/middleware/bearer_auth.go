package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// BearerAuth 校验 Authorization: Bearer <token> 头。
// expected 为空时直接放行所有请求（调用方需要自行在启动时拒绝空 token 的 enabled 配置）。
// onReject 在鉴权失败时被调用（通常注入 metrics）。使用 constant-time 比较避免时间侧信道。
func BearerAuth(expected string, onReject func()) gin.HandlerFunc {
	expBytes := []byte("Bearer " + expected)
	return func(c *gin.Context) {
		got := c.GetHeader("Authorization")
		if subtle.ConstantTimeCompare([]byte(got), expBytes) != 1 {
			if onReject != nil {
				onReject()
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}
