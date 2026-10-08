package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	ssl "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ssl/v20191205"

	"main/internal/cert/deploy/base"
)

var tencentRegionResourceTypes = []string{"clb", "tke", "apigateway", "waf", "tcb", "tse", "cos", "mqtt", "scf", "tdmq"}

/* sleepWithContext 等待指定时长，期间响应 ctx 取消 */
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

/* setDeployCertID 把新的证书ID回写到部署任务配置，info 为空时忽略 */
func setDeployCertID(info *base.DeployInfo, certID string) {
	if info == nil || certID == "" {
		return
	}
	if info.Config == nil {
		info.Config = map[string]interface{}{}
	}
	info.Config["cert_id"] = certID
}

/*
 * DeployWithInfo 部署证书并回传附加信息
 * update 与 update_new 的入口，Deploy 也会委托到此处，因此两条路径语义一致
 */
func (p *TencentProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	switch base.GetConfigString(config, "product") {
	case "update":
		return p.updateCertInPlace(ctx, fullchain, privateKey, config)
	case "update_new":
		return p.updateCertNew(ctx, fullchain, privateKey, config, info)
	default:
		return p.Deploy(ctx, fullchain, privateKey, config)
	}
}

/*
 * queryTencentBindResources 创建关联云资源查询任务并轮询结果
 * 返回 nil 表示轮询超时（update 视为错误，update_new 仅上传新证书）
 */
func (p *TencentProvider) queryTencentBindResources(ctx context.Context, certID string, maxRetry int, logPoll bool) ([]*ssl.BindResourceResult, error) {
	client, err := p.getSSLClient("")
	if err != nil {
		return nil, err
	}

	createRequest := ssl.NewCreateCertificateBindResourceSyncTaskRequest()
	createRequest.CertificateIds = common.StringPtrs([]string{certID})
	createRequest.IsCache = common.Uint64Ptr(1)
	createResponse, err := client.CreateCertificateBindResourceSyncTask(createRequest)
	if err != nil {
		return nil, fmt.Errorf("创建关联云资源查询任务失败: %v", err)
	}
	if createResponse.Response == nil || len(createResponse.Response.CertTaskIds) == 0 ||
		createResponse.Response.CertTaskIds[0] == nil || createResponse.Response.CertTaskIds[0].TaskId == nil {
		return nil, fmt.Errorf("创建关联云资源查询任务失败: 返回任务ID为空")
	}
	taskID := *createResponse.Response.CertTaskIds[0].TaskId
	p.Log("创建关联云资源查询任务成功 TaskId=" + taskID)

	for retry := 1; retry <= maxRetry; retry++ {
		if err := sleepWithContext(ctx, 2*time.Second); err != nil {
			return nil, err
		}

		request := ssl.NewDescribeCertificateBindResourceTaskResultRequest()
		request.TaskIds = common.StringPtrs([]string{taskID})
		response, err := client.DescribeCertificateBindResourceTaskResult(request)
		if err != nil {
			return nil, fmt.Errorf("查询关联云资源任务结果失败: %v", err)
		}
		if response.Response == nil || len(response.Response.SyncTaskBindResourceResult) == 0 ||
			response.Response.SyncTaskBindResourceResult[0] == nil {
			return nil, fmt.Errorf("查询关联云资源任务结果失败: 返回结果为空")
		}

		result := response.Response.SyncTaskBindResourceResult[0]
		status := uint64(0)
		if result.Status != nil {
			status = *result.Status
		}
		if logPoll {
			p.Log(fmt.Sprintf("查询关联云资源结果轮询中... 第%d次，状态=%d", retry, status))
		}

		if status == 1 {
			return result.BindResourceResult, nil
		}
		if status == 2 {
			message := "未知错误"
			if result.Error != nil && result.Error.Message != nil {
				message = *result.Error.Message
			}
			return nil, fmt.Errorf("关联云资源查询任务执行失败: %s", message)
		}
	}

	return nil, nil
}

/*
 * collectTencentResourceTypes 从关联云资源结果中提取存在实例的资源类型
 * onlyCLB 为 true 时仅收集 clb（update 语义），否则收集全部类型并按需附带地域（update_new 语义）
 */
func collectTencentResourceTypes(results []*ssl.BindResourceResult, onlyCLB bool) ([]string, []*ssl.ResourceTypeRegions) {
	var resourceTypes []string
	var resourceTypesRegions []*ssl.ResourceTypeRegions

	for _, result := range results {
		if result == nil || result.ResourceType == nil {
			continue
		}
		resourceType := *result.ResourceType
		if onlyCLB && resourceType != "clb" {
			continue
		}

		totalCount := uint64(0)
		var regions []string
		for _, regionResult := range result.BindResourceRegionResult {
			if regionResult == nil || regionResult.TotalCount == nil || *regionResult.TotalCount == 0 {
				continue
			}
			totalCount += *regionResult.TotalCount
			if regionResult.Region != nil && *regionResult.Region != "" {
				regions = append(regions, *regionResult.Region)
			}
		}
		if totalCount == 0 {
			continue
		}

		resourceTypes = append(resourceTypes, resourceType)
		if len(regions) > 0 && slices.Contains(tencentRegionResourceTypes, resourceType) {
			resourceTypesRegions = append(resourceTypesRegions, &ssl.ResourceTypeRegions{
				ResourceType: common.StringPtr(resourceType),
				Regions:      common.StringPtrs(regions),
			})
		}
	}

	return resourceTypes, resourceTypesRegions
}

