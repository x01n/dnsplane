package apiv1

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
)

/*
 * V1 请求参数解析
 *
 * 控制台的 handler 一律经 middleware.BindDecryptedData 取参，那个函数优先读
 * decrypted_data（由本包的 JSONBodyToDecryptedData 写入），读不到才回落
 * ShouldBindJSON。v1 全部走前者，语义更确定
 */

/*
 * parsePaging 归一化分页参数
 *
 * 越界值一律收敛而非报错：外部调用方传 page_size=10000 时拒绝会让整个集成失败，
 * 收敛到上限并回显实际值更利于对接。仅当 page/page_size 为负数时才判非法——
 * 那是调用方的逻辑错误，静默纠正会掩盖 bug
 */
const (
	defaultPageSize = 20
	maxPageSize     = 200
)

func parsePaging(c *gin.Context, page, pageSize int) (int, int, bool) {
	if page < 0 || pageSize < 0 {
		Fail(c, CodePageOutOfRange, "")
		c.Abort()
		return 0, 0, false
	}
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize, true
}

/*
 * bindJSON 解析请求体到 out
 *
 * 错误一律归为 CodeBodyUnreadable：具体是字段类型不符还是 JSON 语法错误，
 * 对调用方的处置方式相同，细分码值只会让客户端多写分支
 */
func bindJSON(c *gin.Context, out interface{}) bool {
	if err := middleware.BindDecryptedData(c, out); err != nil {
		Failf(c, CodeBodyUnreadable, "请求体解析失败: %s", err.Error())
		c.Abort()
		return false
	}
	return true
}

/*
 * requireString 校验必填字符串参数
 *
 * field 用对外文档里的参数名（英文），不写中文：调用方要据此定位自己少传了哪个字段
 */
func requireString(c *gin.Context, value, field string) bool {
	if strings.TrimSpace(value) == "" {
		Failf(c, CodeParamMissing, "缺少必填参数 %s", field)
		c.Abort()
		return false
	}
	return true
}

/* parseUintParam 解析路径或体里的数字 ID，非法时报参数错误 */
func parseUintParam(c *gin.Context, raw, field string) (uint, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || v == 0 {
		Failf(c, CodeParamInvalid, "参数 %s 必须是正整数，收到 %q", field, raw)
		c.Abort()
		return 0, false
	}
	return uint(v), true
}
