package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("aliyun_oss", NewAliyunOSSProvider)
}

/*
 * AliyunOSSProvider 阿里云对象存储OSS部署器
 */
type AliyunOSSProvider struct {
	AliyunProviderBase
	httpClient *http.Client
}

func NewAliyunOSSProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunOSSProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
		httpClient:         &http.Client{Timeout: 30 * time.Second},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunOSSProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunOSSProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/*
 * deploy 逐个域名绑定证书
 */
func (p *AliyunOSSProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	endpoint := p.GetStringFrom(config, "oss_endpoint")
	if endpoint == "" {
		return errors.New("OSS Endpoint不能为空")
	}
	bucket := p.GetStringFrom(config, "oss_bucket")
	if bucket == "" {
		return errors.New("OSS Bucket不能为空")
	}
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return errors.New("OSS绑定域名不能为空")
	}

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}
	requestCertID := certID + aliyunCertIDSuffix("cn-hangzhou")

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if err := p.addBucketCnameCert(ctx, bucket, endpoint, domain, requestCertID); err != nil {
			return fmt.Errorf("OSS域名 %s 部署证书失败: %v", domain, err)
		}
		p.Log("OSS域名 " + domain + " 部署证书成功！")
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

/*
 * addBucketCnameCert 调用 OSS 的 PutBucketCname 接口写入证书配置
 */
func (p *AliyunOSSProvider) addBucketCnameCert(ctx context.Context, bucket, endpoint, domain, certID string) error {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")
	if accessKeyID == "" || accessKeySecret == "" {
		return errors.New("AccessKey不能为空")
	}

	body, err := aliyunOSSAddCnameBody(domain, certID)
	if err != nil {
		return err
	}

	resourcePath := "/" + bucket + "/"
	canonicalQuery := "cname&comp=add"
	date := time.Now().UTC().Format(http.TimeFormat)

	stringToSign := "POST\n" + "\n" + "application/xml\n" + date + "\n" + resourcePath + "?" + canonicalQuery
	mac := hmac.New(sha1.New, []byte(accessKeySecret))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	requestURL := "https://" + bucket + "." + endpoint + "/?" + canonicalQuery
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("Date", date)
	req.Header.Set("Authorization", "OSS "+accessKeyID+":"+signature)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	var ossErr struct {
		Message string `xml:"Message"`
		Code    string `xml:"Code"`
	}
	if xmlErr := xml.Unmarshal(respBody, &ossErr); xmlErr == nil && ossErr.Message != "" {
		return errors.New(ossErr.Message)
	}
	return fmt.Errorf("HTTP Code: %d", resp.StatusCode)
}

func aliyunOSSAddCnameBody(domain, certID string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	buf.WriteString("<BucketCnameConfiguration><Cname><Domain>")
	if err := xml.EscapeText(&buf, []byte(domain)); err != nil {
		return nil, err
	}
	buf.WriteString("</Domain><CertificateConfiguration><CertId>")
	if err := xml.EscapeText(&buf, []byte(certID)); err != nil {
		return nil, err
	}
	buf.WriteString("</CertId><Force>true</Force></CertificateConfiguration></Cname></BucketCnameConfiguration>")
	return buf.Bytes(), nil
}

func (p *AliyunOSSProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/* 确保 AliyunOSSProvider 满足 DeployWithInfoProvider 接口 */
var _ base.DeployWithInfoProvider = (*AliyunOSSProvider)(nil)
