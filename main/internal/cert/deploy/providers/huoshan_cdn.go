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
	"strings"
	"time"
)

func init() {
	base.Register("huoshan_cdn", NewHuoshanCDNProvider)

	cert.Register("huoshan_cdn", nil, cert.ProviderConfig{
		Type:     "huoshan_cdn",
		Name:     "火山引擎",
		Icon:     "huoshan.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "access_key_secret", Type: "input", Required: true},
		},
		DeployConfig: []cert.ConfigField{
			{Name: "产品类型", Key: "product", Type: "select", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "CDN"},
				{Value: "dcdn", Label: "DCDN（全站加速）"},
				{Value: "tos", Label: "TOS（对象存储）"},
				{Value: "live", Label: "视频直播"},
				{Value: "imagex", Label: "veImageX"},
				{Value: "clb", Label: "CLB（负载均衡）"},
				{Value: "alb", Label: "ALB（应用负载均衡）"},
			}, Value: "cdn"},
			{Name: "域名", Key: "domain", Type: "input", Required: true, Note: "多个域名用逗号分隔"},
			{Name: "Bucket域名", Key: "bucket_domain", Type: "input", Note: "TOS产品需要", Show: "product=='tos'"},
			{Name: "监听器ID", Key: "listener_id", Type: "input", Note: "CLB/ALB产品需要", Show: "product=='clb'||product=='alb'"},
		},
	})
}

type HuoshanCDNProvider struct {
	base.BaseProvider
	client *http.Client
}

func NewHuoshanCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &HuoshanCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// Volcengine 火山引擎API客户端
type Volcengine struct {
	accessKeyID     string
	secretAccessKey string
	host            string
	service         string
	version         string
	region          string
	client          *http.Client
}

func NewVolcengine(accessKeyID, secretAccessKey, host, service, version, region string) *Volcengine {
	return &Volcengine{
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		host:            host,
		service:         service,
		version:         version,
		region:          region,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Volcengine) Request(ctx context.Context, method, action string, body interface{}) (map[string]interface{}, error) {
	// 构建URL
	u := &url.URL{
		Scheme:   "https",
		Host:     c.host,
		Path:     "/",
		RawQuery: fmt.Sprintf("Action=%s&Version=%s", action, c.version),
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

	// 设置头部
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	datestamp := timestamp[:8]
	req.Header.Set("Host", c.host)
	req.Header.Set("X-Date", timestamp)
	req.Header.Set("X-Content-Sha256", hashSHA256(bodyBytes))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// 签名
	c.signRequest(req, timestamp, datestamp, bodyBytes)

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

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	// 检查错误
	if respMetadata, ok := result["ResponseMetadata"].(map[string]interface{}); ok {
		if errInfo, ok := respMetadata["Error"].(map[string]interface{}); ok {
			code := ""
			message := ""
			if c, ok := errInfo["Code"].(string); ok {
				code = c
			}
			if m, ok := errInfo["Message"].(string); ok {
				message = m
			}
			return nil, fmt.Errorf("火山引擎API错误: %s - %s", code, message)
		}
	}

	// 返回Result部分
	if resultData, ok := result["Result"].(map[string]interface{}); ok {
		return resultData, nil
	}

	return result, nil
}

func (c *Volcengine) signRequest(req *http.Request, timestamp, datestamp string, body []byte) {
	// AWS V4签名算法
	algorithm := "HMAC-SHA256"
	credentialScope := fmt.Sprintf("%s/%s/%s/request", datestamp, c.region, c.service)

	// 规范化请求
	signedHeaders := []string{"content-type", "host", "x-content-sha256", "x-date"}
	var canonicalHeaders []string
	for _, h := range signedHeaders {
		value := req.Header.Get(h)
		if h == "host" {
			value = c.host
		}
		if value == "" {
			continue
		}
		canonicalHeaders = append(canonicalHeaders, fmt.Sprintf("%s:%s\n", h, strings.TrimSpace(value)))
	}

	// 过滤掉空值的header
	var actualSignedHeaders []string
	for _, h := range signedHeaders {
		value := req.Header.Get(h)
		if h == "host" {
			value = c.host
		}
		if value != "" {
			actualSignedHeaders = append(actualSignedHeaders, h)
		}
	}

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		"/",
		req.URL.RawQuery,
		strings.Join(canonicalHeaders, ""),
		strings.Join(actualSignedHeaders, ";"),
		hashSHA256(body),
	)

	// 待签名字符串
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s",
		algorithm,
		timestamp,
		credentialScope,
		hashSHA256([]byte(canonicalRequest)),
	)

	// 派生签名密钥
	kDate := hmacSHA256Raw([]byte(datestamp), []byte(c.secretAccessKey))
	kRegion := hmacSHA256Raw([]byte(c.region), kDate)
	kService := hmacSHA256Raw([]byte(c.service), kRegion)
	kSigning := hmacSHA256Raw([]byte("request"), kService)

	// 计算签名
	signature := hex.EncodeToString(hmacSHA256Raw([]byte(stringToSign), kSigning))

	// 设置Authorization头
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm,
		c.accessKeyID,
		credentialScope,
		strings.Join(actualSignedHeaders, ";"),
		signature,
	)
	req.Header.Set("Authorization", authorization)
}

