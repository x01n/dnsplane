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
	base.Register("cachefly", NewCacheFlyProvider)

	cert.Register("cachefly", nil, cert.ProviderConfig{
		Type:     "cachefly",
		Name:     "CacheFly",
		Icon:     "cachefly.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "API Key", Key: "apikey", Type: "input", Required: true, Placeholder: "CacheFly API令牌"},
		},
	})
}

type CacheFlyProvider struct {
	base.BaseProvider
	apiKey string
	client *http.Client
}

func NewCacheFlyProvider(config map[string]interface{}) base.DeployProvider {
	return &CacheFlyProvider{
		BaseProvider: base.BaseProvider{Config: config},
		apiKey:       base.GetConfigString(config, "apikey"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *CacheFlyProvider) Check(ctx context.Context) error {
	if p.apiKey == "" {
		return fmt.Errorf("API令牌不能为空")
	}

	_, err := p.request(ctx, "GET", "/accounts/me", nil)
	return err
}

func (p *CacheFlyProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	params := map[string]interface{}{
		"certificate":    fullchain,
		"certificateKey": privateKey,
	}

	_, err := p.request(ctx, "POST", "/certificates", params)
	if err != nil {
		return fmt.Errorf("证书上传失败: %v", err)
	}

	p.Log("证书上传成功！")
	return nil
}

func (p *CacheFlyProvider) request(ctx context.Context, method, path string, params map[string]interface{}) (map[string]interface{}, error) {
	reqURL := "https://api.cachefly.com/api/2.5" + path

	var bodyReader io.Reader
	if params != nil {
		bodyBytes, _ := json.Marshal(params)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("x-cf-authorization", "Bearer "+p.apiKey)
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

	return nil, fmt.Errorf("请求失败(httpCode=%d)", resp.StatusCode)
}

func (p *CacheFlyProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
