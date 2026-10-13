package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"main/internal/database"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/*
 * 日志详情的归属隔离
 *
 * 控制台对日志的约定是「非管理员只看得到自己的操作」，这套隔离原先只写在
 * GetLogs 内部的一个局部闭包里，而 GetLogDetail 直接按主键取行——同一条约定
 * 在列表上生效、在详情上失效，任意登录用户遍历 /api/logs/:id 就能跨用户读日志
 *
 * 这两个用例锁住详情侧必须与列表侧口径一致：
 *   他人的日志 id → 与不存在的 id 返回同一句话（避免 id 有效性成为预言机）
 *   自己的日志 id → 正常返回
 */

/* seedLogRow 往日志库写一行 */
func seedLogRow(t *testing.T, uid uint, username, action string) models.Log {
	t.Helper()
	row := models.Log{UserID: uid, Username: username, Action: action, IP: "192.0.2.1"}
	if err := database.LogDB.Create(&row).Error; err != nil {
		t.Fatalf("写入日志失败: %v", err)
	}
	return row
}

/* getLogDetailRequest 构造 GET /logs/:id 的请求上下文 */
func getLogDetailRequest(t *testing.T, logID, callerID uint, level int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	idStr := strconv.FormatUint(uint64(logID), 10)
	c.Request = httptest.NewRequest(http.MethodGet, "/logs/"+idStr, nil)
	c.Params = gin.Params{{Key: "id", Value: idStr}}
	c.Set("user_id", strconv.FormatUint(uint64(callerID), 10))
	c.Set("level", level)
	c.Set("username", "caller")

	GetLogDetail(c)
	return w
}

/* TestGetLogDetailRejectsOtherUsersLog 普通用户读不到他人的日志明细 */
func TestGetLogDetailRejectsOtherUsersLog(t *testing.T) {
	setupHandlerTestDB(t)

	victimRow := seedLogRow(t, 2, "victim", "update_record")

	w := getLogDetailRequest(t, victimRow.ID, 3, 1)

	var resp struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	if resp.Code == 0 {
		t.Errorf("读他人日志应被拒绝，实际返回了数据：%s", string(resp.Data))
	}
	if resp.Msg != "日志不存在" {
		t.Errorf("错误提示应为「日志不存在」（与不存在的 id 一致，避免成为预言机），实际 %q", resp.Msg)
	}
}

/* TestGetLogDetailAllowsOwnLog 普通用户能读自己的日志明细 */
func TestGetLogDetailAllowsOwnLog(t *testing.T) {
	setupHandlerTestDB(t)

	ownRow := seedLogRow(t, 7, "self", "update_domain")

	w := getLogDetailRequest(t, ownRow.ID, 7, 1)

	var resp struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Errorf("读自己的日志应成功，实际 code=%d body=%s", resp.Code, w.Body.String())
	}
}

/* TestGetLogDetailAdminSeesAll 管理员不受归属限制 */
func TestGetLogDetailAdminSeesAll(t *testing.T) {
	setupHandlerTestDB(t)

	anyRow := seedLogRow(t, 2, "victim", "delete_account")

	w := getLogDetailRequest(t, anyRow.ID, 1, 2)

	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Errorf("管理员应能读任意日志，实际 code=%d body=%s", resp.Code, w.Body.String())
	}
}
