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
	"strconv"
	"time"
)

type FeishuConfig struct {
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
}

type FeishuNotifier struct {
	config FeishuConfig
	client *http.Client
}

func NewFeishuNotifier(config FeishuConfig) *FeishuNotifier {
	return &FeishuNotifier{config: config, client: &http.Client{Timeout: 30 * time.Second}}
}

func (n *FeishuNotifier) Send(ctx context.Context, title, content string) error {
	if err := ValidateOutboundURL(n.config.WebhookURL); err != nil {
		return err
	}
	payload := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{"text": title + "\n" + content},
	}
	if n.config.Secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(ts+"\n"+n.config.Secret))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		payload["timestamp"] = ts
		payload["sign"] = sign
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", n.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送飞书消息失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("飞书API错误 %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.Code != 0 {
		return fmt.Errorf("飞书API错误: %s", result.Msg)
	}
	return nil
}
