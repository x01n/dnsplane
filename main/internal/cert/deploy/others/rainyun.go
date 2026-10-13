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
	base.Register("rainyun", NewRainyunProvider)
}

// RainyunProvider 雨云部署器
type RainyunProvider struct {
	base.BaseProvider
}

func NewRainyunProvider(config map[string]interface{}) base.DeployProvider {
	return &RainyunProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *RainyunProvider) Check(ctx context.Context) error {
	apiKey := p.GetString("apikey")

	client := &http.Client{Timeout: 30 * time.Second}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.rainyun.com/user/info", nil)
	req.Header.Set("x-api-key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	if code, ok := result["code"].(float64); ok && code != 200 {
		return fmt.Errorf("API错误: %v", result["message"])
	}

	return nil
}

func (p *RainyunProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	apiKey := p.GetString("apikey")
	certID := p.GetStringFrom(config, "id")

	p.Log("开始部署到雨云")

	client := &http.Client{Timeout: 60 * time.Second}

	var apiURL string
	var method string
	if certID == "" {
		apiURL = "https://api.rainyun.com/ssl/cert"
		method = "POST"
		p.Log("添加新证书")
	} else {
		apiURL = fmt.Sprintf("https://api.rainyun.com/ssl/cert/%s", certID)
		method = "PUT"
		p.Log(fmt.Sprintf("更新证书: %s", certID))
	}

	updateData := map[string]interface{}{
		"certificate": fullchain,
		"private_key": privateKey,
	}
	jsonData, _ := json.Marshal(updateData)

	req, _ := http.NewRequestWithContext(ctx, method, apiURL, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	if code, ok := result["code"].(float64); ok && code != 200 {
		return fmt.Errorf("部署失败: %v", result["message"])
	}

	p.Log("雨云部署完成")
	return nil
}

func (p *RainyunProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
