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
	base.Register("aliyun_ga", NewAliyunGAProvider)
}

type AliyunGAProvider struct {
	base.BaseProvider
}

func NewAliyunGAProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunGAProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *AliyunGAProvider) getClient() (*sdk.Client, error) {
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
	return sdk.NewClientWithOptions("cn-hangzhou", conf, cred)
}

func (p *AliyunGAProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	gaID := base.GetConfigString(config, "ga_id")
	if gaID == "" {
		gaID = p.GetString("ga_id")
	}
	if gaID == "" {
		return fmt.Errorf("全球加速实例ID不能为空")
	}

	listenerID := base.GetConfigString(config, "ga_listener_id")
	if listenerID == "" {
		listenerID = p.GetString("ga_listener_id")
	}
	if listenerID == "" {
		return fmt.Errorf("全球加速监听ID不能为空")
	}

	certID := base.GetConfigString(config, "cert_id")
	if certID == "" {
		return fmt.Errorf("证书ID不能为空，需先上传证书到CAS")
	}
	certID = certID + "-cn-hangzhou"

	client, err := p.getClient()
	if err != nil {
		return err
	}

	deployType := base.GetConfigString(config, "deploy_type")

	if deployType == "1" {
		return p.deployAdditional(client, gaID, listenerID, certID, config)
	}
	return p.deployDefault(client, gaID, listenerID, certID)
}

func (p *AliyunGAProvider) deployDefault(client *sdk.Client, gaID, listenerID, certID string) error {
	p.Log("正在更新全球加速监听默认证书")

	req := requests.NewCommonRequest()
	req.Method = "POST"
	req.Domain = "ga.cn-hangzhou.aliyuncs.com"
	req.Version = "2019-11-20"
	req.ApiName = "UpdateListener"
	req.QueryParams["Action"] = "UpdateListener"
	req.QueryParams["RegionId"] = "cn-hangzhou"
	req.QueryParams["AcceleratorId"] = gaID
	req.QueryParams["ListenerId"] = listenerID
	req.QueryParams["Certificates.1.Id"] = certID

	resp, err := client.ProcessCommonRequest(req)
	if err != nil {
		return fmt.Errorf("更新全球加速监听默认证书失败: %v", err)
	}

	var result map[string]interface{}
	if jsonErr := json.Unmarshal(resp.GetHttpContentBytes(), &result); jsonErr == nil {
		if code, ok := result["Code"].(string); ok && code != "" {
			return fmt.Errorf("更新全球加速监听默认证书失败: %s", code)
		}
	}

	p.Log("全球加速实例监听默认证书更新成功")
	return nil
}

func (p *AliyunGAProvider) deployAdditional(client *sdk.Client, gaID, listenerID, certID string, config map[string]interface{}) error {
	gaDomain := base.GetConfigString(config, "ga_domain")
	if gaDomain == "" {
		gaDomain = p.GetString("ga_domain")
	}
	if gaDomain == "" {
		return fmt.Errorf("扩展域名不能为空")
	}

	p.Log("正在查询全球加速监听扩展证书列表")

	listReq := requests.NewCommonRequest()
	listReq.Method = "POST"
	listReq.Domain = "ga.cn-hangzhou.aliyuncs.com"
	listReq.Version = "2019-11-20"
	listReq.ApiName = "ListListenerCertificates"
	listReq.QueryParams["Action"] = "ListListenerCertificates"
	listReq.QueryParams["RegionId"] = "cn-hangzhou"
	listReq.QueryParams["AcceleratorId"] = gaID
	listReq.QueryParams["ListenerId"] = listenerID

	listResp, err := client.ProcessCommonRequest(listReq)
	if err != nil {
		return fmt.Errorf("扩展域名列表查询失败: %v", err)
	}

	var listResult struct {
		Certificates []struct {
			Domain        string `json:"Domain"`
			CertificateId string `json:"CertificateId"`
		} `json:"Certificates"`
	}
	if err := json.Unmarshal(listResp.GetHttpContentBytes(), &listResult); err != nil {
		return fmt.Errorf("解析扩展证书列表失败: %v", err)
	}

	domains := strings.Split(gaDomain, ",")
	var needAdd []string

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}

		var domainExists bool
		var existCertID string
		for _, c := range listResult.Certificates {
			if c.Domain == domain {
				domainExists = true
				existCertID = c.CertificateId
				break
			}
		}

		if domainExists {
			if existCertID == certID {
				p.Log("全球加速实例监听扩展域名 " + domain + " 证书已配置")
				continue
			}
			req := requests.NewCommonRequest()
			req.Method = "POST"
			req.Domain = "ga.cn-hangzhou.aliyuncs.com"
			req.Version = "2019-11-20"
			req.ApiName = "UpdateAdditionalCertificateWithListener"
			req.QueryParams["Action"] = "UpdateAdditionalCertificateWithListener"
			req.QueryParams["RegionId"] = "cn-hangzhou"
			req.QueryParams["AcceleratorId"] = gaID
			req.QueryParams["ListenerId"] = listenerID
			req.QueryParams["Domain"] = domain
			req.QueryParams["CertificateId"] = certID

			if _, err := client.ProcessCommonRequest(req); err != nil {
				return fmt.Errorf("全球加速扩展域名 %s 替换证书失败: %v", domain, err)
			}
			p.Log("全球加速实例监听扩展域名 " + domain + " 替换证书成功")
		} else {
			needAdd = append(needAdd, domain)
		}
	}

	if len(needAdd) > 0 {
		req := requests.NewCommonRequest()
		req.Method = "POST"
		req.Domain = "ga.cn-hangzhou.aliyuncs.com"
		req.Version = "2019-11-20"
		req.ApiName = "AssociateAdditionalCertificatesWithListener"
		req.QueryParams["Action"] = "AssociateAdditionalCertificatesWithListener"
		req.QueryParams["RegionId"] = "cn-hangzhou"
		req.QueryParams["AcceleratorId"] = gaID
		req.QueryParams["ListenerId"] = listenerID

		for i, domain := range needAdd {
			idx := fmt.Sprintf("%d", i+1)
			req.QueryParams["Certificates."+idx+".Id"] = certID
			req.QueryParams["Certificates."+idx+".Domain"] = domain
		}

		if _, err := client.ProcessCommonRequest(req); err != nil {
			return fmt.Errorf("全球加速扩展域名绑定证书失败: %v", err)
		}
		p.Log("全球加速实例监听扩展域名 " + strings.Join(needAdd, ",") + " 绑定证书成功")
	}

	return nil
}

func (p *AliyunGAProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
