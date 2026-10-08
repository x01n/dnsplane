package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

const (
	/* aliyunESASingaporeEndpoint ESA 新加坡接入点 */
	aliyunESASingaporeEndpoint = "esa.ap-southeast-1.aliyuncs.com"
	/* aliyunESADefaultEndpoint ESA 中国内地接入点 */
	aliyunESADefaultEndpoint = "esa.cn-hangzhou.aliyuncs.com"
	/* aliyunESAVersion ESA OpenAPI 版本号 */
	aliyunESAVersion = "2024-09-10"
)

/* aliyunESAEndpoint 依据地域返回 ESA 接入点 */
func aliyunESAEndpoint(region string) string {
	if region == "ap-southeast-1" {
		return aliyunESASingaporeEndpoint
	}
	return aliyunESADefaultEndpoint
}

/*
 * aliyunESASiteID 查询站点并返回 SiteId
 */
func (p *AliyunProviderBase) aliyunESASiteID(client aliyunClient, region, sitename string) (int64, error) {
	req := newAliyunRequest("GET", aliyunESAEndpoint(region), aliyunESAVersion, "ListSites")
	req.QueryParams["SiteName"] = sitename
	req.QueryParams["SiteSearchType"] = "exact"

	result, err := callAliyun(client, req)
	if err != nil {
		return 0, errors.New("查询ESA站点列表失败：" + err.Error())
	}
	if intFromAny(result["TotalCount"]) == 0 {
		return 0, fmt.Errorf("ESA站点 %s 不存在", sitename)
	}

	sites, _ := result["Sites"].([]interface{})
	if len(sites) == 0 {
		return 0, fmt.Errorf("ESA站点 %s 返回的站点ID无效", sitename)
	}
	site, _ := sites[0].(map[string]interface{})
	siteID := intFromAny(site["SiteId"])
	if siteID < 1 {
		return 0, fmt.Errorf("ESA站点 %s 返回的站点ID无效", sitename)
	}
	p.Log(fmt.Sprintf("成功查询到%d个ESA站点", intFromAny(result["TotalCount"])))
	return siteID, nil
}

/* init 注册 ESA 与 ESA SAAS 两个部署器 */
func init() {
	base.Register("aliyun_esa", NewAliyunESAProvider)
	base.Register("aliyun_esa_saas", NewAliyunESASaaSProvider)
}

/*
 * AliyunESAProvider 边缘安全加速ESA站点证书部署器
 */
type AliyunESAProvider struct {
	AliyunProviderBase
}

func NewAliyunESAProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunESAProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunESAProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunESAProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/*
 * deploy ESA 站点证书下发
 * 先查已有证书，命中相同域名集合则复用，配额已满则删除最旧证书，最后写入 CAS 引用
 */
