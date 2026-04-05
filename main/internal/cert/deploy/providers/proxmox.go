package providers

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("proxmox", NewProxmoxProvider)
}

// ProxmoxProvider Proxmox VE证书部署器
type ProxmoxProvider struct {
	base.BaseProvider
	client *http.Client
}

// NewProxmoxProvider 创建Proxmox部署器
func NewProxmoxProvider(config map[string]interface{}) base.DeployProvider {
	return &ProxmoxProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// Check 检查PVE API连通性
func (p *ProxmoxProvider) Check(ctx context.Context) error {
	if p.GetString("url") == "" || p.GetString("api_user") == "" || p.GetString("api_key") == "" {
		return fmt.Errorf("面板地址、API令牌ID和API密钥不能为空")
	}
	_, err := p.pveRequest(ctx, "GET", "/api2/json/access", nil)
	return err
}

// Deploy 部署证书到PVE节点
func (p *ProxmoxProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	node := base.GetConfigString(config, "node")
	if node == "" {
		node = p.GetString("node")
	}
	if node == "" {
		return fmt.Errorf("节点名称不能为空")
	}

	// 计算新证书指纹
	certHash, err := pveCertFingerprint(fullchain)
	if err != nil {
		return err
	}

	// 获取节点现有证书列表，检查是否已部署相同证书
	infoPath := fmt.Sprintf("/api2/json/nodes/%s/certificates/info", node)
	data, err := p.pveRequest(ctx, "GET", infoPath, nil)
	if err == nil {
		if certList, ok := data["data"].([]interface{}); ok {
			for _, item := range certList {
				if certInfo, ok := item.(map[string]interface{}); ok {
					if fp, ok := certInfo["fingerprint"].(string); ok {
						existingHash := strings.ToLower(strings.ReplaceAll(fp, ":", ""))
						if existingHash == certHash {
							p.Log(fmt.Sprintf("节点 %s 证书已是最新，跳过部署", node))
							return nil
						}
					}
				}
			}
		}
	}

	// 上传证书到节点
	p.Log(fmt.Sprintf("正在部署证书到节点: %s", node))
	uploadPath := fmt.Sprintf("/api2/json/nodes/%s/certificates/custom", node)
	params := url.Values{}
	params.Set("certificates", fullchain)
	params.Set("key", privateKey)
	params.Set("force", "1")
	params.Set("restart", "1")

	_, err = p.pveRequest(ctx, "POST", uploadPath, params)
	if err != nil {
		return fmt.Errorf("证书部署失败: %v", err)
	}

	p.Log(fmt.Sprintf("节点 %s 证书部署成功", node))
	return nil
}

// pveRequest 发送PVE API请求（PVEAPIToken认证）
func (p *ProxmoxProvider) pveRequest(ctx context.Context, method, path string, params url.Values) (map[string]interface{}, error) {
	panelURL := strings.TrimSuffix(p.GetString("url"), "/")
	apiUser := p.GetString("api_user")
	apiKey := p.GetString("api_key")

	fullURL := panelURL + path

	var body io.Reader
	if params != nil {
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", apiUser, apiKey))
	if params != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("请求失败(HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	// 检查API返回的错误
	if errors, ok := result["errors"]; ok {
		if errStr, ok := errors.(string); ok {
			return nil, fmt.Errorf("%s", errStr)
		}
		if errMap, ok := errors.(map[string]interface{}); ok {
			var msgs []string
			for _, v := range errMap {
				if s, ok := v.(string); ok {
					msgs = append(msgs, s)
				}
			}
			if len(msgs) > 0 {
				return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
			}
		}
	}

	return result, nil
}

// pveCertFingerprint 计算证书SHA256指纹
func pveCertFingerprint(fullchain string) (string, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return "", fmt.Errorf("无法解析证书PEM")
	}

	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("证书解析失败: %v", err)
	}

	hash := sha256.Sum256(c.Raw)
	return hex.EncodeToString(hash[:]), nil
}

// SetLogger 设置日志记录器
func (p *ProxmoxProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
