package tencent

import (
	"archive/zip"
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
	"sort"
	"strconv"
	"strings"
	"time"
)

/*
 * 腾讯云免费 SSL 证书签发提供商
 * 请求签名算法 TC3-HMAC-SHA256
 */

const (
	/* API 接入域名 */
	defaultEndpoint = "ssl.tencentcloudapi.com"
	/* 服务名 */
	defaultService = "ssl"
	/* API 版本号 */
	defaultVersion = "2019-12-05"
	/* 证书下载格式 */
	downloadServiceType = "nginx"
	requestTimeout      = 30 * time.Second
)

func init() {
	/* Type 留空：保留 providers.go 中已定义的表单字段与说明，仅补全工厂函数 */
	cert.Register("tencent", NewProvider, cert.ProviderConfig{})
}

/* Provider 腾讯云 SSL 证书提供商 */
type Provider struct {
	secretID  string
	secretKey string
	email     string
	client    *http.Client
	logger    cert.Logger
	// apiURL 独立成字段便于测试注入本地服务端，生产路径始终使用 defaultEndpoint
	apiURL string
}

/*
 * NewProvider 创建腾讯云 SSL 提供商实例
 * @param config 账户配置（secret_id、secret_key、email）
 */
func NewProvider(config, ext map[string]interface{}) cert.Provider {
	return &Provider{
		secretID:  getString(config, "secret_id"),
		secretKey: getString(config, "secret_key"),
		email:     getString(config, "email"),
		client:    &http.Client{Timeout: requestTimeout},
		apiURL:    "https://" + defaultEndpoint + "/",
	}
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}

/* SetLogger 设置日志记录器 */
func (p *Provider) SetLogger(logger cert.Logger) {
	p.logger = logger
}

func (p *Provider) log(msg string) {
	if p.logger != nil {
		p.logger(msg)
	}
}

/*
 * Register 校验账户可用性
 */
func (p *Provider) Register(ctx context.Context) (map[string]interface{}, error) {
	if p.secretID == "" || p.secretKey == "" || p.email == "" {
		return nil, fmt.Errorf("必填参数不能为空")
	}
	if _, err := p.request(ctx, "DescribeCertificates", nil); err != nil {
		return nil, err
	}
	return nil, nil
}

/* BuyCert 腾讯云免费证书无需购买*/
func (p *Provider) BuyCert(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	return nil
}

/*
 * CreateOrder 申请免费证书并返回 DNS 验证记录
 */
func (p *Provider) CreateOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (map[string][]cert.DNSRecord, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("域名列表不能为空")
	}
	keyParameter := "2048"
	if strings.EqualFold(keyType, "ECC") {
		keyParameter = "prime256v1"
	}

	params := map[string]interface{}{
		"DvAuthMethod":    "DNS",
		"DomainName":      domains[0],
		"ContactEmail":    p.email,
		"CsrEncryptAlgo":  keyType,
		"CsrKeyParameter": keyParameter,
	}
	resp, err := p.request(ctx, "ApplyCertificate", params)
	if err != nil {
		return nil, err
	}

	certificateID := getString(resp, "CertificateId")
	if certificateID == "" {
		return nil, fmt.Errorf("证书申请失败，CertificateId为空")
	}
	order.OrderURL = certificateID

	detail, err := p.request(ctx, "DescribeCertificate", map[string]interface{}{
		"CertificateId": certificateID,
	})
	if err != nil {
		return nil, err
	}

	return buildDNSRecords(detail), nil
}

/*
 * buildDNSRecords 从 DescribeCertificate 响应组装验证记录
 */
