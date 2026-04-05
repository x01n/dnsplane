package acme

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"main/internal/cert"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const (
	LetsEncryptURL     = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
	ZeroSSLURL         = "https://acme.zerossl.com/v2/DV90"
	GoogleACMEURL      = "https://dv.acme-v02.api.pki.goog/directory"
	GoogleStagingURL   = "https://dv.acme-v02.test-api.pki.goog/directory"
	LiteSSLURL         = "https://acme.freessl.cn/v2/DV90"
)

func init() {
	cert.Register("letsencrypt", NewLetsEncryptProvider, cert.ProviderConfig{
		Type: "letsencrypt",
		Name: "Let's Encrypt",
		Icon: "letsencrypt.png",
		Config: []cert.ConfigField{
			{Name: "邮箱地址", Key: "email", Type: "input", Required: true},
		},
		CNAME: true,
	})

	cert.Register("zerossl", NewZeroSSLProvider, cert.ProviderConfig{
		Type: "zerossl",
		Name: "ZeroSSL",
		Icon: "zerossl.png",
		Config: []cert.ConfigField{
			{Name: "邮箱地址", Key: "email", Type: "input", Required: true},
			{Name: "EAB KID", Key: "eab_kid", Type: "input", Required: true},
			{Name: "EAB HMAC Key", Key: "eab_hmac_key", Type: "input", Required: true},
		},
		CNAME: true,
	})

	/* Google Trust Services ACME 提供商 */
	cert.Register("google", NewGoogleProvider, cert.ProviderConfig{})

	/* LiteSSL (freessl.cn) ACME 提供商 */
	cert.Register("litessl", NewLiteSSLProvider, cert.ProviderConfig{})

	/* 自定义 ACME 提供商 */
	cert.Register("customacme", NewCustomACMEProvider, cert.ProviderConfig{})
}

type ACMEClient struct {
	directoryURL string
	email        string
	eabKID       string
	eabHMACKey   string
	accountKey   crypto.PrivateKey
	accountURL   string
	directory    *Directory
	client       *http.Client
	logger       cert.Logger
	nonce        string // 缓存的 nonce，优先复用
}

type Directory struct {
	NewNonce   string `json:"newNonce"`
	NewAccount string `json:"newAccount"`
	NewOrder   string `json:"newOrder"`
	RevokeCert string `json:"revokeCert"`
	KeyChange  string `json:"keyChange"`
}

