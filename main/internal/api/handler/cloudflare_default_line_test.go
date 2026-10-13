package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"main/internal/database"
	"main/internal/models"
)

/*
 * TestCfDefaultLine 覆盖默认线路解析的三条路径
 *
 * Cloudflare 是声明了线路编目的 DNS 服务商，编目首条为 "0"；
 * 未导入的域名回退 "0"；不具备线路查询能力的服务商回退静态映射
 * （spaceship 的 default 与 dns.DefaultLineMapping 一致，可作为对照）
 */
func TestCfDefaultLine(t *testing.T) {
	cases := []struct {
		name     string
		account  string
		body     map[string]string
		wantLine string
	}{
		{
			name:     "Cloudflare账户按域名ID查询",
			account:  "cloudflare",
			body:     map[string]string{"domain_id": "1"},
			wantLine: "0",
		},
		{
			name:     "Cloudflare账户按域名本身查询",
			account:  "cloudflare",
			body:     map[string]string{"domain": "example.com"},
			wantLine: "0",
		},
		{
			name:     "未知域名标识回退",
			account:  "cloudflare",
			body:     map[string]string{"domain_id": "999"},
			wantLine: "0",
		},
		{
			name:     "静态映射型服务商回退",
			account:  "spaceship",
			body:     map[string]string{"domain_id": "1"},
			wantLine: "default",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			seedDomainForAccount(t, tc.account)

			resp := postJSON(t, GetDomainDefaultLine, "/api/cloudflare/get_domain_default_line", tc.body)
			if resp.Code != http.StatusOK {
				t.Fatalf("状态码应为 200，实际 %d（%s）", resp.Code, resp.Body.String())
			}
			if line := respDefaultLine(t, resp); line != tc.wantLine {
				t.Errorf("响应中默认线路应为 %q，实际 %q", tc.wantLine, line)
			}
		})
	}
}

/*
 * TestGetDomainDefaultLineMissingParam 断言缺少域名标识时返回参数错误
 *
 * 旧实现同样是 400 + code 1，这里锁定该错误契约不被改动破坏
 */
func TestGetDomainDefaultLineMissingParam(t *testing.T) {
	setupHandlerTestDB(t)

	resp := postJSON(t, GetDomainDefaultLine, "/api/cloudflare/get_domain_default_line", map[string]string{})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("状态码应为 400，实际 %d（%s）", resp.Code, resp.Body.String())
	}
	if msg := respMsg(t, resp); msg != "domain_id 不能为空" {
		t.Errorf("错误信息应为 domain_id 不能为空，实际 %q", msg)
	}
}

/* seedDomainForAccount 写入一个指定类型的账户与其名下的 example.com */
func seedDomainForAccount(t *testing.T, accountType string) {
	t.Helper()

	account := models.Account{
		Type:   accountType,
		Name:   accountType + "-test",
		Config: `{"apikey":"0123456789abcdef","apisecret":"sk","account_id":"acc1"}`,
	}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}

	domain := models.Domain{
		AccountID: account.ID,
		Name:      "example.com",
		ThirdID:   "zone1",
	}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
}

/* respDefaultLine 取出响应体 data.default_line */
func respDefaultLine(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Code int `json:"code"`
		Data struct {
			DefaultLine string `json:"default_line"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, resp.Body.String())
	}
	if out.Code != 0 {
		t.Fatalf("业务码应为 0，实际 %d", out.Code)
	}
	return out.Data.DefaultLine
}