func buildDNSRecords(detail map[string]interface{}) map[string][]cert.DNSRecord {
	dnsRecords := make(map[string][]cert.DNSRecord)

	dvAuthDetail, ok := detail["DvAuthDetail"].(map[string]interface{})
	if !ok {
		return dnsRecords
	}
	auths, ok := dvAuthDetail["DvAuths"].([]interface{})
	if !ok {
		return dnsRecords
	}

	for _, item := range auths {
		auth, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		authKey := getString(auth, "DvAuthKey")
		if authKey == "" {
			continue
		}
		recordType := getString(auth, "DvAuthVerifyType")
		if recordType == "" {
			recordType = "CNAME"
		}
		mainDomain := getMainDomain(authKey)
		dnsRecords[mainDomain] = append(dnsRecords[mainDomain], cert.DNSRecord{
			Name:  strings.TrimSuffix(authKey, "."+mainDomain),
			Type:  recordType,
			Value: getString(auth, "DvAuthValue"),
		})
	}
	return dnsRecords
}

/*
 * AuthOrder 触发域名验证
 */
func (p *Provider) AuthOrder(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	detail, err := p.request(ctx, "DescribeCertificate", map[string]interface{}{
		"CertificateId": order.OrderURL,
	})
	if err != nil {
		return err
	}
	if status := getInt(detail, "Status"); status == 0 || status == 4 {
		if _, err := p.request(ctx, "CompleteCertificate", map[string]interface{}{
			"CertificateId": order.OrderURL,
		}); err != nil {
			return err
		}
		time.Sleep(3 * time.Second)
	}
	return nil
}

/*
 * GetAuthStatus 查询验证状态
 */
func (p *Provider) GetAuthStatus(ctx context.Context, domains []string, order *cert.OrderInfo) (bool, error) {
	detail, err := p.request(ctx, "DescribeCertificate", map[string]interface{}{
		"CertificateId": order.OrderURL,
	})
	if err != nil {
		return false, err
	}
	switch getInt(detail, "Status") {
	case 1:
		return true, nil
	case 2:
		if msg := getString(detail, "StatusMsg"); msg != "" {
			return false, fmt.Errorf("证书审核失败: %s", msg)
		}
		return false, fmt.Errorf("证书审核失败")
	default:
		return false, nil
	}
}

/*
 * FinalizeOrder 下载并解析已签发证书
 */
