package providers

import (
	"context"
	"fmt"
	"strings"

	waf "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/waf/v1"
	wafModel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/waf/v1/model"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("huawei_waf", NewHuaweiWAFProvider)
}

/* HuaweiWAFProvider 华为云Web应用防火墙WAF证书部署器，按证书ID更新证书内容 */
type HuaweiWAFProvider struct {
	base.BaseProvider
}

/* NewHuaweiWAFProvider 创建华为云WAF部署器 */
func NewHuaweiWAFProvider(config map[string]interface{}) base.DeployProvider {
	return &HuaweiWAFProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

/* getClient 构建WAF客户端，endpoint 为 waf.{region_id}.myhuaweicloud.com */
func (p *HuaweiWAFProvider) getClient(projectID, regionID string) (*waf.WafClient, error) {
	accessKeyID, accessKeySecret := huaweiAccessKeys(&p.BaseProvider)
	credentials, err := huaweiCredentials(projectID, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, err
	}

	return waf.NewWafClient(
		waf.WafClientBuilder().
			WithRegion(huaweiRegion(regionID, "waf."+regionID+".myhuaweicloud.com")).
			WithCredential(credentials).
			Build()), nil
}

/* Check 检查凭据与项目/区域配置 */
func (p *HuaweiWAFProvider) Check(ctx context.Context) error {
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

	_, err = client.ListCertificates(&wafModel.ListCertificatesRequest{})
	return err
}

func (p *HuaweiWAFProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
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

	detail, err := client.ShowCertificate(&wafModel.ShowCertificateRequest{CertificateId: certID})
	if err != nil {
		return fmt.Errorf("证书详情查询失败: %v", err)
	}
	if detail.Content != nil && strings.TrimSpace(*detail.Content) == strings.TrimSpace(fullchain) {
		p.Log(fmt.Sprintf("WAF证书ID %s 已存在，无需重复部署", certID))
		return nil
	}

	certName, err := buildCertName(fullchain)
	if err != nil {
		return err
	}

	request := &wafModel.UpdateCertificateRequest{
		CertificateId: certID,
		Body: &wafModel.UpdateCertificateRequestBody{
			Name:    certName,
			Content: &fullchain,
			Key:     &privateKey,
		},
	}
	if _, err := client.UpdateCertificate(request); err != nil {
		return fmt.Errorf("WAF证书更新失败: %v", err)
	}

	p.Log(fmt.Sprintf("WAF证书ID %s 更新证书成功", certID))
	return nil
}

/* SetLogger 设置日志记录器 */
func (p *HuaweiWAFProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
