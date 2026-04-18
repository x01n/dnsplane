package ucloud

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	defaultAPIUrl = "https://api.ucloud.cn/"
)

func init() {
	cert.Register("ucloud", NewProvider, cert.ProviderConfig{
		Type: "ucloud",
		Name: "UCloud",
		Icon: "ucloud.png",
		Config: []cert.ConfigField{
			{Name: "公钥 (PublicKey)", Key: "public_key", Type: "input", Required: true},
			{Name: "私钥 (PrivateKey)", Key: "private_key", Type: "input", Required: true},
			{Name: "姓名", Key: "username", Type: "input", Required: true},
			{Name: "手机号码", Key: "phone", Type: "input", Required: true},
			{Name: "邮箱地址", Key: "email", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		CNAME:    false,
		IsDeploy: false,
	})
}

type Provider struct {
	publicKey  string
	privateKey string
	username   string
	phone      string
	email      string
	proxy      bool
	client     *http.Client
	logger     cert.Logger
}

func NewProvider(config, ext map[string]interface{}) cert.Provider {
	p := &Provider{
		publicKey:  getString(config, "public_key"),
		privateKey: getString(config, "private_key"),
		username:   getString(config, "username"),
		phone:      getString(config, "phone"),
		email:      getString(config, "email"),
		client:     &http.Client{Timeout: 30 * time.Second},
	}
	if v, ok := config["proxy"]; ok {
		p.proxy = fmt.Sprintf("%v", v) == "1"
	}
	return p
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func (p *Provider) SetLogger(logger cert.Logger) {
	p.logger = logger
}

func (p *Provider) log(msg string) {
	if p.logger != nil {
		p.logger(msg)
	}
}

func (p *Provider) Register(ctx context.Context) (map[string]interface{}, error) {
	// Check credentials
	params := map[string]interface{}{
		"Action": "GetCertificateList",
		"Mode":   "free",
		"Limit":  1,
	}
	_, err := p.request(ctx, params)
	return nil, err
}

func (p *Provider) BuyCert(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	params := map[string]interface{}{
		"Action":           "PurchaseCertificate",
		"CertificateBrand": "TrustAsia",
		"CertificateName":  "TrustAsiaC1DVFree",
		"DomainsCount":     1,
		"ValidYear":        1,
	}
	resp, err := p.request(ctx, params)
	if err != nil {
		return err
	}

	if certID, ok := resp["CertificateID"].(string); ok {
		order.OrderURL = certID // Store CertificateID in OrderURL
		return nil
	}
	return fmt.Errorf("response missing CertificateID")
}

func (p *Provider) CreateOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (map[string][]cert.DNSRecord, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("domains required")
	}

	// If BuyCert wasn't called (which it might not be in the generic flow if CreateOrder is the entry point),
	// we need to call it.
	// However, `processCertOrderAsync` in handler calls `CreateOrder`. It does NOT call `BuyCert` explicitly.
	// So we must call BuyCert inside CreateOrder if we don't have an ID.
	if order.OrderURL == "" {
		if err := p.BuyCert(ctx, domains, order); err != nil {
			return nil, err
		}
	}

	certID := order.OrderURL
	domain := domains[0]

	// ComplementCSRInfo
	csrAlgo := "RSA"
	if keyType == "EC" || keyType == "ECDSA" {
		csrAlgo = "ECDSA"
	}

	csrKeyParam := "2048"
	if keyType == "RSA" {
		if keySize == "4096" {
			csrKeyParam = "4096" // UCloud might support this? PHP code had mapping.
			// PHP: '2048' => '2048', '3072' => '3072', '256' => 'prime256v1', '384' => 'prime384v1'
		} else {
			csrKeyParam = "2048"
		}
	} else {
		if keySize == "384" {
			csrKeyParam = "prime384v1"
		} else {
			csrKeyParam = "prime256v1"
		}
	}

	params := map[string]interface{}{
		"Action":            "ComplementCSRInfo",
		"CertificateID":     certID,
		"Domains":           domain,
		"CSROnline":         1,
		"CSREncryptAlgo":    csrAlgo,
		"CSRKeyParameter":   csrKeyParam,
		"CompanyName":       "Individual",
		"CompanyAddress":    "Address",
		"CompanyRegion":     "Beijing",
		"CompanyCity":       "Beijing",
		"CompanyCountry":    "CN",
		"CompanyDivision":   "IT",
		"CompanyPhone":      p.phone,
		"CompanyPostalCode": "100000",
		"AdminName":         p.username,
		"AdminPhone":        p.phone,
		"AdminEmail":        p.email,
		"AdminTitle":        "Staff",
		"DVAuthMethod":      "DNS",
	}

	if _, err := p.request(ctx, params); err != nil {
		return nil, fmt.Errorf("补全 CSR 信息失败: %w", err)
	}

	time.Sleep(3 * time.Second)

	// GetDVAuthInfo
	authParams := map[string]interface{}{
		"Action":        "GetDVAuthInfo",
		"CertificateID": certID,
	}
	authResp, err := p.request(ctx, authParams)
	if err != nil {
		return nil, fmt.Errorf("获取 DV 认证信息失败: %w", err)
	}

	dnsRecords := make(map[string][]cert.DNSRecord)
	if auths, ok := authResp["Auths"].([]interface{}); ok {
		for _, a := range auths {
			authMap, ok := a.(map[string]interface{})
			if !ok {
				continue
			}
			authKey := getString(authMap, "AuthKey")
			authValue := getString(authMap, "AuthValue")
			authType := getString(authMap, "AuthType") // DNS_TXT or DNS_CNAME
			if authKey == "" || authValue == "" {
				continue
			}

			recordType := "TXT"
			if authType == "DNS_CNAME" {
				recordType = "CNAME"
			}

			// Simple logic to strip domain. Assuming authKey ends with "."+domain or is just domain?
			// PHP: substr($authKey, 0, -(strlen($mainDomain) + 1));
			// We don't have mainDomain easily here without parsing.
			// But usually we just return the full name and type/value, the caller handles logic?
			// `processCertOrderAsync` expects:
			// "域名 " + domainName + " 需要添加以下DNS记录:"
			// It iterates over `dnsRecords` map which is keyed by domainName.
			// So we should map it to the domain.

			// For simplicity, let's just strip the suffix if it exists.
			name := authKey
			if strings.HasSuffix(authKey, "."+domain) {
				name = strings.TrimSuffix(authKey, "."+domain)
			}

			dnsRecords[domain] = append(dnsRecords[domain], cert.DNSRecord{
				Name:  name,
				Type:  recordType,
				Value: authValue,
			})
		}
	}

	return dnsRecords, nil
}

func (p *Provider) AuthOrder(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	// UCloud doesn't have explicit "trigger auth" endpoint, it's automatic.
	// But we can check status.
	return nil
}

func (p *Provider) GetAuthStatus(ctx context.Context, domains []string, order *cert.OrderInfo) (bool, error) {
	params := map[string]interface{}{
		"Action":        "GetCertificateDetailInfo",
		"CertificateID": order.OrderURL,
	}
	resp, err := p.request(ctx, params)
	if err != nil {
		return false, err
	}

	info, ok := resp["CertificateInfo"].(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("invalid response structure")
	}

	stateCode := getString(info, "StateCode")
	// COMPLETED or RENEWED means success
	if stateCode == "COMPLETED" || stateCode == "RENEWED" {
		return true, nil
	}

	if stateCode == "REJECTED" || stateCode == "SECURITY_REVIEW_FAILED" {
		return false, fmt.Errorf("certificate rejected: %s", getString(info, "State"))
	}

	return false, nil
}

func (p *Provider) FinalizeOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (*cert.CertResult, error) {
	certID := order.OrderURL

	// DownloadCertificate
	params := map[string]interface{}{
		"Action":        "DownloadCertificate",
		"CertificateID": certID,
	}
	resp, err := p.request(ctx, params)
	if err != nil {
		return nil, err
	}

	certURL, ok := resp["CertificateUrl"].(string)
	if !ok {
		return nil, fmt.Errorf("missing CertificateUrl")
	}

	/* 下载证书 ZIP（使用带超时的客户端，防止外部服务不可达时永久挂起） */
	dlClient := &http.Client{Timeout: 60 * time.Second}
	zipResp, err := dlClient.Get(certURL)
	if err != nil {
		return nil, err
	}
	defer zipResp.Body.Close()

	bodyBytes, err := io.ReadAll(zipResp.Body)
	if err != nil {
		return nil, err
	}

	zipReader, err := zip.NewReader(bytes.NewReader(bodyBytes), int64(len(bodyBytes)))
	if err != nil {
		return nil, err
	}

	var fullChain, privateKey string

	// Iterate zip to find key and pem
	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		// We look for files in Nginx folder usually, or just by extension
		if strings.HasSuffix(file.Name, ".key") {
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			content, _ := io.ReadAll(rc)
			privateKey = string(content)
			rc.Close()
		} else if strings.HasSuffix(file.Name, ".pem") || strings.HasSuffix(file.Name, ".crt") {
			// Prefer .pem or .crt that looks like full chain
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			content, _ := io.ReadAll(rc)
			fullChain = string(content)
			rc.Close()
		}
	}

	if fullChain == "" || privateKey == "" {
		return nil, fmt.Errorf("could not find certificate files in zip")
	}

	// Get info for result
	infoParams := map[string]interface{}{
		"Action":        "GetCertificateDetailInfo",
		"CertificateID": certID,
	}
	infoResp, err := p.request(ctx, infoParams)
	if err != nil {
		return nil, err
	}

	info, _ := infoResp["CertificateInfo"].(map[string]interface{})
	issuer := getString(info, "CaOrganization")

	issuedDate := int64(0)
	expiredDate := int64(0)

	if v, ok := info["IssuedDate"].(float64); ok {
		issuedDate = int64(v)
	}
	if v, ok := info["ExpiredDate"].(float64); ok {
		expiredDate = int64(v)
	}

	return &cert.CertResult{
		FullChain:  fullChain,
		PrivateKey: privateKey,
		Issuer:     issuer,
		ValidFrom:  issuedDate,
		ValidTo:    expiredDate,
	}, nil
}

func (p *Provider) Revoke(ctx context.Context, order *cert.OrderInfo, pem string) error {
	params := map[string]interface{}{
		"Action":        "RevokeCertificate",
		"CertificateID": order.OrderURL,
		"Reason":        "UserRequest",
	}
	_, err := p.request(ctx, params)
	return err
}

func (p *Provider) Cancel(ctx context.Context, order *cert.OrderInfo) error {
	params := map[string]interface{}{
		"Action":        "CancelCertificateOrder",
		"CertificateID": order.OrderURL,
	}
	p.request(ctx, params)

	delParams := map[string]interface{}{
		"Action":          "DeleteSSLCertificate",
		"CertificateID":   order.OrderURL,
		"CertificateMode": "purchase",
	}
	_, err := p.request(ctx, delParams)
	return err
}

func (p *Provider) request(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	params["PublicKey"] = p.publicKey

	// Signature
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sigStr := ""
	for _, k := range keys {
		sigStr += fmt.Sprintf("%s%v", k, params[k])
	}
	sigStr += p.privateKey

	hash := sha1.Sum([]byte(sigStr))
	params["Signature"] = hex.EncodeToString(hash[:])

	jsonBytes, _ := json.Marshal(params)

	req, err := http.NewRequestWithContext(ctx, "POST", p.defaultAPIUrl(), bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if retCode, ok := result["RetCode"].(float64); ok && retCode == 0 {
		return result, nil
	}

	msg := getString(result, "Message")
	return nil, fmt.Errorf("API 返回错误: %s", msg)
}

func (p *Provider) defaultAPIUrl() string {
	return defaultAPIUrl
}
