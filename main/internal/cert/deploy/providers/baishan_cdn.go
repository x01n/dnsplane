package providers

import (
	"main/internal/cert/deploy/base"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"main/internal/cert"
	"net/http"
	"strings"
	"time"
)

func init() {
	base.Register("baishan_cdn", NewBaishanCDNProvider)

	cert.Register("baishan_cdn", nil, cert.ProviderConfig{
		Type:     "baishan_cdn",
		Name:     "白山云CDN",
		Icon:     "baishan.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "API Token", Key: "token", Type: "input", Required: true, Placeholder: "白山云API Token"},
			{Name: "证书ID", Key: "id", Type: "input", Required: true, Placeholder: "白山云证书ID"},
		},
	})
}

type BaishanCDNProvider struct {
	base.BaseProvider
	token  string
	client *http.Client
}

func NewBaishanCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &BaishanCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
		token:        base.GetConfigString(config, "token"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *BaishanCDNProvider) Check(ctx context.Context) error {
	if p.token == "" {
		return fmt.Errorf("token不能为空")
	}
	return nil
}

func (p *BaishanCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	certID := base.GetConfigString(config, "id")
	if certID == "" {
		certID = p.GetString("id")
	}
	if certID == "" {
		return fmt.Errorf("证书ID不能为空")
	}

	// 解析证书获取名称
	certName, err := p.getCertName(fullchain)
	if err != nil {
		return err
	}

	params := map[string]interface{}{
		"cert_id":     certID,
		"name":        certName,
		"certificate": fullchain,
		"key":         privateKey,
	}

	err = p.request(ctx, "/v2/domain/certificate?token="+p.token, params)
	if err != nil {
		if strings.Contains(err.Error(), "this certificate is exists") {
			p.Log(fmt.Sprintf("证书ID:%s已存在，无需更新", certID))
			return nil
		}
		return err
	}

	p.Log(fmt.Sprintf("证书ID:%s更新成功！", certID))
	return nil
}

func (p *BaishanCDNProvider) request(ctx context.Context, path string, params map[string]interface{}) error {
	reqURL := "https://cdn.api.baishan.com" + path

	bodyBytes, _ := json.Marshal(params)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 0 {
		return nil
	}

	if msg, ok := result["message"].(string); ok {
		return fmt.Errorf("%s", msg)
	}

	return fmt.Errorf("请求失败(httpCode=%d)", resp.StatusCode)
}

func (p *BaishanCDNProvider) getCertName(fullchain string) (string, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return "", fmt.Errorf("无法解析证书")
	}

	certObj, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("证书解析失败: %v", err)
	}

	cn := certObj.Subject.CommonName
	cn = strings.ReplaceAll(cn, "*.", "")
	certName := fmt.Sprintf("%s-%d", cn, certObj.NotBefore.Unix())
	return certName, nil
}

func (p *BaishanCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