/*
 * updateCertInPlace 更新证书内容且证书ID保持不变
 */
func (p *TencentProvider) updateCertInPlace(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	certID := base.GetConfigString(config, "cert_id")
	if certID == "" {
		return fmt.Errorf("证书ID不能为空")
	}

	results, err := p.queryTencentBindResources(ctx, certID, 30, false)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("关联云资源查询任务超时未完成，请稍后重试")
	}

	resourceTypes, resourceTypesRegions := collectTencentResourceTypes(results, true)

	client, err := p.getSSLClient("")
	if err != nil {
		return err
	}

	request := ssl.NewUploadUpdateCertificateInstanceRequest()
	request.OldCertificateId = common.StringPtr(certID)
	request.CertificatePublicKey = common.StringPtr(fullchain)
	request.CertificatePrivateKey = common.StringPtr(privateKey)
	request.ResourceTypes = common.StringPtrs(resourceTypes)
	request.ResourceTypesRegions = resourceTypesRegions

	for retry := 1; retry <= 10; retry++ {
		response, err := client.UploadUpdateCertificateInstance(request)
		if err != nil {
			return fmt.Errorf("更新证书内容失败: %v", err)
		}
		if response.Response != nil && response.Response.DeployStatus != nil && *response.Response.DeployStatus == 1 {
			break
		}
		if err := sleepWithContext(ctx, time.Second); err != nil {
			return err
		}
	}

	p.Log("更新证书内容成功，可能需要一些时间完成各资源的证书更新部署")
	return nil
}

/*
 * updateCertNew 生成新的证书ID并迁移旧证书绑定的云资源，可选删除旧证书
 */
func (p *TencentProvider) updateCertNew(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	oldCertID := base.GetConfigString(config, "cert_id")
	if oldCertID == "" {
		return fmt.Errorf("证书ID不能为空")
	}

	results, err := p.queryTencentBindResources(ctx, oldCertID, 60, true)
	if err != nil {
		return err
	}
	if results == nil {
		p.Log("关联云资源查询任务超时（120秒），将跳过资源查询，仅上传新证书")
	}

	resourceTypes, resourceTypesRegions := collectTencentResourceTypes(results, false)
	if len(resourceTypes) == 0 {
		p.Log("未检测到该证书关联的云资源实例，将仅上传新证书")
		newCertID := p.uploadTencentNewCert(ctx, fullchain, privateKey, info)
		p.deleteTencentOldCert(config, oldCertID, newCertID)
		return nil
	}

	p.Log("发现关联云资源类型: " + strings.Join(resourceTypes, ", "))

	client, err := p.getSSLClient("")
	if err != nil {
		return err
	}

	request := ssl.NewUpdateCertificateInstanceRequest()
	request.OldCertificateId = common.StringPtr(oldCertID)
	request.CertificatePublicKey = common.StringPtr(fullchain)
	request.CertificatePrivateKey = common.StringPtr(privateKey)
	request.ResourceTypes = common.StringPtrs(resourceTypes)
	request.ResourceTypesRegions = resourceTypesRegions

	// 直接传证书内容由接口内部处理，避免单独上传的证书ID匹配不上
	p.logUpdateInstanceParams(oldCertID, resourceTypes, resourceTypesRegions)

	newCertID := ""
	for retry := 1; retry <= 10; retry++ {
		response, err := client.UpdateCertificateInstance(request)
		if err != nil {
			return fmt.Errorf("一键更新旧证书资源失败: %v", err)
		}
		if response.Response != nil && response.Response.DeployRecordId != nil && *response.Response.DeployRecordId > 0 {
			p.Log(fmt.Sprintf("一键更新任务创建成功 DeployRecordId=%d", *response.Response.DeployRecordId))
			// 查询新生成的证书ID，用于回写到本地数据库
			newCertID = p.findNewCertID(fullchain)
			break
		}
		if err := sleepWithContext(ctx, 2*time.Second); err != nil {
			return err
		}
	}

	if newCertID != "" {
		setDeployCertID(info, newCertID)
		p.Log("证书ID已更新为: " + newCertID)
	}

	p.deleteTencentOldCert(config, oldCertID, newCertID)

	p.Log("一键更新旧证书资源已提交，可能需要一些时间完成各资源的证书更新部署")
	return nil
}

/* logUpdateInstanceParams 输出已隐藏证书内容的更新请求参数 */
func (p *TencentProvider) logUpdateInstanceParams(oldCertID string, resourceTypes []string, resourceTypesRegions []*ssl.ResourceTypeRegions) {
	params := map[string]interface{}{
		"OldCertificateId":      oldCertID,
		"CertificatePublicKey":  "***",
		"CertificatePrivateKey": "***",
		"ResourceTypes":         resourceTypes,
	}
	if len(resourceTypesRegions) > 0 {
		params["ResourceTypesRegions"] = resourceTypesRegions
	}
	if encoded, err := json.Marshal(params); err == nil {
		p.Log("UpdateCertificateInstance 请求参数(已隐藏证书内容): " + string(encoded))
	}
}

