package providers

import (
	"context"
	"fmt"
	"strings"

	elb "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/elb/v3"
	elbModel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/elb/v3/model"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("huawei_elb", NewHuaweiELBProvider)
}

/* HuaweiELBProvider 华为云弹性负载均衡ELB证书部署器，按证书ID更新证书内容 */
type HuaweiELBProvider struct {
	base.BaseProvider
}

/* NewHuaweiELBProvider 创建华为云ELB部署器 */
func NewHuaweiELBProvider(config map[string]interface{}) base.DeployProvider {
	return &HuaweiELBProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

/* getClient 构建ELB客户端，endpoint 为 elb.{region_id}.myhuaweicloud.com */
func (p *HuaweiELBProvider) getClient(projectID, regionID string) (*elb.ElbClient, error) {
	accessKeyID, accessKeySecret := huaweiAccessKeys(&p.BaseProvider)
	credentials, err := huaweiCredentials(projectID, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, err
	}

	return elb.NewElbClient(
		elb.ElbClientBuilder().
			WithRegion(huaweiRegion(regionID, "elb."+regionID+".myhuaweicloud.com")).
			WithCredential(credentials).
			Build()), nil
}

/* Check 检查凭据与项目/区域配置 */
func (p *HuaweiELBProvider) Check(ctx context.Context) error {
	projectID := p.GetString("project_id")
	regionID := p.GetString("region_id")
	if projectID == "" {
		return fmt.Errorf("项目ID不能为空")
	}
	if regionID == "" {
		return fmt.Errorf("区域ID不能为空")
	}

	client, err := p.getClient(projectID, regionID)
	if err != nil {
		return err
	}

	_, err = client.ListCertificates(&elbModel.ListCertificatesRequest{})
	return err
}

/*
 * Deploy 按证书ID更新ELB证书内容
 */
func (p *HuaweiELBProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	projectID := base.GetConfigString(config, "project_id")
	regionID := base.GetConfigString(config, "region_id")
	certID := base.GetConfigString(config, "cert_id")
	if projectID == "" {
		return fmt.Errorf("项目ID不能为空")
	}
	if regionID == "" {
		return fmt.Errorf("区域ID不能为空")
	}
	if certID == "" {
		return fmt.Errorf("证书ID不能为空")
	}

	client, err := p.getClient(projectID, regionID)
	if err != nil {
		return err
	}

	detail, err := client.ShowCertificate(&elbModel.ShowCertificateRequest{CertificateId: certID})
	if err != nil {
		return fmt.Errorf("证书详情查询失败: %v", err)
	}
	if detail.Certificate != nil && strings.TrimSpace(detail.Certificate.Certificate) == strings.TrimSpace(fullchain) {
		p.Log(fmt.Sprintf("ELB证书ID %s 已存在，无需重复部署", certID))
		return nil
	}

	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("绑定的域名不能为空")
	}

	request := &elbModel.UpdateCertificateRequest{
		CertificateId: certID,
		Body: &elbModel.UpdateCertificateRequestBody{
			Certificate: &elbModel.UpdateCertificateOption{
				Certificate: &fullchain,
				PrivateKey:  &privateKey,
				Domain:      stringPtr(strings.Join(domains, ",")),
			},
		},
	}
	if _, err := client.UpdateCertificate(request); err != nil {
		return fmt.Errorf("ELB证书更新失败: %v", err)
	}

	p.Log(fmt.Sprintf("ELB证书ID %s 更新证书成功", certID))
	return nil
}

/* SetLogger 设置日志记录器 */
func (p *HuaweiELBProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
