package apiv1

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"main/internal/api/handler"
	"main/internal/database"
	"main/internal/dns"
	"main/internal/models"
	"main/internal/service"
)

/*
 * 解析记录的同步写操作
 *
 * 控制台版本（handler.CreateRecord 等）把云端调用交给 utils.SafeGo 后台执行，
 * 接口只回「提交成功」。对人机交互尚可——页面会重新拉列表，但对程序化调用不成立：
 * 调用方拿不到成败、失败原因、以及新记录的标识，无法据此决定重试还是上报。
 *
 * 因此这五个写接口在 v1 内重写为同步：等云端返回后再响应，失败时回具体原因。
 * 读写链路与业务规则沿用控制台版本，仅把 goroutine 换成就地等待
 */

/* recordOpTimeout 单次云端写操作的超时，与 SafeGo 版内部的 30s 保持一致 */
const recordOpTimeout = 30 * time.Second

/* recordWriteRequest 记录写操作的公共入参（新增/修改/删除/启停 共用域名字段） */
type recordWriteRequest struct {
	DomainID string `json:"domain_id"`
	RecordID string `json:"record_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Line     string `json:"line"`
	TTL      int    `json:"ttl"`
	MX       int    `json:"mx"`
	Weight   *int   `json:"weight"`
	Remark   string `json:"remark"`
	Enable   *bool  `json:"enable"`
}

/*
 * resolveRecordScope 解析记录操作的作用域
 *
 * 顺序：取域名 ID → 加载域名 → 域名权限 → 子域权限 → 只读检查。
 * 子域校验用请求里带的 name；删除与启停这类只给 record_id 的调用，
 * 先从云端回查该记录的真实名称再校验，否则子域限制可以被绕过
 */
func resolveRecordScope(c *gin.Context, domainID, recordID, subDomain string, needWrite bool) (*models.Domain, dns.Provider, bool) {
	domain, ok := scopeDomain(c, domainID, subDomain, needWrite)
	if !ok {
		return nil, nil, false
	}

	provider := providerForDomain(c, domain)
	if provider == nil {
		return nil, nil, false
	}

	/*
	 * 只给 record_id 时无法从请求得知子域名，须回查云端。
	 * 否则持有 A 子域授权的用户可以对同域名下 B 子域的记录发起删除
	 */
	if strings.TrimSpace(subDomain) == "" && strings.TrimSpace(recordID) != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), recordOpTimeout)
		defer cancel()
		rec, err := provider.GetDomainRecordInfo(ctx, recordID)
		if err != nil {
			Failf(c, CodeRecordNotFound, "记录 %s 不存在或无法读取: %s", recordID, err.Error())
			c.Abort()
			return nil, nil, false
		}
		if !requireSubDomain(c, domain, rec.Name) {
			return nil, nil, false
		}
	}

	return domain, provider, true
}

/*
 * providerForDomain 取域名对应的 DNS 服务商实例
 *
 * 与 handler.getProviderByDomain 同逻辑，但那个函数未导出且用控制台的错误格式写响应，
 * 在 v1 下会写出 shape 不一致的 JSON，故在此重写一份
 */
func providerForDomain(c *gin.Context, domain *models.Domain) dns.Provider {
	var account models.Account
	if err := database.WithContext(c).First(&account, domain.AccountID).Error; err != nil {
		Fail(c, CodeAccountNotFound, "")
		c.Abort()
		return nil
	}
	config := map[string]string{}
	if err := jsonUnmarshal([]byte(account.Config), &config); err != nil {
		Failf(c, CodeAccountConfigBad, "账户 %s 的配置无法解析", account.Name)
		c.Abort()
		return nil
	}
	provider, err := dns.GetProvider(account.Type, config, domain.Name, domain.ThirdID)
	if err != nil {
		Failf(c, CodeProviderUnsupported, "无法初始化 %s 服务商: %s", account.Type, err.Error())
		c.Abort()
		return nil
	}
	return provider
}

/*
 * CreateRecordSync 新增解析记录（同步）
 *
 * 返回新建记录的 ID：控制台版本不回传，调用方若想立即引用该记录只能再查一次列表，
 * 而列表是最终一致的，刚写入的记录未必立刻可见
 */
func CreateRecordSync(c *gin.Context) {
	var req recordWriteRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if !requireString(c, req.Name, "name") || !requireString(c, req.Type, "type") || !requireString(c, req.Value, "value") {
		return
	}

	domain, provider, ok := resolveRecordScope(c, req.DomainID, "", req.Name, true)
	if !ok {
		return
	}

	if req.Line == "" {
		req.Line = "default"
	}
	if req.TTL == 0 {
		req.TTL = 600
	}

	/*
	 * 子域名前缀占用判定，必须在云端写入之前
	 *
	 * 占用是排他性的：先写后判会让云端先出现一条马上要被删掉的记录，
	 * 并发下两个用户还可能同时通过判定
	 */
	switch service.CheckSubdomainClaim(domain, req.Name, currentUserUID(c), c.GetInt("level")) {
	case service.ClaimTakenByOther:
		Failf(c, CodeResourceConflict, "子域名 %s.%s 已被其他用户占用", req.Name, domain.Name)
		c.Abort()
		return
	case service.ClaimDeniedByExisting:
		Failf(c, CodeResourceConflict, "子域名 %s.%s 下已存在记录，无法占用", req.Name, domain.Name)
		c.Abort()
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), recordOpTimeout)
	defer cancel()

	recordID, err := provider.AddDomainRecord(ctx, req.Name, req.Type, req.Value, req.Line, req.TTL, req.MX, req.Weight, req.Remark)
	if err != nil {
		Failf(c, CodeDNSWriteFailed, "新增记录失败: %s", err.Error())
		logRecordAction(c, "add_record_failed", domain.Name,
			fmt.Sprintf("%s.%s -> %s 失败: %s", req.Name, domain.Name, req.Value, err.Error()))
		c.Abort()
		return
	}

	logRecordAction(c, "add_record", domain.Name, fmt.Sprintf("%s.%s -> %s", req.Name, domain.Name, req.Value))

	/*
	 * 立即回填本地副本
	 *
	 * 定时同步要等一个周期，期间本地看不到这条记录，而配额判定读的是本地——
	 * 不即时写入就等于给了一个「新建后立刻再建」的窗口，两次都能通过配额校验
	 */
	syncCreatedRecord(domain, recordID, req, currentUserUID(c))
	service.RegisterSubdomainClaim(domain, req.Name, currentUserUID(c))

	OK(c, gin.H{"record_id": recordID})
}

/*
 * UpdateRecordSync 修改解析记录（同步）
 *
 * 先回查原记录再改：控制台版本直接按请求体更新，若调用方传错 record_id，
 * 会把另一条记录的属性改掉而调用方毫无察觉。回查既能确认记录存在，
 * 也能拿到它的真实名称用于子域校验
 */
func UpdateRecordSync(c *gin.Context) {
	var req recordWriteRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.RecordID == "" {
		req.RecordID = c.Param("recordId")
	}
	if !requireString(c, req.RecordID, "record_id") {
		return
	}
	if !requireString(c, req.Name, "name") || !requireString(c, req.Type, "type") || !requireString(c, req.Value, "value") {
		return
	}

	domain, provider, ok := resolveRecordScope(c, req.DomainID, req.RecordID, "", true)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), recordOpTimeout)
	defer cancel()

	original, err := provider.GetDomainRecordInfo(ctx, req.RecordID)
	if err != nil {
		Failf(c, CodeRecordNotFound, "记录 %s 不存在: %s", req.RecordID, err.Error())
		c.Abort()
		return
	}

	/*
	 * 未传的字段用**该记录的原值**兜底，而不是硬编码默认值
	 *
	 * 控制台的批量编辑就是这个做法（batch.go:274-280，从回查结果取 ttl/line）。
	 * 此前这里对未传的 TTL 填 600、未传的 type 直接用空串送进云端，
	 * 于是「只想改记录值」的调用会把 TTL 从自定义值改成 600、把类型清空
	 */
	if req.Line == "" {
		req.Line = original.Line
	}
	if req.TTL == 0 {
		req.TTL = original.TTL
	}
	if req.Type == "" {
		req.Type = original.Type
	}

	/*
	 * req.Name 是**写入云端的新名**，此前从未被校验
	 *
	 * 上一步的 resolveRecordScope 校验的是 original.Name（该记录当前的名字），
	 * 于是持有 old 子域授权的用户可以把记录改名到任意子域。
	 * 与删除/启停路径「只给 record_id 时回查真实主机名再校验」是同一条思路，
	 * 这里补的是改名这一维
	 */
	if !requireSubDomain(c, domain, req.Name) {
		return
	}

	/*
	 * 改名要迁移子域名前缀的占用
	 *
	 * oldPrefix 取的是 original.Name（云端回查结果）而非本地副本：
	 * 本地副本可能落后于云端，改动前的名字以云端为准
	 */
	oldPrefix := service.NormalizeRecordPrefix(original.Name)
	newPrefix := service.NormalizeRecordPrefix(req.Name)
	uid := currentUserUID(c)
	renamed := oldPrefix != newPrefix

	if renamed {
		switch service.CheckSubdomainClaim(domain, req.Name, uid, c.GetInt("level")) {
		case service.ClaimTakenByOther, service.ClaimDeniedByExisting:
			Failf(c, CodeSubDomainDenied, "改名为 %s.%s 被拒：该子域名前缀已被占用", req.Name, domain.Name)
			c.Abort()
			return
		}
	}

	if err := provider.UpdateDomainRecord(ctx, req.RecordID, req.Name, req.Type, req.Value, req.Line, req.TTL, req.MX, req.Weight, req.Remark); err != nil {
		Failf(c, CodeDNSWriteFailed, "修改记录失败: %s", err.Error())
		logRecordAction(c, "update_record_failed", domain.Name,
			fmt.Sprintf("%s.%s -> %s 失败: %s", req.Name, domain.Name, req.Value, err.Error()))
		c.Abort()
		return
	}

	logRecordAction(c, "update_record", domain.Name, fmt.Sprintf("%s.%s -> %s", req.Name, domain.Name, req.Value))

	syncRecordToLocal(domain, req.RecordID, func(rec *models.DNSRecord) {
		rec.Name = req.Name
		rec.Type = req.Type
		rec.Value = req.Value
		rec.Line = req.Line
		rec.TTL = req.TTL
		rec.MX = req.MX
		if req.Weight != nil {
			rec.Weight = *req.Weight
		}
	})
	if renamed {
		service.RegisterSubdomainClaim(domain, req.Name, uid)
		// 旧前缀下再无记录才释放：多值 A 改掉其中一条时保留占用
		service.ReleaseSubdomainClaimIfEmpty(domain.ID, oldPrefix)
	}
	OK(c, gin.H{"record_id": req.RecordID})
}

/* DeleteRecordSync 删除解析记录（同步） */
func DeleteRecordSync(c *gin.Context) {
	var req recordWriteRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.RecordID == "" {
		req.RecordID = c.Param("recordId")
	}
	if !requireString(c, req.RecordID, "record_id") {
		return
	}

	domain, provider, ok := resolveRecordScope(c, req.DomainID, req.RecordID, "", true)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), recordOpTimeout)
	defer cancel()

	if err := provider.DeleteDomainRecord(ctx, req.RecordID); err != nil {
		Failf(c, CodeDNSWriteFailed, "删除记录失败: %s", err.Error())
		logRecordAction(c, "delete_record_failed", domain.Name, fmt.Sprintf("%s 失败: %s", req.RecordID, err.Error()))
		c.Abort()
		return
	}

	logRecordAction(c, "delete_record", domain.Name, req.RecordID)

	/*
	 * 释放占用前先取该记录的前缀：本地副本删掉后就查不到它的 name 了
	 *
	 * 只在同前缀下已无其他记录时才真正释放——多值 A 删掉一条不该放掉整个占用
	 */
	releasedPrefix := ""
	if local, ok := service.LocalRecord(domain.ID, req.RecordID); ok {
		releasedPrefix = local.Name
	}
	service.DeleteLocalRecord(domain.ID, req.RecordID)
	if releasedPrefix != "" {
		service.ReleaseSubdomainClaimIfEmpty(domain.ID, releasedPrefix)
	}
	OK(c, gin.H{"record_id": req.RecordID})
}

/* SetRecordStatusSync 启用或暂停解析记录（同步） */
func SetRecordStatusSync(c *gin.Context) {
	var req recordWriteRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.RecordID == "" {
		req.RecordID = c.Param("recordId")
	}
	if !requireString(c, req.RecordID, "record_id") {
		return
	}
	if req.Enable == nil {
		Fail(c, CodeParamMissing, "缺少必填参数 enable")
		c.Abort()
		return
	}

	domain, provider, ok := resolveRecordScope(c, req.DomainID, req.RecordID, "", true)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), recordOpTimeout)
	defer cancel()

	if err := provider.SetDomainRecordStatus(ctx, req.RecordID, *req.Enable); err != nil {
		Failf(c, CodeDNSWriteFailed, "设置记录状态失败: %s", err.Error())
		c.Abort()
		return
	}

	action := "disable_record"
	if *req.Enable {
		action = "enable_record"
	}
	logRecordAction(c, action, domain.Name, req.RecordID)

	status := "disable"
	if *req.Enable {
		status = "enable"
	}
	syncRecordToLocal(domain, req.RecordID, func(rec *models.DNSRecord) { rec.Status = status })
	OK(c, gin.H{"record_id": req.RecordID, "enable": *req.Enable})
}

/* UpdateRecordRemarkSync 修改解析记录备注（同步） */
func UpdateRecordRemarkSync(c *gin.Context) {
	var req recordWriteRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.RecordID == "" {
		req.RecordID = c.Param("recordId")
	}
	if !requireString(c, req.RecordID, "record_id") {
		return
	}

	domain, provider, ok := resolveRecordScope(c, req.DomainID, req.RecordID, "", true)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), recordOpTimeout)
	defer cancel()

	if err := provider.UpdateDomainRecordRemark(ctx, req.RecordID, req.Remark); err != nil {
		Failf(c, CodeDNSWriteFailed, "修改备注失败: %s", err.Error())
		c.Abort()
		return
	}

	syncRecordToLocal(domain, req.RecordID, func(rec *models.DNSRecord) { rec.Remark = req.Remark })
	OK(c, gin.H{"record_id": req.RecordID, "remark": req.Remark})
}

/* currentUserUID 当前 API 调用的用户 ID（数字），失败时为 0 */
func currentUserUID(c *gin.Context) uint {
	u, _ := strconv.ParseUint(c.GetString("user_id"), 10, 32)
	return uint(u)
}

/*
 * syncCreatedRecord 新建记录成功后按已知入参回填本地副本
 *
 * 新建路径的字段全部来自请求（未传的用默认值补齐），不需要再读云端，
 * 但本地可能残留同 record_id 的旧行（删除后 record_id 被复用），故走 Upsert
 */
func syncCreatedRecord(domain *models.Domain, recordID string, req recordWriteRequest, userID uint) {
	if domain == nil || recordID == "" {
		return
	}
	weight := 0
	if req.Weight != nil {
		weight = *req.Weight
	}
	service.UpsertLocalRecord(&models.DNSRecord{
		DomainID:  domain.ID,
		RecordID:  recordID,
		Name:      req.Name,
		Type:      req.Type,
		Value:     req.Value,
		Line:      req.Line,
		TTL:       req.TTL,
		MX:        req.MX,
		Weight:    weight,
		Remark:    req.Remark,
		Status:    "enable",
		Domain:    domain.Name,
		CreatedBy: userID,
	})
}

/*
 * syncRecordToLocal 云端写成功后回填本地副本
 *
 * 本地副本是配额判定与同步对账的基准，不即时回填就要等一个同步周期，
 * 期间它的内容与云端不一致
 *
 * 不依赖「本地已有该行」：记录可能是直接建在云端、由上一轮对账落库的，
 * 也可能本轮才建好而回填尚未发生。因此走 Update 有变更行则更新，
 * 否则按本地快照补一行（对账会在一轮内校正其内容）
 */
func syncRecordToLocal(domain *models.Domain, recordID string, mutate func(*models.DNSRecord)) {
	if domain == nil || recordID == "" {
		return
	}
	if local, ok := service.LocalRecord(domain.ID, recordID); ok {
		mutate(&local)
		service.UpsertLocalRecord(&local)
		return
	}
	rec := models.DNSRecord{
		DomainID: domain.ID,
		RecordID: recordID,
		Domain:   domain.Name,
	}
	mutate(&rec)
	service.UpsertLocalRecord(&rec)
}

/* logRecordAction 写操作审计，与控制台同一张表与同一套 action 命名 */
func logRecordAction(c *gin.Context, action, domainName, detail string) {
	service.Audit.LogActionDirect(
		c.GetString("user_id"), c.GetString("username"),
		c.ClientIP(), c.Request.UserAgent(),
		action, domainName, detail,
	)
}

var _ = handler.GetRecords
