package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("nginxproxymanager", NewNPMProvider)
}

// NPMProvider Nginx Proxy Manager 证书部署
type NPMProvider struct {
	base.BaseProvider
	client *http.Client
	url    string
	email  string
	password string
	proxy  bool
	token  string
}

// NewNPMProvider creates a new NPM provider
func NewNPMProvider(config map[string]interface{}) base.DeployProvider {
	proxy := base.GetConfigBool(config, "proxy")
	return &NPMProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
		url:          strings.TrimSuffix(base.GetConfigString(config, "url"), "/"),
		email:        base.GetConfigString(config, "email"),
		password:     base.GetConfigString(config, "password"),
		proxy:        proxy,
	}
}

// Check verifies connection to NPM
func (p *NPMProvider) Check(ctx context.Context) error {
	if p.url == "" || p.email == "" || p.password == "" {
		return fmt.Errorf("请填写面板地址、登录邮箱和登录密码")
	}
	if err := p.login(ctx); err != nil {
		return err
	}
	_, err := p.request(ctx, "GET", "/nginx/certificates", nil, false)
	return err
}

// Deploy deploys certificate to NPM
func (p *NPMProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("没有设置要部署的域名")
	}

	if err := p.login(ctx); err != nil {
		return err
	}

	certificateID := p.GetIntFromConfig(config, "id", 0)
	if certificateID > 0 {
		p.Log(fmt.Sprintf("使用配置中的证书ID:%d 直接更新 NPM 自定义证书", certificateID))
		certificate, err := p.getCertificate(ctx, certificateID)
		if err != nil {
			return err
		}
		if err := p.assertCustomCertificate(certificate, certificateID); err != nil {
			return err
		}
		if err := p.uploadCertificate(ctx, certificateID, fullchain, privateKey); err != nil {
			return err
		}
		p.Log(fmt.Sprintf("证书ID:%d 更新成功！", certificateID))
		return nil
	}

	hostID := p.GetIntFromConfig(config, "host_id", 0)
	hosts, err := p.resolveTargetHosts(ctx, domains, hostID)
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return fmt.Errorf("未找到匹配的 Proxy Host，请填写证书ID或 Proxy Host ID")
	}

	p.Log(fmt.Sprintf("匹配到 Proxy Host %d 个", len(hosts)))

	resolvedCertificateID := 0
	var conflictMessage string
	for _, host := range hosts {
		hostCertificateID := int(host["certificate_id"].(float64))
		if hostCertificateID <= 0 {
			continue
		}

		certificate, err := p.getCertificate(ctx, hostCertificateID)
		if err != nil {
			p.Log(fmt.Sprintf("Proxy Host ID:%d 当前证书不可直接更新：%v", host["id"], err))
			continue
		}

		if err := p.assertCustomCertificate(certificate, hostCertificateID); err != nil {
			p.Log(fmt.Sprintf("Proxy Host ID:%d 当前证书不可直接更新：%v", host["id"], err))
			continue
		}

		if resolvedCertificateID == 0 {
			resolvedCertificateID = hostCertificateID
		} else if resolvedCertificateID != hostCertificateID {
			conflictMessage = "匹配到多个 Proxy Host，但它们绑定了不同的自定义证书ID，无法自动决定更新哪个证书，请手动填写证书ID"
		}
	}

	if conflictMessage != "" {
		return fmt.Errorf("%s", conflictMessage)
	}

	if resolvedCertificateID == 0 {
		resolvedCertificateID, err = p.createCustomCertificate(ctx, domains)
		if err != nil {
			return err
		}
		p.Log(fmt.Sprintf("创建自定义证书成功，证书ID:%d", resolvedCertificateID))
	}

	if err := p.uploadCertificate(ctx, resolvedCertificateID, fullchain, privateKey); err != nil {
		return err
	}
	p.Log(fmt.Sprintf("证书ID:%d 更新成功！", resolvedCertificateID))

	for _, host := range hosts {
		currentCertificateID := int(host["certificate_id"].(float64))
		if currentCertificateID != resolvedCertificateID {
			if err := p.updateProxyHostCertificate(ctx, host, resolvedCertificateID); err != nil {
				return err
			}
			p.Log(fmt.Sprintf("Proxy Host ID:%v 已绑定到证书ID:%d", host["id"], resolvedCertificateID))
		} else {
			p.Log(fmt.Sprintf("Proxy Host ID:%v 已绑定目标证书，无需重复更新绑定", host["id"]))
		}
	}

	return nil
}

