package providers

import (
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
	"main/internal/cert/deploy/base"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func init() {
	base.Register("ksyun_cdn", NewKsyunCDNProvider)

	cert.Register("ksyun_cdn", nil, cert.ProviderConfig{
		Type:     "ksyun_cdn",
		Name:     "金山云CDN",
		Icon:     "ksyun.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true, Placeholder: "金山云 AccessKeyId"},
			{Name: "SecretAccessKey", Key: "secret_access_key", Type: "input", Required: true, Placeholder: "金山云 SecretAccessKey"},
		},
		DeployConfig: []cert.ConfigField{
			{Name: "CDN域名", Key: "domain", Type: "input", Required: true, Placeholder: "多个域名用逗号分隔"},
		},
	})
}

type KsyunCDNProvider struct {
	base.BaseProvider
	accessKeyID     string
	secretAccessKey string
	client          *http.Client
}

func NewKsyunCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &KsyunCDNProvider{
		BaseProvider:    base.BaseProvider{Config: config},
		accessKeyID:     base.GetConfigString(config, "access_key_id"),
		secretAccessKey: base.GetConfigString(config, "secret_access_key"),
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *KsyunCDNProvider) Check(ctx context.Context) error {
	if p.accessKeyID == "" || p.secretAccessKey == "" {
		return fmt.Errorf("AccessKeyId和SecretAccessKey不能为空")
	}

	// 测试获取证书列表
	params := map[string]string{}
	_, err := p.request(ctx, "GET", "cdn.api.ksyun.com", "cdn", "cn-shanghai-2",
		"GetCertificates", "2016-09-01", "/2016-09-01/cert/GetCertificates", params)
	return err
}

func (p *KsyunCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domain := base.GetConfigString(config, "domain")
	if domain == "" {
		domain = p.GetString("domain")
	}
	if domain == "" {
		return fmt.Errorf("绑定的域名不能为空")
	}

	// 解析证书获取名称
	certName, err := p.getCertName(fullchain)
	if err != nil {
		return err
	}

	domains := strings.Split(domain, ",")
	for i := range domains {
		domains[i] = strings.TrimSpace(domains[i])
	}

	// 获取CDN域名列表
	params := map[string]string{
		"PageSize":   "100",
		"PageNumber": "1",
	}
	resp, err := p.request(ctx, "GET", "cdn.api.ksyun.com", "cdn", "cn-shanghai-2",
		"GetCdnDomains", "2019-06-01", "/2019-06-01/domain/GetCdnDomains", params)
	if err != nil {
		return fmt.Errorf("获取CDN域名列表失败: %v", err)
	}

	// 查找匹配的域名ID
	var domainIDs []string
	if domainsData, ok := resp["Domains"].([]interface{}); ok {
		for _, d := range domainsData {
			if dm, ok := d.(map[string]interface{}); ok {
				domainName, _ := dm["DomainName"].(string)
				for _, targetDomain := range domains {
					if domainName == targetDomain {
						if domainID, ok := dm["DomainId"].(string); ok {
							domainIDs = append(domainIDs, domainID)
						}
					}
				}
			}
		}
	}

	if len(domainIDs) == 0 {
		return fmt.Errorf("未找到对应的CDN域名")
	}

	// 配置证书
	params = map[string]string{
		"Enable":            "on",
		"DomainIds":         strings.Join(domainIDs, ","),
		"CertificateName":   certName,
		"ServerCertificate": fullchain,
		"PrivateKey":        privateKey,
	}
	resp, err = p.request(ctx, "POST", "cdn.api.ksyun.com", "cdn", "cn-shanghai-2",
		"ConfigCertificate", "2016-09-01", "/2016-09-01/cert/ConfigCertificate", params)
	if err != nil {
		return fmt.Errorf("配置证书失败: %v", err)
	}

	if certID, ok := resp["CertificateId"].(string); ok {
		p.Log(fmt.Sprintf("CDN证书部署成功，证书ID：%s", certID))
	} else {
		p.Log("CDN证书部署成功")
	}

	return nil
}

func (p *KsyunCDNProvider) request(ctx context.Context, method, host, service, region, action, version, path string, params map[string]string) (map[string]interface{}, error) {
	// 构建请求URL
	query := url.Values{}
	query.Set("Action", action)
	query.Set("Version", version)
	for k, v := range params {
		query.Set(k, v)
	}

	reqURL := fmt.Sprintf("https://%s%s?%s", host, path, query.Encode())

	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 签名请求
	now := time.Now().UTC()
	dateStr := now.Format("20060102T150405Z")
	dateShort := now.Format("20060102")

	req.Header.Set("Host", host)
	req.Header.Set("X-Amz-Date", dateStr)
	req.Header.Set("Content-Type", "application/json")

	// 创建签名
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-date:%s\n", host, dateStr)
	signedHeaders := "host;x-amz-date"

	hashedPayload := sha256Hash("")
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		method, path, query.Encode(), canonicalHeaders, signedHeaders, hashedPayload)

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateShort, region, service)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		dateStr, credentialScope, sha256Hash(canonicalRequest))

	signingKey := p.getSigningKey(dateShort, region, service)
	signature := hex.EncodeToString(ksyunHmacSHA256(signingKey, []byte(stringToSign)))

	authorization := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.accessKeyID, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authorization)

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

	if errCode, ok := result["Error"]; ok {
		if errMap, ok := errCode.(map[string]interface{}); ok {
			if msg, ok := errMap["Message"].(string); ok {
				return nil, fmt.Errorf("%s", msg)
			}
		}
		return nil, fmt.Errorf("请求失败: %v", errCode)
	}

	return result, nil
}

func (p *KsyunCDNProvider) getSigningKey(dateStr, region, service string) []byte {
	kDate := ksyunHmacSHA256([]byte("AWS4"+p.secretAccessKey), []byte(dateStr))
	kRegion := ksyunHmacSHA256(kDate, []byte(region))
	kService := ksyunHmacSHA256(kRegion, []byte(service))
	kSigning := ksyunHmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

func (p *KsyunCDNProvider) getCertName(fullchain string) (string, error) {
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

func (p *KsyunCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

// sha256Hash 计算 SHA256 哈希
func sha256Hash(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// ksyunHmacSHA256 计算 HMAC-SHA256
func ksyunHmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// sortQueryString 对查询字符串进行排序
func sortQueryString(query string) string {
	if query == "" {
		return ""
	}
	params := strings.Split(query, "&")
	sort.Strings(params)
	return strings.Join(params, "&")
}
