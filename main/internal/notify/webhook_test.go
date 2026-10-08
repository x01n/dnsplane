package notify

import (
	"encoding/json"
	"strings"
	"testing"
)

/*
 * 聚合群机器人 Webhook 测试
 */
func TestDetectWebhookBot(t *testing.T) {
	cases := []struct {
		url      string
		wantType webhookBotType
		wantOK   bool
	}{
		{"https://oapi.dingtalk.com/robot/send?access_token=x", webhookBotDingTalk, true},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x", webhookBotWechatWork, true},
		{"https://open.feishu.cn/open-apis/bot/v2/hook/x", webhookBotFeishu, true},
		{"https://open.larksuite.com/open-apis/bot/v2/hook/x", webhookBotFeishu, true},
		{"https://example.com/webhook", 0, false},
		{"https://my-dingtalk-proxy.example.com/hook", 0, false},
	}
	for _, c := range cases {
		gotType, gotOK := detectWebhookBot(c.url)
		if gotOK != c.wantOK {
			t.Errorf("url=%q 命中=%v 期望 %v", c.url, gotOK, c.wantOK)
			continue
		}
		if gotOK && gotType != c.wantType {
			t.Errorf("url=%q 类型=%v 期望 %v", c.url, gotType, c.wantType)
		}
	}
}

func TestWebhookDingTalkBody(t *testing.T) {
	body, err := buildBotWebhookBody(webhookBotDingTalk, "标题", "行一\n行二", "")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("解析失败: %v body=%s", err, body)
	}
	if got["msgtype"] != "markdown" {
		t.Errorf("msgtype 不符: %v", got["msgtype"])
	}
	md, _ := got["markdown"].(map[string]interface{})
	if md["title"] != "标题" {
		t.Errorf("markdown.title 不符: %v", md["title"])
	}
	if md["text"] != "### 标题  \n 行一  \n 行二" {
		t.Errorf("markdown.text 不符: %q", md["text"])
	}
	if _, ok := got["at"]; ok {
		t.Error("未配置 webhook_user 时不应携带 at 字段")
	}

	// @全体
	body, _ = buildBotWebhookBody(webhookBotDingTalk, "t", "c", "all")
	_ = json.Unmarshal([]byte(body), &got)
	at, _ := got["at"].(map[string]interface{})
	if at["isAtAll"] != true {
		t.Errorf("all 应设置 isAtAll=true: %v", at)
	}
	if _, ok := at["atMobiles"]; ok {
		t.Errorf("all 不应设置 atMobiles: %v", at)
	}

	// @指定手机号
	body, _ = buildBotWebhookBody(webhookBotDingTalk, "t", "c", "13800000000,13900000000")
	_ = json.Unmarshal([]byte(body), &got)
	at, _ = got["at"].(map[string]interface{})
	if at["isAtAll"] != false {
		t.Errorf("指定用户应设置 isAtAll=false: %v", at)
	}
	mobiles, _ := at["atMobiles"].([]interface{})
	if len(mobiles) != 2 || mobiles[0] != "13800000000" || mobiles[1] != "13900000000" {
		t.Errorf("atMobiles 不符: %v", at["atMobiles"])
	}
}

func TestWebhookWechatWorkBody(t *testing.T) {
	body, err := buildBotWebhookBody(webhookBotWechatWork, "标题", "正文", "13800000000")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("解析失败: %v body=%s", err, body)
	}
	if got["msgtype"] != "markdown" {
		t.Errorf("msgtype 不符: %v", got["msgtype"])
	}
	md, _ := got["markdown"].(map[string]interface{})
	if md["content"] != "## 标题\n正文" {
		t.Errorf("markdown.content 不符: %q", md["content"])
	}
	// 企业微信分支不处理 webhook_user
	if strings.Contains(body, "13800000000") {
		t.Errorf("企业微信不应包含 @用户信息: %s", body)
	}
	if _, ok := got["at"]; ok {
		t.Errorf("企业微信不应携带 at 字段: %s", body)
	}
}

func TestWebhookFeishuBody(t *testing.T) {
	body, err := buildBotWebhookBody(webhookBotFeishu, "DNS容灾切换-发生告警通知", `警告 <font color="warning">err</font>`, "")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("解析失败: %v body=%s", err, body)
	}
	if got["msg_type"] != "interactive" {
		t.Errorf("msg_type 不符: %v", got["msg_type"])
	}
	card, _ := got["card"].(map[string]interface{})
	if card["schema"] != "2.0" {
		t.Errorf("card.schema 不符: %v", card["schema"])
	}
	header, _ := card["header"].(map[string]interface{})
	if header["template"] != "red" {
		t.Errorf("含「发生告警」应为 red: %v", header["template"])
	}
	if header["padding"] != "12px 12px 12px 12px" {
		t.Errorf("header.padding 不符: %v", header["padding"])
	}
	title, _ := header["title"].(map[string]interface{})
	if title["tag"] != "plain_text" || title["content"] != "DNS容灾切换-发生告警通知" {
		t.Errorf("header.title 不符: %v", title)
	}
	elements, _ := card["body"].(map[string]interface{})["elements"].([]interface{})
	if len(elements) != 1 {
		t.Fatalf("elements 长度不符: %v", elements)
	}
	el, _ := elements[0].(map[string]interface{})
	if el["tag"] != "markdown" || el["text_align"] != "left" || el["text_size"] != "normal_v2" || el["margin"] != "0px 0px 0px 0px" {
		t.Errorf("element 字段不符: %v", el)
	}
	// <font color="warning"> 应替换为 red
	if el["content"] != `警告 <font color="red">err</font>` {
		t.Errorf("正文替换不符: %q", el["content"])
	}

	// @全体：<at> 标签位于 JSON 字符串值内，双引号会被转义，故解码后比对
	body, _ = buildBotWebhookBody(webhookBotFeishu, "t", "c", "all")
	if got := feishuElementContent(t, body); got != "c\n<at id=all></at> " {
		t.Errorf("@全体正文不符: %q", got)
	}
	// @指定用户
	body, _ = buildBotWebhookBody(webhookBotFeishu, "t", "c", "ou_a,ou_b")
	if got := feishuElementContent(t, body); got != "c\n<at user_id=\"ou_a\"></at> <at user_id=\"ou_b\"></at> " {
		t.Errorf("@指定用户正文不符: %q", got)
	}
}

