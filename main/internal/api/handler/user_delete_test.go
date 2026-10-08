package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"main/internal/database"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/*
 * 删除用户必须释放第三方账号的绑定
 *
 * 绑定判据是 provider + provider_user_id，记录只要还在，任何其他用户再去绑定
 * 同一个第三方账号都会被拒为「已被他人绑定」；而唯一的解绑入口要求本人登录，
 * 用户删除后这条路走不通，那个第三方账号就永久无法在此系统绑定
 */
func TestDeleteUserReleasesOAuthBinding(t *testing.T) {
	setupHandlerTestDB(t)

	target := models.User{Username: "to-delete", Email: "to-delete@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&target).Error; err != nil {
		t.Fatalf("创建待删用户失败: %v", err)
	}
	if err := database.DB.Create(&models.UserOAuth{
		UserID:         target.ID,
		Provider:       "github",
		ProviderUserID: "third-party-id",
	}).Error; err != nil {
		t.Fatalf("创建绑定失败: %v", err)
	}

	w := deleteUserRequest(t, target.ID, 2, 999)

	var left int64
	database.DB.Model(&models.UserOAuth{}).
		Where("user_id = ?", target.ID).Count(&left)
	if left != 0 {
		t.Errorf("删除用户后仍有 %d 条绑定记录，第三方账号将被永久占用", left)
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Errorf("删除应成功，实际 code=%d msg=%s", resp.Code, resp.Msg)
	}
}

/*
 * 删除用户不得连带删除其业务数据
 *
 * DNS 账户、证书账户与订单承载的是可接管的资产，删用户不等于销毁它们。
 * 这里锁住「只清理会锁死的绑定与权限」这一边界，防止后续演变成级联全删
 */
func TestDeleteUserKeepsBusinessAssets(t *testing.T) {
	setupHandlerTestDB(t)

	target := models.User{Username: "asset-owner", Email: "asset-owner@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&target).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := database.DB.Create(&models.Account{
		UserID: target.ID, Type: "cloudflare", Name: "cf-account",
	}).Error; err != nil {
		t.Fatalf("创建 DNS 账户失败: %v", err)
	}

	deleteUserRequest(t, target.ID, 2, 999)

	var accounts int64
	database.DB.Model(&models.Account{}).Where("uid = ?", target.ID).Count(&accounts)
	if accounts != 1 {
		t.Errorf("业务资产被误删：DNS 账户剩余 %d 条，期望 1 条", accounts)
	}
}

/*
 * 删除用户不应影响他人的绑定
 *
 * 回归防线：清理必须按 user_id 精确限定，写成无条件 Delete 会把所有用户的绑定一起清空
 */
func TestDeleteUserLeavesOtherBindings(t *testing.T) {
	setupHandlerTestDB(t)

	doomed := models.User{Username: "doomed", Email: "doomed@example.com", Level: 1, Status: 1}
	keeper := models.User{Username: "keeper", Email: "keeper@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&doomed).Error; err != nil {
		t.Fatalf("创建待删用户失败: %v", err)
	}
	if err := database.DB.Create(&keeper).Error; err != nil {
		t.Fatalf("创建保留用户失败: %v", err)
	}
	for _, b := range []models.UserOAuth{
		{UserID: doomed.ID, Provider: "github", ProviderUserID: "doomed-id"},
		{UserID: keeper.ID, Provider: "github", ProviderUserID: "keeper-id"},
	} {
		if err := database.DB.Create(&b).Error; err != nil {
			t.Fatalf("创建绑定失败: %v", err)
		}
	}

	deleteUserRequest(t, doomed.ID, 2, 999)

	var kept int64
	database.DB.Model(&models.UserOAuth{}).
		Where("user_id = ?", keeper.ID).Count(&kept)
	if kept != 1 {
		t.Errorf("他人绑定被误删：剩余 %d 条，期望 1 条", kept)
	}
}

/* deleteUserRequest 构造一次带路径参数与管理员上下文的删除请求 */
func deleteUserRequest(t *testing.T, targetID uint, level int, uid uint) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	idStr := strconv.FormatUint(uint64(targetID), 10)
	c.Request = httptest.NewRequest(http.MethodPost, "/users/"+idStr+"/delete", strings.NewReader("{}"))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: idStr}}
	c.Set("level", level)
	c.Set("user_id", strconv.FormatUint(uint64(uid), 10))
	c.Set("username", "tester")

	DeleteUser(c)
	return w
}