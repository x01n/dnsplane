package utils

import (
	"unicode"
)

/*
 * ValidatePasswordStrength 密码强度校验
 * 功能：检查密码是否满足最低复杂度要求
 * 规则：
 *   - 最少 8 个字符
 *   - 至少包含一个大写字母
 *   - 至少包含一个小写字母
 *   - 至少包含一个数字
 * 返回空字符串表示通过，否则返回具体不满足的提示
 */
func ValidatePasswordStrength(password string) string {
	if len(password) < 8 {
		return "密码长度至少8位"
	}

	var hasUpper, hasLower, hasDigit bool
	for _, ch := range password {
		switch {
		case unicode.IsUpper(ch):
			hasUpper = true
		case unicode.IsLower(ch):
			hasLower = true
		case unicode.IsDigit(ch):
			hasDigit = true
		}
	}

	if !hasUpper {
		return "密码需包含至少一个大写字母"
	}
	if !hasLower {
		return "密码需包含至少一个小写字母"
	}
	if !hasDigit {
		return "密码需包含至少一个数字"
	}

	return ""
}
