package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert"
	"main/internal/cert/deploy/base"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	base.Register("qiniu", NewQiniuProvider)
}

type qiniuProvider struct {
	base.BaseProvider
	inner *QiniuDeploy
}

func NewQiniuProvider(config map[string]interface{}) base.DeployProvider {
	ak := base.GetConfigString(config, "access_key")
	if ak == "" {
		ak = base.GetConfigString(config, "AccessKey")
	}
	sk := base.GetConfigString(config, "secret_key")
	if sk == "" {
		sk = base.GetConfigString(config, "SecretKey")
	}
	return &qiniuProvider{
		BaseProvider: base.BaseProvider{Config: config},
		inner:        NewQiniuDeploy(map[string]string{"access_key": ak, "secret_key": sk}),
	}
}

func (p *qiniuProvider) Check(ctx context.Context) error {
	return p.inner.Check(ctx)
}

func (p *qiniuProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.inner.Deploy(ctx, fullchain, privateKey, config)
}

func (p *qiniuProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
	p.inner.SetLogger(logger)
}

type QiniuDeploy struct {
	accessKey string
	secretKey string
	client    *http.Client
	logger    cert.Logger
}

func NewQiniuDeploy(config map[string]string) *QiniuDeploy {
	return &QiniuDeploy{
		accessKey: config["access_key"],
		secretKey: config["secret_key"],
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (d *QiniuDeploy) SetLogger(logger cert.Logger) { d.logger = logger }

func (d *QiniuDeploy) logf(format string, args ...interface{}) {
	if d.logger != nil {
		d.logger(fmt.Sprintf(format, args...))
	}
}

// requestQBox 与 dnsmgr Qiniu.php::request 一致：签名为 path[?query] + "\n"，Authorization: QBox AccessKey:urlsafe_base64(hmac-sha1)
func (d *QiniuDeploy) requestQBox(ctx context.Context, method, path string, query url.Values, body interface{}) (map[string]interface{}, error) {
	const apiBase = "https://api.qiniu.com"
	u, err := url.Parse(apiBase + path)
	if err != nil {
		return nil, err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}

	signPath := u.Path
	if u.RawQuery != "" {
		signPath = u.Path + "?" + u.RawQuery
	}
	signStr := signPath + "\n"
	mac := hmac.New(sha1.New, []byte(d.secretKey))
	mac.Write([]byte(signStr))
	token := d.accessKey + ":" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	var bodyBytes []byte
	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "QBox "+token)
	if len(bodyBytes) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

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
		_ = json.Unmarshal(respBody, &result)
	}
	return result, nil
}

func (d *QiniuDeploy) Check(ctx context.Context) error {
	_, err := d.requestQBox(ctx, "GET", "/sslcert", nil, nil)
	return err
}

func qiniuDomainPathSegment(domain string) (string, error) {
	d := strings.TrimSpace(domain)
	if d == "" || strings.ContainsAny(d, "/?#") {
		return "", fmt.Errorf("非法域名: %q", domain)
	}
	return d, nil
}

func qiniuCertIDFromMap(m map[string]interface{}) string {
	if m == nil {
		return ""
	}
	if id, ok := m["certID"].(string); ok && id != "" {
		return id
	}
	if id, ok := m["certid"].(string); ok && id != "" {
		return id
	}
	if v, ok := m["certID"].(float64); ok {
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

func (d *QiniuDeploy) Deploy(ctx context.Context, certPEM, keyPEM string, config map[string]interface{}) error {
	prod := strings.TrimSpace(base.GetConfigString(config, "product"))
	if prod == "" {
		prod = "cdn"
	}

	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		if v, ok := config["domain"].(string); ok && v != "" {
			domains = base.SplitDomains(v)
		}
	}
	if len(domains) == 0 {
		return fmt.Errorf("绑定的域名不能为空")
	}

	commonName := ""
	if len(domains) > 0 {
		commonName = domains[0]
	}

	body := map[string]interface{}{
		"name":        "cert_" + time.Now().Format("20060102150405"),
		"common_name": commonName,
		"ca":          certPEM,
		"pri":         keyPEM,
	}

	result, err := d.requestQBox(ctx, "POST", "/sslcert", nil, body)
	if err != nil {
		return err
	}

	certID := qiniuCertIDFromMap(result)
	if certID == "" {
		return fmt.Errorf("上传证书成功但未返回证书ID")
	}

	if prod == "upload" {
		d.logf("证书已上传到七牛证书管理，certID=%s", certID)
		return nil
	}

	if prod == "oss" {
		for _, domain := range domains {
			domain = strings.TrimSpace(domain)
			if domain == "" {
				continue
			}
			_, err = d.requestQBox(ctx, "POST", "/cert/bind", nil, map[string]interface{}{
				"certid": certID,
				"domain": domain,
			})
			if err != nil {
				return fmt.Errorf("绑定 OSS 域名 %s 失败: %w", domain, err)
			}
			d.logf("OSS 域名 %s 证书绑定成功", domain)
		}
		return nil
	}

	if prod != "cdn" {
		return fmt.Errorf("不支持的七牛产品类型: %s（当前实现: cdn、oss、upload；直播 pili 等需单独对接 pili.qiniuapi.com 签名）", prod)
	}

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		d.logf("正在部署证书到七牛 CDN: %s", domain)

		seg, err := qiniuDomainPathSegment(domain)
		if err != nil {
			return err
		}

		info, err := d.requestQBox(ctx, "GET", "/domain/"+seg, nil, nil)
		if err != nil {
			return fmt.Errorf("获取域名信息失败 %s: %w", domain, err)
		}

		https, _ := info["https"].(map[string]interface{})
		existingID := ""
		if https != nil {
			if id, ok := https["certId"].(string); ok {
				existingID = id
			}
		}

		if existingID == certID {
			d.logf("域名 %s 已使用该证书，跳过", domain)
			continue
		}

		if existingID == "" {
			_, err = d.requestQBox(ctx, "PUT", "/domain/"+seg+"/sslize", nil, map[string]interface{}{
				"certid": certID,
			})
		} else {
			forceHTTPS := false
			http2 := true
			if https != nil {
				if v, ok := https["forceHttps"].(bool); ok {
					forceHTTPS = v
				}
				if v, ok := https["http2Enable"].(bool); ok {
					http2 = v
				}
			}
			_, err = d.requestQBox(ctx, "PUT", "/domain/"+seg+"/httpsconf", nil, map[string]interface{}{
				"certid":      certID,
				"forceHttps":  forceHTTPS,
				"http2Enable": http2,
			})
		}
		if err != nil {
			return fmt.Errorf("绑定 CDN 域名 %s 失败: %w", domain, err)
		}
		d.logf("CDN 域名 %s 证书部署成功", domain)
	}

	return nil
}

func (d *QiniuDeploy) GetConfig() []cert.ConfigField {
	return []cert.ConfigField{
		{Name: "域名", Key: "domain", Type: "input", Placeholder: "要部署证书的CDN域名"},
	}
}
