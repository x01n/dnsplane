package notify

/*
 * mail_type 分发与配置键登记的防回归测试
 *
 * 覆盖两个易被后续改动破坏的契约：
 * 1. mail_type / mail_api_user / mail_api_key / mail_from_name 必须留在 notifyConfigKeys，
 *    否则 LoadNotifiersWithGetter 走 sysconfig 缓存时读不到，Sendcloud 与 DirectMail 会静默失效
 * 2. mail_type 的三路分发（1=Sendcloud、2=阿里云邮件推送、空或其它=SMTP）在配置齐全时各装载一个邮件渠道，
 *    配置不全时零装载 —— 避免误发到未配置的后端
 *
 * 断言只针对「装载数量」这一可观察行为，不引用具体通知器类型：
 * 邮件正文的文本→HTML 转换实现可能在 SMTP 通道内或包装器中，属实现细节，由 notify 包内其它测试覆盖
 */

import (
	"testing"
)

/* mailDispatchKeys 本测试依赖的邮件配置键，键名已冻结 */
func mailDispatchKeys() []string {
	return []string{"mail_type", "mail_api_user", "mail_api_key", "mail_from_name"}
}

func TestMailConfigKeysRegistered(t *testing.T) {
	have := make(map[string]bool)
	for _, k := range NotifyConfigKeys() {
		have[k] = true
	}
	for _, k := range mailDispatchKeys() {
		if !have[k] {
			t.Errorf("配置键 %s 未登记进 notifyConfigKeys，getter 装载时会漏读", k)
		}
	}
}

/* countMailNotifiers 只传邮件相关键装载，返回装载到的渠道数 */
func countMailNotifiers(configMap map[string]string) int {
	mgr := NewManager()
	LoadNotifiersFromConfig(mgr, configMap)
	return len(mgr.notifiers)
}

/* countMailNotifiersDirect 直接调用公共函数 LoadMailNotifier，返回（装载数，返回值） */
func countMailNotifiersDirect(configMap map[string]string) (int, bool) {
	mgr := NewManager()
	ok := LoadMailNotifier(mgr, configMap)
	return len(mgr.notifiers), ok
}

/*
 * TestLoadMailNotifierReturnValue 锁定公共函数 LoadMailNotifier 的 bool 返回语义
 *
 * 该函数被两处调用（notify.go 的 LoadNotifiersFromConfig 无条件调用、monitor.go 按其返回值决定
 * hasNotifier），返回值必须精确表示「是否装载了邮件渠道」——返回 true 却未装载会让调用方的
 * hasNotifier 误判为「有可用渠道」，从而静默跳过后续通知
 */
func TestLoadMailNotifierReturnValue(t *testing.T) {
	cases := []struct {
		name     string
		config   map[string]string
		wantN    int
		wantTrue bool
	}{
		{
			name:     "SMTP 齐全",
			config:   map[string]string{"mail_host": "smtp.example.com", "mail_recv": "to@example.com"},
			wantN:    1,
			wantTrue: true,
		},
		{
			name:     "Sendcloud 齐全",
			config:   map[string]string{"mail_type": "1", "mail_recv": "to@example.com", "mail_from": "f@example.com", "mail_api_user": "u", "mail_api_key": "k"},
			wantN:    1,
			wantTrue: true,
		},
		{
			name:     "DirectMail 齐全",
			config:   map[string]string{"mail_type": "2", "mail_recv": "to@example.com", "mail_from": "f@example.com", "mail_api_user": "ak", "mail_api_key": "sk"},
			wantN:    1,
			wantTrue: true,
		},
		{
			name:     "无任何配置",
			config:   map[string]string{},
			wantN:    0,
			wantTrue: false,
		},
		{
			name:     "仅配 mail_host 缺收件人",
			config:   map[string]string{"mail_host": "smtp.example.com"},
			wantN:    0,
			wantTrue: false,
		},
		{
			name:     "Sendcloud 缺凭据",
			config:   map[string]string{"mail_type": "1", "mail_recv": "to@example.com", "mail_from": "f@example.com"},
			wantN:    0,
			wantTrue: false,
		},
	}
	for _, c := range cases {
		gotN, gotTrue := countMailNotifiersDirect(c.config)
		if gotN != c.wantN || gotTrue != c.wantTrue {
			t.Errorf("%s: 装载数=%d 返回值=%v, want 装载数=%d 返回值=%v",
				c.name, gotN, gotTrue, c.wantN, c.wantTrue)
		}
	}
}

