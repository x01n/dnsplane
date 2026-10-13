package providers

import (
	"bytes"
	"context"
	"encoding/base64"
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
	base.Register("lucky", NewLuckyProvider)
}

// LuckyProvider Lucky证书部署器
type LuckyProvider struct {
	base.BaseProvider
	panelURL  string
	openToken string
	client    *http.Client
}

// NewLuckyProvider 创建Lucky部署器
func NewLuckyProvider(config map[string]interface{}) base.DeployProvider {
	baseURL := strings.TrimSuffix(base.GetConfigString(config, "url"), "/")
	pathPrefix := base.GetConfigString(config, "path")
	if pathPrefix != "" {
		baseURL += pathPrefix
	}
	openTok := base.GetConfigString(config, "opentoken")
	if openTok == "" {
		openTok = base.GetConfigString(config, "token")
	}
	return &LuckyProvider{
		BaseProvider: base.BaseProvider{Config: config},
		panelURL:     baseURL,
		openToken:    openTok,
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// Check 通过OpenToken验证Lucky连通性
func (p *LuckyProvider) Check(ctx context.Context) error {
	if p.panelURL == "" || p.openToken == "" {
		return fmt.Errorf("面板地址和OpenToken不能为空")
	}
	_, err := p.luckyRequest(ctx, "GET", "/api/modules/list", nil)
	return err
}

// Deploy 部署证书到Lucky
func (p *LuckyProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	// 获取目标域名
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		domainsStr := p.GetString("domains")
		if domainsStr != "" {
			domains = base.SplitDomains(domainsStr)
		}
	}
	if len(domains) == 0 {
		return fmt.Errorf("没有设置要部署的域名")
	}

	// 获取Lucky证书列表
	resp, err := p.luckyRequest(ctx, "GET", "/api/ssl", nil)
	if err != nil {
		return fmt.Errorf("获取证书列表失败: %v", err)
	}
	p.Log("获取证书列表成功")

	list, _ := resp["list"].([]interface{})
	var success int
	var lastErr error

	for _, item := range list {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		certsInfo, ok := row["CertsInfo"].(map[string]interface{})
		if !ok {
			continue
		}
		certDomains, ok := certsInfo["Domains"].([]interface{})
		if !ok || len(certDomains) == 0 {
			continue
		}

		// 匹配域名
		matched := false
		for _, cd := range certDomains {
			cdStr, _ := cd.(string)
			for _, target := range domains {
				if cdStr == target {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		if matched {
			key, _ := row["Key"].(string)
			remark, _ := row["Remark"].(string)

			params := map[string]interface{}{
				"Key":           key,
				"CertBase64":    base64.StdEncoding.EncodeToString([]byte(fullchain)),
				"KeyBase64":     base64.StdEncoding.EncodeToString([]byte(privateKey)),
				"AddFrom":       "file",
				"Enable":        true,
				"MappingToPath": false,
				"Remark":        remark,
				"AllSyncClient": false,
			}

			_, err := p.luckyRequest(ctx, "PUT", "/api/ssl", params)
			if err != nil {
				lastErr = err
				p.Log(fmt.Sprintf("证书 %s 更新失败: %v", key, err))
			} else {
				p.Log(fmt.Sprintf("证书 %s 更新成功", key))
				success++
			}
		}
	}

	if success == 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("未找到匹配的证书")
	}

	return nil
}

// luckyRequest 发送Lucky API请求（OpenToken认证）
func (p *LuckyProvider) luckyRequest(ctx context.Context, method, path string, params map[string]interface{}) (map[string]interface{}, error) {
	reqURL := p.panelURL + path

	var bodyReader io.Reader
	if params != nil {
		jsonBytes, _ := json.Marshal(params)
		bodyReader = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("openToken", p.openToken)
	if params != nil {
		req.Header.Set("Content-Type", "application/json")
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

	if ret, ok := result["ret"].(float64); ok && ret == 0 {
		return result, nil
	}

	if msg, ok := result["msg"].(string); ok {
		return nil, fmt.Errorf("%s", msg)
	}

	return nil, fmt.Errorf("请求失败(HTTP %d)", resp.StatusCode)
}

// SetLogger 设置日志记录器
func (p *LuckyProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
