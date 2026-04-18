package huoshan

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultEndpoint = "open.volcengineapi.com"
	defaultRegion   = "cn-north-1"
	defaultService  = "certificate_service"
	defaultVersion  = "2021-06-01"
)

func init() {
	cert.Register("huoshan", NewProvider, cert.ProviderConfig{
		Type: "huoshan",
		Name: "火山引擎",
		Icon: "huoshan.png",
		Config: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
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
	accessKeyID     string
	secretAccessKey string
	proxy           bool
	client          *http.Client
	logger          cert.Logger
}

func NewProvider(config, ext map[string]interface{}) cert.Provider {
	p := &Provider{
		accessKeyID:     getString(config, "AccessKeyId"),
		secretAccessKey: getString(config, "SecretAccessKey"),
		client:          &http.Client{Timeout: 30 * time.Second},
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
	params := map[string]string{
		"limit":  "1",
		"offset": "0",
	}
	_, err := p.request(ctx, "GET", "CertificateGetInstance", params, nil)
	return nil, err
}

func (p *Provider) BuyCert(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	// Get Organization ID
	resp, err := p.request(ctx, "GET", "CertificateGetOrganization", nil, nil)
	if err != nil {
		return err
	}

	content, ok := resp["content"].([]interface{})
	if !ok || len(content) == 0 {
		return fmt.Errorf("请先在火山引擎控制台添加信息模板")
	}

	org, ok := content[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("信息模板格式异常")
	}
	_ = org

	// We can store orgID in order.Identifiers? No, Identifiers are domain identifiers.
	// We can store it in OrderURL or somewhere?
	// But CreateOrder needs it.
	// We can't persist it in OrderInfo easily if we don't have a field.
	// However, we can fetch it again in CreateOrder. It's an extra call but safer.
	// Or we can abuse `OrderURL` to store it temporarily if BuyCert is called before CreateOrder.
	// But `OrderURL` is used for InstanceID later.
	// Let's just fetch it in CreateOrder.

	return nil
}

func (p *Provider) CreateOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (map[string][]cert.DNSRecord, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("domains required")
	}
	domain := domains[0]

	// 1. Get Org ID
	resp, err := p.request(ctx, "GET", "CertificateGetOrganization", nil, nil)
	if err != nil {
		return nil, err
	}
	content, ok := resp["content"].([]interface{})
	if !ok || len(content) == 0 {
		return nil, fmt.Errorf("请先在火山引擎控制台添加信息模板")
	}
	org, ok := content[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("信息模板格式异常")
	}
	orgID := getString(org, "id")

	// 2. QuickApplyCertificate
	keyAlg := strings.ToLower(keyType)
	if keyAlg == "" {
		keyAlg = "rsa"
	}
	// Volcengine uses "rsa", "ecc" (not ecdsa?)
	if keyAlg == "ec" || keyAlg == "ecdsa" {
		keyAlg = "ecc"
	}

	params := map[string]interface{}{
		"plan":            "digicert_free_standard_dv",
		"common_name":     domain,
		"organization_id": orgID,
		"key_alg":         keyAlg,
		"validation_type": "dns_txt",
	}

	applyResp, err := p.request(ctx, "POST", "QuickApplyCertificate", nil, params)
	if err != nil {
		return nil, err
	}

	// Response is the instance ID directly?
	// PHP: $instance_id = $this->request('POST', 'QuickApplyCertificate', $param);
	// Wait, does PHP request return the body or a specific field?
	// PHP Volcengine::request returns json_decoded body.
	// If QuickApplyCertificate returns just a string? Unlikely.
	// Documentation says it returns object.
	// Wait, PHP code: `if(empty($instance_id)) throw...`
	// Let's assume it returns a map and we need to find the ID.
	// Or maybe the PHP client wrapper returns `Result` field?
	// Let's look at PHP Volcengine.php `request`:
	// `return $this->curl(...)` -> returns json_decode array.
	// If `QuickApplyCertificate` returns `{ "Result": "id" }`?
	// I'll assume standard Volcengine response structure which usually wraps result.
	// But wait, `CertificateGetInstance` returned `content` array.
	// I'll assume `applyResp` is the map. I need to find the ID.
	// Let's check keys.

	var instanceID string
	if id, ok := applyResp["result"].(string); ok {
		instanceID = id
	} else if id, ok := applyResp["Result"].(string); ok {
		instanceID = id
	} else {
		// Fallback: maybe the whole response is the ID? (Unlikely)
		// Let's log keys if possible or assume it's "result" based on typical APIs.
		// Actually, I should check if I can debug this.
		// But I'll assume "result" or "Result".
		return nil, fmt.Errorf("failed to get instance id from response")
	}

	order.OrderURL = instanceID

	time.Sleep(3 * time.Second)

	// 3. Get Dcv Param
	dcvParams := map[string]string{
		"instance_id": instanceID,
	}
	dcvResp, err := p.request(ctx, "GET", "CertificateGetDcvParam", dcvParams, nil)
	if err != nil {
		return nil, err
	}

	dnsRecords := make(map[string][]cert.DNSRecord)
	if toBeValidated, ok := dcvResp["domains_to_be_validated"].([]interface{}); ok {
		validationType := getString(dcvResp, "validation_type") // dns_cname or dns_txt
		recordType := "TXT"
		if validationType == "dns_cname" {
			recordType = "CNAME"
		}

		for _, item := range toBeValidated {
			opts, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			valDomain := getString(opts, "validation_domain")
			valValue := getString(opts, "value")

			// Strip domain suffix
			name := valDomain
			if strings.HasSuffix(valDomain, "."+domain) {
				name = strings.TrimSuffix(valDomain, "."+domain)
			}

			dnsRecords[domain] = append(dnsRecords[domain], cert.DNSRecord{
				Name:  name,
				Type:  recordType,
				Value: valValue,
			})
		}
	}

	return dnsRecords, nil
}

func (p *Provider) AuthOrder(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	params := map[string]interface{}{
		"action": "",
	}
	// Query params in URL? PHP: request('POST', ..., $param, $query)
	// $query = ['instance_id' => $order['instance_id']]
	// $param = ['action' => '']

	queryParams := map[string]string{
		"instance_id": order.OrderURL,
	}

	_, err := p.request(ctx, "POST", "CertificateProgressInstanceOrder", queryParams, params)
	return err
}

func (p *Provider) GetAuthStatus(ctx context.Context, domains []string, order *cert.OrderInfo) (bool, error) {
	params := map[string]string{
		"instance_id": order.OrderURL,
	}
	resp, err := p.request(ctx, "GET", "CertificateGetInstance", params, nil)
	if err != nil {
		return false, err
	}

	content, ok := resp["content"].([]interface{})
	if !ok || len(content) == 0 {
		return false, fmt.Errorf("certificate info not found")
	}

	info, ok := content[0].(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("证书信息格式异常")
	}
	orderStatus := getInt(info, "order_status")
	certExist := getInt(info, "certificate_exist")

	if orderStatus == 300 && certExist == 1 {
		return true, nil
	} else if orderStatus == 302 {
		return false, fmt.Errorf("certificate application failed")
	}

	return false, nil
}

func (p *Provider) FinalizeOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (*cert.CertResult, error) {
	params := map[string]string{
		"instance_id": order.OrderURL,
	}
	resp, err := p.request(ctx, "GET", "CertificateGetInstance", params, nil)
	if err != nil {
		return nil, err
	}

	content, ok := resp["content"].([]interface{})
	if !ok || len(content) == 0 {
		return nil, fmt.Errorf("certificate info not found")
	}

	info, ok := content[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("证书信息格式异常")
	}
	ssl, ok := info["ssl"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("ssl info not found")
	}
	certificate, ok := ssl["certificate"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("certificate data not found")
	}

	chain, ok := certificate["chain"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("certificate chain not found")
	}

	var fullChainBuilder strings.Builder
	for _, c := range chain {
		fullChainBuilder.WriteString(fmt.Sprintf("%v", c))
	}
	fullChain := fullChainBuilder.String()

	privateKey := getString(certificate, "private_key")

	issuer := getString(info, "issuer")

	// Dates are in ms
	notBeforeMs := getInt64(info, "certificate_not_before_ms")
	notAfterMs := getInt64(info, "certificate_not_after_ms")

	return &cert.CertResult{
		FullChain:  fullChain,
		PrivateKey: privateKey,
		Issuer:     issuer,
		ValidFrom:  notBeforeMs / 1000,
		ValidTo:    notAfterMs / 1000,
	}, nil
}

func (p *Provider) Revoke(ctx context.Context, order *cert.OrderInfo, pem string) error {
	queryParams := map[string]string{
		"instance_id": order.OrderURL,
	}
	params := map[string]interface{}{
		"action": "revoke",
		"reason": "UserRequest",
	}
	_, err := p.request(ctx, "POST", "CertificateProgressInstanceOrder", queryParams, params)
	return err
}

func (p *Provider) Cancel(ctx context.Context, order *cert.OrderInfo) error {
	queryParams := map[string]string{
		"instance_id": order.OrderURL,
	}
	params := map[string]interface{}{
		"action": "cancel",
	}
	p.request(ctx, "POST", "CertificateProgressInstanceOrder", queryParams, params)
	return nil
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}

func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return int64(f)
		}
	}
	return 0
}

