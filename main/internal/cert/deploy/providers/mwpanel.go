package providers

import (
	"context"
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
	base.Register("mwpanel", NewMWPanelProvider)
}

// MWPanelProvider MW面板证书部署
type MWPanelProvider struct {
	base.BaseProvider
	panelURL  string
	appID     string
	appSecret string
	client    *http.Client
}

// NewMWPanelProvider creates a new MWPanelProvider
func NewMWPanelProvider(config map[string]interface{}) base.DeployProvider {
	return &MWPanelProvider{
		BaseProvider: base.BaseProvider{Config: config},
		panelURL:     strings.TrimSuffix(base.GetConfigString(config, "url"), "/"),
		appID:        base.GetConfigString(config, "appid"),
		appSecret:    base.GetConfigString(config, "appsecret"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// httpRequest sends an HTTP request and parses JSON response
func (p *MWPanelProvider) httpRequest(ctx context.Context, method, reqURL string, body interface{}, headers map[string]string) (map[string]interface{}, error) {
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

// request sends an authenticated request to MW panel
// Authentication: app-id and app-secret headers
func (p *MWPanelProvider) request(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	reqURL := p.panelURL + path

	headers := map[string]string{
		"app-id":     p.appID,
		"app-secret": p.appSecret,
	}

	var body interface{}
	if params != nil {
		data := url.Values{}
		for k, v := range params {
			data.Set(k, v)
		}
		body = data.Encode()
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	}

	return p.httpRequest(ctx, "POST", reqURL, body, headers)
}

// Check verifies connection to MW panel
func (p *MWPanelProvider) Check(ctx context.Context) error {
	if p.panelURL == "" || p.appID == "" || p.appSecret == "" {
		return fmt.Errorf("请填写面板地址和接口密钥")
	}

	resp, err := p.request(ctx, "/task/count", nil)
	if err != nil {
		return fmt.Errorf("面板地址无法连接: %v", err)
	}

	if status, ok := resp["status"].(bool); ok && status {
		return nil
	}

	if msg, ok := resp["msg"].(string); ok {
		return fmt.Errorf("%s", msg)
	}

	return fmt.Errorf("面板连接失败")
}

// Deploy deploys certificate to MW panel
func (p *MWPanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "" {
		deployType = p.GetString("type")
	}

	if deployType == "1" {
		// 面板证书
		p.Log("正在部署面板证书")
		return p.deployPanel(ctx, fullchain, privateKey)
	}

	// 网站证书
	sites := base.GetConfigString(config, "sites")
	if sites == "" {
		sites = p.GetString("sites")
	}

	siteList := base.SplitDomains(sites)
	if len(siteList) == 0 {
		return fmt.Errorf("网站名称不能为空")
	}

	var success int
	var lastErr error

	for _, site := range siteList {
		err := p.deploySite(ctx, site, fullchain, privateKey)
		if err != nil {
			lastErr = err
			p.Log(fmt.Sprintf("网站 %s 证书部署失败: %v", site, err))
		} else {
			p.Log(fmt.Sprintf("网站 %s 证书部署成功", site))
			success++
		}
	}

	if success == 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("要部署的网站不存在")
	}

	return nil
}

// deployPanel deploys certificate to MW panel itself
func (p *MWPanelProvider) deployPanel(ctx context.Context, fullchain, privateKey string) error {
	params := map[string]string{
		"privateKey": privateKey,
		"certPem":    fullchain,
		"choose":     "local",
	}

	resp, err := p.request(ctx, "/setting/save_panel_ssl", params)
	if err != nil {
		return err
	}

	if status, ok := resp["status"].(bool); ok && status {
		p.Log("面板证书部署成功")
		return nil
	}

	if msg, ok := resp["msg"].(string); ok {
		return fmt.Errorf("%s", msg)
	}

	return fmt.Errorf("返回数据解析失败")
}

// deploySite deploys certificate to a website
func (p *MWPanelProvider) deploySite(ctx context.Context, siteName, fullchain, privateKey string) error {
	params := map[string]string{
		"type":     "1",
		"siteName": siteName,
		"key":      privateKey,
		"csr":      fullchain,
	}

	resp, err := p.request(ctx, "/site/set_ssl", params)
	if err != nil {
		return err
	}

	if status, ok := resp["status"].(bool); ok && status {
		return nil
	}

	if msg, ok := resp["msg"].(string); ok {
		return fmt.Errorf("%s", msg)
	}

	return fmt.Errorf("返回数据解析失败")
}

// SetLogger sets the logger
func (p *MWPanelProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
