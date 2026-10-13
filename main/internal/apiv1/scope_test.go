package apiv1

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/models"
)

/*
 * V1 鉴权范围的端到端测试
 *
 * 这些用例锁定四个维度的实际拦截效果：模块、域名、子域、只读。
 * 它们全部依赖真实数据库（sqlite 临时库），因为权限判定要经 getDomainPermission
 * 查 accounts 与 permissions 两张表，用桩替换会失去对 SQL 条件的覆盖
 */

/* seedV1Domain 建一个属于某用户的账户与其下的域名 */
func seedV1Domain(t *testing.T, ownerID uint, accountType, domainName string) (models.Account, models.Domain) {
	t.Helper()
	account := models.Account{UserID: ownerID, Type: accountType, Name: "acc-" + domainName}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: domainName}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	return account, domain
}

/* seedV1User 建一个普通用户 */
func seedV1User(t *testing.T, username string) models.User {
	t.Helper()
	u := models.User{Username: username, Email: username + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return u
}

/* scopeEngine 构造一个只含 scopeDomain 探针的路由，用于单独验证鉴权链 */
func scopeEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(APIKeyAuth())
	v1.Use(JSONBodyToDecryptedData())
	v1.Use(AdaptResponse())
	v1.POST("/probe", func(c *gin.Context) {
		var req struct {
			DomainID  string `json:"domain_id"`
			SubDomain string `json:"sub_domain"`
			Write     bool   `json:"write"`
		}
		_ = bindJSON(c, &req)
		domain, ok := scopeDomain(c, req.DomainID, req.SubDomain, req.Write)
		if !ok {
			return
		}
		OK(c, gin.H{"domain": domain.Name})
	})
	return r
}

/* signedHeaders 为一个请求生成合法的 API Key 签名头 */
func signedHeaders(t *testing.T, uid uint, apiKey, method, path, body string) map[string]string {
	t.Helper()
	ts := timestampNow()
	msg := itoa(uid) + "\n" + ts + "\n" + method + "\n" + path
	return map[string]string{
		"X-API-UID":       itoa(uid),
		"X-API-Timestamp": ts,
		"X-API-Sign":      signWithKey(msg, apiKey),
	}
}

/*
 * 自有账户的域名，所有者可读写
 *
 * 基线用例：若这条失败，说明权限判定把「自己的域名」也判成了无权，
 * 后续所有越权用例的失败都无法解释为「拦截生效」
 */
func TestScopeOwnerAllowed(t *testing.T) {
	setupV1TestDB(t)
	user := seedV1User(t, "owner")
	_, domain := seedV1Domain(t, user.ID, "cloudflare", "owner.example.com")

	const key = "0123456789abcdef0123456789abcdef"
	database.DB.Model(&user).Updates(map[string]interface{}{"is_api": true, "api_key": key})

	r := scopeEngine()
	path := "/api/v1/probe"
	body := `{"domain_id":"` + itoa(domain.ID) + `","write":true}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, user.ID, key, http.MethodPost, path, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("自有域名应放行，实际 HTTP=%d", resp.StatusCode)
	}
}

/*
 * 他人域名且无授权 → 拒绝
 *
 * 这是最关键的一条：无授权用户能拿到别人的域名 ID 时，必须在这里被挡下
 */
func TestScopeStrangerDenied(t *testing.T) {
	setupV1TestDB(t)
	owner := seedV1User(t, "real-owner")
	stranger := seedV1User(t, "stranger")
	_, domain := seedV1Domain(t, owner.ID, "cloudflare", "private.example.com")

	const key = "fedcba9876543210fedcba9876543210"
	database.DB.Model(&stranger).Updates(map[string]interface{}{"is_api": true, "api_key": key})

	r := scopeEngine()
	path := "/api/v1/probe"
	body := `{"domain_id":"` + itoa(domain.ID) + `"}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, stranger.ID, key, http.MethodPost, path, body))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("无授权访问他人域名应被拒，实际 HTTP=%d", resp.StatusCode)
	}
	var got v1Err
	decodeBody(t, resp, &got)
	if got.Code != CodeDomainDenied {
		t.Errorf("错误码 = %d，期望 %d", got.Code, CodeDomainDenied)
	}
}

/*
 * 被授予只读权限的用户执行写操作 → 拒绝
 *
 * 这正是控制台因 Permission 中间件未挂载而失效的能力。
 * 用例直接写 permissions 表，不经过任何中间件，因此 v1 与修复后的控制台都会受此约束
 */
func seedDelegatedPermission(t *testing.T, uid uint, domainName string, readOnly bool, sub string) {
	t.Helper()
	if err := database.DB.Create(&models.Permission{
		UserID:    uid,
		Domain:    domainName,
		SubDomain: sub,
		ReadOnly:  readOnly,
	}).Error; err != nil {
		t.Fatalf("创建授权失败: %v", err)
	}
}

