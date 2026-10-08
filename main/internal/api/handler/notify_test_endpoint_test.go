package handler

import (
	"reflect"
	"strings"
	"testing"

	"main/internal/notify"
)

/*
 * 通知渠道测试端点的装配测试
 *
 * 这些端点此前各自硬编码一份配置读取与通知器构造，已出现多处参数缺失：
 * Webhook 测试只传 URL 并硬编码 Method=POST（正文模板、请求头、内容格式、@用户全部无效）、
 * Telegram 测试只传 BotToken 与 ChatID（话题 ID、自定义反代、代理模式全部丢失）；
 * 邮件的鉴权方式键还写成了全仓无写入点的 mail_auth，导致 AuthType 恒为默认值。
 * 改为遍历渠道注册表构造后，参数缺失不可能只发生在测试端点这一条路径上
 */

/*
 * notifierConfigField 读取通知器 config 内字符串字段
 *
 * config 在 notify 包内未导出，handler 不能反向依赖其内部结构，故沿反射链取值。
 * reflect.Value.Interface() 对未导出字段会 panic，本条链上只用 Kind()/String()/MapRange，
 * 全程不做 Interface() 转换
 */
func notifierConfigField(t *testing.T, n notify.Notifier, field string) (string, bool) {
	t.Helper()
	f := notifierConfigValue(t, n, field)
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

/* notifierConfigMapField 读取通知器 config 内 map[string]string 字段 */
func notifierConfigMapField(t *testing.T, n notify.Notifier, field string) (map[string]string, bool) {
	t.Helper()
	f := notifierConfigValue(t, n, field)
	if !f.IsValid() || f.Kind() != reflect.Map {
		return nil, false
	}
	out := make(map[string]string, f.Len())
	iter := f.MapRange()
	for iter.Next() {
		k, v := iter.Key(), iter.Value()
		if k.Kind() == reflect.String && v.Kind() == reflect.String {
			out[k.String()] = v.String()
		}
	}
	return out, true
}

/* notifierConfigValue 定位通知器实例的 config 字段值 */
func notifierConfigValue(t *testing.T, n notify.Notifier, field string) reflect.Value {
	t.Helper()
	v := reflect.ValueOf(n)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		t.Fatalf("通知器不是有效指针: %T", n)
	}
	f := v.Elem().FieldByName("config").FieldByName(field)
	if !f.IsValid() {
		t.Fatalf("通知器 %T 上找不到 config.%s", n, field)
	}
	return f
}

/* TestBuildChannelForTestKeepsWebhookFields Webhook 测试端点必须装配七个字段，不得只传 URL */
func TestBuildChannelForTestKeepsWebhookFields(t *testing.T) {
	setupHandlerTestDB(t)
	seedConfig(t, map[string]string{
		"webhook_url":            "https://oapi.dingtalk.com/robot/send?access_token=x",
		"webhook_method":         "PUT",
		"webhook_content_type":   "application/json",
		"webhook_headers":        "X-A: 1",
		"webhook_body":           `{"k":"{title}"}`,
		"webhook_content_format": "markdown",
		"webhook_user":           "all",
	})

	c, _ := newTestContext()
	notifier, _, err := buildChannelForTest(c, notify.NotifyWebhook)
	if err != nil {
		t.Fatalf("构造渠道失败: %v", err)
	}
	if _, ok := notifier.(*notify.WebhookNotifier); !ok {
		t.Fatalf("构造类型 = %T，期望 *notify.WebhookNotifier", notifier)
	}

	want := map[string]string{
		"URL":           "https://oapi.dingtalk.com/robot/send?access_token=x",
		"Method":        "PUT",
		"ContentType":   "application/json",
		"Template":      `{"k":"{title}"}`,
		"ContentFormat": "markdown",
		"User":          "all",
	}
	for field, wantVal := range want {
		got, ok := notifierConfigField(t, notifier, field)
		if !ok {
			t.Errorf("WebhookConfig.%s 未装配到", field)
			continue
		}
		if got != wantVal {
			t.Errorf("WebhookConfig.%s = %q, want %q", field, got, wantVal)
		}
	}
	if headers, ok := notifierConfigMapField(t, notifier, "Headers"); !ok || headers["X-A"] != "1" {
		t.Errorf("WebhookConfig.Headers 未装配: %v", headers)
	}
}

/* TestBuildChannelForTestKeepsTelegramFields Telegram 测试端点必须装配五个字段 */
func TestBuildChannelForTestKeepsTelegramFields(t *testing.T) {
	setupHandlerTestDB(t)
	seedConfig(t, map[string]string{
		"tgbot_token":   "tk",
		"tgbot_chatid":  "-100123",
		"tgbot_topicid": "42",
		"tgbot_url":     "https://tg.example.com/",
		"tgbot_proxy":   "2",
	})

	c, _ := newTestContext()
	notifier, _, err := buildChannelForTest(c, notify.NotifyTelegram)
	if err != nil {
		t.Fatalf("构造渠道失败: %v", err)
	}
	if _, ok := notifier.(*notify.TelegramNotifier); !ok {
		t.Fatalf("构造类型 = %T，期望 *notify.TelegramNotifier", notifier)
	}

	want := map[string]string{
		"BotToken": "tk", "ChatID": "-100123", "TopicID": "42",
		"ServerURL": "https://tg.example.com/", "ProxyMode": "2",
	}
	for field, wantVal := range want {
		got, ok := notifierConfigField(t, notifier, field)
		if !ok {
			t.Errorf("TelegramConfig.%s 未装配到", field)
			continue
		}
		if got != wantVal {
			t.Errorf("TelegramConfig.%s = %q, want %q", field, got, wantVal)
		}
	}
}

