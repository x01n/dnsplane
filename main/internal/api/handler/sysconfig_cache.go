package handler

import (
	"main/internal/database"
	"main/internal/models"
	"main/internal/sysconfig"
)

/*
 * 业务缓存层（系统配置）
 * 功能：委托到 sysconfig 包，供 handler / 安装流程等统一读写 SysConfig 缓存
 */

// GetSysConfigValue 获取系统配置值（带缓存）
func GetSysConfigValue(key string) string {
	return sysconfig.GetValue(key)
}

// InvalidateSysConfigCache 清除指定配置项的缓存
func InvalidateSysConfigCache(keys ...string) {
	sysconfig.Invalidate(keys...)
}

// InvalidateAllAuthConfigCache 清除所有 auth 相关配置缓存
func InvalidateAllAuthConfigCache() {
	knownKeys := []string{
		"auth_password_login", "auth_password_register", "auth_email_verify",
		"auth_email_whitelist_enabled", "auth_email_whitelist",
		"register_enabled", "site_url",
		"turnstile_site_key", "turnstile_secret_key",
		"github_client_id", "github_client_secret",
	}
	InvalidateSysConfigCache(knownKeys...)
	var oauthKeys []string
	database.DB.Model(&models.SysConfig{}).Where("`key` LIKE ?", "auth_oauth_%").Pluck("key", &oauthKeys)
	if len(oauthKeys) > 0 {
		InvalidateSysConfigCache(oauthKeys...)
	}
}
