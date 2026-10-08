package notify

import (
	"encoding/json"
	"strings"
	"testing"
)

/*
 * Telegram 高级配置测试
 */

/* TestTelegramBaseURL 覆盖 tgbot_proxy 三种取值下的基址选择 */
func TestTelegramBaseURL(t *testing.T) {
	cases := []struct {
		name      string
		proxyMode string
		serverURL string
		want      string
	}{
		{"未配置代理", "", "", "https://api.telegram.org"},
		{"显式直连", "0", "https://custom.example.com", "https://api.telegram.org"},
		{"系统代理不改变基址", "1", "https://custom.example.com", "https://api.telegram.org"},
		{"自定义反代", "2", "https://custom.example.com", "https://custom.example.com"},
		{"自定义反代去除尾部斜杠", "2", "https://custom.example.com/", "https://custom.example.com"},
		{"自定义反代多尾斜杠", "2", "https://custom.example.com///", "https://custom.example.com"},
		{"自定义反代含空格", "2", "  https://custom.example.com  ", "https://custom.example.com"},
		{"模式 2 但地址为空则回退默认", "2", "", "https://api.telegram.org"},
		{"模式 2 但地址为空白则回退默认", "2", "   ", "https://api.telegram.org"},
	}
	for _, c := range cases {
		n := NewTelegramNotifier(TelegramConfig{BotToken: "tk", ChatID: "1", ProxyMode: c.proxyMode, ServerURL: c.serverURL})
		if got := n.telegramBaseURL(); got != c.want {
			t.Errorf("%s: got=%q want=%q", c.name, got, c.want)
		}
	}
}

/* TestTelegramPayloadTopicID 覆盖 tgbot_topicid 的 intval > 0 判定 */
func TestTelegramPayloadTopicID(t *testing.T) {
	base := NewTelegramNotifier(TelegramConfig{BotToken: "tk", ChatID: "123"})
	payload := base.buildTelegramPayload("标题", "正文")
	if payload["chat_id"] != "123" {
		t.Errorf("chat_id 不符: %v", payload["chat_id"])
	}
	if payload["parse_mode"] != "HTML" {
		t.Errorf("parse_mode 应为 HTML: %v", payload["parse_mode"])
	}
	if payload["text"] != "<b>标题</b>\n\n正文" {
		t.Errorf("text 不符: %v", payload["text"])
	}
	if _, ok := payload["message_thread_id"]; ok {
		t.Error("未配置 topicid 时不应携带 message_thread_id")
	}

	withTopic := []struct {
		topicID  string
		wantKept bool
		wantVal  int
	}{
		{"", false, 0},
		{"0", false, 0},
		{"-1", false, 0},
		{"abc", false, 0},
		{"1", true, 1},
		{" 42 ", true, 42},
		{"999999", true, 999999},
	}
	for _, c := range withTopic {
		n := NewTelegramNotifier(TelegramConfig{BotToken: "tk", ChatID: "1", TopicID: c.topicID})
		p := n.buildTelegramPayload("t", "c")
		v, ok := p["message_thread_id"]
		if ok != c.wantKept {
			t.Errorf("topicid=%q 携带状态=%v 期望 %v", c.topicID, ok, c.wantKept)
			continue
		}
		if ok && v != c.wantVal {
			t.Errorf("topicid=%q 值=%v 期望 %d", c.topicID, v, c.wantVal)
		}
	}
}

/* TestTelegramPayloadJSONShape 确认序列化后的字段名与 JSON 类型符合 Telegram sendMessage 契约 */
func TestTelegramPayloadJSONShape(t *testing.T) {
	n := NewTelegramNotifier(TelegramConfig{BotToken: "tk", ChatID: "1", TopicID: "7"})
	raw, err := json.Marshal(n.buildTelegramPayload("t", "c"))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if v, ok := decoded["message_thread_id"].(float64); !ok || int(v) != 7 {
		t.Errorf("message_thread_id 应为 JSON 数字: %s", raw)
	}
	for _, k := range []string{"chat_id", "text", "parse_mode", "message_thread_id"} {
		if _, ok := decoded[k]; !ok {
			t.Errorf("缺少字段 %s: %s", k, raw)
		}
	}
}

/* TestTelegramSendRejectsPrivateReverseProxy 自定义反代地址仍须通过 SSRF 校验 */
func TestTelegramSendRejectsPrivateReverseProxy(t *testing.T) {
	for _, addr := range []string{
		"http://127.0.0.1:8080",
		"http://localhost:8080",
		"http://10.0.0.5",
		"http://192.168.1.1",
		"http://169.254.169.254",
		"file:///etc/passwd",
	} {
		n := NewTelegramNotifier(TelegramConfig{BotToken: "tk", ChatID: "1", ProxyMode: "2", ServerURL: addr})
		err := n.Send(t.Context(), "t", "c")
		if err == nil {
			t.Errorf("地址 %q 未被 SSRF 校验拦截", addr)
		}
		if err != nil && !strings.Contains(err.Error(), "拒绝") && !strings.Contains(err.Error(), "不允许") {
			t.Errorf("地址 %q 的报错不符合预期: %v", addr, err)
		}
	}
}
