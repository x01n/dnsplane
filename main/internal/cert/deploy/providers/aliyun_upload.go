package providers

import (
	"context"
	"errors"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("aliyun_upload", NewAliyunUploadProvider)
}

/*
 * AliyunUploadProvider 仅上传证书到证书管理，不下发到任何云产品
 */
type AliyunUploadProvider struct {
	AliyunProviderBase
}

func NewAliyunUploadProvider(config map[string]interface{}) base.DeployProvider {
	return &AliyunUploadProvider{
		AliyunProviderBase: AliyunProviderBase{BaseProvider: base.BaseProvider{Config: config}},
	}
}

/* Deploy 仅上传证书，不回传附加信息 */
func (p *AliyunUploadProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/* DeployWithInfo 上传证书并回传 CertId 与证书名称 */
func (p *AliyunUploadProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

func (p *AliyunUploadProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	if fullchain == "" || privateKey == "" {
		return errors.New("证书或私钥内容不存在")
	}

	certID, certName, err := p.getAliyunCertID(config, fullchain, privateKey)
	if err != nil {
		return err
	}

	p.Log("证书已上传到证书管理服务，CertId=" + certID)
	fillAliyunInfo(info, certID, certName)
	return nil
}

func (p *AliyunUploadProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

/* 确保上传部署器满足 DeployWithInfoProvider 接口 */
var _ base.DeployWithInfoProvider = (*AliyunUploadProvider)(nil)
