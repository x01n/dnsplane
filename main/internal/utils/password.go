package utils

import (
	"unicode"
)

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
