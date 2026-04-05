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
	base.Register("uusec", NewUusecProvider)
}

// UusecProvider 南墙WAF部署器
type UusecProvider struct {
	base.BaseProvider
	token string
}

func NewUusecProvider(config map[string]interface{}) base.DeployProvider {
	return &UusecProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *UusecProvider) Check(ctx context.Context) error {
	return p.login()
}

func (p *UusecProvider) login() error {
	panelURL := strings.TrimRight(p.GetString("url"), "/")
	username := p.GetString("username")
	password := p.GetString("password")

	client := &http.Client{Timeout: 30 * time.Second}

	loginData := map[string]string{
		"username": username,
		"password": password,
	}
	jsonData, _ := json.Marshal(loginData)

	resp, err := client.Post(panelURL+"/api/login", "application/json", bytes.NewReader(jsonData))
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

func (p *UusecProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	panelURL := strings.TrimRight(p.GetString("url"), "/")
	certID := p.GetStringFrom(config, "id")
	certName := p.GetStringFrom(config, "name")

	p.Log(fmt.Sprintf("开始部署到南墙WAF: %s", panelURL))

	if err := p.login(); err != nil {
		return err
	}

	client := &http.Client{Timeout: 60 * time.Second}

	p.Log(fmt.Sprintf("正在更新证书: %s (ID: %s)", certName, certID))

	updateData := map[string]interface{}{
		"id":          certID,
		"name":        certName,
		"certificate": fullchain,
		"private_key": privateKey,
	}
	jsonData, _ := json.Marshal(updateData)

	req, _ := http.NewRequestWithContext(ctx, "PUT", panelURL+"/api/certificate/"+certID, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("更新请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	if code, ok := result["code"].(float64); ok && code != 0 {
		return fmt.Errorf("更新失败: %v", result["msg"])
	}

	p.Log("南墙WAF部署完成")
	return nil
}

func (p *UusecProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
