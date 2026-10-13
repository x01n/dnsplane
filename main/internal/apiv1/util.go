package apiv1

import (
	"encoding/json"
	"strconv"

	"main/internal/api/middleware"
	"main/internal/models"
)

/* itoa 数字 ID 转字符串，各层 SQL 与上下文键统一用字符串形式 */
func itoa(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}

/* domainPermissionOf 取域名权限明细，转发到 middleware 的导出包装 */
func domainPermissionOf(userID, domainID string) (*models.Permission, bool, bool) {
	return middleware.DomainPermissionOf(userID, domainID)
}

/* jsonUnmarshal 统一 JSON 解析入口 */
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
