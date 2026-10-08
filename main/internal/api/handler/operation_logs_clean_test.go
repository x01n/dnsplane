package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"main/internal/database"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/*
 * 操作日志清空与读取范围的用例
 *
 * 清空是破坏性操作，全部用例只作用于 setupHandlerTestDB 建的临时 SQLite，
 * 覆盖成功、非管理员、重复清空三条路径，并补读取侧的越权收窄
 */

/* seedOperationLog 写入一条操作日志 */
func seedOperationLog(t *testing.T, uid uint, username, action, domain string, createdAt time.Time) {
	t.Helper()
	if err := database.LogDB.Create(&models.Log{
		UserID:    uid,
		Username:  username,
		Action:    action,
		Domain:    domain,
		CreatedAt: createdAt,
	}).Error; err != nil {
		t.Fatalf("写入操作日志失败: %v", err)
	}
}

/* countOperationLogs 统计操作日志总条数 */
func countOperationLogs(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := database.LogDB.Model(&models.Log{}).Count(&n).Error; err != nil {
		t.Fatalf("统计操作日志失败: %v", err)
	}
	return n
}

/* operationLogReq 构造一次操作日志接口请求；level 为 0 表示未带身份上下文 */
func operationLogReq(t *testing.T, method, target, body string, level int, uid uint) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if method == http.MethodGet {
		c.Request = httptest.NewRequest(method, target, nil)
	} else {
		c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
	}
	if level > 0 {
		c.Set("level", level)
		c.Set("user_id", strconv.FormatUint(uint64(uid), 10))
		c.Set("username", "tester")
	}
	return c, w
}

/* operationLogResp 清空接口的响应体 */
type operationLogResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Msg     string `json:"msg"`
		Deleted int64  `json:"deleted"`
	} `json:"data"`
}

/* decodeOperationLogResp 解析清空接口响应 */
func decodeOperationLogResp(t *testing.T, w *httptest.ResponseRecorder) operationLogResp {
	t.Helper()
	var out operationLogResp
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	return out
}

// TestCleanOperationLogsClearsAllAndAudits 管理员清空后日志表只剩本次清空的审计记录
func TestCleanOperationLogsClearsAllAndAudits(t *testing.T) {
	setupHandlerTestDB(t)
	seedOperationLog(t, 7, "alice", "login", "", time.Now())
	seedOperationLog(t, 7, "alice", "delete_domain", "a.example", time.Now())
	seedOperationLog(t, 9, "bob", "clear_cache", "", time.Now())

	c, w := operationLogReq(t, http.MethodPost, "/api/logs/clean", `{"days":0}`, 2, 1)
	CleanOperationLogs(c)

	resp := decodeOperationLogResp(t, w)
	if resp.Code != 0 {
		t.Fatalf("code = %d，期望 0（msg=%s）", resp.Code, resp.Msg)
	}
	if resp.Data.Deleted != 3 {
		t.Errorf("deleted = %d，期望 3", resp.Data.Deleted)
	}
	if resp.Data.Msg != "日志已清空" {
		t.Errorf("msg = %q，期望 %q", resp.Data.Msg, "日志已清空")
	}

	var rest []models.Log
	database.LogDB.Find(&rest)
	if len(rest) != 1 {
		t.Fatalf("清空后剩余 %d 条，期望 1 条审计记录", len(rest))
	}
	if rest[0].Action != "clean_logs" {
		t.Errorf("剩余记录 action = %q，期望 %q", rest[0].Action, "clean_logs")
	}
	if rest[0].UserID != 1 {
		t.Errorf("剩余记录 uid = %d，期望 1（清空操作者）", rest[0].UserID)
	}
}

// TestCleanOperationLogsRejectsNonAdmin 非管理员被拒绝且一条都不能删
func TestCleanOperationLogsRejectsNonAdmin(t *testing.T) {
	setupHandlerTestDB(t)
	seedOperationLog(t, 7, "alice", "login", "", time.Now())
	seedOperationLog(t, 9, "bob", "clear_cache", "", time.Now())

	c, w := operationLogReq(t, http.MethodPost, "/api/logs/clean", `{"days":0}`, 1, 7)
	CleanOperationLogs(c)

	resp := decodeOperationLogResp(t, w)
	if resp.Code != -1 {
		t.Fatalf("code = %d，期望 -1", resp.Code)
	}
	if !strings.Contains(resp.Msg, "权限不足") {
		t.Errorf("msg = %q，期望包含 %q", resp.Msg, "权限不足")
	}
	if n := countOperationLogs(t); n != 2 {
		t.Errorf("非管理员请求后剩余 %d 条，期望 2（未删除）", n)
	}
}

/*
 * TestCleanOperationLogsRepeatIsNoop 重复清空仍返回成功
 *
 * 清空接口自身会留下一条审计记录，所以第二次清空的 deleted 恰好是上一次留下的这一条，
 * 而不是 0；关键是它不能报错，也不能把库删成负数或残留业务日志
 */