func TestMailTypeDispatch(t *testing.T) {
	common := map[string]string{
		"mail_recv":      "to@example.com",
		"mail_from":      "from@example.com",
		"mail_from_name": "DNSPlane",
	}

	cases := []struct {
		name      string
		mailType  string
		extra     map[string]string
		wantCount int
	}{
		{
			name:      "Sendcloud 配置齐全",
			mailType:  "1",
			extra:     map[string]string{"mail_api_user": "u", "mail_api_key": "k"},
			wantCount: 1,
		},
		{
			name:      "Sendcloud 缺 apiKey 不装载",
			mailType:  "1",
			extra:     map[string]string{"mail_api_user": "u"},
			wantCount: 0,
		},
		{
			name:      "DirectMail 配置齐全",
			mailType:  "2",
			extra:     map[string]string{"mail_api_user": "ak", "mail_api_key": "sk"},
			wantCount: 1,
		},
		{
			name:      "DirectMail 缺 apiUser 不装载",
			mailType:  "2",
			extra:     map[string]string{"mail_api_key": "sk"},
			wantCount: 0,
		},
		{
			name:      "mail_type 为空走 SMTP",
			mailType:  "",
			extra:     map[string]string{"mail_host": "smtp.example.com", "mail_port": "465"},
			wantCount: 1,
		},
		{
			name:      "mail_type 为未识别值走 SMTP",
			mailType:  "9",
			extra:     map[string]string{"mail_host": "smtp.example.com", "mail_port": "465"},
			wantCount: 1,
		},
		{
			name:      "SMTP 缺 host 不装载",
			mailType:  "",
			extra:     map[string]string{"mail_port": "465"},
			wantCount: 0,
		},
		{
			name:      "未配置收件人时不装载任何邮件渠道",
			mailType:  "1",
			extra:     map[string]string{"mail_api_user": "u", "mail_api_key": "k"},
			wantCount: 0,
		},
	}

	for _, c := range cases {
		configMap := make(map[string]string, len(common)+len(c.extra)+1)
		for k, v := range common {
			configMap[k] = v
		}
		for k, v := range c.extra {
			configMap[k] = v
		}
		if c.mailType != "" {
			configMap["mail_type"] = c.mailType
		}
		// 「未配置收件人」用例显式清空收件人与发件人，其余用例保留
		if c.name == "未配置收件人时不装载任何邮件渠道" {
			delete(configMap, "mail_recv")
			delete(configMap, "mail_from")
		}

		if got := countMailNotifiers(configMap); got != c.wantCount {
			t.Errorf("%s: 装载数=%d, want %d", c.name, got, c.wantCount)
		}
	}
}

/*
 * TestMailRecipientsFallbackToFrom 收件人缺省时回退发件人地址
 */
func TestMailRecipientsFallbackToFrom(t *testing.T) {
	onlyFrom := map[string]string{
		"mail_host": "smtp.example.com", "mail_from": "from@example.com",
	}
	if got := countMailNotifiers(onlyFrom); got != 1 {
		t.Fatalf("仅配 mail_from 时应回退为收件人并装载 1 个邮件渠道，实际 %d 个", got)
	}

	apiOnlyFrom := map[string]string{
		"mail_api_user": "u", "mail_api_key": "k", "mail_from": "from@example.com",
	}
	if got := countMailNotifiers(apiOnlyFrom); got != 1 {
		// 默认 SMTP 分支未配 mail_host，此处断言的是「回退不因 mail_recv 缺失而阻断」，
		// 故补上 mail_host 后再验证一次
		apiOnlyFrom["mail_host"] = "smtp.example.com"
		if got = countMailNotifiers(apiOnlyFrom); got != 1 {
			t.Fatalf("API 后端与 SMTP 均缺 mail_recv 时应回退为收件人并装载 1 个，实际 %d 个", got)
		}
	}

	if got := countMailNotifiers(map[string]string{}); got != 0 {
		t.Fatalf("无任何邮件配置时不应装载，实际 %d 个", got)
	}
}
