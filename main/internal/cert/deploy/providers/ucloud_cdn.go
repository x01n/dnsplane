package providers

import (
	"context"
	"crypto/sha1"
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
	base.Register("ucloud_cdn", NewUCloudCDNProvider)

	cert.Register("ucloud_cdn", nil, cert.ProviderConfig{
		Type:     "ucloud_cdn",
		Name:     "UCloud CDN",
		Icon:     "ucloud.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "PublicKey", Key: "public_key", Type: "input", Required: true, Placeholder: "UCloud PublicKey"},
			{Name: "PrivateKey", Key: "private_key", Type: "input", Required: true, Placeholder: "UCloud PrivateKey"},
			{Name: "云分发资源ID", Key: "domain_id", Type: "input", Required: true, Placeholder: "UCDN资源ID"},
		},
	})
}

type UCloudCDNProvider struct {
	base.BaseProvider
	publicKey  string
	privateKey string
	client     *http.Client
}

func NewUCloudCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &UCloudCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
		publicKey:    base.GetConfigString(config, "public_key"),
		privateKey:   base.GetConfigString(config, "private_key"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *UCloudCDNProvider) Check(ctx context.Context) error {
	if p.publicKey == "" || p.privateKey == "" {
		return fmt.Errorf("PublicKey和PrivateKey不能为空")
	}

	params := map[string]string{
		"Mode": "free",
	}
	_, err := p.request(ctx, "GetCertificateList", params)
	return err
}

func (p *UCloudCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domainID := base.GetConfigString(config, "domain_id")
	if domainID == "" {
		domainID = p.GetString("domain_id")
	}
	if domainID == "" {
		return fmt.Errorf("云分发资源ID不能为空")
	}

	// 解析证书
	certName, err := p.getCertName(fullchain)
	if err != nil {
		return err
	}

	// 添加证书
	params := map[string]string{
		"CertName":   certName,
		"UserCert":   fullchain,
		"PrivateKey": privateKey,
	}
	_, err = p.request(ctx, "AddCertificate", params)
	if err != nil {
		if !strings.Contains(err.Error(), "cert already exist") {
			return fmt.Errorf("添加证书失败: %v", err)
		}
		p.Log(fmt.Sprintf("证书已存在，名称: %s", certName))
	} else {
		p.Log(fmt.Sprintf("添加证书成功，名称: %s", certName))
	}

	// 获取加速域名配置
	params = map[string]string{
		"DomainId.0": domainID,
	}
	resp, err := p.request(ctx, "GetUcdnDomainConfig", params)
	if err != nil {
		return fmt.Errorf("获取加速域名配置失败: %v", err)
	}

	domainList, ok := resp["DomainList"].([]interface{})
	if !ok || len(domainList) == 0 {
		return fmt.Errorf("云分发资源ID: %s 不存在", domainID)
	}

	domainInfo, ok := domainList[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("域名信息格式异常")
	}
	domain, _ := domainInfo["Domain"].(string)
	httpsStatusCn, _ := domainInfo["HttpsStatusCn"].(string)
	httpsStatusAbroad, _ := domainInfo["HttpsStatusAbroad"].(string)
	certNameCn, _ := domainInfo["CertNameCn"].(string)
	certNameAbroad, _ := domainInfo["CertNameAbroad"].(string)

	// 检查证书是否已配置
	if certNameCn == certName || certNameAbroad == certName {
		p.Log(fmt.Sprintf("云分发 %s 证书已配置，无需重复操作", domainID))
		return nil
	}

	// 获取可用证书列表
	params = map[string]string{
		"Domain": domain,
	}
	resp, err = p.request(ctx, "GetCertificateBaseInfoList", params)
	if err != nil {
		return fmt.Errorf("获取可用证书列表失败: %v", err)
	}

	certList, ok := resp["CertList"].([]interface{})
	if !ok || len(certList) == 0 {
		return fmt.Errorf("可用证书列表为空")
	}

	var certID string
	for _, c := range certList {
		certInfo, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		cName, _ := certInfo["CertName"].(string)
		if cName == certName {
			certID, _ = certInfo["CertId"].(string)
			break
		}
	}
	if certID == "" {
		return fmt.Errorf("证书ID不存在")
	}
	p.Log(fmt.Sprintf("证书ID获取成功: %s", certID))

	// 配置HTTPS
	params = map[string]string{
		"DomainId": domainID,
		"CertName": certName,
		"CertId":   certID,
		"CertType": "ucdn",
	}
	if httpsStatusCn == "enable" {
		params["HttpsStatusCn"] = httpsStatusCn
	}
	if httpsStatusAbroad == "enable" {
		params["HttpsStatusAbroad"] = httpsStatusAbroad
	}
	if httpsStatusCn != "enable" && httpsStatusAbroad != "enable" {
		params["HttpsStatusCn"] = "enable"
	}

	_, err = p.request(ctx, "UpdateUcdnDomainHttpsConfigV2", params)
	if err != nil {
		return fmt.Errorf("HTTPS加速配置失败: %v", err)
	}

	p.Log(fmt.Sprintf("云分发 %s 证书配置成功！", domainID))
	return nil
}

func (p *UCloudCDNProvider) request(ctx context.Context, action string, params map[string]string) (map[string]interface{}, error) {
	// 构建请求参数
	params["Action"] = action
	params["PublicKey"] = p.publicKey

	// 签名
	signature := p.sign(params)
	params["Signature"] = signature

	// 构建请求URL
	query := url.Values{}
	for k, v := range params {
		query.Set(k, v)
	}

	reqURL := "https://api.ucloud.cn/?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
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

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	retCode, _ := result["RetCode"].(float64)
	if retCode != 0 {
		if msg, ok := result["Message"].(string); ok {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("请求失败，错误码: %v", retCode)
	}

	return result, nil
}

func (p *UCloudCDNProvider) sign(params map[string]string) string {
	// 按key排序
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 拼接参数
	var str strings.Builder
	for _, k := range keys {
		str.WriteString(k)
		str.WriteString(params[k])
	}
	str.WriteString(p.privateKey)

	// SHA1签名
	h := sha1.New()
	h.Write([]byte(str.String()))
	return hex.EncodeToString(h.Sum(nil))
}

func (p *UCloudCDNProvider) getCertName(fullchain string) (string, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return "", fmt.Errorf("无法解析证书")
	}

	certObj, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("证书解析失败: %v", err)
	}

	cn := certObj.Subject.CommonName
	cn = strings.ReplaceAll(cn, "*", "")
	cn = strings.ReplaceAll(cn, ".", "")
	certName := fmt.Sprintf("%s-%d", cn, certObj.NotBefore.Unix())
	return certName, nil
}

func (p *UCloudCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
