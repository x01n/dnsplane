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
	base.Register("safeline", NewSafeLineProvider)
}

// SafeLineProvider 雷池WAF证书部署
type SafeLineProvider struct {
	base.BaseProvider
	client *http.Client
}

// NewSafeLineProvider creates a new SafeLineProvider
func NewSafeLineProvider(config map[string]interface{}) base.DeployProvider {
	return &SafeLineProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// httpRequest sends an HTTP request and parses JSON response
func (p *SafeLineProvider) httpRequest(ctx context.Context, method, reqURL string, body interface{}, headers map[string]string) (map[string]interface{}, error) {
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

// request sends an authenticated request to SafeLine WAF
// Authentication: X-SLCE-API-TOKEN header
func (p *SafeLineProvider) request(ctx context.Context, path string, params map[string]interface{}) (map[string]interface{}, error) {
	baseURL := strings.TrimSuffix(p.GetString("url"), "/")
	token := p.GetString("token")

	method := "GET"
	var body interface{}
	headers := map[string]string{
		"X-SLCE-API-TOKEN": token,
	}

	if params != nil {
		method = "POST"
		body = params
		headers["Content-Type"] = "application/json"
	}

	result, err := p.httpRequest(ctx, method, baseURL+path, body, headers)
	if err != nil {
		return nil, err
	}

	// SafeLine wraps response in "data" field
	if data, ok := result["data"].(map[string]interface{}); ok {
		return data, nil
	}

	return result, nil
}

// Check verifies connection to SafeLine WAF
func (p *SafeLineProvider) Check(ctx context.Context) error {
	url := p.GetString("url")
	token := p.GetString("token")

	if url == "" || token == "" {
		return fmt.Errorf("请填写控制台地址和API Token")
	}

	_, err := p.request(ctx, "/api/open/system", nil)
	return err
}

// Deploy deploys certificate to SafeLine WAF
// Lists existing certs, matches by domain, updates matched certs or creates new
func (p *SafeLineProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		domainsStr := p.GetString("domainList")
		if domainsStr != "" {
			domains = base.SplitDomains(domainsStr)
		}
	}
	if len(domains) == 0 {
		return fmt.Errorf("没有设置要部署的域名")
	}

	// Get cert list
	data, err := p.request(ctx, "/api/open/cert", nil)
	if err != nil {
		return fmt.Errorf("获取证书列表失败: %v", err)
	}

	nodes, ok := data["nodes"].([]interface{})
	if !ok {
		nodes = []interface{}{}
	}

	p.Log(fmt.Sprintf("获取证书列表成功(total=%v)", data["total"]))

	successCount := 0

	for _, node := range nodes {
		row, ok := node.(map[string]interface{})
		if !ok {
			continue
		}

		rowDomains, ok := row["domains"].([]interface{})
		if !ok || len(rowDomains) == 0 {
			continue
		}

		// Check if any cert domain matches a target domain
		matched := false
		for _, rd := range rowDomains {
			rDomain, ok := rd.(string)
			if !ok {
				continue
			}
			for _, target := range domains {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}
				// Exact match
				if rDomain == target {
					matched = true
					break
				}
				// Wildcard match: check if *.suffix matches
				dotIdx := strings.Index(rDomain, ".")
				if dotIdx != -1 {
					wildcard := "*" + rDomain[dotIdx:]
					if wildcard == target {
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
			id := int(row["id"].(float64))
			params := map[string]interface{}{
				"id": id,
				"manual": map[string]string{
					"crt": fullchain,
					"key": privateKey,
				},
				"type": 2,
			}

			if _, err := p.request(ctx, "/api/open/cert", params); err != nil {
				p.Log(fmt.Sprintf("证书ID:%d 更新失败: %v", id, err))
			} else {
				p.Log(fmt.Sprintf("证书ID:%d 更新成功", id))
				successCount++
			}
		}
	}

	// If no cert matched, upload as new
	if successCount == 0 {
		params := map[string]interface{}{
			"manual": map[string]string{
				"crt": fullchain,
				"key": privateKey,
			},
			"type": 2,
		}
		if _, err := p.request(ctx, "/api/open/cert", params); err != nil {
			return fmt.Errorf("证书上传失败: %v", err)
		}
		p.Log("证书上传成功")
	}

	return nil
}

// SetLogger sets the logger
func (p *SafeLineProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
