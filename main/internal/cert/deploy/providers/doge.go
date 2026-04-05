package providers

import (
	"main/internal/cert/deploy/base"
	"context"
	"crypto/hmac"
	"crypto/sha1"
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
	base.Register("doge", NewDogeProvider)

	cert.Register("doge", nil, cert.ProviderConfig{
		Type:     "doge",
		Name:     "Doge云CDN",
		Icon:     "doge.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "AccessKey", Key: "access_key", Type: "input", Required: true, Placeholder: "Doge云 AccessKey"},
			{Name: "SecretKey", Key: "secret_key", Type: "input", Required: true, Placeholder: "Doge云 SecretKey"},
		},
		DeployConfig: []cert.ConfigField{
			{Name: "CDN域名", Key: "domain", Type: "input", Required: true, Placeholder: "多个域名用逗号分隔"},
		},
	})
}

type DogeProvider struct {
	base.BaseProvider
	accessKey string
	secretKey string
	client    *http.Client
}

func NewDogeProvider(config map[string]interface{}) base.DeployProvider {
	return &DogeProvider{
		BaseProvider: base.BaseProvider{Config: config},
		accessKey:    base.GetConfigString(config, "access_key"),
		secretKey:    base.GetConfigString(config, "secret_key"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *DogeProvider) Check(ctx context.Context) error {
	if p.accessKey == "" || p.secretKey == "" {
		return fmt.Errorf("必填参数不能为空")
	}

	_, err := p.request(ctx, "/cdn/cert/list.json", nil)
	return err
}

func (p *DogeProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
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

	// 获取或创建证书ID
	certID, err := p.getCertID(ctx, fullchain, privateKey, certName)
	if err != nil {
		return err
	}

	// 绑定域名
	domains := strings.Split(domain, ",")
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}

		params := map[string]string{
			"id":     fmt.Sprintf("%v", certID),
			"domain": d,
		}
		_, err := p.request(ctx, "/cdn/cert/bind.json", params)
		if err != nil {
			return fmt.Errorf("CDN域名 %s 绑定证书失败：%v", d, err)
		}
		p.Log(fmt.Sprintf("CDN域名 %s 绑定证书成功！", d))
	}

	return nil
}

func (p *DogeProvider) getCertID(ctx context.Context, fullchain, privateKey, certName string) (interface{}, error) {
	// 获取证书列表
	resp, err := p.request(ctx, "/cdn/cert/list.json", nil)
	if err != nil {
		return nil, fmt.Errorf("获取证书列表失败：%v", err)
	}

	certs, _ := resp["certs"].([]interface{})
	var certID interface{}

	for _, c := range certs {
		certData := c.(map[string]interface{})
		note, _ := certData["note"].(string)
		id := certData["id"]
		expire, _ := certData["expire"].(float64)
		domainCount, _ := certData["domainCount"].(float64)

		if certName == note {
			certID = id
			p.Log(fmt.Sprintf("证书%s已存在，证书ID:%v", certName, certID))
		} else if int64(expire) < time.Now().Unix() && domainCount == 0 {
			// 清理过期且未使用的证书
			deleteParams := map[string]string{"id": fmt.Sprintf("%v", id)}
			_, err := p.request(ctx, "/cdn/cert/delete.json", deleteParams)
			if err == nil {
				p.Log(fmt.Sprintf("证书%v已过期，删除证书成功", certData["name"]))
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	if certID == nil {
		// 上传新证书
		params := map[string]string{
			"note":    certName,
			"cert":    fullchain,
			"private": privateKey,
		}
		resp, err := p.request(ctx, "/cdn/cert/upload.json", params)
		if err != nil {
			return nil, fmt.Errorf("上传证书失败：%v", err)
		}

		certID = resp["id"]
		p.Log(fmt.Sprintf("上传证书成功，证书ID:%v", certID))
		time.Sleep(500 * time.Millisecond)
	}

	return certID, nil
}

func (p *DogeProvider) request(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	var body string
	if params != nil {
		data := url.Values{}
		for k, v := range params {
			data.Set(k, v)
		}
		body = data.Encode()
	}

	// 计算签名
	signStr := path + "\n" + body
	h := hmac.New(sha1.New, []byte(p.secretKey))
	h.Write([]byte(signStr))
	sign := hex.EncodeToString(h.Sum(nil))
	authorization := "TOKEN " + p.accessKey + ":" + sign

	reqURL := "https://api.dogecloud.com" + path

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", authorization)
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

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

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			return data, nil
		}
		return result, nil
	}

	if msg, ok := result["msg"].(string); ok {
		return nil, fmt.Errorf("%s", msg)
	}

	return nil, fmt.Errorf("请求失败")
}

func (p *DogeProvider) getCertName(fullchain string) (string, error) {
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

func (p *DogeProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
