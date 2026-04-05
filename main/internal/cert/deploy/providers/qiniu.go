package providers

import (
	"main/internal/cert/deploy/base"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert"
	"net/http"
	"time"
)

type QiniuDeploy struct {
	accessKey string
	secretKey string
	client    *http.Client
}

func NewQiniuDeploy(config map[string]string) *QiniuDeploy {
	return &QiniuDeploy{
		accessKey: config["access_key"],
		secretKey: config["secret_key"],
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (d *QiniuDeploy) sign(data string) string {
	h := hmac.New(sha1.New, []byte(d.secretKey))
	h.Write([]byte(data))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

func (d *QiniuDeploy) request(ctx context.Context, method, url string, body interface{}) (map[string]interface{}, error) {
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

	signStr := method + " " + req.URL.Path
	if req.URL.RawQuery != "" {
		signStr += "?" + req.URL.RawQuery
	}
	signStr += "\nHost: " + req.URL.Host
	if len(bodyBytes) > 0 {
		signStr += "\n\n" + string(bodyBytes)
	}

	sign := d.sign(signStr)
	req.Header.Set("Authorization", "Qiniu "+d.accessKey+":"+sign)

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
		return nil, fmt.Errorf("七牛云API错误: %s", string(respBody))
	}

	var result map[string]interface{}
	if len(respBody) > 0 {
		json.Unmarshal(respBody, &result)
	}

	return result, nil
}

func (d *QiniuDeploy) Check(ctx context.Context) error {
	_, err := d.request(ctx, "GET", "https://api.qiniu.com/sslcert", nil)
	return err
}

func (d *QiniuDeploy) Deploy(ctx context.Context, cert, key string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		if v, ok := config["domain"].(string); ok && v != "" {
			domains = base.SplitDomains(v)
		}
	}
	commonName := ""
	if len(domains) > 0 {
		commonName = domains[0]
	}

	body := map[string]interface{}{
		"name":        "cert_" + time.Now().Format("20060102150405"),
		"common_name": commonName,
		"ca":          cert,
		"pri":         key,
	}

	result, err := d.request(ctx, "POST", "https://api.qiniu.com/sslcert", body)
	if err != nil {
		return err
	}

	certID := ""
	if id, ok := result["certID"].(string); ok {
		certID = id
	}

	if certID != "" && len(domains) > 0 {
		for _, domain := range domains {
			_, err = d.request(ctx, "PUT", "https://api.qiniu.com/domain/"+domain+"/sslize", map[string]interface{}{
				"certId":      certID,
				"forceHttps":  false,
				"http2Enable": true,
			})
			if err != nil {
				return fmt.Errorf("上传证书成功,但绑定域名失败: %w", err)
			}
		}
	}

	return nil
}

func (d *QiniuDeploy) GetConfig() []cert.ConfigField {
	return []cert.ConfigField{
		{Name: "域名", Key: "domain", Type: "input", Placeholder: "要部署证书的CDN域名"},
	}
}

func (d *QiniuDeploy) SetLogger(logger cert.Logger) {}