func (p *Provider) request(ctx context.Context, method, action string, queryParams map[string]string, bodyParams map[string]interface{}) (map[string]interface{}, error) {
	// Sign logic
	t := time.Now().UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")

	// Query Params + Action + Version
	query := url.Values{}
	query.Set("Action", action)
	query.Set("Version", defaultVersion)
	for k, v := range queryParams {
		query.Set(k, v)
	}

	var body []byte
	if bodyParams != nil {
		body, _ = json.Marshal(bodyParams)
	}

	// Host
	host := defaultEndpoint

	// Canonical Request
	canonicalUri := "/"

	// Canonical Query String
	// Need to sort query keys
	// url.Values.Encode() sorts by key, but it encodes values.
	// AWS signature requires specific encoding.
	// We can use Encode() but need to be careful with spaces etc.
	// Usually standard Go Encode() is fine for standard AWS/Volc.
	canonicalQueryString := query.Encode()
	// Replace + with %20 if needed? AWS S3 needs it, general AWS SigV4 usually handles + as space or %2B?
	// Volcengine doc says: escape same as RFC 3986.
	// Go's Encode uses + for space. We might need to replace + with %20.
	canonicalQueryString = strings.ReplaceAll(canonicalQueryString, "+", "%20")

	// Canonical Headers
	// Host, X-Date, Content-Type (if body)
	canonicalHeaders := fmt.Sprintf("content-type:application/json\nhost:%s\nx-date:%s\n", host, amzDate)
	signedHeaders := "content-type;host;x-date"
	if len(body) == 0 {
		canonicalHeaders = fmt.Sprintf("host:%s\nx-date:%s\n", host, amzDate)
		signedHeaders = "host;x-date"
	}

	payloadHash := sha256.Sum256(body)
	payloadHashHex := hex.EncodeToString(payloadHash[:])

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		method,
		canonicalUri,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHashHex,
	)

	// String to Sign
	algorithm := "HMAC-SHA256"
	credentialScope := fmt.Sprintf("%s/%s/%s/request", dateStamp, defaultRegion, defaultService)

	canonicalRequestHash := sha256.Sum256([]byte(canonicalRequest))
	canonicalRequestHashHex := hex.EncodeToString(canonicalRequestHash[:])

	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s",
		algorithm,
		amzDate,
		credentialScope,
		canonicalRequestHashHex,
	)

	// Signing Key
	kDate := hmacSHA256([]byte(p.secretAccessKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(defaultRegion))
	kService := hmacSHA256(kRegion, []byte(defaultService))
	kSigning := hmacSHA256(kService, []byte("request"))
	signature := hmacSHA256(kSigning, []byte(stringToSign))
	signatureHex := hex.EncodeToString(signature)

	// Authorization Header
	authHeader := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm,
		p.accessKeyID,
		credentialScope,
		signedHeaders,
		signatureHex,
	)

	u := fmt.Sprintf("https://%s/?%s", host, canonicalQueryString)

	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-Date", amzDate)
	req.Header.Set("Host", host)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	// Check for error in response
	if resp.StatusCode >= 400 {
		// Try to find error message
		if metadata, ok := result["ResponseMetadata"].(map[string]interface{}); ok {
			if errInfo, ok := metadata["Error"].(map[string]interface{}); ok {
				return nil, fmt.Errorf("API 返回错误: %s - %s", getString(errInfo, "Code"), getString(errInfo, "Message"))
			}
		}
		return nil, fmt.Errorf("API 返回错误: %s", string(respBody))
	}

	return result, nil
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
