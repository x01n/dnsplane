package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var qqBotOpenIDEscapes = []struct{ in, out string }{
	{"A1B2C3D4E5F6G7H8I9J0", "A1B2C3D4E5F6G7H8I9J0"},
	{"a-b_c.d~e", "a-b_c.d~e"},
	{"user@example.com", "user%40example.com"},
	{"a/b?c#d", "a%2Fb%3Fc%23d"},
	{"a b", "a%20b"},
	{"中文openid", "%E4%B8%AD%E6%96%87openid"},
	{"a+b", "a%2Bb"},
	{"A&B=C;D:E", "A%26B%3DC%3BD%3AE"},
	{"!*'();:@&=+$,/?#[]", "%21%2A%27%28%29%3B%3A%40%26%3D%2B%24%2C%2F%3F%23%5B%5D"},
	{"%zz", "%25zz"},
	{"~", "~"},
	{"-_.~", "-_.~"},
	{"", ""},
	{"0123456789abcdef", "0123456789abcdef"},
}

/* TestQQBotEscapeOpenIDEscapesPathSegment 校验 openid 按 RFC 3986 未保留字符集转义（固定基线比对） */
func TestQQBotEscapeOpenIDEscapesPathSegment(t *testing.T) {
	for _, c := range qqBotOpenIDEscapes {
		if got := qqBotEscapeOpenID(c.in); got != c.out {
			t.Errorf("in=%q got=%q want=%q", c.in, got, c.out)
		}
	}
}

/* TestQQBotTokenRequest 校验 getAppAccessToken 的请求路径、头与请求体字段 */
func TestQQBotTokenRequest(t *testing.T) {
	var gotPath, gotCT string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "getAppAccessToken") {
			_, _ = w.Write([]byte(`{"id":"m"}`))
			return
		}
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"access_token":"tok_abc","expires_in":7200}`))
	}))
	defer srv.Close()

	n := NewQQBotNotifier(QQBotConfig{AppID: "102000001", AppSecret: "sec", OpenID: "openid_1"})
	n.client = newRewriteClient(t, srv)
	n.useCache = false

	if err := n.Send(context.Background(), "标题", "正文"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	if gotPath != "/app/getAppAccessToken" {
		t.Errorf("token 请求路径不符: %s", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("token 请求 Content-Type 不符: %q", gotCT)
	}
	if gotBody["appId"] != "102000001" || gotBody["clientSecret"] != "sec" {
		t.Errorf("token 请求体字段不符: %v", gotBody)
	}
}

/* TestQQBotMessageRequest 校验发消息的路径、Authorization 头与请求体 */
func TestQQBotMessageRequest(t *testing.T) {
	var gotMsgPath, gotAuth, gotCT string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getAppAccessToken") {
			_, _ = w.Write([]byte(`{"access_token":"tok_abc","expires_in":7200}`))
			return
		}
		// URL.Path 已被服务端解码，须用 RequestURI 观察原始转义形态
		gotMsgPath = r.RequestURI
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"msg_1","timestamp":1728000000}`))
	}))
	defer srv.Close()

	n := NewQQBotNotifier(QQBotConfig{AppID: "app1", AppSecret: "sec", OpenID: "user@example.com"})
	n.client = newRewriteClient(t, srv)
	n.useCache = false

	if err := n.Send(context.Background(), "告警", "磁盘满"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	// openid 路径需按 rawurlencode 转义
	if gotMsgPath != "/v2/users/user%40example.com/messages" {
		t.Errorf("消息请求路径不符: %s", gotMsgPath)
	}
	if gotAuth != "QQBot tok_abc" {
		t.Errorf("Authorization 头不符: %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("消息请求 Content-Type 不符: %q", gotCT)
	}
	if v, ok := gotBody["msg_type"].(float64); !ok || int(v) != 2 {
		t.Errorf("msg_type 应为数字 2: %v", gotBody["msg_type"])
	}
	md, _ := gotBody["markdown"].(map[string]interface{})
	if md["content"] != "# 告警\n\n磁盘满" {
		t.Errorf("markdown.content 不符: %v", md["content"])
	}
	// 无 msg_id 时不携带 msg_seq
	if _, ok := gotBody["msg_id"]; ok {
		t.Errorf("Send 不应携带 msg_id: %v", gotBody)
	}
	if _, ok := gotBody["msg_seq"]; ok {
		t.Errorf("无 msg_id 时不应携带 msg_seq: %v", gotBody)
	}
}

/* TestQQBotSendMarkdownToWithMsgID 带 msg_id 时必须同时携带 msg_seq=1 */
func TestQQBotSendMarkdownToWithMsgID(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getAppAccessToken") {
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":7200}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"m2"}`))
	}))
	defer srv.Close()

	n := NewQQBotNotifier(QQBotConfig{AppID: "a", AppSecret: "s"})
	n.client = newRewriteClient(t, srv)
	n.useCache = false

	if err := n.SendMarkdownTo(context.Background(), "openid_x", "# 绑定成功", "msg_orig"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if gotBody["msg_id"] != "msg_orig" {
		t.Errorf("msg_id 不符: %v", gotBody["msg_id"])
	}
	if v, ok := gotBody["msg_seq"].(float64); !ok || int(v) != 1 {
		t.Errorf("msg_seq 应为数字 1: %v", gotBody["msg_seq"])
	}
}