/*
 * uploadTencentNewCert 仅上传新证书并关闭到期提醒，返回新证书ID
 */
func (p *TencentProvider) uploadTencentNewCert(ctx context.Context, fullchain, privateKey string, info *base.DeployInfo) string {
	certName, err := buildCertName(fullchain)
	if err != nil {
		p.Log("上传新证书失败: " + err.Error())
		return ""
	}

	client, err := p.getSSLClient("")
	if err != nil {
		p.Log("上传新证书失败: " + err.Error())
		return ""
	}
	request := ssl.NewUploadCertificateRequest()
	request.CertificatePublicKey = common.StringPtr(fullchain)
	request.CertificatePrivateKey = common.StringPtr(privateKey)
	request.CertificateType = common.StringPtr("SVR")
	request.Alias = common.StringPtr(certName)
	request.Repeatable = common.BoolPtr(true)

	response, err := client.UploadCertificate(request)
	if err != nil {
		p.Log("上传新证书失败: " + err.Error())
		return ""
	}
	if response.Response == nil || response.Response.CertificateId == nil {
		p.Log("上传新证书失败: 返回证书ID为空")
		return ""
	}

	newCertID := *response.Response.CertificateId
	p.Log("新证书上传成功 CertificateId=" + newCertID)
	setDeployCertID(info, newCertID)
	p.Log("证书ID已更新为: " + newCertID)

	if err := sleepWithContext(ctx, 300*time.Millisecond); err != nil {
		return newCertID
	}

	switchRequest := ssl.NewModifyCertificatesExpiringNotificationSwitchRequest()
	switchRequest.CertificateIds = common.StringPtrs([]string{newCertID})
	switchRequest.SwitchStatus = common.Uint64Ptr(1)
	if _, err := client.ModifyCertificatesExpiringNotificationSwitch(switchRequest); err != nil {
		p.Log("关闭证书到期提醒失败: " + err.Error())
	}

	return newCertID
}

/*
 * deleteTencentOldCert 删除旧证书
 * 未勾选删除选项时跳过；新旧证书ID相同时视为证书内容未变而跳过
 */
func (p *TencentProvider) deleteTencentOldCert(config map[string]interface{}, oldCertID, newCertID string) {
	if !base.GetConfigBool(config, "delete_old_cert") {
		return
	}
	if newCertID != "" && newCertID == oldCertID {
		p.Log("新证书ID与旧证书ID相同（证书内容未变），跳过删除")
		return
	}

	client, err := p.getSSLClient("")
	if err != nil {
		p.Log(fmt.Sprintf("删除旧证书 %s 失败: %v", oldCertID, err))
		return
	}

	request := ssl.NewDeleteCertificateRequest()
	request.CertificateId = common.StringPtr(oldCertID)
	if _, err := client.DeleteCertificate(request); err != nil {
		p.Log(fmt.Sprintf("删除旧证书 %s 失败: %v", oldCertID, err))
		return
	}

	p.Log(fmt.Sprintf("旧证书 %s 已从腾讯云删除", oldCertID))
}

/*
 * findNewCertID 按证书主域名查询腾讯云上新生成的证书ID
 * 仅匹配已签发且域名一致的证书，取过期时间最晚的一张
 */
func (p *TencentProvider) findNewCertID(fullchain string) string {
	commonName, err := certCommonName(fullchain)
	if err != nil || commonName == "" {
		return ""
	}
	searchKey := strings.ReplaceAll(commonName, "*.", "")

	client, err := p.getSSLClient("")
	if err != nil {
		p.Log("查询新证书ID失败: " + err.Error())
		return ""
	}

	request := ssl.NewDescribeCertificatesRequest()
	request.SearchKey = common.StringPtr(searchKey)
	request.Limit = common.Uint64Ptr(20)
	response, err := client.DescribeCertificates(request)
	if err != nil {
		p.Log("查询新证书ID失败: " + err.Error())
		return ""
	}
	if response.Response == nil || len(response.Response.Certificates) == 0 {
		return ""
	}

	var matched []*ssl.Certificates
	for _, item := range response.Response.Certificates {
		if item == nil || item.Domain == nil || item.Status == nil || *item.Status != 1 {
			continue
		}
		if *item.Domain != commonName && *item.Domain != searchKey {
			continue
		}
		matched = append(matched, item)
	}
	if len(matched) == 0 {
		return ""
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return certEndTimeUnix(matched[i]) > certEndTimeUnix(matched[j])
	})
	if matched[0].CertificateId == nil {
		return ""
	}
	return *matched[0].CertificateId
}

/* certEndTimeUnix 解析证书过期时间，无法解析时返回 0 */
func certEndTimeUnix(item *ssl.Certificates) int64 {
	if item.CertEndTime == nil {
		return 0
	}
	raw := strings.TrimSpace(*item.CertEndTime)
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed.Unix()
		}
	}
	return 0
}