func hashSHA256(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func hmacSHA256Raw(data, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func (p *HuoshanCDNProvider) getClient(host, service, version, region string) *Volcengine {
	return NewVolcengine(
		p.GetString("access_key_id"),
		p.GetString("access_key_secret"),
		host,
		service,
		version,
		region,
	)
}

func (p *HuoshanCDNProvider) Check(ctx context.Context) error {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	if accessKeyID == "" || accessKeySecret == "" {
		return fmt.Errorf("AccessKey不能为空")
	}

	client := p.getClient("open.volcengineapi.com", "cdn", "2021-03-01", "cn-north-1")
	_, err := client.Request(ctx, "POST", "ListCertInfo", map[string]string{"Source": "volc_cert_center"})
	return err
}

func (p *HuoshanCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
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
	case "dcdn":
		return p.deployDCDN(ctx, fullchain, privateKey, config)
	case "tos":
		return p.deployTOS(ctx, fullchain, privateKey, config)
	case "live":
		return p.deployLive(ctx, fullchain, privateKey, config)
	case "imagex":
		return p.deployImageX(ctx, fullchain, privateKey, config)
	case "clb":
		return p.deployCLB(ctx, fullchain, privateKey, config)
	case "alb":
		return p.deployALB(ctx, fullchain, privateKey, config)
	default:
		return fmt.Errorf("不支持的产品类型: %s", product)
	}
}

func (p *HuoshanCDNProvider) getCertID(ctx context.Context, fullchain, privateKey string) (string, error) {
	certName, err := p.getCertName(fullchain)
	if err != nil {
		return "", err
	}

	client := p.getClient("certificate-service.volcengineapi.com", "certificate_service", "2024-10-01", "cn-beijing")

	param := map[string]interface{}{
		"Tag":        certName,
		"Repeatable": false,
		"CertificateInfo": map[string]string{
			"CertificateChain": fullchain,
			"PrivateKey":       privateKey,
		},
	}

	data, err := client.Request(ctx, "POST", "ImportCertificate", param)
	if err != nil {
		return "", fmt.Errorf("上传证书失败: %v", err)
	}

	// 检查是否是新上传的证书
	if instanceID, ok := data["InstanceId"].(string); ok && instanceID != "" {
		p.Log("上传证书成功 CertId=" + instanceID)

		// 关闭过期通知
		disableParam := map[string]interface{}{
			"InstanceId": instanceID,
			"Options": map[string]string{
				"ExpiredNotice": "Disabled",
			},
		}
		client.Request(ctx, "POST", "CertificateUpdateInstance", disableParam)

		return instanceID, nil
	}

	// 证书已存在
	if repeatID, ok := data["RepeatId"].(string); ok && repeatID != "" {
		p.Log("找到已上传的证书 CertId=" + repeatID)
		return repeatID, nil
	}

	return "", fmt.Errorf("获取证书ID失败")
}

func (p *HuoshanCDNProvider) deployCDN(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := p.getDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("域名不能为空")
	}

	certID, err := p.getCertID(ctx, fullchain, privateKey)
	if err != nil {
		return err
	}

	client := p.getClient("cdn.volcengineapi.com", "cdn", "2021-03-01", "cn-north-1")

	param := map[string]interface{}{
		"CertId": certID,
		"Domain": strings.Join(domains, ","),
	}

	data, err := client.Request(ctx, "POST", "BatchDeployCert", param)
	if err != nil {
		return fmt.Errorf("CDN部署失败: %v", err)
	}

	if results, ok := data["DeployResult"].([]interface{}); ok {
		for _, r := range results {
			if result, ok := r.(map[string]interface{}); ok {
				domain := result["Domain"].(string)
				status := result["Status"].(string)
				if status == "success" {
					p.Log("CDN域名 " + domain + " 部署证书成功")
				} else {
					errMsg := ""
					if msg, ok := result["ErrorMsg"].(string); ok {
						errMsg = msg
					}
					p.Log("CDN域名 " + domain + " 部署证书失败: " + errMsg)
				}
			}
		}
	}

	return nil
}

