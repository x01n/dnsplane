package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

/* NotifyType 通知渠道类型枚举 */
type NotifyType string

const (
	NotifyEmail     NotifyType = "email"
	NotifyTelegram  NotifyType = "telegram"
	NotifyWebhook   NotifyType = "webhook"
	NotifyDiscord   NotifyType = "discord"
	NotifyBark      NotifyType = "bark"
	NotifyWechat    NotifyType = "wechat"
	NotifyDingTalk  NotifyType = "dingtalk"
	NotifyFeishu    NotifyType = "feishu"
	NotifyWxWorkApp NotifyType = "wxwork_app"
	NotifyWxTpl     NotifyType = "wxtpl"
	NotifyWxPusher  NotifyType = "wxpusher"
	NotifyQQBot     NotifyType = "qqbot"
	/* NotifyCustomWebhook 自定义 Webhook，与 NotifyWebhook 的通用渠道并存：地址与报文完全由用户定义 */
	NotifyCustomWebhook NotifyType = "custom_webhook"
)

/* Notifier 通知发送接口，所有通知渠道实现此接口 */
type Notifier interface {
	Send(ctx context.Context, title, content string) error
}

/* EmailConfig 邮件通知配置 */
type EmailConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	To       string `json:"to"`
	Secure   string `json:"secure"`    // none, ssl, tls
	AuthType string `json:"auth_type"` // plain, login, crammd5
	UseTLS   bool   `json:"use_tls"`   // deprecated, use Secure instead
}

/* EmailNotifier 邮件通知发送器 */
type EmailNotifier struct {
	config EmailConfig
}

func NewEmailNotifier(config EmailConfig) *EmailNotifier {
	return &EmailNotifier{config: config}
}

/*
 * emailHTMLContent 将通知正文转为邮件可用的 HTML 片段
 * NotifyManager 与 monitor 告警的 content 契约为纯文本（+ \n），不含 HTML 标签；
 * 而邮件后端按 HTML 投递（SMTP Content-Type: text/html、Sendcloud 的 html 字段、
 * DirectMail 的 HtmlBody），纯文本的 \n 会塌成一行，故需转义
 * 判定依据来自实测：templates.go 的 6 个模板邮件均以 <!DOCTYPE html> 起始，
 * 模板邮件走 auth.go / dashboard.go 的独立构造点，不经本函数
 * 误判方向取舍：纯文本被当成 HTML 仅导致换行未生效（可接受），
 * 反之则会损坏模板邮件的样式表（详见实测：无条件替换会在 <style> 内插入 12 个 <br/>）
 * @param content 通知正文
 * @returns 邮件 HTML 正文
 */
func emailHTMLContent(content string) string {
	if strings.HasPrefix(strings.TrimSpace(content), "<") {
		return content
	}
	// 先转义 HTML 特殊字符，避免纯文本中的 < 被当作标签解析
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(content)
	escaped = strings.ReplaceAll(escaped, "\r\n", "\n")
	escaped = strings.ReplaceAll(escaped, "\r", "\n")
	return strings.ReplaceAll(escaped, "\n", "<br/>")
}

/*
 * mailContentAdapter 邮件正文适配器
 * 包装各类邮件后端，在交给内部实现前完成纯文本到 HTML 片段的转换，
 * 使 SMTP / Sendcloud / DirectMail 三种后端共用同一契约而无需各自重复转换逻辑
 */
type mailContentAdapter struct {
	inner Notifier
}

func (a *mailContentAdapter) Send(ctx context.Context, title, content string) error {
	return a.inner.Send(ctx, title, emailHTMLContent(content))
}

/*
 * NewMailContentAdapter 包装任意邮件后端，使其接受纯文本契约
 * 供邮件通知之外的构造点（如 internal/monitor 的容灾告警）使用，
 * 避免各处重复实现纯文本到 HTML 片段的转换逻辑
 * @param inner 已构造的邮件后端
 * @returns 包装后的通知发送器
 */
func NewMailContentAdapter(inner Notifier) Notifier {
	return &mailContentAdapter{inner: inner}
}

