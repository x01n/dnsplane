package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

/*
 * 自定义 Webhook 渠道的构造与请求契约测试
 *
 * 该渠道此前完全缺失：设置页保存 custom_webhook_* 却无任何代码消费，
 * 容灾告警的 notice_custom_webhook 为空判。本测试锁定其四类配置组合与请求形态
 */

/* customWebhookBuildConfig 渠道构造所需的最小配置 */
func customWebhookBuildConfig() map[string]string {
	return map[string]string{
		"custom_webhook_url": "https://example.com/hook",
	}
}

/*
 * TestCustomWebhookChannelBuild 构造与开关语义
 * 四类组合：完整配置、缺 URL、开关显式关闭、开关未配置（零倒退）
 */
func TestCustomWebhookChannelBuild(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]string
		want bool
	}{
		{"完整配置装载", customWebhookBuildConfig(), true},
		{"缺 URL 不装载", map[string]string{}, false},
		{"URL 为空串不装载", map[string]string{"custom_webhook_url": ""}, false},
		{"显式关闭不装载", merge(customWebhookBuildConfig(), map[string]string{"custom_webhook_enabled": "0"}), false},
		{"显式开启装载", merge(customWebhookBuildConfig(), map[string]string{"custom_webhook_enabled": "1"}), true},
		{"非法 URL 不装载", map[string]string{"custom_webhook_url": "http://127.0.0.1/hook"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, ok := (customWebhookChannel{}).Build(c.cfg)
			if ok != c.want {
				t.Fatalf("Build 返回 %v，期望 %v", ok, c.want)
			}
			if ok {
				if _, isCustom := n.(*CustomWebhookNotifier); !isCustom {
					t.Errorf("构造类型 = %T，期望 *CustomWebhookNotifier", n)
				}
			}
		})
	}
}

/* TestCustomWebhookChannelConfigKeys 六个配置键必须齐备：漏一个就会在 getter 装载时读不到 */
func TestCustomWebhookChannelConfigKeys(t *testing.T) {
	want := []string{
		"custom_webhook_url", "custom_webhook_method", "custom_webhook_content_type",
		"custom_webhook_headers", "custom_webhook_body", "custom_webhook_content_format",
	}
	have := make(map[string]bool)
	for _, k := range (customWebhookChannel{}).ConfigKeys() {
		have[k] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("配置键 %s 未声明", k)
		}
		if !slicesContains(NotifyConfigKeys(), k) {
			t.Errorf("配置键 %s 未登记进 NotifyConfigKeys", k)
		}
	}
	if got := (customWebhookChannel{}).EnabledKey(); got != "custom_webhook_enabled" {
		t.Errorf("启用开关 = %q，期望 custom_webhook_enabled", got)
	}
}

/* TestCustomWebhookContentFormat 三种内容格式的正文转换 */
func TestCustomWebhookContentFormat(t *testing.T) {
	cases := []struct {
		format string
		in     string
		want   string
	}{
		{"markdown", "<br/>域名 <b>a.com</b> 异常", "\n域名 **a.com** 异常"},
		{"text", "<br/>域名 <b>a.com</b> 异常", "\n域名 a.com 异常"},
		{"html", "<br/>域名 <b>a.com</b> 异常", "<br/>域名 <b>a.com</b> 异常"},
		{"", "<br/>域名 <b>a.com</b> 异常", "<br/>域名 <b>a.com</b> 异常"},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			if got := formatCustomWebhookContent(c.format, c.in); got != c.want {
				t.Errorf("format=%q\n got=%q\nwant=%q", c.format, got, c.want)
			}
		})
	}
}

/* TestCustomWebhookBodyTemplate 正文模板的占位符替换与缺省模板 */
func TestCustomWebhookBodyTemplate(t *testing.T) {
	t.Run("缺省模板为通用 JSON", func(t *testing.T) {
		body := buildCustomWebhookBody("", "正文", "标题")
		var decoded map[string]string
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("缺省模板不是合法 JSON: %s", body)
		}
		if decoded["title"] != "标题" || decoded["content"] != "正文" {
			t.Errorf("缺省模板替换有误: %v", decoded)
		}
	})
	t.Run("自定义模板替换两个占位符", func(t *testing.T) {
		got := buildCustomWebhookBody(`{"msg":"{title}: {content}"}`, "正文", "标题")
		if got != `{"msg":"标题: 正文"}` {
			t.Errorf("模板替换有误: %s", got)
		}
	})
	t.Run("无占位符时原样返回", func(t *testing.T) {
		if got := buildCustomWebhookBody(`{"fixed":true}`, "正文", "标题"); got != `{"fixed":true}` {
			t.Errorf("无占位符时不应改动模板: %s", got)
		}
	})
}

