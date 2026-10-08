package notify

/*
 * LoadMailNotifier 的防回归测试
 *
 * 该函数自 LoadNotifiersFromConfig 提取而来，使 internal/monitor 的容灾告警能与常规通知
 * 共用同一份 mail_type 三路分发与 mail_recv→mail_from 回退。monitor 原先自带一份等价实现，
 * 守卫上多带了 mail_host 与 mail_recv 两个前置条件，导致：
 * 1. 仅配发件人时收件人回退不可达，邮件渠道静默失效
 * 2. 配 Sendcloud / DirectMail（通常不填 mail_host）时整条邮件分支被拦
 * 3. 装载的邮件后端未经 mailContentAdapter 包装，纯文本正文在 HTML 邮件中换行塌陷
 * 本测试锁定提取后的契约：三路分发、收件人回退、包装类型、以及与全量装载的一致性
 */

import (
	"reflect"
	"testing"
)

/* loadMailOnly 只装载邮件渠道，返回管理器与是否装载 */
func loadMailOnly(configMap map[string]string) (*NotifyManager, bool) {
	mgr := NewManager()
	return mgr, LoadMailNotifier(mgr, configMap)
}

/* unwrapMailBackend 拆出 adapter 内层后端；未被包装时返回 false */
func unwrapMailBackend(n Notifier) (Notifier, bool) {
	a, ok := n.(*mailContentAdapter)
	if !ok {
		return nil, false
	}
	return a.inner, true
}

/* backendTypeName 返回具体后端类型名，用于断言三路分发选中的后端 */
func backendTypeName(n Notifier) string {
	return reflect.TypeOf(n).Elem().Name()
}

/*
 * TestLoadMailNotifierFallbackToFromOnly 仅配 mail_host 与 mail_from（无 mail_recv）时应装载 1 个
 * 收件人守卫必须用回退后的 mailTo，用 mail_recv 会让回退成为死代码
 */
func TestLoadMailNotifierFallbackToFromOnly(t *testing.T) {
	mgr, ok := loadMailOnly(map[string]string{
		"mail_host": "smtp.example.com", "mail_from": "from@example.com",
	})
	if !ok {
		t.Fatal("仅配 mail_from 时应回退为收件人并返回 true")
	}
	if len(mgr.notifiers) != 1 {
		t.Fatalf("应装载 1 个邮件渠道，实际 %d 个", len(mgr.notifiers))
	}
	inner, wrapped := unwrapMailBackend(mgr.notifiers[0])
	if !wrapped {
		t.Fatalf("装载项未经 mailContentAdapter 包装: %T", mgr.notifiers[0])
	}
	smtp, isSMTP := inner.(*EmailNotifier)
	if !isSMTP {
		t.Fatalf("默认分支应使用 SMTP 后端，实际 %s", backendTypeName(inner))
	}
	if smtp.config.To != "from@example.com" {
		t.Errorf("收件人未回退到 mail_from: %q", smtp.config.To)
	}
	if smtp.config.From != "from@example.com" {
		t.Errorf("发件人传递错误: %q", smtp.config.From)
	}
}

/*
 * TestLoadMailNotifierBackendWithoutHost mail_type=1/2 的 API 后端不要求 mail_host
 * monitor 原先把 mail_host 非空当作整条邮件分支的前置条件，配 API 后端时被整条拦下
 */
func TestLoadMailNotifierBackendWithoutHost(t *testing.T) {
	cases := []struct {
		name     string
		mailType string
		wantType string
	}{
		{name: "Sendcloud", mailType: "1", wantType: "SendcloudNotifier"},
		{name: "阿里云 DirectMail", mailType: "2", wantType: "AliyunDirectMailNotifier"},
	}
	for _, c := range cases {
		mgr, ok := loadMailOnly(map[string]string{
			"mail_type": c.mailType, "mail_api_user": "u", "mail_api_key": "k",
			"mail_from": "from@example.com",
		})
		if !ok || len(mgr.notifiers) != 1 {
			t.Errorf("%s: 未配 mail_host 时也应装载 1 个，ok=%v 实际 %d 个", c.name, ok, len(mgr.notifiers))
			continue
		}
		inner, wrapped := unwrapMailBackend(mgr.notifiers[0])
		if !wrapped {
			t.Errorf("%s: 装载项未经 mailContentAdapter 包装: %T", c.name, mgr.notifiers[0])
			continue
		}
		if name := backendTypeName(inner); name != c.wantType {
			t.Errorf("%s: 后端类型=%s, want %s", c.name, name, c.wantType)
		}
	}

	// 默认 SMTP 分支仍要求 mail_host：缺 host 时不得装载
	if _, ok := loadMailOnly(map[string]string{"mail_from": "from@example.com"}); ok {
		t.Error("SMTP 分支缺 mail_host 时不应装载")
	}
}

