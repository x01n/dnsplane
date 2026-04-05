package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert"
	"main/internal/cert/deploy/base"
	"net/http"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	ssl "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ssl/v20191205"
)

// sha256Hex 计算SHA256并返回十六进制字符串
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:])
}

// tencentHmacSHA256 计算HMAC-SHA256（腾讯云签名专用）
func tencentHmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func init() {
	// 注册统一的腾讯云部署器，支持CDN/EO/CLB/COS等全部产品
	base.Register("tencent_cdn", NewTencentProvider)
	base.Register("tencent_teo", NewTencentProvider)
	base.Register("tencent_clb", NewTencentProvider)
	base.Register("tencent_cos", NewTencentProvider)
	base.Register("tencent_waf", NewTencentProvider)
	base.Register("tencent_live", NewTencentProvider)
	base.Register("tencent_vod", NewTencentProvider)
	base.Register("tencent_tke", NewTencentProvider)
	base.Register("tencent_scf", NewTencentProvider)
	base.Register("tencent_upload", NewTencentProvider)
}

// TencentProvider 腾讯云统一证书部署器
// 参考 dnsmgr 实现：上传证书后使用 SSL 服务的 DeployCertificateInstance API 部署
type TencentProvider struct {
	base.BaseProvider
}

func NewTencentProvider(config map[string]interface{}) base.DeployProvider {
	return &TencentProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *TencentProvider) getSSLClient(region string) (*ssl.Client, error) {
	secretID := p.GetString("SecretId")
	secretKey := p.GetString("SecretKey")
	credential := common.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	return ssl.NewClient(credential, region, cpf)
}

func (p *TencentProvider) Check(ctx context.Context) error {
	secretID := p.GetString("SecretId")
	secretKey := p.GetString("SecretKey")

	if secretID == "" || secretKey == "" {
		return fmt.Errorf("SecretId/SecretKey不能为空")
	}

	client, err := p.getSSLClient("")
	if err != nil {
		return err
	}

	request := ssl.NewDescribeCertificatesRequest()
	request.Limit = common.Uint64Ptr(1)
	_, err = client.DescribeCertificates(request)
	return err
}

func (p *TencentProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	product := base.GetConfigString(config, "product")
	if product == "" {
		product = "cdn" // 默认CDN
	}

	// upload 模式：仅上传证书不部署
	if product == "upload" {
		_, err := p.uploadCert(fullchain, privateKey)
		return err
	}

	// EO(EdgeOne) 使用专用部署方式
	if product == "teo" {
		return p.deployTEO(fullchain, privateKey, config)
	}

	// CLB 可选使用专用 CLB API（如有 clb_listener_id 指定）
	if product == "clb" {
		return p.deployCLB(fullchain, privateKey, config)
	}

	// 通用部署：使用 SSL 服务的 DeployCertificateInstance API
	return p.deployCommon(fullchain, privateKey, product, config)
}

// uploadCert 上传证书到腾讯云 SSL 证书服务
func (p *TencentProvider) uploadCert(fullchain, privateKey string) (string, error) {
	client, err := p.getSSLClient("")
	if err != nil {
		return "", err
	}

	certName := fmt.Sprintf("dnsplane-%d", time.Now().Unix())

	request := ssl.NewUploadCertificateRequest()
	request.CertificatePublicKey = common.StringPtr(fullchain)
	request.CertificatePrivateKey = common.StringPtr(privateKey)
	request.Alias = common.StringPtr(certName)
	repeatable := false
	request.Repeatable = &repeatable

	response, err := client.UploadCertificate(request)
	if err != nil {
		return "", fmt.Errorf("上传证书失败: %v", err)
	}

	certID := *response.Response.CertificateId
	p.Log(fmt.Sprintf("上传证书成功 CertificateId=%s", certID))

	// 等待证书处理
	time.Sleep(300 * time.Millisecond)

	return certID, nil
}