/* TestParseWebhookHeadersGeneric 请求头解析：按行、首个冒号分隔、忽略非法行 */
func TestParseWebhookHeadersGeneric(t *testing.T) {
	got := parseWebhookHeaders("X-A: 1\n\n  X-B :2\n无冒号行\n: 空键\nX-C: 值: 含冒号")
	want := map[string]string{"X-A": "1", "X-B": "2", "X-C": "值: 含冒号"}
	if len(got) != len(want) {
		t.Fatalf("解析出 %d 个头，期望 %d 个：%v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("请求头 %s = %q，期望 %q", k, got[k], v)
		}
	}
}

/* customWebhookRequest 一次自定义 Webhook 请求的观测结果 */
type customWebhookRequest struct {
	Method      string
	Path        string
	RawQuery    string
	Header      http.Header
	Body        string
	ContentType string
}

/* captureCustomWebhook 用重定向传输发起一次发送，返回服务端观测到的请求 */
func captureCustomWebhook(t *testing.T, cfg CustomWebhookConfig, status int) (*customWebhookRequest, error) {
	t.Helper()
	got := &customWebhookRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.Method = r.Method
		got.Path = r.URL.Path
		got.RawQuery = r.URL.RawQuery
		got.Header = r.Header.Clone()
		got.Body = string(body)
		got.ContentType = r.Header.Get("Content-Type")
		w.WriteHeader(status)
	}))
	defer srv.Close()

	return got, sendCustomWebhook(context.Background(), newRewriteClient(t, srv), cfg, "标题", "<br/>正文 <b>粗</b>")
}

/*
 * TestCustomWebhookSendDefaults 默认方法 POST、默认内容类型 application/json
 * 请求体走缺省模板，正文按缺省格式（原样）透传
 */
func TestCustomWebhookSendDefaults(t *testing.T) {
	cfg := CustomWebhookConfig{URL: "https://example.com/hook"}
	got, err := captureCustomWebhook(t, cfg, http.StatusOK)
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if got.Method != http.MethodPost {
		t.Errorf("方法 = %s，期望 POST", got.Method)
	}
	if got.ContentType != "application/json" {
		t.Errorf("内容类型 = %q，期望 application/json", got.ContentType)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(got.Body), &decoded); err != nil {
		t.Fatalf("请求体不是合法 JSON: %s", got.Body)
	}
	if decoded["title"] != "标题" || decoded["content"] != "<br/>正文 <b>粗</b>" {
		t.Errorf("请求体内容有误: %v", decoded)
	}
	if got.Path != "/hook" {
		t.Errorf("路径 = %q，期望 /hook", got.Path)
	}
}

/*
 * TestCustomWebhookSendCustomMethodAndBody 自定义方法与正文模板，并叠加自定义请求头
 *
 * 正文含换行，模板替换后不是合法 JSON，故走兜底路径：
 * 接收端拿到的是可解析的通用报文，标题与正文均完整保留（正文的换行由 JSON 转义承载）
 */
func TestCustomWebhookSendCustomMethodAndBody(t *testing.T) {
	cfg := CustomWebhookConfig{
		URL:           "https://example.com/hook",
		Method:        "PUT",
		ContentType:   "application/json",
		Body:          `{"t":"{title}","c":"{content}"}`,
		Headers:       "X-Token: abc\nX-Trace: t-1",
		ContentFormat: "markdown",
	}
	got, err := captureCustomWebhook(t, cfg, http.StatusOK)
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if got.Method != http.MethodPut {
		t.Errorf("方法 = %s，期望 PUT", got.Method)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(got.Body), &decoded); err != nil {
		t.Fatalf("兜底后的正文必须是合法 JSON: %s", got.Body)
	}
	if decoded["title"] != "标题" {
		t.Errorf("标题 = %q，期望 标题", decoded["title"])
	}
	if want := "\n正文 **粗**"; decoded["content"] != want {
		t.Errorf("正文 = %q，期望 %q", decoded["content"], want)
	}
	if got.Header.Get("X-Token") != "abc" || got.Header.Get("X-Trace") != "t-1" {
		t.Errorf("自定义请求头未生效: %v", got.Header)
	}
}

