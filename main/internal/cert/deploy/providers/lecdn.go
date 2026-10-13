package providers

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
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
	base.Register("lecdn", NewLeCDNProvider)
}

// LeCDNProvider LeCDN部署器
type LeCDNProvider struct {
	base.BaseProvider
	panelURL    string
	auth        string
	email       string
	password    string
	apiKey      string
	accessToken string
	client      *http.Client
}

// NewLeCDNProvider 创建LeCDN部署器
func NewLeCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &LeCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
		panelURL:     strings.TrimSuffix(base.GetConfigString(config, "url"), "/"),
		auth:         base.GetConfigString(config, "auth"),
		email:        base.GetConfigString(config, "email"),
		password:     base.GetConfigString(config, "password"),
		apiKey:       base.GetConfigString(config, "api_key"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *LeCDNProvider) Check(ctx context.Context) error {
	if p.auth == "1" {
		if p.panelURL == "" || p.apiKey == "" {
			return fmt.Errorf("API访问令牌不能为空")
		}
		_, err := p.request(ctx, "GET", "/prod-api/system/info", nil)
		return err
	}

	if p.panelURL == "" || p.email == "" || p.password == "" {
		return fmt.Errorf("账号和密码不能为空")
	}
	return p.login(ctx)
}

func (p *LeCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	if p.auth == "0" {
		if err := p.login(ctx); err != nil {
			return err
		}
	}

	certID := base.GetConfigString(config, "id")
	if certID == "" {
		certID = p.GetString("id")
	}

	if certID == "" {
		certName, err := p.getCertName(fullchain)
		if err != nil {
			return err
		}

		params := map[string]interface{}{
			"name":         certName,
			"type":         "upload",
			"ssl_pem":      base64.StdEncoding.EncodeToString([]byte(fullchain)),
			"ssl_key":      base64.StdEncoding.EncodeToString([]byte(privateKey)),
			"auto_renewal": false,
		}

		resp, err := p.request(ctx, "POST", "/prod-api/certificate", params)
		if err != nil {
			return fmt.Errorf("添加证书失败: %v", err)
		}

		if id, ok := resp["id"].(float64); ok {
			p.Log(fmt.Sprintf("证书ID:%d添加成功！", int(id)))
		} else if id, ok := resp["id"].(string); ok {
			p.Log(fmt.Sprintf("证书ID:%s添加成功！", id))
		}
		return nil
	}

	// 获取已有证书信息
	resp, err := p.request(ctx, "GET", "/prod-api/certificate/"+certID, nil)
	if err != nil {
		return fmt.Errorf("证书ID:%s获取失败：%v", certID, err)
	}

	certName, _ := resp["name"].(string)
	description, _ := resp["description"].(string)

	params := map[string]interface{}{
		"id":           certID,
		"name":         certName,
		"description":  description,
		"type":         "upload",
		"ssl_pem":      base64.StdEncoding.EncodeToString([]byte(fullchain)),
		"ssl_key":      base64.StdEncoding.EncodeToString([]byte(privateKey)),
		"auto_renewal": false,
	}

	_, err = p.request(ctx, "PUT", "/prod-api/certificate/"+certID, params)
	if err != nil {
		return fmt.Errorf("证书ID:%s更新失败：%v", certID, err)
	}

	p.Log(fmt.Sprintf("证书ID:%s更新成功！", certID))
	return nil
}

func (p *LeCDNProvider) login(ctx context.Context) error {
	params := map[string]interface{}{
		"email":    p.email,
		"username": p.email,
		"password": p.password,
	}

	resp, err := p.request(ctx, "POST", "/prod-api/login", params)
	if err != nil {
		return err
	}

	if token, ok := resp["token"].(string); ok {
		p.accessToken = token
		return nil
	}
	return fmt.Errorf("登录成功，获取access_token失败")
}

func (p *LeCDNProvider) request(ctx context.Context, method, path string, params map[string]interface{}) (map[string]interface{}, error) {
	reqURL := p.panelURL + path

	var bodyReader io.Reader
	if params != nil {
		bodyBytes, _ := json.Marshal(params)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	if p.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.accessToken)
	} else if p.auth == "1" && p.apiKey != "" {
		req.Header.Set("Authorization", p.apiKey)
	}

	if params != nil {
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
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

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			return data, nil
		}
		return result, nil
	}

	if msg, ok := result["message"].(string); ok {
		return nil, fmt.Errorf("%s", msg)
	}
	return nil, fmt.Errorf("返回数据解析失败")
}

func (p *LeCDNProvider) getCertName(fullchain string) (string, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return "", fmt.Errorf("无法解析证书")
	}

	certObj, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("证书解析失败: %v", err)
	}

	cn := strings.ReplaceAll(certObj.Subject.CommonName, "*.", "")
	return fmt.Sprintf("%s-%d", cn, certObj.NotBefore.Unix()), nil
}

func (p *LeCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
