package handler

import (
	"context"
	"strconv"
	"strings"

	"main/internal/api/middleware"
	"main/internal/dns"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/*
 * 批量记录接口的子域归属校验
 *
 * 单条接口（domain.go 的 CreateRecord / UpdateRecord）在建写之前一律走
 * CheckSubDomainPermission，批量族（batch.go 的三个）只走 CheckDomainPermission——
 * 域级校验只回答「这个域名归不归你管」，回答不了「这条记录归不归你管」。
 * 结果是收到 rr 级委派的用户可以把记录增删改到该域名下任意子域
 *
 * 校验对象必须是**云端记录的真实 rr**，不能用请求里的字段：
 *   BatchAddRecords 的文本模式一行是 "rr value"，结构模式的 Name 也是 rr，
 *   但 BatchEditRecords 与 BatchActionRecords 的请求里只有 record_id，没有 rr。
 *   从请求取参时两族各自缺一半，只有查云端记录能同时覆盖。
 *
 * 失败时**不整批拒绝**：BatchActionRecords 是一个请求删除全站记录的接口，
 * 整批拒绝会把它变成「别人有一条记录就能让你整批失败」的拒绝服务面。
 * 拒绝的只是越权那几条，调用方拿到被拒条数即可判断该重试什么。
 */

/* batchRecordAuthReq 批量接口在权限校验阶段需要的最小入参 */
type batchRecordAuthReq struct {
	DomainID  string
	RecordIDs []string
	Targets   []string // 已知 rr 的待写入目标；未知的由 fetchCloudRecordRR 解析
}

/*
 * cloudRecordRRIndex 取该域名的全量云端记录，索引 record_id → rr
 *
 * 分页聚合到上限，因为「record_id 在不在这个索引里」本身就是判定的一部分：
 * 截断的索引会把属于本域名的记录误判成「不属于本域名」，进而误拒合法操作
 *
 * 任一页失败返回 nil，调用方据此判为「无法判定」并拒绝，而不是放行
 */
func cloudRecordRRIndex(ctx context.Context, provider dns.Provider) map[string]string {
	const perPage = 500
	const maxPages = 10 // 5000 条封顶，与本项目其它分页聚合处一致
	idx := map[string]string{}
	for page := 1; page <= maxPages; page++ {
		result, err := provider.GetDomainRecords(ctx, page, perPage, "", "", "", "", "", "")
		if err != nil || result == nil {
			return nil
		}
		records, ok := result.Records.([]dns.Record)
		if !ok {
			return nil
		}
		for _, r := range records {
			idx[r.ID] = r.Name
		}
		if len(records) < perPage {
			break
		}
	}
	return idx
}

/*
 * fetchCloudRecordRR 判定给定的 record_id 是否属于本域名
 *
 * 命中返回该记录的真实 rr（第二返回值为 true），未命中返回 ("", false)。
 * 调用方据此区分「不属于本域名」与「取不回来」两种拒绝理由
 */
func fetchCloudRecordRR(ctx context.Context, provider dns.Provider, recordID string) (string, bool) {
	idx := cloudRecordRRIndex(ctx, provider)
	if idx == nil {
		return "", false
	}
	rr, ok := idx[strings.TrimSpace(recordID)]
	return rr, ok
}

/* checkBatchRecordSubDomain 逐条判定批量操作的目标是否落在授权子域内 */
func checkBatchRecordSubDomain(c *gin.Context, domain *models.Domain, userID string, level int, req batchRecordAuthReq) bool {
	/*
	 * 域主与管理员不受子域限制，先短路，省掉一次云端查询
	 *
	 * 这里不用 CheckDomainPermission 判域级：那个函数对管理员直接返回 true、
	 * 对域主走账户归属，恰好与下面的短路条件重合；分开写是为了让「先判域、
	 * 再判子域」的两层结构在调用点上看得见
	 */
	_, isOwner, allowed := middleware.DomainPermissionOf(userID, strconv.FormatUint(uint64(domain.ID), 10))
	if !allowed {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return false
	}
	if isOwner || level >= 2 {
		return true
	}

	/*
	 * 请求里已给出 rr 的（新建类）直接判，不需要云端数据
	 *
	 * 顺序上刻意放在建 provider 之前：这一族拒绝不需要任何上游调用，
	 * 把能本地判定完的先判掉，越权请求就不会在拒绝之前先消耗一次云端配额
	 */
	for _, rr := range req.Targets {
		if !middleware.CheckSubDomainPermission(userID, level, strconv.FormatUint(uint64(domain.ID), 10), rr) {
			middleware.ErrorResponse(c, "无权限操作该子域名: "+strings.TrimSpace(rr))
			return false
		}
	}
	if len(req.RecordIDs) == 0 {
		return true
	}

	provider, err := getDNSProviderByDomain(domain)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return false
	}

	/*
	 * 改/删类：请求里只有 record_id，必须回查云端拿到它真实归属的子域
	 *
	 * 判定依据刻意取自云端而非请求：请求里的 record_id 是攻击者可控的，
	 * 用它反查 rr 等于让攻击者自证清白
	 */
	mismatched := 0
	unresolved := 0
	for _, id := range req.RecordIDs {
		rr, known := fetchCloudRecordRR(c.Request.Context(), provider, id)
		if !known {
			// 云端查不到：要么该 record_id 不属于本域名，要么这一页取不回来。
			// 两种情况都不能放行——放行会让一次跨域批量删除静默生效
			unresolved++
			continue
		}
		if !middleware.CheckSubDomainPermission(userID, level, strconv.FormatUint(uint64(domain.ID), 10), rr) {
			mismatched++
		}
	}
	if unresolved > 0 {
		middleware.ErrorResponse(c, "存在无法确认归属的记录，已拒绝执行")
		return false
	}
	if mismatched > 0 {
		middleware.ErrorResponse(c, "存在无权限操作的子域名记录，已拒绝执行")
		return false
	}
	return true
}
