package providers

import (
	"main/internal/cert/deploy/base"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
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
	base.Register("wangsu_cdn", NewWangsuCDNProvider)

	cert.Register("wangsu_cdn", nil, cert.ProviderConfig{
		Type:     "wangsu_cdn",
		Name:     "网宿CDN",
		Icon:     "wangsu.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "用户名", Key: "username", Type: "input", Required: true, Placeholder: "网宿账号用户名"},
			{Name: "API Key", Key: "api_key", Type: "input", Required: true, Placeholder: "网宿API密钥"},
			{Name: "SP Key", Key: "sp_key", Type: "input", Note: "CDN Pro私钥加密用（可选）"},
			{Name: "产品类型", Key: "product", Type: "select", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "CDN加速"},
				{Value: "cdnpro", Label: "CDN Pro"},
				{Value: "certificate", Label: "仅更新证书"},
			}, Value: "cdn"},
			{Name: "域名", Key: "domain", Type: "input", Note: "CDN Pro需填写域名"},
			{Name: "域名列表", Key: "domains", Type: "input", Note: "CDN加速需填写，多个用逗号分隔"},
			{Name: "证书ID", Key: "cert_id", Type: "input", Note: "仅更新证书时必填"},
		},
	})
}

type WangsuCDNProvider struct {
	base.BaseProvider
	username string
	apiKey   string
	spKey    string
	client   *http.Client
}

func NewWangsuCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &WangsuCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
		username:     base.GetConfigString(config, "username"),
		apiKey:       base.GetConfigString(config, "api_key"),
		spKey:        base.GetConfigString(config, "sp_key"),
		client:       &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *WangsuCDNProvider) Check(ctx context.Context) error {
	if p.username == "" || p.apiKey == "" {
		return fmt.Errorf("必填参数不能为空")
	}

	_, err := p.request(ctx, "GET", "/api/ssl/certificate", nil, nil, "")
	return err
}

func (p *WangsuCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	product := base.GetConfigString(config, "product")
	if product == "" {
		product = p.GetString("product")
	}

	switch product {
	case "cdnpro":
		return p.deployCDNPro(ctx, fullchain, privateKey, config)
	case "cdn":
		return p.deployCDN(ctx, fullchain, privateKey, config)
	case "certificate":
		return p.deployCertOnly(ctx, fullchain, privateKey, config)
	default:
		return fmt.Errorf("未知的产品类型")
	}
}

func (p *WangsuCDNProvider) deployCDN(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := base.GetConfigString(config, "domains")
	if domains == "" {
		domains = p.GetString("domains")
	}
	if domains == "" {
		return fmt.Errorf("绑定的域名不能为空")
	}

	domainList := strings.Split(domains, ",")
	for i := range domainList {
		domainList[i] = strings.TrimSpace(domainList[i])
	}

	// 获取证书信息
	certInfo, err := p.parseCertInfo(fullchain)
	if err != nil {
		return err
	}
	certName := certInfo.certName
	serialNo := certInfo.serialNo

	p.Log(fmt.Sprintf("证书序列号：%s", serialNo))

	// 获取或创建证书ID
	certID, err := p.getCertID(ctx, fullchain, privateKey, certName, "", serialNo, false)
	if err != nil {
		return err
	}

	// 绑定域名
	params := map[string]interface{}{
		"certificateId": certID,
		"domainNames":   domainList,
	}

	_, err = p.request(ctx, "PUT", "/api/config/certificate/batch", params, nil, "")
	if err != nil {
		return fmt.Errorf("绑定域名失败：%v", err)
	}

	p.Log(fmt.Sprintf("绑定证书成功，证书ID：%s", certID))
	return nil
}

