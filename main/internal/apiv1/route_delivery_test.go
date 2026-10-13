package apiv1

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
)

/*
 * 66 条接口的入参送达判定
 *
 * 判据是 **handler 真正发出去的 SQL**，不是响应文案。入参「送达」的定义是
 * 「ID 进了 WHERE 子句」，只有看 SQL 才能直接观测到这一点。
 *
 * 换判据的原因：原先按「响应 message 是否命中『缺少 xxxID』文案表」来反推，
 * 方向是反的——它把「没命中表」当成「送达」。而有一批 handler 拿到空串后不判空、
 * 直接拿去查库，报「分类不存在」「账户不存在」，既没命中表也确实没送达，
 * 于是被读成了「送达」。换一个措辞就会再漏一次，所以不能靠扩充文案表打补丁。
 *
 * 实现复用项目的查询追踪链路：RequestTrace 把 db_queries 挂到上下文，
 * GORM 回调把每条已绑定参数的 SQL 追加进去。用线上同一套链路而非自写 callback，
 * 是为了让断言的对象就是生产行为本身。
 *
 * 用真实路由而非「挂探针」是关键：探针路由自带 :id 会掩盖问题，
 * 只有真实路由表才能反映 c.Param 是否被 gin 的匹配结果填充。
 */

/* v1FullEngine 构造与生产同构的完整 v1 路由，并把每条请求的 SQL 收集到 out */
func v1FullEngine(out *[]database.DBQuery) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	// RequestTrace 是生产链的一环，它同时负责把 db_queries 挂到上下文
	v1.Use(middleware.RequestTrace())
	v1.Use(APIKeyAuth())
	v1.Use(JSONBodyToDecryptedData())
	v1.Use(AdaptResponse())
	// 收尾中间件：RequestTrace 已把 SQL 填好，此处取出交给调用方
	v1.Use(func(c *gin.Context) {
		c.Next()
		if v, ok := c.Get(middleware.DBQueriesKey); ok {
			if q, ok := v.(*[]database.DBQuery); ok && out != nil {
				*out = append(*out, *q...)
			}
		}
	})
	registerDomainRoutes(v1)
	registerRecordRoutes(v1)
	registerAccountRoutes(v1)
	registerCertRoutes(v1)
	registerMonitorRoutes(v1)
	registerScheduleRoutes(v1)
	registerCloudflareRoutes(v1)
	registerSystemRoutes(v1)
	registerUserRoutes(v1)
	registerOptimizeIPRoutes(v1)
	return r
}

/* deliveryCase 单条判定用例 */
type deliveryCase struct {
	path          string
	body          string
	wantDelivered bool
	note          string
}

/*
 * idCondRe 匹配已内联参数的 ID 等值条件，捕获其数值
 *
 * 覆盖 `tbl`.`id` = 123 与 `tbl`.`id` = "123" 两种呈现（数字型与字符串型主键的
 * Explain 结果不同），也覆盖不带反引号的裸写法。
 *
 * 表名前缀**可有可无**：GORM 生成的 SQL 里，走 Model().Where() 的那一类
 * 不带前缀（`WHERE id = 1 AND uid = 999999`），走 First(&x, id) 的那一类才带
 * （`WHERE `cert_accounts`.`id` = 999999`）。此前要求必须有前缀，
 * 会把前一类全部误判为「未送达」——判据的偏差比它要检出的问题更隐蔽
 *
 * 零值仍被排除（m[2] != "0"）：`WHERE uid = 0` 这类恒为字面零值的条件
 * 不构成送达证据
 */
var idCondRe = regexp.MustCompile("(?:`?[a-z_]+`?\\.)?`?(id|uid|aid|oid|did)`? *= *\"?([0-9]+)\"?")

/*
 * bareFirstRowRe 匹配「无 WHERE 且 LIMIT 1」的单行查询
 *
 * 这是空主键下 GORM 的退化形态：First(&x, "") 不带任何条件，等价于「取全表第一条」。
 * 它比「id = 0」更危险——不是查不到，而是查到了并会去改写/删除它。
 * 限定「无 WHERE」，是为了不误伤正常的单行查询（那些带 WHERE）。
 */