/*
 * TestBuildChannelForTestIgnoresEnabledSwitch 测试端点不受渠道启用开关影响
 * 渠道被关闭与凭据是否可用是两件事：关闭的渠道仍应能测出配置本身有没有问题
 */
func TestBuildChannelForTestIgnoresEnabledSwitch(t *testing.T) {
	setupHandlerTestDB(t)
	seedConfig(t, map[string]string{
		"tgbot_token":     "tk",
		"tgbot_chatid":    "-100123",
		"tgbot_enabled":   "0",
		"webhook_url":     "https://example.com/hook",
		"webhook_enabled": "off",
	})

	c, _ := newTestContext()
	for _, typ := range []notify.NotifyType{notify.NotifyTelegram, notify.NotifyWebhook} {
		if _, _, err := buildChannelForTest(c, typ); err != nil {
			t.Errorf("渠道 %s 被显式关闭时测试端点仍应可构造，实际报错: %v", typ, err)
		}
	}
}

/* TestBuildChannelForTestReportsMissingConfig 配置不全时必须报错，且错误信息带上渠道名 */
func TestBuildChannelForTestReportsMissingConfig(t *testing.T) {
	setupHandlerTestDB(t)

	c, _ := newTestContext()
	_, ch, err := buildChannelForTest(c, notify.NotifyCustomWebhook)
	if err == nil {
		t.Fatal("未配置时构造应失败")
	}
	if !strings.Contains(err.Error(), ch.Label()) {
		t.Errorf("错误信息未标明渠道: %v", err)
	}
}

/* TestBuildChannelForTestUnknownChannel 未注册的渠道标识必须报错而不是 panic */
func TestBuildChannelForTestUnknownChannel(t *testing.T) {
	setupHandlerTestDB(t)
	c, _ := newTestContext()
	if _, _, err := buildChannelForTest(c, notify.NotifyType("no_such_channel")); err == nil {
		t.Fatal("未注册的渠道应返回错误")
	}
}

/* TestBuildChannelForTestCustomWebhook 自定义 Webhook 通道可用且六个字段齐备 */
func TestBuildChannelForTestCustomWebhook(t *testing.T) {
	setupHandlerTestDB(t)
	seedConfig(t, map[string]string{
		"custom_webhook_url":            "https://example.com/hook",
		"custom_webhook_method":         "GET",
		"custom_webhook_content_type":   "application/json",
		"custom_webhook_headers":        "X-Token: abc",
		"custom_webhook_body":           `{"t":"{title}"}`,
		"custom_webhook_content_format": "text",
	})

	c, _ := newTestContext()
	notifier, _, err := buildChannelForTest(c, notify.NotifyCustomWebhook)
	if err != nil {
		t.Fatalf("构造渠道失败: %v", err)
	}
	if _, ok := notifier.(*notify.CustomWebhookNotifier); !ok {
		t.Fatalf("构造类型 = %T，期望 *notify.CustomWebhookNotifier", notifier)
	}
	want := map[string]string{
		"URL": "https://example.com/hook", "Method": "GET", "ContentType": "application/json",
		"Headers": "X-Token: abc", "Body": `{"t":"{title}"}`, "ContentFormat": "text",
	}
	for field, wantVal := range want {
		got, ok := notifierConfigField(t, notifier, field)
		if !ok {
			t.Errorf("CustomWebhookConfig.%s 未装配到", field)
			continue
		}
		if got != wantVal {
			t.Errorf("CustomWebhookConfig.%s = %q, want %q", field, got, wantVal)
		}
	}
}

/*
 * TestNotificationTestHandlersCoverAllChannels 每个已注册渠道都必须有测试端点可达
 * 渠道注册了却点不到「测试」按钮，与渠道未实现没有区别
 */
func TestNotificationTestHandlersCoverAllChannels(t *testing.T) {
	setupHandlerTestDB(t)
	// 覆盖渠道构造所需的最小配置，逐渠道验证构造路径本身可用
	cfg := map[string]string{
		"mail_host": "smtp.example.com", "mail_from": "from@example.com",
		"tgbot_token": "tk", "tgbot_chatid": "-100123",
		"webhook_url":        "https://example.com/hook",
		"custom_webhook_url": "https://example.com/custom",
		"discord_webhook":    "https://discord.com/api/webhooks/x",
		"bark_server":        "https://api.day.app", "bark_key": "k",
		"wechat_webhook":   "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x",
		"dingtalk_webhook": "https://oapi.dingtalk.com/robot/send?access_token=x",
		"feishu_webhook":   "https://open.feishu.cn/open-apis/bot/v2/hook/x",
		"wxwork_corpid":    "corp", "wxwork_agentid": "1", "wxwork_secret": "s",
		"wxtpl_appid": "a", "wxtpl_appsecret": "s", "wxtpl_template_id": "t", "wxtpl_users": "u",
		"wechat_apptoken": "AT", "wechat_appuid": "UID",
		"qqbot_appid": "a", "qqbot_appsecret": "s", "qqbot_openid": "o",
	}
	seedConfig(t, cfg)

	c, _ := newTestContext()
	for _, ch := range notify.Channels() {
		t.Run(string(ch.Type()), func(t *testing.T) {
			notifier, _, err := buildChannelForTest(c, ch.Type())
			if err != nil {
				t.Fatalf("渠道 %s 构造失败: %v", ch.Type(), err)
			}
			if notifier == nil {
				t.Fatalf("渠道 %s 构造出 nil 通知器", ch.Type())
			}
		})
	}
}
