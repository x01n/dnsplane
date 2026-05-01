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
func deriveKey() []byte {
	keyOnce.Do(func() {
		cfg := config.Get()
		if cfg == nil || strings.TrimSpace(cfg.Security.MasterKey) == "" {
			panic("crypto: 加密主密钥 security.master_key 为空，请检查配置加载流程")
		}
		h := sha256.Sum256([]byte(cfg.Security.MasterKey))
		cached = h[:]
	})
	return cached
}

func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, encPrefix)
}

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

func MustDecrypt(s string) string {
	out, err := Decrypt(s)
	if err != nil {
		return s
	}
	return out
}
