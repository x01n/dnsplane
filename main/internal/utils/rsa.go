package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const customBase62 = "9Kp2LmNqRs4TuVw6XyZ0AaBbCcDdEeFfGgHhIiJj1k3l5MnOoPQr7StUvWxYz8-_"

func customEncode(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var result strings.Builder
	zeros := 0
	for _, b := range data {
		if b == 0 {
			zeros++
		} else {
			break
		}
	}
	for i := 0; i < len(data); i += 3 {
		var chunk uint32
		remaining := len(data) - i
		if remaining >= 3 {
			chunk = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
			result.WriteByte(customBase62[(chunk>>18)&0x3F])
			result.WriteByte(customBase62[(chunk>>12)&0x3F])
			result.WriteByte(customBase62[(chunk>>6)&0x3F])
			result.WriteByte(customBase62[chunk&0x3F])
		} else if remaining == 2 {
			chunk = uint32(data[i])<<16 | uint32(data[i+1])<<8
			result.WriteByte(customBase62[(chunk>>18)&0x3F])
			result.WriteByte(customBase62[(chunk>>12)&0x3F])
			result.WriteByte(customBase62[(chunk>>6)&0x3F])
		} else {
			chunk = uint32(data[i]) << 16
			result.WriteByte(customBase62[(chunk>>18)&0x3F])
			result.WriteByte(customBase62[(chunk>>12)&0x3F])
		}
	}

	return result.String()
}

/* 预构建解码映射表，避免每次调用 customDecode 时重复创建 */
var customDecodeMap = func() map[byte]uint32 {
	m := make(map[byte]uint32, len(customBase62))
	for i, c := range customBase62 {
		m[byte(c)] = uint32(i)
	}
	return m
}()

func customDecode(s string) ([]byte, error) {
	if len(s) == 0 {
		return []byte{}, nil
	}
	decodeMap := customDecodeMap

	var result []byte
	for i := 0; i < len(s); i += 4 {
		var chunk uint32
		remaining := len(s) - i

		if remaining >= 4 {
			v0, ok0 := decodeMap[s[i]]
			v1, ok1 := decodeMap[s[i+1]]
			v2, ok2 := decodeMap[s[i+2]]
			v3, ok3 := decodeMap[s[i+3]]
			if !ok0 || !ok1 || !ok2 || !ok3 {
				return nil, errors.New("invalid character in encoded string")
			}
			chunk = v0<<18 | v1<<12 | v2<<6 | v3
			result = append(result, byte(chunk>>16), byte(chunk>>8), byte(chunk))
		} else if remaining == 3 {
			v0, ok0 := decodeMap[s[i]]
			v1, ok1 := decodeMap[s[i+1]]
			v2, ok2 := decodeMap[s[i+2]]
			if !ok0 || !ok1 || !ok2 {
				return nil, errors.New("invalid character in encoded string")
			}
			chunk = v0<<18 | v1<<12 | v2<<6
			result = append(result, byte(chunk>>16), byte(chunk>>8))
		} else if remaining == 2 {
			v0, ok0 := decodeMap[s[i]]
			v1, ok1 := decodeMap[s[i+1]]
			if !ok0 || !ok1 {
				return nil, errors.New("invalid character in encoded string")
			}
			chunk = v0<<18 | v1<<12
			result = append(result, byte(chunk>>16))
		}
	}

	return result, nil
}

var (
	rsaPrivateKey *rsa.PrivateKey
	rsaPublicKey  string
	rsaOnce       sync.Once
)

const (
	rsaKeyDir      = "data"
	rsaPrivateFile = "private.pem"
	rsaPublicFile  = "public.pem"
	rsaKeyBits     = 4096
)

/* EncryptedPayload RSA+AES 混合加密请求载荷 */
type EncryptedPayload struct {
	Key  string `json:"key"`
	IV   string `json:"iv"`
	Data string `json:"data"`
}

func InitRSAKey() error {
	var initErr error
	rsaOnce.Do(func() {
		if err := os.MkdirAll(rsaKeyDir, 0755); err != nil {
			initErr = err
			return
		}

		privatePath := filepath.Join(rsaKeyDir, rsaPrivateFile)
		publicPath := filepath.Join(rsaKeyDir, rsaPublicFile)

		/* 尝试加载已有密钥 */
		if privateData, err := os.ReadFile(privatePath); err == nil {
			if publicData, err := os.ReadFile(publicPath); err == nil {
				block, _ := pem.Decode(privateData)
				if block != nil {
					if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
						if key.N.BitLen() == rsaKeyBits {
							rsaPrivateKey = key
							rsaPublicKey = string(publicData)
							return
						}
					}
				}
			}
		}

		/* 生成新的4096位密钥对 */
		var err error
		rsaPrivateKey, err = rsa.GenerateKey(rand.Reader, rsaKeyBits)
		if err != nil {
			initErr = err
			return
		}

		privateKeyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(rsaPrivateKey),
		})

		pubKeyBytes, err := x509.MarshalPKIXPublicKey(&rsaPrivateKey.PublicKey)
		if err != nil {
			initErr = err
			return
		}
		publicKeyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubKeyBytes,
		})
		rsaPublicKey = string(publicKeyPEM)

		if err := os.WriteFile(privatePath, privateKeyPEM, 0600); err != nil {
			initErr = err
			return
		}
		if err := os.WriteFile(publicPath, publicKeyPEM, 0644); err != nil {
			initErr = err
			return
		}
	})
	return initErr
}

