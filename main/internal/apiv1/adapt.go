package apiv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

/*
 * 响应格式适配
 *
 * v1 复用控制台的 handler，那些 handler 用 middleware.SuccessResponse 等写出
 * {code, msg, data} 形状——码值只有 0 与 -1，且没有 request_id。对外接口需要
 * 稳定的机器可读码，故在中间件层拦一次：缓冲 handler 的写出，解析后重新编码成
 * v1 格式，调用方始终只看到 v1 契约
 *
 * 选择拦截而非逐个改写 handler，是因为后者要动 20 多个控制台函数，
 * 而它们同时服务控制台，改动风险与收益不成比例
 */
type adapterWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *adapterWriter) Write(b []byte) (int, error)       { return w.body.Write(b) }
func (w *adapterWriter) WriteString(s string) (int, error) { return w.body.WriteString(s) }

/* AdaptResponse 把控制台格式的控制台响应改写为 v1 格式 */
func AdaptResponse() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 跳过预检等无体请求，避免给 204 强塞 JSON
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		orig := c.Writer
		buf := &bytes.Buffer{}
		c.Writer = &adapterWriter{ResponseWriter: orig, body: buf}

		c.Next()

		// 恢复真实 writer 后再写出，否则适配后的内容仍会落进缓冲
		c.Writer = orig

		// 认证/参数阶段的中间件已直接写 v1 格式，此时缓冲为空，不重复处理
		raw := buf.Bytes()
		if len(raw) == 0 {
			return
		}

		status := orig.Status()
		/*
		 * 先判是不是 v1 自己的响应
		 *
		 * v1 的 handler 直接调 Fail/OK 时也会落进这块缓冲，若按控制台格式解析，
		 * 其 message 字段会被当成 msg 读到空串，进而被重新映射成参数错误——
		 * 余额的鉴权拒绝会被改写成 2001。request_id 是 v1 响应独有的标记，据此区分
		 */
		if bytes.Contains(raw, []byte(`"request_id"`)) {
			orig.WriteHeader(status)
			_, _ = orig.Write(raw)
			return
		}

		var console struct {
			Code int         `json:"code"`
			Msg  string      `json:"msg"`
			Data interface{} `json:"data"`
		}
		if err := json.Unmarshal(raw, &console); err != nil {
			// 不是预期的 JSON 形状（如 404 页面、panic 堆栈），原样透出，避免把错误掩盖成格式问题
			orig.WriteHeader(status)
			_, _ = orig.Write(raw)
			return
		}

		if console.Code == 0 {
			/*
			 * 控制台的成功响应有两种形状，必须分别保留信息：
			 *   SuccessResponse → {code, data}
			 *   SuccessMsg      → {code, msg}    只有文案、没有 data
			 * 早期只取 data，导致后者一律变成 data:null，而「成功添加 3 条」这类
			 * 计数文案恰恰是调用方判断部分失败的唯一线索，丢掉就没法复盘
			 *
			 * msg 与 v1 的 message 语义一致（都是给人的说明、不是契约），
			 * 因此直接落到同一个字段；错误分支的 message 复用同一结构，不额外区分
			 */
			body := okBody{
				Code:      0,
				RequestID: requestIDOf(c),
				Data:      console.Data,
			}
			if strings.TrimSpace(console.Msg) != "" {
				body.Message = console.Msg
			}
			c.JSON(status, body)
			return
		}

		code := mapConsoleError(console.Msg)
		c.JSON(httpStatusFor(code), errBody{
			Code:      code,
			RequestID: requestIDOf(c),
			Message:   console.Msg,
		})
	}
}

/*
 * mapConsoleError 把控制台的中文错误串映射到 v1 错误码
 *
 * 控制台所有错误都是 code=-1，只能按文案反推类别。这是一层有损映射：
 * 未收录的文案落到 CodeParamInvalid，因为控制台最常见的失败就是参数类。
 * 新增 v1 专用 handler 时应直接调用 Fail 给出准确码值，不依赖本函数
 */
func mapConsoleError(msg string) int {
	switch {
	case strings.Contains(msg, "无权限") || strings.Contains(msg, "仅管理员") ||
		strings.Contains(msg, "无权操作") || strings.Contains(msg, "权限不足"):
		return CodeDomainDenied
	case strings.Contains(msg, "只读"):
		return CodeReadOnly
	case strings.Contains(msg, "不存在") && strings.Contains(msg, "域名"):
		return CodeDomainNotFound
	case strings.Contains(msg, "不存在") && strings.Contains(msg, "账户"):
		return CodeAccountNotFound
	case strings.Contains(msg, "不存在") && strings.Contains(msg, "任务"):
		return CodeTaskNotFound
	case strings.Contains(msg, "不存在"):
		return CodeResourceNotFound
	case strings.Contains(msg, "已存在") || strings.Contains(msg, "重复"):
		return CodeResourceConflict
	case strings.Contains(msg, "账户配置"):
		return CodeAccountConfigBad
	case strings.Contains(msg, "不支持"):
		return CodeProviderUnsupported
	case strings.Contains(msg, "超时"):
		return CodeUpstreamTimeout
	case strings.Contains(msg, "参数") || strings.Contains(msg, "缺少") || strings.Contains(msg, "不能为空"):
		return CodeParamInvalid
	default:
		return CodeParamInvalid
	}
}