func (n *EmailNotifier) Send(ctx context.Context, title, content string) error {
	// 邮件头 CRLF 注入防御（安全审计 R-4）
	if err := SanitizeMailHeader("Subject", title); err != nil {
		return err
	}
	if err := SanitizeMailHeader("From", n.config.From); err != nil {
		return err
	}
	if err := SanitizeMailHeader("FromName", n.config.FromName); err != nil {
		return err
	}

	to := strings.Split(n.config.To, ",")
	for i := range to {
		to[i] = strings.TrimSpace(to[i])
		if err := SanitizeMailHeader("To", to[i]); err != nil {
			return err
		}
	}

	// 构建发件人头部
	fromHeader := n.config.From
	if n.config.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", n.config.FromName, n.config.From)
	}

	// 构建邮件内容
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromHeader, strings.Join(to, ", "), title, content)

	addr := fmt.Sprintf("%s:%d", n.config.Host, n.config.Port)

	// 选择认证方式
	var auth smtp.Auth
	switch n.config.AuthType {
	case "login":
		auth = LoginAuth(n.config.Username, n.config.Password)
	case "crammd5":
		auth = smtp.CRAMMD5Auth(n.config.Username, n.config.Password)
	default:
		auth = smtp.PlainAuth("", n.config.Username, n.config.Password, n.config.Host)
	}

	// 判断加密方式
	secure := n.config.Secure
	if secure == "" && n.config.UseTLS {
		secure = "ssl"
	}

	switch secure {
	case "ssl":
		return n.sendWithSSL(addr, auth, to, msg)
	case "tls":
		return n.sendWithSTARTTLS(addr, auth, to, msg)
	default:
		return n.sendPlain(addr, auth, to, msg)
	}
}

// smtpPhaseDeadline 单次连接上读 SMTP 横幅/握手等阶段的最长等待，避免恶意或故障对端拖死请求
const smtpDialTimeout = 12 * time.Second
const smtpPhaseDeadline = 15 * time.Second

func setSMTPDeadline(c net.Conn) {
	if c != nil {
		_ = c.SetDeadline(time.Now().Add(smtpPhaseDeadline))
	}
}

/* sendPlain 无加密 SMTP 发送（短连接/阶段超时，避免长时间阻塞） */
func (n *EmailNotifier) sendPlain(addr string, auth smtp.Auth, to []string, msg string) error {
	conn, err := net.DialTimeout("tcp", addr, smtpDialTimeout)
	if err != nil {
		return fmt.Errorf("SMTP连接失败: %w", err)
	}
	defer conn.Close()
	setSMTPDeadline(conn)

	client, err := smtp.NewClient(conn, n.config.Host)
	if err != nil {
		return fmt.Errorf("创建SMTP客户端失败: %w", err)
	}
	defer client.Close()

	return n.sendViaClient(client, auth, to, msg)
}

func (n *EmailNotifier) sendWithSSL(addr string, auth smtp.Auth, to []string, msg string) error {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         n.config.Host,
	}
	/* 使用 net.DialTimeout + tls.Client 替代无超时的 tls.Dial */
	rawConn, err := net.DialTimeout("tcp", addr, smtpDialTimeout)
	if err != nil {
		return fmt.Errorf("SSL连接失败: %w", err)
	}
	conn := tls.Client(rawConn, tlsConfig)
	setSMTPDeadline(rawConn)
	if err := conn.Handshake(); err != nil {
		rawConn.Close()
		return fmt.Errorf("SSL握手失败: %w", err)
	}
	defer conn.Close()
	setSMTPDeadline(conn)

	client, err := smtp.NewClient(conn, n.config.Host)
	if err != nil {
		return fmt.Errorf("创建SMTP客户端失败: %w", err)
	}
	defer client.Close()

	return n.sendViaClient(client, auth, to, msg)
}

func (n *EmailNotifier) sendWithSTARTTLS(addr string, auth smtp.Auth, to []string, msg string) error {
	conn, err := net.DialTimeout("tcp", addr, smtpDialTimeout)
	if err != nil {
		return fmt.Errorf("STARTTLS连接失败: %w", err)
	}
	defer conn.Close()
	setSMTPDeadline(conn)

	client, err := smtp.NewClient(conn, n.config.Host)
	if err != nil {
		return fmt.Errorf("创建SMTP客户端失败: %w", err)
	}
	defer client.Close()

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         n.config.Host,
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("STARTTLS失败: %w", err)
	}
	setSMTPDeadline(conn)

	return n.sendViaClient(client, auth, to, msg)
}