func (p *WangsuCDNProvider) deployCDNPro(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domain := base.GetConfigString(config, "domain")
	if domain == "" {
		domain = p.GetString("domain")
	}
	if domain == "" {
		return fmt.Errorf("绑定的域名不能为空")
	}

	// 获取证书信息
	certInfo, err := p.parseCertInfo(fullchain)
	if err != nil {
		return err
	}

	// 获取或创建证书ID（CDN Pro）
	certID, err := p.getCertIDCDNPro(ctx, fullchain, privateKey, certInfo.certName)
	if err != nil {
		return err
	}

	// 获取域名信息
	hostnameInfo, err := p.request(ctx, "GET", "/cdn/hostnames/"+domain, nil, nil, "")
	if err != nil {
		return fmt.Errorf("获取域名信息失败：%v", err)
	}

	propertyInProduction, ok := hostnameInfo["propertyInProduction"].(map[string]interface{})
	if !ok || propertyInProduction == nil {
		return fmt.Errorf("域名 %s 不存在或未部署到生产环境", domain)
	}

	propertyID, _ := propertyInProduction["propertyId"].(string)
	version, _ := propertyInProduction["version"].(float64)
	existingCertID, _ := propertyInProduction["certificateId"].(string)

	p.Log(fmt.Sprintf("CDN域名 %s 对应的加速项目ID：%s", domain, propertyID))
	p.Log(fmt.Sprintf("CDN域名 %s 对应的加速项目生产版本：%d", domain, int(version)))

	if existingCertID == certID {
		p.Log(fmt.Sprintf("CDN域名 %s 已绑定证书：%s", domain, certInfo.certName))
		return nil
	}

	// 获取加速项目版本信息
	propertyPath := fmt.Sprintf("/cdn/properties/%s/versions/%d", propertyID, int(version))
	property, err := p.request(ctx, "GET", propertyPath, nil, nil, "")
	if err != nil {
		return fmt.Errorf("获取加速项目版本信息失败：%v", err)
	}

	// 更新证书配置
	propertyConfig, _ := property["configs"].(map[string]interface{})
	propertyConfig["tlsCertificateId"] = certID

	// 创建新版本
	newVersionPath := fmt.Sprintf("/cdn/properties/%s/versions", propertyID)
	location, err := p.request(ctx, "POST", newVersionPath, propertyConfig, nil, "")
	if err != nil {
		return fmt.Errorf("新增加速项目版本失败：%v", err)
	}

	// 解析新版本号
	locationStr, _ := location["location"].(string)
	parts := strings.Split(locationStr, "/")
	newVersion := parts[len(parts)-1]

	// 验证新版本
	validationParams := map[string]interface{}{
		"propertyId": propertyID,
		"version":    newVersion,
	}

	validationLocation, err := p.request(ctx, "POST", "/cdn/validations", validationParams, nil, "")
	if err != nil {
		return fmt.Errorf("发起加速项目验证失败：%v", err)
	}

	validationLocationStr, _ := validationLocation["location"].(string)
	validationParts := strings.Split(validationLocationStr, "/")
	validationTaskID := validationParts[len(validationParts)-1]

	p.Log(fmt.Sprintf("验证任务ID：%s", validationTaskID))

	// 等待验证完成
	for attempts := 0; attempts < 12; attempts++ {
		time.Sleep(5 * time.Second)

		validationStatus, err := p.request(ctx, "GET", "/cdn/validations/"+validationTaskID, nil, nil, "")
		if err != nil {
			return fmt.Errorf("获取验证任务状态失败：%v", err)
		}

		status, _ := validationStatus["status"].(string)
		if status == "failed" {
			return fmt.Errorf("证书绑定失败，加速项目验证失败")
		}
		if status == "succeeded" {
			break
		}

		if attempts == 11 {
			return fmt.Errorf("证书绑定超时，加速项目验证时间过长")
		}
	}

	p.Log("加速项目验证成功，开始部署...")

	// 部署
	deployParams := map[string]interface{}{
		"target": "production",
		"actions": []map[string]interface{}{
			{
				"action":        "deploy_cert",
				"certificateId": certID,
				"version":       1,
			},
			{
				"action":     "deploy_property",
				"propertyId": propertyID,
				"version":    newVersion,
			},
		},
		"name": fmt.Sprintf("Deploy certificate and property for %s", propertyID),
	}

	extraHeaders := map[string]string{
		"Check-Certificate": "no",
		"Check-Usage":       "no",
	}

	deployLocation, err := p.request(ctx, "POST", "/cdn/deploymentTasks", deployParams, extraHeaders, "")
	if err != nil {
		return fmt.Errorf("下发证书部署任务失败：%v", err)
	}

	deployLocationStr, _ := deployLocation["location"].(string)
	deployParts := strings.Split(deployLocationStr, "/")
	deploymentTaskID := deployParts[len(deployParts)-1]

	p.Log(fmt.Sprintf("CDN域名 %s 绑定证书部署任务下发成功，部署任务ID：%s", domain, deploymentTaskID))
	return nil
}