/*
 * TestLoadMailNotifierWrapsContentAdapter 三路后端装载后内层类型可辨识且统一经 adapter 包装
 * 包装是纯文本正文换行生效的唯一保证，缺失即导致告警正文塌成一行
 */
func TestLoadMailNotifierWrapsContentAdapter(t *testing.T) {
	configs := []map[string]string{
		{"mail_host": "smtp.example.com", "mail_from": "from@example.com"},
		{"mail_type": "1", "mail_api_user": "u", "mail_api_key": "k", "mail_from": "from@example.com"},
		{"mail_type": "2", "mail_api_user": "ak", "mail_api_key": "sk", "mail_from": "from@example.com"},
	}
	for i, cfg := range configs {
		mgr, ok := loadMailOnly(cfg)
		if !ok || len(mgr.notifiers) != 1 {
			t.Fatalf("配置 %d: 应装载 1 个，ok=%v 实际 %d 个", i, ok, len(mgr.notifiers))
		}
		if _, wrapped := unwrapMailBackend(mgr.notifiers[0]); !wrapped {
			t.Errorf("配置 %d: 未经 mailContentAdapter 包装: %T", i, mgr.notifiers[0])
		}
	}
}

/* TestLoadMailNotifierIncompleteConfig 配置不全时返回 false 且零装载，避免误发到未配置的后端 */
func TestLoadMailNotifierIncompleteConfig(t *testing.T) {
	cases := []struct {
		name      string
		configMap map[string]string
	}{
		{name: "空配置", configMap: map[string]string{}},
		{name: "仅有 host 无发件人", configMap: map[string]string{"mail_host": "smtp.example.com"}},
		{name: "Sendcloud 缺 apiKey", configMap: map[string]string{"mail_type": "1", "mail_api_user": "u", "mail_from": "f@example.com"}},
		{name: "DirectMail 缺 apiUser", configMap: map[string]string{"mail_type": "2", "mail_api_key": "k", "mail_from": "f@example.com"}},
		{name: "SMTP 缺 host", configMap: map[string]string{"mail_port": "465", "mail_from": "f@example.com"}},
	}
	for _, c := range cases {
		mgr, ok := loadMailOnly(c.configMap)
		if ok || len(mgr.notifiers) != 0 {
			t.Errorf("%s: ok=%v 装载数=%d, want false/0", c.name, ok, len(mgr.notifiers))
		}
	}
}

/*
 * TestLoadMailNotifierAgreesWithFullLoad 提取后全量装载中的邮件数与单独调用必须一致
 * LoadNotifiersFromConfig 改为委托 LoadMailNotifier，两者不得分叉
 * 邮件渠道在全量装载中唯一可辨识（只有邮件分支经 mailContentAdapter 包装）
 *
 * wantMail 是声明的期望值，不由实现推导：
 * 只做一致性断言时，两侧同时退化（守卫过窄或过宽）仍会全绿，
 * 三方比对（got == wantMail && fullMail == wantMail）才能锁住守卫的宽窄
 */
func TestLoadMailNotifierAgreesWithFullLoad(t *testing.T) {
	configs := []struct {
		cfg      map[string]string
		wantMail int
	}{
		// 仅配发件人：收件人回退 mail_from，应装载（本轮修复的回归防护点）
		{map[string]string{"mail_host": "smtp.example.com", "mail_from": "from@example.com"}, 1},
		// SMTP 完整配置
		{map[string]string{"mail_host": "smtp.example.com", "mail_port": "465", "mail_from": "from@example.com", "mail_recv": "to@example.com", "mail_secure": "ssl"}, 1},
		// Sendcloud
		{map[string]string{"mail_type": "1", "mail_api_user": "u", "mail_api_key": "k", "mail_from": "from@example.com"}, 1},
		// 阿里云邮件推送
		{map[string]string{"mail_type": "2", "mail_api_user": "ak", "mail_api_key": "sk", "mail_from": "from@example.com", "mail_recv": "to@example.com"}, 1},
		// 非法 mail_type 落 default 兜底 SMTP
		{map[string]string{"mail_type": "9", "mail_host": "smtp.example.com", "mail_from": "from@example.com"}, 1},
		// 有邮件服务器但无任何收件人来源，不得装载
		{map[string]string{"mail_host": "smtp.example.com"}, 0},
		// 空配置
		{map[string]string{}, 0},
	}
	for i, tc := range configs {
		full := NewManager()
		LoadNotifiersFromConfig(full, tc.cfg)
		fullMail := 0
		for _, n := range full.notifiers {
			if _, isMail := n.(*mailContentAdapter); isMail {
				fullMail++
			}
		}

		mgr, ok := loadMailOnly(tc.cfg)
		got := len(mgr.notifiers)
		if !ok {
			got = 0
		}
		if got != tc.wantMail {
			t.Errorf("配置 %d: LoadMailNotifier 装载 %d 个，期望 %d 个", i, got, tc.wantMail)
		}
		if fullMail != tc.wantMail {
			t.Errorf("配置 %d: 全量装载中邮件 %d 个，期望 %d 个", i, fullMail, tc.wantMail)
		}
	}
}

