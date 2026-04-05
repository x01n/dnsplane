package verify

import (
	"crypto/rand"
	"fmt"
	"main/internal/cache"
	"strings"
	"time"
)

// 验证码字符集（排除易混淆字符 0O1IL）
const charset = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
const codeLength = 8
const codeTTL = 5 * time.Minute   // 验证码有效期
const sendInterval = 60 * time.Second // 同一邮箱发送间隔
const maxAttempts = 5               // 最大错误尝试次数

// 场景枚举
const (
	SceneRegister       = "register"
	SceneBindMail       = "bindmail"
	SceneForgotPassword = "forgot_password"
	SceneForgotTOTP     = "forgot_totp"
)

// codeEntry 缓存中的验证码数据
type codeEntry struct {
	Code      string `json:"code"`
	Attempts  int    `json:"attempts"`
	CreatedAt int64  `json:"created_at"`
}

// Generate 生成验证码并存储到缓存
// 返回验证码字符串，或错误（如发送过于频繁）
func Generate(email, scene string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	key := codeKey(email, scene)

	// 检查发送间隔
	var existing codeEntry
	if cache.C.GetJSON(key, &existing) {
		elapsed := time.Since(time.Unix(existing.CreatedAt, 0))
		if elapsed < sendInterval {
			remaining := int((sendInterval - elapsed).Seconds())
			return "", fmt.Errorf("发送过于频繁，请 %d 秒后重试", remaining)
		}
	}

	// 生成 8 位随机验证码
	code := generateRandomCode(codeLength)

	// 存储到缓存
	entry := codeEntry{
		Code:      code,
		Attempts:  0,
		CreatedAt: time.Now().Unix(),
	}
	if err := cache.C.SetJSON(key, entry, codeTTL); err != nil {
		return "", fmt.Errorf("存储验证码失败: %w", err)
	}

	return code, nil
}

// Verify 校验验证码（成功后自动删除）
func Verify(email, scene, code string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	code = strings.ToUpper(strings.TrimSpace(code))
	key := codeKey(email, scene)

	var entry codeEntry
	if !cache.C.GetJSON(key, &entry) {
		return fmt.Errorf("验证码不存在或已过期，请重新获取")
	}

	// 检查尝试次数
	if entry.Attempts >= maxAttempts {
		cache.C.Delete(key)
		return fmt.Errorf("错误次数过多，验证码已失效，请重新获取")
	}

	// 校验
	if strings.ToUpper(entry.Code) != code {
		// 增加错误计数
		entry.Attempts++
		remaining := codeTTL - time.Since(time.Unix(entry.CreatedAt, 0))
		if remaining > 0 {
			cache.C.SetJSON(key, entry, remaining)
		}
		return fmt.Errorf("验证码错误，还可尝试 %d 次", maxAttempts-entry.Attempts)
	}

	// 验证成功，删除验证码
	cache.C.Delete(key)
	return nil
}

// ==================== 工具 ====================

func codeKey(email, scene string) string {
	return fmt.Sprintf("vcode:%s:%s", scene, email)
}

func generateRandomCode(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	code := make([]byte, length)
	for i := range code {
		code[i] = charset[int(b[i])%len(charset)]
	}
	return string(code)
}

// CheckIPLimit 检查 IP 发送频率限制
// 返回 nil 表示未超限，否则返回错误
func CheckIPLimit(ip string, maxPerHour int64) error {
	key := fmt.Sprintf("vcode:ip:%s", ip)
	count, err := cache.C.Incr(key, 1*time.Hour)
	if err != nil {
		return nil // 缓存失败不阻塞
	}
	if count > maxPerHour {
		return fmt.Errorf("请求过于频繁，请稍后重试")
	}
	return nil
}
