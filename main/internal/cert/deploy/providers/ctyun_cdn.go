package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"main/internal/cert"
	"main/internal/cert/deploy/base"
	"net/http"
	"sort"
	"strings"
	"time"
)

func init() {
	base.Register("ctyun_cdn", NewCtyunCDNProvider)

	cert.Register("ctyun_cdn", nil, cert.ProviderConfig{
		Type:     "ctyun_cdn",
		Name:     "天翼云CDN",
		Icon:     "ctyun.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "access_key_secret", Type: "input", Required: true},
		},
		DeployConfig: []cert.ConfigField{
			{Name: "产品类型", Key: "product", Type: "select", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "CDN加速"},
				{Value: "icdn", Label: "智能CDN"},
				{Value: "accessone", Label: "边缘安全加速"},
			}, Value: "cdn"},
			{Name: "域名", Key: "domain", Type: "input", Required: true},
		},
	})
}

type CtyunCDNProvider struct {
	base.BaseProvider
	client *http.Client
}

func NewCtyunCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &CtyunCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *CtyunCDNProvider) Check(ctx context.Context) error {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	if accessKeyID == "" || accessKeySecret == "" {
		return fmt.Errorf("AccessKey不能为空")
	}

	_, err := p.request(ctx, "ctcdn-global.ctapi.ctyun.cn", "GET", "/v1/cert/query-cert-list", nil)
	return err
}

func (p *CtyunCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	product := base.GetConfigString(config, "product")
	if product == "" {
		product = p.GetString("product")
	}
	if product == "" {
		product = "cdn"
	}

	domain := base.GetConfigString(config, "domain")
	if domain == "" {
		domain = p.GetString("domain")
	}
	if domain == "" {
		return fmt.Errorf("域名不能为空")
	}

	// 获取证书名称
	certName, err := p.getCertName(fullchain)
	if err != nil {
		return err
	}

	var host string
	switch product {
	case "cdn":
		host = "ctcdn-global.ctapi.ctyun.cn"
	case "icdn":
		host = "icdn-global.ctapi.ctyun.cn"
	case "accessone":
		host = "accessone-global.ctapi.ctyun.cn"
	default:
		return fmt.Errorf("不支持的产品类型: %s", product)
	}

	// 上传证书
	uploadParam := map[string]interface{}{
		"name":  certName,
		"key":   privateKey,
		"certs": fullchain,
	}

	_, err = p.request(ctx, host, "POST", "/v1/cert/creat-cert", uploadParam)
	if err != nil {
		if !strings.Contains(err.Error(), "已存在重名的证书") {
			return fmt.Errorf("上传证书失败: %v", err)
		}
		p.Log("已存在重名的证书 cert_name=" + certName)
	} else {
		p.Log("上传证书成功 cert_name=" + certName)
	}

	// 部署到域名
	deployParam := map[string]interface{}{
		"domain":       domain,
		"https_status": "on",
		"cert_name":    certName,
	}

	_, err = p.request(ctx, host, "POST", "/v1/domain/update-domain", deployParam)
	if err != nil {
		if !strings.Contains(err.Error(), "请求已提交，请勿重复操作") {
			return fmt.Errorf("部署证书失败: %v", err)
		}
	}

	p.Log(fmt.Sprintf("CDN域名 %s 部署证书成功", domain))
	return nil
}

func (p *CtyunCDNProvider) request(ctx context.Context, host, method, path string, body interface{}) (map[string]interface{}, error) {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	// 构建URL
	fullURL := fmt.Sprintf("https://%s%s", host, path)

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
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置时间戳
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	req.Header.Set("ctyun-eop-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))

	// 签名
	p.signCtyunRequest(req, accessKeyID, accessKeySecret, timestamp, bodyBytes)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// 发送请求
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
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	// 检查错误
	if code, ok := result["statusCode"].(float64); ok && code != 200 && code != 800 {
		msg := ""
		if m, ok := result["message"].(string); ok {
			msg = m
		}
		return nil, fmt.Errorf("天翼云API错误(%v): %s", code, msg)
	}

	if returnObj, ok := result["returnObj"].(map[string]interface{}); ok {
		return returnObj, nil
	}

	return result, nil
}

func (p *CtyunCDNProvider) signCtyunRequest(req *http.Request, accessKeyID, accessKeySecret, timestamp string, body []byte) {
	// 天翼云签名算法
	dateStr := timestamp[:8]

	// 计算请求体哈希
	var contentHash string
	if len(body) > 0 {
		h := sha256.Sum256(body)
		contentHash = hex.EncodeToString(h[:])
	} else {
		h := sha256.Sum256([]byte(""))
		contentHash = hex.EncodeToString(h[:])
	}

	// 规范化头部
	signedHeaders := []string{"content-type", "ctyun-eop-request-id", "host"}
	sort.Strings(signedHeaders)

	var headerStrBuilder strings.Builder
	for _, h := range signedHeaders {
		value := req.Header.Get(h)
		if h == "host" {
			value = req.URL.Host
		}
		if h == "content-type" && value == "" {
			value = "application/json"
		}
		headerStrBuilder.WriteString(fmt.Sprintf("%s:%s\n", h, value))
	}

	// 规范化查询字符串
	canonicalQuery := req.URL.RawQuery

	// 构建待签名字符串
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		req.URL.Path,
		canonicalQuery,
		headerStrBuilder.String(),
		strings.Join(signedHeaders, ";"),
		contentHash,
	)

	// 计算签名
	kDate := hmacSHA256Ctyun([]byte(accessKeySecret), dateStr)
	kSigning := hmacSHA256Ctyun(kDate, accessKeyID)
	signature := base64.StdEncoding.EncodeToString(hmacSHA256Ctyun(kSigning, canonicalRequest))

	// 设置Authorization头
	authorization := fmt.Sprintf("%s Headers=%s Signature=%s",
		accessKeyID,
		strings.Join(signedHeaders, ";"),
		signature,
	)
	req.Header.Set("Eop-Authorization", authorization)
	req.Header.Set("Eop-Date", timestamp)
}

func hmacSHA256Ctyun(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func (p *CtyunCDNProvider) getCertName(fullchain string) (string, error) {
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

func (p *CtyunCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
