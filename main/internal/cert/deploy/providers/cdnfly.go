package providers

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("cdnfly", NewCdnFlyProvider)
}

// CdnFlyProvider CdnFly CDN部署器
type CdnFlyProvider struct {
	base.BaseProvider
	client *http.Client
}

// NewCdnFlyProvider 创建CdnFly部署器
func NewCdnFlyProvider(config map[string]interface{}) base.DeployProvider {
	return &CdnFlyProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *CdnFlyProvider) Check(ctx context.Context) error {
	panelURL := p.GetString("url")
	auth := p.GetString("auth")

	if panelURL == "" {
		return fmt.Errorf("面板地址不能为空")
	}

	if auth == "1" {
		username := p.GetString("username")
		password := p.GetString("password")
		if username == "" || password == "" {
			return fmt.Errorf("用户名和密码不能为空")
		}
		_, err := p.login(ctx)
		return err
	}

	apiKey := p.GetString("api_key")
	apiSecret := p.GetString("api_secret")
	if apiKey == "" || apiSecret == "" {
		return fmt.Errorf("API Key和Secret不能为空")
	}

	_, err := p.request(ctx, "GET", "/v1/user", nil)
	return err
}

func (p *CdnFlyProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	certID := base.GetConfigString(config, "id")
	if certID == "" {
		certID = p.GetString("id")
	}

	if certID == "" {
		return p.createCert(ctx, fullchain, privateKey)
	}
	return p.updateCert(ctx, certID, fullchain, privateKey)
}

func (p *CdnFlyProvider) createCert(ctx context.Context, fullchain, privateKey string) error {
	certName, err := p.getCertName(fullchain)
	if err != nil {
		return err
	}

	params := map[string]interface{}{
		"type": "custom",
		"name": certName,
		"cert": fullchain,
		"key":  privateKey,
	}

	auth := p.GetString("auth")
	if auth == "1" {
		accessToken, err := p.login(ctx)
		if err != nil {
			return err
		}
		data, err := p.requestWithToken(ctx, "POST", "/v1/certs", params, accessToken)
		if err != nil {
			return fmt.Errorf("证书添加失败: %v", err)
		}
		p.Log(fmt.Sprintf("证书ID:%v添加成功", data))
	} else {
		data, err := p.request(ctx, "POST", "/v1/certs", params)
		if err != nil {
			return fmt.Errorf("证书添加失败: %v", err)
		}
		p.Log(fmt.Sprintf("证书ID:%v添加成功", data))
	}
	return nil
}

func (p *CdnFlyProvider) updateCert(ctx context.Context, certID, fullchain, privateKey string) error {
	params := map[string]interface{}{
		"type": "custom",
		"cert": fullchain,
		"key":  privateKey,
	}

	path := fmt.Sprintf("/v1/certs/%s", certID)
	auth := p.GetString("auth")

	if auth == "1" {
		accessToken, err := p.login(ctx)
		if err != nil {
			return err
		}
		_, err = p.requestWithToken(ctx, "PUT", path, params, accessToken)
		if err != nil {
			return fmt.Errorf("证书ID:%s更新失败: %v", certID, err)
		}
	} else {
		_, err := p.request(ctx, "PUT", path, params)
		if err != nil {
			return fmt.Errorf("证书ID:%s更新失败: %v", certID, err)
		}
	}

	p.Log(fmt.Sprintf("证书ID:%s更新成功", certID))
	return nil
}

func (p *CdnFlyProvider) login(ctx context.Context) (string, error) {
	panelURL := strings.TrimSuffix(p.GetString("url"), "/")

	params := map[string]interface{}{
		"account":  p.GetString("username"),
		"password": p.GetString("password"),
	}

	bodyBytes, _ := json.Marshal(params)
	req, err := http.NewRequestWithContext(ctx, "POST", panelURL+"/v1/login", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 0 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if token, ok := data["access_token"].(string); ok {
				return token, nil
			}
		}
	}

	if msg, ok := result["msg"].(string); ok {
		return "", fmt.Errorf("%s", msg)
	}
	return "", fmt.Errorf("登录失败")
}

func (p *CdnFlyProvider) request(ctx context.Context, method, path string, params map[string]interface{}) (interface{}, error) {
	panelURL := strings.TrimSuffix(p.GetString("url"), "/")

	var bodyReader io.Reader
	if params != nil {
		bodyBytes, _ := json.Marshal(params)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, panelURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("api-key", p.GetString("api_key"))
	req.Header.Set("api-secret", p.GetString("api_secret"))
	if params != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 0 {
		return result["data"], nil
	}

	if msg, ok := result["msg"].(string); ok {
		return nil, fmt.Errorf("%s", msg)
	}
	return nil, fmt.Errorf("返回数据解析失败")
}

func (p *CdnFlyProvider) requestWithToken(ctx context.Context, method, path string, params map[string]interface{}, token string) (interface{}, error) {
	panelURL := strings.TrimSuffix(p.GetString("url"), "/")

	var bodyReader io.Reader
	if params != nil {
		bodyBytes, _ := json.Marshal(params)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, panelURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Access-Token", token)
	if params != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 0 {
		return result["data"], nil
	}

	if msg, ok := result["msg"].(string); ok {
		return nil, fmt.Errorf("%s", msg)
	}
	return nil, fmt.Errorf("返回数据解析失败")
}

func (p *CdnFlyProvider) getCertName(fullchain string) (string, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return "", fmt.Errorf("无法解析证书")
	}

	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("证书解析失败: %v", err)
	}

	cn := strings.ReplaceAll(c.Subject.CommonName, "*.", "")
	return fmt.Sprintf("%s-%d", cn, c.NotBefore.Unix()), nil
}

func (p *CdnFlyProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