// deployCommon 通用部署：通过 SSL 服务 DeployCertificateInstance API
// 支持: cdn, waf, live, vod, ddos, cos, tke 等产品
func (p *TencentProvider) deployCommon(fullchain, privateKey, product string, config map[string]interface{}) error {
	p.Log("正在上传证书到腾讯云SSL")
	certID, err := p.uploadCert(fullchain, privateKey)
	if err != nil {
		return err
	}

	// 构建 InstanceIdList
	instanceIDs, err := p.buildInstanceIDs(product, config)
	if err != nil {
		return err
	}

	// CDN 的 InstanceId 格式: domain|on (开启HTTPS)
	if product == "cdn" {
		for i, id := range instanceIDs {
			if !strings.Contains(id, "|") {
				instanceIDs[i] = id + "|on"
			}
		}
	}

	// 确定 region
	region := ""
	if product == "cos" || product == "tke" || product == "waf" || product == "scf" {
		region = base.GetConfigString(config, "regionid")
		if region == "" {
			region = base.GetConfigString(config, "region")
		}
	}

	client, err := p.getSSLClient(region)
	if err != nil {
		return err
	}

	p.Log(fmt.Sprintf("正在部署证书到腾讯云%s: %s", strings.ToUpper(product), strings.Join(instanceIDs, ", ")))

	request := ssl.NewDeployCertificateInstanceRequest()
	request.CertificateId = common.StringPtr(certID)
	request.ResourceType = common.StringPtr(product)
	request.InstanceIdList = common.StringPtrs(instanceIDs)
	if product == "live" {
		request.Status = common.Int64Ptr(1)
	}

	response, err := client.DeployCertificateInstance(request)
	if err != nil {
		return fmt.Errorf("腾讯云%s部署失败: %v", strings.ToUpper(product), err)
	}

	// 记录部署记录ID
	if response.Response != nil && response.Response.DeployRecordId != nil {
		recordID := *response.Response.DeployRecordId
		p.Log(fmt.Sprintf("部署任务已提交 DeployRecordId=%d", recordID))

		// 查询部署结果
		p.queryDeployResult(client, uint64(recordID))
	}

	p.Log(fmt.Sprintf("腾讯云%s证书部署成功", strings.ToUpper(product)))
	return nil
}

// buildInstanceIDs 根据产品类型构建实例ID列表
func (p *TencentProvider) buildInstanceIDs(product string, config map[string]interface{}) ([]string, error) {
	switch product {
	case "cos":
		regionID := base.GetConfigString(config, "regionid")
		bucket := base.GetConfigString(config, "cos_bucket")
		domain := base.GetConfigString(config, "domain")
		if regionID == "" || bucket == "" || domain == "" {
			return nil, fmt.Errorf("COS部署需要填写所属地域ID、存储桶名称、绑定域名")
		}
		return []string{regionID + "|" + bucket + "|" + domain}, nil

	case "tke":
		regionID := base.GetConfigString(config, "regionid")
		clusterID := base.GetConfigString(config, "tke_cluster_id")
		namespace := base.GetConfigString(config, "tke_namespace")
		secret := base.GetConfigString(config, "tke_secret")
		if regionID == "" || clusterID == "" || namespace == "" || secret == "" {
			return nil, fmt.Errorf("TKE部署需要填写地域ID、集群ID、命名空间、secret名称")
		}
		return []string{clusterID + "|" + namespace + "|" + secret}, nil

	case "ddos":
		instanceID := base.GetConfigString(config, "lighthouse_id")
		domain := base.GetConfigString(config, "domain")
		if instanceID == "" || domain == "" {
			return nil, fmt.Errorf("DDoS部署需要填写实例ID和域名")
		}
		return []string{instanceID + "|" + domain + "|443"}, nil

	default:
		// cdn, waf, live, vod, scf 等：使用域名作为 instanceId
		domain := base.GetConfigString(config, "domain")
		if domain == "" {
			// 从 domainList 获取
			domains := base.GetConfigDomains(config)
			if len(domains) > 0 {
				domain = strings.Join(domains, ",")
			}
		}
		if domain == "" {
			return nil, fmt.Errorf("绑定的域名不能为空")
		}

		// 支持逗号分隔的多域名
		if strings.Contains(domain, ",") {
			parts := strings.Split(domain, ",")
			var result []string
			for _, d := range parts {
				d = strings.TrimSpace(d)
				if d != "" {
					result = append(result, d)
				}
			}
			return result, nil
		}
		return []string{domain}, nil
	}
}

