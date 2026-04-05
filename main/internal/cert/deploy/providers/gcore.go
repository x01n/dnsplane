package providers

import (
	"main/internal/cert/deploy/base"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert"
	"net/http"
	"time"
)

func init() {
	base.Register("gcore", NewGcoreProvider)

	cert.Register("gcore", nil, cert.ProviderConfig{
		Type:     "gcore",
		Name:     "Gcore CDN",
		Icon:     "gcore.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "API Key", Key: "apikey", Type: "input", Required: true, Placeholder: "Gcore API令牌"},
			{Name: "证书ID", Key: "id", Type: "input", Required: true, Placeholder: "Gcore SSL证书ID"},
			{Name: "证书名称", Key: "name", Type: "input", Required: true, Placeholder: "证书名称"},
		},
	})
}

type GcoreProvider struct {
	base.BaseProvider
	apiKey string
	client *http.Client
}

func NewGcoreProvider(config map[string]interface{}) base.DeployProvider {
	return &GcoreProvider{
		BaseProvider: base.BaseProvider{Config: config},
		apiKey:       base.GetConfigString(config, "apikey"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *GcoreProvider) Check(ctx context.Context) error {
	if p.apiKey == "" {
		return fmt.Errorf("API令牌不能为空")
	}

	_, err := p.request(ctx, "GET", "/iam/clients/me", nil)
	return err
}

func (p *GcoreProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	certID := base.GetConfigString(config, "id")
	if certID == "" {
		certID = p.GetString("id")
	}
	if certID == "" {
		return fmt.Errorf("证书ID不能为空")
	}

	certName := base.GetConfigString(config, "name")
	if certName == "" {
		certName = p.GetString("name")
	}

	params := map[string]interface{}{
		"name":             certName,
		"sslCertificate":   fullchain,
		"sslPrivateKey":    privateKey,
		"validate_root_ca": true,
	}

	_, err := p.request(ctx, "PUT", "/cdn/sslData/"+certID, params)
	if err != nil {
		return fmt.Errorf("证书更新失败: %v", err)
	}

	p.Log(fmt.Sprintf("证书ID:%s更新成功！", certID))
	return nil
}

func (p *GcoreProvider) request(ctx context.Context, method, path string, params map[string]interface{}) (map[string]interface{}, error) {
	reqURL := "https://api.gcore.com" + path

	var bodyReader io.Reader
	if params != nil {
		bodyBytes, _ := json.Marshal(params)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "APIKey "+p.apiKey)
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

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var result map[string]interface{}
		if len(body) > 0 {
			json.Unmarshal(body, &result)
		}
		return result, nil
	}

	var result map[string]interface{}
	if json.Unmarshal(body, &result) == nil {
		if msgMap, ok := result["message"].(map[string]interface{}); ok {
			if msg, ok := msgMap["message"].(string); ok {
				return nil, fmt.Errorf("%s", msg)
			}
		}
		if errors, ok := result["errors"].(map[string]interface{}); ok {
			for _, v := range errors {
				if errList, ok := v.([]interface{}); ok && len(errList) > 0 {
					return nil, fmt.Errorf("%v", errList[0])
				}
			}
		}
	}

	return nil, fmt.Errorf("请求失败(httpCode=%d)", resp.StatusCode)
}

func (p *GcoreProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
