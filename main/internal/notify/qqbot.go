package notify

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"main/internal/cache"
)
const (
	qqBotTokenURL = "https://api.bot.qq.com/app/getAppAccessToken"
	qqBotAPIBase  = "https://api.bot.qq.com"
)

/* QQBotConfig QQ 机器人（开放平台 API v2）通知配置 */
type QQBotConfig struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
	OpenID    string `json:"open_id"`
}

/* QQBotNotifier QQ 机器人通知发送器（含 access_token 缓存） */
type QQBotNotifier struct {
	config      QQBotConfig
	client      *http.Client
	tokenMu     sync.Mutex
	cachedToken string
	tokenExpire time.Time
	// useCache 控制是否走 cache.C 的跨实例缓存（内存缓存为进程内），测试中关闭以避免污染
	useCache bool
}

func NewQQBotNotifier(config QQBotConfig) *QQBotNotifier {
	return &QQBotNotifier{
		config:   config,
		client:   &http.Client{Timeout: 30 * time.Second},
		useCache: true,
	}
}

/*
 * QQBotSecretToSeed 按官方文档把 Bot Secret repeat 后截取为 32 字节 Ed25519 seed
 */
func QQBotSecretToSeed(secret string) []byte {
	if secret == "" {
		return nil
	}
	seed := secret
	for len(seed) < ed25519.SeedSize {
		seed += seed
	}
	return []byte(seed[:ed25519.SeedSize])
}

/*
 * QQBotPrivateKey 从 AppSecret 派生 Ed25519 私钥
 * @param secret Bot AppSecret
 * @returns 私钥；secret 为空时返回 nil（ed25519.NewKeyFromSeed 对非法 seed 会 panic）
 */
func QQBotPrivateKey(secret string) ed25519.PrivateKey {
	seed := QQBotSecretToSeed(secret)
	if len(seed) != ed25519.SeedSize {
		return nil
	}
	return ed25519.NewKeyFromSeed(seed)
}

/*
 * QQBotSignValidation 回调地址验证（op=13）：对 event_ts + plain_token 签名
 */
func QQBotSignValidation(secret, eventTs, plainToken string) (string, error) {
	if eventTs == "" || plainToken == "" {
		return "", fmt.Errorf("回调校验参数不完整")
	}
	key := QQBotPrivateKey(secret)
	if key == nil {
		return "", fmt.Errorf("QQ机器人AppSecret无效")
	}
	sig := ed25519.Sign(key, []byte(eventTs+plainToken))
	return hex.EncodeToString(sig), nil
}

/*
 * QQBotVerifySignature 校验 HTTP 回调签名
 */
func QQBotVerifySignature(secret, timestamp, signatureHex, body string) bool {
	timestamp = strings.TrimSpace(timestamp)
	signatureHex = strings.ToLower(strings.TrimSpace(signatureHex))
	if timestamp == "" || signatureHex == "" {
		return false
	}
	signature, err := hex.DecodeString(signatureHex)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	if signature[ed25519.SignatureSize-1]&224 != 0 {
		return false
	}
	key := QQBotPrivateKey(secret)
	if key == nil {
		return false
	}
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		return false
	}
	return ed25519.Verify(pub, []byte(timestamp+body), signature)
}

/*
 * QQBotToMarkdown 将站内通知模板转为 QQ Markdown
 */
func QQBotToMarkdown(title, content string) string {
	replacer := strings.NewReplacer("<br/>", "\n", "<b>", "**", "</b>", "**")
	content = replacer.Replace(content)
	content = stripTags(content)
	content = html.UnescapeString(content)
	content = strings.TrimSpace(content)
	return "# " + title + "\n\n" + content
}