/*
 * TestCustomWebhookCustomTemplateKeptWhenValid 正文可安全嵌入时用户模板必须原样生效
 * 兜底只应在前述非法 JSON 场景触发，否则自定义模板形同虚设
 */
func TestCustomWebhookCustomTemplateKeptWhenValid(t *testing.T) {
	cfg := CustomWebhookConfig{
		URL:         "https://example.com/hook",
		ContentType: "application/json",
		Body:        `{"event":"dns","msg":"{title}"}`,
	}
	got, err := captureCustomWebhook(t, cfg, http.StatusOK)
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if want := `{"event":"dns","msg":"标题"}`; got.Body != want {
		t.Errorf("请求体 = %q，期望 %q", got.Body, want)
	}
}

/* TestCustomWebhookSendGET GET 方法把正文转为查询串，不带请求体 */
func TestCustomWebhookSendGET(t *testing.T) {
	t.Run("JSON 正文转查询参数", func(t *testing.T) {
		cfg := CustomWebhookConfig{URL: "https://example.com/hook", Method: "GET"}
		got, err := captureCustomWebhook(t, cfg, http.StatusOK)
		if err != nil {
			t.Fatalf("发送失败: %v", err)
		}
		if got.Method != http.MethodGet {
			t.Fatalf("方法 = %s，期望 GET", got.Method)
		}
		if got.Body != "" {
			t.Errorf("GET 不应带请求体，实际 %q", got.Body)
		}
		values, err := url.ParseQuery(got.RawQuery)
		if err != nil {
			t.Fatalf("查询串解析失败: %v", err)
		}
		if values.Get("title") != "标题" || values.Get("content") != "<br/>正文 <b>粗</b>" {
			t.Errorf("查询参数有误: %v", values)
		}
	})
	t.Run("表单正文原样转查询参数", func(t *testing.T) {
		cfg := CustomWebhookConfig{
			URL: "https://example.com/hook", Method: "GET",
			ContentType: "application/x-www-form-urlencoded",
			Body:        "t={title}&c={content}",
		}
		got, err := captureCustomWebhook(t, cfg, http.StatusOK)
		if err != nil {
			t.Fatalf("发送失败: %v", err)
		}
		values, err := url.ParseQuery(got.RawQuery)
		if err != nil {
			t.Fatalf("查询串解析失败: %v", err)
		}
		if values.Get("t") != "标题" || values.Get("c") != "<br/>正文 <b>粗</b>" {
			t.Errorf("查询参数有误: %v", values)
		}
	})
}

/* TestCustomWebhookSendNon2xxIsError 非 2xx 必须报错，否则失败通知会被当成已送达 */
func TestCustomWebhookSendNon2xxIsError(t *testing.T) {
	_, err := captureCustomWebhook(t, CustomWebhookConfig{URL: "https://example.com/hook"}, http.StatusInternalServerError)
	if err == nil {
		t.Fatal("500 响应应返回错误")
	}
}

/* TestCustomWebhookSendRejectsPrivateURL 内网地址必须在校验阶段被拒，且不发起任何请求 */
func TestCustomWebhookSendRejectsPrivateURL(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/hook", "http://10.0.0.1/hook", "http://localhost/hook", ""} {
		sent := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sent = true
		}))
		client := newRewriteClient(t, srv)
		err := sendCustomWebhook(context.Background(), client, CustomWebhookConfig{URL: raw}, "t", "c")
		srv.Close()
		if err == nil {
			t.Errorf("地址 %q 应被拒绝", raw)
		}
		if sent {
			t.Errorf("地址 %q 被拒后仍发起了请求", raw)
		}
	}
}