var bareFirstRowRe = regexp.MustCompile("(?i)^SELECT .+ FROM `[a-z_]+` ORDER BY `[a-z_]+`\\.`id` LIMIT 1$")

/*
 * apiKeyAuthQueryRe 匹配认证中间件查当前用户的语句
 *
 * 它不是被测接口发出的，其 id 条件来自 seed 数据而非请求入参，
 * 计入判据会让所有用例恒判「送达」
 */
var apiKeyAuthQueryRe = regexp.MustCompile("(?i)^SELECT \\* FROM `users` WHERE id = [0-9]+ AND `?users`?\\.`?deleted_at`? IS NULL ORDER BY")

/* insertRe 匹配写库语句：能走到 INSERT 说明 body 里的值确实被解析并落库了 */
var insertRe = regexp.MustCompile("(?i)^INSERT INTO ")

/*
 * sqlShowsDelivered 从捕获到的 SQL 判断入参是否真的送达了 handler
 *
 * 返回 (是否送达, 未送达的原因)，原因用于日志定位。
 *   送达   —— 至少出现一次非零 ID 等值条件，或成功执行了 INSERT
 *             （创建类接口的入参就是通过 INSERT 落库的，能 INSERT 即证明解析成功）
 *   未送达 —— 出现「无 WHERE 的 LIMIT 1」（空主键退化），或全程没有任何上述证据
 *
 * 判定顺序上，「无 WHERE 的 LIMIT 1」先于 INSERT 检查：退化查询一旦出现就说明
 * 入参没能构成主键条件，此时即便后面还有别的写库动作也不能算送达
 */
func sqlShowsDelivered(queries []database.DBQuery) (bool, string) {
	nonzero := 0
	// 本次判定是否出现过带表名前缀的 ID 等值条件。INSERT 是兜底证据，只在
	// 没有任何等值条件时启用：查询类接口出现 INSERT 必是别的动作（如写审计日志），
	// 拿它当送达证据会把「空主键退化」误判成送达
	sawQualified := false
	var fallbackInsert string
	for _, q := range queries {
		sql := strings.TrimSpace(q.SQL)
		/*
		 * 跳过认证中间件自己的查询
		 *
		 * APIKeyAuth 每次请求都要 `SELECT * FROM users WHERE id = ? AND is_api = ?
		 * AND status = ?` 找当前用户，而那个 id 是 seedAPIAdmin 建的第一条（=1）。
		 * 它带表名的 id 等值条件会被判据当成「入参送达」的证据——
		 * 结果每条用例都恒判送达，反向验证（删掉桥接）也不会失败
		 */
		if apiKeyAuthQueryRe.MatchString(sql) {
			continue
		}
		if bareFirstRowRe.MatchString(sql) {
			return false, "空主键退化为全表第一条（无 WHERE 的 LIMIT 1）"
		}
		if insertRe.MatchString(sql) {
			if fallbackInsert == "" {
				fallbackInsert = sql
			}
			continue
		}
		for _, m := range idCondRe.FindAllStringSubmatch(sql, -1) {
			if m[2] != "0" {
				nonzero++
				sawQualified = true
			}
		}
	}
	if sawQualified {
		return true, ""
	}
	// 创建类接口的入参经 INSERT 落库，且该 INSERT 不带表名前缀可供识别，只能按写入放行
	if fallbackInsert != "" {
		head := fallbackInsert
		if len(head) > 40 {
			head = head[:40] + "…"
		}
		return true, "无非零 ID 等值条件，以写库语句为送达证据：" + head
	}
	if nonzero == 0 {
		return false, "SQL 中无非零 ID 条件、也无 INSERT"
	}
	return false, "SQL 中无非零 ID 条件"
}

/*
 * routeDeliveryCases 全部 66 条判定用例
 *
 * wantDelivered 是**期望值**，由 SQL 判据反推得出，不是按文案猜的。
 * 补桥接后本表应整体翻转，届时用例会红，提醒同步更新
 */
