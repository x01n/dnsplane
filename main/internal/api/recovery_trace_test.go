package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

// newProbeRouter 构建与生产同源的中间件链，并开放 /api 组供用例注册探针路由。
// 中间件顺序取自 applyAPIMiddleware，用例不复制顺序：链序被改坏时下列断言会失败
func newProbeRouter(t *testing.T) (*gin.Engine, *gin.RouterGroup) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := newBaseEngine()
	api := r.Group("/api")
	applyAPIMiddleware(api)
	return r, api
}

// waitRequestLog 等待 RequestTrace 的异步落库完成，返回该路径的全部请求日志
func waitRequestLog(t *testing.T, path string) []models.RequestLog {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var logs []models.RequestLog
		database.RequestDB.Where("path = ?", path).Find(&logs)
		if len(logs) > 0 {
			return logs
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 %s 的请求日志落库超时", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAPIPanicPersistsRequestLog panic 请求必须留下 RequestLog 记录（含 ErrorID 与堆栈）。
// 请求日志是运维唯一的结构化排查入口：内层 Recovery 缺失时该请求在库中零留痕
func TestAPIPanicPersistsRequestLog(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/panic-probe", func(*gin.Context) { panic("probe panic") })

	w := doGet(r, "/api/panic-probe", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 500", w.Code)
	}
	// 响应体必须与一次 recovery 写出的内容完全一致，避免内外两层重复写入
	if body := w.Body.String(); body != `{"code":-1,"msg":"服务器内部错误"}` {
		t.Errorf("响应体 = %q，期望单次 recovery 的 JSON", body)
	}

	logs := waitRequestLog(t, "/api/panic-probe")
	if len(logs) != 1 {
		t.Fatalf("请求日志条数 = %d，期望 1", len(logs))
	}
	log := logs[0]
	if !log.IsError {
		t.Error("IsError = false，期望 true")
	}
	if log.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d，期望 500", log.StatusCode)
	}
	if !strings.Contains(log.ErrorMsg, "PANIC") {
		t.Errorf("ErrorMsg = %q，未包含 PANIC", log.ErrorMsg)
	}
	if !strings.Contains(log.ErrorMsg, "probe panic") {
		t.Errorf("ErrorMsg = %q，未包含 panic 原因", log.ErrorMsg)
	}
	if log.ErrorStack == "" {
		t.Error("ErrorStack 为空，期望记录堆栈")
	}
	if log.ErrorID == "" {
		t.Error("ErrorID 为空，期望已生成")
	}
	if log.RequestID == "" {
		t.Error("RequestID 为空，期望已生成")
	}
}

// TestPageRoutePanicStillReturns500 静态资源与页面路由不经 RequestTrace，
// 其 panic 仍须由链首的 gin.Recovery 兜住并返回 500
func TestPageRoutePanicStillReturns500(t *testing.T) {
	setupRouterTestDB(t)
	r := newTestRouter(t)
	r.GET("/page-panic-probe", func(*gin.Context) { panic("page probe panic") })

	w := doGet(r, "/page-panic-probe", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 500", w.Code)
	}
	assertSecurityHeaders(t, w)
}

// TestNonPanicErrorUnaffected handler 自行返回的 500 不受 panic 兜底影响，错误信息保持业务语义
func TestNonPanicErrorUnaffected(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/manual-error-probe", func(c *gin.Context) {
		middleware.SetError(c, "手动标记的业务错误")
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "业务失败"})
	})

	w := doGet(r, "/api/manual-error-probe", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 500", w.Code)
	}

	logs := waitRequestLog(t, "/api/manual-error-probe")
	if len(logs) != 1 {
		t.Fatalf("请求日志条数 = %d，期望 1", len(logs))
	}
	log := logs[0]
	if !log.IsError {
		t.Error("IsError = false，期望 true")
	}
	if log.ErrorMsg != "手动标记的业务错误" {
		t.Errorf("ErrorMsg = %q，期望业务错误原文", log.ErrorMsg)
	}
	if strings.Contains(log.ErrorMsg, "PANIC") {
		t.Errorf("ErrorMsg = %q，非 panic 请求不应被 panic 兜底改写", log.ErrorMsg)
	}
	if log.ErrorStack == "" {
		t.Error("ErrorStack 为空，期望记录调用堆栈")
	}
}