/* TestCustomWebhookLoadFromConfig 渠道在两条装载路径上都应可达，且全字段装配 */
func TestCustomWebhookLoadFromConfig(t *testing.T) {
	cfg := map[string]string{
		"custom_webhook_url":            "https://example.com/hook",
		"custom_webhook_method":         "PUT",
		"custom_webhook_content_type":   "application/x-www-form-urlencoded",
		"custom_webhook_headers":        "X-Token: abc",
		"custom_webhook_body":           "t={title}&c={content}",
		"custom_webhook_content_format": "text",
	}

	mgr := NewManager()
	LoadNotifiersFromConfig(mgr, cfg)
	if len(mgr.notifiers) != 1 {
		t.Fatalf("应装载 1 个渠道，实际 %d 个", len(mgr.notifiers))
	}
	n, ok := mgr.notifiers[0].(*CustomWebhookNotifier)
	if !ok {
		t.Fatalf("装载类型 = %T，期望 *CustomWebhookNotifier", mgr.notifiers[0])
	}
	want := CustomWebhookConfig{
		URL:           "https://example.com/hook",
		Method:        "PUT",
		ContentType:   "application/x-www-form-urlencoded",
		Headers:       "X-Token: abc",
		Body:          "t={title}&c={content}",
		ContentFormat: "text",
	}
	if n.config != want {
		t.Errorf("配置装配有误\n got=%+v\nwant=%+v", n.config, want)
	}

	// getter 装载路径：键未登记时会读不到，渠道随之消失
	store := make(map[string]string, len(cfg))
	for k, v := range cfg {
		store[k] = v
	}
	viaGetter := NewManager()
	LoadNotifiersWithGetter(viaGetter, func(key string) string { return store[key] })
	if len(viaGetter.notifiers) != 1 {
		t.Errorf("getter 装载应得到 1 个渠道，实际 %d 个（配置键可能未登记）", len(viaGetter.notifiers))
	}
}

/*
 * TestCustomWebhookRegistryIsLoadBearing 回归有效性验证
 *
 * 渠道装载完全由注册表驱动：把 custom_webhook 从注册表摘掉（装载路径读不到它），
 * 其装载测试必须转红。本条以「临时摘除后再装载」的方式验证这层因果关系确实成立，
 * 而不是碰巧由别处的硬编码装载命中
 */
func TestCustomWebhookRegistryIsLoadBearing(t *testing.T) {
	registryMu.Lock()
	saved := registeredChannels
	filtered := make([]Channel, 0, len(saved))
	for _, c := range saved {
		if c.Type() != NotifyCustomWebhook {
			filtered = append(filtered, c)
		}
	}
	registeredChannels = filtered
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registeredChannels = saved
		registryMu.Unlock()
	})

	mgr := NewManager()
	LoadNotifiersFromConfig(mgr, map[string]string{"custom_webhook_url": "https://example.com/hook"})
	if len(mgr.notifiers) != 0 {
		t.Fatalf("渠道已从注册表摘除，却仍装载了 %d 个，说明存在绕过注册表的装载路径", len(mgr.notifiers))
	}
	if _, ok := LookupChannel(NotifyCustomWebhook); ok {
		t.Error("渠道已摘除，LookupChannel 仍能查到")
	}

	// 摘除后其配置键也必须从聚合结果中消失，否则 monitor 的 IN 查询仍会读一份无人消费的键
	for _, k := range (customWebhookChannel{}).ConfigKeys() {
		if slicesContains(ChannelConfigKeys(), k) {
			t.Errorf("渠道已摘除，配置键 %s 仍留在聚合结果中", k)
		}
	}
}

/* errCustomWebhookTransport 固定失败的传输，用于验证错误包装 */
type errCustomWebhookTransport struct{}

func (errCustomWebhookTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("传输层失败")
}

/* TestCustomWebhookTransportErrorWrapped 传输层错误必须带上渠道名，便于在日志里定位 */
func TestCustomWebhookTransportErrorWrapped(t *testing.T) {
	client := &http.Client{Transport: errCustomWebhookTransport{}}
	err := sendCustomWebhook(context.Background(), client, CustomWebhookConfig{URL: "https://example.com/hook"}, "t", "c")
	if err == nil {
		t.Fatal("传输层失败应返回错误")
	}
	if !strings.HasPrefix(err.Error(), "自定义Webhook请求失败") {
		t.Errorf("错误未标明渠道来源: %v", err)
	}
}

/* slicesContains 判定字符串切片是否含指定元素 */
func slicesContains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