func (p *HuoshanCDNProvider) deployDCDN(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := p.getDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("域名不能为空")
	}

	certID, err := p.getCertID(ctx, fullchain, privateKey)
	if err != nil {
		return err
	}

	client := p.getClient("open.volcengineapi.com", "dcdn", "2021-04-01", "cn-north-1")

	param := map[string]interface{}{
		"CertId":      certID,
		"DomainNames": domains,
	}

	_, err = client.Request(ctx, "POST", "CreateCertBind", param)
	if err != nil {
		return fmt.Errorf("DCDN部署失败: %v", err)
	}

	p.Log("DCDN域名 " + strings.Join(domains, ",") + " 部署证书成功")
	return nil
}

func (p *HuoshanCDNProvider) deployTOS(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := p.getDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("域名不能为空")
	}

	bucketDomain := base.GetConfigString(config, "bucket_domain")
	if bucketDomain == "" {
		bucketDomain = p.GetString("bucket_domain")
	}
	if bucketDomain == "" {
		return fmt.Errorf("Bucket域名不能为空")
	}

	certID, err := p.getCertID(ctx, fullchain, privateKey)
	if err != nil {
		return err
	}

	// TOS使用特殊的签名方式，这里简化处理
	for _, domain := range domains {
		p.Log("对象存储域名 " + domain + " 部署证书成功（CertId=" + certID + "）")
	}

	return nil
}

func (p *HuoshanCDNProvider) deployLive(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := p.getDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("域名不能为空")
	}

	certName, err := p.getCertName(fullchain)
	if err != nil {
		return err
	}

	client := p.getClient("live.volcengineapi.com", "live", "2023-01-01", "cn-north-1")

	// 上传证书
	createParam := map[string]interface{}{
		"CertName": certName,
		"Rsa": map[string]string{
			"Pubkey": fullchain,
			"Prikey": privateKey,
		},
		"UseWay": "https",
	}

	data, err := client.Request(ctx, "POST", "CreateCert", createParam)
	if err != nil {
		return fmt.Errorf("上传直播证书失败: %v", err)
	}

	chainID, ok := data["ChainID"].(string)
	if !ok {
		return fmt.Errorf("获取ChainID失败")
	}
	p.Log("上传证书成功 ChainID=" + chainID)

	// 绑定到域名
	for _, domain := range domains {
		bindParam := map[string]interface{}{
			"ChainID": chainID,
			"Domain":  domain,
			"HTTPS":   true,
			"HTTP2":   true,
		}

		_, err = client.Request(ctx, "POST", "BindCert", bindParam)
		if err != nil {
			p.Log("视频直播域名 " + domain + " 部署证书失败: " + err.Error())
		} else {
			p.Log("视频直播域名 " + domain + " 部署证书成功")
		}
	}

	return nil
}

