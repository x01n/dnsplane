package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 首页统计的作用域
 *
 * 管理员看全站，普通用户只看自己名下的资源。此前一律返回全站计数——
 * 普通用户打开首页，「域名总数」是别人的域名加上去的，既泄露实例规模，
 * 又与他实际拥有的数量不符。
 *
 * 同文件的 GetSystemInfo 早已按 level 门控，这一路漏了。
 */

/* dashCtx 构造首页统计所需的上下文 */
func dashCtx(t *testing.T, uid uint, level int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/dashboard/stats", strings.NewReader(""))
	c.Set("user_id", itoaUint(uid))
	c.Set("level", level)
	return c, w
}

/* decodeDashStats 解出首页统计的字段 */
func decodeDashStats(t *testing.T, w *httptest.ResponseRecorder) map[string]int64 {
	t.Helper()
	var resp struct {
		Code int                `json:"code"`
		Data map[string]float64 `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	out := map[string]int64{}
	for k, v := range resp.Data {
		out[k] = int64(v)
	}
	return out
}

/* TestDashboardStatsScopesToOwner 普通用户只看到自己名下的计数 */
func TestDashboardStatsScopesToOwner(t *testing.T) {
	setupHandlerTestDB(t)

	me := quotaPrefixUser(t, "dash-me", `{"max_domains":-1}`)
	other := quotaPrefixUser(t, "dash-other", `{"max_domains":-1}`)

	d1 := quotaPrefixDomain(t, me.ID, "mine.example.com")
	d2 := quotaPrefixDomain(t, other.ID, "theirs1.example.com")
	d3 := quotaPrefixDomain(t, other.ID, "theirs2.example.com")

	// 甲自有 1 个域名、1 条监控；乙有 2 个域名、2 条监控
	database.DB.Create(&models.DMTask{DomainID: d1.ID, RR: "a"})
	database.DB.Create(&models.DMTask{DomainID: d2.ID, RR: "b"})
	database.DB.Create(&models.DMTask{DomainID: d3.ID, RR: "c"})

	c, w := dashCtx(t, me.ID, 1)
	GetDashboardStats(c)

	got := decodeDashStats(t, w)
	if got["domains"] != 1 {
		t.Errorf("普通用户的域名数应为自有 1 个，实际 %d（2 是他人的，3 是全站）", got["domains"])
	}
	if got["tasks"] != 1 {
		t.Errorf("普通用户的监控任务数应为 1，实际 %d", got["tasks"])
	}
}

/* TestDashboardStatsAdminSeesAll 管理员仍看全站 */
func TestDashboardStatsAdminSeesAll(t *testing.T) {
	setupHandlerTestDB(t)

	a := quotaPrefixUser(t, "dash-a", `{"max_domains":-1}`)
	b := quotaPrefixUser(t, "dash-b", `{"max_domains":-1}`)
	quotaPrefixDomain(t, a.ID, "aa.example.com")
	quotaPrefixDomain(t, b.ID, "bb.example.com")

	admin := models.User{Username: "dash-admin", Email: "dash-admin@example.com", Level: 2, Status: 1}
	if err := database.DB.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	c, w := dashCtx(t, admin.ID, 2)
	GetDashboardStats(c)

	if got := decodeDashStats(t, w); got["domains"] != 2 {
		t.Errorf("管理员应看到全站 2 个域名，实际 %d", got["domains"])
	}
}

/* TestDashboardStatsCountsDelegatedDomains 被单独授权的域名计入普通用户的域名数 */
func TestDashboardStatsCountsDelegatedDomains(t *testing.T) {
	setupHandlerTestDB(t)

	owner := quotaPrefixUser(t, "dash-owner", `{"max_domains":-1}`)
	guest := quotaPrefixUser(t, "dash-guest", `{"max_domains":-1}`)
	domain := quotaPrefixDomain(t, owner.ID, "shared-dash.example.com")

	// 域主自己看：1 个
	c, w := dashCtx(t, owner.ID, 1)
	GetDashboardStats(c)
	if got := decodeDashStats(t, w); got["domains"] != 1 {
		t.Errorf("域主应看到 1 个域名，实际 %d", got["domains"])
	}

	// 被授权者看：也是 1 个（授权域名计入他的可见范围，与配额 domains 分支同口径）
	if err := database.DB.Create(&models.Permission{
		UserID: guest.ID, DomainID: domain.ID, Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("创建授权失败: %v", err)
	}
	c2, w2 := dashCtx(t, guest.ID, 1)
	GetDashboardStats(c2)
	if got := decodeDashStats(t, w2); got["domains"] != 1 {
		t.Errorf("被授权的域名应计入可见范围，实际 %d", got["domains"])
	}
}
