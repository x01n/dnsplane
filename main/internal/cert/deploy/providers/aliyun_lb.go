package providers

import (
	"context"
	"errors"
	"fmt"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("aliyun_clb", NewAliyunCLBProvider)
	base.Register("aliyun_alb", NewAliyunALBProvider)
	base.Register("aliyun_nlb", NewAliyunNLBProvider)
}

/*
 * AliyunCLBProvider 传统型负载均衡部署器
 */
type AliyunCLBProvider struct {
	AliyunProviderBase
}

func NewAliyunCLBProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunCLBProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunCLBProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunCLBProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

func (p *AliyunCLBProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	loadBalancerID := p.GetStringFrom(config, "clb_id")
	if loadBalancerID == "" {
		return errors.New("负载均衡实例ID不能为空")
	}
	listenerPort := p.GetStringFrom(config, "clb_port")
	if listenerPort == "" {
		return errors.New("HTTPS监听端口不能为空")
	}
	regionID := p.regionIDOf(config)
	endpoint := "slb." + regionID + ".aliyuncs.com"

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}

	client, err := p.client(regionID)
	if err != nil {
		return err
	}

	serverCertID, err := p.ensureServerCertificate(client, endpoint, regionID, certID, certName)
	if err != nil {
		return err
	}

	if p.deployTypeOf(config) == 1 {
		if err := p.deployDomainExtensions(client, endpoint, regionID, loadBalancerID, listenerPort, serverCertID, config); err != nil {
			return err
		}
	} else {
		if err := p.deployHTTPSListener(client, endpoint, regionID, loadBalancerID, listenerPort, serverCertID); err != nil {
			return err
		}
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

/*
 * ensureServerCertificate 查找或上传服务器证书
 */
func (p *AliyunCLBProvider) ensureServerCertificate(client aliyunClient, endpoint, regionID, certID, certName string) (string, error) {
	listReq := newAliyunRequest("POST", endpoint, "2014-05-15", "DescribeServerCertificates")
	listReq.QueryParams["RegionId"] = regionID

	result, err := callAliyun(client, listReq)
	if err != nil {
		return "", errors.New("获取服务器证书列表失败：" + err.Error())
	}

	certificates, _ := result["ServerCertificates"].(map[string]interface{})
	items, _ := certificates["ServerCertificate"].([]interface{})
	for _, item := range items {
		cert, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if intFromAny(cert["IsAliCloudCertificate"]) == 1 && stringFromAny(cert["AliCloudCertificateId"]) == certID {
			serverCertID := stringFromAny(cert["ServerCertificateId"])
			p.Log("找到已添加的服务器证书 ServerCertificateId=" + serverCertID)
			return serverCertID, nil
		}
	}

	uploadReq := newAliyunRequest("POST", endpoint, "2014-05-15", "UploadServerCertificate")
	uploadReq.QueryParams["RegionId"] = regionID
	uploadReq.QueryParams["AliCloudCertificateId"] = certID
	uploadReq.QueryParams["AliCloudCertificateName"] = certName
	uploadReq.QueryParams["AliCloudCertificateRegionId"] = "cn-hangzhou"

	uploadResult, err := callAliyun(client, uploadReq)
	if err != nil {
		return "", errors.New("服务器证书添加失败：" + err.Error())
	}
	serverCertID := stringFromAny(uploadResult["ServerCertificateId"])
	if serverCertID == "" {
		return "", errors.New("服务器证书添加失败：未返回 ServerCertificateId")
	}
	p.Log("服务器证书添加成功 ServerCertificateId=" + serverCertID)
	return serverCertID, nil
}

func (p *AliyunCLBProvider) deployDomainExtensions(client aliyunClient, endpoint, regionID, loadBalancerID, listenerPort, serverCertID string, config map[string]interface{}) error {
	extensionDomain := p.GetStringFrom(config, "clb_domain")
	if extensionDomain == "" {
		return errors.New("扩展域名不能为空")
	}
	domains := splitAliyunDomains(extensionDomain)

	listReq := newAliyunRequest("POST", endpoint, "2014-05-15", "DescribeDomainExtensions")
	listReq.QueryParams["RegionId"] = regionID
	listReq.QueryParams["LoadBalancerId"] = loadBalancerID
	listReq.QueryParams["ListenerPort"] = listenerPort

	result, err := callAliyun(client, listReq)
	if err != nil {
		return errors.New("扩展域名列表查询失败：" + err.Error())
	}

	extensions, _ := result["DomainExtensions"].(map[string]interface{})
	items, _ := extensions["DomainExtension"].([]interface{})
	for _, item := range items {
		extension, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		domain := stringFromAny(extension["Domain"])
		if !aliyunContains(domains, domain) {
			continue
		}
		if stringFromAny(extension["ServerCertificateId"]) == serverCertID {
			p.Log("负载均衡HTTPS扩展域名 " + domain + " 证书已配置")
			continue
		}

		setReq := newAliyunRequest("POST", endpoint, "2014-05-15", "SetDomainExtensionAttribute")
		setReq.QueryParams["RegionId"] = regionID
		setReq.QueryParams["DomainExtensionId"] = stringFromAny(extension["DomainExtensionId"])
		setReq.QueryParams["ServerCertificateId"] = serverCertID
		if _, err := callAliyun(client, setReq); err != nil {
			return fmt.Errorf("负载均衡HTTPS扩展域名 %s 证书更新失败: %v", domain, err)
		}
		p.Log("负载均衡HTTPS扩展域名 " + domain + " 证书更新成功")
	}
	return nil
}
func (p *AliyunCLBProvider) deployHTTPSListener(client aliyunClient, endpoint, regionID, loadBalancerID, listenerPort, serverCertID string) error {
	detailReq := newAliyunRequest("POST", endpoint, "2014-05-15", "DescribeLoadBalancerHTTPSListenerAttribute")
	detailReq.QueryParams["RegionId"] = regionID
	detailReq.QueryParams["LoadBalancerId"] = loadBalancerID
	detailReq.QueryParams["ListenerPort"] = listenerPort

	result, err := callAliyun(client, detailReq)
	if err != nil {
		return errors.New("HTTPS监听配置查询失败：" + err.Error())
	}
	if stringFromAny(result["ServerCertificateId"]) == serverCertID {
		p.Log("负载均衡HTTPS监听已配置该证书，无需重复操作")
		return nil
	}

	setReq := newAliyunRequest("POST", endpoint, "2014-05-15", "SetLoadBalancerHTTPSListenerAttribute")
	setReq.QueryParams["RegionId"] = regionID
	setReq.QueryParams["LoadBalancerId"] = loadBalancerID
	setReq.QueryParams["ListenerPort"] = listenerPort
	setReq.QueryParams["ServerCertificateId"] = serverCertID
	if _, err := callAliyun(client, setReq); err != nil {
		return fmt.Errorf("负载均衡HTTPS监听证书配置失败: %v", err)
	}
	p.Log("负载均衡HTTPS监听证书配置成功！")
	return nil
}

func (p *AliyunCLBProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/*
 * AliyunALBProvider 应用型负载均衡部署器
 */
type AliyunALBProvider struct {
	AliyunProviderBase
}

func NewAliyunALBProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunALBProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunALBProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunALBProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/* deploy ALB 证书下发 */
func (p *AliyunALBProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	listenerID := p.GetStringFrom(config, "alb_listener_id")
	if listenerID == "" {
		return errors.New("负载均衡监听ID不能为空")
	}
	regionID := p.regionIDOf(config)
	endpoint := "alb." + regionID + ".aliyuncs.com"

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}
	/* 请求使用带 cn-hangzhou 后缀的证书ID，回传保留原始值 */
	requestCertID := certID + aliyunCertIDSuffix("cn-hangzhou")

	client, err := p.client(regionID)
	if err != nil {
		return err
	}

	if p.deployTypeOf(config) == 1 {
		listReq := newAliyunRequest("POST", endpoint, "2020-06-16", "ListListenerCertificates")
		listReq.QueryParams["MaxResults"] = "100"
		listReq.QueryParams["ListenerId"] = listenerID
		listReq.QueryParams["CertificateType"] = "Server"

		result, err := callAliyun(client, listReq)
		if err != nil {
			return errors.New("获取监听证书列表失败：" + err.Error())
		}
		certificates, _ := result["Certificates"].([]interface{})
		for _, item := range certificates {
			cert, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if stringFromAny(cert["CertificateId"]) == requestCertID {
				p.Log("负载均衡监听扩展证书已添加，无需重复操作")
				fillAliyunInfo(info, certID, certName)
				return nil
			}
		}

		assocReq := newAliyunRequest("POST", endpoint, "2020-06-16", "AssociateAdditionalCertificatesWithListener")
		assocReq.QueryParams["ListenerId"] = listenerID
		assocReq.QueryParams["Certificates.1.CertificateId"] = requestCertID
		if _, err := callAliyun(client, assocReq); err != nil {
			return fmt.Errorf("应用型负载均衡监听扩展证书添加失败: %v", err)
		}
		p.Log("应用型负载均衡监听扩展证书添加成功！")
	} else {
		updateReq := newAliyunRequest("POST", endpoint, "2020-06-16", "UpdateListenerAttribute")
		updateReq.QueryParams["ListenerId"] = listenerID
		updateReq.QueryParams["Certificates.1.CertificateId"] = requestCertID
		if _, err := callAliyun(client, updateReq); err != nil {
			return fmt.Errorf("应用型负载均衡监听默认证书更新失败: %v", err)
		}
		p.Log("应用型负载均衡监听默认证书更新成功！")
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

func (p *AliyunALBProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/*
 * AliyunNLBProvider 网络型负载均衡部署器
 */
type AliyunNLBProvider struct {
	AliyunProviderBase
}

func NewAliyunNLBProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunNLBProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunNLBProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunNLBProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/* deploy NLB 证书下发 */
func (p *AliyunNLBProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	listenerID := p.GetStringFrom(config, "nlb_listener_id")
	if listenerID == "" {
		return errors.New("负载均衡监听ID不能为空")
	}
	regionID := p.regionIDOf(config)
	endpoint := "nlb." + regionID + ".aliyuncs.com"

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}
	/* 请求使用带 cn-hangzhou 后缀的证书ID，回传保留原始值 */
	requestCertID := certID + aliyunCertIDSuffix("cn-hangzhou")

	client, err := p.client(regionID)
	if err != nil {
		return err
	}

	if p.deployTypeOf(config) == 1 {
		listReq := newAliyunRequest("POST", endpoint, "2022-04-30", "ListListenerCertificates")
		listReq.QueryParams["MaxResults"] = "50"
		listReq.QueryParams["ListenerId"] = listenerID
		listReq.QueryParams["CertificateType"] = "Server"

		result, err := callAliyun(client, listReq)
		if err != nil {
			return errors.New("获取监听证书列表失败：" + err.Error())
		}
		certificates, _ := result["Certificates"].([]interface{})
		for _, item := range certificates {
			cert, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if stringFromAny(cert["CertificateId"]) == requestCertID {
				p.Log("负载均衡监听扩展证书已添加，无需重复操作")
				fillAliyunInfo(info, certID, certName)
				return nil
			}
		}

		assocReq := newAliyunRequest("POST", endpoint, "2022-04-30", "AssociateAdditionalCertificatesWithListener")
		assocReq.QueryParams["ListenerId"] = listenerID
		assocReq.QueryParams["AdditionalCertificateIds.1"] = requestCertID
		if _, err := callAliyun(client, assocReq); err != nil {
			return fmt.Errorf("网络型负载均衡监听扩展证书添加失败: %v", err)
		}
		p.Log("网络型负载均衡监听扩展证书添加成功！")
	} else {
		updateReq := newAliyunRequest("POST", endpoint, "2022-04-30", "UpdateListenerAttribute")
		updateReq.QueryParams["ListenerId"] = listenerID
		updateReq.QueryParams["CertificateIds.1"] = requestCertID
		if _, err := callAliyun(client, updateReq); err != nil {
			return fmt.Errorf("网络型负载均衡监听默认证书更新失败: %v", err)
		}
		p.Log("网络型负载均衡监听默认证书更新成功！")
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

func (p *AliyunNLBProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/* aliyunContains 判断字符串切片是否包含指定值 */
func aliyunContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

/* 确保负载均衡部署器满足 DeployWithInfoProvider 接口 */
var (
	_ base.DeployWithInfoProvider = (*AliyunCLBProvider)(nil)
	_ base.DeployWithInfoProvider = (*AliyunALBProvider)(nil)
	_ base.DeployWithInfoProvider = (*AliyunNLBProvider)(nil)
)
