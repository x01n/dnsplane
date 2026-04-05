package others

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert/deploy/base"
	"net/http"
	"time"

	"main/internal/cert"
)

func init() {
	base.Register("kuocai", NewKuocaiProvider)
}

// KuocaiProvider 括彩云部署器
type KuocaiProvider struct {
	base.BaseProvider
	token string
}

func NewKuocaiProvider(config map[string]interface{}) base.DeployProvider {
	return &KuocaiProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *KuocaiProvider) Check(ctx context.Context) error {
	return p.login()
}

func (p *KuocaiProvider) login() error {
	username := p.GetString("username")
	password := p.GetString("password")

	client := &http.Client{Timeout: 30 * time.Second}

	loginData := map[string]string{
		"username": username,
		"password": password,
	}
	jsonData, _ := json.Marshal(loginData)

	resp, err := client.Post("https://api.kuocai.cn/api/login", "application/json", bytes.NewReader(jsonData))
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
		return fmt.Errorf("登录失败: %v", result["msg"])
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		if token, ok := data["token"].(string); ok {
			p.token = token
			return nil
		}
	}

	return fmt.Errorf("登录失败: 无法获取token")
}

func (p *KuocaiProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domainID := p.GetStringFrom(config, "id")

	p.Log(fmt.Sprintf("开始部署到括彩云域名: %s", domainID))

	if err := p.login(); err != nil {
		return err
	}

	client := &http.Client{Timeout: 60 * time.Second}

	updateData := map[string]interface{}{
		"id":          domainID,
		"certificate": fullchain,
		"private_key": privateKey,
	}
	jsonData, _ := json.Marshal(updateData)

	req, _ := http.NewRequestWithContext(ctx, "PUT", fmt.Sprintf("https://api.kuocai.cn/api/domain/%s/ssl", domainID), bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("部署请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	if code, ok := result["code"].(float64); ok && code != 0 {
		return fmt.Errorf("部署失败: %v", result["msg"])
	}

	p.Log("括彩云部署完成")
	return nil
}

func (p *KuocaiProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
