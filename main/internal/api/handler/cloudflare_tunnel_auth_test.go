package handler

import (
	"net/http"
	"strings"
	"testing"

	"main/internal/database"
	"main/internal/models"
)

const (
	cfTokenMsg  = "Cloudflare Tunnels 仅支持 API 令牌认证，请将当前账户的认证方式切换为 API令牌"
	cfModuleMsg = "无权限访问该功能模块"
)

/*
 * TestTunnelEntryRejectsGlobalAPIKey 覆盖 Tunnel 入口的认证方式守卫：Global API Key 必须被拒
 *
 * 认证方式由 getCFEnhanceService 从账户配置解析：auth=1 视为 API 令牌，
 * 否则按 DetectAuthMode 判断（纯十六进制串视为 Global API Key）。
 * 请求体里的凭据指向不可达地址也无妨——守卫在调用 Cloudflare 之前就该拦下
 */
func TestTunnelEntryRejectsGlobalAPIKey(t *testing.T) {
	setupHandlerTestDB(t)
	seedDomainForAccount(t, "cloudflare") // 同时给出一个 cloudflare 账户，其 auth=0

	resp := postJSONWithParam(t, GetTunnels, "/api/cloudflare/tunnels/1", "id", "1", map[string]interface{}{})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("状态码应为 400，实际 %d（%s）", resp.Code, resp.Body.String())
	}
	if msg := respMsg(t, resp); msg != cfTokenMsg {
		t.Errorf("应返回 %q，实际 %q", cfTokenMsg, msg)
	}
}

/*
 * TestTunnelEntryAllowsApiToken 覆盖放行路径：API 令牌账户不因认证方式守卫被拦
 *
 * 用一个本地不存在的 tunnel_id 作探针：守卫若误拦会返回认证方式那条消息，
 * 正常放行则继续走到「Tunnel 不存在」。这样既拿到正向证据，又完全不依赖网络
 */
func TestTunnelEntryAllowsApiToken(t *testing.T) {
	setupHandlerTestDB(t)
	seedCloudflareAccount(t, "tokentest", `{"apikey":"0123456789abcdef","auth":"1","account_id":"acc1"}`)

	resp := postJSONWithParam(t, DeleteTunnel, "/api/cloudflare/tunnels/delete/1", "id", "1",
		map[string]interface{}{"tunnel_id": "nonexistent"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("状态码应为 400，实际 %d（%s）", resp.Code, resp.Body.String())
	}
	msg := respMsg(t, resp)
	if msg == cfTokenMsg {
		t.Fatalf("API 令牌账户不应被认证方式守卫拦下，实际响应 %s", resp.Body.String())
	}
	if msg == cfModuleMsg {
		t.Fatalf("不应被模块权限拦下，实际响应 %s", resp.Body.String())
	}
	if msg != "Tunnel 不存在" {
		t.Errorf("应越过守卫后报 Tunnel 不存在，实际 %q", msg)
	}
}

/*
 * TestTunnelEntryRejectsNonCloudflareAccount 覆盖非 Cloudflare 账户在守卫之前就被拒
 *
 * 守卫位于 getCFAccountContextForRequest 内，账户类型检查仍是先决条件，
 * 保证新增的认证方式检查没有把原来的错误语义顶掉
 */
func TestTunnelEntryRejectsNonCloudflareAccount(t *testing.T) {
	setupHandlerTestDB(t)
	seedDomainForAccount(t, "dnspod")

	resp := postJSONWithParam(t, GetTunnels, "/api/cloudflare/tunnels/1", "id", "1", map[string]interface{}{})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("状态码应为 400，实际 %d（%s）", resp.Code, resp.Body.String())
	}
	if msg := respMsg(t, resp); !strings.Contains(msg, "不是 Cloudflare 类型") {
		t.Errorf("应返回账户类型错误，实际 %q", msg)
	}
}

/* seedCloudflareAccount 写入一个指定名称与配置的 Cloudflare 账户 */
func seedCloudflareAccount(t *testing.T, name, config string) {
	t.Helper()
	if err := database.DB.Create(&models.Account{Type: "cloudflare", Name: name, Config: config}).Error; err != nil {
		t.Fatalf("创建 Cloudflare 账户失败: %v", err)
	}
}
