package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

/* DingTalkConfig 钉钉群自定义机器人通知配置 */
type DingTalkConfig struct {
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
}

/* DingTalkNotifier 钉钉群自定义机器人通知发送器 */
type DingTalkNotifier struct {
	config DingTalkConfig
	client *http.Client
}

func NewDingTalkNotifier(config DingTalkConfig) *DingTalkNotifier {
	return &DingTalkNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (n *DingTalkNotifier) Send(ctx context.Context, title, content string) error {
	if err := ValidateOutboundURL(n.config.WebhookURL); err != nil {
		return err
	}

	finalURL := n.config.WebhookURL
	if n.config.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		stringToSign := timestamp + "\n" + n.config.Secret
		mac := hmac.New(sha256.New, []byte(n.config.Secret))
		mac.Write([]byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		sep := "?"
		if strings.Contains(finalURL, "?") {
			sep = "&"
		}
		finalURL = finalURL + sep + "timestamp=" + timestamp + "&sign=" + url.QueryEscape(sign)
	}

	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  "## " + title + "\n" + content,
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", finalURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送钉钉消息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("钉钉API错误 %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
