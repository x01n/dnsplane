package providers

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("btwaf", NewBTWAFProvider)
}

// BTWAFProvider 堡塔云WAF部署器
type BTWAFProvider struct {
	base.BaseProvider
	panelURL string
	apiKey   string
	client   *http.Client
}

// NewBTWAFProvider 创建堡塔云WAF部署器
func NewBTWAFProvider(config map[string]interface{}) base.DeployProvider {
	return &BTWAFProvider{
		BaseProvider: base.BaseProvider{Config: config},
		panelURL:     strings.TrimSuffix(base.GetConfigString(config, "url"), "/"),
		apiKey:       base.GetConfigString(config, "key"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *BTWAFProvider) Check(ctx context.Context) error {
	if p.panelURL == "" || p.apiKey == "" {
		return fmt.Errorf("请填写面板地址和接口密钥")
	}

	resp, err := p.request(ctx, "/api/user/latest_version", nil)
	if err != nil {
		return fmt.Errorf("面板地址无法连接: %v", err)
	}

	if code, ok := resp["code"].(float64); ok && code == 0 {
		return nil
	}

	if res, ok := resp["res"].(string); ok {
		return fmt.Errorf("%s", res)
	}
	return fmt.Errorf("面板连接失败")
}

func (p *BTWAFProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
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

func (p *BTWAFProvider) deployPanel(ctx context.Context, fullchain, privateKey string) error {
	params := map[string]interface{}{
		"certContent": fullchain,
		"keyContent":  privateKey,
	}

	resp, err := p.request(ctx, "/api/config/set_cert", params)
	if err != nil {
		return err
	}

	if code, ok := resp["code"].(float64); ok && code == 0 {
		p.Log("面板证书部署成功")
		return nil
	}

	if res, ok := resp["res"].(string); ok {
		return fmt.Errorf("%s", res)
	}
	return fmt.Errorf("返回数据解析失败")
}

func (p *BTWAFProvider) deploySite(ctx context.Context, siteName, fullchain, privateKey string) error {
	// 获取网站列表查找site_id
	params := map[string]interface{}{
		"p":         1,
		"p_size":    10,
		"site_name": siteName,
	}

	resp, err := p.request(ctx, "/api/wafmastersite/get_site_list", params)
	if err != nil {
		return err
	}

	if code, ok := resp["code"].(float64); ok && code != 0 {
		if res, ok := resp["res"].(string); ok {
			return fmt.Errorf("%s", res)
		}
		return fmt.Errorf("获取网站列表失败")
	}

	resData, ok := resp["res"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("返回数据解析失败")
	}

	siteList, ok := resData["list"].([]interface{})
	if !ok {
		return fmt.Errorf("返回数据解析失败")
	}

	var siteID string
	listenSSLPort := []string{"443"}

	for _, s := range siteList {
		site := s.(map[string]interface{})
		if site["site_name"].(string) == siteName {
			siteID = site["site_id"].(string)
			if server, ok := site["server"].(map[string]interface{}); ok {
				if ports, ok := server["listen_ssl_port"].([]interface{}); ok && len(ports) > 0 {
					listenSSLPort = make([]string, len(ports))
					for i, port := range ports {
						listenSSLPort[i] = fmt.Sprintf("%v", port)
					}
				}
			}
			break
		}
	}

	if siteID == "" {
		return fmt.Errorf("网站名称不存在")
	}

	// 配置SSL证书
	params = map[string]interface{}{
		"types":   "openCert",
		"site_id": siteID,
		"server": map[string]interface{}{
			"listen_ssl_port": listenSSLPort,
			"ssl": map[string]interface{}{
				"is_ssl":      1,
				"private_key": privateKey,
				"full_chain":  fullchain,
			},
		},
	}

	resp, err = p.request(ctx, "/api/wafmastersite/modify_site", params)
	if err != nil {
		return err
	}

	if code, ok := resp["code"].(float64); ok && code == 0 {
		return nil
	}

	if res, ok := resp["res"].(string); ok {
		return fmt.Errorf("%s", res)
	}
	return fmt.Errorf("返回数据解析失败")
}

func (p *BTWAFProvider) request(ctx context.Context, path string, params map[string]interface{}) (map[string]interface{}, error) {
	reqURL := p.panelURL + path

	var bodyReader io.Reader
	if params != nil {
		bodyBytes, _ := json.Marshal(params)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 生成签名: md5(timestamp + md5(key))
	nowTime := time.Now().Unix()
	keyMD5 := md5.Sum([]byte(p.apiKey))
	token := md5.Sum([]byte(fmt.Sprintf("%d%s", nowTime, hex.EncodeToString(keyMD5[:]))))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("waf_request_time", fmt.Sprintf("%d", nowTime))
	req.Header.Set("waf_request_token", hex.EncodeToString(token[:]))

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

func (p *BTWAFProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
