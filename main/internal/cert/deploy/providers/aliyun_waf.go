package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("aliyun_waf", NewAliyunWAFProvider)
	base.Register("aliyun_wafres", NewAliyunWAFResProvider)
	base.Register("aliyun_waf2", NewAliyunWAF2Provider)
	base.Register("aliyun_api", NewAliyunAPIProvider)
}

/*
 * aliyunWAFClient 构造 WAF 客户端
 */
func (p *AliyunProviderBase) aliyunWAFClient(region string) (aliyunClient, error) {
	return p.client(region)
}

func aliyunWAFEndpoint(region string) string {
	return "wafopenapi." + region + ".aliyuncs.com"
}

func (p *AliyunProviderBase) aliyunWAFInstanceID(client aliyunClient, region string) (string, error) {
	req := newAliyunRequest("GET", aliyunWAFEndpoint(region), "2021-10-01", "DescribeInstance")
	req.QueryParams["RegionId"] = region

	result, err := callAliyun(client, req)
	if err != nil {
		return "", errors.New("获取WAF实例详情失败：" + err.Error())
	}
	instanceID := stringFromAny(result["InstanceId"])
	if instanceID == "" {
		return "", errors.New("当前账号未找到WAF实例")
	}
	p.Log("获取WAF实例ID成功 InstanceId=" + instanceID)
	return instanceID, nil
}

/*
 * AliyunWAFProvider Web应用防火墙3.0部署器
 */
type AliyunWAFProvider struct {
	AliyunProviderBase
}

func NewAliyunWAFProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunWAFProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunWAFProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunWAFProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/*
 * deploy WAF 3.0 证书下发
 */
func (p *AliyunWAFProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return errors.New("WAF绑定域名不能为空")
	}
	region := p.regionOf(config)

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}
	/* 请求使用带地域后缀的证书ID，回传保留原始值 */
	requestCertID := certID + aliyunCertIDSuffix(region)

	client, err := p.aliyunWAFClient(region)
	if err != nil {
		return err
	}
	instanceID, err := p.aliyunWAFInstanceID(client, region)
	if err != nil {
		return err
	}

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		done, err := p.deployDomain(client, region, instanceID, domain, requestCertID)
		if err != nil {
			return err
		}
		if done {
			fillAliyunInfo(info, certID, certName)
			return nil
		}
		p.Log("WAF域名 " + domain + " 部署证书成功！")
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

/*
 * deployDomain 查询并回写单个域名的监听与回源配置
 * 当证书已配置时返回 true，调用方据此结束整个部署流程
 */
func (p *AliyunWAFProvider) deployDomain(client aliyunClient, region, instanceID, domain, certID string) (bool, error) {
	detailReq := newAliyunRequest("GET", aliyunWAFEndpoint(region), "2021-10-01", "DescribeDomainDetail")
	detailReq.QueryParams["InstanceId"] = instanceID
	detailReq.QueryParams["Domain"] = domain
	detailReq.QueryParams["RegionId"] = region

	result, err := callAliyun(client, detailReq)
	if err != nil {
		return false, errors.New("查询CNAME接入详情失败：" + err.Error())
	}

	listen, ok := result["Listen"].(map[string]interface{})
	if !ok {
		return false, errors.New("没有找到" + domain + "监听器")
	}
	if stringFromAny(listen["CertId"]) == certID {
		p.Log("WAF域名 " + domain + " 证书已配置，无需重复操作")
		return true, nil
	}

	listen["CertId"] = certID
	if emptyAliyunValue(listen["HttpsPorts"]) {
		listen["HttpsPorts"] = []interface{}{float64(443)}
		listen["TLSVersion"] = "tlsv1.1"
		listen["EnableTLSv3"] = true
		listen["CipherSuite"] = float64(1)
	}

	redirect, ok := result["Redirect"].(map[string]interface{})
	if !ok {
		return false, errors.New("没有找到" + domain + "回源配置")
	}
	backendPorts, _ := redirect["BackendPorts"].([]interface{})
	if len(backendPorts) == 1 {
		if firstPort, ok := backendPorts[0].(map[string]interface{}); ok && stringFromAny(firstPort["Protocol"]) == "http" {
			backendPorts = append(backendPorts, map[string]interface{}{
				"ListenPort":  float64(443),
				"Protocol":    "https",
				"BackendPort": firstPort["BackendPort"],
			})
			redirect["BackendPorts"] = backendPorts
			redirect["FocusHttpBackend"] = true
		}
	}
	redirect["Backends"] = redirect["AllBackends"]

	modifyReq := newAliyunRequest("POST", aliyunWAFEndpoint(region), "2021-10-01", "ModifyDomain")
	modifyReq.QueryParams["InstanceId"] = instanceID
	modifyReq.QueryParams["Domain"] = domain
	modifyReq.QueryParams["Listen"] = aliyunJSONText(listen)
	modifyReq.QueryParams["Redirect"] = aliyunJSONText(redirect)
	modifyReq.QueryParams["RegionId"] = region

	if _, err := callAliyun(client, modifyReq); err != nil {
		return false, fmt.Errorf("WAF域名 %s 部署证书失败: %v", domain, err)
	}
	return false, nil
}