// queryDeployResult 查询部署结果
func (p *TencentProvider) queryDeployResult(client *ssl.Client, recordID uint64) {
	request := ssl.NewDescribeHostDeployRecordDetailRequest()
	request.DeployRecordId = common.StringPtr(fmt.Sprintf("%d", recordID))

	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Second)
		response, err := client.DescribeHostDeployRecordDetail(request)
		if err != nil {
			p.Log(fmt.Sprintf("查询部署记录失败: %v", err))
			return
		}
		if response.Response == nil {
			continue
		}

		respJSON, _ := json.Marshal(response.Response)
		var result map[string]interface{}
		if err := json.Unmarshal(respJSON, &result); err != nil {
			p.Log(fmt.Sprintf("部署状态响应解析失败: %v", err))
			continue
		}

		successCount, _ := result["SuccessTotalCount"].(float64)
		runningCount, _ := result["RunningTotalCount"].(float64)
		failedCount, _ := result["FailedTotalCount"].(float64)

		if successCount >= 1 || runningCount >= 1 {
			p.Log(fmt.Sprintf("部署状态: 成功=%d 执行中=%d", int(successCount), int(runningCount)))
			return
		}
		if failedCount >= 1 {
			if details, ok := result["DeployRecordDetailList"].([]interface{}); ok && len(details) > 0 {
				if detail, ok := details[0].(map[string]interface{}); ok {
					if errMsg, ok := detail["ErrorMsg"].(string); ok {
						p.Log(fmt.Sprintf("部署失败原因: %s", errMsg))
					}
				}
			}
			return
		}
	}
}

// deployTEO 部署到腾讯云 EdgeOne (EO)
// 参考 dnsmgr: 先上传证书到 SSL 获取 CertId，再调用 TEO API ModifyHostsCertificate
func (p *TencentProvider) deployTEO(fullchain, privateKey string, config map[string]interface{}) error {
	siteID := base.GetConfigString(config, "site_id")
	domain := base.GetConfigString(config, "domain")
	if siteID == "" {
		return fmt.Errorf("站点ID不能为空")
	}
	if domain == "" {
		domains := base.GetConfigDomains(config)
		if len(domains) > 0 {
			domain = strings.Join(domains, ",")
		}
	}
	if domain == "" {
		return fmt.Errorf("绑定的域名不能为空")
	}

	p.Log("正在上传证书到腾讯云SSL")
	certID, err := p.uploadCert(fullchain, privateKey)
	if err != nil {
		return err
	}

	hosts := strings.Split(domain, ",")
	for i := range hosts {
		hosts[i] = strings.TrimSpace(hosts[i])
	}

	p.Log(fmt.Sprintf("正在部署证书到腾讯云EdgeOne: %s (ZoneId=%s)", domain, siteID))

	// 构建 TEO API ModifyHostsCertificate 请求
	// 参考 dnsmgr: 使用 teo.tencentcloudapi.com 的 ModifyHostsCertificate
	hostsJSON := make([]string, len(hosts))
	for i, h := range hosts {
		hostsJSON[i] = fmt.Sprintf(`"%s"`, h)
	}

	reqBody := fmt.Sprintf(`{
		"ZoneId": "%s",
		"Hosts": [%s],
		"Mode": "sslcert",
		"ServerCertInfo": [{"CertId": "%s"}]
	}`, siteID, strings.Join(hostsJSON, ","), certID)

	secretID := p.GetString("SecretId")
	secretKey := p.GetString("SecretKey")

	err = p.callTencentAPI("teo.tencentcloudapi.com", "teo", "2022-09-01",
		"ModifyHostsCertificate", reqBody, secretID, secretKey)
	if err != nil {
		return fmt.Errorf("EdgeOne部署失败: %v", err)
	}

	p.Log("腾讯云EdgeOne证书部署成功")
	return nil
}

