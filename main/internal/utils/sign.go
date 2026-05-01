package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	signKeyFile      = "data/sign.key"
	signKeyLength    = 64
	timestampMaxDiff = 5 * time.Minute
	nonceExpiry      = 10 * time.Minute
)

var (
	signKey     []byte
	signKeyOnce sync.Once

	nonceMu    sync.RWMutex
	nonceCache = make(map[string]time.Time)
)

func InitSignKey() error {
	var initErr error
	signKeyOnce.Do(func() {
		keyPath := signKeyFile
		if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
			initErr = err
			return
		}

		if keyData, err := os.ReadFile(keyPath); err == nil && len(keyData) == signKeyLength {
			signKey = keyData
			return
		}

		signKey = make([]byte, signKeyLength)
		if _, err := rand.Read(signKey); err != nil {
			initErr = err
			return
		}

		if err := os.WriteFile(keyPath, signKey, 0600); err != nil {
			initErr = err
			return
		}
	})
	return initErr
}

/* GetSignKey 获取 HMAC 签名密钥 */
func GetSignKey() []byte {
	if signKey == nil {
		InitSignKey()
	}
	return signKey
}

/* SignedData 带签名的请求数据结构（含时间戳、nonce、签名） */
type SignedData struct {
	Timestamp int64           `json:"_t"`
	Nonce     string          `json:"_n"`
	Sign      string          `json:"_s"`
	Data      json.RawMessage `json:"-"`
}

func DeriveSignKey(refreshToken, accessToken, secretToken string) []byte {
	combined := refreshToken + accessToken + secretToken
	h := sha256.Sum256([]byte(combined))
	return h[:]
}

func ParseSignedRequest(decryptedData []byte) (*SignedData, map[string]interface{}, error) {
	return ParseSignedRequestWithKey(decryptedData, nil)
}

func ParseSignedRequestWithKey(decryptedData []byte, signKey []byte) (*SignedData, map[string]interface{}, error) {
	var rawMap map[string]interface{}
	if err := json.Unmarshal(decryptedData, &rawMap); err != nil {
		return nil, nil, errors.New("invalid json format")
	}

	signed := &SignedData{}

	// 时间戳是必须的
	if t, ok := rawMap["_t"].(float64); ok {
		signed.Timestamp = int64(t)
	} else {
		return nil, nil, errors.New("missing timestamp")
	}

	// nonce是必须的
	if n, ok := rawMap["_n"].(string); ok {
		signed.Nonce = n
	} else {
		return nil, nil, errors.New("missing nonce")
	}

	// 签名是必须的
	if s, ok := rawMap["_s"].(string); ok {
		signed.Sign = s
	} else {
		return nil, nil, errors.New("missing sign")
	}

	delete(rawMap, "_t")
	delete(rawMap, "_n")
	delete(rawMap, "_s")

	if err := ValidateTimestamp(signed.Timestamp); err != nil {
		return nil, nil, err
	}

	if err := ValidateNonce(signed.Nonce); err != nil {
		return nil, nil, err
	}

	// 使用派生密钥验证签名
	if signKey != nil {
		if err := ValidateSignWithKey(signed.Timestamp, signed.Nonce, rawMap, signed.Sign, signKey); err != nil {
			return nil, nil, err
		}
	}

	return signed, rawMap, nil
}

func ValidateTimestamp(ts int64) error {
	now := time.Now().UnixMilli()
	diff := now - ts
	if diff < 0 {
		diff = -diff
	}
	if diff > timestampMaxDiff.Milliseconds() {
		return errors.New("timestamp expired")
	}
	return nil
}

func ValidateNonce(nonce string) error {
	if len(nonce) < 8 || len(nonce) > 64 {
		return errors.New("invalid nonce length")
	}

	nonceMu.Lock()
	defer nonceMu.Unlock()

	now := time.Now()
	for k, v := range nonceCache {
		if now.Sub(v) > nonceExpiry {
			delete(nonceCache, k)
		}
	}

	if _, exists := nonceCache[nonce]; exists {
		return errors.New("nonce already used")
	}

	nonceCache[nonce] = now
	return nil
}

func ValidateSign(timestamp int64, nonce string, data map[string]interface{}, sign string) error {
	expected := GenerateSign(timestamp, nonce, data)
	if !hmac.Equal([]byte(expected), []byte(sign)) {
		return errors.New("invalid sign")
	}
	return nil
}

func ValidateSignWithKey(timestamp int64, nonce string, data map[string]interface{}, sign string, key []byte) error {
	expected := GenerateSignWithKey(timestamp, nonce, data, key)
	if !hmac.Equal([]byte(expected), []byte(sign)) {
		return errors.New("invalid sign")
	}
	return nil
}

func GenerateSign(timestamp int64, nonce string, data map[string]interface{}) string {
	return GenerateSignWithKey(timestamp, nonce, data, GetSignKey())
}

/* GenerateSignWithKey 使用指定密钥生成 HMAC-SHA256 签名 */
func GenerateSignWithKey(timestamp int64, nonce string, data map[string]interface{}, key []byte) string {
	sortedData := sortMapToString(data)
	message := fmt.Sprintf("%d%s%s", timestamp, nonce, sortedData)

	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

func sortMapToString(m map[string]interface{}) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		v := m[k]
		if v == nil {
			continue
		}
		var vStr string
		switch val := v.(type) {
		case string:
			vStr = val
		case float64:
			if val == float64(int64(val)) {
				vStr = strconv.FormatInt(int64(val), 10)
			} else {
				vStr = strconv.FormatFloat(val, 'f', -1, 64)
			}
		case bool:
			vStr = strconv.FormatBool(val)
		case map[string]interface{}:
			vStr = sortMapToString(val)
		case []interface{}:
			jsonBytes, _ := json.Marshal(val)
			vStr = string(jsonBytes)
		default:
			jsonBytes, _ := json.Marshal(val)
			vStr = string(jsonBytes)
		}
		parts = append(parts, k+"="+vStr)
	}
	return strings.Join(parts, "&")
}

func ObfuscateResponse(payload *ResponsePayload) map[string]interface{} {
	return map[string]interface{}{
		"_e": true,
		"_p": map[string]interface{}{
			"_i": payload.IV,
			"_d": payload.Data,
		},
	}
}

func EncryptAndObfuscate(data interface{}, aesKey []byte) (map[string]interface{}, error) {
	payload, err := EncryptWithKey(data, aesKey)
	if err != nil {
		return nil, err
	}
	return ObfuscateResponse(payload), nil
}

func GenerateNonce() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
