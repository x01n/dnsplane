package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
	"main/internal/oauth"
)

/*
 * 登录注册控制（/system/auth-control）
 *
 * 前端 auth-control-settings.tsx 用该端点读写「密码登录 / 密码注册 / 邮箱验证 /
 * 邮箱白名单 / 各 OAuth 来源的登录与注册」这些开关。
 * 这些键此前只有后端消费方，没有任何写入端点，前端组件因此无法挂载。
 */

/*
 * authControlFixedKeys 本页面可读写的固定配置键
 *
 * 这份清单同时是读与写的唯一来源，两者必须完全一致：
 * 前端保存时提交整个 config 对象，读到的键若多于可写的键，保存会因
 * 「含不支持修改的配置项」整体失败；可写多于可读则写入永不生效
 *
 * 与 sysconfig_cache.go 的 InvalidateAllAuthConfigCache 清单是包含关系：
 * 此处是它的子集（该清单另含 turnstile_* / github_* / site_url 等本页面不涉及的键），
 * 两边不共用同一份定义是有意为之 —— 共用会把 turnstile_secret_key 这类
 * 敏感键一并纳入本端点的返回体，等于给管理员页面开一条凭据读取口
 *
 * @returns 固定配置键列表
 */
func authControlFixedKeys() []string {
	return []string{
		"auth_password_login",
		"auth_password_register",
		"auth_magic_link_login",
		"auth_email_verify",
		"auth_email_whitelist_enabled",
		"auth_email_whitelist",
		"register_enabled",
	}
}

/* authOAuthSwitchSuffixes OAuth 来源开关的键后缀 */
var authOAuthSwitchSuffixes = []string{"_login", "_register"}

/*
 * isWritableAuthControlKey 判定配置键是否属于本页面的读写范围
 *
 * 动态键 auth_oauth_<provider>_login|register 按前缀与后缀放行，
 * provider 取自 oauth.GetEnabledProviders()，与前端 providers 接口同源：
 * 固定清单会在新增 provider 时漏放，而未启用的 provider 本就不会出现在前端列表里
 *
 * @param key 配置键
 * @returns 是否可读写
 */
func isWritableAuthControlKey(key string) bool {
	for _, k := range authControlFixedKeys() {
		if key == k {
			return true
		}
	}

	const prefix = "auth_oauth_"
	if !strings.HasPrefix(key, prefix) {
		return false
	}
	rest := key[len(prefix):]
	for _, suffix := range authOAuthSwitchSuffixes {
		if !strings.HasSuffix(rest, suffix) {
			continue
		}
		name := rest[:len(rest)-len(suffix)]
		for _, p := range oauth.GetEnabledProviders() {
			if p.Name == name {
				return true
			}
		}
	}
	return false
}

/*
 * GetAuthControl 读取登录注册控制页的全部开关
 *
 * 只返回库中确实存在的键，不补齐缺失项：
 * 前端 getBool 只在键为 undefined 时使用其默认值，补空串会让默认值失效
 * （auth_password_login 等默认开启的开关会被显示成关闭，用户一保存即写入关闭）
 *
 * @param c 请求上下文
 */
func GetAuthControl(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	var configs []models.SysConfig
	database.DB.Where("`key` LIKE ? OR `key` = ?", "auth_%", "register_enabled").Find(&configs)

	result := make(map[string]string)
	for _, cfg := range configs {
		if isWritableAuthControlKey(cfg.Key) {
			result[cfg.Key] = cfg.Value
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

/*
 * UpdateAuthControl 写入登录注册控制页的开关
 *
 * 只接受白名单内的键，且先整体校验再落库：
 * 含任一不可写键时整批拒绝，避免「一半写入、一半报错」留下的中间状态
 * 写入后逐个失效缓存，否则开关最长 60 秒不生效（表现为「打开了却没反应」）
 *
 * @param c 请求上下文
 */
func UpdateAuthControl(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	req := make(map[string]interface{})
	if d := middleware.GetDecryptedData(c); d != nil {
		req = d
	} else if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}
	if len(req) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	keys := make([]string, 0, len(req))
	for key := range req {
		if !isWritableAuthControlKey(key) {
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "不支持修改的配置项: " + key})
			return
		}
		keys = append(keys, key)
	}

	for _, key := range keys {
		value := sysConfigJSONValueToDB(req[key])
		database.DB.Where("`key` = ?", key).Assign(models.SysConfig{Key: key, Value: value}).FirstOrCreate(&models.SysConfig{})
	}
	InvalidateSysConfigCache(keys...)

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}