func TestScopeReadOnlyDeniedOnWrite(t *testing.T) {
	setupV1TestDB(t)
	owner := seedV1User(t, "ro-owner")
	viewer := seedV1User(t, "ro-viewer")
	_, domain := seedV1Domain(t, owner.ID, "cloudflare", "ro.example.com")
	seedDelegatedPermission(t, viewer.ID, domain.Name, true, "")

	const key = "aaaabbbbccccddddaaaabbbbccccdddd"
	database.DB.Model(&viewer).Updates(map[string]interface{}{"is_api": true, "api_key": key})

	r := scopeEngine()
	path := "/api/v1/probe"
	body := `{"domain_id":"` + itoa(domain.ID) + `","write":true}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, viewer.ID, key, http.MethodPost, path, body))

	var got v1Err
	decodeBody(t, resp, &got)
	if got.Code != CodeReadOnly {
		t.Errorf("只读用户写入应回 CodeReadOnly(%d)，实际 code=%d message=%q",
			CodeReadOnly, got.Code, got.Message)
	}
}

/* 只读授权下的读操作必须放行，否则「只读」就退化成「禁止访问」 */
func TestScopeReadOnlyAllowedOnRead(t *testing.T) {
	setupV1TestDB(t)
	owner := seedV1User(t, "ro2-owner")
	viewer := seedV1User(t, "ro2-viewer")
	_, domain := seedV1Domain(t, owner.ID, "cloudflare", "ro2.example.com")
	seedDelegatedPermission(t, viewer.ID, domain.Name, true, "")

	const key = "11112222333344441111222233334444"
	database.DB.Model(&viewer).Updates(map[string]interface{}{"is_api": true, "api_key": key})

	r := scopeEngine()
	path := "/api/v1/probe"
	body := `{"domain_id":"` + itoa(domain.ID) + `","write":false}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, viewer.ID, key, http.MethodPost, path, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("只读授权下的读操作应放行，实际 HTTP=%d", resp.StatusCode)
	}
}

/*
 * 子域白名单之外的子域 → 拒绝
 *
 * 授权只覆盖 api 子域时，www 上的写操作必须被挡；
 * 若这里放行，说明 CheckSubDomainPermission 的比对逻辑没接上
 */
func TestScopeSubDomainRestricted(t *testing.T) {
	setupV1TestDB(t)
	owner := seedV1User(t, "sub-owner")
	viewer := seedV1User(t, "sub-viewer")
	_, domain := seedV1Domain(t, owner.ID, "cloudflare", "sub.example.com")
	seedDelegatedPermission(t, viewer.ID, domain.Name, false, "api")

	const key = "55556666777788885555666677778888"
	database.DB.Model(&viewer).Updates(map[string]interface{}{"is_api": true, "api_key": key})

	r := scopeEngine()
	path := "/api/v1/probe"

	// 授权内的子域应放行
	okBody := `{"domain_id":"` + itoa(domain.ID) + `","sub_domain":"api","write":true}`
	if resp := doRequest(r, http.MethodPost, path, []byte(okBody),
		signedHeaders(t, viewer.ID, key, http.MethodPost, path, okBody)); resp.StatusCode != http.StatusOK {
		t.Errorf("授权内的子域应放行，实际 HTTP=%d", resp.StatusCode)
	}

	// 授权外的子域应拒绝
	badBody := `{"domain_id":"` + itoa(domain.ID) + `","sub_domain":"www","write":true}`
	resp := doRequest(r, http.MethodPost, path, []byte(badBody),
		signedHeaders(t, viewer.ID, key, http.MethodPost, path, badBody))
	var got v1Err
	decodeBody(t, resp, &got)
	if got.Code != CodeSubDomainDenied {
		t.Errorf("越权子域应回 CodeSubDomainDenied(%d)，实际 code=%d message=%q",
			CodeSubDomainDenied, got.Code, got.Message)
	}
}

/*
 * 未配置模块权限的用户放行（兼容旧库）
 *
 * ApplyUserPermissionModules 把「permissions 列为空」视作未配置并放行，
 * 这是既有约定：老库里的用户没有 permissions 值，不应因此失去全部功能。
 * 若这条失败，v1 会对存量用户全面拒绝
 */
func TestScopeModuleUnconfiguredAllows(t *testing.T) {
	setupV1TestDB(t)
	owner := seedV1User(t, "mod-owner")
	_, domain := seedV1Domain(t, owner.ID, "cloudflare", "mod.example.com")

	const key = "99990000999900009999000099990000"
	database.DB.Model(&owner).Updates(map[string]interface{}{"is_api": true, "api_key": key})

	r := scopeEngine()
	path := "/api/v1/probe"
	body := `{"domain_id":"` + itoa(domain.ID) + `"}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, owner.ID, key, http.MethodPost, path, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("未配置 permissions 的用户应放行，实际 HTTP=%d", resp.StatusCode)
	}
}

/*
 * 显式配置了不含 domain 的 permissions → 拒绝
 *
 * 与上一条配对：空数组 [] 是「已配置且一个模块都不允许」，
 * 把它也当作未配置会让模块权限形同虚设
 */
func TestScopeModuleExplicitlyDenied(t *testing.T) {
	setupV1TestDB(t)
	owner := seedV1User(t, "mod2-owner")
	seedV1Domain(t, owner.ID, "cloudflare", "mod2.example.com")

	const key = "abcdabcdabcdabcdabcdabcdabcdabcd"
	database.DB.Model(&owner).Updates(map[string]interface{}{
		"is_api": true, "api_key": key, "permissions": `["cert"]`,
	})

	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(APIKeyAuth())
	v1.Use(AdaptResponse())
	v1.POST("/probe", requireModuleMW("domain"), func(c *gin.Context) { OK(c, gin.H{"ok": true}) })

	path := "/api/v1/probe"
	resp := doRequest(r, http.MethodPost, path, []byte("{}"),
		signedHeaders(t, owner.ID, key, http.MethodPost, path, "{}"))

	var got v1Err
	decodeBody(t, resp, &got)
	if got.Code != CodeModuleDenied {
		t.Errorf("仅授予 cert 模块的用户访问 domain 模块应回 CodeModuleDenied(%d)，实际 code=%d",
			CodeModuleDenied, got.Code)
	}
}