func GetRSAPublicKey() string {
	if rsaPublicKey == "" {
		InitRSAKey()
	}
	return rsaPublicKey
}

/* DecryptResult 解密结果，包含明文和 AES 密钥 */
type DecryptResult struct {
	Plaintext []byte
	AESKey    []byte
}

/* HybridDecrypt 混合解密：RSA解密AES密钥，AES解密数据 */
func HybridDecrypt(payload *EncryptedPayload) ([]byte, error) {
	result, err := HybridDecryptWithKey(payload)
	if err != nil {
		return nil, err
	}
	return result.Plaintext, nil
}

/* HybridDecryptWithKey 混合解密并返回AES密钥（用于加密响应） */
func HybridDecryptWithKey(payload *EncryptedPayload) (*DecryptResult, error) {
	if rsaPrivateKey == nil {
		InitRSAKey()
	}

	/* 1. 自定义解码 */
	encryptedKey, err := customDecode(payload.Key)
	if err != nil {
		return nil, errors.New("invalid key format: " + err.Error())
	}
	iv, err := customDecode(payload.IV)
	if err != nil {
		return nil, errors.New("invalid iv format: " + err.Error())
	}
	encryptedData, err := customDecode(payload.Data)
	if err != nil {
		return nil, errors.New("invalid data format: " + err.Error())
	}

	/* 2. RSA-OAEP解密AES密钥 */
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, rsaPrivateKey, encryptedKey, nil)
	if err != nil {
		return nil, errors.New("decrypt key failed: " + err.Error())
	}

	/* 3. AES-GCM解密数据 */
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, errors.New("create cipher failed")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("create gcm failed")
	}

	plaintext, err := gcm.Open(nil, iv, encryptedData, nil)
	if err != nil {
		return nil, errors.New("decrypt data failed")
	}

	return &DecryptResult{
		Plaintext: plaintext,
		AESKey:    aesKey,
	}, nil
}

/* HybridEncrypt 混合加密：生成AES密钥加密数据，RSA加密AES密钥 */
func HybridEncrypt(data []byte, publicKeyPEM string) (*EncryptedPayload, error) {
	/* 解析公钥 */
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, errors.New("invalid public key")
	}
	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := pubInterface.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("invalid public key type")
	}

	/* 1. 生成随机AES密钥(256位) */
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		return nil, err
	}

	/* 2. AES-GCM加密数据 */
	cipherBlock, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(cipherBlock)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	encryptedData := gcm.Seal(nil, iv, data, nil)

	/* 3. RSA-OAEP加密AES密钥 */
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, aesKey, nil)
	if err != nil {
		return nil, err
	}

	return &EncryptedPayload{
		Key:  customEncode(encryptedKey),
		IV:   customEncode(iv),
		Data: customEncode(encryptedData),
	}, nil
}

/* ServerEncrypt 服务端加密响应（使用自己的公钥） */
func ServerEncrypt(data interface{}) (*EncryptedPayload, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return HybridEncrypt(jsonData, GetRSAPublicKey())
}

/* ResponsePayload AES 加密响应载荷（IV + 密文） */
type ResponsePayload struct {
	IV   string `json:"iv"`
	Data string `json:"data"`
}

/* EncryptWithKey 使用指定的AES密钥加密响应数据 */
func EncryptWithKey(data interface{}, aesKey []byte) (*ResponsePayload, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	// AES-GCM加密
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	encryptedData := gcm.Seal(nil, iv, jsonData, nil)

	return &ResponsePayload{
		IV:   customEncode(iv),
		Data: customEncode(encryptedData),
	}, nil
}

/* RSADecrypt 简单 RSA-OAEP 解密（仅用于小数据如密码） */
func RSADecrypt(ciphertext string) (string, error) {
	if rsaPrivateKey == nil {
		InitRSAKey()
	}
	cipherBytes, err := customDecode(ciphertext)
	if err != nil {
		return "", err
	}
	plainBytes, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, rsaPrivateKey, cipherBytes, nil)
	if err != nil {
		return "", err
	}
	return string(plainBytes), nil
}
