package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// 本文件覆盖不经 SetError、由 handler 直接写 4xx/5xx 的路径。
// 这类调用点全仓 218 处，无法逐处添加 X-Error-ID，
// 因此改由 RequestTrace 的 respCaptureWriter 在状态码写出前统一补写。
// 断言一律用 Result().Header：recorder.Header 是「将要写入的」活 map，快照之后补的头不在其中

// TestDirectJSONErrorCarriesErrorIDHeader 直接 c.JSON(4xx/5xx) 的响应必须带 X-Error-ID，
// 且与落库 ErrorID 一致，运维才能凭客户端提供的 ID 反查
func TestDirectJSONErrorCarriesErrorIDHeader(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/direct-json-400-probe", func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
	})
	api.GET("/direct-json-500-probe", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "服务器错误"})
	})

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/direct-json-400-probe", http.StatusBadRequest},
		{"/api/direct-json-500-probe", http.StatusInternalServerError},
	} {
		w := doGet(r, tc.path, nil)
		if w.Code != tc.want {
			t.Fatalf("%s 状态码 = %d，期望 %d", tc.path, w.Code, tc.want)
		}
		got := w.Result().Header.Get("X-Error-ID")
		if got == "" {
			t.Errorf("%s 的 Result().Header 中 X-Error-ID 为空，客户端拿不到错误 ID", tc.path)
			continue
		}
		logs := waitRequestLog(t, tc.path)
		if !logs[0].IsError {
			t.Errorf("%s 落库 IsError = false，期望 true", tc.path)
		}
		if logs[0].ErrorID != got {
			t.Errorf("%s 落库 ErrorID = %q，与响应头 %q 不一致", tc.path, logs[0].ErrorID, got)
		}
	}
}

// TestStatusOnlyErrorCarriesErrorIDHeader 只写状态码不写 body 的两条支路：
// c.AbortWithStatus 显式写出状态码，c.Status 仅记录状态码、由引擎补写
func TestStatusOnlyErrorCarriesErrorIDHeader(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/abort-status-probe", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	api.GET("/status-only-probe", func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/abort-status-probe", http.StatusUnauthorized},
		{"/api/status-only-probe", http.StatusNotFound},
	} {
		w := doGet(r, tc.path, nil)
		if w.Code != tc.want {
			t.Fatalf("%s 状态码 = %d，期望 %d", tc.path, w.Code, tc.want)
		}
		got := w.Result().Header.Get("X-Error-ID")
		if got == "" {
			t.Errorf("%s 的 Result().Header 中 X-Error-ID 为空", tc.path)
			continue
		}
		logs := waitRequestLog(t, tc.path)
		if logs[0].ErrorID != got {
			t.Errorf("%s 落库 ErrorID = %q，与响应头 %q 不一致", tc.path, logs[0].ErrorID, got)
		}
	}
}

// TestSuccessAndBoundaryStatusHaveNoErrorIDHeader 成功语义的状态码不得携带 X-Error-ID：
// 3xx 与 2xx 会把成功请求误标成失败，400 是补写阈值的下界，其上一档必须仍为空
func TestSuccessAndBoundaryStatusHaveNoErrorIDHeader(t *testing.T) {
	setupRouterTestDB(t)
	r, api := newProbeRouter(t)
	api.GET("/json-399-probe", func(c *gin.Context) {
		c.JSON(399, gin.H{"code": 0})
	})
	api.GET("/no-content-probe", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for _, path := range []string{"/api/json-399-probe", "/api/no-content-probe"} {
		w := doGet(r, path, nil)
		if got := w.Result().Header.Get("X-Error-ID"); got != "" {
			t.Errorf("%s（状态码 %d）X-Error-ID = %q，成功语义的状态码不应携带", path, w.Code, got)
		}
	}
}
