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

/* CustomWebhookConfig 自定义 Webhook 通知配置 */
type CustomWebhookConfig struct {
	URL           string `json:"url"`
	Method        string `json:"method"`
	Headers       string `json:"headers"`
	Body          string `json:"body"`
	ContentType   string `json:"content_type"`
	ContentFormat string `json:"content_format"`
}

/*
 * CustomWebhookNotifier 自定义 Webhook 通知发送器
 *
 * 与 WebhookNotifier 的差异在于地址由用户完全自定义：
 * WebhookNotifier 会按地址识别钉钉/企业微信/飞书并改用各自的群机器人报文，
 * 本渠道不做任何识别，方法、请求头、正文模板与内容格式全部照配置执行
 */
type CustomWebhookNotifier struct {
	config CustomWebhookConfig
	client *http.Client
}

func NewCustomWebhookNotifier(config CustomWebhookConfig) *CustomWebhookNotifier {
	return &CustomWebhookNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

/*
 * Send 按配置发送自定义 Webhook 通知
 * @param ctx     请求上下文
 * @param title   通知标题
 * @param content 通知正文
 * @returns error
 */
func (n *CustomWebhookNotifier) Send(ctx context.Context, title, content string) error {
	return sendCustomWebhook(ctx, n.client, n.config, title, content)
}

/*
 * sendCustomWebhook 自定义 Webhook 的统一发送流程
 *
 * 抽成包级函数而非挂在 notifier 上：通知器的 client 固定为默认传输，
 * 集成测试需要注入重定向到本地服务端的 client 来断言请求形态
 *
 * @param ctx     请求上下文
 * @param client  HTTP 客户端
 * @param config  自定义 Webhook 配置
 * @param title   通知标题
 * @param content 通知正文
 * @returns error
 */
func sendCustomWebhook(ctx context.Context, client *http.Client, config CustomWebhookConfig, title, content string) error {
	// 出站 URL SSRF 防御（安全审计 R-5）：地址完全由用户填写，必须校验
	if err := ValidateOutboundURL(config.URL); err != nil {
		return err
	}

	method := strings.ToUpper(strings.TrimSpace(config.Method))
	if method == "" {
		method = "POST"
	}
	contentType := strings.TrimSpace(config.ContentType)
	if contentType == "" {
		contentType = "application/json"
	}

	formatted := formatCustomWebhookContent(config.ContentFormat, content)
	body := buildCustomWebhookBody(config.Body, formatted, title)

	var err error
	target := config.URL
	if method == "GET" {
		if target, err = customWebhookGETURL(config.URL, contentType, body); err != nil {
			return err
		}
		body = ""
	} else if contentType == "application/json" && !json.Valid([]byte(body)) {
		// 占位符按字面替换，正文含引号或换行时拼出的报文不是合法 JSON，
		// 接收端会解析失败而整条通知丢失；此时回退为通用报文，
		// 用户自定义的模板在正文安全时照常生效
		fallback, mErr := json.Marshal(map[string]string{"title": title, "content": formatted})
		if mErr != nil {
			return fmt.Errorf("自定义Webhook正文序列化失败: %w", mErr)
		}
		body = string(fallback)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range parseWebhookHeaders(config.Headers) {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("自定义Webhook请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("自定义Webhook响应错误 %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

/*
 * customWebhookGETURL 把正文拼进查询串
 * @param rawURL      原地址
 * @param contentType 内容类型
 * @param body        已构建的正文
 * @returns 拼接后的地址与错误
 */
func customWebhookGETURL(rawURL, contentType, body string) (string, error) {
	var params url.Values
	if contentType == "application/json" {
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			return "", err
		}
		params = url.Values{}
		for k, v := range decoded {
			params.Set(k, fmt.Sprintf("%v", v))
		}
	} else {
		parsed, err := url.ParseQuery(body)
		if err != nil {
			return "", err
		}
		params = parsed
	}
	if len(params) == 0 {
		return rawURL, nil
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + params.Encode(), nil
}

/*
 * formatCustomWebhookContent 按内容格式把站内通知正文转为可读文本
 * markdown：<br/> 转真正的换行、粗体标签转 ** 后再去标签
 * text：仅把 <br/> 转为换行后去标签
 * html：原样透传，由接收端自行渲染
 * @param format  内容格式配置值
 * @param content 站内通知正文，格式为纯文本 + <br/> 与少量行内标签
 * @returns 转换后的正文
 */
func formatCustomWebhookContent(format, content string) string {
	switch format {
	case "markdown":
		return stripHTMLTags(strings.NewReplacer("<br/>", "\n", "<b>", "**", "</b>", "**").Replace(content))
	case "text":
		return stripHTMLTags(strings.ReplaceAll(content, "<br/>", "\n"))
	default:
		return content
	}
}

/*
 * buildCustomWebhookBody 构建请求正文：先替换模板占位符，未配模板时用通用 JSON 报文
 * @param template 正文模板
 * @param content  已按内容格式转换的正文
 * @param title    通知标题
 * @returns 请求正文
 */
func buildCustomWebhookBody(template, content, title string) string {
	if template == "" {
		template = `{"title":"{title}","content":"{content}"}`
	}
	return strings.NewReplacer("{title}", title, "{content}", content).Replace(template)
}

/*
 * parseWebhookHeaders 解析每行一条的请求头配置
 * 行内以首个冒号分隔键值，无冒号或键为空的行忽略 —— 与设置页文本框的填写说明一致
 * @param raw 每行一条 HeaderName: HeaderValue 的原始文本
 * @returns 请求头映射
 */
func parseWebhookHeaders(raw string) map[string]string {
	headers := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pos := strings.Index(line, ":")
		if pos <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:pos])
		if key == "" {
			continue
		}
		headers[key] = strings.TrimSpace(line[pos+1:])
	}
	return headers
}
