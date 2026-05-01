package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type WxWorkAppConfig struct {
	CorpID  string `json:"corp_id"`
	AgentID string `json:"agent_id"`
	Secret  string `json:"secret"`
	ToUser  string `json:"to_user"`
}

type WxWorkAppNotifier struct {
	config      WxWorkAppConfig
	client      *http.Client
	tokenMu     sync.Mutex
	cachedToken string
	tokenExpire time.Time
}

func NewWxWorkAppNotifier(config WxWorkAppConfig) *WxWorkAppNotifier {
	return &WxWorkAppNotifier{config: config, client: &http.Client{Timeout: 30 * time.Second}}
}

func (n *WxWorkAppNotifier) getToken(ctx context.Context) (string, error) {
	n.tokenMu.Lock()
	defer n.tokenMu.Unlock()
	if n.cachedToken != "" && time.Now().Before(n.tokenExpire) {
		return n.cachedToken, nil
	}
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s", n.config.CorpID, n.config.Secret)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取企业微信 token 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 || result.AccessToken == "" {
		return "", fmt.Errorf("获取企业微信 token 失败: %s", result.ErrMsg)
	}
	n.cachedToken = result.AccessToken
	n.tokenExpire = time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)
	return n.cachedToken, nil
}

func (n *WxWorkAppNotifier) Send(ctx context.Context, title, content string) error {
	token, err := n.getToken(ctx)
	if err != nil {
		return err
	}
	toUser := n.config.ToUser
	if toUser == "" {
		toUser = "@all"
	}
	payload := map[string]interface{}{
		"touser":  toUser,
		"msgtype": "textcard",
		"agentid": n.config.AgentID,
		"textcard": map[string]string{
			"title":       title,
			"description": content,
			"url":         "https://localhost.invalid",
			"btntxt":      "查看",
		},
	}
	body, _ := json.Marshal(payload)
	apiURL := "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=" + token
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送企业微信应用消息失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return err
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("企业微信应用消息错误: %s", result.ErrMsg)
	}
	return nil
}