// callTencentAPI 通用腾讯云 API v3 签名调用
func (p *TencentProvider) callTencentAPI(host, service, version, action, body, secretID, secretKey string) error {
	now := time.Now().UTC()
	timestamp := fmt.Sprintf("%d", now.Unix())
	date := now.Format("2006-01-02")

	// 1. 构建规范请求
	hashedBody := sha256Hex(body)
	canonicalRequest := fmt.Sprintf("POST\n/\n\ncontent-type:application/json\nhost:%s\n\ncontent-type;host\n%s",
		host, hashedBody)

	// 2. 构建签名字符串
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	stringToSign := fmt.Sprintf("TC3-HMAC-SHA256\n%s\n%s\n%s",
		timestamp, credentialScope, sha256Hex(canonicalRequest))

	// 3. 计算签名
	secretDate := tencentHmacSHA256([]byte("TC3"+secretKey), date)
	secretService := tencentHmacSHA256(secretDate, service)
	secretSigning := tencentHmacSHA256(secretService, "tc3_request")
	signature := fmt.Sprintf("%x", tencentHmacSHA256(secretSigning, stringToSign))

	// 4. 构建 Authorization
	authorization := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=content-type;host, Signature=%s",
		secretID, credentialScope, signature)

	// 5. 发送请求
	req, err := http.NewRequest("POST", "https://"+host, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", host)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", version)
	req.Header.Set("X-TC-Timestamp", timestamp)
	req.Header.Set("Authorization", authorization)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	if response, ok := result["Response"].(map[string]interface{}); ok {
		if errInfo, ok := response["Error"].(map[string]interface{}); ok {
			code, _ := errInfo["Code"].(string)
			message, _ := errInfo["Message"].(string)
			return fmt.Errorf("[%s] %s", code, message)
		}
	}

	return nil
}

// deployCLB 部署到腾讯云负载均衡
func (p *TencentProvider) deployCLB(fullchain, privateKey string, config map[string]interface{}) error {
	regionID := base.GetConfigString(config, "regionid")
	clbID := base.GetConfigString(config, "clb_id")
	if regionID == "" {
		return fmt.Errorf("所属地域ID不能为空")
	}
	if clbID == "" {
		return fmt.Errorf("负载均衡ID不能为空")
	}

	p.Log("正在上传证书到腾讯云SSL")
	certID, err := p.uploadCert(fullchain, privateKey)
	if err != nil {
		return err
	}

	// 使用 SSL 统一部署接口
	client, err := p.getSSLClient(regionID)
	if err != nil {
		return err
	}

	listenerID := base.GetConfigString(config, "clb_listener_id")
	clbDomain := base.GetConfigString(config, "clb_domain")

	// 构建 instance ID
	instanceID := clbID
	if listenerID != "" {
		instanceID = clbID + "|" + listenerID
		if clbDomain != "" {
			instanceID = instanceID + "|" + clbDomain
		}
	}

	p.Log(fmt.Sprintf("正在部署证书到腾讯云CLB: %s", instanceID))

	request := ssl.NewDeployCertificateInstanceRequest()
	request.CertificateId = common.StringPtr(certID)
	request.ResourceType = common.StringPtr("clb")
	request.InstanceIdList = common.StringPtrs([]string{instanceID})

	_, err = client.DeployCertificateInstance(request)
	if err != nil {
		return fmt.Errorf("腾讯云CLB部署失败: %v", err)
	}

	p.Log("腾讯云CLB证书部署成功")
	return nil
}

func (p *TencentProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
