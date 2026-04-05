package providers

import (
	"main/internal/cert/deploy/base"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"main/internal/cert"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func init() {
	base.Register("baidu_cdn", NewBaiduCDNProvider)

	cert.Register("baidu_cdn", nil, cert.ProviderConfig{
		Type:     "baidu_cdn",
		Name:     "百度云CDN",
		Icon:     "baidu.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "access_key_secret", Type: "input", Required: true},
		},
		DeployConfig: []cert.ConfigField{
			{Name: "产品类型", Key: "product", Type: "select", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "CDN"},
				{Value: "blb", Label: "普通型BLB"},
				{Value: "appblb", Label: "应用型BLB"},
			}, Value: "cdn"},
			{Name: "域名", Key: "domain", Type: "input", Required: true, Note: "多个域名用逗号分隔"},
			{Name: "地域", Key: "region", Type: "input", Note: "BLB需要，如 bj、gz", Show: "product!='cdn'"},
			{Name: "实例ID", Key: "blb_id", Type: "input", Note: "BLB实例ID", Show: "product!='cdn'"},
			{Name: "监听端口", Key: "blb_port", Type: "input", Note: "HTTPS监听端口", Show: "product!='cdn'"},
		},
	})
}

type BaiduCDNProvider struct {
	base.BaseProvider
	client *http.Client
}

func NewBaiduCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &BaiduCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// BaiduCloud 百度云API客户端
type BaiduCloud struct {
	accessKeyID     string
	secretAccessKey string
	host            string
	client          *http.Client
}

func NewBaiduCloud(accessKeyID, secretAccessKey, host string) *BaiduCloud {
	return &BaiduCloud{
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		host:            host,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *BaiduCloud) Request(ctx context.Context, method, path string, query map[string]string, body interface{}) (map[string]interface{}, error) {
	// 构建URL
	u := &url.URL{
		Scheme: "https",
		Host:   c.host,
		Path:   path,
	}
	if query != nil {
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	// 构建请求体
	var bodyReader io.Reader
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %v", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置时间戳
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	req.Header.Set("x-bce-date", timestamp)
	req.Header.Set("Host", c.host)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// 签名
	c.signRequest(req, timestamp)

	// 发送请求
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("百度云API错误(%d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("解析响应失败: %v", err)
		}
	}

	return result, nil
}

func (c *BaiduCloud) signRequest(req *http.Request, timestamp string) {
	// BCE签名算法
	expirationPeriodInSeconds := 1800

	// 1. 生成签名密钥
	authStringPrefix := fmt.Sprintf("bce-auth-v1/%s/%s/%d",
		c.accessKeyID, timestamp, expirationPeriodInSeconds)
	signingKey := hmacSHA256(c.secretAccessKey, authStringPrefix)

	// 2. 生成规范请求
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// 规范化查询字符串
	var canonicalQueryString string
	if req.URL.RawQuery != "" {
		params := strings.Split(req.URL.RawQuery, "&")
		sort.Strings(params)
		canonicalQueryString = strings.Join(params, "&")
	}

	// 规范化头部
	signedHeaders := []string{"host", "x-bce-date"}
	var canonicalHeaders []string
	for _, h := range signedHeaders {
		value := req.Header.Get(h)
		if h == "host" {
			value = c.host
		}
		canonicalHeaders = append(canonicalHeaders, fmt.Sprintf("%s:%s",
			strings.ToLower(h), url.QueryEscape(strings.TrimSpace(value))))
	}

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s",
		req.Method, canonicalURI, canonicalQueryString, strings.Join(canonicalHeaders, "\n"))

	// 3. 生成签名
	signature := hmacSHA256(signingKey, canonicalRequest)

	// 4. 设置Authorization头
	authorization := fmt.Sprintf("%s/%s/%s",
		authStringPrefix, strings.Join(signedHeaders, ";"), signature)
	req.Header.Set("Authorization", authorization)
}