/* emptyAliyunValue 判断字段是否为空 */
func emptyAliyunValue(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []interface{}:
		return len(typed) == 0
	case map[string]interface{}:
		return len(typed) == 0
	}
	return false
}

func (p *AliyunWAFProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/*
 * AliyunWAFResProvider Web应用防火墙3.0云产品接入部署器
 */
type AliyunWAFResProvider struct {
	AliyunProviderBase
}

func NewAliyunWAFResProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunWAFResProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunWAFResProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunWAFResProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/*
 * deploy WAF 3.0 云产品接入证书下发
 */
func (p *AliyunWAFResProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	resourceIDs := splitAliyunDomains(p.GetStringFrom(config, "waf_resource_id"))
	if len(resourceIDs) == 0 {
		return errors.New("云产品防护对象ID不能为空")
	}
	region := p.regionOf(config)
	deployType := p.deployTypeOf(config)

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}
	/* 请求使用带地域后缀的证书ID，回传保留原始值 */
	requestCertID := certID + aliyunCertIDSuffix(region)

	client, err := p.aliyunWAFClient(region)
	if err != nil {
		return err
	}
	instanceID, err := p.aliyunWAFInstanceID(client, region)
	if err != nil {
		return err
	}

	for _, resourceID := range resourceIDs {
		resourceID = strings.TrimSpace(resourceID)
		if resourceID == "" {
			continue
		}
		if err := p.deployResource(client, region, instanceID, resourceID, requestCertID, deployType); err != nil {
			return err
		}
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

/* deployResource 下发单个云产品防护对象的证书 */
func (p *AliyunWAFResProvider) deployResource(client aliyunClient, region, instanceID, resourceID, certID string, deployType int) error {
	parts := strings.Split(resourceID, "-")
	if len(parts) < 3 {
		return errors.New("ResourceInstanceId解析失败：" + resourceID)
	}
	resourceInstanceID := parts[len(parts)-3]
	if resourceInstanceID == "" {
		return errors.New("ResourceInstanceId解析失败：" + resourceID)
	}

	listReq := newAliyunRequest("GET", aliyunWAFEndpoint(region), "2021-10-01", "DescribeCloudResourceList")
	listReq.QueryParams["InstanceId"] = instanceID
	listReq.QueryParams["CloudResourceId"] = resourceID
	listReq.QueryParams["RegionId"] = region

	result, err := callAliyun(client, listReq)
	if err != nil {
		return errors.New("查询云产品接入WAF配置失败：" + err.Error())
	}
	if emptyAliyunValue(result["CloudResourceList"]) {
		return errors.New("WAF云产品接入实例不存在：" + resourceID)
	}

	if deployType == 0 {
		req := newAliyunRequest("POST", aliyunWAFEndpoint(region), "2021-10-01", "ModifyCloudResourceDefaultCert")
		req.QueryParams["InstanceId"] = instanceID
		req.QueryParams["CloudResourceId"] = resourceID
		req.QueryParams["CertId"] = certID
		req.QueryParams["RegionId"] = region
		if _, err := callAliyun(client, req); err != nil {
			return fmt.Errorf("WAF云产品防护对象 %s 部署默认证书失败: %v", resourceID, err)
		}
		p.Log("WAF云产品防护对象 " + resourceID + " 部署默认证书成功！")
		return nil
	}

	req := newAliyunRequest("POST", aliyunWAFEndpoint(region), "2021-10-01", "CreateCloudResourceExtensionCert")
	req.QueryParams["InstanceId"] = instanceID
	req.QueryParams["CloudResourceId"] = resourceID
	req.QueryParams["CertId"] = certID
	req.QueryParams["RegionId"] = region
	if _, err := callAliyun(client, req); err != nil {
		return fmt.Errorf("WAF云产品防护对象 %s 部署扩展证书失败: %v", resourceID, err)
	}
	p.Log("WAF云产品防护对象 " + resourceID + " 部署扩展证书成功！")

	p.cleanExpiredResourceCerts(client, region, instanceID, resourceInstanceID, resourceID)
	return nil
}

/*
 * cleanExpiredResourceCerts 删除实例下已过期的扩展证书
 */
func (p *AliyunWAFResProvider) cleanExpiredResourceCerts(client aliyunClient, region, instanceID, resourceInstanceID, resourceID string) {
	listReq := newAliyunRequest("GET", aliyunWAFEndpoint(region), "2021-10-01", "DescribeResourceInstanceCerts")
	listReq.QueryParams["InstanceId"] = instanceID
	listReq.QueryParams["ResourceInstanceId"] = resourceInstanceID
	listReq.QueryParams["RegionId"] = region

	result, err := callAliyun(client, listReq)
	if err != nil {
		p.Log("查询扩展证书列表失败：" + err.Error())
		return
	}

	certs, _ := result["Certs"].([]interface{})
	now := time.Now()
	for _, item := range certs {
		cert, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		certIdentifier := stringFromAny(cert["CertIdentifier"])
		afterDate := stringFromAny(cert["AfterDate"])
		if certIdentifier == "" || afterDate == "" {
			continue
		}
		expireTime, ok := parseAliyunTime(afterDate)
		if !ok || !expireTime.Before(now) {
			continue
		}

		delReq := newAliyunRequest("POST", aliyunWAFEndpoint(region), "2021-10-01", "DeleteCloudResourceExtensionCert")
		delReq.QueryParams["InstanceId"] = instanceID
		delReq.QueryParams["CloudResourceId"] = resourceID
		delReq.QueryParams["CertId"] = certIdentifier
		delReq.QueryParams["RegionId"] = region
		if _, err := callAliyun(client, delReq); err != nil {
			p.Log("删除过期扩展证书失败：" + certIdentifier + " " + err.Error())
			continue
		}
		p.Log("已删除过期扩展证书：" + certIdentifier)
	}
}

func (p *AliyunWAFResProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/*
 * AliyunWAF2Provider Web应用防火墙2.0部署器
 */
type AliyunWAF2Provider struct {
	AliyunProviderBase
}

func NewAliyunWAF2Provider(config map[string]interface{}) base.DeployProvider {
	return &AliyunWAF2Provider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunWAF2Provider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunWAF2Provider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}
func (p *AliyunWAF2Provider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return errors.New("WAF绑定域名不能为空")
	}
	region := p.regionOf(config)

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}

	client, err := p.client(region)
	if err != nil {
		return err
	}

	infoReq := newAliyunRequest("GET", aliyunWAFEndpoint(region), "2019-09-10", "DescribeInstanceInfo")
	infoReq.QueryParams["RegionId"] = region
	infoResult, err := callAliyun(client, infoReq)
	if err != nil {
		return errors.New("获取WAF实例详情失败：" + err.Error())
	}
	instanceInfo, _ := infoResult["InstanceInfo"].(map[string]interface{})
	instanceID := stringFromAny(instanceInfo["InstanceId"])
	if instanceID == "" {
		return errors.New("当前账号未找到WAF实例")
	}
	p.Log("获取WAF实例ID成功 InstanceId=" + instanceID)

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		req := newAliyunRequest("POST", aliyunWAFEndpoint(region), "2019-09-10", "CreateCertificateByCertificateId")
		req.QueryParams["InstanceId"] = instanceID
		req.QueryParams["Domain"] = domain
		req.QueryParams["CertificateId"] = certID
		if _, err := callAliyun(client, req); err != nil {
			return fmt.Errorf("WAF域名 %s 部署证书失败: %v", domain, err)
		}
		p.Log("WAF域名 " + domain + " 部署证书成功！")
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

func (p *AliyunWAF2Provider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/*
 * AliyunAPIProvider API网关部署器
 */
type AliyunAPIProvider struct {
	AliyunProviderBase
}

func NewAliyunAPIProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunAPIProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书 */
func (p *AliyunAPIProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config)
}

/* deploy API网关证书下发 */
func (p *AliyunAPIProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	groupID := p.GetStringFrom(config, "api_groupid")
	if groupID == "" {
		return errors.New("API分组ID不能为空")
	}
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return errors.New("API分组绑定域名不能为空")
	}
	regionID := p.regionIDOf(config)

	certInfo, err := parseAliyunCert(fullchain)
	if err != nil {
		return err
	}

	client, err := p.client(regionID)
	if err != nil {
		return err
	}

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		req := newAliyunRequest("POST", "apigateway."+regionID+".aliyuncs.com", "2016-07-14", "SetDomainCertificate")
		req.QueryParams["GroupId"] = groupID
		req.QueryParams["DomainName"] = domain
		req.QueryParams["CertificateName"] = certInfo.certName
		req.QueryParams["CertificateBody"] = fullchain
		req.QueryParams["CertificatePrivateKey"] = privateKey
		if _, err := callAliyun(client, req); err != nil {
			return fmt.Errorf("API网关域名 %s 部署证书失败: %v", domain, err)
		}
		p.Log("API网关域名 " + domain + " 部署证书成功！")
	}

	return nil
}

func (p *AliyunAPIProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/* 确保 WAF 部署器满足 DeployWithInfoProvider 接口 */
var (
	_ base.DeployWithInfoProvider = (*AliyunWAFProvider)(nil)
	_ base.DeployWithInfoProvider = (*AliyunWAFResProvider)(nil)
	_ base.DeployWithInfoProvider = (*AliyunWAF2Provider)(nil)
)
