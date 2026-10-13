package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"main/internal/database"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/*
 * 路径参数为空时不得把空串交给 GORM
 *
 * 立论：First(&x, "") 会退化成不带主键条件的查询，静默命中全表第一条。
 * 这不是推测——本文件最后一个用例直接把它测出来。
 *
 * 触发条件是对外 API 的路由表：它全部是无路径参数的 POST（g.POST("/categories/update", ...)），
 * c.Param("id") 恒返回空串，而 handler 直接把这个空串传给了 First。
 * 结果是「改一条不存在的分类」变成「改掉全表第一条分类」，且响应报成功。
 *
 * 修复形态是取值后立即早退，六个位置逐一覆盖；本文件的用例锁住这个早退行为，
 * 断言的是可观测结果（第一条记录的字段是否被改动），不是实现细节
 */

/* paramGuardContext 构造一个没有路径参数的请求上下文，模拟对外 API 的路由表 */
func paramGuardContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	// 刻意不设 c.Params：这就是 v1 路由表下 handler 看到的真实状态
	c.Set("level", 2)
	c.Set("user_id", "999")
	c.Set("username", "tester")
	return c, w
}

/* decodeHandlerResp 解出控制台响应体的 code 与 msg */
func decodeHandlerResp(t *testing.T, w *httptest.ResponseRecorder) (int, string) {
	t.Helper()
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	return resp.Code, resp.Msg
}

/* TestGORMEmptyPrimaryKeyDegradesToFirstRow 锁定本文所有守卫的立论前提 */
func TestGORMEmptyPrimaryKeyDegradesToFirstRow(t *testing.T) {
	setupHandlerTestDB(t)

	rows := []models.DomainCategory{
		{Name: "first-cat"},
		{Name: "second-cat"},
	}
	for i := range rows {
		if err := database.DB.Create(&rows[i]).Error; err != nil {
			t.Fatalf("创建分类失败: %v", err)
		}
	}

	var got models.DomainCategory
	if err := database.DB.First(&got, "").Error; err != nil {
		t.Fatalf("空主键查询意外报错: %v", err)
	}
	if got.Name != "first-cat" {
		t.Errorf("空主键查询命中 %q，预期退化为全表第一条 %q", got.Name, "first-cat")
	}

	// 数值零值走的是正常主键条件，不会退化——两类写法必须区分开
	var zero models.DomainCategory
	if err := database.DB.First(&zero, uint(0)).Error; err == nil {
		t.Error("uint(0) 主键查询应报 record not found，实际命中了记录")
	}
}

/* TestUpdateDomainCategoryRejectsEmptyID 空 ID 不得改名第一条分类 */
func TestUpdateDomainCategoryRejectsEmptyID(t *testing.T) {
	setupHandlerTestDB(t)

	first := models.DomainCategory{Name: "first-cat"}
	if err := database.DB.Create(&first).Error; err != nil {
		t.Fatalf("创建分类失败: %v", err)
	}

	c, w := paramGuardContext(t, `{"name":"renamed","remark":"x","sort":1}`)
	UpdateDomainCategory(c)

	code, msg := decodeHandlerResp(t, w)
	if code == 0 {
		t.Errorf("空 ID 的更新请求应被拒绝，实际报成功：%s", msg)
	}

	var after models.DomainCategory
	if err := database.DB.First(&after, first.ID).Error; err != nil {
		t.Fatalf("回查分类失败: %v", err)
	}
	if after.Name != "first-cat" {
		t.Errorf("第一条分类被静默改名：名称从 %q 变成 %q", "first-cat", after.Name)
	}
}

/* TestDeleteDomainCategoryRejectsEmptyID 空 ID 不得删除第一条分类 */
func TestDeleteDomainCategoryRejectsEmptyID(t *testing.T) {
	setupHandlerTestDB(t)

	first := models.DomainCategory{Name: "first-cat"}
	if err := database.DB.Create(&first).Error; err != nil {
		t.Fatalf("创建分类失败: %v", err)
	}

	c, w := paramGuardContext(t, `{}`)
	DeleteDomainCategory(c)

	if code, msg := decodeHandlerResp(t, w); code == 0 {
		t.Errorf("空 ID 的删除请求应被拒绝，实际报成功：%s", msg)
	}

	var count int64
	database.DB.Model(&models.DomainCategory{}).Count(&count)
	if count != 1 {
		t.Errorf("第一条分类被静默删除：剩余 %d 条，期望 1 条", count)
	}
}

