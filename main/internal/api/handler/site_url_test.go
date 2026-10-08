package handler

import (
	"strconv"
	"testing"

	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
)

/*
 * getSiteURL 未配置 site_url 时必须回退到实际监听端口
 *
 * 回退到恒定端口会让 OAuth 的 redirect_uri 与实际部署端口不符，
 * 第三方以 redirect_uri 不匹配拒绝授权；复位链接与 magic-link 也会指向打不开的地址
 */
func TestGetSiteURLFallsBackToConfiguredPort(t *testing.T) {
	setupHandlerTestDB(t)

	port := config.Get().Server.Port
	if port == 0 {
		t.Fatal("配置未加载出监听端口，用例前提不成立")
	}
	want := "http://localhost:" + strconv.Itoa(port)
	if got := getSiteURL(); got != want {
		t.Errorf("getSiteURL() = %q，期望 %q", got, want)
	}
}

/*
 * site_url 已配置时原样采信，仅去掉尾部斜杠
 *
 * 拼接方一律补 "/path"，配置值带尾斜杠会产出双斜杠地址，
 * 而第三方对 redirect_uri 做逐字比对，多一个斜杠即判定不匹配
 */
func TestGetSiteURLPrefersConfiguredValue(t *testing.T) {
	setupHandlerTestDB(t)

	if err := database.DB.Create(&models.SysConfig{
		Key: "site_url", Value: "https://dns.example.com/",
	}).Error; err != nil {
		t.Fatalf("写入 site_url 失败: %v", err)
	}

	if got, want := getSiteURL(), "https://dns.example.com"; got != want {
		t.Errorf("getSiteURL() = %q，期望 %q", got, want)
	}
}