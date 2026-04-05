package providers

import (
	"main/internal/cert/deploy/base"
	"context"
	"fmt"
	"main/internal/cert"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	cdn "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cdn/v1"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cdn/v1/model"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cdn/v1/region"
)

func init() {
	base.Register("huawei_cdn", NewHuaweiCDNProvider)
}

type HuaweiCDNProvider struct {
	base.BaseProvider
}

func NewHuaweiCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &HuaweiCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *HuaweiCDNProvider) getClient() (*cdn.CdnClient, error) {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("AccessKey不能为空")
	}

	auth := basic.NewCredentialsBuilder().
		WithAk(accessKeyID).
		WithSk(accessKeySecret).
		Build()

	return cdn.NewCdnClient(
		cdn.CdnClientBuilder().
			WithRegion(region.CN_NORTH_1).
			WithCredential(auth).
			Build()), nil
}

func (p *HuaweiCDNProvider) Check(ctx context.Context) error {
	client, err := p.getClient()
	if err != nil {
		return err
	}

	request := &model.ListDomainsRequest{}
	pageSize := int32(1)
	request.PageSize = &pageSize
	_, err = client.ListDomains(request)
	return err
}

func (p *HuaweiCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		domain := p.GetString("domain")
		if domain != "" {
			domains = []string{domain}
		}
	}
	if len(domains) == 0 {
		return fmt.Errorf("域名不能为空")
	}

	client, err := p.getClient()
	if err != nil {
		return err
	}

	for _, domain := range domains {
		p.Log("正在部署证书到华为云CDN: " + domain)

		certName := fmt.Sprintf("cert-%s-%d", strings.ReplaceAll(domain, ".", "-"), time.Now().Unix())
		httpsSwitch := int32(1)
		certType := int32(0) // 0: 自有证书

		request := &model.UpdateDomainMultiCertificatesRequest{}
		content := &model.UpdateDomainMultiCertificatesRequestBodyContent{
			DomainName:      domain,
			CertName:        &certName,
			Certificate:     &fullchain,
			PrivateKey:      &privateKey,
			HttpsSwitch:     httpsSwitch,
			CertificateType: &certType,
		}

		request.Body = &model.UpdateDomainMultiCertificatesRequestBody{
			Https: content,
		}

		forceHTTPS := base.GetConfigBool(config, "force_https") || p.GetString("force_https") == "1"
		if forceHTTPS {
			forceRedirect := int32(1)
			content.ForceRedirectHttps = &forceRedirect
		}

		_, err = client.UpdateDomainMultiCertificates(request)
		if err != nil {
			return fmt.Errorf("华为云CDN部署失败(%s): %v", domain, err)
		}
	}

	p.Log("华为云CDN证书部署成功")
	return nil
}

func (p *HuaweiCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