func (p *WangsuCDNProvider) deployCertOnly(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	certID := base.GetConfigString(config, "cert_id")
	if certID == "" {
		certID = p.GetString("cert_id")
	}
	if certID == "" {
		return fmt.Errorf("证书ID不能为空")
	}

	certInfo, err := p.parseCertInfo(fullchain)
	if err != nil {
		return err
	}

	_, err = p.getCertID(ctx, fullchain, privateKey, certInfo.certName, certID, certInfo.serialNo, true)
	return err
}

func (p *WangsuCDNProvider) getCertID(ctx context.Context, fullchain, privateKey, certName, certID, serialNo string, overwrite bool) (string, error) {
	if certID != "" {
		// 检查现有证书
		resp, err := p.request(ctx, "GET", "/api/certificate/"+certID, nil, nil, "")
		if err != nil {
			return "", fmt.Errorf("获取证书详情失败：%v", err)
		}

		if msg, _ := resp["message"].(string); msg == "success" {
			if data, ok := resp["data"].(map[string]interface{}); ok {
				existingName, _ := data["name"].(string)
				existingSerial, _ := data["serial"].(string)
				if existingName == certName && existingSerial == serialNo {
					p.Log(fmt.Sprintf("证书已是最新，证书ID：%s", certID))
					return certID, nil
				}
			}
		}

		p.Log("证书已过期或被删除，准备重新上传")
	} else if overwrite {
		return "", fmt.Errorf("证书ID不能为空")
	}

	if overwrite {
		// 更新证书
		params := map[string]interface{}{
			"name":        certName,
			"certificate": fullchain,
			"privateKey":  privateKey,
		}

		_, err := p.request(ctx, "PUT", "/api/certificate/"+certID, params, nil, "")
		if err != nil {
			return "", fmt.Errorf("更新证书失败：%v", err)
		}
		p.Log(fmt.Sprintf("更新证书成功，证书ID：%s", certID))
		return certID, nil
	}

	// 获取证书列表检查是否存在
	resp, err := p.request(ctx, "GET", "/api/ssl/certificate", nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("获取证书列表失败：%v", err)
	}

	if certs, ok := resp["ssl-certificate"].([]interface{}); ok {
		for _, c := range certs {
			certData := c.(map[string]interface{})
			existingSerial, _ := certData["certificate-serial"].(string)
			existingName, _ := certData["name"].(string)
			existingID, _ := certData["certificate-id"].(string)

			if serialNo == existingSerial {
				p.Log(fmt.Sprintf("证书%s已存在，新证书ID：%s", certName, existingID))
				// 更新名称
				updateParams := map[string]interface{}{"name": certName}
				p.request(ctx, "PUT", "/api/certificate/"+existingID, updateParams, nil, "")
				p.Log(fmt.Sprintf("将证书ID为%s的证书更名为：%s", existingID, certName))
				return existingID, nil
			} else if certName == existingName {
				p.Log(fmt.Sprintf("证书%s已存在，但序列号（%s）不匹配，准备重新上传", certName, existingID))
				// 重命名旧证书
				updateParams := map[string]interface{}{"name": certName + "-bak"}
				p.request(ctx, "PUT", "/api/certificate/"+existingID, updateParams, nil, "")
				p.Log(fmt.Sprintf("将证书ID为%s的证书更名为：%s-bak", existingID, certName))
			}
		}
	}

	// 上传新证书
	params := map[string]interface{}{
		"name":        certName,
		"certificate": fullchain,
		"privateKey":  privateKey,
	}

	resp, err = p.request(ctx, "POST", "/api/certificate", params, nil, "")
	if err != nil {
		return "", fmt.Errorf("上传证书失败：%v", err)
	}

	if location, ok := resp["location"].(string); ok {
		parts := strings.Split(location, "/")
		newCertID := parts[len(parts)-1]
		p.Log(fmt.Sprintf("上传证书成功，证书ID：%s", newCertID))
		return newCertID, nil
	}

	return "", fmt.Errorf("上传证书失败，无法获取证书ID")
}

