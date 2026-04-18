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
	"strings"
	"time"
)

/* NotifyType 通知渠道类型枚举 */
type NotifyType string

const (
	NotifyEmail    NotifyType = "email"
	NotifyTelegram NotifyType = "telegram"
	NotifyWebhook  NotifyType = "webhook"
	NotifyDiscord  NotifyType = "discord"
	NotifyBark     NotifyType = "bark"
	NotifyWechat   NotifyType = "wechat"
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

func (n *TelegramNotifier) Send(ctx context.Context, title, content string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.config.BotToken)
	text := fmt.Sprintf("<b>%s</b>\n\n%s", title, content)

	payload := map[string]interface{}{
		"chat_id":    n.config.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, _ := json.Marshal(payload)
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

/* WebhookConfig 自定义 Webhook 通知配置 */
type WebhookConfig struct {
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	ContentType string            `json:"content_type"`
	Template    string            `json:"template"`
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

func (n *WebhookNotifier) Send(ctx context.Context, title, content string) error {
	// 出站 URL SSRF 防御（安全审计 R-5）
	if err := ValidateOutboundURL(n.config.URL); err != nil {
		return err
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

	req, err := http.NewRequestWithContext(ctx, n.config.Method, n.config.URL, strings.NewReader(body))
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
/* NotifyConfigKeys 返回通知渠道所需的全部配置键（供外部包按需读取） */
func NotifyConfigKeys() []string { return notifyConfigKeys }

var notifyConfigKeys = []string{
	"mail_host", "mail_port", "mail_user", "mail_password", "mail_from", "mail_recv", "mail_secure", "mail_tls",
	"tgbot_token", "tgbot_chatid",
	"webhook_url",
	"discord_webhook",
	"bark_url", "bark_key",
	"wechat_webhook",
}

/*
 * LoadNotifiersWithGetter 使用配置读取函数加载通知渠道（缓存友好）
 * 功能：接受一个 key→value 的读取函数，避免每次都从 DB 批量查询
 *       适用于 executor / task_runner 等后台任务使用 sysconfig 缓存层
 */
func LoadNotifiersWithGetter(manager *NotifyManager, getter func(key string) string) {
	configMap := make(map[string]string, len(notifyConfigKeys))
	for _, key := range notifyConfigKeys {
		if v := getter(key); v != "" {
			configMap[key] = v
		}
	}
	LoadNotifiersFromConfig(manager, configMap)
}

func LoadNotifiersFromConfig(manager *NotifyManager, configMap map[string]string) {
	if configMap["mail_host"] != "" && configMap["mail_recv"] != "" {
		port := 25
		fmt.Sscanf(configMap["mail_port"], "%d", &port)
		useTLS := configMap["mail_secure"] == "ssl" || configMap["mail_tls"] == "1"
		mailTo := configMap["mail_recv"]
		if mailTo == "" {
			mailTo = configMap["mail_from"]
		}
		manager.AddNotifier(NewEmailNotifier(EmailConfig{
			Host: configMap["mail_host"], Port: port,
			Username: configMap["mail_user"], Password: configMap["mail_password"],
			From: configMap["mail_from"], To: mailTo, UseTLS: useTLS,
		}))
	}
	if configMap["tgbot_token"] != "" && configMap["tgbot_chatid"] != "" {
		manager.AddNotifier(NewTelegramNotifier(TelegramConfig{
			BotToken: configMap["tgbot_token"], ChatID: configMap["tgbot_chatid"],
		}))
	}
	if configMap["webhook_url"] != "" {
		manager.AddNotifier(NewWebhookNotifier(WebhookConfig{URL: configMap["webhook_url"]}))
	}
	if configMap["discord_webhook"] != "" {
		manager.AddNotifier(NewDiscordNotifier(DiscordConfig{WebhookURL: configMap["discord_webhook"]}))
	}
	if configMap["bark_url"] != "" && configMap["bark_key"] != "" {
		manager.AddNotifier(NewBarkNotifier(BarkConfig{ServerURL: configMap["bark_url"], DeviceKey: configMap["bark_key"]}))
	}
	if configMap["wechat_webhook"] != "" {
		manager.AddNotifier(NewWechatWorkNotifier(WechatWorkConfig{WebhookURL: configMap["wechat_webhook"]}))
	}
}
