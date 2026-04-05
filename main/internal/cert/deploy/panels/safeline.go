package panels

import (
	"main/internal/cert/deploy/base"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
)

func init() {
	base.Register("safeline", NewSafeLineProvider)
}

type SafeLineProvider struct {
	base.BaseProvider
	client *http.Client
}

func NewSafeLineProvider(config map[string]interface{}) base.DeployProvider {
	return &SafeLineProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *SafeLineProvider) Check(ctx context.Context) error {
	url := p.GetString("url")
	token := p.GetString("token")

	if url == "" || token == "" {
		return fmt.Errorf("请填写控制台地址和API Token")
	}

	_, err := p.request(ctx, "/api/open/system", nil)
	return err
}

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

	// 1. Get cert list
	data, err := p.request(ctx, "/api/open/cert", nil)
	if err != nil {
		return fmt.Errorf("获取证书列表失败: %v", err)
	}

	nodes, ok := data["nodes"].([]interface{})
	if !ok {
		// Maybe empty?
		nodes = []interface{}{}
	}

	p.Log(fmt.Sprintf("获取证书列表成功(total=%v)", data["total"]))

	successCount := 0

	for _, node := range nodes {
		row := node.(map[string]interface{})
		rowDomains, ok := row["domains"].([]interface{})
		if !ok || len(rowDomains) == 0 {
			continue
		}

		flag := false
		for _, rd := range rowDomains {
			rDomain := rd.(string)
			for _, target := range domains {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}
				// Check exact match or wildcard match
				if rDomain == target {
					flag = true
					break
				}
				// Simple wildcard check logic from PHP:
				// in_array('*' . substr($domain, strpos($domain, '.')), $domains)
				// Here rDomain is from existing cert. target is what we want to deploy.
				// If existing cert covers our domain?
				// PHP logic: if existing cert's domain matches one of our target domains.
				// "if (in_array($domain, $domains) || in_array('*' . substr($domain, strpos($domain, '.')), $domains))"
				// This checks if `rDomain` is in `domains` OR `*.suffix(rDomain)` is in `domains`.

				dotIdx := strings.Index(rDomain, ".")
				if dotIdx != -1 {
					wildcard := "*" + rDomain[dotIdx:]
					if wildcard == target {
						flag = true
						break
					}
				}
			}
			if flag {
				break
			}
		}

		if flag {
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
				p.Log(fmt.Sprintf("证书ID:%d 更新失败：%v", id, err))
			} else {
				p.Log(fmt.Sprintf("证书ID:%d 更新成功！", id))
				successCount++
			}
		}
	}

	if successCount == 0 {
		params := map[string]interface{}{
			"manual": map[string]string{
				"crt": fullchain,
				"key": privateKey,
			},
			"type": 2,
		}
		if _, err := p.request(ctx, "/api/open/cert", params); err != nil {
			return fmt.Errorf("证书上传失败：%v", err)
		}
		p.Log("证书上传成功！")
	}

	return nil
}

func (p *SafeLineProvider) request(ctx context.Context, path string, params map[string]interface{}) (map[string]interface{}, error) {
	baseURL := strings.TrimRight(p.GetString("url"), "/")
	u := baseURL + path
	token := p.GetString("token")

	var body io.Reader
	method := "GET"
	if params != nil {
		method = "POST"
		jsonBytes, _ := json.Marshal(params)
		body = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-SLCE-API-TOKEN", token)
	if params != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code int                    `json:"code"`
		Msg  string                 `json:"msg"`
		Data map[string]interface{} `json:"data"`
	}
	// Note: Safeline API might return different structure? PHP assumes result['data'].
	// PHP: if ($response['code'] == 200 && $result)
	// But Safeline API response body usually has code/msg/data? Or just data?
	// PHP code: `return $result['data'] ?? null;` if httpCode==200.
	// But it checks `$result`.

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	// Wait, standard Safeline response?
	// If HTTP 200, we assume success?
	// PHP code throws exception if `!empty($result['msg'])`.
	// So likely it returns { "msg": "error" } on failure even with 200?
	// Or maybe PHP `http_request` helper handles non-200.

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("请求失败(httpCode=%d): %s", resp.StatusCode, result.Msg)
	}

	if result.Msg != "" && result.Msg != "success" && result.Msg != "ok" { // Heuristic
		// But PHP code: `throw new Exception(!empty($result['msg']) ? $result['msg'] : ...)`
		// ONLY if `$response['code'] != 200` OR `!$result`.
		// Wait, PHP: `if ($response['code'] == 200 && $result) { return ... } else { throw ... }`
		// So if HTTP 200, it returns data.
		return result.Data, nil
	}

	return result.Data, nil
}

func (p *SafeLineProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