/* sendViaClient 通过已建立的 SMTP 客户端发送邮件（从 sendWithSSL/sendWithSTARTTLS 提取的公共逻辑） */
func (n *EmailNotifier) sendViaClient(client *smtp.Client, auth smtp.Auth, to []string, msg string) error {
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP认证失败: %w", err)
	}
	if err := client.Mail(n.config.From); err != nil {
		return err
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write([]byte(msg)); err != nil {
		return err
	}
	return w.Close()
}

/* loginAuth SMTP LOGIN 认证方式（部分邮件服务商需要） */
type loginAuth struct {
	username, password string
}

func LoginAuth(username, password string) smtp.Auth {
	return &loginAuth{username, password}
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte(a.username), nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		switch string(fromServer) {
		case "Username:":
			return []byte(a.username), nil
		case "Password:":
			return []byte(a.password), nil
		}
	}
	return nil, nil
}

/* TelegramConfig Telegram Bot 通知配置 */
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	// TopicID 超级群话题 ID（tgbot_topicid），>0 时作为 message_thread_id 发送
	TopicID string `json:"topic_id"`
	// ServerURL 自定义反代地址（tgbot_url），仅当 ProxyMode 为 2 时生效
	ServerURL string `json:"server_url"`
	ProxyMode string `json:"proxy_mode"`
}

/* TelegramNotifier Telegram Bot 通知发送器 */
type TelegramNotifier struct {
	config TelegramConfig
	client *http.Client
}

func NewTelegramNotifier(config TelegramConfig) *TelegramNotifier {
	return &TelegramNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

/*
 * telegramBaseURL 计算 Telegram API 基址
 * 仅当 tgbot_proxy == 2 且 tgbot_url 非空时改用自定义反代地址（去除尾部斜杠）
 * @returns API 基址
 */
func (n *TelegramNotifier) telegramBaseURL() string {
	if n.config.ProxyMode == "2" {
		if custom := strings.TrimSpace(n.config.ServerURL); custom != "" {
			return strings.TrimRight(custom, "/")
		}
	}
	return "https://api.telegram.org"
}

/*
 * buildTelegramPayload 构建 sendMessage 请求体
 * tgbot_topicid 经 intval 后 > 0 才附加 message_thread_id
 * @param title   通知标题
 * @param content 通知正文
 * @returns 请求体
 */
func (n *TelegramNotifier) buildTelegramPayload(title, content string) map[string]interface{} {
	payload := map[string]interface{}{
		"chat_id":    n.config.ChatID,
		"text":       fmt.Sprintf("<b>%s</b>\n\n%s", title, content),
		"parse_mode": "HTML",
	}
	if topicID, err := strconv.Atoi(strings.TrimSpace(n.config.TopicID)); err == nil && topicID > 0 {
		payload["message_thread_id"] = topicID
	}
	return payload
}

func (n *TelegramNotifier) Send(ctx context.Context, title, content string) error {
	baseURL := n.telegramBaseURL()
	// 出站 URL SSRF 防御（安全审计 R-5）
	if err := ValidateOutboundURL(baseURL); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", baseURL, n.config.BotToken)

	body, _ := json.Marshal(n.buildTelegramPayload(title, content))
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送Telegram消息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Telegram API错误: %s", string(respBody))
	}

	return nil
}

/* WebhookConfig 聚合群机器人 Webhook 通知配置 */
type WebhookConfig struct {
	URL string `json:"url"`
	// User 对应 webhook_user：钉钉为手机号列表或 all，飞书为用户 ID 列表或 all，企业微信不支持
	User string `json:"user"`
	// ContentFormat 对应 webhook_content_format：html（不转换）/ markdown / text
	ContentFormat string            `json:"content_format"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers"`
	ContentType   string            `json:"content_type"`
	Template      string            `json:"template"`
}

/* WebhookNotifier 自定义 Webhook 通知发送器 */
type WebhookNotifier struct {
	config WebhookConfig
	client *http.Client
}

