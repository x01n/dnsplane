package notify

import (
	"context"
	"reflect"
	"testing"
)

/*
 * LoadNotifiersFromConfig 装载测试
 * 验证新增渠道（WxPusher / QQ 机器人）以及 Telegram、Webhook 新增配置项能被正确装配
 */

/* loadedTypes 返回 manager 中各 notifier 的具体类型名，便于按类型断言 */
func loadedTypes(m *NotifyManager) map[string]int {
	out := make(map[string]int)
	for _, n := range m.notifiers {
		out[reflect.TypeOf(n).Elem().Name()]++
	}
	return out
}

/* TestLoadNotifiersNewChannels WxPusher 与 QQ 机器人应能被配置装载 */
func TestLoadNotifiersNewChannels(t *testing.T) {
	m := NewManager()
	LoadNotifiersFromConfig(m, map[string]string{
		"wechat_apptoken": "AT_token",
		"wechat_appuid":   "UID_user",
		"qqbot_appid":     "102000001",
		"qqbot_appsecret": "secret",
		"qqbot_openid":    "openid_1",
	})

	types := loadedTypes(m)
	if types["WxPusherNotifier"] != 1 {
		t.Errorf("WxPusherNotifier 应装载 1 个，实际 %d", types["WxPusherNotifier"])
	}
	if types["QQBotNotifier"] != 1 {
		t.Errorf("QQBotNotifier 应装载 1 个，实际 %d", types["QQBotNotifier"])
	}

	// 逐项确认装配进 notifier 的配置值正确
	for _, n := range m.notifiers {
		switch v := n.(type) {
		case *WxPusherNotifier:
			if v.config.AppToken != "AT_token" || v.config.UID != "UID_user" {
				t.Errorf("WxPusher 配置装配有误: %+v", v.config)
			}
		case *QQBotNotifier:
			if v.config.AppID != "102000001" || v.config.AppSecret != "secret" || v.config.OpenID != "openid_1" {
				t.Errorf("QQ 机器人配置装配有误: %+v", v.config)
			}
		}
	}
}

/* TestLoadNotifiersNewChannelsRequireAllKeys 缺任一必需键时不应装载对应渠道 */
func TestLoadNotifiersNewChannelsRequireAllKeys(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]string
		want   string
	}{
		{"WxPusher 缺 uid", map[string]string{"wechat_apptoken": "AT"}, ""},
		{"WxPusher 缺 token", map[string]string{"wechat_appuid": "UID"}, ""},
		{"QQ 缺 openid", map[string]string{"qqbot_appid": "a", "qqbot_appsecret": "s"}, ""},
		{"QQ 缺 secret", map[string]string{"qqbot_appid": "a", "qqbot_openid": "o"}, ""},
		{"QQ 缺 appid", map[string]string{"qqbot_appsecret": "s", "qqbot_openid": "o"}, ""},
	}
	for _, c := range cases {
		m := NewManager()
		LoadNotifiersFromConfig(m, c.config)
		types := loadedTypes(m)
		if types["WxPusherNotifier"] != 0 || types["QQBotNotifier"] != 0 {
			t.Errorf("%s: 不应装载任何新渠道，实际 %v", c.name, types)
		}
	}
}

/* TestLoadNotifiersTelegramAdvanced Telegram 的 topicid/url/proxy 应被装配 */
func TestLoadNotifiersTelegramAdvanced(t *testing.T) {
	m := NewManager()
	LoadNotifiersFromConfig(m, map[string]string{
		"tgbot_token":   "tk",
		"tgbot_chatid":  "-100123",
		"tgbot_topicid": "42",
		"tgbot_url":     "https://tg.example.com/",
		"tgbot_proxy":   "2",
	})

	if len(m.notifiers) != 1 {
		t.Fatalf("应装载 1 个渠道，实际 %d", len(m.notifiers))
	}
	tn, ok := m.notifiers[0].(*TelegramNotifier)
	if !ok {
		t.Fatalf("渠道类型不符: %T", m.notifiers[0])
	}
	if tn.config.TopicID != "42" || tn.config.ServerURL != "https://tg.example.com/" || tn.config.ProxyMode != "2" {
		t.Errorf("Telegram 高级配置装配有误: %+v", tn.config)
	}
	if got := tn.telegramBaseURL(); got != "https://tg.example.com" {
		t.Errorf("反代基址不符: %q", got)
	}
}

