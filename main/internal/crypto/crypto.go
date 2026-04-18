// Package crypto 提供字段级 AES-256-GCM 加解密，用于敏感凭据（AK/SK、私钥、SSH 口令等）落库前加密。
//
// 密文格式：enc:v1:base64(nonce || ciphertext)
// - 前缀 enc:v1: 用于区分明文与密文，便于启动迁移时原地加密历史明文
// - 主密钥来源于 config.Security.MasterKey，可由环境变量 DNSPLANE_MASTER_KEY 覆盖
// - 主密钥经 SHA-256 派生为 AES-256 密钥
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"

	"main/internal/config"
)

const (
	encPrefix = "enc:v1:"
	keySize   = 32 // AES-256
)

var (
	keyOnce sync.Once
	cached  []byte
)

// deriveKey 派生 AES-256 密钥；主密钥变更后需重启进程刷新。
func deriveKey() []byte {
	keyOnce.Do(func() {
		cfg := config.Get()
		if cfg == nil || strings.TrimSpace(cfg.Security.MasterKey) == "" {
			// 理论不可达：config.Load 已保证非空
			panic("crypto: 加密主密钥 security.master_key 为空，请检查配置加载流程")
		}
		h := sha256.Sum256([]byte(cfg.Security.MasterKey))
		cached = h[:]
	})
	return cached
}

// IsEncrypted 判断给定字符串是否已被本模块加密过。
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, encPrefix)
}

// Encrypt 将明文加密为 enc:v1:... 格式；空串直接返回空串（避免把空列变成密文）。
// 若已经是密文则原样返回，幂等安全。
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if IsEncrypted(plaintext) {
		return plaintext, nil
	}
	block, err := aes.NewCipher(deriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.RawStdEncoding.EncodeToString(ct), nil
}

// Decrypt 将 enc:v1:... 解密回明文；若输入不是密文则原样返回（用于迁移期混合读取）。
func Decrypt(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !IsEncrypted(s) {
		return s, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(s[len(encPrefix):])
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(deriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("crypto: ciphertext too short")
	}
	pt, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// MustDecrypt 与 Decrypt 相同但解密失败时返回原字符串；用于 GORM AfterFind 这种不希望阻断的场景。
func MustDecrypt(s string) string {
	out, err := Decrypt(s)
	if err != nil {
		return s
	}
	return out
}
