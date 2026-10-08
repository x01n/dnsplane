package handler

import (
	"path/filepath"
	"testing"

	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
)

/*
 * getEmailConfig 读取的 SMTP 用户名键名回归测试
 *
 * 历史缺陷：设置页与通知渠道写入的键是 mail_user，而 getEmailConfig 只读 mail_username，
 * 后者全仓无任何写入点，导致 ForgotPassword / ForgotTOTP / RequestMagicLink / SendAuthCode
 * 等六个公开接口拿到的 Username 恒为空。这些接口通过 enqueueNotifyMail 在 goroutine 内发信，
 * 失败仅记日志、HTTP 已先返回成功，用户表现为收不到邮件却提示已发送
 */
func TestGetEmailConfigUsernameKey(t *testing.T) {
	dir := t.TempDir()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(dir, "test.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	base := map[string]string{
		"mail_host":     "smtp.example.com",
		"mail_port":     "465",
		"mail_password": "secret",
		"mail_from":     "noreply@example.com",
	}

	cases := []struct {
		name     string
		extra    map[string]string
		wantUser string
	}{
		{
			name:     "仅 mail_user（设置页实际写入的键）",
			extra:    map[string]string{"mail_user": "user@example.com"},
			wantUser: "user@example.com",
		},
		{
			name:     "仅 mail_username（历史键，需兜底）",
			extra:    map[string]string{"mail_username": "legacy@example.com"},
			wantUser: "legacy@example.com",
		},
		{
			name: "两键并存时 mail_user 优先",
			extra: map[string]string{
				"mail_user":     "preferred@example.com",
				"mail_username": "legacy@example.com",
			},
			wantUser: "preferred@example.com",
		},
		{
			name:     "两键皆无时为空",
			extra:    map[string]string{},
			wantUser: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := database.DB.Where("1 = 1").Delete(&models.SysConfig{}).Error; err != nil {
				t.Fatalf("清空配置失败: %v", err)
			}
			for k, v := range base {
				if err := database.DB.Create(&models.SysConfig{Key: k, Value: v}).Error; err != nil {
					t.Fatalf("写入 %s 失败: %v", k, err)
				}
			}
			for k, v := range tc.extra {
				if err := database.DB.Create(&models.SysConfig{Key: k, Value: v}).Error; err != nil {
					t.Fatalf("写入 %s 失败: %v", k, err)
				}
			}

			cfg, err := getEmailConfig()
			if err != nil {
				t.Fatalf("getEmailConfig 出错: %v", err)
			}
			if cfg == nil {
				t.Fatal("getEmailConfig 返回 nil，期望非空配置")
			}
			if cfg.Username != tc.wantUser {
				t.Errorf("Username = %q，期望 %q", cfg.Username, tc.wantUser)
			}
		})
	}
}

/* TestGetEmailConfigMissingHost 未配置 mail_host 时返回 nil，调用方据此跳过发信 */
func TestGetEmailConfigMissingHost(t *testing.T) {
	dir := t.TempDir()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(dir, "test.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg, err := getEmailConfig()
	if err != nil {
		t.Fatalf("getEmailConfig 出错: %v", err)
	}
	if cfg != nil {
		t.Errorf("未配置 mail_host 时应返回 nil，实际 %+v", cfg)
	}
}

/*
 * TestGetEmailConfigSecureAndAuth 加密方式与鉴权方式的键名回归测试
 *
 * 历史缺陷：本函数只读早期键 mail_tls，而设置页「加密方式」写入的是 mail_secure
 * （none/ssl/tls），导致六个公开邮件接口一律明文投递；「验证方式」写入的 mail_auth
 * 则从未被任何装配点读取，AuthType 恒为 PLAIN，仅支持 LOGIN / CRAM-MD5 的服务端静默认证失败
 *
 * 断言 Secure / AuthType 的取值，而非 UseTLS —— 后者已被 Secure 取代（Send 先看 Secure），
 * 断言它只会掩盖 Secure 未装配的缺陷
 */
func TestGetEmailConfigSecureAndAuth(t *testing.T) {
	dir := t.TempDir()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(dir, "test.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	base := map[string]string{
		"mail_host":     "smtp.example.com",
		"mail_port":     "465",
		"mail_password": "secret",
		"mail_from":     "noreply@example.com",
	}

	cases := []struct {
		name       string
		extra      map[string]string
		wantSecure string
		wantAuth   string
	}{
		{
			name:       "设置页写入的 ssl 必须映射为 ssl（此前被忽略）",
			extra:      map[string]string{"mail_secure": "ssl"},
			wantSecure: "ssl",
			wantAuth:   "plain",
		},
		{
			name:       "设置页写入的 tls 必须映射为 tls（此前被忽略，退化成明文）",
			extra:      map[string]string{"mail_secure": "tls"},
			wantSecure: "tls",
			wantAuth:   "plain",
		},
		{
			name:       "显式无加密",
			extra:      map[string]string{"mail_secure": "none"},
			wantSecure: "none",
			wantAuth:   "plain",
		},
		{
			name:       "历史键 mail_tls 为 1 时等价 SSL",
			extra:      map[string]string{"mail_tls": "1"},
			wantSecure: "ssl",
			wantAuth:   "plain",
		},
		{
			name:       "mail_secure 显式 none 不被历史键 mail_tls 改写",
			extra:      map[string]string{"mail_secure": "none", "mail_tls": "1"},
			wantSecure: "none",
			wantAuth:   "plain",
		},
		{
			name:       "未配置时无加密，与设置页默认档之外的空值一致",
			extra:      map[string]string{},
			wantSecure: "none",
			wantAuth:   "plain",
		},
		{
			name:       "验证方式 login",
			extra:      map[string]string{"mail_auth": "login"},
			wantSecure: "none",
			wantAuth:   "login",
		},
		{
			name:       "验证方式 crammd5",
			extra:      map[string]string{"mail_auth": "crammd5"},
			wantSecure: "none",
			wantAuth:   "crammd5",
		},
		{
			name:       "未识别的验证方式回退 plain",
			extra:      map[string]string{"mail_auth": "unknown"},
			wantSecure: "none",
			wantAuth:   "plain",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := database.DB.Where("1 = 1").Delete(&models.SysConfig{}).Error; err != nil {
				t.Fatalf("清空配置失败: %v", err)
			}
			for k, v := range base {
				if err := database.DB.Create(&models.SysConfig{Key: k, Value: v}).Error; err != nil {
					t.Fatalf("写入 %s 失败: %v", k, err)
				}
			}
			for k, v := range tc.extra {
				if err := database.DB.Create(&models.SysConfig{Key: k, Value: v}).Error; err != nil {
					t.Fatalf("写入 %s 失败: %v", k, err)
				}
			}

			cfg, err := getEmailConfig()
			if err != nil {
				t.Fatalf("getEmailConfig 出错: %v", err)
			}
			if cfg == nil {
				t.Fatal("getEmailConfig 返回 nil，期望非空配置")
			}
			if cfg.Secure != tc.wantSecure {
				t.Errorf("Secure = %q，期望 %q", cfg.Secure, tc.wantSecure)
			}
			if cfg.AuthType != tc.wantAuth {
				t.Errorf("AuthType = %q，期望 %q", cfg.AuthType, tc.wantAuth)
			}
		})
	}
}
