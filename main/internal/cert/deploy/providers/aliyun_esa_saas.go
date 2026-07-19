package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"main/internal/cert"
	"main/internal/cert/deploy/base"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

func init() {
	base.Register("aliyun_esa_saas", NewAliyunESASaaSProvider)
}

type AliyunESASaaSProvider struct {
	base.BaseProvider
}

func NewAliyunESASaaSProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunESASaaSProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *AliyunESASaaSProvider) getClient(region string) (*sdk.Client, error) {
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

func (p *AliyunESASaaSProvider) getEndpoint(region string) string {
	if region == "ap-southeast-1" {
		return "esa.ap-southeast-1.aliyuncs.com"
	}
	return "esa.cn-hangzhou.aliyuncs.com"
}

func (p *AliyunESASaaSProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	sitename := base.GetConfigString(config, "esa_sitename")
	if sitename == "" {
		sitename = p.GetString("esa_sitename")
	}
	if sitename == "" {
		return fmt.Errorf("ESA站点名称不能为空")
	}

	saasSitename := base.GetConfigString(config, "esa_saas_sitename")
	if saasSitename == "" {
		saasSitename = p.GetString("esa_saas_sitename")
	}
	if saasSitename == "" {
		return fmt.Errorf("ESA SAAS域名不能为空")
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

	endpoint := p.getEndpoint(region)

	p.Log("正在查询ESA站点: " + sitename)

	listReq := requests.NewCommonRequest()
	listReq.Method = "GET"
	listReq.Domain = endpoint
	listReq.Version = "2024-09-10"
	listReq.ApiName = "ListSites"
	listReq.QueryParams["Action"] = "ListSites"
	listReq.QueryParams["SiteName"] = sitename
	listReq.QueryParams["SiteSearchType"] = "exact"

	listResp, err := client.ProcessCommonRequest(listReq)
	if err != nil {
		return fmt.Errorf("查询ESA站点列表失败: %v", err)
	}

	var siteResult struct {
		TotalCount int `json:"TotalCount"`
		Sites      []struct {
			SiteId int64 `json:"SiteId"`
		} `json:"Sites"`
	}
	if err := json.Unmarshal(listResp.GetHttpContentBytes(), &siteResult); err != nil {
		return fmt.Errorf("解析ESA站点列表失败: %v", err)
	}
	if siteResult.TotalCount == 0 {
		return fmt.Errorf("ESA站点 %s 不存在", sitename)
	}

	siteID := fmt.Sprintf("%d", siteResult.Sites[0].SiteId)
	p.Log(fmt.Sprintf("成功查询到ESA站点, SiteId=%s", siteID))

	p.Log("正在查询SAAS自定义域名: " + saasSitename)

	hostnameReq := requests.NewCommonRequest()
	hostnameReq.Method = "GET"
	hostnameReq.Domain = endpoint
	hostnameReq.Version = "2024-09-10"
	hostnameReq.ApiName = "ListCustomHostnames"
	hostnameReq.QueryParams["Action"] = "ListCustomHostnames"
	hostnameReq.QueryParams["SiteName"] = saasSitename
	hostnameReq.QueryParams["SiteId"] = siteID
	hostnameReq.QueryParams["SiteSearchType"] = "exact"

	hostnameResp, err := client.ProcessCommonRequest(hostnameReq)
	if err != nil {
		return fmt.Errorf("查询ESA SAAS域名失败: %v", err)
	}

	var hostnameResult struct {
		TotalCount int `json:"TotalCount"`
		Hostnames  []struct {
			HostnameId string `json:"HostnameId"`
		} `json:"Hostnames"`
	}
	if err := json.Unmarshal(hostnameResp.GetHttpContentBytes(), &hostnameResult); err != nil {
		return fmt.Errorf("解析ESA SAAS域名列表失败: %v", err)
	}
	if hostnameResult.TotalCount == 0 {
		return fmt.Errorf("ESA SAAS站点 %s 不存在", saasSitename)
	}

	hostnameID := hostnameResult.Hostnames[0].HostnameId
	p.Log("正在部署证书到ESA SAAS自定义域名, HostnameId=" + hostnameID)

	deployReq := requests.NewCommonRequest()
	deployReq.Method = "POST"
	deployReq.Domain = endpoint
	deployReq.Version = "2024-09-10"
	deployReq.ApiName = "UpdateCustomHostname"
	deployReq.QueryParams["Action"] = "UpdateCustomHostname"
	deployReq.QueryParams["HostnameId"] = hostnameID
	deployReq.QueryParams["SslFlag"] = "on"
	deployReq.QueryParams["CertType"] = "cas"
	deployReq.QueryParams["CasId"] = certID
	deployReq.QueryParams["CasRegion"] = region

	resp, err := client.ProcessCommonRequest(deployReq)
	if err != nil {
		return fmt.Errorf("ESA SAAS站点部署失败: %v", err)
	}

	var deployResult map[string]interface{}
	if jsonErr := json.Unmarshal(resp.GetHttpContentBytes(), &deployResult); jsonErr == nil {
		if code, ok := deployResult["Code"].(string); ok && code != "" {
			return fmt.Errorf("ESA SAAS站点部署失败: %s", code)
		}
	}

	p.Log("ESA SAAS站点 " + saasSitename + " 证书部署成功")
	return nil
}

func (p *AliyunESASaaSProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
