package providers

import (
	"fmt"
	"strings"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	coreRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/region"

	"main/internal/cert/deploy/base"
)

/*
 * huaweiAccessKeys 读取华为云 AK/SK
 */
func huaweiAccessKeys(p *base.BaseProvider) (string, string) {
	accessKeyID := p.GetString("AccessKeyId")
	if accessKeyID == "" {
		accessKeyID = p.GetString("access_key")
	}
	accessKeySecret := p.GetString("SecretAccessKey")
	if accessKeySecret == "" {
		accessKeySecret = p.GetString("secret_key")
	}
	return accessKeyID, accessKeySecret
}

/*
 * huaweiCredentials 构建华为云基础凭据
 */
func huaweiCredentials(projectID, accessKeyID, accessKeySecret string) (*basic.Credentials, error) {
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("AccessKeyId/SecretAccessKey不能为空")
	}

	builder := basic.NewCredentialsBuilder().WithAk(accessKeyID).WithSk(accessKeySecret)
	if projectID != "" {
		builder = builder.WithProjectId(projectID)
	}

	credentials, err := builder.SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("华为云凭据初始化失败: %v", err)
	}
	return credentials, nil
}

/*
 * huaweiAccessKeysQuiet 读取华为云 AK/SK，缺失时返回错误
 * 供只需校验凭据完整性的 Check 使用
 */
func huaweiAccessKeysQuiet(p *base.BaseProvider) (string, string, error) {
	accessKeyID, accessKeySecret := huaweiAccessKeys(p)
	if accessKeyID == "" || accessKeySecret == "" {
		return "", "", fmt.Errorf("AccessKeyId/SecretAccessKey不能为空")
	}
	return accessKeyID, accessKeySecret, nil
}

/*
 * huaweiEndpoint 把 endpoint 归一化为带 scheme 的形式
 * SDK 只在走 region 时补 scheme，直接用 WithEndpoint 时需要自行补全
 */
func huaweiEndpoint(endpoint string) string {
	if endpoint == "" || strings.HasPrefix(endpoint, "http") {
		return endpoint
	}
	return "https://" + endpoint
}

/* stringPtr 返回字符串指针，便于构造 SDK 的可选字段 */
func stringPtr(value string) *string {
	return &value
}

/*
 * huaweiRegion 构造指向指定 endpoint 的 region
 * ELB/WAF 按地域分服，需要通过「服务名.区域ID.myhuaweicloud.com」覆盖默认 endpoint，
 * 且路径含 {project_id}，必须由调用方传入 projectId 以免 SDK 自动查询项目
 */
func huaweiRegion(regionID, endpoint string) *coreRegion.Region {
	return coreRegion.NewRegion(regionID, huaweiEndpoint(endpoint))
}