/* TestQQBotTokenCached 同一实例第二次发送应复用 token，不重复请求凭证接口 */
func TestQQBotTokenCached(t *testing.T) {
	var tokenCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getAppAccessToken") {
			atomic.AddInt32(&tokenCalls, 1)
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"m"}`))
	}))
	defer srv.Close()

	n := NewQQBotNotifier(QQBotConfig{AppID: "a", AppSecret: "s", OpenID: "o"})
	n.client = newRewriteClient(t, srv)
	n.useCache = false

	for i := 0; i < 3; i++ {
		if err := n.Send(context.Background(), "t", "c"); err != nil {
			t.Fatalf("第 %d 次发送失败: %v", i+1, err)
		}
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 1 {
		t.Errorf("token 接口应只请求 1 次，实际 %d 次", got)
	}
}

/* TestQQBotTokenTTL 覆盖 TTL 计算：max(60, expires_in-120) */
func TestQQBotTokenTTL(t *testing.T) {
	cases := []struct {
		expiresIn interface{}
		wantTTL   int
	}{
		{7200, 7080},
		{200, 80},
		{180, 60},
		{120, 60},
		{60, 60},
		{0, 60},
		{"7200", 7080},
		{"180", 60},
		{nil, 7080},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "getAppAccessToken") {
				_, _ = w.Write([]byte(`{"access_token":"t","expires_in":` + jsonScalar(c.expiresIn) + `}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"m"}`))
		}))

		n := NewQQBotNotifier(QQBotConfig{AppID: "a", AppSecret: "s", OpenID: "o"})
		n.client = newRewriteClient(t, srv)
		n.useCache = false

		if _, err := n.getAccessToken(context.Background()); err != nil {
			srv.Close()
			t.Fatalf("expires_in=%v 获取 token 失败: %v", c.expiresIn, err)
		}
		remaining := int(n.tokenExpire.Sub(time.Now()).Seconds())
		srv.Close()
		// 允许 2 秒误差
		if remaining < c.wantTTL-2 || remaining > c.wantTTL+1 {
			t.Errorf("expires_in=%v TTL=%d 期望 %d", c.expiresIn, remaining, c.wantTTL)
		}
	}
}

/* jsonScalar 把测试表中的 expires_in 取值渲染为 JSON 标量文本 */
func jsonScalar(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		b, _ := json.Marshal(t)
		return string(b)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

/* TestQQBotTokenError 凭证失败时应透出 code 与 message */
func TestQQBotTokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":100007,"message":"appid or secret is invalid"}`))
	}))
	defer srv.Close()

	n := NewQQBotNotifier(QQBotConfig{AppID: "a", AppSecret: "s", OpenID: "o"})
	n.client = newRewriteClient(t, srv)
	n.useCache = false

	err := n.Send(context.Background(), "t", "c")
	if err == nil {
		t.Fatal("凭证失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "100007") || !strings.Contains(err.Error(), "appid or secret is invalid") {
		t.Errorf("错误信息未透出 code 与 message: %v", err)
	}
}

/* TestQQBotMessageError 发消息失败时应透出 message */
func TestQQBotMessageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getAppAccessToken") {
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":11244,"message":"msg_id 已过期"}`))
	}))
	defer srv.Close()

	n := NewQQBotNotifier(QQBotConfig{AppID: "a", AppSecret: "s", OpenID: "o"})
	n.client = newRewriteClient(t, srv)
	n.useCache = false

	err := n.Send(context.Background(), "t", "c")
	if err == nil {
		t.Fatal("接口返回 code 非 0 时应返回错误")
	}
	if !strings.Contains(err.Error(), "msg_id 已过期") {
		t.Errorf("错误信息未透出 message: %v", err)
	}
}

/* TestQQBotSendRequiresParams 参数不完整时必须报错且不发起请求 */
func TestQQBotSendRequiresParams(t *testing.T) {
	n := NewQQBotNotifier(QQBotConfig{AppID: "a", AppSecret: "s", OpenID: "o"})
	if err := n.SendMarkdownTo(context.Background(), "  ", "# x", ""); err == nil {
		t.Error("空 openid 应返回错误")
	}
	if err := n.SendMarkdownTo(context.Background(), "openid", "", ""); err == nil {
		t.Error("空 markdown 应返回错误")
	}
}