func (p *WangsuCDNProvider) getCertIDCDNPro(ctx context.Context, fullchain, privateKey, certName string) (string, error) {
	// 搜索证书
	searchPath := "/cdn/certificates?search=" + url.QueryEscape(certName)
	resp, err := p.request(ctx, "GET", searchPath, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("获取证书列表失败：%v", err)
	}

	if count, ok := resp["count"].(float64); ok && count > 0 {
		if certs, ok := resp["certificates"].([]interface{}); ok {
			for _, c := range certs {
				certData := c.(map[string]interface{})
				if certData["name"].(string) == certName {
					certID := certData["certificateId"].(string)
					p.Log(fmt.Sprintf("证书%s已存在，证书ID：%s", certName, certID))
					return certID, nil
				}
			}
		}
	}

	// 上传新证书
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	encryptedKey := p.encryptPrivateKey(privateKey, date)

	params := map[string]interface{}{
		"name":      certName,
		"autoRenew": "Off",
		"newVersion": map[string]interface{}{
			"privateKey":  encryptedKey,
			"certificate": fullchain,
		},
	}

	resp, err = p.request(ctx, "POST", "/cdn/certificates", params, nil, date)
	if err != nil {
		return "", fmt.Errorf("上传证书失败：%v", err)
	}

	if location, ok := resp["location"].(string); ok {
		parts := strings.Split(location, "/")
		certID := parts[len(parts)-1]
		p.Log(fmt.Sprintf("上传证书成功，证书ID：%s", certID))
		time.Sleep(500 * time.Millisecond)
		return certID, nil
	}

	return "", fmt.Errorf("上传证书失败")
}

func (p *WangsuCDNProvider) encryptPrivateKey(privateKey, date string) string {
	apiKey := p.apiKey
	if p.spKey != "" {
		apiKey = p.spKey
	}

	// HMAC-SHA256 生成密钥
	h := hmac.New(sha256.New, []byte(apiKey))
	h.Write([]byte(date))
	hmacResult := h.Sum(nil)
	aesIvKeyHex := hex.EncodeToString(hmacResult)

	// 提取 IV 和 Key
	iv, _ := hex.DecodeString(aesIvKeyHex[:32])
	key, _ := hex.DecodeString(aesIvKeyHex[32:])

	// PKCS7 填充
	blockSize := 16
	padding := blockSize - len(privateKey)%blockSize
	paddedText := make([]byte, len(privateKey)+padding)
	copy(paddedText, privateKey)
	for i := len(privateKey); i < len(paddedText); i++ {
		paddedText[i] = byte(padding)
	}

	// AES-128-CBC 加密
	block, _ := aes.NewCipher(key)
	encrypted := make([]byte, len(paddedText))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(encrypted, paddedText)

	return base64.StdEncoding.EncodeToString(encrypted)
}

func (p *WangsuCDNProvider) request(ctx context.Context, method, path string, data interface{}, extraHeaders map[string]string, date string) (map[string]interface{}, error) {
	reqURL := "https://open.chinanetcenter.com" + path

	var bodyReader io.Reader
	var bodyBytes []byte
	if data != nil {
		bodyBytes, _ = json.Marshal(data)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	if date == "" {
		date = time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	}

	// 签名
	h := hmac.New(sha1.New, []byte(p.apiKey))
	h.Write([]byte(date))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	authorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(p.username+":"+signature))

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", authorization)
	req.Header.Set("Date", date)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "close")

	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
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

	// 检查 Location 头
	if resp.StatusCode == 201 || resp.StatusCode == 200 {
		if location := resp.Header.Get("Location"); location != "" {
			return map[string]interface{}{"location": location}, nil
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		var result map[string]interface{}
		if len(body) > 0 {
			json.Unmarshal(body, &result)
		}
		return result, nil
	}

	var result map[string]interface{}
	if json.Unmarshal(body, &result) == nil {
		if msg, ok := result["message"].(string); ok {
			return nil, fmt.Errorf("%s", msg)
		}
		if msg, ok := result["result"].(string); ok {
			return nil, fmt.Errorf("%s", msg)
		}
	}

	return nil, fmt.Errorf("请求失败")
}

type wangsuCertInfo struct {
	certName string
	serialNo string
}

func (p *WangsuCDNProvider) parseCertInfo(fullchain string) (*wangsuCertInfo, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return nil, fmt.Errorf("证书解析失败")
	}

	certObj, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("证书解析失败: %v", err)
	}

	cn := certObj.Subject.CommonName
	cn = strings.ReplaceAll(cn, "*.", "")
	certName := fmt.Sprintf("%s-%d", cn, certObj.NotBefore.Unix())
	serialNo := strings.ToLower(certObj.SerialNumber.Text(16))

	return &wangsuCertInfo{
		certName: certName,
		serialNo: serialNo,
	}, nil
}

func (p *WangsuCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