func TestCleanOperationLogsRepeatIsNoop(t *testing.T) {
	setupHandlerTestDB(t)
	seedOperationLog(t, 7, "alice", "login", "", time.Now())
	seedOperationLog(t, 9, "bob", "clear_cache", "", time.Now())

	first, firstW := operationLogReq(t, http.MethodPost, "/api/logs/clean", `{"days":0}`, 2, 1)
	CleanOperationLogs(first)
	if got := decodeOperationLogResp(t, firstW).Data.Deleted; got != 2 {
		t.Fatalf("首次清空 deleted = %d，期望 2", got)
	}

	second, secondW := operationLogReq(t, http.MethodPost, "/api/logs/clean", `{"days":0}`, 2, 1)
	CleanOperationLogs(second)
	resp := decodeOperationLogResp(t, secondW)

	if resp.Code != 0 {
		t.Fatalf("重复清空 code = %d，期望 0（msg=%s）", resp.Code, resp.Msg)
	}
	if resp.Data.Deleted != 1 {
		t.Errorf("重复清空 deleted = %d，期望 1（上一次清空留下的审计记录）", resp.Data.Deleted)
	}

	var rest []models.Log
	database.LogDB.Find(&rest)
	if len(rest) != 1 || rest[0].Action != "clean_logs" {
		t.Errorf("重复清空后剩余 %d 条且首条 action = %q，期望仅剩本次清空的审计记录",
			len(rest), rest[0].Action)
	}
}

// TestCleanOperationLogsDaysKeepsRecent days 为正时只删更早的记录
func TestCleanOperationLogsDaysKeepsRecent(t *testing.T) {
	setupHandlerTestDB(t)
	seedOperationLog(t, 7, "alice", "login", "", time.Now().AddDate(0, 0, -3))
	seedOperationLog(t, 7, "alice", "delete_domain", "a.example", time.Now())

	c, w := operationLogReq(t, http.MethodPost, "/api/logs/clean", `{"days":1}`, 2, 1)
	CleanOperationLogs(c)

	resp := decodeOperationLogResp(t, w)
	if resp.Code != 0 {
		t.Fatalf("code = %d，期望 0（msg=%s）", resp.Code, resp.Msg)
	}
	if resp.Data.Deleted != 1 {
		t.Errorf("deleted = %d，期望 1（仅 3 天前那条）", resp.Data.Deleted)
	}

	var rest []models.Log
	database.LogDB.Where("action = ?", "delete_domain").Find(&rest)
	if len(rest) != 1 {
		t.Fatalf("按天数清空后命中的近期待删记录 %d 条，期望保留 1 条", len(rest))
	}
}

// TestCleanOperationLogsLogDBNilFails 日志库未初始化时必须明确报错，不能静默返回成功
func TestCleanOperationLogsLogDBNilFails(t *testing.T) {
	setupHandlerTestDB(t)
	saved := database.LogDB
	database.LogDB = nil
	defer func() { database.LogDB = saved }()

	c, w := operationLogReq(t, http.MethodPost, "/api/logs/clean", `{"days":0}`, 2, 1)
	CleanOperationLogs(c)

	resp := decodeOperationLogResp(t, w)
	if resp.Code != -1 {
		t.Fatalf("code = %d，期望 -1", resp.Code)
	}
	if resp.Msg != "日志库未初始化" {
		t.Errorf("msg = %q，期望 %q", resp.Msg, "日志库未初始化")
	}
}

// TestGetLogsScopesToOwnUser 非管理员只读到自己的日志，统计口径同步收窄
func TestGetLogsScopesToOwnUser(t *testing.T) {
	setupHandlerTestDB(t)
	for i := 0; i < 2; i++ {
		seedOperationLog(t, 7, "alice", "login", "a.example", time.Now())
	}
	for i := 0; i < 3; i++ {
		seedOperationLog(t, 9, "bob", "clear_cache", "b.example", time.Now())
	}

	type logsResp struct {
		Code int `json:"code"`
		Data struct {
			Total int64        `json:"total"`
			List  []models.Log `json:"list"`
			Stats struct {
				TodayCount      int64 `json:"today_count"`
				DistinctUsers   int64 `json:"distinct_users"`
				DistinctDomains int64 `json:"distinct_domains"`
			} `json:"stats"`
		} `json:"data"`
	}

	c, w := operationLogReq(t, http.MethodGet, "/api/logs?page=1&page_size=20", "", 1, 7)
	GetLogs(c)
	var mine logsResp
	if err := json.Unmarshal(w.Body.Bytes(), &mine); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	if mine.Data.Total != 2 {
		t.Errorf("非管理员看到 total = %d，期望 2", mine.Data.Total)
	}
	if len(mine.Data.List) != 2 {
		t.Fatalf("非管理员看到 list 长度 = %d，期望 2", len(mine.Data.List))
	}
	for _, l := range mine.Data.List {
		if l.UserID != 7 {
			t.Errorf("列表中出现了 uid = %d 的记录，非管理员只能看到自己的", l.UserID)
		}
	}
	if mine.Data.Stats.DistinctUsers != 1 {
		t.Errorf("distinct_users = %d，期望 1", mine.Data.Stats.DistinctUsers)
	}
	if mine.Data.Stats.DistinctDomains != 1 {
		t.Errorf("distinct_domains = %d，期望 1", mine.Data.Stats.DistinctDomains)
	}
	if mine.Data.Stats.TodayCount != 2 {
		t.Errorf("today_count = %d，期望 2", mine.Data.Stats.TodayCount)
	}

	adminCtx, adminW := operationLogReq(t, http.MethodGet, "/api/logs?page=1&page_size=20", "", 2, 1)
	GetLogs(adminCtx)
	var all logsResp
	if err := json.Unmarshal(adminW.Body.Bytes(), &all); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, adminW.Body.String())
	}
	if all.Data.Total != 5 {
		t.Errorf("管理员看到 total = %d，期望 5", all.Data.Total)
	}
	if all.Data.Stats.DistinctUsers != 2 {
		t.Errorf("管理员 distinct_users = %d，期望 2", all.Data.Stats.DistinctUsers)
	}
}
