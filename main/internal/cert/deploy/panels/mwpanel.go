package panels

import (
	"main/internal/cert/deploy/base"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	base.Register("mwpanel", NewMWPanelProvider)

	cert.Register("mwpanel", nil, cert.ProviderConfig{
		Type:     "mwpanel",
		Name:     "MW面板",
		Icon:     "mwpanel.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true, Placeholder: "https://panel.example.com"},
			{Name: "App ID", Key: "appid", Type: "input", Required: true, Placeholder: "MW面板App ID"},
			{Name: "App Secret", Key: "appsecret", Type: "input", Required: true, Placeholder: "MW面板App Secret"},
			{Name: "部署类型", Key: "type", Type: "select", Options: []cert.ConfigOption{
				{Value: "0", Label: "网站证书"},
				{Value: "1", Label: "面板证书"},
			}, Value: "0"},
			{Name: "网站列表", Key: "sites", Type: "textarea", Note: "每行一个网站名称（部署网站证书时填写）"},
		},
	})
}

type MWPanelProvider struct {
	base.BaseProvider
	panelURL  string
	appID     string
	appSecret string
	client    *http.Client
}

func NewMWPanelProvider(config map[string]interface{}) base.DeployProvider {
	return &MWPanelProvider{
		BaseProvider: base.BaseProvider{Config: config},
		panelURL:     strings.TrimSuffix(base.GetConfigString(config, "url"), "/"),
		appID:        base.GetConfigString(config, "appid"),
		appSecret:    base.GetConfigString(config, "appsecret"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

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

func (p *MWPanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "" {
		deployType = p.GetString("type")
	}

	if deployType == "1" {
		return p.deployPanel(ctx, fullchain, privateKey)
	}

	sites := base.GetConfigString(config, "sites")
	if sites == "" {
		sites = p.GetString("sites")
	}

	siteList := strings.Split(sites, "\n")
	var success int
	var lastErr error

	for _, site := range siteList {
		site = strings.TrimSpace(site)
		if site == "" {
			continue
		}

		err := p.deploySite(ctx, site, fullchain, privateKey)
		if err != nil {
			lastErr = err
			p.Log(fmt.Sprintf("网站 %s 证书部署失败：%v", site, err))
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

func (p *MWPanelProvider) request(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	reqURL := p.panelURL + path

	var bodyReader io.Reader
	if params != nil {
		data := url.Values{}
		for k, v := range params {
			data.Set(k, v)
		}
		bodyReader = strings.NewReader(data.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("app-id", p.appID)
	req.Header.Set("app-secret", p.appSecret)
	if params != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

	return result, nil
}

func (p *MWPanelProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
