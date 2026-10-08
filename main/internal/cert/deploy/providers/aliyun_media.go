package providers

import (
	"bytes"
	"context"
	"encoding/json"
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
	base.Register("aliyun_live", NewAliyunLiveProvider)
	base.Register("aliyun_vod", NewAliyunVODProvider)
	base.Register("aliyun_fc", NewAliyunFCProvider)
	base.Register("aliyun_fc2", NewAliyunFCProvider)
}

/*
 * AliyunLiveProvider 视频直播部署器
 */
type AliyunLiveProvider struct {
	AliyunProviderBase
}

func NewAliyunLiveProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunLiveProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunLiveProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunLiveProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/* deploy 视频直播证书下发 */
func (p *AliyunLiveProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return errors.New("视频直播绑定域名不能为空")
	}

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}

	client, err := p.client("cn-hangzhou")
	if err != nil {
		return err
	}

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		req := newAliyunRequest("POST", "live.aliyuncs.com", "2016-11-01", "SetLiveDomainCertificate")
		req.QueryParams["DomainName"] = domain
		req.QueryParams["CertName"] = certName
		req.QueryParams["CertType"] = "cas"
		req.QueryParams["SSLProtocol"] = "on"
		req.QueryParams["CertId"] = certID
		if _, err := callAliyun(client, req); err != nil {
			return fmt.Errorf("设置视频直播域名 %s 证书失败: %v", domain, err)
		}
		p.Log("设置视频直播域名 " + domain + " 证书成功！")
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

func (p *AliyunLiveProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/*
 * AliyunVODProvider 视频点播部署器
 * 直接提交证书内容，接入点固定为中国上海
 */
type AliyunVODProvider struct {
	AliyunProviderBase
}

func NewAliyunVODProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunVODProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunVODProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config)
}

/* deploy 视频点播证书下发 */
func (p *AliyunVODProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return errors.New("视频点播绑定域名不能为空")
	}

	client, err := p.client("cn-shanghai")
	if err != nil {
		return err
	}

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		req := newAliyunRequest("POST", "vod.cn-shanghai.aliyuncs.com", "2017-03-21", "SetVodDomainCertificate")
		req.QueryParams["DomainName"] = domain
		req.QueryParams["SSLProtocol"] = "on"
		req.QueryParams["SSLPub"] = fullchain
		req.QueryParams["SSLPri"] = privateKey
		if _, err := callAliyun(client, req); err != nil {
			return fmt.Errorf("视频点播域名 %s 部署证书失败: %v", domain, err)
		}
		p.Log("视频点播域名 " + domain + " 部署证书成功！")
	}

	return nil
}

func (p *AliyunVODProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/*
 * AliyunFCProvider 函数计算部署器，兼容3.0与2.0两个接口版本
 * 走 V3 签名直传自定义域名配置
 */
type AliyunFCProvider struct {
	AliyunProviderBase
	httpClient *http.Client
}

func NewAliyunFCProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunFCProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
		httpClient:         &http.Client{Timeout: 30 * time.Second},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunFCProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config)
}

/* fcVersion 依据 product 选择函数计算接口版本，缺省为3.0 */
func (p *AliyunFCProvider) fcVersion(config map[string]interface{}) (string, error) {
	switch p.GetStringFrom(config, "product") {
	case "", "fc":
		return "2023-03-30", nil
	case "fc2":
		return "2021-04-06", nil
	}
	return "", fmt.Errorf("未知的产品类型: %s", p.GetStringFrom(config, "product"))
}

/*
 * deploy 函数计算证书下发
 * 已有证书内容相同时跳过，HTTP 协议自动升级为 HTTP,HTTPS
 */
func (p *AliyunFCProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return errors.New("函数计算域名不能为空")
	}
	fcCname := p.GetStringFrom(config, "fc_cname")
	if fcCname == "" {
		return errors.New("域名CNAME地址不能为空")
	}

	version, err := p.fcVersion(config)
	if err != nil {
		return err
	}

	certInfo, err := parseAliyunCert(fullchain)
	if err != nil {
		return err
	}

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		done, err := p.deployDomain(ctx, fcCname, version, domain, fullchain, privateKey, certInfo.certName)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		p.Log("函数计算域名 " + domain + " 部署证书成功！")
	}

	return nil
}

/*
 * deployDomain 查询并回写单个自定义域名的证书配置
 * 当证书内容已一致时返回 true，调用方据此结束整个部署流程
 */
func (p *AliyunFCProvider) deployDomain(ctx context.Context, fcCname, version, domain, fullchain, privateKey, certName string) (bool, error) {
	path := "/" + version + "/custom-domains/" + domain
	domainData, err := p.fcRequest(ctx, fcCname, version, "GET", "GetCustomDomain", path, nil)
	if err != nil {
		return false, errors.New("获取绑定域名信息失败：" + err.Error())
	}
	p.Log("获取函数计算绑定域名信息成功")

	certConfig, _ := domainData["certConfig"].(map[string]interface{})
	if certConfig == nil {
		certConfig = map[string]interface{}{}
	}
	if stringFromAny(certConfig["certificate"]) == fullchain {
		p.Log("函数计算域名 " + domain + " 证书已配置，无需重复操作")
		return true, nil
	}

	protocol := stringFromAny(domainData["protocol"])
	if protocol == "HTTP" {
		protocol = "HTTP,HTTPS"
	}
	certConfig["certName"] = certName
	certConfig["certificate"] = fullchain
	certConfig["privateKey"] = privateKey
	body := map[string]interface{}{"certConfig": certConfig, "protocol": protocol}
	for _, key := range []string{"authConfig", "routeConfig", "tlsConfig", "wafConfig"} {
		if value, ok := domainData[key]; ok && value != nil {
			body[key] = value
		}
	}

	if _, err := p.fcRequest(ctx, fcCname, version, "PUT", "UpdateCustomDomain", path, body); err != nil {
		return false, fmt.Errorf("函数计算域名 %s 部署证书失败: %v", domain, err)
	}
	return false, nil
}

/*
 * fcRequest 以 V3 签名调用函数计算接口
 */
func (p *AliyunFCProvider) fcRequest(ctx context.Context, endpoint, version, method, action, path string, body map[string]interface{}) (map[string]interface{}, error) {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, errors.New("AccessKey不能为空")
	}

	bodyBytes := []byte{}
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyBytes = encoded
	}

	requestURL := "https://" + endpoint + path
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"x-acs-action":          action,
		"x-acs-version":         version,
		"x-acs-signature-nonce": aliyunNonce(),
		"x-acs-date":            time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"x-acs-content-sha256":  aliyunSHA256Hex(bodyBytes),
		"host":                  endpoint,
	}
	if len(bodyBytes) > 0 {
		headers["content-type"] = "application/json; charset=utf-8"
	}
	headers["authorization"] = aliyunACS3Authorization(method, path, headers, bodyBytes, accessKeyID, accessKeySecret)

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	result := map[string]interface{}{}
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &result)
	}
	if resp.StatusCode == http.StatusOK {
		return result, nil
	}

	message := stringFromAny(result["Message"])
	if message == "" {
		message = stringFromAny(result["message"])
	}
	if message != "" {
		if idx := strings.Index(message, "."); idx > 0 {
			message = message[:idx+1]
		}
		return nil, errors.New(message)
	}
	return nil, fmt.Errorf("HTTP Code: %d", resp.StatusCode)
}

func (p *AliyunFCProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

var _ base.DeployWithInfoProvider = (*AliyunLiveProvider)(nil)
