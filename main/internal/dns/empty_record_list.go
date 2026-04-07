package dns

import (
	"errors"
	"strings"
)

/*
 * TreatAsEmptySubDomainRecordListError
 * 部分厂商在「按子域/条件列举解析记录」且尚无匹配记录时返回业务错误而非空列表，
 * ACME EnsureChallengeRecord 会先 List 再 Create；此类错误应视为 0 条记录。
 * 已单独处理的实现（如 dnspod）仍可与本函数并存。
 */
func TreatAsEmptySubDomainRecordListError(err error) bool {
	if err == nil {
		return false
	}
	markers := []string{
		"ResourceNotFound.NoDataOfRecord", // 腾讯云 DNSPod 等
		"NoDataOfRecord",
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		msg := e.Error()
		for _, m := range markers {
			if strings.Contains(msg, m) {
				return true
			}
		}
	}
	return false
}