/*
 * TestLoadMailNotifierSMTPEncryptionAndAuth 装配 SMTP 的加密方式与鉴权方式
 *
 * 历史缺陷有两处，外部行为均为静默退化：
 *   1. 加密只读早期键 mail_tls，而设置页写入 mail_secure（none/ssl/tls），
 *      用户选 STARTTLS 时仍按明文投递，选 SSL 时同样明文（邮件可被 MITM 读取）
 *   2. 鉴权方式从未装配，AuthType 恒为空 → Send 落 default 用 PLAIN，
 *      仅支持 LOGIN / CRAM-MD5 的服务端认证失败且表现为「测试通知失败」这一笼统错误
 *
 * 断言配置对象的 Secure / AuthType 两个字段：它们是 Send 选择分支的唯一依据，
 * 断言装载数量无法覆盖这条路径（两个缺陷下装载数都正常为 1）
 */
func TestLoadMailNotifierSMTPEncryptionAndAuth(t *testing.T) {
	cases := []struct {
		name       string
		extra      map[string]string
		wantSecure string
		wantAuth   string
	}{
		{
			name:       "设置页默认档 STARTTLS",
			extra:      map[string]string{"mail_secure": "tls"},
			wantSecure: "tls",
			wantAuth:   "plain",
		},
		{
			name:       "设置页 SSL",
			extra:      map[string]string{"mail_secure": "ssl"},
			wantSecure: "ssl",
			wantAuth:   "plain",
		},
		{
			name:       "设置页无加密",
			extra:      map[string]string{"mail_secure": "none"},
			wantSecure: "none",
			wantAuth:   "plain",
		},
		{
			name:       "历史键 mail_tls=1 等价 SSL",
			extra:      map[string]string{"mail_tls": "1"},
			wantSecure: "ssl",
			wantAuth:   "plain",
		},
		{
			name:       "历史键 mail_tls=true 等价 SSL",
			extra:      map[string]string{"mail_tls": "true"},
			wantSecure: "ssl",
			wantAuth:   "plain",
		},
		{
			name:       "mail_secure 显式 none 不被历史键改写",
			extra:      map[string]string{"mail_secure": "none", "mail_tls": "1"},
			wantSecure: "none",
			wantAuth:   "plain",
		},
		{
			name:       "未配置加密方式时明文",
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
			name:       "验证方式 crammd5 大小写不敏感",
			extra:      map[string]string{"mail_auth": "CRAMMD5"},
			wantSecure: "none",
			wantAuth:   "crammd5",
		},
		{
			name:       "未识别的验证方式回退 plain",
			extra:      map[string]string{"mail_auth": "ntlm"},
			wantSecure: "none",
			wantAuth:   "plain",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := map[string]string{
				"mail_host": "smtp.example.com", "mail_port": "465", "mail_from": "from@example.com",
			}
			for k, v := range c.extra {
				cfg[k] = v
			}
			mgr, ok := loadMailOnly(cfg)
			if !ok || len(mgr.notifiers) != 1 {
				t.Fatalf("应装载 1 个邮件渠道，ok=%v 实际 %d 个", ok, len(mgr.notifiers))
			}
			inner, wrapped := unwrapMailBackend(mgr.notifiers[0])
			if !wrapped {
				t.Fatalf("装载项未经 mailContentAdapter 包装: %T", mgr.notifiers[0])
			}
			smtp, isSMTP := inner.(*EmailNotifier)
			if !isSMTP {
				t.Fatalf("应使用 SMTP 后端，实际 %s", backendTypeName(inner))
			}
			if smtp.config.Secure != c.wantSecure {
				t.Errorf("Secure = %q，期望 %q", smtp.config.Secure, c.wantSecure)
			}
			if smtp.config.AuthType != c.wantAuth {
				t.Errorf("AuthType = %q，期望 %q", smtp.config.AuthType, c.wantAuth)
			}
		})
	}
}

/*
 * TestMailAuthKeyRegisteredForGetter 邮件装配依赖的键必须登记进 NotifyConfigKeys
 *
 * LoadNotifiersWithGetter 按 NotifyConfigKeys 逐键回源，未登记的键在 map 中不存在，
 * 装配时取到空串 —— 表现为「设置页已选 LOGIN 但仍在用 PLAIN」这类只在特定装载路径出现的分叉
 */
func TestMailAuthKeyRegisteredForGetter(t *testing.T) {
	have := make(map[string]bool)
	for _, k := range NotifyConfigKeys() {
		have[k] = true
	}
	for _, k := range []string{"mail_auth", "mail_secure", "mail_type", "mail_api_user", "mail_api_key", "mail_from_name"} {
		if !have[k] {
			t.Errorf("配置键 %s 未登记进 NotifyConfigKeys，按 getter 装载时会取到空串", k)
		}
	}
}