// SetLogger sets the logger
func (p *NPMProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

func (p *NPMProvider) login(ctx context.Context) error {
	data := map[string]string{
		"identity": p.email,
		"secret":   p.password,
	}
	resp, err := p.request(ctx, "POST", "/tokens", data)
	if err != nil {
		return err
	}

	result, ok := resp.(map[string]interface{})
	if !ok {
		return fmt.Errorf("登录 NPM 失败，未返回访问令牌")
	}

	token, ok := result["token"].(string)
	if !ok || token == "" {
		if _, ok := result["requires_2fa"]; ok {
			return fmt.Errorf("当前 NPM 账户启用了双因素认证，暂不支持")
		}
		return fmt.Errorf("登录 NPM 失败，未返回访问令牌")
	}

	p.token = token
	return nil
}

func (p *NPMProvider) resolveTargetHosts(ctx context.Context, domains []string, hostID int) ([]map[string]interface{}, error) {
	if hostID > 0 {
		host, err := p.getProxyHost(ctx, hostID)
		if err != nil {
			return nil, err
		}
		return []map[string]interface{}{host}, nil
	}

	hosts, err := p.request(ctx, "GET", "/nginx/proxy-hosts", nil, false)
	if err != nil {
		return nil, err
	}

	hostsList, ok := hosts.([]interface{})
	if !ok {
		return nil, fmt.Errorf("获取 Proxy Host 列表失败")
	}

	var matched []map[string]interface{}
	for _, h := range hostsList {
		host, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		hostDomains, ok := host["domain_names"].([]interface{})
		if !ok {
			continue
		}

		var hostDomainStrs []string
		for _, hd := range hostDomains {
			if s, ok := hd.(string); ok {
				hostDomainStrs = append(hostDomainStrs, s)
			}
		}

		if p.hasIntersectDomain(domains, hostDomainStrs) {
			hostID := int(host["id"].(float64))
			fullHost, err := p.getProxyHost(ctx, hostID)
			if err != nil {
				return nil, err
			}
			matched = append(matched, fullHost)
		}
	}

	return matched, nil
}

func (p *NPMProvider) hasIntersectDomain(domains, hostDomains []string) bool {
	for _, hostDomain := range hostDomains {
		hostDomain = strings.TrimSpace(hostDomain)
		if hostDomain == "" {
			continue
		}
		for _, domain := range domains {
			if p.domainMatches(domain, hostDomain) || p.domainMatches(hostDomain, domain) {
				return true
			}
		}
	}
	return false
}

func (p *NPMProvider) domainMatches(pattern, domain string) bool {
	pattern = strings.TrimSpace(strings.ToLower(pattern))
	domain = strings.TrimSpace(strings.ToLower(domain))
	if pattern == "" || domain == "" {
		return false
	}
	if pattern == domain {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:]
		return strings.HasSuffix(domain, suffix)
	}
	return false
}

func (p *NPMProvider) createCustomCertificate(ctx context.Context, domains []string) (int, error) {
	certName := ""
	if len(domains) > 0 {
		certName = strings.TrimSpace(domains[0])
	}

	data := map[string]interface{}{
		"provider":  "other",
		"nice_name": certName,
	}

	result, err := p.request(ctx, "POST", "/nginx/certificates", data)
	if err != nil {
		return 0, err
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("创建 NPM 自定义证书失败")
	}

	if ownerID, ok := resultMap["owner_user_id"]; ok {
		p.Log(fmt.Sprintf("NPM 新建证书归属用户ID:%v（由当前登录账号决定）", ownerID))
	}

	certificateID, ok := resultMap["id"].(float64)
	if !ok || int(certificateID) <= 0 {
		return 0, fmt.Errorf("创建 NPM 自定义证书失败")
	}
	return int(certificateID), nil
}

func (p *NPMProvider) uploadCertificate(ctx context.Context, certificateID int, fullchain, privateKey string) error {
	certificate, intermediateCertificate := p.splitFullchain(fullchain)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add certificate
	part, _ := writer.CreateFormFile("certificate", "certificate.pem")
	part.Write([]byte(certificate))

	// Add certificate key
	part, _ = writer.CreateFormFile("certificate_key", "certificate.key")
	part.Write([]byte(privateKey))

	// Add intermediate certificate if exists
	if intermediateCertificate != "" {
		part, _ = writer.CreateFormFile("intermediate_certificate", "intermediate.pem")
		part.Write([]byte(intermediateCertificate))
	}

	writer.Close()

	url := fmt.Sprintf("%s/api/nginx/certificates/%d/upload", p.url, certificateID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.Log("Response:" + string(body))
		var result map[string]interface{}
		if json.Unmarshal(body, &result) == nil {
			if errObj, ok := result["error"].(map[string]interface{}); ok {
				if msg, ok := errObj["message"].(string); ok {
					return fmt.Errorf("%s", msg)
				}
			}
			if msg, ok := result["message"].(string); ok {
				return fmt.Errorf("%s", msg)
			}
		}
		return fmt.Errorf("请求失败(httpCode=%d): %s", resp.StatusCode, p.truncateBody(string(body)))
	}

	return nil
}

