package providers

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
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
	base.Register("btpanel", NewBTPanelProvider)
}

// md5Hex returns the MD5 hex digest of a string
func md5Hex(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// BTPanelProvider 宝塔面板证书部署
type BTPanelProvider struct {
	base.BaseProvider
	client *http.Client
}

// NewBTPanelProvider creates a new BTPanelProvider
func NewBTPanelProvider(config map[string]interface{}) base.DeployProvider {
	return &BTPanelProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// httpRequest sends an HTTP request and parses JSON response
func (p *BTPanelProvider) httpRequest(ctx context.Context, method, reqURL string, body interface{}, headers map[string]string) (map[string]interface{}, error) {
	var bodyReader io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			bodyReader = strings.NewReader(v)
		default:
			data, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("序列化请求体失败: %v", err)
			}
			bodyReader = strings.NewReader(string(data))
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
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

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败(status=%d): %s", resp.StatusCode, string(respBody))
	}

	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("HTTP请求失败(status=%d)", resp.StatusCode)
		if m, ok := result["msg"].(string); ok && m != "" {
			msg += ": " + m
		} else if m, ok := result["message"].(string); ok && m != "" {
			msg += ": " + m
		}
		return nil, fmt.Errorf("%s", msg)
	}

	return result, nil
}

// request sends an authenticated request to the BT panel
// Authentication: request_token = md5(timestamp + md5(key))
func (p *BTPanelProvider) request(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	panelURL := strings.TrimSuffix(p.GetString("url"), "/")
	apiKey := p.GetString("key")
	if apiKey == "" {
		apiKey = p.GetString("api_key")
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	token := md5Hex(timestamp + md5Hex(apiKey))

	data := url.Values{}
	data.Set("request_time", timestamp)
	data.Set("request_token", token)
	for k, v := range params {
		data.Set(k, v)
	}

	headers := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}

	result, err := p.httpRequest(ctx, "POST", panelURL+path, data.Encode(), headers)
	if err != nil {
		return nil, err
	}

	if status, ok := result["status"].(bool); ok && !status {
		msg := "未知错误"
		if m, ok := result["msg"].(string); ok {
			msg = m
		}
		return nil, fmt.Errorf("宝塔API错误: %s", msg)
	}

	return result, nil
}

// Check verifies connection to the BT panel
// v0: /config?action=get_config | v1: /config/get_config
func (p *BTPanelProvider) Check(ctx context.Context) error {
	panelURL := p.GetString("url")
	apiKey := p.GetString("key")
	if apiKey == "" {
		apiKey = p.GetString("api_key")
	}

	if panelURL == "" {
		return fmt.Errorf("面板地址不能为空")
	}
	if apiKey == "" {
		return fmt.Errorf("API密钥不能为空")
	}

	version := p.GetString("version")
	if version == "1" {
		_, err := p.request(ctx, "/config/get_config", nil)
		return err
	}
	_, err := p.request(ctx, "/config?action=get_config", nil)
	return err
}

// Deploy deploys certificate to BT panel
func (p *BTPanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "" {
		deployType = "0"
	}

	var sites []string
	if sitesStr := base.GetConfigString(config, "sites"); sitesStr != "" {
		sites = base.SplitDomains(sitesStr)
	}
	if len(sites) == 0 {
		if domain := base.GetConfigString(config, "domain"); domain != "" {
			sites = base.SplitDomains(domain)
		}
	}

	switch deployType {
	case "1":
		// 面板证书
		p.Log("正在部署面板证书")
		return p.deployPanel(ctx, fullchain, privateKey)
	default:
		// 网站证书
		if len(sites) == 0 {
			return fmt.Errorf("站点名称不能为空")
		}
		for _, siteName := range sites {
			p.Log("正在部署证书到站点: " + siteName)
			if err := p.deploySite(ctx, siteName, fullchain, privateKey); err != nil {
				return err
			}
		}
		return nil
	}
}

// deployPanel deploys certificate to panel itself
// v0: /config?action=SavePanelSSL | v1: /config/set_panel_ssl
func (p *BTPanelProvider) deployPanel(ctx context.Context, fullchain, privateKey string) error {
	version := p.GetString("version")
	if version == "1" {
		params := map[string]string{
			"ssl_key": privateKey,
			"ssl_pem": fullchain,
		}
		_, err := p.request(ctx, "/config/set_panel_ssl", params)
		if err != nil {
			return fmt.Errorf("部署面板证书失败: %v", err)
		}
		p.Log("面板证书部署成功")
		return nil
	}

	params := map[string]string{
		"privateKey": privateKey,
		"certPem":    fullchain,
	}
	_, err := p.request(ctx, "/config?action=SavePanelSSL", params)
	if err != nil {
		return fmt.Errorf("部署面板证书失败: %v", err)
	}
	p.Log("面板证书部署成功")
	return nil
}

// deploySite deploys certificate to a website
// v0: /site?action=SetSSL | v1: /site/set_site_ssl (requires site ID lookup)
func (p *BTPanelProvider) deploySite(ctx context.Context, siteName, fullchain, privateKey string) error {
	version := p.GetString("version")
	if version == "1" {
		siteID, err := p.getSiteID(ctx, siteName)
		if err != nil {
			return err
		}
		params := map[string]string{
			"siteid":  fmt.Sprintf("%d", siteID),
			"status":  "true",
			"sslType": "",
			"cert":    fullchain,
			"key":     privateKey,
		}
		_, err = p.request(ctx, "/site/set_site_ssl", params)
		if err != nil {
			return fmt.Errorf("部署证书到站点 %s 失败: %v", siteName, err)
		}
		p.Log(fmt.Sprintf("站点 %s 证书部署成功", siteName))
		return nil
	}

	params := map[string]string{
		"type":     "0",
		"siteName": siteName,
		"key":      privateKey,
		"csr":      fullchain,
	}
	_, err := p.request(ctx, "/site?action=SetSSL", params)
	if err != nil {
		return fmt.Errorf("部署证书到站点 %s 失败: %v", siteName, err)
	}
	p.Log(fmt.Sprintf("站点 %s 证书部署成功", siteName))
	return nil
}

// getSiteID searches for a site by name and returns its ID (v1 only)
func (p *BTPanelProvider) getSiteID(ctx context.Context, siteName string) (int, error) {
	params := map[string]string{
		"table":       "sites",
		"search_type": "PHP",
		"search":      siteName,
		"p":           "1",
		"limit":       "10",
		"type":        "-1",
	}
	result, err := p.request(ctx, "/datalist/get_data_list", params)
	if err != nil {
		return 0, err
	}
	if data, ok := result["data"].([]interface{}); ok {
		for _, item := range data {
			if site, ok := item.(map[string]interface{}); ok {
				if name, ok := site["name"].(string); ok && name == siteName {
					if id, ok := site["id"].(float64); ok {
						return int(id), nil
					}
				}
			}
		}
	}
	return 0, fmt.Errorf("站点不存在: %s", siteName)
}

// SetLogger sets the logger
func (p *BTPanelProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