func hmacSHA256(key, data string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func (p *BaiduCDNProvider) getClient(host string) *BaiduCloud {
	return NewBaiduCloud(
		p.GetString("access_key_id"),
		p.GetString("access_key_secret"),
		host,
	)
}

func (p *BaiduCDNProvider) Check(ctx context.Context) error {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	if accessKeyID == "" || accessKeySecret == "" {
		return fmt.Errorf("AccessKey不能为空")
	}

	client := p.getClient("cdn.baidubce.com")
	_, err := client.Request(ctx, "GET", "/v2/domain", nil, nil)
	return err
}

func (p *BaiduCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	product := base.GetConfigString(config, "product")
	if product == "" {
		product = p.GetString("product")
	}
	if product == "" {
		product = "cdn"
	}

	switch product {
	case "cdn":
		return p.deployCDN(ctx, fullchain, privateKey, config)
	case "blb":
		return p.deployBLB(ctx, fullchain, privateKey, config, false)
	case "appblb":
		return p.deployBLB(ctx, fullchain, privateKey, config, true)
	default:
		return fmt.Errorf("不支持的产品类型: %s", product)
	}
}

func (p *BaiduCDNProvider) deployCDN(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		domain := p.GetString("domain")
		if domain != "" {
			domains = base.SplitDomains(domain)
		}
	}
	if len(domains) == 0 {
		return fmt.Errorf("域名不能为空")
	}

	// 解析证书获取名称
	certName, err := p.getCertName(fullchain)
	if err != nil {
		return err
	}

	client := p.getClient("cdn.baidubce.com")

	for _, domain := range domains {
		p.Log("正在部署证书到百度云CDN: " + domain)

		// 检查证书是否已存在
		data, err := client.Request(ctx, "GET", "/v2/"+domain+"/certificates", nil, nil)
		if err == nil {
			if existingName, ok := data["certName"].(string); ok && existingName == certName {
				p.Log("CDN域名 " + domain + " 证书已存在，无需重复部署")
				continue
			}
		}

		// 部署证书
		param := map[string]interface{}{
			"httpsEnable": "ON",
			"certificate": map[string]interface{}{
				"certName":        certName,
				"certServerData":  fullchain,
				"certPrivateData": privateKey,
			},
		}

		_, err = client.Request(ctx, "PUT", "/v2/"+domain+"/certificates", nil, param)
		if err != nil {
			return fmt.Errorf("百度云CDN部署失败(%s): %v", domain, err)
		}
		p.Log("CDN域名 " + domain + " 证书部署成功")
	}

	p.Log("百度云CDN证书部署完成")
	return nil
}

func (p *BaiduCDNProvider) deployBLB(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, isApp bool) error {
	region := base.GetConfigString(config, "region")
	if region == "" {
		region = p.GetString("region")
	}
	if region == "" {
		return fmt.Errorf("地域不能为空")
	}

	blbID := base.GetConfigString(config, "blb_id")
	if blbID == "" {
		blbID = p.GetString("blb_id")
	}
	if blbID == "" {
		return fmt.Errorf("负载均衡实例ID不能为空")
	}

	blbPort := base.GetConfigString(config, "blb_port")
	if blbPort == "" {
		blbPort = p.GetString("blb_port")
	}
	if blbPort == "" {
		return fmt.Errorf("HTTPS监听端口不能为空")
	}

	// 先上传证书获取ID
	certID, err := p.uploadCert(ctx, fullchain, privateKey)
	if err != nil {
		return err
	}

	// 部署到BLB
	host := fmt.Sprintf("blb.%s.baidubce.com", region)
	client := p.getClient(host)

	var path string
	if isApp {
		path = "/v1/appblb/" + blbID + "/HTTPSlistener"
		p.Log("正在部署证书到应用型BLB: " + blbID)
	} else {
		path = "/v1/blb/" + blbID + "/HTTPSlistener"
		p.Log("正在部署证书到普通型BLB: " + blbID)
	}

	query := map[string]string{
		"listenerPort": blbPort,
	}
	param := map[string]interface{}{
		"certIds": []string{certID},
	}

	_, err = client.Request(ctx, "PUT", path, query, param)
	if err != nil {
		return fmt.Errorf("BLB部署失败: %v", err)
	}

	if isApp {
		p.Log("应用型BLB " + blbID + " 部署证书成功")
	} else {
		p.Log("普通型BLB " + blbID + " 部署证书成功")
	}
	return nil
}

func (p *BaiduCDNProvider) uploadCert(ctx context.Context, fullchain, privateKey string) (string, error) {
	certName, err := p.getCertName(fullchain)
	if err != nil {
		return "", err
	}

	client := p.getClient("certificate.baidubce.com")

	// 先查询是否已存在
	query := map[string]string{
		"certName": certName,
	}
	data, err := client.Request(ctx, "GET", "/v1/certificate", query, nil)
	if err == nil {
		if certs, ok := data["certs"].([]interface{}); ok {
			for _, c := range certs {
				if cert, ok := c.(map[string]interface{}); ok {
					if name, ok := cert["certName"].(string); ok && name == certName {
						certID := cert["certId"].(string)
						p.Log("证书已存在 CertId=" + certID)
						return certID, nil
					}
				}
			}
		}
	}

	// 上传新证书
	param := map[string]interface{}{
		"certName":        certName,
		"certServerData":  fullchain,
		"certPrivateData": privateKey,
	}

	data, err = client.Request(ctx, "POST", "/v1/certificate", nil, param)
	if err != nil {
		return "", fmt.Errorf("上传证书失败: %v", err)
	}

	certID, ok := data["certId"].(string)
	if !ok {
		return "", fmt.Errorf("获取证书ID失败")
	}

	p.Log("上传证书成功 CertId=" + certID)
	return certID, nil
}

func (p *BaiduCDNProvider) getCertName(fullchain string) (string, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return "", fmt.Errorf("无法解析证书")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("证书解析失败: %v", err)
	}

	cn := cert.Subject.CommonName
	cn = strings.ReplaceAll(cn, "*.", "")
	certName := fmt.Sprintf("%s-%d", cn, cert.NotBefore.Unix())
	return certName, nil
}

func (p *BaiduCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
