package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const wxPusherAPIURL = "https://wxpusher.zjiecode.com/api/send/message"

/* WxPusherConfig WxPusher 微信推送配置 */
type WxPusherConfig struct {
	AppToken string `json:"app_token"`
	UID      string `json:"uid"`
}

/* WxPusherNotifier WxPusher 微信推送发送器 */
type WxPusherNotifier struct {
	config WxPusherConfig
	client *http.Client
}

func NewWxPusherNotifier(config WxPusherConfig) *WxPusherNotifier {
	return &WxPusherNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

/*
 * Send 通过 WxPusher 推送消息
 * contentType 固定为 3（markdown），summary 取标题，uids 为单元素数组
 */
func (n *WxPusherNotifier) Send(ctx context.Context, title, content string) error {
	// 出站 URL SSRF 防御（安全审计 R-5）
	if err := ValidateOutboundURL(wxPusherAPIURL); err != nil {
		return err
	}

	payload := map[string]interface{}{
		"appToken":    n.config.AppToken,
		"content":     content,
		"summary":     title,
		"contentType": 3,
		"uids":        []string{n.config.UID},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", wxPusherAPIURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送WxPusher消息失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("WxPusher API错误 %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("WxPusher 接口响应解析失败: %s", string(respBody))
	}
	if !result.Success {
		if result.Msg == "" {
			result.Msg = "请求失败"
		}
		return fmt.Errorf("WxPusher API错误: %s", result.Msg)
	}
	return nil
}
