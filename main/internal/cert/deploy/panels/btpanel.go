package panels

import (
	"main/internal/cert/deploy/base"
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
)

func init() {
	base.Register("btpanel", NewBTPanelProvider)
}

type BTPanelProvider struct {
	base.BaseProvider
	client *http.Client
}

func NewBTPanelProvider(config map[string]interface{}) base.DeployProvider {
	return &BTPanelProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *BTPanelProvider) Check(ctx context.Context) error {
	panelURL := p.GetString("url")
	apiKey := p.GetString("api_key")
	if apiKey == "" {
		apiKey = p.GetString("key")
	}

	if panelURL == "" {
		return fmt.Errorf("面板地址不能为空")
	}
	if apiKey == "" {
		return fmt.Errorf("API密钥不能为空")
	}

	result, err := p.request(ctx, "/system?action=GetSystemTotal", nil)
	if err != nil {
		return err
	}

	if _, ok := result["system"]; !ok {
		return fmt.Errorf("无法获取系统信息，请检查API密钥")
	}

	return nil
}

func (p *BTPanelProvider) request(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	panelURL := strings.TrimSuffix(p.GetString("url"), "/")
	apiKey := p.GetString("api_key")
	if apiKey == "" {
		apiKey = p.GetString("key")
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	token := md5Hash(timestamp + md5Hash(apiKey))

	data := url.Values{}
	data.Set("request_time", timestamp)
	data.Set("request_token", token)
	for k, v := range params {
		data.Set(k, v)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", panelURL+path, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %s", string(body))
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

func md5Hash(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func (p *BTPanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "" {
		deployType = "0"
	}

	isIIS := base.GetConfigBool(config, "is_iis")
	if isIIS {
		return fmt.Errorf("IIS站点部署需要PFX证书，当前版本暂不支持")
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
		p.Log("正在部署面板证书")
		return p.deployPanel(ctx, fullchain, privateKey)
	case "2":
		if len(sites) == 0 {
			return fmt.Errorf("邮局域名不能为空")
		}
		for _, siteName := range sites {
			p.Log("正在部署邮局证书: " + siteName)
			if err := p.deployMailSys(ctx, siteName, fullchain, privateKey); err != nil {
				return err
			}
		}
		return nil
	case "3":
		if len(sites) == 0 {
			return fmt.Errorf("Docker站点名称不能为空")
		}
		for _, siteName := range sites {
			p.Log("正在部署Docker证书: " + siteName)
			if err := p.deployDocker(ctx, siteName, fullchain, privateKey); err != nil {
				return err
			}
		}
		return nil
	default:
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

func (p *BTPanelProvider) deployPanel(ctx context.Context, fullchain, privateKey string) error {
	version := p.GetString("version")
	if version == "1" {
		params := map[string]string{
			"ssl_key": privateKey,
			"ssl_pem": fullchain,
		}
		result, err := p.request(ctx, "/config/set_panel_ssl", params)
		if err != nil {
			return err
		}
		if status, ok := result["status"].(bool); !ok || !status {
			return fmt.Errorf("部署面板证书失败")
		}
		return nil
	}

	params := map[string]string{
		"privateKey": privateKey,
		"certPem":    fullchain,
	}
	result, err := p.request(ctx, "/config?action=SavePanelSSL", params)
	if err != nil {
		return err
	}
	if status, ok := result["status"].(bool); !ok || !status {
		return fmt.Errorf("部署面板证书失败")
	}
	return nil
}

func (p *BTPanelProvider) deploySite(ctx context.Context, siteName, fullchain, privateKey string) error {
	version := p.GetString("version")
	if version == "1" {
		siteID, err := p.getSiteIDWin(ctx, siteName)
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
		result, err := p.request(ctx, "/site/set_site_ssl", params)
		if err != nil {
			return err
		}
		if status, ok := result["status"].(bool); !ok || !status {
			return fmt.Errorf("部署证书失败")
		}
		return nil
	}

	params := map[string]string{
		"type":     "0",
		"siteName": siteName,
		"key":      privateKey,
		"csr":      fullchain,
	}
	result, err := p.request(ctx, "/site?action=SetSSL", params)
	if err != nil {
		return err
	}
	if status, ok := result["status"].(bool); !ok || !status {
		return fmt.Errorf("部署证书失败")
	}
	return nil
}

func (p *BTPanelProvider) deployMailSys(ctx context.Context, domain, fullchain, privateKey string) error {
	params := map[string]string{
		"domain": domain,
		"key":    privateKey,
		"csr":    fullchain,
		"act":    "add",
	}
	result, err := p.request(ctx, "/plugin?action=a&name=mail_sys&s=set_mail_certificate_multiple", params)
	if err != nil {
		return err
	}
	if status, ok := result["status"].(bool); !ok || !status {
		return fmt.Errorf("部署邮局证书失败")
	}
	return nil
}

func (p *BTPanelProvider) deployDocker(ctx context.Context, siteName, fullchain, privateKey string) error {
	params := map[string]string{
		"site_name": siteName,
		"key":       privateKey,
		"csr":       fullchain,
	}
	result, err := p.request(ctx, "/mod/docker/com/set_ssl", params)
	if err != nil {
		return err
	}
	if status, ok := result["status"].(bool); !ok || !status {
		return fmt.Errorf("部署Docker证书失败")
	}
	return nil
}

func (p *BTPanelProvider) getSiteIDWin(ctx context.Context, siteName string) (int, error) {
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
	if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
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

func (p *BTPanelProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
