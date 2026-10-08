package providers

import (
	"context"
	"fmt"
	"strings"

	scm "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/scm/v3"
	scmModel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/scm/v3/model"
	scmRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/scm/v3/region"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("huawei_upload", NewHuaweiUploadProvider)
}

/* HuaweiUploadProvider 华为云证书管理SCM上传部署器 */
type HuaweiUploadProvider struct {
	base.BaseProvider
}

/* NewHuaweiUploadProvider 创建华为云证书上传部署器 */
func NewHuaweiUploadProvider(config map[string]interface{}) base.DeployProvider {
	return &HuaweiUploadProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *HuaweiUploadProvider) getClient() (*scm.ScmClient, error) {
	accessKeyID, accessKeySecret := huaweiAccessKeys(&p.BaseProvider)
	credentials, err := huaweiCredentials("", accessKeyID, accessKeySecret)
	if err != nil {
		return nil, err
	}

	return scm.NewScmClient(
		scm.ScmClientBuilder().
			WithRegion(scmRegion.CN_NORTH_4).
			WithCredential(credentials).
			Build()), nil
}

/* Check 检查凭据有效性 */
func (p *HuaweiUploadProvider) Check(ctx context.Context) error {
	client, err := p.getClient()
	if err != nil {
		return err
	}

	_, err = client.ListCertificates(&scmModel.ListCertificatesRequest{})
	return err
}

/*
 * Deploy 上传证书到华为云证书管理并回传证书ID
 */
func (p *HuaweiUploadProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.DeployWithInfo(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 上传证书并把证书ID写入 info.Config，供下次部署复用 */
func (p *HuaweiUploadProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	certName, err := buildCertName(fullchain)
	if err != nil {
		return err
	}

	client, err := p.getClient()
	if err != nil {
		return err
	}

	request := &scmModel.ImportCertificateRequest{
		Body: &scmModel.ImportCertificateRequestBody{
			Name:        certName,
			Certificate: fullchain,
			PrivateKey:  privateKey,
		},
	}
	response, err := client.ImportCertificate(request)
	if err != nil {
		return fmt.Errorf("上传证书失败: %v", err)
	}
	if response.CertificateId == nil || strings.TrimSpace(*response.CertificateId) == "" {
		return fmt.Errorf("上传证书失败: 返回证书ID为空")
	}

	certID := *response.CertificateId
	p.Log("上传证书成功 certificate_id=" + certID)

	setDeployCertID(info, certID)

	return nil
}

/* SetLogger 设置日志记录器 */
func (p *HuaweiUploadProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
