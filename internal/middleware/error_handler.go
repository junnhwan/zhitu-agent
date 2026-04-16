package middleware

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/zhitu-agent/zhitu-agent/internal/common"
)

// ErrorHandler mirrors Java GlobalExceptionHandler
// Recovers from panics and returns JSON error response per the mixed response contract
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[GlobalExceptionHandler] panic recovered: %v\n%s", err, debug.Stack())

				switch e := err.(type) {
				case *common.BusinessException:
					c.JSON(http.StatusOK, common.ErrorWithCode(e.Code, e.Message))
				case error:
					c.JSON(http.StatusInternalServerError, common.Error(common.SystemError, "系统错误"))
				default:
					c.JSON(http.StatusInternalServerError, common.Error(common.SystemError, "系统内部异常"))
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}