/* TestLoadNotifiersWebhookNewFields Webhook 的 user 与 content_format 应被装配 */
func TestLoadNotifiersWebhookNewFields(t *testing.T) {
	m := NewManager()
	LoadNotifiersFromConfig(m, map[string]string{
		"webhook_url":            "https://oapi.dingtalk.com/robot/send?access_token=x",
		"webhook_user":           "all",
		"webhook_content_format": "markdown",
		"webhook_headers":        "X-A: 1\nX-B: 2",
	})

	wn, ok := m.notifiers[0].(*WebhookNotifier)
	if !ok {
		t.Fatalf("渠道类型不符: %T", m.notifiers[0])
	}
	if wn.config.User != "all" {
		t.Errorf("webhook_user 未装配: %q", wn.config.User)
	}
	if wn.config.ContentFormat != "markdown" {
		t.Errorf("content_format 未装配: %q", wn.config.ContentFormat)
	}
	if wn.config.Headers["X-A"] != "1" || wn.config.Headers["X-B"] != "2" {
		t.Errorf("请求头装配有误: %v", wn.config.Headers)
	}
	// 端到端确认钉钉分支生效
	body, err := wn.buildBody("标题", "<b>正文</b>")
	if err != nil {
		t.Fatalf("构建请求体失败: %v", err)
	}
	if !containsAll(body, `"msgtype":"markdown"`, `"isAtAll":true`, `"title":"标题"`) {
		t.Errorf("钉钉请求体不符: %s", body)
	}
}

/* TestNotifyConfigKeysIncludeNewKeys 新增配置键必须登记进 notifyConfigKeys，否则 getter 装载时会漏读 */
func TestNotifyConfigKeysIncludeNewKeys(t *testing.T) {
	required := []string{
		"tgbot_topicid", "tgbot_url", "tgbot_proxy",
		"wechat_apptoken", "wechat_appuid",
		"qqbot_appid", "qqbot_appsecret", "qqbot_openid",
		"webhook_user", "webhook_content_format",
	}
	have := make(map[string]bool)
	for _, k := range NotifyConfigKeys() {
		have[k] = true
	}
	for _, k := range required {
		if !have[k] {
			t.Errorf("配置键 %s 未登记进 notifyConfigKeys", k)
		}
	}
}

/* TestLoadNotifiersFromConfigFullKeys 用 getter 装载：只有登记过的键才会被读到 */
func TestLoadNotifiersFromConfigFullKeys(t *testing.T) {
	store := map[string]string{
		"tgbot_token":            "tk",
		"tgbot_chatid":           "1",
		"tgbot_topicid":          "42",
		"wechat_apptoken":        "AT",
		"wechat_appuid":          "UID",
		"qqbot_appid":            "a",
		"qqbot_appsecret":        "s",
		"qqbot_openid":           "o",
		"webhook_url":            "https://example.com/hook",
		"webhook_user":           "13800000000",
		"webhook_content_format": "text",
	}
	m := NewManager()
	LoadNotifiersWithGetter(m, func(key string) string { return store[key] })

	types := loadedTypes(m)
	for _, want := range []string{"TelegramNotifier", "WxPusherNotifier", "QQBotNotifier", "WebhookNotifier"} {
		if types[want] != 1 {
			t.Errorf("%s 应装载 1 个，实际 %d（配置键可能未登记）", want, types[want])
		}
	}
}

/* TestSendAllIsolatesFailures 多渠道发送时单个渠道失败不应影响其它渠道（NotifyManager 行为） */
func TestSendAllIsolatesFailures(t *testing.T) {
	m := NewManager()
	// 两个指向内网地址、必然被 SSRF 校验拒绝的 Webhook
	m.AddNotifier(NewWebhookNotifier(WebhookConfig{URL: "http://127.0.0.1/hook"}))
	m.AddNotifier(NewWebhookNotifier(WebhookConfig{URL: "http://10.0.0.1/hook"}))
	errs := m.SendAll(context.Background(), "t", "c")
	if len(errs) != 2 {
		t.Errorf("应收集到 2 个错误，实际 %d", len(errs))
	}
}

/* containsAll 判定 s 是否同时包含全部子串 */
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

/* contains 简易子串判定，避免额外导入 strings */
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
