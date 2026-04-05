package providers

import (
	"context"
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
	base.Register("opanel", NewOPanelProvider)
}

// OPanelProvider 1Panel证书部署
type OPanelProvider struct {
	base.BaseProvider
	client *http.Client
}

// NewOPanelProvider creates a new OPanelProvider
func NewOPanelProvider(config map[string]interface{}) base.DeployProvider {
	return &OPanelProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// httpRequest sends an HTTP request and parses JSON response
func (p *OPanelProvider) httpRequest(ctx context.Context, method, reqURL string, body interface{}, headers map[string]string) (map[string]interface{}, error) {
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

// request sends an authenticated request to 1Panel
// Authentication: 1Panel-Token = md5('1panel' + key + timestamp)
func (p *OPanelProvider) request(ctx context.Context, path string, params map[string]interface{}) (map[string]interface{}, error) {
	panelURL := strings.TrimSuffix(p.GetString("url"), "/")
	apiKey := p.GetString("key")
	version := p.GetString("version")
	if version == "" {
		version = "v2"
	}

	fullURL := fmt.Sprintf("%s/api/%s%s", panelURL, version, path)

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	token := md5Hex("1panel" + apiKey + timestamp)

	headers := map[string]string{
		"Content-Type":     "application/json",
		"1Panel-Token":     token,
		"1Panel-Timestamp": timestamp,
	}

	var bodyData interface{}
	if params != nil {
		bodyData = params
	} else {
		bodyData = map[string]interface{}{}
	}

	result, err := p.httpRequest(ctx, "POST", fullURL, bodyData, headers)
	if err != nil {
		return nil, err
	}

	// 1Panel wraps response as { "code": 200, "data": {...}, "message": "..." }
	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			return data, nil
		}
		return nil, nil
	}

	if msg, ok := result["message"].(string); ok && msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}

	return nil, fmt.Errorf("请求失败")
}

// Check verifies connection to 1Panel
func (p *OPanelProvider) Check(ctx context.Context) error {
	panelURL := p.GetString("url")
	apiKey := p.GetString("key")

	if panelURL == "" {
		return fmt.Errorf("面板地址不能为空")
	}
	if apiKey == "" {
		return fmt.Errorf("接口密钥不能为空")
	}

	_, err := p.request(ctx, "/settings/search", nil)
	return err
}

// Deploy deploys certificate to 1Panel
func (p *OPanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "" {
		deployType = p.GetString("type")
	}
	if deployType == "" {
		deployType = "0"
	}

	switch deployType {
	case "3":
		// 面板证书
		p.Log("正在部署面板证书")
		return p.deployPanel(ctx, fullchain, privateKey)
	default:
		// 证书管理
		return p.deployCert(ctx, fullchain, privateKey, config)
	}
}

// deployPanel deploys certificate to 1Panel itself
func (p *OPanelProvider) deployPanel(ctx context.Context, fullchain, privateKey string) error {
	params := map[string]interface{}{
		"cert":    fullchain,
		"key":     privateKey,
		"ssl":     "Enable",
		"sslID":   nil,
		"sslType": "import-paste",
	}

	_, err := p.request(ctx, "/core/settings/ssl/update", params)
	if err != nil {
		return fmt.Errorf("面板证书更新失败: %v", err)
	}
	p.Log("面板证书更新成功")
	return nil
}

// deployCert searches for matching certificates by domain and updates them
func (p *OPanelProvider) deployCert(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	// Check if a specific cert ID is provided
	certID := base.GetConfigString(config, "id")
	if certID == "" {
		certID = p.GetString("id")
	}
	if certID != "" {
		return p.updateCertByID(ctx, fullchain, privateKey, certID)
	}

	// Otherwise, search by domain
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("没有设置要部署的域名")
	}

	// Get cert list
	listParams := map[string]interface{}{
		"page":     1,
		"pageSize": 500,
	}
	data, err := p.request(ctx, "/websites/ssl/search", listParams)
	if err != nil {
		return fmt.Errorf("获取证书列表失败: %v", err)
	}

	total := 0
	if t, ok := data["total"].(float64); ok {
		total = int(t)
	}
	p.Log(fmt.Sprintf("获取证书列表成功(total=%d)", total))

	success := 0
	var lastErr error

	if items, ok := data["items"].([]interface{}); ok {
		for _, item := range items {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			primaryDomain, _ := row["primaryDomain"].(string)
			if primaryDomain == "" {
				continue
			}

			// Collect certificate domains
			certDomains := []string{primaryDomain}
			if domainsStr, ok := row["domains"].(string); ok && domainsStr != "" {
				certDomains = append(certDomains, strings.Split(domainsStr, ",")...)
			}

			// Check if any cert domain matches a target domain
			matched := false
			for _, certDomain := range certDomains {
				for _, targetDomain := range domains {
					if certDomain == targetDomain {
						matched = true
						break
					}
					// Wildcard match
					if strings.HasPrefix(targetDomain, "*.") {
						wildcardSuffix := targetDomain[1:] // e.g. ".example.com"
						if strings.HasSuffix(certDomain, wildcardSuffix) || certDomain == targetDomain[2:] {
							matched = true
							break
						}
					}
				}
				if matched {
					break
				}
			}

			if matched {
				id, _ := row["id"].(float64)
				params := map[string]interface{}{
					"sslID":       int(id),
					"type":        "paste",
					"certificate": fullchain,
					"privateKey":  privateKey,
					"description": "",
				}
				_, err := p.request(ctx, "/websites/ssl/upload", params)
				if err != nil {
					lastErr = err
					p.Log(fmt.Sprintf("证书ID:%d更新失败: %v", int(id), err))
				} else {
					p.Log(fmt.Sprintf("证书ID:%d更新成功", int(id)))
					success++
				}
			}
		}
	}

	// If no cert matched, upload as new
	if success == 0 && lastErr == nil {
		params := map[string]interface{}{
			"sslID":       0,
			"type":        "paste",
			"certificate": fullchain,
			"privateKey":  privateKey,
			"description": "",
		}
		_, err := p.request(ctx, "/websites/ssl/upload", params)
		if err != nil {
			return fmt.Errorf("证书上传失败: %v", err)
		}
		p.Log("证书上传成功")
	}

	if success == 0 && lastErr != nil {
		return lastErr
	}

	return nil
}

// updateCertByID updates a certificate by its ID directly
func (p *OPanelProvider) updateCertByID(ctx context.Context, fullchain, privateKey, certID string) error {
	params := map[string]interface{}{
		"sslID":       certID,
		"type":        "paste",
		"certificate": fullchain,
		"privateKey":  privateKey,
		"description": "",
	}

	_, err := p.request(ctx, "/websites/ssl/upload", params)
	if err != nil {
		return fmt.Errorf("证书ID:%s更新失败: %v", certID, err)
	}
	p.Log(fmt.Sprintf("证书ID:%s更新成功", certID))
	return nil
}

// SetLogger sets the logger
func (p *OPanelProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
