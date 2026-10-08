package handler

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
	"main/internal/notify"
	"main/internal/sysconfig"

	"github.com/gin-gonic/gin"
)

/*
 * QQ 机器人 Webhook 回调测试
 */

const (
	qqBotTestAppID     = "102000001"
	qqBotTestAppSecret = "test-secret-value"
)

/* setupQQBotTest 初始化内存数据库与 sysconfig 缓存，并写入机器人配置 */
func setupQQBotTest(t *testing.T, openID string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	initQQBotTestEnv(t)

	for k, v := range map[string]string{
		"qqbot_appid":     qqBotTestAppID,
		"qqbot_appsecret": qqBotTestAppSecret,
		"qqbot_openid":    openID,
		"site_name":       "测试站点",
	} {
		setQQBotTestConfig(t, k, v)
	}
}

/* callQQBotWebhook 直接调用 handler，返回响应记录器 */
func callQQBotWebhook(t *testing.T, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/qqbot/webhook", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	QQBotWebhook(c)
	return w
}

/* TestQQBotWebhookNotConfigured 未配置 appid/appsecret 时返回 400 */
func TestQQBotWebhookNotConfigured(t *testing.T) {
	initQQBotTestEnv(t)

	w := callQQBotWebhook(t, []byte(`{"op":13,"d":{}}`), nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("未配置时应返回 400，实际 %d body=%s", w.Code, w.Body.String())
	}
}

/* TestQQBotWebhookBadJSON 非 JSON 报文返回 400 */
func TestQQBotWebhookBadJSON(t *testing.T) {
	setupQQBotTest(t, "")
	w := callQQBotWebhook(t, []byte(`not json`), nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("非 JSON 应返回 400，实际 %d", w.Code)
	}
}

/* TestQQBotWebhookAppIDMismatch 声明了不匹配的 x-bot-appid 时返回 403 */
func TestQQBotWebhookAppIDMismatch(t *testing.T) {
	setupQQBotTest(t, "")
	w := callQQBotWebhook(t, []byte(`{"op":13,"d":{"event_ts":"1","plain_token":"t"}}`),
		map[string]string{"x-bot-appid": "other-app"})
	if w.Code != http.StatusForbidden {
		t.Errorf("appid 不匹配应返回 403，实际 %d", w.Code)
	}
}

