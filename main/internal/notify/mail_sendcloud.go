package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const sendcloudAPIURL = "http://api.sendcloud.net/apiv2/mail/send"

/* SendcloudConfig Sendcloud 邮件 API 配置 */
type SendcloudConfig struct {
	APIUser  string `json:"api_user"`
	APIKey   string `json:"api_key"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	To       string `json:"to"`
}

/* SendcloudNotifier Sendcloud 邮件通知发送器 */
type SendcloudNotifier struct {
	config SendcloudConfig
	client *http.Client
	// apiURL 独立成字段便于测试注入本地服务端，生产路径始终使用 sendcloudAPIURL
	apiURL string
}

/*
 * NewSendcloudNotifier 创建 Sendcloud 邮件发送器
 */
func NewSendcloudNotifier(config SendcloudConfig) *SendcloudNotifier {
	return &SendcloudNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
		apiURL: sendcloudAPIURL,
	}
}

/*
 * Send 通过 Sendcloud HTTP API 发送 HTML 邮件
 * 参数：title 邮件主题，content HTML 正文
 */
func (n *SendcloudNotifier) Send(ctx context.Context, title, content string) error {
	if n.config.APIUser == "" || n.config.APIKey == "" {
		return fmt.Errorf("Sendcloud apiUser/apiKey 未配置")
	}
	// 邮件头 CRLF 注入防御：API 路径同样禁止头部字段携带控制字符
	if err := SanitizeMailHeader("Subject", title); err != nil {
		return err
	}
	if err := SanitizeMailHeader("From", n.config.From); err != nil {
		return err
	}
	if err := SanitizeMailHeader("FromName", n.config.FromName); err != nil {
		return err
	}
	to := splitMailRecipients(n.config.To)
	if len(to) == 0 {
		return fmt.Errorf("未配置收件人")
	}
	for _, addr := range to {
		if err := SanitizeMailHeader("To", addr); err != nil {
			return err
		}
	}

	form := url.Values{}
	form.Set("apiUser", n.config.APIUser)
	form.Set("apiKey", n.config.APIKey)
	form.Set("from", n.config.From)
	form.Set("fromName", n.config.FromName)
	form.Set("to", strings.Join(to, ","))
	form.Set("subject", title)
	form.Set("html", content)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送Sendcloud邮件失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取Sendcloud响应失败: %w", err)
	}
	var result struct {
		StatusCode int             `json:"statusCode"`
		Message    json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("Sendcloud响应解析失败(http_code=%d): %s", resp.StatusCode, string(body))
	}
	if result.StatusCode == 200 {
		return nil
	}
	return fmt.Errorf("Sendcloud返回错误(%d): %s", result.StatusCode, string(result.Message))
}
