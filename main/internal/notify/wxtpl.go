package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type WxTplConfig struct {
	AppID      string `json:"app_id"`
	AppSecret  string `json:"app_secret"`
	TemplateID string `json:"template_id"`
	ToUsers    string `json:"to_users"`
	URL        string `json:"url"`
}

type WxTplNotifier struct {
	config      WxTplConfig
	client      *http.Client
	tokenMu     sync.Mutex
	cachedToken string
	tokenExpire time.Time
}

func NewWxTplNotifier(config WxTplConfig) *WxTplNotifier {
	return &WxTplNotifier{config: config, client: &http.Client{Timeout: 30 * time.Second}}
}

func (n *WxTplNotifier) getToken(ctx context.Context) (string, error) {
	n.tokenMu.Lock()
	defer n.tokenMu.Unlock()
	if n.cachedToken != "" && time.Now().Before(n.tokenExpire) {
		return n.cachedToken, nil
	}
	url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s", n.config.AppID, n.config.AppSecret)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取公众号 token 失败: %w", err)
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
	if result.AccessToken == "" {
		return "", fmt.Errorf("获取公众号 token 失败: %s", result.ErrMsg)
	}
	n.cachedToken = result.AccessToken
	n.tokenExpire = time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)
	return n.cachedToken, nil
}

func (n *WxTplNotifier) Send(ctx context.Context, title, content string) error {
	token, err := n.getToken(ctx)
	if err != nil {
		return err
	}
	users := strings.Split(n.config.ToUsers, ",")
	for _, user := range users {
		user = strings.TrimSpace(user)
		if user == "" {
			continue
		}
		payload := map[string]interface{}{
			"touser":      user,
			"template_id": n.config.TemplateID,
			"url":         n.config.URL,
			"data": map[string]interface{}{
				"first":    map[string]string{"value": title},
				"keyword1": map[string]string{"value": content},
				"keyword2": map[string]string{"value": time.Now().Format("2006-01-02 15:04:05")},
				"remark":   map[string]string{"value": "DNSPlane"},
			},
		}
		body, _ := json.Marshal(payload)
		apiURL := "https://api.weixin.qq.com/cgi-bin/message/template/send?access_token=" + token
		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := n.client.Do(req)
		if err != nil {
			return fmt.Errorf("发送公众号模板消息失败: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var result struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return err
		}
		if result.ErrCode != 0 {
			return fmt.Errorf("公众号模板消息错误: %s", result.ErrMsg)
		}
	}
	return nil
}