/* TestUpdateScheduleTaskRejectsEmptyID 空 ID 不得改写第一条切换策略 */
func TestUpdateScheduleTaskRejectsEmptyID(t *testing.T) {
	setupHandlerTestDB(t)

	first := models.ScheduleTask{DomainID: 1, RR: "first-task", RecordID: "r1"}
	if err := database.DB.Create(&first).Error; err != nil {
		t.Fatalf("创建策略失败: %v", err)
	}

	// body 填全，否则修复前会在必填项校验处提前返回，测不出退化路径
	c, w := paramGuardContext(t, `{"did":1,"rr":"renamed","recordid":"r1","type":0,"cycle":0,"switchtype":0}`)
	UpdateScheduleTask(c)

	if code, msg := decodeHandlerResp(t, w); code == 0 {
		t.Errorf("空 ID 的更新请求应被拒绝，实际报成功：%s", msg)
	}

	var after models.ScheduleTask
	if err := database.DB.First(&after, first.ID).Error; err != nil {
		t.Fatalf("回查策略失败: %v", err)
	}
	if after.RR != "first-task" {
		t.Errorf("第一条策略被静默改写：rr 从 %q 变成 %q", "first-task", after.RR)
	}
}

/* TestDeleteScheduleTaskRejectsEmptyID 空 ID 不得删除第一条切换策略 */
func TestDeleteScheduleTaskRejectsEmptyID(t *testing.T) {
	setupHandlerTestDB(t)

	first := models.ScheduleTask{DomainID: 1, RR: "first-task", RecordID: "r1"}
	if err := database.DB.Create(&first).Error; err != nil {
		t.Fatalf("创建策略失败: %v", err)
	}

	c, w := paramGuardContext(t, `{}`)
	DeleteScheduleTask(c)

	if code, msg := decodeHandlerResp(t, w); code == 0 {
		t.Errorf("空 ID 的删除请求应被拒绝，实际报成功：%s", msg)
	}

	var count int64
	database.DB.Model(&models.ScheduleTask{}).Count(&count)
	if count != 1 {
		t.Errorf("第一条策略被静默删除：剩余 %d 条，期望 1 条", count)
	}
}

/* TestToggleScheduleTaskRejectsEmptyID 空 ID 不得翻转第一条策略的开关 */
func TestToggleScheduleTaskRejectsEmptyID(t *testing.T) {
	setupHandlerTestDB(t)

	first := models.ScheduleTask{DomainID: 1, RR: "first-task", RecordID: "r1", Active: false}
	if err := database.DB.Create(&first).Error; err != nil {
		t.Fatalf("创建策略失败: %v", err)
	}

	c, w := paramGuardContext(t, `{}`)
	ToggleScheduleTask(c)

	if code, msg := decodeHandlerResp(t, w); code == 0 {
		t.Errorf("空 ID 的开关请求应被拒绝，实际报成功：%s", msg)
	}

	var after models.ScheduleTask
	if err := database.DB.First(&after, first.ID).Error; err != nil {
		t.Fatalf("回查策略失败: %v", err)
	}
	if after.Active {
		t.Error("第一条策略的开关被静默翻转：active 从 false 变成 true")
	}
}

/* TestUpdateRecordWeightRejectsEmptyDomainID 空域名 ID 不得落到第一条域名上 */
func TestUpdateRecordWeightRejectsEmptyDomainID(t *testing.T) {
	setupHandlerTestDB(t)

	c, w := paramGuardContext(t, `{"record_id":"r1","weight":10}`)
	UpdateRecordWeight(c)

	code, msg := decodeHandlerResp(t, w)
	if code == 0 {
		t.Errorf("空域名 ID 的权重请求应被拒绝，实际报成功：%s", msg)
	}
	// 修复后是显式拒绝；修复前空串会流进 First，报的是别的错误
	if msg != "缺少域名ID" {
		t.Errorf("错误提示应为「缺少域名ID」，实际 %q", msg)
	}
}
