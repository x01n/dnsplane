package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("aliyun_cdn", NewAliyunCDNProvider)
	base.Register("aliyun_dcdn", NewAliyunCDNProvider)
}

/*
 * AliyunCDNProvider 阿里云CDN与全站加速DCDN部署器
 * 两者接口仅接入点与 Action 不同，共用同一实现
 */
type AliyunCDNProvider struct {
	AliyunProviderBase
}

func NewAliyunCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunCDNProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* aliyunCDNTarget CDN/DCDN 的接口差异 */
type aliyunCDNTarget struct {
	endpoint string
	version  string
	action   string
	label    string
}

/* targetOf 依据 product 选择 CDN 或 DCDN 接入点，缺省为 CDN */
func (p *AliyunCDNProvider) targetOf(config map[string]interface{}) (aliyunCDNTarget, error) {
	switch p.GetStringFrom(config, "product") {
	case "", "cdn":
		return aliyunCDNTarget{
			endpoint: "cdn.aliyuncs.com",
			version:  "2018-05-10",
			action:   "SetCdnDomainSSLCertificate",
			label:    "CDN",
		}, nil
	case "dcdn":
		return aliyunCDNTarget{
			endpoint: "dcdn.aliyuncs.com",
			version:  "2018-01-15",
			action:   "SetDcdnDomainSSLCertificate",
			label:    "DCDN",
		}, nil
	}
	return aliyunCDNTarget{}, fmt.Errorf("未知的产品类型: %s", p.GetStringFrom(config, "product"))
}

/* Deploy 部署证书，不回传附加信息 */
func (p *AliyunCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 部署证书并回传 CertId 与证书名称 */
func (p *AliyunCDNProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/*
 * deploy 逐个域名下发证书
 */
func (p *AliyunCDNProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	target, err := p.targetOf(config)
	if err != nil {
		return err
	}

	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return errors.New(target.label + "绑定域名不能为空")
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

		req := newAliyunRequest("POST", target.endpoint, target.version, target.action)
		req.QueryParams["DomainName"] = domain
		req.QueryParams["CertName"] = certName
		req.QueryParams["CertType"] = "cas"
		req.QueryParams["SSLProtocol"] = "on"
		req.QueryParams["CertId"] = certID

		if _, err := callAliyun(client, req); err != nil {
			return fmt.Errorf("阿里云%s域名 %s 部署证书失败: %v", target.label, domain, err)
		}
		p.Log(target.label + "域名 " + domain + " 部署证书成功！")
	}

	fillAliyunInfo(info, certID, certName)
	return nil
}

func (p *AliyunCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/* 确保 AliyunCDNProvider 满足 DeployWithInfoProvider 接口 */
var _ base.DeployWithInfoProvider = (*AliyunCDNProvider)(nil)
