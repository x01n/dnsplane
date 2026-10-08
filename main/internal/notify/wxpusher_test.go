package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * WxPusher 渠道 HTTP 契约测试
 * 用本地 httptest 服务端接收请求，不使用任何外部网络
 */

/* TestWxPusherPostsExpectedFields 校验 URL、Content-Type 与请求体字段 */
func TestWxPusherPostsExpectedFields(t *testing.T) {
	var gotPath, gotCT string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1000,"msg":"处理成功","success":true,"data":[]}`))
	}))
	defer srv.Close()

	n := NewWxPusherNotifier(WxPusherConfig{AppToken: "AT_x", UID: "UID_y"})
	n.client = newRewriteClient(t, srv)

	if err := n.Send(context.Background(), "标题", "正文内容"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	if gotPath != "/api/send/message" {
		t.Errorf("请求路径不符: %s", gotPath)
	}
	if gotCT != "application/json; charset=UTF-8" {
		t.Errorf("Content-Type 不符: %q", gotCT)
	}
	if gotBody["appToken"] != "AT_x" {
		t.Errorf("appToken 不符: %v", gotBody["appToken"])
	}
	if gotBody["content"] != "正文内容" {
		t.Errorf("content 不符: %v", gotBody["content"])
	}
	if gotBody["summary"] != "标题" {
		t.Errorf("summary 不符: %v", gotBody["summary"])
	}
	if v, ok := gotBody["contentType"].(float64); !ok || int(v) != 3 {
		t.Errorf("contentType 应为数字 3: %v", gotBody["contentType"])
	}
	uids, ok := gotBody["uids"].([]interface{})
	if !ok || len(uids) != 1 || uids[0] != "UID_y" {
		t.Errorf("uids 应为单元素数组: %v", gotBody["uids"])
	}
}

/* TestWxPusherSurfacesAPIError success=false 时必须返回 msg 内容 */
func TestWxPusherSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1005,"msg":"appToken 不合法","success":false}`))
	}))
	defer srv.Close()

	n := NewWxPusherNotifier(WxPusherConfig{AppToken: "bad", UID: "u"})
	n.client = newRewriteClient(t, srv)

	err := n.Send(context.Background(), "s", "c")
	if err == nil {
		t.Fatal("success=false 时应返回错误")
	}
	if !strings.Contains(err.Error(), "appToken 不合法") {
		t.Errorf("错误信息未透出 msg 字段: %v", err)
	}
}

/* TestWxPusherSuccessWithoutMsg success=true 时即使无 msg 也应成功 */
func TestWxPusherSuccessWithoutMsg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1000,"success":true}`))
	}))
	defer srv.Close()

	n := NewWxPusherNotifier(WxPusherConfig{AppToken: "a", UID: "u"})
	n.client = newRewriteClient(t, srv)
	if err := n.Send(context.Background(), "s", "c"); err != nil {
		t.Errorf("success=true 应成功: %v", err)
	}
}

/* TestWxPusherFailureWithoutMsg 失败且无 msg 时回退为「请求失败 */
func TestWxPusherFailureWithoutMsg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1005,"success":false}`))
	}))
	defer srv.Close()

	n := NewWxPusherNotifier(WxPusherConfig{AppToken: "a", UID: "u"})
	n.client = newRewriteClient(t, srv)
	err := n.Send(context.Background(), "s", "c")
	if err == nil {
		t.Fatal("success=false 时应返回错误")
	}
	if !strings.Contains(err.Error(), "请求失败") {
		t.Errorf("无 msg 时应回退为「请求失败」: %v", err)
	}
}

/* TestWxPusherHTTPError HTTP 4xx/5xx 应返回错误并带上响应体 */
func TestWxPusherHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`bad gateway`))
	}))
	defer srv.Close()

	n := NewWxPusherNotifier(WxPusherConfig{AppToken: "a", UID: "u"})
	n.client = newRewriteClient(t, srv)
	err := n.Send(context.Background(), "s", "c")
	if err == nil {
		t.Fatal("HTTP 502 应返回错误")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("错误信息未包含状态码: %v", err)
	}
}

/* TestWxPusherInvalidJSON 响应非 JSON 时应返回解析失败错误 */
func TestWxPusherInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	n := NewWxPusherNotifier(WxPusherConfig{AppToken: "a", UID: "u"})
	n.client = newRewriteClient(t, srv)
	err := n.Send(context.Background(), "s", "c")
	if err == nil || !strings.Contains(err.Error(), "解析失败") {
		t.Errorf("非 JSON 响应应返回解析失败: %v", err)
	}
}
