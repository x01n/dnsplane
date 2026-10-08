package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("huawei_obs", NewHuaweiOBSProvider)
}

/* huaweiOBSConfiguration 桶自定义域名配置请求体，对应 OBS 的 CustomDomainConfiguration */
type huaweiOBSConfiguration struct {
	XMLName     xml.Name `xml:"CustomDomainConfiguration"`
	Name        string   `xml:"Name"`
	Certificate string   `xml:"Certificate"`
	PrivateKey  string   `xml:"PrivateKey"`
}

/* HuaweiOBSProvider 华为云对象存储OBS自定义域名证书部署器 */
type HuaweiOBSProvider struct {
	base.BaseProvider
	client *http.Client
}

/* NewHuaweiOBSProvider 创建华为云OBS部署器 */
func NewHuaweiOBSProvider(config map[string]interface{}) base.DeployProvider {
	return &HuaweiOBSProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

/* Check 检查必填配置 */
func (p *HuaweiOBSProvider) Check(ctx context.Context) error {
	if _, _, err := huaweiAccessKeysQuiet(&p.BaseProvider); err != nil {
		return err
	}
	if strings.TrimSpace(p.GetString("obs_endpoint")) == "" {
		return fmt.Errorf("OBS Endpoint不能为空")
	}
	if strings.TrimSpace(p.GetString("obs_bucket")) == "" {
		return fmt.Errorf("OBS 桶名称不能为空")
	}
	return nil
}

/*
 * Deploy 为桶的每个自定义域名绑定证书
 */
func (p *HuaweiOBSProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	endpoint := base.GetConfigString(config, "obs_endpoint")
	if endpoint == "" {
		endpoint = p.GetString("obs_endpoint")
	}
	bucket := base.GetConfigString(config, "obs_bucket")
	if bucket == "" {
		bucket = p.GetString("obs_bucket")
	}
	if endpoint == "" {
		return fmt.Errorf("OBS Endpoint不能为空")
	}
	if bucket == "" {
		return fmt.Errorf("OBS 桶名称不能为空")
	}

	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("绑定的域名不能为空")
	}

	certName, err := buildCertName(fullchain)
	if err != nil {
		return err
	}

	accessKeyID, accessKeySecret := huaweiAccessKeys(&p.BaseProvider)
	if accessKeyID == "" || accessKeySecret == "" {
		return fmt.Errorf("AccessKeyId/SecretAccessKey不能为空")
	}

	host := bucket + "." + strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://"), "/")

	for _, domain := range domains {
		if err := p.setBucketCustomDomain(ctx, host, bucket, domain, certName, fullchain, privateKey, accessKeyID, accessKeySecret); err != nil {
			return fmt.Errorf("OBS域名 %s 部署证书失败: %v", domain, err)
		}
		p.Log(fmt.Sprintf("OSS域名 %s 部署证书成功", domain))
	}

	return nil
}

/*
 * setBucketCustomDomain 调用 OBS SetBucketCustomDomain 接口
 * 使用 OBS 自有的签名算法：Authorization = "OBS " + AK + ":" + base64(hmac-sha1(SK, stringToSign))
 */
func (p *HuaweiOBSProvider) setBucketCustomDomain(ctx context.Context, host, bucket, domain, certName, fullchain, privateKey, accessKeyID, accessKeySecret string) error {
	body, err := xml.Marshal(huaweiOBSConfiguration{
		Name:        certName,
		Certificate: fullchain,
		PrivateKey:  privateKey,
	})
	if err != nil {
		return fmt.Errorf("构造请求体失败: %v", err)
	}

	contentMD5 := base64.StdEncoding.EncodeToString(md5Sum(body))
	contentType := "application/xml"
	date := time.Now().UTC().Format(http.TimeFormat)
	// CanonicalizedResource 形如 /{bucket}/，桶自定义域名作为子资源参与签名
	resourcePath := "/" + bucket + "/"
	query := "customdomain=" + domain

	// OBS 签名串：VERB + Content-MD5 + Content-Type + Date + 规范化头 + 规范化资源
	stringToSign := "PUT\n" + contentMD5 + "\n" + contentType + "\n" + date + "\n" + resourcePath + "?" + query
	mac := hmac.New(sha1.New, []byte(accessKeySecret))
	mac.Write([]byte(stringToSign))
	authorization := "OBS " + accessKeyID + ":" + base64.StdEncoding.EncodeToString(mac.Sum(nil))

	reqURL := "https://" + host + "/?" + query
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-MD5", contentMD5)
	req.Header.Set("Date", date)
	req.Header.Set("Authorization", authorization)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	if message := parseOBSErrorMessage(respBody); message != "" {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("HTTP Code: %d", resp.StatusCode)
}

/* parseOBSErrorMessage 提取 OBS 错误响应中的错误描述 */
func parseOBSErrorMessage(body []byte) string {
	var result struct {
		Message string `xml:"Message"`
		Code    string `xml:"Code"`
	}
	if err := xml.Unmarshal(body, &result); err != nil {
		return ""
	}
	if result.Message != "" {
		return result.Message
	}
	return result.Code
}

/* md5Sum 计算 MD5 摘要 */
func md5Sum(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}

/* SetLogger 设置日志记录器 */
func (p *HuaweiOBSProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