/* feishuElementContent 解出飞书卡片的正文（card.body.elements[0].content），供断言使用 */
func feishuElementContent(t *testing.T, body string) string {
	t.Helper()
	var parsed struct {
		Card struct {
			Body struct {
				Elements []struct {
					Content string `json:"content"`
				} `json:"elements"`
			} `json:"body"`
		} `json:"card"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("解析飞书卡片失败: %v body=%s", err, body)
	}
	if len(parsed.Card.Body.Elements) == 0 {
		t.Fatalf("飞书卡片缺少 elements: %s", body)
	}
	return parsed.Card.Body.Elements[0].Content
}

func TestBuildBotWebhookBodyJSONEscape(t *testing.T) {
	for _, tc := range []struct {
		name string
		bot  webhookBotType
		at   string
	}{
		{"钉钉", webhookBotDingTalk, "13800000000"},
		{"飞书@全体", webhookBotFeishu, "all"},
		{"飞书@指定用户", webhookBotFeishu, "ou_a"},
	} {
		body, err := buildBotWebhookBody(tc.bot, "标题", `正文 <b>x</b> & y`, tc.at)
		if err != nil {
			t.Fatalf("%s 构建失败: %v", tc.name, err)
		}
		if strings.Contains(body, `\u003c`) || strings.Contains(body, `\u003e`) || strings.Contains(body, `\u0026`) {
			t.Errorf("%s 序列化不应转义 HTML 字符: %s", tc.name, body)
		}
	}
}

func TestFeishuCardTemplate(t *testing.T) {
	cases := []struct{ title, want string }{
		{"DNS容灾切换-发生告警通知", "red"},
		{"证书部署失败通知", "red"},
		{"DNS容灾切换-恢复正常通知", "green"},
		{"example.com 域名SSL证书即将到期提醒", "yellow"},
		{"普通通知标题", "blue"},
		{"", "blue"},
	}
	for _, c := range cases {
		if got := feishuCardTemplate(c.title); got != c.want {
			t.Errorf("title=%q got=%q want=%q", c.title, got, c.want)
		}
	}
}

func TestWebhookContentFormat(t *testing.T) {
	raw := `您好：<br/>域名 <b>example.com</b> 失败`

	cases := []struct {
		format string
		want   string
	}{
		{"html", raw},
		{"", raw},
		{"unknown", raw},
		{"text", "您好：\n域名 example.com 失败"},
		{"markdown", "您好：\n域名 **example.com** 失败"},
	}
	for _, c := range cases {
		n := NewWebhookNotifier(WebhookConfig{
			URL:           "https://example.com/hook",
			ContentFormat: c.format,
		})
		body, err := n.buildBody("t", raw)
		if err != nil {
			t.Fatalf("format=%q 构建失败: %v", c.format, err)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("format=%q 解析失败: %v body=%s", c.format, err, body)
		}
		if got["content"] != c.want {
			t.Errorf("format=%q got=%q want=%q", c.format, got["content"], c.want)
		}
	}
}

/* TestWebhookGenericTemplate 未命中群机器人时应保留原有通用模板行为（向后兼容） */
func TestWebhookGenericTemplate(t *testing.T) {
	n := NewWebhookNotifier(WebhookConfig{URL: "https://example.com/hook"})
	body, err := n.buildBody("标题", "正文")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got["title"] != "标题" || got["content"] != "正文" {
		t.Errorf("默认模板不符: %v", got)
	}

	n = NewWebhookNotifier(WebhookConfig{
		URL:      "https://example.com/hook",
		Template: `title={title}&content={content}`,
	})
	body, err = n.buildBody("T", "C")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if body != "title=T&content=C" {
		t.Errorf("自定义模板占位符替换不符: %q", body)
	}
}

/* TestWebhookUserIgnoredForGenericURL 非群机器人地址下 webhook_user 不产生任何影响 */
func TestWebhookUserIgnoredForGenericURL(t *testing.T) {
	n := NewWebhookNotifier(WebhookConfig{URL: "https://example.com/hook", User: "all"})
	body, err := n.buildBody("t", "c")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if strings.Contains(body, "at") {
		t.Errorf("通用 Webhook 不应受 webhook_user 影响: %s", body)
	}
}