/* getAccessToken 获取 access_token（带缓存） */
func (n *QQBotNotifier) getAccessToken(ctx context.Context) (string, error) {
	n.tokenMu.Lock()
	defer n.tokenMu.Unlock()
	if n.cachedToken != "" && time.Now().Before(n.tokenExpire) {
		return n.cachedToken, nil
	}
	sum := sha256.Sum256([]byte(n.config.AppID + ":" + n.config.AppSecret))
	cacheKey := "qqbot_access_token_" + hex.EncodeToString(sum[:8])
	if n.useCache && cache.C != nil {
		if v, ok := cache.C.Get(cacheKey); ok && v != "" {
			n.cachedToken = v
			n.tokenExpire = time.Now().Add(time.Hour)
			return v, nil
		}
	}

	payload := map[string]string{
		"appId":        n.config.AppID,
		"clientSecret": n.config.AppSecret,
	}
	body, _ := json.Marshal(payload)
	// 出站 URL SSRF 防御（安全审计 R-5）
	if err := ValidateOutboundURL(qqBotTokenURL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", qqBotTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取QQ机器人凭证失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		AccessToken string      `json:"access_token"`
		ExpiresIn   interface{} `json:"expires_in"`
		Code        interface{} `json:"code"`
		Message     string      `json:"message"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("获取QQ机器人凭证失败，响应解析失败")
	}
	if result.AccessToken == "" {
		code := fmt.Sprintf("%v", result.Code)
		if result.Code == nil {
			code = ""
		}
		suffix := ""
		if code != "" {
			suffix = "[" + code + "]"
		}
		if result.Message == "" {
			result.Message = "未知错误"
		}
		return "", fmt.Errorf("获取QQ机器人凭证失败%s：%s", suffix, result.Message)
	}
	ttl := qqBotInt(result.ExpiresIn, 7200)
	if ttl < 120 {
		ttl = 120
	}
	ttl -= 120
	if ttl < 60 {
		ttl = 60
	}

	n.cachedToken = result.AccessToken
	n.tokenExpire = time.Now().Add(time.Duration(ttl) * time.Second)
	if n.useCache && cache.C != nil {
		_ = cache.C.Set(cacheKey, result.AccessToken, time.Duration(ttl)*time.Second)
	}
	return result.AccessToken, nil
}

/*
 * qqBotEscapeOpenID 转义 openid 以拼入请求路径
 * @param openID 用户 openid
 * @returns 转义后的字符串
 */
func qqBotEscapeOpenID(openID string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(openID))
	for i := 0; i < len(openID); i++ {
		c := openID[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperhex[c>>4])
		b.WriteByte(upperhex[c&0x0f])
	}
	return b.String()
}

/* qqBotInt 宽容解析 JSON 中可能为数字或字符串的整数字段 */
func qqBotInt(v interface{}, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
	case string:
		var i int
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%d", &i); err == nil {
			return i
		}
	}
	return def
}

/*
 * Send 发送 Markdown 单聊消息到配置的 OpenID
 */
func (n *QQBotNotifier) Send(ctx context.Context, title, content string) error {
	return n.SendMarkdownTo(ctx, n.config.OpenID, QQBotToMarkdown(title, content), "")
}

/*
 * SendMarkdownTo 发送 Markdown 单聊消息到指定 OpenID
 *   POST /v2/users/{openid}/messages，msg_type=2，msgId 非空时附带 msg_seq=1
 */
func (n *QQBotNotifier) SendMarkdownTo(ctx context.Context, openID, markdown, msgID string) error {
	openID = strings.TrimSpace(openID)
	if openID == "" || markdown == "" {
		return fmt.Errorf("QQ机器人消息参数不完整")
	}

	token, err := n.getAccessToken(ctx)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"msg_type": 2,
		"markdown": map[string]string{"content": markdown},
	}
	if msgID != "" {
		payload["msg_id"] = msgID
		payload["msg_seq"] = 1
	}

	body, _ := json.Marshal(payload)
	apiURL := qqBotAPIBase + "/v2/users/" + qqBotEscapeOpenID(openID) + "/messages"
	// 出站 URL SSRF 防御（安全审计 R-5）：openid 来自配置或回调，须校验拼接后的最终地址
	if err := ValidateOutboundURL(apiURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("QQ机器人接口请求失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		ID      string      `json:"id"`
		Code    interface{} `json:"code"`
		Message string      `json:"message"`
		Msg     string      `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("QQ机器人接口响应解析失败: %s", string(respBody))
	}
	if result.ID != "" {
		return nil
	}

	message := result.Message
	if message == "" {
		message = result.Msg
	}
	if message == "" {
		message = "发送失败"
	}
	if result.Code != nil {
		return fmt.Errorf("QQ机器人发送失败[%v]：%s", result.Code, message)
	}
	return fmt.Errorf("QQ机器人发送失败：%s", message)
}
