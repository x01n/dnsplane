package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert"
	"main/internal/cert/deploy/base"
	"net/http"
	"time"
)

func init() {
	base.Register("upyun", NewUpyunProvider)
}

type upyunProvider struct {
	base.BaseProvider
	inner *UpyunDeploy
}

func NewUpyunProvider(config map[string]interface{}) base.DeployProvider {
	tok := base.GetConfigString(config, "token")
	return &upyunProvider{
		BaseProvider: base.BaseProvider{Config: config},
		inner:        NewUpyunDeploy(map[string]string{"token": tok}),
	}
}

func (p *upyunProvider) Check(ctx context.Context) error {
	return p.inner.Check(ctx)
}

func (p *upyunProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.inner.Deploy(ctx, fullchain, privateKey, config)
}

func (p *upyunProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
	p.inner.SetLogger(logger)
}

type UpyunDeploy struct {
	token  string
	client *http.Client
}

func NewUpyunDeploy(config map[string]string) *UpyunDeploy {
	return &UpyunDeploy{
		token:  config["token"],
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (d *UpyunDeploy) request(ctx context.Context, method, url string, body interface{}) (map[string]interface{}, error) {
	var bodyBytes []byte
	var err error
	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.token)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("又拍云API错误: %s", string(respBody))
	}

	var result map[string]interface{}
	if len(respBody) > 0 {
		json.Unmarshal(respBody, &result)
	}

	return result, nil
}

func (d *UpyunDeploy) Check(ctx context.Context) error {
	_, err := d.request(ctx, "GET", "https://api.upyun.com/https/certificate/", nil)
	return err
}

func (d *UpyunDeploy) Deploy(ctx context.Context, cert, key string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		if v, ok := config["domain"].(string); ok && v != "" {
			domains = base.SplitDomains(v)
		}
	}

	body := map[string]interface{}{
		"certificate": cert,
		"private_key": key,
	}

	result, err := d.request(ctx, "POST", "https://api.upyun.com/https/certificate/", body)
	if err != nil {
		return err
	}

	certID := ""
	if data, ok := result["result"].(map[string]interface{}); ok {
		if id, ok := data["certificate_id"].(string); ok {
			certID = id
		}
	}

	if certID != "" && len(domains) > 0 {
		for _, domain := range domains {
			_, err = d.request(ctx, "POST", "https://api.upyun.com/https/bindcertificate", map[string]interface{}{
				"certificate_id": certID,
				"domain":         domain,
			})
			if err != nil {
				return fmt.Errorf("上传证书成功,但绑定域名失败: %w", err)
			}
		}
	}

	return nil
}

func (d *UpyunDeploy) GetConfig() []cert.ConfigField {
	return []cert.ConfigField{
		{Name: "域名", Key: "domain", Type: "input", Placeholder: "要部署证书的CDN域名"},
	}
}

func (d *UpyunDeploy) SetLogger(logger cert.Logger) {}