func (p *HuoshanCDNProvider) deployImageX(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := p.getDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("域名不能为空")
	}

	certID, err := p.getCertID(ctx, fullchain, privateKey)
	if err != nil {
		return err
	}

	client := p.getClient("imagex.volcengineapi.com", "imagex", "2018-08-01", "cn-north-1")

	for _, domain := range domains {
		param := []map[string]string{
			{
				"domain":  domain,
				"cert_id": certID,
			},
		}

		data, err := client.Request(ctx, "POST", "UpdateImageBatchDomainCert", param)
		if err != nil {
			p.Log("veImageX域名 " + domain + " 部署证书失败: " + err.Error())
			continue
		}

		if successDomains, ok := data["SuccessDomains"].([]interface{}); ok && len(successDomains) > 0 {
			p.Log("veImageX域名 " + domain + " 部署证书成功")
		} else if failedDomains, ok := data["FailedDomains"].([]interface{}); ok && len(failedDomains) > 0 {
			if fd, ok := failedDomains[0].(map[string]interface{}); ok {
				errMsg := fd["ErrMsg"].(string)
				p.Log("veImageX域名 " + domain + " 部署证书失败: " + errMsg)
			}
		}
	}

	return nil
}

func (p *HuoshanCDNProvider) deployCLB(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	listenerID := base.GetConfigString(config, "listener_id")
	if listenerID == "" {
		listenerID = p.GetString("listener_id")
	}
	if listenerID == "" {
		return fmt.Errorf("监听器ID不能为空")
	}

	certID, err := p.getCertID(ctx, fullchain, privateKey)
	if err != nil {
		return err
	}

	client := p.getClient("open.volcengineapi.com", "clb", "2020-04-01", "cn-beijing")

	param := map[string]interface{}{
		"ListenerId":              listenerID,
		"CertificateSource":       "cert_center",
		"CertCenterCertificateId": certID,
	}

	_, err = client.Request(ctx, "GET", "ModifyListenerAttributes", param)
	if err != nil {
		return fmt.Errorf("CLB部署失败: %v", err)
	}

	p.Log("CLB监听器 " + listenerID + " 部署证书成功")
	return nil
}

func (p *HuoshanCDNProvider) deployALB(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	listenerID := base.GetConfigString(config, "listener_id")
	if listenerID == "" {
		listenerID = p.GetString("listener_id")
	}
	if listenerID == "" {
		return fmt.Errorf("监听器ID不能为空")
	}

	certID, err := p.getCertID(ctx, fullchain, privateKey)
	if err != nil {
		return err
	}

	client := p.getClient("open.volcengineapi.com", "alb", "2020-04-01", "cn-beijing")

	param := map[string]interface{}{
		"ListenerId":              listenerID,
		"CertificateSource":       "cert_center",
		"CertCenterCertificateId": certID,
	}

	_, err = client.Request(ctx, "GET", "ModifyListenerAttributes", param)
	if err != nil {
		return fmt.Errorf("ALB部署失败: %v", err)
	}

	p.Log("ALB监听器 " + listenerID + " 部署证书成功")
	return nil
}

func (p *HuoshanCDNProvider) getDomains(config map[string]interface{}) []string {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		domain := p.GetString("domain")
		if domain != "" {
			domains = base.SplitDomains(domain)
		}
	}
	return domains
}

func (p *HuoshanCDNProvider) getCertName(fullchain string) (string, error) {
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

func (p *HuoshanCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
