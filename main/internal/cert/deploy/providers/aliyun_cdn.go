package providers

import (
	"main/internal/cert/deploy/base"
	"context"
	"fmt"
	"main/internal/cert"
	"strings"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cdn"
)

func init() {
	base.Register("aliyun_cdn", NewAliyunCDNProvider)
}

type AliyunCDNProvider struct {
	base.BaseProvider
}

func NewAliyunCDNProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunCDNProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *AliyunCDNProvider) getClient() (*cdn.Client, error) {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("AccessKey不能为空")
	}

	cred := credentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
	conf := sdk.NewConfig()
	return cdn.NewClientWithOptions("cn-hangzhou", conf, cred)
}

func (p *AliyunCDNProvider) Check(ctx context.Context) error {
	client, err := p.getClient()
	if err != nil {
		return err
	}

	request := cdn.CreateDescribeUserDomainsRequest()
	request.PageSize = requests.NewInteger(1)
	_, err = client.DescribeUserDomains(request)
	return err
}

func (p *AliyunCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
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
		p.Log("正在部署证书到阿里云CDN: " + domain)

		certName := fmt.Sprintf("cert-%s-%d", strings.ReplaceAll(domain, ".", "-"), time.Now().Unix())

		request := cdn.CreateSetCdnDomainSSLCertificateRequest()
		request.DomainName = domain
		request.CertName = certName
		request.CertType = "upload"
		request.SSLProtocol = "on"
		request.SSLPub = fullchain
		request.SSLPri = privateKey

		_, err := client.SetCdnDomainSSLCertificate(request)
		if err != nil {
			return fmt.Errorf("阿里云CDN部署失败(%s): %v", domain, err)
		}
	}

	p.Log("阿里云CDN证书部署成功")
	return nil
}

func (p *AliyunCDNProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
