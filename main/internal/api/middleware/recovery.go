package middleware

import (
	"fmt"
	"main/internal/logger"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"
)

// Recovery 自定义panic恢复中间件
// 相比默认gin.Recovery()的改进:
//   - 记录请求ID用于关联追踪
//   - 集成RequestTrace错误标记
//   - 使用结构化日志记录到文件
//   - 区分连接中断（客户端断开）和真实panic
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// 检查是否是客户端连接中断（不需要告警）
				var brokenPipe bool
				if ne, ok := err.(*net.OpError); ok {
					if se, ok := ne.Err.(*os.SyscallError); ok {
						errStr := strings.ToLower(se.Error())
						if strings.Contains(errStr, "broken pipe") ||
							strings.Contains(errStr, "connection reset by peer") {
							brokenPipe = true
						}
					}
				}

				// 获取请求上下文信息
				reqID := c.GetString(RequestIDKey)
				method := c.Request.Method
				path := c.Request.URL.Path
				clientIP := c.ClientIP()

				// 获取堆栈信息
				buf := make([]byte, 8192)
				n := runtime.Stack(buf, false)
				stackTrace := string(buf[:n])

				if brokenPipe {
					// 客户端断开连接，记录为警告
					logger.Warn("[RECOVERY] 连接中断 %s %s ip=%s rid=%s err=%v",
						method, path, clientIP, reqID, err)
					c.Abort()
					return
				}

				// 真实panic，记录为严重错误
				errMsg := fmt.Sprintf("PANIC: %v", err)
				logger.Error("[RECOVERY] %s %s %s ip=%s rid=%s\n%s",
					errMsg, method, path, clientIP, reqID, stackTrace)

				// 标记到RequestTrace用于数据库记录
				SetError(c, errMsg)
				c.Set(ErrorStackKey, stackTrace)

				// 返回500错误
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code": -1,
					"msg":  "服务器内部错误",
				})
			}
		}()
		c.Next()
	}
}
