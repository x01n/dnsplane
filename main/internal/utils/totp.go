package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

/* TOTPConfig TOTP 二步验证配置参数 */
type TOTPConfig struct {
	Secret    string
	Issuer    string
	Account   string
	Period    int
	Digits    int
	Algorithm string
}

/* GenerateTOTPSecret 生成 20 字节随机 TOTP 密钥（Base32 编码） */
func GenerateTOTPSecret() (string, error) {
	secret := make([]byte, 20)
	_, err := rand.Read(secret)
	if err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

/* GenerateTOTPURI 生成 TOTP URI（用于二维码扫码添加） */
func GenerateTOTPURI(config TOTPConfig) string {
	if config.Period == 0 {
		config.Period = 30
	}
	if config.Digits == 0 {
		config.Digits = 6
	}
	if config.Algorithm == "" {
		config.Algorithm = "SHA1"
	}

	v := url.Values{}
	v.Set("secret", config.Secret)
	v.Set("issuer", config.Issuer)
	v.Set("period", fmt.Sprintf("%d", config.Period))
	v.Set("digits", fmt.Sprintf("%d", config.Digits))
	v.Set("algorithm", config.Algorithm)

	u := url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/" + url.PathEscape(config.Issuer) + ":" + url.PathEscape(config.Account),
		RawQuery: v.Encode(),
	}

	return u.String()
}

/* VerifyTOTPCode 验证 TOTP 验证码（允许前后 1 个时间窗口偏移） */
func VerifyTOTPCode(secret, code string) bool {
	// 去除空格并统一大写
	secret = strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	code = strings.TrimSpace(code)

	// 验证当前时间窗口和前后各一个时间窗口
	currentTime := time.Now().Unix()
	for _, offset := range []int64{-30, 0, 30} {
		expectedCode := generateTOTPCode(secret, currentTime+offset)
		if expectedCode == code {
			return true
		}
	}
	return false
}

/* generateTOTPCode 根据密钥和时间戳生成 6 位 TOTP 验证码 */
func generateTOTPCode(secret string, timestamp int64) string {
	// 解码密钥
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		// 尝试带填充的解码
		key, err = base32.StdEncoding.DecodeString(secret)
		if err != nil {
			return ""
		}
	}

	// 计算时间计数器 (30秒为周期)
	counter := uint64(timestamp / 30)

	// 将计数器转换为8字节大端序
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, counter)

	// HMAC-SHA1
	h := hmac.New(sha1.New, key)
	h.Write(counterBytes)
	hash := h.Sum(nil)

	// 动态截取
	offset := hash[len(hash)-1] & 0x0f
	truncatedHash := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff

	// 取模得到6位数字
	otp := truncatedHash % 1000000

	return fmt.Sprintf("%06d", otp)
}

/* GetCurrentTOTPCode 获取当前时间的 TOTP 码（仅用于测试） */
func GetCurrentTOTPCode(secret string) string {
	secret = strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	return generateTOTPCode(secret, time.Now().Unix())
}

/* ValidateTOTPSecret 验证 TOTP 密钥格式有效性（长度 + Base32 合法性） */
func ValidateTOTPSecret(secret string) bool {
	secret = strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	if len(secret) < 16 {
		return false
	}

	_, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		_, err = base32.StdEncoding.DecodeString(secret)
		return err == nil
	}
	return true
}

/* GenerateRecoveryCodes 生成指定数量的 TOTP 恢复码（XXXXX-XXXXX 格式） */
func GenerateRecoveryCodes(count int) []string {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		b := make([]byte, 5)
		rand.Read(b)
		// 生成10位恢复码，格式: XXXXX-XXXXX
		code := fmt.Sprintf("%05X-%05X", binary.BigEndian.Uint32(append([]byte{0}, b[:3]...))&0xFFFFF, binary.BigEndian.Uint32(append([]byte{0}, b[2:]...))&0xFFFFF)
		codes[i] = code
	}
	return codes
}

/* VerifyRecoveryCode 验证并消耗恢复码（一次性使用） */
func VerifyRecoveryCode(storedCodes []string, inputCode string) (bool, []string) {
	inputCode = strings.ToUpper(strings.ReplaceAll(inputCode, " ", ""))
	for i, code := range storedCodes {
		if code == inputCode {
			// 移除已使用的恢复码
			remainingCodes := append(storedCodes[:i], storedCodes[i+1:]...)
			return true, remainingCodes
		}
	}
	return false, storedCodes
}
