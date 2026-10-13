package apiv1

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
)

/* sprintf 统一格式化入口，避免各文件重复 import fmt */
func sprintf(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

/*
 * V1 统一响应
 *
 * 与控制台的 {code, msg, data} 刻意不同：控制台的 msg 是给人看的中文串、
 * 码值只有 0 与 -1，页面据此分不出错误类别；对外接口需要稳定的机器可读码，
 * 且文案可能随界面调整而变。因此这里固定 message 仅供参考、code 才是契约。
 *
 * request_id 回传请求追踪中间件生成的 ID，便于调用方与我们对照同一次调用
 */
type okBody struct {
	Code      int         `json:"code"`
	RequestID string      `json:"request_id"`
	Data      interface{} `json:"data"`
	/*
	 * Message 只在控制台用 SuccessMsg 写出文案时出现
	 *
	 * 那类响应没有 data 键，文案本身就是全部信息（如「成功添加 3 条」这类计数），
	 * 丢弃会让调用方看不到部分失败。OK() 自己构造的响应不带该字段，
	 * 因此正常接口的响应形状不受影响
	 */
	Message string `json:"message,omitempty"`
}

type errBody struct {
	Code      int    `json:"code"`
	RequestID string `json:"request_id"`
	Message   string `json:"message"`
}

/* requestIDOf 取请求追踪中间件写入的 ID，缺失时为空串而非省略字段 */
func requestIDOf(c *gin.Context) string {
	if v, ok := c.Get(middleware.RequestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

/*
 * OK 成功响应，HTTP 200
 *
 * data 为 nil 时仍然输出 data 字段（值为 null），保持响应结构恒定，
 * 让客户端可以无条件读 resp.data 而不必先判字段是否存在
 */
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, okBody{Code: 0, RequestID: requestIDOf(c), Data: data})
}

/*
 * Fail 错误响应，HTTP 状态由错误码推导
 *
 * message 为空时取该码的默认说明；非空时用调用方给的更具体文案
 * （如「域名 example.com 不存在」），但 code 始终不变
 */
func Fail(c *gin.Context, code int, message string) {
	if message == "" {
		message = messageFor(code)
	}
	c.JSON(httpStatusFor(code), errBody{
		Code:      code,
		RequestID: requestIDOf(c),
		Message:   message,
	})
}

/* Failf 带格式化文案的错误响应 */
func Failf(c *gin.Context, code int, format string, args ...interface{}) {
	Fail(c, code, sprintf(format, args...))
}

/*
 * Page 分页载荷
 *
 * total 是满足条件的总条数（不是本页条数），客户端据此算总页数；
 * 显式回显 page 与 page_size 便于客户端核对服务端实际采用的值——
 * 服务端会对越界值做收敛，回显可避免客户端误以为自己传的值生效了
 */
type Page struct {
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	List     interface{} `json:"list"`
}

/* OKPage 分页成功响应，list 为 nil 时归一化为空数组而非 null */
func OKPage(c *gin.Context, total int64, page, pageSize int, list interface{}) {
	if list == nil {
		list = []interface{}{}
	}
	OK(c, Page{Total: total, Page: page, PageSize: pageSize, List: list})
}
