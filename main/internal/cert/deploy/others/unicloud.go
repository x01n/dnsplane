package others

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert/deploy/base"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
)

func init() {
	base.Register("unicloud", NewUnicloudProvider)
}

// UnicloudProvider uniCloud部署器
type UnicloudProvider struct {
	base.BaseProvider
	token string
}

func NewUnicloudProvider(config map[string]interface{}) base.DeployProvider {
	return &UnicloudProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *UnicloudProvider) Check(ctx context.Context) error {
	return p.login()
}

func (p *UnicloudProvider) login() error {
	username := p.GetString("username")
	password := p.GetString("password")

	client := &http.Client{Timeout: 30 * time.Second}

	loginData := map[string]string{
		"username": username,
		"password": password,
	}
	jsonData, _ := json.Marshal(loginData)

	resp, err := client.Post("https://unicloud.dcloud.net.cn/api/login", "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	if code, ok := result["code"].(float64); ok && code != 0 {
		return fmt.Errorf("登录失败: %v", result["message"])
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		if token, ok := data["token"].(string); ok {
			p.token = token
			return nil
		}
	}

	return fmt.Errorf("登录失败: 无法获取token")
}

func (p *UnicloudProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	spaceID := p.GetStringFrom(config, "spaceId")
	provider := p.GetStringFrom(config, "provider")
	domains := strings.Split(p.GetStringFrom(config, "domains"), ",")

	p.Log(fmt.Sprintf("开始部署到uniCloud空间: %s", spaceID))

	if err := p.login(); err != nil {
		return err
	}

	client := &http.Client{Timeout: 60 * time.Second}

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}

		p.Log(fmt.Sprintf("正在部署域名: %s", domain))

		updateData := map[string]interface{}{
			"spaceId":     spaceID,
			"provider":    provider,
			"domain":      domain,
			"certificate": fullchain,
			"privateKey":  privateKey,
		}
		jsonData, _ := json.Marshal(updateData)

		req, _ := http.NewRequestWithContext(ctx, "POST", "https://unicloud.dcloud.net.cn/api/domain/bindssl", bytes.NewReader(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.token)

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("部署请求失败: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("响应解析失败: %w", err)
		}

		if code, ok := result["code"].(float64); ok && code != 0 {
			return fmt.Errorf("部署失败: %v", result["message"])
		}
	}

	p.Log("uniCloud部署完成")
	return nil
}

func (p *UnicloudProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
