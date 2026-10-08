package api

import (
	"net/http"
	"strings"
	"testing"

	"main/internal/api/middleware"

	"github.com/gin-gonic/gin"
)

// TestPanicResponseCarriesErrorIDHeader panic 请求的 X-Error-ID 必须真正送达客户端，
// 且与落库 ErrorID 一致：否则用户报错时无法提供可追溯的 ID。
// 断言用 Result().Header —— recorder.Header 是「将要写入的」活 map，状态码送出后补写的头不在其中
func TestPanicResponseCarriesErrorIDHeader(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/panic-error-id-probe", func(*gin.Context) { panic("error id probe") })

	w := doGet(r, "/api/panic-error-id-probe", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 500", w.Code)
	}
	got := w.Result().Header.Get("X-Error-ID")
	if got == "" {
		t.Fatal("Result().Header 中 X-Error-ID 为空，客户端拿不到错误 ID")
	}
	if !strings.HasPrefix(got, "err_") {
		t.Errorf("X-Error-ID = %q，期望 err_ 前缀", got)
	}

	logs := waitRequestLog(t, "/api/panic-error-id-probe")
	if logs[0].ErrorID != got {
		t.Errorf("落库 ErrorID = %q，与响应头 %q 不一致，运维无法凭该 ID 反查", logs[0].ErrorID, got)
	}
}

// TestBusinessErrorResponseCarriesErrorIDHeader 业务错误响应经 ErrorResponse 标记后同样必须送达 X-Error-ID。
// 这类调用点是客户端拿到错误 ID 的主路径，多于 panic 路径
func TestBusinessErrorResponseCarriesErrorIDHeader(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/biz-error-id-probe", func(c *gin.Context) {
		middleware.ErrorResponse(c, "业务失败")
	})

	w := doGet(r, "/api/biz-error-id-probe", nil)
	got := w.Result().Header.Get("X-Error-ID")
	if got == "" {
		t.Fatal("Result().Header 中 X-Error-ID 为空，业务错误响应未带错误 ID")
	}

	logs := waitRequestLog(t, "/api/biz-error-id-probe")
	if !logs[0].IsError {
		t.Error("IsError = false，期望 true")
	}
	if logs[0].ErrorID != got {
		t.Errorf("落库 ErrorID = %q，与响应头 %q 不一致", logs[0].ErrorID, got)
	}
}

// TestNonErrorResponseHasNoErrorIDHeader 正常响应不得携带 X-Error-ID，避免客户端把成功请求当失败
func TestNonErrorResponseHasNoErrorIDHeader(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/ok-no-error-id-probe", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": "ok"})
	})

	w := doGet(r, "/api/ok-no-error-id-probe", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", w.Code)
	}
	if got := w.Result().Header.Get("X-Error-ID"); got != "" {
		t.Errorf("X-Error-ID = %q，正常响应不应携带", got)
	}
}

// TestWriteThenPanicKeepsSingleResponseBody handler 先写响应再 panic 时，响应体只能是已写出的那一段；
// 追加 recovery 的 JSON 会让客户端解析失败。落库留痕不受影响：panic 仍被记为错误并保留堆栈
func TestWriteThenPanicKeepsSingleResponseBody(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/write-then-panic-probe", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "先写了成功响应"})
		panic("write then panic probe")
	})

	w := doGet(r, "/api/write-then-panic-probe", nil)
	// 200 已发出，状态码无法回退到 500，此处只约束响应体不与 recovery 内容拼接
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200（响应已写出，无法改写为 500）", w.Code)
	}
	if body := w.Body.String(); body != `{"code":0,"msg":"先写了成功响应"}` {
		t.Errorf("响应体 = %q，期望仅 handler 已写出的单段 JSON", body)
	}

	logs := waitRequestLog(t, "/api/write-then-panic-probe")
	if len(logs) != 1 {
		t.Fatalf("请求日志条数 = %d，期望 1", len(logs))
	}
	log := logs[0]
	if !log.IsError {
		t.Error("IsError = false，期望 true")
	}
	if !strings.Contains(log.ErrorMsg, "PANIC") {
		t.Errorf("ErrorMsg = %q，未包含 PANIC", log.ErrorMsg)
	}
	if log.ErrorStack == "" {
		t.Error("ErrorStack 为空，期望记录堆栈")
	}
	if log.ErrorID == "" {
		t.Error("ErrorID 为空，期望已生成")
	}
}