func NewLetsEncryptProvider(config, ext map[string]interface{}) cert.Provider {
	client := &ACMEClient{
		directoryURL: LetsEncryptURL,
		email:        getString(config, "email"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
	if ext != nil {
		if key, ok := ext["account_key"].(string); ok {
			client.accountKey = parsePrivateKey(key)
		}
		if url, ok := ext["account_url"].(string); ok {
			client.accountURL = url
		}
	}
	return client
}

func NewZeroSSLProvider(config, ext map[string]interface{}) cert.Provider {
	client := &ACMEClient{
		directoryURL: ZeroSSLURL,
		email:        getString(config, "email"),
		eabKID:       getString(config, "eab_kid"),
		eabHMACKey:   getString(config, "eab_hmac_key"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
	if ext != nil {
		if key, ok := ext["account_key"].(string); ok {
			client.accountKey = parsePrivateKey(key)
		}
		if url, ok := ext["account_url"].(string); ok {
			client.accountURL = url
		}
	}
	return client
}

/*
 * NewGoogleProvider Google Trust Services ACME 提供商
 * 功能：使用 Google PKI ACME 接口签发免费 SSL 证书，需要 EAB 凭证
 * @param config - 账户配置（email, kid, key, mode）
 * @param ext - 扩展信息（account_key, account_url）
 * @returns cert.Provider
 */
func NewGoogleProvider(config, ext map[string]interface{}) cert.Provider {
	directoryURL := GoogleACMEURL
	if getString(config, "mode") == "staging" {
		directoryURL = GoogleStagingURL
	}
	client := &ACMEClient{
		directoryURL: directoryURL,
		email:        getString(config, "email"),
		eabKID:       getString(config, "kid"),
		eabHMACKey:   getString(config, "key"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
	if ext != nil {
		if key, ok := ext["account_key"].(string); ok {
			client.accountKey = parsePrivateKey(key)
		}
		if url, ok := ext["account_url"].(string); ok {
			client.accountURL = url
		}
	}
	return client
}

/*
 * NewLiteSSLProvider LiteSSL (freessl.cn) ACME 提供商
 * 功能：使用 freessl.cn 的 ACME 接口签发免费 SSL 证书，需要 EAB 凭证
 * @param config - 账户配置（email, kid, key）
 * @param ext - 扩展信息（account_key, account_url）
 * @returns cert.Provider
 */
func NewLiteSSLProvider(config, ext map[string]interface{}) cert.Provider {
	client := &ACMEClient{
		directoryURL: LiteSSLURL,
		email:        getString(config, "email"),
		eabKID:       getString(config, "kid"),
		eabHMACKey:   getString(config, "key"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
	if ext != nil {
		if key, ok := ext["account_key"].(string); ok {
			client.accountKey = parsePrivateKey(key)
		}
		if url, ok := ext["account_url"].(string); ok {
			client.accountURL = url
		}
	}
	return client
}

/*
 * NewCustomACMEProvider 自定义 ACME 提供商
 * 功能：用户自行指定 ACME Directory 地址，可选 EAB 认证
 * @param config - 账户配置（directory, email, kid, key）
 * @param ext - 扩展信息（account_key, account_url）
 * @returns cert.Provider
 */
func NewCustomACMEProvider(config, ext map[string]interface{}) cert.Provider {
	client := &ACMEClient{
		directoryURL: getString(config, "directory"),
		email:        getString(config, "email"),
		eabKID:       getString(config, "kid"),
		eabHMACKey:   getString(config, "key"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
	if ext != nil {
		if key, ok := ext["account_key"].(string); ok {
			client.accountKey = parsePrivateKey(key)
		}
		if url, ok := ext["account_url"].(string); ok {
			client.accountURL = url
		}
	}
	return client
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func parsePrivateKey(pemStr string) crypto.PrivateKey {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key
	}
	return nil
}

func (c *ACMEClient) SetLogger(logger cert.Logger) {
	c.logger = logger
}

func (c *ACMEClient) log(msg string) {
	if c.logger != nil {
		c.logger(msg)
	}
}

func (c *ACMEClient) getDirectory(ctx context.Context) error {
	if c.directory != nil {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.directoryURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	c.directory = &Directory{}
	return json.NewDecoder(resp.Body).Decode(c.directory)
}

func (c *ACMEClient) getNonce(ctx context.Context) (string, error) {
	// 优先使用缓存的 nonce
	if c.nonce != "" {
		nonce := c.nonce
		c.nonce = ""
		return nonce, nil
	}

	if err := c.getDirectory(ctx); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", c.directory.NewNonce, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	return resp.Header.Get("Replay-Nonce"), nil
}

func (c *ACMEClient) signedRequest(ctx context.Context, url string, payload interface{}, useKID bool) ([]byte, http.Header, error) {
	nonce, err := c.getNonce(ctx)
	if err != nil {
		return nil, nil, err
	}

	var payloadBytes []byte
	if payload != nil {
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
	}

	/* 根据密钥类型动态设置签名算法 */
	alg := "ES256"
	if _, ok := c.accountKey.(*rsa.PrivateKey); ok {
		alg = "RS256"
	}
	protected := map[string]interface{}{
		"alg":   alg,
		"nonce": nonce,
		"url":   url,
	}

	if useKID && c.accountURL != "" {
		protected["kid"] = c.accountURL
	} else {
		jwk, err := c.getJWK()
		if err != nil {
			return nil, nil, err
		}
		protected["jwk"] = jwk
	}

	protectedBytes, _ := json.Marshal(protected)
	protectedB64 := base64.RawURLEncoding.EncodeToString(protectedBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := protectedB64 + "." + payloadB64
	signature, err := c.sign([]byte(signingInput))
	if err != nil {
		return nil, nil, err
	}

	body := map[string]string{
		"protected": protectedB64,
		"payload":   payloadB64,
		"signature": base64.RawURLEncoding.EncodeToString(signature),
	}

	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/jose+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// 保存新的 nonce
	if newNonce := resp.Header.Get("Replay-Nonce"); newNonce != "" {
		c.nonce = newNonce
	}

	if resp.StatusCode >= 400 {
		c.log(fmt.Sprintf("ACME请求失败: status=%d url=%s body=%s", resp.StatusCode, url, string(respBody)))
		return nil, nil, fmt.Errorf("ACME error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, resp.Header, nil
}

func (c *ACMEClient) getJWK() (map[string]interface{}, error) {
	switch key := c.accountKey.(type) {
	case *ecdsa.PrivateKey:
		/* P-256 曲线的 x/y 坐标必须固定 32 字节，左补零对齐 */
		byteLen := (key.Curve.Params().BitSize + 7) / 8
		xBytes := key.X.Bytes()
		yBytes := key.Y.Bytes()
		xPadded := make([]byte, byteLen)
		yPadded := make([]byte, byteLen)
		copy(xPadded[byteLen-len(xBytes):], xBytes)
		copy(yPadded[byteLen-len(yBytes):], yBytes)
		return map[string]interface{}{
			"kty": "EC",
			"crv": "P-256",
			"x":   base64.RawURLEncoding.EncodeToString(xPadded),
			"y":   base64.RawURLEncoding.EncodeToString(yPadded),
		}, nil
	case *rsa.PrivateKey:
		return map[string]interface{}{
			"kty": "RSA",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported key type")
	}
}

func (c *ACMEClient) sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	switch key := c.accountKey.(type) {
	case *ecdsa.PrivateKey:
		r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
		if err != nil {
			return nil, err
		}
		/* ES256 签名要求 r 和 s 各固定 32 字节（P-256），左补零对齐 */
		curveBits := key.Curve.Params().BitSize
		keyBytes := curveBits / 8
		rBytes := r.Bytes()
		sBytes := s.Bytes()
		sig := make([]byte, keyBytes*2)
		copy(sig[keyBytes-len(rBytes):keyBytes], rBytes)
		copy(sig[keyBytes*2-len(sBytes):], sBytes)
		return sig, nil
	case *rsa.PrivateKey:
		return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	default:
		return nil, fmt.Errorf("unsupported key type")
	}
}

func (c *ACMEClient) Register(ctx context.Context) (map[string]interface{}, error) {
	if err := c.getDirectory(ctx); err != nil {
		return nil, err
	}

	if c.accountKey == nil {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		c.accountKey = key
	}

	payload := map[string]interface{}{
		"termsOfServiceAgreed": true,
		"contact":              []string{"mailto:" + c.email},
	}

	if c.eabKID != "" && c.eabHMACKey != "" {
		eab, err := c.createEAB(ctx)
		if err != nil {
			return nil, err
		}
		payload["externalAccountBinding"] = eab
	}

	body, header, err := c.signedRequest(ctx, c.directory.NewAccount, payload, false)
	if err != nil {
		return nil, err
	}

	c.accountURL = header.Get("Location")
	c.log("账户注册成功: " + c.accountURL)

	var keyPEM []byte
	switch k := c.accountKey.(type) {
	case *ecdsa.PrivateKey:
		keyBytes, _ := x509.MarshalECPrivateKey(k)
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	case *rsa.PrivateKey:
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		c.log(fmt.Sprintf("账户注册响应解析失败: %v", err))
	}

	return map[string]interface{}{
		"account_key": string(keyPEM),
		"account_url": c.accountURL,
	}, nil
}

func (c *ACMEClient) createEAB(ctx context.Context) (map[string]interface{}, error) {
	jwk, _ := c.getJWK()
	jwkBytes, _ := json.Marshal(jwk)

	protected := map[string]interface{}{
		"alg": "HS256",
		"kid": c.eabKID,
		"url": c.directory.NewAccount,
	}
	protectedBytes, _ := json.Marshal(protected)
	protectedB64 := base64.RawURLEncoding.EncodeToString(protectedBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(jwkBytes)

	hmacKey, err := base64.RawURLEncoding.DecodeString(c.eabHMACKey)
	if err != nil {
		return nil, fmt.Errorf("EAB HMAC Key 解码失败（需要 Base64URL 格式）: %w", err)
	}
	h := hmacSHA256(hmacKey, []byte(protectedB64+"."+payloadB64))

	return map[string]interface{}{
		"protected": protectedB64,
		"payload":   payloadB64,
		"signature": base64.RawURLEncoding.EncodeToString(h),
	}, nil
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func (c *ACMEClient) BuyCert(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	return nil
}

func (c *ACMEClient) CreateOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (map[string][]cert.DNSRecord, error) {
	if err := c.getDirectory(ctx); err != nil {
		return nil, err
	}

	if c.accountURL == "" {
		if _, err := c.Register(ctx); err != nil {
			return nil, err
		}
	}

	identifiers := make([]map[string]string, len(domains))
	for i, d := range domains {
		identifiers[i] = map[string]string{"type": "dns", "value": d}
	}

	payload := map[string]interface{}{
		"identifiers": identifiers,
	}

	body, header, err := c.signedRequest(ctx, c.directory.NewOrder, payload, true)
	if err != nil {
		return nil, err
	}

	/* 从 Location 头获取订单 URL（用于后续查询订单状态） */
	if loc := header.Get("Location"); loc != "" {
		order.OrderURL = loc
	}

	var orderResp map[string]interface{}
	if err := json.Unmarshal(body, &orderResp); err != nil {
		return nil, fmt.Errorf("ACME订单响应解析失败: %w", err)
	}

	order.Status, _ = orderResp["status"].(string)
	order.FinalizeURL, _ = orderResp["finalize"].(string)

	auths, ok := orderResp["authorizations"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("ACME订单响应缺少 authorizations 字段")
	}
	order.Authorizations = make([]string, len(auths))
	for i, a := range auths {
		s, _ := a.(string)
		order.Authorizations[i] = s
	}

	dnsRecords := make(map[string][]cert.DNSRecord)
	order.Challenges = make(map[string]cert.Challenge)

	for authIdx, authURL := range order.Authorizations {
		authBody, _, err := c.signedRequest(ctx, authURL, nil, true)
		if err != nil {
			return nil, err
		}

		var auth map[string]interface{}
		if err := json.Unmarshal(authBody, &auth); err != nil {
			return nil, fmt.Errorf("授权响应解析失败: %w", err)
		}

		identifierRaw, _ := auth["identifier"].(map[string]interface{})
		if identifierRaw == nil {
			return nil, fmt.Errorf("授权响应缺少 identifier 字段")
		}
		domain, _ := identifierRaw["value"].(string)
		isWildcard := false
		if wc, ok := auth["wildcard"]; ok {
			if wcBool, ok := wc.(bool); ok && wcBool {
				isWildcard = true
			}
		}
		mainDomain := getMainDomain(domain)

		// 用 authURL 作为 key 避免通配符和根域名冲突（两者 domain 相同）
		challengeKey := fmt.Sprintf("auth_%d_%s", authIdx, domain)
		if isWildcard {
			challengeKey = "wildcard_" + domain
		}

		c.log(fmt.Sprintf("授权[%d]: domain=%s wildcard=%v authURL=%s", authIdx, domain, isWildcard, authURL))

		challenges, ok := auth["challenges"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("授权响应缺少 challenges 字段")
		}
		for _, ch := range challenges {
			challenge, ok := ch.(map[string]interface{})
			if !ok {
				continue
			}
			chType, _ := challenge["type"].(string)
			if chType == "dns-01" {
				token, _ := challenge["token"].(string)
				keyAuth := c.getKeyAuthorization(token)
				hash := sha256.Sum256([]byte(keyAuth))
				txtValue := base64.RawURLEncoding.EncodeToString(hash[:])

				order.Challenges[challengeKey] = cert.Challenge{
					Type:   "dns-01",
					URL:    challenge["url"].(string),
					Token:  token,
					Status: challenge["status"].(string),
				}

				rr := "_acme-challenge"
				if domain != mainDomain {
					rr = "_acme-challenge." + strings.TrimSuffix(domain, "."+mainDomain)
				}

				if _, ok := dnsRecords[mainDomain]; !ok {
					dnsRecords[mainDomain] = []cert.DNSRecord{}
				}
				dnsRecords[mainDomain] = append(dnsRecords[mainDomain], cert.DNSRecord{
					Name:  rr,
					Type:  "TXT",
					Value: txtValue,
				})
				break
			}
		}
	}

	c.log("订单创建成功")
	return dnsRecords, nil
}

func (c *ACMEClient) getKeyAuthorization(token string) string {
	jwk, _ := c.getJWK()
	jwkBytes, _ := json.Marshal(jwk)
	thumbprint := sha256.Sum256(jwkBytes)
	return token + "." + base64.RawURLEncoding.EncodeToString(thumbprint[:])
}

func getMainDomain(domain string) string {
	if strings.HasPrefix(domain, "*.") {
		domain = domain[2:]
	}
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return domain
}

func (c *ACMEClient) AuthOrder(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	for domain, challenge := range order.Challenges {
		c.log(fmt.Sprintf("触发验证: domain=%s type=%s url=%s", domain, challenge.Type, challenge.URL))
		respBody, _, err := c.signedRequest(ctx, challenge.URL, map[string]interface{}{}, true)
		if err != nil {
			return fmt.Errorf("触发验证失败(%s): %v", domain, err)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			c.log(fmt.Sprintf("验证触发响应解析失败: %v", err))
		}
		respStatus, _ := resp["status"].(string)
		c.log(fmt.Sprintf("验证触发响应: status=%s", respStatus))
	}
	return nil
}

func (c *ACMEClient) GetAuthStatus(ctx context.Context, domains []string, order *cert.OrderInfo) (bool, error) {
	for _, authURL := range order.Authorizations {
		c.log(fmt.Sprintf("查询授权状态: %s", authURL))
		body, _, err := c.signedRequest(ctx, authURL, nil, true)
		if err != nil {
			return false, fmt.Errorf("查询授权状态失败: %v", err)
		}

		var auth map[string]interface{}
		if err := json.Unmarshal(body, &auth); err != nil {
			return false, fmt.Errorf("授权状态响应解析失败: %w", err)
		}

		statusVal, ok := auth["status"]
		if !ok {
			c.log(fmt.Sprintf("授权响应无status字段: %s", string(body)))
			return false, fmt.Errorf("授权响应格式异常")
		}
		status, _ := statusVal.(string)
		c.log(fmt.Sprintf("授权状态: %s", status))

		if status == "pending" || status == "processing" {
			// 打印 challenges 详情
			if challenges, ok := auth["challenges"].([]interface{}); ok {
				for _, ch := range challenges {
					if chMap, ok := ch.(map[string]interface{}); ok {
						chStatus, _ := chMap["status"].(string)
						chType, _ := chMap["type"].(string)
						c.log(fmt.Sprintf("  验证项: type=%s status=%s", chType, chStatus))
						if errInfo, ok := chMap["error"].(map[string]interface{}); ok {
							detail, _ := errInfo["detail"].(string)
							c.log(fmt.Sprintf("  验证错误: %s", detail))
						}
					}
				}
			}
			return false, nil
		}
		if status == "invalid" {
			// 提取详细错误信息
			errMsg := "authorization failed"
			if challenges, ok := auth["challenges"].([]interface{}); ok {
				for _, ch := range challenges {
					if chMap, ok := ch.(map[string]interface{}); ok {
						if errInfo, ok := chMap["error"].(map[string]interface{}); ok {
							detail, _ := errInfo["detail"].(string)
							errType, _ := errInfo["type"].(string)
							errMsg = fmt.Sprintf("%s: %s", errType, detail)
							c.log(fmt.Sprintf("验证失败详情: %s", errMsg))
						}
					}
				}
			}
			return false, fmt.Errorf("%s", errMsg)
		}
		c.log(fmt.Sprintf("域名授权通过: status=%s", status))
	}
	return true, nil
}

func (c *ACMEClient) FinalizeOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (*cert.CertResult, error) {
	var privateKey crypto.PrivateKey
	var err error

	if keyType == "EC" || keyType == "ECDSA" {
		var curve elliptic.Curve
		switch keySize {
		case "384":
			curve = elliptic.P384()
		default:
			curve = elliptic.P256()
		}
		privateKey, err = ecdsa.GenerateKey(curve, rand.Reader)
	} else {
		bits := 2048
		if keySize == "4096" {
			bits = 4096
		}
		privateKey, err = rsa.GenerateKey(rand.Reader, bits)
	}
	if err != nil {
		return nil, err
	}

	csr, err := c.createCSR(domains, privateKey)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"csr": base64.RawURLEncoding.EncodeToString(csr),
	}

	body, _, err := c.signedRequest(ctx, order.FinalizeURL, payload, true)
	if err != nil {
		return nil, err
	}

	var orderResp map[string]interface{}
	if err := json.Unmarshal(body, &orderResp); err != nil {
		return nil, fmt.Errorf("签发响应解析失败: %w", err)
	}

	/* 检查订单状态，invalid 直接返回错误 */
	if orderStatus, _ := orderResp["status"].(string); orderStatus == "invalid" {
		return nil, fmt.Errorf("证书签发失败: 订单状态为 invalid")
	}

	certURL, _ := orderResp["certificate"].(string)
	if certURL == "" && order.OrderURL != "" {
		time.Sleep(2 * time.Second)
		/* 重试时查询订单状态 URL 而非重新 finalize（符合 RFC 8555） */
		body, _, err = c.signedRequest(ctx, order.OrderURL, nil, true)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &orderResp); err != nil {
			return nil, fmt.Errorf("重试签发响应解析失败: %w", err)
		}
		certURL, _ = orderResp["certificate"].(string)
	}

	if certURL == "" {
		return nil, fmt.Errorf("证书签发失败: 未获取到证书下载地址")
	}

	certBody, _, err := c.signedRequest(ctx, certURL, nil, true)
	if err != nil {
		return nil, err
	}

	var keyPEM []byte
	switch k := privateKey.(type) {
	case *ecdsa.PrivateKey:
		keyBytes, _ := x509.MarshalECPrivateKey(k)
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	case *rsa.PrivateKey:
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	}

	certInfo := parseCertificate(certBody)

	c.log("证书签发成功")
	return &cert.CertResult{
		FullChain:  string(certBody),
		PrivateKey: string(keyPEM),
		Issuer:     certInfo.issuer,
		ValidFrom:  certInfo.validFrom,
		ValidTo:    certInfo.validTo,
	}, nil
}

func (c *ACMEClient) createCSR(domains []string, privateKey crypto.PrivateKey) ([]byte, error) {
	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: domains[0],
		},
		DNSNames: domains,
	}
	return x509.CreateCertificateRequest(rand.Reader, template, privateKey)
}

type certInfo struct {
	issuer    string
	validFrom int64
	validTo   int64
}

func parseCertificate(pemData []byte) certInfo {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return certInfo{}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return certInfo{}
	}
	return certInfo{
		issuer:    cert.Issuer.CommonName,
		validFrom: cert.NotBefore.Unix(),
		validTo:   cert.NotAfter.Unix(),
	}
}

func (c *ACMEClient) Revoke(ctx context.Context, order *cert.OrderInfo, pemStr string) error {
	if err := c.getDirectory(ctx); err != nil {
		return err
	}

	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return fmt.Errorf("invalid certificate PEM")
	}

	payload := map[string]interface{}{
		"certificate": base64.RawURLEncoding.EncodeToString(block.Bytes),
	}

	_, _, err := c.signedRequest(ctx, c.directory.RevokeCert, payload, true)
	return err
}

func (c *ACMEClient) Cancel(ctx context.Context, order *cert.OrderInfo) error {
	return nil
}