func (p *AliyunESAProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	sitename := p.GetStringFrom(config, "esa_sitename")
	if sitename == "" {
		return errors.New("ESA站点名称不能为空")
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

	siteID, err := p.aliyunESASiteID(client, region, sitename)
	if err != nil {
		return err
	}

	certs, err := p.listESACertificates(client, region, siteID, sitename)
	if err != nil {
		return err
	}

	domainSet := map[string]bool{}
	for _, domain := range base.GetConfigDomains(config) {
		domainSet[domain] = true
	}

	var existCert map[string]interface{}
	var oldestCert map[string]interface{}
	for _, cert := range certs {
		if stringFromAny(cert["Type"]) == "free" {
			continue
		}
		if aliyunESACertCoversDomains(cert, domainSet) {
			existCert = cert
			break
		}
		if oldestCert == nil {
			oldestCert = cert
			continue
		}
		certTime, _ := parseAliyunTime(stringFromAny(cert["CreateTime"]))
		oldestTime, _ := parseAliyunTime(stringFromAny(oldestCert["CreateTime"]))
		if certTime.Before(oldestTime) {
			oldestCert = cert
		}
	}

	if existCert == nil && oldestCert != nil {
		if err := p.freeESACertQuota(client, region, siteID, sitename, oldestCert); err != nil {
			return err
		}
	}

	setReq := newAliyunRequest("POST", aliyunESAEndpoint(region), aliyunESAVersion, "SetCertificate")
	setReq.QueryParams["SiteId"] = fmt.Sprintf("%d", siteID)
	setReq.QueryParams["Type"] = "cas"
	setReq.QueryParams["CasId"] = certID
	setReq.QueryParams["Name"] = certName
	setReq.QueryParams["Region"] = region

	if existCert != nil {
		if stringFromAny(existCert["CasId"]) == certID {
			p.Log("ESA站点 " + sitename + " 证书已配置，无需重复操作")
			fillAliyunInfo(info, certID, certName)
			return nil
		}
		setReq.QueryParams["Id"] = stringFromAny(existCert["Id"])
	}

	if _, err := callAliyun(client, setReq); err != nil {
		return fmt.Errorf("ESA站点 %s 证书部署失败: %v", sitename, err)
	}

	if existCert != nil {
		p.Log("ESA站点 " + sitename + " 证书 " + stringFromAny(existCert["Name"]) + " 更新成功")
	} else {
		p.Log("ESA站点 " + sitename + " 证书添加成功！")
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}
func (p *AliyunESAProvider) listESACertificates(client aliyunClient, region string, siteID int64, sitename string) ([]map[string]interface{}, error) {
	req := newAliyunRequest("GET", aliyunESAEndpoint(region), aliyunESAVersion, "ListCertificates")
	req.QueryParams["SiteId"] = fmt.Sprintf("%d", siteID)

	result, err := callAliyun(client, req)
	if err != nil {
		return nil, fmt.Errorf("查询ESA站点%s证书列表失败：%v", sitename, err)
	}
	p.Log(fmt.Sprintf("ESA站点 %s 查询到%d个SSL证书", sitename, intFromAny(result["TotalCount"])))

	items, _ := result["Result"].([]interface{})
	certs := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if cert, ok := item.(map[string]interface{}); ok {
			certs = append(certs, cert)
		}
	}
	return certs, nil
}

func aliyunESACertCoversDomains(cert map[string]interface{}, domainSet map[string]bool) bool {
	for _, domain := range strings.Split(stringFromAny(cert["SAN"]), ",") {
		if !domainSet[domain] {
			return false
		}
	}
	return true
}

/*
 * freeESACertQuota 证书配额已满时删除最旧的自定义证书
 */
func (p *AliyunESAProvider) freeESACertQuota(client aliyunClient, region string, siteID int64, sitename string, oldestCert map[string]interface{}) error {
	quotaReq := newAliyunRequest("GET", aliyunESAEndpoint(region), aliyunESAVersion, "ListInstanceQuotasWithUsage")
	quotaReq.QueryParams["SiteId"] = fmt.Sprintf("%d", siteID)
	quotaReq.QueryParams["QuotaNames"] = "customHttpCert"

	result, err := callAliyun(client, quotaReq)
	if err != nil {
		return fmt.Errorf("查询ESA站点证书配额失败：%v", err)
	}

	quotas, _ := result["Quotas"].([]interface{})
	if len(quotas) == 0 {
		return nil
	}
	quota, _ := quotas[0].(map[string]interface{})
	if intFromAny(quota["Usage"]) < intFromAny(quota["QuotaValue"]) {
		return nil
	}

	certName := stringFromAny(oldestCert["Name"])
	delReq := newAliyunRequest("GET", aliyunESAEndpoint(region), aliyunESAVersion, "DeleteCertificate")
	delReq.QueryParams["SiteId"] = fmt.Sprintf("%d", siteID)
	delReq.QueryParams["Id"] = stringFromAny(oldestCert["Id"])
	if _, err := callAliyun(client, delReq); err != nil {
		return fmt.Errorf("ESA站点 %s 删除证书%s失败：%v", sitename, certName, err)
	}
	p.Log("ESA站点 " + sitename + " 删除证书 " + certName + " 成功")
	return nil
}

func (p *AliyunESAProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/*
 * AliyunESASaaSProvider ESA SAAS 自定义域名部署器
 */
type AliyunESASaaSProvider struct {
	AliyunProviderBase
}

func NewAliyunESASaaSProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunESASaaSProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunESASaaSProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunESASaaSProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/*
 * deploy ESA SAAS 自定义域名证书下发
 */
func (p *AliyunESASaaSProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	sitename := p.GetStringFrom(config, "esa_sitename")
	if sitename == "" {
		return errors.New("ESA站点名称不能为空")
	}

	saasDomains := aliyunSaaSDomainList(p.GetStringFrom(config, "esa_saas_sitename"))
	if len(saasDomains) == 0 {
		return errors.New("ESA SAAS域名不能为空")
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

	siteID, err := p.aliyunESASiteID(client, region, sitename)
	if err != nil {
		return err
	}

	success := 0
	failed := 0
	firstError := ""
	for _, saasDomain := range saasDomains {
		stage := "查询"
		hostnameID, err := p.findSaaSHostname(client, region, siteID, saasDomain)
		if err == nil {
			stage = "部署"
			err = p.updateSaaSHostname(client, region, hostnameID, saasDomain, certID)
		}
		if err != nil {
			msg := "ESA SAAS站点 " + saasDomain + " " + stage + "失败：" + err.Error()
			p.Log("[Error] " + msg)
			if firstError == "" {
				firstError = msg
			}
			failed++
			continue
		}
		success++
	}

	summary := fmt.Sprintf("ESA SAAS证书部署完成：成功 %d 个，失败 %d 个", success, failed)
	p.Log(summary)
	if failed > 0 {
		summary += "；详见部署日志；首个错误："
		return errors.New(summary + aliyunTruncateBytes(firstError, 300-len(summary)))
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

/*
 * findSaaSHostname 查询自定义主机名并返回 HostnameId
 */
func (p *AliyunESASaaSProvider) findSaaSHostname(client aliyunClient, region string, siteID int64, saasDomain string) (string, error) {
	req := newAliyunRequest("GET", aliyunESAEndpoint(region), aliyunESAVersion, "ListCustomHostnames")
	req.QueryParams["Hostname"] = saasDomain
	req.QueryParams["SiteId"] = fmt.Sprintf("%d", siteID)
	req.QueryParams["NameMatchType"] = "exact"

	result, err := callAliyun(client, req)
	if err != nil {
		return "", err
	}

	rawHostnames, ok := result["Hostnames"]
	if _, hasCount := result["TotalCount"]; !hasCount || !ok {
		return "", errors.New("返回的域名列表无效")
	}
	if intFromAny(result["TotalCount"]) == 0 {
		return "", errors.New("域名不存在")
	}
	hostnames, ok := rawHostnames.([]interface{})
	if !ok || len(hostnames) == 0 {
		return "", errors.New("域名不存在")
	}

	var matches []map[string]interface{}
	for _, item := range hostnames {
		hostname, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if strings.EqualFold(stringFromAny(hostname["Hostname"]), saasDomain) && intFromAny(hostname["SiteId"]) == siteID {
			matches = append(matches, hostname)
		}
	}
	if len(matches) == 0 {
		return "", errors.New("返回的域名或站点不匹配")
	}
	if len(matches) > 1 {
		return "", errors.New("返回多个匹配域名，无法确定部署目标")
	}

	hostnameID := stringFromAny(matches[0]["HostnameId"])
	if intFromAny(hostnameID) < 1 {
		return "", errors.New("返回的HostnameId无效")
	}
	return hostnameID, nil
}

func (p *AliyunESASaaSProvider) updateSaaSHostname(client aliyunClient, region, hostnameID, saasDomain, certID string) error {
	req := newAliyunRequest("POST", aliyunESAEndpoint(region), aliyunESAVersion, "UpdateCustomHostname")
	req.QueryParams["HostnameId"] = hostnameID
	req.QueryParams["SslFlag"] = "on"
	req.QueryParams["CertType"] = "cas"
	req.QueryParams["CasId"] = certID
	req.QueryParams["CasRegion"] = region

	p.Log("ESA SAAS站点 " + saasDomain + " 部署参数 " + aliyunJSONText(map[string]interface{}{
		"Action":     "UpdateCustomHostname",
		"HostnameId": hostnameID,
		"SslFlag":    "on",
		"CertType":   "cas",
		"CasId":      certID,
		"CasRegion":  region,
	}))

	result, err := callAliyun(client, req)
	if err != nil {
		return err
	}
	p.Log("ESA SAAS站点 " + saasDomain + " 部署结果 " + aliyunJSONText(result))
	p.Log("ESA SAAS站点 " + saasDomain + " 证书添加成功！")
	return nil
}

func (p *AliyunESASaaSProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

func aliyunSaaSDomainList(input string) []string {
	seen := map[string]bool{}
	var domains []string
	for _, line := range base.SplitDomains(input) {
		domain := strings.ToLower(line)
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		domains = append(domains, domain)
	}
	return domains
}

/*
 * aliyunTruncateBytes 按 UTF-8 边界截断字符串到指定字节数
 * limit 小于等于 0 时返回空串
 */
func aliyunTruncateBytes(input string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(input) <= limit {
		return input
	}
	end := limit
	for end > 0 && input[end]&0xC0 == 0x80 {
		end--
	}
	return input[:end]
}

/* 确保两个 ESA 部署器满足 DeployWithInfoProvider 接口 */
var (
	_ base.DeployWithInfoProvider = (*AliyunESAProvider)(nil)
	_ base.DeployWithInfoProvider = (*AliyunESASaaSProvider)(nil)
)