func NewWebhookNotifier(config WebhookConfig) *WebhookNotifier {
	if config.Method == "" {
		config.Method = "POST"
	}
	if config.ContentType == "" {
		config.ContentType = "application/json"
	}
	return &WebhookNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

/*
 * buildBody 构建 Webhook 请求体
 * 流程：先按 webhook_content_format 转换正文，再判定是否为聚合群机器人地址；
 * 命中钉钉/企业微信/飞书时走各自的原生 payload，否则回退通用 title/content 模板
 * @param title   通知标题
 * @param content 通知正文
 * @returns 请求体字符串与错误
 */
func (n *WebhookNotifier) buildBody(title, content string) (string, error) {
	switch n.config.ContentFormat {
	case "markdown":
		content = stripHTMLTags(strings.NewReplacer("<br/>", "\n", "<b>", "**", "</b>", "**").Replace(content))
	case "text":
		content = stripHTMLTags(strings.ReplaceAll(content, "<br/>", "\n"))
	}
	if botType, ok := detectWebhookBot(n.config.URL); ok {
		return buildBotWebhookBody(botType, title, content, n.config.User)
	}

	body := n.config.Template
	if body == "" {
		payload := map[string]string{
			"title":   title,
			"content": content,
		}
		bodyBytes, _ := json.Marshal(payload)
		body = string(bodyBytes)
	} else {
		body = strings.ReplaceAll(body, "{title}", title)
		body = strings.ReplaceAll(body, "{content}", content)
	}
	return body, nil
}

func (n *WebhookNotifier) Send(ctx context.Context, title, content string) error {
	// 出站 URL SSRF 防御（安全审计 R-5）
	if err := ValidateOutboundURL(n.config.URL); err != nil {
		return err
	}

	body, err := n.buildBody(title, content)
	if err != nil {
		return err
	}
	return n.post(ctx, n.config.URL, body)
}

/*
 * post 发送已构建好的请求体
 * @param ctx    请求上下文
 * @param url    目标地址
 * @param body   请求体
 * @returns error
 */
func (n *WebhookNotifier) post(ctx context.Context, url, body string) error {
	req, err := http.NewRequestWithContext(ctx, n.config.Method, url, strings.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", n.config.ContentType)
	for k, v := range n.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("Webhook请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Webhook响应错误 %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

/* webhookBotType 聚合群机器人类型（由 Webhook 地址判定） */
type webhookBotType int

/*
 * marshalJSONNoHTMLEscape 序列化 JSON 且不转义 < > &
 * @param v 待序列化值
 * @returns JSON 字节与错误
 */
func marshalJSONNoHTMLEscape(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encoder.Encode 末尾会追加换行，裁剪后与 json.Marshal 输出形态一致
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

const (
	webhookBotDingTalk webhookBotType = iota
	webhookBotWechatWork
	webhookBotFeishu
)

/*
 * detectWebhookBot 按地址判定群机器人类型
 * @param rawURL 用户配置的 Webhook 地址
 * @returns 命中的类型与是否命中
 */
func detectWebhookBot(rawURL string) (webhookBotType, bool) {
	switch {
	case strings.Contains(rawURL, "oapi.dingtalk.com"):
		return webhookBotDingTalk, true
	case strings.Contains(rawURL, "qyapi.weixin.qq.com"):
		return webhookBotWechatWork, true
	case strings.Contains(rawURL, "open.feishu.cn"), strings.Contains(rawURL, "open.larksuite.com"):
		return webhookBotFeishu, true
	}
	return 0, false
}

/*
 * buildBotWebhookBody 构建群机器人请求体
 *   - 钉钉 markdown 正文加 "### " 前缀且换行改为行尾双空格，@用户走 at.atMobiles / at.isAtAll
 *   - 企业微信 markdown 正文加 "## " 前缀，无 @
 *   - 飞书为 schema 2.0 交互卡片，@用户以 <at> 标签追加到正文，卡片配色由标题关键字决定
 * @param bot     机器人类型
 * @param title   通知标题
 * @param content 已按内容格式转换的正文
 * @param atUser  @用户配置（all 或逗号分隔列表）
 * @returns JSON 请求体与错误
 */
func buildBotWebhookBody(bot webhookBotType, title, content, atUser string) (string, error) {
	var payload interface{}
	switch bot {
	case webhookBotDingTalk:
		markdown := map[string]interface{}{
			"title": title,
			"text":  "### " + title + "  \n " + strings.ReplaceAll(content, "\n", "  \n "),
		}
		payload = map[string]interface{}{"msgtype": "markdown", "markdown": markdown}
		if atUser != "" {
			at := map[string]interface{}{}
			if atUser == "all" {
				at["isAtAll"] = true
			} else {
				at["atMobiles"] = strings.Split(atUser, ",")
				at["isAtAll"] = false
			}
			payload.(map[string]interface{})["at"] = at
		}
	case webhookBotWechatWork:
		payload = map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": "## " + title + "\n" + content,
			},
		}
	case webhookBotFeishu:
		body := strings.ReplaceAll(content, `<font color="warning">`, `<font color="red">`)
		if atUser != "" {
			if atUser == "all" {
				body += "\n" + `<at id=all></at> `
			} else {
				body += "\n"
				for _, u := range strings.Split(atUser, ",") {
					body += `<at user_id="` + u + `"></at> `
				}
			}
		}
		payload = map[string]interface{}{
			"msg_type": "interactive",
			"card": map[string]interface{}{
				"schema": "2.0",
				"config": map[string]interface{}{
					"update_multi": true,
					"style": map[string]interface{}{
						"text_size": map[string]interface{}{
							"normal_v2": map[string]string{
								"default": "normal",
								"pc":      "normal",
								"mobile":  "heading",
							},
						},
					},
				},
				"header": map[string]interface{}{
					"title":    map[string]string{"tag": "plain_text", "content": title},
					"subtitle": map[string]string{"tag": "plain_text", "content": ""},
					"template": feishuCardTemplate(title),
					"padding":  "12px 12px 12px 12px",
				},
				"body": map[string]interface{}{
					"direction": "vertical",
					"padding":   "12px 12px 12px 12px",
					"elements": []map[string]interface{}{
						{
							"tag":        "markdown",
							"content":    body,
							"text_align": "left",
							"text_size":  "normal_v2",
							"margin":     "0px 0px 0px 0px",
						},
					},
				},
			},
		}
	default:
		return "", fmt.Errorf("不支持的Webhook类型")
	}

	bodyBytes, err := marshalJSONNoHTMLEscape(payload)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

/*
 * feishuCardTemplate 飞书卡片配色
 * 判定顺序为告警/失败 -> 恢复正常 -> 到期提醒，均未命中时为 blue
 * @param title 通知标题
 * @returns 卡片模板色名
 */
func feishuCardTemplate(title string) string {
	switch {
	case strings.Contains(title, "发生告警"), strings.Contains(title, "失败"):
		return "red"
	case strings.Contains(title, "恢复正常"):
		return "green"
	case strings.Contains(title, "到期提醒"):
		return "yellow"
	}
	return "blue"
}

/*
 * stripHTMLTags 剥离 HTML 标签，语义
 * @param s 原始文本
 * @returns 去除标签后的文本
 */
func stripHTMLTags(s string) string {
	return stripTags(s)
}

/* NotifyManager 多渠道通知管理器，支持同时发送到多个渠道 */
type NotifyManager struct {
	notifiers []Notifier
}

func NewManager() *NotifyManager {
	return &NotifyManager{
		notifiers: make([]Notifier, 0),
	}
}

func (m *NotifyManager) AddNotifier(n Notifier) {
	m.notifiers = append(m.notifiers, n)
}

/*
 * NotifyManager 的 content 契约为纯文本（+ \n），不含 HTML 标签
 * 邮件后端按 HTML 投递（SMTP Content-Type: text/html、Sendcloud 的 html 字段、
 * DirectMail 的 HtmlBody），故纯文本的 \n 会塌成一行，
 * 需经 NewMailContentAdapter 包装后转义为 <br/> 才能换行
 * 已是 HTML 的内容（auth.go / dashboard.go 的模板邮件走独立构造点）原样透传
 * 契约此前未写明，导致邮件分支按 HTML 投递而调用方传纯文本，两边各自都正确却组合出错
 */
func (m *NotifyManager) SendAll(ctx context.Context, title, content string) []error {
	var errors []error
	for _, n := range m.notifiers {
		if err := n.Send(ctx, title, content); err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

func (m *NotifyManager) Send(ctx context.Context, title, content string) error {
	errors := m.SendAll(ctx, title, content)
	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

/* DiscordConfig Discord Webhook 通知配置 */
type DiscordConfig struct {
	WebhookURL string `json:"webhook_url"`
}

/* DiscordNotifier Discord Webhook 通知发送器 */
type DiscordNotifier struct {
	config DiscordConfig
	client *http.Client
}

func NewDiscordNotifier(config DiscordConfig) *DiscordNotifier {
	return &DiscordNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (n *DiscordNotifier) Send(ctx context.Context, title, content string) error {
	// 出站 URL SSRF 防御（安全审计 R-5）
	if err := ValidateOutboundURL(n.config.WebhookURL); err != nil {
		return err
	}
	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       title,
				"description": content,
				"color":       5814783,
			},
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", n.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送Discord消息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Discord API错误 %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

/* BarkConfig Bark 推送通知配置 */
type BarkConfig struct {
	ServerURL string `json:"server_url"`
	DeviceKey string `json:"device_key"`
}

/* BarkNotifier Bark 推送通知发送器 */
type BarkNotifier struct {
	config BarkConfig
	client *http.Client
}

func NewBarkNotifier(config BarkConfig) *BarkNotifier {
	if config.ServerURL == "" {
		config.ServerURL = "https://api.day.app"
	}
	return &BarkNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (n *BarkNotifier) Send(ctx context.Context, title, content string) error {
	// 出站 URL SSRF 防御（安全审计 R-5）：先校验 ServerURL，再拼最终路径
	if err := ValidateOutboundURL(n.config.ServerURL); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/%s/%s/%s", strings.TrimSuffix(n.config.ServerURL, "/"), n.config.DeviceKey, title, content)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送Bark推送失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Bark API错误 %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

/* WechatWorkConfig 企业微信群机器人通知配置 */
type WechatWorkConfig struct {
	WebhookURL string `json:"webhook_url"`
}

/* WechatWorkNotifier 企业微信群机器人通知发送器 */
type WechatWorkNotifier struct {
	config WechatWorkConfig
	client *http.Client
}

func NewWechatWorkNotifier(config WechatWorkConfig) *WechatWorkNotifier {
	return &WechatWorkNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (n *WechatWorkNotifier) Send(ctx context.Context, title, content string) error {
	// 出站 URL SSRF 防御（安全审计 R-5）
	if err := ValidateOutboundURL(n.config.WebhookURL); err != nil {
		return err
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": fmt.Sprintf("## %s\n%s", title, content),
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", n.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送企业微信消息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("企业微信API错误 %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

/*
 * LoadNotifiersFromConfig 从配置 map 加载所有已配置的通知渠道到 manager
 * @param manager   通知管理器实例
 * @param configMap SysConfig key-value 配置映射
 * 功能：自动检测已配置的邮件/Telegram/Webhook/Discord/Bark/企业微信渠道并注册
 */
/*
 * NotifyConfigKeys 返回通知渠道所需的全部配置键（供外部包按需读取）
 * 聚合自渠道注册表：各渠道的配置键、启用开关与容灾告警开关，
 * 新增渠道时由注册动作自动并入，本文件不再维护任何键清单
 */
func NotifyConfigKeys() []string { return ChannelConfigKeys() }

/*
 * LoadNotifiersWithGetter 使用配置读取函数加载通知渠道（缓存友好）
 * 功能：接受一个 key→value 的读取函数，避免每次都从 DB 批量查询
 *       适用于 executor / task_runner 等后台任务使用 sysconfig 缓存层
 */
func LoadNotifiersWithGetter(manager *NotifyManager, getter func(key string) string) {
	keys := NotifyConfigKeys()
	configMap := make(map[string]string, len(keys))
	for _, key := range keys {
		if v := getter(key); v != "" {
			configMap[key] = v
		}
	}
	LoadNotifiersFromConfig(manager, configMap)
}

/*
 * LoadMailNotifier 仅装载邮件通知渠道
 * 委托邮件渠道适配器构造，使按渠道筛选的装载点（internal/monitor 的容灾告警）
 * 与全量装载共用同一份收件人回退与 mail_type 三路分发，避免两处各写一遍而逐渐分叉
 * @param manager   通知管理器
 * @param configMap 系统配置键值
 * @returns 是否装载了邮件渠道
 */
func LoadMailNotifier(manager *NotifyManager, configMap map[string]string) bool {
	n, ok := buildMailNotifier(configMap)
	if !ok {
		return false
	}
	manager.AddNotifier(n)
	return true
}

/*
 * LoadNotifiersFromConfig 从配置 map 加载全部已配置且已启用的通知渠道到 manager
 * 遍历渠道注册表逐个构造，渠道增删只影响注册表，本函数不再随渠道变化而改动
 * 启用开关的判定在各渠道的 Build 内完成（语义见 ChannelEnabled）
 * @param manager   通知管理器实例
 * @param configMap SysConfig key-value 配置映射
 */
func LoadNotifiersFromConfig(manager *NotifyManager, configMap map[string]string) {
	for _, ch := range Channels() {
		if n, ok := ch.Build(configMap); ok {
			manager.AddNotifier(n)
		}
	}
}