func (p *Provider) FinalizeOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (*cert.CertResult, error) {
	if _, err := p.request(ctx, "ModifyCertificatesExpiringNotificationSwitch", map[string]interface{}{
		"CertificateIds": []string{order.OrderURL},
		"SwitchStatus":   1,
	}); err != nil {
		return nil, err
	}

	resp, err := p.request(ctx, "DescribeDownloadCertificateUrl", map[string]interface{}{
		"CertificateId": order.OrderURL,
		"ServiceType":   downloadServiceType,
	})
	if err != nil {
		return nil, err
	}
	downloadURL := getString(resp, "DownloadCertificateUrl")
	if downloadURL == "" {
		return nil, fmt.Errorf("下载证书失败")
	}

	zipData, err := p.download(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	privateKey, fullChain, err := extractCertificate(zipData)
	if err != nil {
		return nil, err
	}

	return buildCertResult(fullChain, privateKey)
}

func (p *Provider) Revoke(ctx context.Context, order *cert.OrderInfo, pem string) error {
	_, err := p.request(ctx, "RevokeCertificate", map[string]interface{}{
		"CertificateId": order.OrderURL,
	})
	return err
}

/* Cancel 取消审核中的证书订单 */
func (p *Provider) Cancel(ctx context.Context, order *cert.OrderInfo) error {
	_, err := p.request(ctx, "CancelAuditCertificate", map[string]interface{}{
		"CertificateId": order.OrderURL,
	})
	return err
}

/*
 * request 发起一次腾讯云 API 调用
 * @param action 接口名（如 ApplyCertificate）
 * @param params 请求参数，nil 或全空时序列化为 {}
 * @return Response 节点内容
 */
func (p *Provider) request(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
	payload, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().Unix()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", p.sign(payload, timestamp))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Version", defaultVersion)

	p.log("Action:" + action + "\nRequest:" + string(payload))

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求腾讯云失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	p.log("Response:" + string(body))

	var result struct {
		Response map[string]interface{} `json:"Response"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Response == nil {
		return nil, fmt.Errorf("返回数据解析失败")
	}
	if errInfo, ok := result.Response["Error"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("腾讯云API错误: %s - %s",
			getString(errInfo, "Code"), getString(errInfo, "Message"))
	}
	return result.Response, nil
}

/*
 * marshalParams 序列化请求参数
 */
func marshalParams(params map[string]interface{}) ([]byte, error) {
	filtered := make(map[string]interface{}, len(params))
	for k, v := range params {
		if v != nil {
			filtered[k] = v
		}
	}
	if len(filtered) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(filtered)
}

/*
 * sign 计算 TC3-HMAC-SHA256 签名
 * 凭证范围 <date>/<service>/tc3_request，密钥按 日期 → 服务 → tc3_request 逐级派生
 */
func (p *Provider) sign(payload []byte, timestamp int64) string {
	const (
		algorithm      = "TC3-HMAC-SHA256"
		canonicalURI   = "/"
		signedHeaders  = "content-type;host"
		canonicalType  = "application/json; charset=utf-8"
		credentialTerm = "tc3_request"
	)

	canonicalHeaders := "content-type:" + canonicalType + "\nhost:" + defaultEndpoint + "\n"
	canonicalRequest := "POST\n" + canonicalURI + "\n\n" + canonicalHeaders + "\n" +
		signedHeaders + "\n" + sha256Hex(payload)

	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	credentialScope := date + "/" + defaultService + "/" + credentialTerm
	stringToSign := algorithm + "\n" + strconv.FormatInt(timestamp, 10) + "\n" +
		credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))

	secretDate := hmacSHA256([]byte("TC3"+p.secretKey), date)
	secretService := hmacSHA256(secretDate, defaultService)
	secretSigning := hmacSHA256(secretService, credentialTerm)
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	return algorithm + " Credential=" + p.secretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

/* download 下载证书 ZIP，独立客户端避免复用 API 客户端的短超时 */
func (p *Provider) download(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载证书失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("下载证书失败")
	}
	return data, nil
}

/*
 * extractCertificate 从下载包中取出私钥与证书链
 */
func extractCertificate(zipData []byte) (privateKey, fullChain string, err error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return "", "", fmt.Errorf("解压证书失败: %w", err)
	}

	names := make([]string, 0, len(reader.File))
	entries := make(map[string]*zip.File, len(reader.File))
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		names = append(names, f.Name)
		entries[f.Name] = f
	}
	sort.Strings(names)

	for _, name := range names {
		switch {
		case strings.HasSuffix(name, ".key"):
			if content, ok := readZipEntry(entries[name]); ok {
				privateKey = content
			}
		case strings.HasSuffix(name, ".crt"):
			if content, ok := readZipEntry(entries[name]); ok {
				fullChain = content
			}
		}
	}
	if privateKey == "" || fullChain == "" {
		return "", "", fmt.Errorf("解压后的证书文件夹内未找到证书文件")
	}
	return privateKey, fullChain, nil
}

func readZipEntry(f *zip.File) (string, bool) {
	rc, err := f.Open()
	if err != nil {
		return "", false
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		return "", false
	}
	return string(content), true
}

func buildCertResult(fullChain, privateKey string) (*cert.CertResult, error) {
	block, _ := pem.Decode([]byte(fullChain))
	if block == nil {
		return nil, fmt.Errorf("证书解析失败")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("证书解析失败")
	}
	return &cert.CertResult{
		FullChain:  fullChain,
		PrivateKey: privateKey,
		Issuer:     parsed.Issuer.CommonName,
		ValidFrom:  parsed.NotBefore.Unix(),
		ValidTo:    parsed.NotAfter.Unix(),
	}, nil
}

/* getMainDomain 取域名的主域名（最后两段），与 internal/cert/acme 的同类实现保持一致 */
func getMainDomain(domain string) string {
	domain = strings.TrimPrefix(domain, "*.")
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return domain
}