func (p *NPMProvider) splitFullchain(fullchain string) (string, string) {
	var certificates []string
	var current strings.Builder
	inCert := false

	for _, line := range strings.Split(fullchain, "\n") {
		if strings.Contains(line, "-----BEGIN CERTIFICATE-----") {
			inCert = true
			current.Reset()
		}
		if inCert {
			current.WriteString(line + "\n")
		}
		if strings.Contains(line, "-----END CERTIFICATE-----") {
			inCert = false
			certificates = append(certificates, current.String())
		}
	}

	if len(certificates) == 0 {
		return "", ""
	}

	certificate := certificates[0]
	intermediateCertificate := ""
	if len(certificates) > 1 {
		intermediateCertificate = strings.Join(certificates[1:], "\n")
	}

	return certificate, intermediateCertificate
}

func (p *NPMProvider) updateProxyHostCertificate(ctx context.Context, host map[string]interface{}, certificateID int) error {
	hostID := int(host["id"].(float64))
	data := map[string]interface{}{
		"certificate_id": certificateID,
	}

	_, err := p.request(ctx, "PUT", fmt.Sprintf("/nginx/proxy-hosts/%d", hostID), data)
	return err
}

func (p *NPMProvider) assertCustomCertificate(certificate map[string]interface{}, certificateID int) error {
	provider, _ := certificate["provider"].(string)
	if provider != "other" {
		return fmt.Errorf("证书ID:%d 不是自定义证书(provider=other)，无法通过上传接口更新", certificateID)
	}
	return nil
}

func (p *NPMProvider) getCertificate(ctx context.Context, certificateID int) (map[string]interface{}, error) {
	result, err := p.request(ctx, "GET", fmt.Sprintf("/nginx/certificates/%d", certificateID), nil, false)
	if err != nil {
		return nil, err
	}

	cert, ok := result.(map[string]interface{})
	if !ok || cert["id"] == nil {
		return nil, fmt.Errorf("证书ID:%d 不存在", certificateID)
	}
	return cert, nil
}

func (p *NPMProvider) getProxyHost(ctx context.Context, hostID int) (map[string]interface{}, error) {
	result, err := p.request(ctx, "GET", fmt.Sprintf("/nginx/proxy-hosts/%d", hostID), nil, false)
	if err != nil {
		return nil, err
	}

	host, ok := result.(map[string]interface{})
	if !ok || host["id"] == nil {
		return nil, fmt.Errorf("Proxy Host ID:%d 不存在", hostID)
	}

	ownerID := 0
	if oid, ok := host["owner_user_id"].(float64); ok {
		ownerID = int(oid)
	}
	certID := 0
	if cid, ok := host["certificate_id"].(float64); ok {
		certID = int(cid)
	}
	p.Log(fmt.Sprintf("读取 Proxy Host ID:%d owner_user_id:%d certificate_id:%d", int(host["id"].(float64)), ownerID, certID))

	return host, nil
}

func (p *NPMProvider) request(ctx context.Context, method, path string, params interface{}, logBodyOnError ...bool) (interface{}, error) {
	shouldLogBody := len(logBodyOnError) == 0 || logBodyOnError[0]

	url := fmt.Sprintf("%s/api%s", p.url, path)
	var bodyReader io.Reader
	var contentType string

	if params != nil && method != "GET" {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %v", err)
		}
		bodyReader = bytes.NewReader(data)
		contentType = "application/json"
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result interface{}
	json.Unmarshal(body, &result)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return result, nil
	}

	if shouldLogBody && len(body) > 0 {
		p.Log("Response:" + string(body))
	}

	if resultMap, ok := result.(map[string]interface{}); ok {
		if errObj, ok := resultMap["error"].(map[string]interface{}); ok {
			if msg, ok := errObj["message"].(string); ok && msg != "" {
				return nil, fmt.Errorf("%s", msg)
			}
		}
		if msg, ok := resultMap["message"].(string); ok && msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
			return nil, fmt.Errorf("%s", errMsg)
		}
	}

	bodyStr := string(body)
	if bodyStr != "" {
		return nil, fmt.Errorf("请求失败(httpCode=%d): %s", resp.StatusCode, p.truncateBody(bodyStr))
	}

	return nil, fmt.Errorf("请求失败(httpCode=%d)", resp.StatusCode)
}

func (p *NPMProvider) truncateBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if len(body) > 300 {
		return body[:300] + "..."
	}
	return body
}

func (p *NPMProvider) GetIntFromConfig(config map[string]interface{}, key string, defaultVal int) int {
	if config != nil {
		if v, ok := config[key]; ok {
			switch val := v.(type) {
			case float64:
				return int(val)
			case int:
				return val
			case string:
				var i int
				if _, err := fmt.Sscanf(val, "%d", &i); err == nil {
					return i
				}
			}
		}
	}
	return defaultVal
}
