package apiv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
)

/*
 * 复用控制台 handler 时的两处不兼容，都在这里一次性抹平
 *
 *   1. 控制台 handler 只认 c.Param("id")（CF 的 27 处均如此），不读 body 里的 domain_id；
 *      v1 把路径参数移进 body 后必须回填，否则 handler 会拿到 id=0 直接报「域名不存在」
 *   2. JSONBodyToDecryptedData 用 ShouldBindJSON 读过一次请求体，body 已到 EOF；
 *      控制台 handler（cfRequestBody、UpdateSystemConfig、UpdateUser 等）会再读一次，
 *      不回填就永远是空的，表现为「参数错误」而调用方看不出哪里错
 *
 * 分成两个层级，是因为两者的副作用范围不同：
 *
 *   paramAlias   只回填路径参数 + 还原请求体，不改请求的其他部分，可安全地整组挂载
 *   paramBridge  在此之上把标量入参搬进查询串，供只读 c.Query 的 handler 使用，
 *                但它会改写调用方自己传的查询串，只应挂在确有需要的单个接口上
 *
 * 之所以把 query 搬运降级为按需：全仓 Caller 里读 c.Query 的只有三个
 * （cfString 的兜底分支、UpdateSystemConfig 的 GetDecryptedData 分支、SmartParseRecord），
 * 前两者都先读 body，实际依赖 query 搬运的只有 SmartParseRecord 一处
 */

/* paramAlias 回填路径参数并还原请求体 */
func paramAlias(alias map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		bridgeParams(c, alias)
		c.Next()
	}
}

/*
 * paramBridge 在回填路径参数之外，再把标量入参搬进查询串
 *
 * 已经挂在 CF 与 system 两组上，行为保持不变；新接口除非确有 c.Query 消费点，
 * 否则应优先用 paramAlias
 */
func paramBridge(alias map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if data := bridgeParams(c, alias); data != nil {
			mergeScalarsToQuery(c, data)
		}
		c.Next()
	}
}

/* cfDomainBridge 把 body 里的 domain_id 回填成控制台所需的 :id */
func cfDomainBridge() gin.HandlerFunc {
	return paramBridge(map[string]string{"id": "domain_id"})
}

/* cfAccountBridge 把 body 里的 account_id 回填成控制台所需的 :id */
func cfAccountBridge() gin.HandlerFunc {
	return paramBridge(map[string]string{"id": "account_id"})
}

/* systemParamBridge 把系统管理接口的 body 参数回填到路径参数与查询串 */
func systemParamBridge() gin.HandlerFunc {
	return paramBridge(map[string]string{"id": "log_id"})
}

/*
 * bridgeParams 回填路径参数并还原请求体，返回解析出的 body
 *
 * 只在对应键存在时才动作，不覆盖调用方显式给出的路径参数
 */
func bridgeParams(c *gin.Context, alias map[string]string) map[string]interface{} {
	data := middleware.GetDecryptedData(c)
	if data == nil || len(data) == 0 {
		return nil
	}

	for paramKey, bodyKey := range alias {
		if c.Param(paramKey) != "" {
			continue
		}
		raw, ok := data[bodyKey]
		if !ok {
			continue
		}
		if v := strings.TrimSpace(fmt.Sprint(raw)); v != "" && v != "<nil>" {
			c.Params = append(c.Params, gin.Param{Key: paramKey, Value: v})
		}
	}

	// 还原请求体：原始 JSON 已经不可得，用 decrypted_data 的序列化结果等价替代
	if raw, err := json.Marshal(data); err == nil {
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		c.Request.ContentLength = int64(len(raw))
	}
	return data
}

/* mergeScalarsToQuery 把 body 里的标量搬进查询串，供只读 c.Query 的 handler 使用 */
func mergeScalarsToQuery(c *gin.Context, data map[string]interface{}) {
	query := c.Request.URL.Query()
	for key, raw := range data {
		if query.Get(key) != "" {
			continue
		}
		switch raw.(type) {
		case string, float64, bool, nil:
			if v := strings.TrimSpace(fmt.Sprint(raw)); v != "" && v != "<nil>" {
				query.Set(key, v)
			}
		}
	}
	c.Request.URL.RawQuery = query.Encode()
}
