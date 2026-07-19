package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"main/internal/cert"
	"main/internal/cert/deploy/base"
	"strings"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

func init() {
	base.Register("aliyun_ddoscoo", NewAliyunDDoSCooProvider)
}

type AliyunDDoSCooProvider struct {
	base.BaseProvider
}

func NewAliyunDDoSCooProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunDDoSCooProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *AliyunDDoSCooProvider) getClient(region string) (*sdk.Client, error) {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")
	if accessKeyID == "" {
		accessKeyID = p.GetString("AccessKeyId")
	}
	if accessKeySecret == "" {
		accessKeySecret = p.GetString("AccessKeySecret")
	}
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("AccessKey不能为空")
	}

	cred := credentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
	conf := sdk.NewConfig()
	return sdk.NewClientWithOptions(region, conf, cred)
}

func (p *AliyunDDoSCooProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domainStr := base.GetConfigString(config, "domain")
	if domainStr == "" {
		domainStr = p.GetString("domain")
	}
	if domainStr == "" {
		return fmt.Errorf("绑定域名不能为空")
	}

	region := base.GetConfigString(config, "region")
	if region == "" {
		region = "cn-hangzhou"
	}

	certID := base.GetConfigString(config, "cert_id")
	if certID == "" {
		return fmt.Errorf("证书ID不能为空，需先上传证书到CAS")
	}

	client, err := p.getClient(region)
	if err != nil {
		return err
	}

	domains := strings.Split(domainStr, ",")
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}

		p.Log("正在部署证书到DDoS高防域名: " + domain)

		req := requests.NewCommonRequest()
		req.Method = "POST"
		req.Domain = "ddoscoo." + region + ".aliyuncs.com"
		req.Version = "2020-01-01"
		req.ApiName = "AssociateWebCert"
		req.QueryParams["Action"] = "AssociateWebCert"
		req.QueryParams["Domain"] = domain
		req.QueryParams["CertId"] = certID

		resp, err := client.ProcessCommonRequest(req)
		if err != nil {
			return fmt.Errorf("DDoS高防域名 %s 部署证书失败: %v", domain, err)
		}

		var result map[string]interface{}
		if jsonErr := json.Unmarshal(resp.GetHttpContentBytes(), &result); jsonErr == nil {
			if code, ok := result["Code"].(string); ok && code != "" {
				return fmt.Errorf("DDoS高防域名 %s 部署失败: %s", domain, code)
			}
		}

		p.Log("DDoS高防域名 " + domain + " 部署证书成功")
	}

	p.Log("阿里云DDoS高防证书部署完成")
	return nil
}

func (p *AliyunDDoSCooProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
