package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

/*
 * TestCfValidateCustomOrigin 覆盖源站校验的六条规则与合法值
 *
 * 规则顺序对齐 PHP 版 validateCustomOrigin，因此同时锁定「首个命中的错误」，
 * 避免未来调整顺序时前端提示语义发生漂移
 */
func TestCfValidateCustomOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		want   string
	}{
		{name: "空值表示不设置源站", origin: "", want: ""},
		{name: "合法域名", origin: "origin.example.com", want: ""},
		{name: "合法域名含子域与数字", origin: "edge01.origin.example.com", want: ""},
		{name: "合法域名可含下划线", origin: "my_host.example.com", want: ""},
		{name: "规则1 带http前缀", origin: "http://origin.example.com", want: "自定义源站不支持填写 http:// 或 https://"},
		{name: "规则1 带https前缀", origin: "https://origin.example.com", want: "自定义源站不支持填写 http:// 或 https://"},
		{name: "规则2 含通配符", origin: "*.origin.example.com", want: "自定义源站不支持通配符"},
		{name: "规则3 含斜杠", origin: "origin.example.com/path", want: "自定义源站格式不正确"},
		{name: "规则4 带端口", origin: "origin.example.com:8080", want: "自定义源站不支持端口"},
		{name: "规则5 IPv4", origin: "192.0.2.10", want: "自定义源站不支持 IP 地址，请填写域名"},
		{name: "规则5 IPv6", origin: "2001:db8::1", want: "自定义源站不支持 IP 地址，请填写域名"},
		{name: "规则5 带端口的IPv4", origin: "192.0.2.10:8080", want: "自定义源站不支持端口"},
		{name: "规则6 不含点", origin: "origin", want: "自定义源站格式不正确"},
		{name: "规则6 以点开头", origin: ".example.com", want: "自定义源站格式不正确"},
		{name: "规则6 以点结尾", origin: "origin.example.com.", want: "自定义源站格式不正确"},
		{name: "规则6 以连字符开头", origin: "-origin.example.com", want: "自定义源站格式不正确"},
		{name: "规则6 含非法字符", origin: "origin examp le.com", want: "自定义源站格式不正确"},
		{name: "规则6 超长", origin: strings.Repeat("a", 250) + ".example.com", want: "自定义源站格式不正确"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cfValidateCustomOrigin(tc.origin); got != tc.want {
				t.Errorf("origin=%q 应返回 %q，实际 %q", tc.origin, tc.want, got)
			}
		})
	}
}

/*
 * TestCfValidateSSLParams 覆盖证书验证方式与最低 TLS 版本的取值范围
 *
 * 非法值此前会被原样发给 Cloudflare 换回 400 透传，这里锁定前置拦截
 */
func TestCfValidateSSLParams(t *testing.T) {
	cases := []struct {
		name   string
		method string
		minTLS string
		want   string
	}{
		{name: "合法 txt 与 1.0", method: "txt", minTLS: "1.0", want: ""},
		{name: "合法 http 与 1.3", method: "http", minTLS: "1.3", want: ""},
		{name: "验证方式非法", method: "dns", minTLS: "1.2", want: "证书验证方法无效"},
		{name: "验证方式为空", method: "", minTLS: "1.2", want: "证书验证方法无效"},
		{name: "验证方式大小写敏感", method: "TXT", minTLS: "1.2", want: "证书验证方法无效"},
		{name: "TLS版本非法", method: "txt", minTLS: "1.4", want: "最低 TLS 版本无效"},
		{name: "TLS版本为空", method: "txt", minTLS: "", want: "最低 TLS 版本无效"},
		{name: "TLS版本缺小数点", method: "txt", minTLS: "12", want: "最低 TLS 版本无效"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cfValidateSSLParams(tc.method, tc.minTLS); got != tc.want {
				t.Errorf("method=%q minTLS=%q 应返回 %q，实际 %q", tc.method, tc.minTLS, tc.want, got)
			}
		})
	}
}

/*
 * TestAddCustomHostnameRejectsBadOrigin 断言单条新增在调用 Cloudflare 之前就拦下非法参数
 *
 * 请求体里的 API Key 指向不可达地址，若校验未前置就会走到 HTTP 请求并返回网络错误；
 * 断言拿到的是校验消息，即可证明拦截发生在网络调用之前
 */
func TestAddCustomHostnameRejectsBadOrigin(t *testing.T) {
	setupHandlerTestDB(t)
	seedDomainForAccount(t, "cloudflare")

	cases := []struct {
		name string
		body map[string]interface{}
		want string
	}{
		{
			name: "源站带端口",
			body: map[string]interface{}{"hostname": "shop.example.com", "custom_origin_server": "origin.example.com:8080"},
			want: "自定义源站不支持端口",
		},
		{
			name: "源站是IP",
			body: map[string]interface{}{"hostname": "shop.example.com", "custom_origin_server": "192.0.2.10"},
			want: "自定义源站不支持 IP 地址，请填写域名",
		},
		{
			name: "验证方式非法",
			body: map[string]interface{}{"hostname": "shop.example.com", "ssl_method": "dns", "min_tls_version": "1.2"},
			want: "证书验证方法无效",
		},
		{
			name: "TLS版本非法",
			body: map[string]interface{}{"hostname": "shop.example.com", "ssl_method": "txt", "min_tls_version": "1.4"},
			want: "最低 TLS 版本无效",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSONWithParam(t, AddCustomHostname, "/api/cloudflare/hostnames/add/1", "id", "1", tc.body)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("状态码应为 400，实际 %d（%s）", resp.Code, resp.Body.String())
			}
			if msg := respMsg(t, resp); msg != tc.want {
				t.Errorf("应返回 %q，实际 %q", tc.want, msg)
			}
		})
	}
}

/* postJSONWithParam 构造带路径参数的 JSON 请求并执行 */
func postJSONWithParam(t *testing.T, handler gin.HandlerFunc, path, paramKey, paramVal string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化请求体失败: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: paramKey, Value: paramVal}}
	handler(c)
	return w
}