/* TestQQBotWebhookOp13SignValidation op=13 时返回 plain_token 与 signature，且不要求验签头 */
func TestQQBotWebhookOp13SignValidation(t *testing.T) {
	setupQQBotTest(t, "")
	const eventTs, plainToken = "1728000000", "Arq0ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	body, _ := json.Marshal(map[string]interface{}{
		"op": 13,
		"d":  map[string]string{"event_ts": eventTs, "plain_token": plainToken},
	})

	w := callQQBotWebhook(t, body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("op=13 应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}

	var got struct {
		PlainToken string `json:"plain_token"`
		Signature  string `json:"signature"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应解析失败: %v body=%s", err, w.Body.String())
	}
	if got.PlainToken != plainToken {
		t.Errorf("plain_token 不符: %q", got.PlainToken)
	}
	want, err := notify.QQBotSignValidation(qqBotTestAppSecret, eventTs, plainToken)
	if err != nil {
		t.Fatalf("生成期望签名失败: %v", err)
	}
	if got.Signature != want {
		t.Errorf("signature 不符: got=%s want=%s", got.Signature, want)
	}
	if _, err := hex.DecodeString(got.Signature); err != nil {
		t.Errorf("signature 应为 hex: %v", err)
	}
}

/* TestQQBotWebhookOp13MissingParams op=13 参数不完整时应返回 500 */
func TestQQBotWebhookOp13MissingParams(t *testing.T) {
	setupQQBotTest(t, "")
	w := callQQBotWebhook(t, []byte(`{"op":13,"d":{"event_ts":"","plain_token":""}}`), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("参数不完整应返回 500，实际 %d body=%s", w.Code, w.Body.String())
	}
}

/* TestQQBotWebhookRejectsBadSignature 普通事件缺少有效签名时返回 401 */
func TestQQBotWebhookRejectsBadSignature(t *testing.T) {
	setupQQBotTest(t, "")
	body := []byte(`{"t":"C2C_MESSAGE_CREATE","d":{"id":"m1","author":{"user_openid":"openid_new"}}}`)

	// 无签名头
	if w := callQQBotWebhook(t, body, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("无签名头应返回 401，实际 %d", w.Code)
	}
	// 伪造签名
	w := callQQBotWebhook(t, body, map[string]string{
		"x-signature-timestamp": "1728000000",
		"x-signature-ed25519":   "00" + hex.EncodeToString(make([]byte, 63)),
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("伪造签名应返回 401，实际 %d", w.Code)
	}
}

/* TestQQBotWebhookVerifiesSignatureOverRawBody 验签对象必须是 timestamp + 原始报文 */
func TestQQBotWebhookVerifiesSignatureOverRawBody(t *testing.T) {
	setupQQBotTest(t, "")
	const ts = "1728000000"
	body := []byte(`{"t":"OTHER_EVENT","d":{}}`)

	// 用同一 body 生成正确签名 → 通过
	sig := signQQBotBody(t, ts, string(body))
	w := callQQBotWebhook(t, body, map[string]string{
		"x-signature-timestamp": ts,
		"x-signature-ed25519":   sig,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("正确签名应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}

	// 报文被改动后同一签名必须失败（说明验签覆盖了完整原始报文）
	other := []byte(`{"t":"OTHER_EVENT","d":{"x":1}}`)
	w = callQQBotWebhook(t, other, map[string]string{
		"x-signature-timestamp": ts,
		"x-signature-ed25519":   sig,
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("报文被篡改应返回 401，实际 %d", w.Code)
	}
}

/* TestQQBotWebhookMessagesNotParsedAsFloat 确认 op 字段判定不受 JSON 数字/字符串形态影响 */
func TestQQBotWebhookMessagesNotParsedAsFloat(t *testing.T) {
	setupQQBotTest(t, "")
	// op 以数字形式给出
	body, _ := json.Marshal(map[string]interface{}{
		"op": 13,
		"d":  map[string]string{"event_ts": "1", "plain_token": "tok"},
	})
	w := callQQBotWebhook(t, body, nil)
	if w.Code != http.StatusOK {
		t.Errorf("op 为 JSON 数字 13 时应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}
}

/* signQQBotBody 用测试密钥对 timestamp+body 生成 hex 签名 */
func signQQBotBody(t *testing.T, ts, body string) string {
	t.Helper()
	sig, err := notify.QQBotSignValidation(qqBotTestAppSecret, ts, body)
	if err != nil {
		t.Fatalf("生成签名失败: %v", err)
	}
	return sig
}

/* initQQBotTestEnv 初始化内存 SQLite 与内存缓存；sysconfig 依赖 cache.C 非空 */
func initQQBotTestEnv(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(dir, "test.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

/* setQQBotTestConfig 写入 sysconfig 并校验可读回，避免配置静默丢失导致用例误判 */
func setQQBotTestConfig(t *testing.T, key, value string) {
	t.Helper()
	var cfg models.SysConfig
	if err := database.DB.Where("`key` = ?", key).First(&cfg).Error; err != nil {
		if err := database.DB.Create(&models.SysConfig{Key: key, Value: value}).Error; err != nil {
			t.Fatalf("写入配置 %s 失败: %v", key, err)
		}
	} else {
		if err := database.DB.Model(&cfg).Update("value", value).Error; err != nil {
			t.Fatalf("更新配置 %s 失败: %v", key, err)
		}
	}
	sysconfig.Invalidate(key)
	if got := sysconfig.GetValue(key); got != value {
		t.Fatalf("配置 %s 读回不符: got=%q want=%q", key, got, value)
	}
}