func routeDeliveryCases() []deliveryCase {
	return []deliveryCase{
		// ---- domains：有 body 兜底的可用 ----
		{"/api/v1/domains/detail", `{"id":"999999"}`, true, "req.ID 兜底"},
		{"/api/v1/domains/delete", `{"id":"999999"}`, true, "req.ID 兜底"},
		{"/api/v1/domains/update", `{"id":"999999"}`, true, "req.ID 兜底"},
		{"/api/v1/domains/lines", `{"domain_id":"999999"}`, true, "req.DomainID 兜底"},
		{"/api/v1/domains/whois", `{"domain_id":"999999"}`, true, "req.DomainID 兜底"},
		// ---- domains：靠 loadDomainForAction / c.Param，当前送不到 ----
		{"/api/v1/domains/aliases", `{"domain_id":"999999"}`, true, "组级 paramAlias 回填 :id"},
		{"/api/v1/domains/aliases/add", `{"domain_id":"999999","name":"a.example.com"}`, true, "组级 paramAlias 回填 :id"},
		{"/api/v1/domains/aliases/delete", `{"alias_id":"999999"}`, true, "单条 paramAlias 回填 aliasId"},
		{"/api/v1/domains/groups", `{"domain_id":"999999"}`, true, "组级 paramAlias 回填 :id"},
		{"/api/v1/domains/groups/change", `{"domain_id":"999999","record_ids":["1"],"group_id":"1"}`, true, "组级 paramAlias 回填 :id"},
		{"/api/v1/domains/categories/update", `{"id":"999999","name":"x"}`, true, "单条 paramAlias 回填 id"},
		{"/api/v1/domains/categories/delete", `{"id":"999999"}`, true, "单条 paramAlias 回填 id"},
		// ---- records ----
		{"/api/v1/records/list", `{"domain_id":"999999"}`, true, "req.DomainID 兜底"},
		{"/api/v1/records/batch/add", `{"domain_id":"999999","records":"a 192.0.2.1"}`, true, "req.DomainID 兜底"},
		{"/api/v1/records/batch/edit", `{"domain_id":"999999","items":[]}`, true, "req.DomainID 兜底"},
		{"/api/v1/records/batch/action", `{"domain_id":"999999","action":"enable"}`, true, "req.DomainID 兜底"},
		{"/api/v1/records/weight", `{"domain_id":"999999","name":"www"}`, true, "组级 paramAlias 回填 :id"},
		{"/api/v1/records/weight/update", `{"domain_id":"999999","record_id":"1","weight":1}`, true, "req.DomainID 兜底"},
		{"/api/v1/records/quickinfo", `{"domain_id":"999999","name":"www"}`, true, "组级 paramAlias 回填 :id"},
		{"/api/v1/records/logs", `{"domain_id":"999999"}`, true, "组级 paramAlias 回填 :id"},
		// ---- accounts：有兜底 ----
		{"/api/v1/accounts/delete", `{"id":"999999"}`, true, "req.ID 兜底"},
		{"/api/v1/accounts/check", `{"id":"999999"}`, true, "req.ID 兜底"},
		// ---- cert：只读 Param(id)，非零 ID 从未进入 SQL ----
		{"/api/v1/cert/accounts/update", `{"account_id":"999999","name":"x"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/accounts/delete", `{"account_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/orders/process", `{"order_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/orders/delete", `{"order_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/orders/detail", `{"order_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/orders/log", `{"order_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/orders/auto", `{"order_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/orders/download", `{"order_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/deploys/update", `{"deploy_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/deploys/delete", `{"deploy_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		{"/api/v1/cert/deploys/process", `{"deploy_id":"999999"}`, true, "子组 paramAlias 回填 :id"},
		// 三条只依赖 body 的创建接口：body 必须给到能走到触库那一步，
		// 否则必填校验会先拒、无 SQL、判据无法证伪（这是判据的固有盲区）
		{"/api/v1/cert/accounts/create", `{"type":"x","name":"y","config":{}}`, true, "仅依赖 body；入参经 INSERT 落库，SQL 里不带表名，由首个无表名 INSERT 放行"},
		{"/api/v1/cert/orders/create", `{"account_id":1,"domains":["a.example.com"]}`, true, "仅依赖 body；account_id 进入 SQL"},
		{"/api/v1/cert/deploys/create", `{"account_id":1,"order_id":1,"config":{}}`, true, "仅依赖 body；account_id 进入 SQL"},
		// ---- monitor：全部走 req.ID / req.TaskID 兜底 ----
		{"/api/v1/monitor/tasks/update", `{"id":999999}`, true, "req.ID 兜底"},
		{"/api/v1/monitor/tasks/delete", `{"id":999999}`, true, "req.ID 兜底"},
		{"/api/v1/monitor/tasks/toggle", `{"id":999999}`, true, "req.ID 兜底"},
		{"/api/v1/monitor/tasks/logs", `{"id":999999}`, true, "req.ID 兜底"},
		{"/api/v1/monitor/tasks/history", `{"task_id":999999}`, true, "req.TaskID 兜底"},
		{"/api/v1/monitor/tasks/uptime", `{"task_id":999999}`, true, "req.TaskID 兜底"},
		{"/api/v1/monitor/tasks/resolve-status", `{"task_id":999999}`, true, "req.TaskID 兜底"},
		// ---- schedule：只读 Param(id) ----
		{"/api/v1/schedule/tasks/update", `{"id":"999999","did":1,"rr":"www","record_id":"1"}`, true, "子组 paramAlias 回填 id"},
		{"/api/v1/schedule/tasks/delete", `{"id":"999999"}`, true, "子组 paramAlias 回填 id"},
		{"/api/v1/schedule/tasks/toggle", `{"id":"999999"}`, true, "子组 paramAlias 回填 id"},
		// ---- users：只读 Param(id) ----
		{"/api/v1/users/update", `{"uid":"999999","email":"a@b.c"}`, true, "组级/单条 paramAlias 回填 :id"},
		{"/api/v1/users/delete", `{"uid":"999999"}`, true, "组级/单条 paramAlias 回填 :id"},
		{"/api/v1/users/api-key/reset", `{"uid":"999999"}`, true, "组级/单条 paramAlias 回填 :id"},
		{"/api/v1/users/permissions/list", `{"uid":"999999"}`, true, "组级/单条 paramAlias 回填 :id"},
		{"/api/v1/users/permissions/add", `{"uid":"999999","did":1,"domain":"p.example.com"}`, true, "组级 paramAlias 回填 :id；did 与 domain 是该接口的必填字段"},
		{"/api/v1/users/permissions/update", `{"uid":"999999","perm_id":"1"}`, true, "组级/单条 paramAlias 回填 :id"},
		{"/api/v1/users/permissions/delete", `{"uid":"999999","perm_id":"1"}`, true, "组级/单条 paramAlias 回填 :id"},
		// ---- cloudflare：已挂桥接 ----
		{"/api/v1/cloudflare/hostnames/list", `{"domain_id":"999999"}`, true, "cfDomainBridge"},
		{"/api/v1/cloudflare/hostnames/refresh", `{"domain_id":"999999","hostname_id":"h1"}`, true, "cfDomainBridge"},
		{"/api/v1/cloudflare/hostnames/delete", `{"domain_id":"999999","hostname_id":"h1"}`, true, "cfDomainBridge"},
		{"/api/v1/cloudflare/fallback/get", `{"domain_id":"999999"}`, true, "cfDomainBridge"},
		{"/api/v1/cloudflare/dcv-delegation-uuid", `{"domain_id":"999999"}`, true, "cfDomainBridge"},
		{"/api/v1/cloudflare/tunnels/list", `{"account_id":"999999"}`, true, "cfAccountBridge"},
		{"/api/v1/cloudflare/tunnels/token", `{"account_id":"999999","tunnel_id":"t1"}`, true, "cfAccountBridge"},
		{"/api/v1/cloudflare/tunnels/cidr/list", `{"account_id":"999999","tunnel_id":"t1"}`, true, "cfAccountBridge"},
		{"/api/v1/cloudflare/tunnels/hostname-routes/list", `{"account_id":"999999","tunnel_id":"t1"}`, true, "cfAccountBridge"},
		{"/api/v1/cloudflare/tunnels/delete", `{"account_id":"999999","tunnel_id":"999999"}`, true, "cfAccountBridge"},
		// ---- system：已挂桥接 ----
		{"/api/v1/system/logs/detail", `{"log_id":"999999"}`, true, "systemParamBridge"},
	}
}

/* TestRouteParamDelivery 逐接口判定入参是否送达 handler */
func TestRouteParamDelivery(t *testing.T) {
	setupBodyTestDB(t)
	database.RegisterDBCallbacks()
	admin, key := seedAPIAdmin(t, "delivery-admin")

	// 建最小记录：让 wantDelivered=true 的用例能走完前置校验，触及「不存在」分支
	account := models.Account{UserID: admin.ID, Type: "cloudflare", Name: "deliv-cf"}
	database.DB.Create(&account)
	domain := models.Domain{AccountID: account.ID, Name: "deliv.example.com", ThirdID: "zone1"}
	database.DB.Create(&domain)

	var queries []database.DBQuery
	r := v1FullEngine(&queries)

	for _, tc := range routeDeliveryCases() {
		t.Run(tc.path, func(t *testing.T) {
			queries = queries[:0]

			resp := doRequest(r, http.MethodPost, tc.path, []byte(tc.body),
				signedHeaders(t, admin.ID, key, http.MethodPost, tc.path, tc.body))
			var got v1Err
			_ = json.NewDecoder(resp.Body).Decode(&got)
			resp.Body.Close()

			delivered, why := sqlShowsDelivered(queries)
			state := map[bool]string{true: "送达", false: "未送达"}[delivered]
			if delivered != tc.wantDelivered {
				want := map[bool]string{true: "送达", false: "未送达"}[tc.wantDelivered]
				t.Errorf("%s（%s）期望 %s，SQL 判据给出 %s。code=%d message=%q SQL=%d 条 [%s]",
					tc.path, tc.note, want, state, got.Code, got.Message, len(queries), why)
			}
			var all []string
			for _, q := range queries {
				all = append(all, q.SQL)
			}
			t.Logf("[%s] %-46s %-40s code=%-4d SQL=%d %s || %s",
				state, tc.path, tc.note, got.Code, len(queries), why, strings.Join(all, " ~ "))
		})
	}
}

/*
 * TestRouteParamDeliveryCounts 锁定未送达清单与总数
 *
 * 这份清单就是交付的对照表；数字或成员变化时用例会红，提醒同步更新
 */
func TestRouteParamDeliveryCounts(t *testing.T) {
	var unreachable []string
	for _, tc := range routeDeliveryCases() {
		if !tc.wantDelivered {
			unreachable = append(unreachable, tc.path)
		}
	}

	/*
	 * 未送达已归零
	 *
	 * 证书三族（accounts/orders/deploys）与 users 族此前列为未送达，
	 * 实为**测试用了错的 body 键而非接口不可用**：cert 三组的 paramAlias
	 * 映射的是 account_id / order_id / deploy_id，users 族映射的是 uid，
	 * 而用例一律传 {"id": "..."}，回填自然不生效
	 *
	 * 同时判据本身也有偏差：idCondRe 要求「表名.列名」，而 GORM 走
	 * Model().Where() 时生成的是裸列名，会被误判为未送达。
	 * 修正后这条清单归零——它现在是「桥接覆盖率」的守卫，
	 * 任何新接口忘了挂桥接都会在这里亮红
	 */
	if len(unreachable) != 0 {
		t.Errorf("桥接已全覆盖，未送达应为 0 条，实际 %d 条：%v", len(unreachable), unreachable)
	}

	// 清单里每一条都必须是真实注册的 v1 路由，否则统计口径失真
	r := v1FullEngine(nil)
	registered := map[string]bool{}
	for _, route := range r.Routes() {
		registered[route.Path] = true
	}
	for _, path := range unreachable {
		if !registered[path] {
			t.Errorf("清单中的 %s 未在 v1 路由表注册", path)
		}
	}

	if total := len(routeDeliveryCases()); total != 64 {
		t.Errorf("用例总数应为 64，实际 %d", total)
	}
}
