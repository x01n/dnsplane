package notify

import "testing"

/*
 * 渠道启用开关（*_enabled）测试
 *
 * 语义：既要有配置，又要开关未显式关闭。未配置一律视为启用，这是零倒退的关键——
 * 这些键此前从未被后端消费，绝大多数实例的库中根本不存在，
 * 按「未配置=关」处理会让升级后所有通知渠道静默失效。
 */

/*
 * TestChannelEnabledDefaultOn 未配置与各类显式开启值都应判定为启用
 * 未配置=启用是本轮的核心裁决，单独列出以防被误改为默认关闭
 */
func TestChannelEnabledDefaultOn(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]string
	}{
		{"键不存在", map[string]string{}},
		{"空字符串", map[string]string{"mail_enabled": ""}},
		{"仅空白", map[string]string{"mail_enabled": "   "}},
		{"true", map[string]string{"mail_enabled": "true"}},
		{"1", map[string]string{"mail_enabled": "1"}},
		{"yes", map[string]string{"mail_enabled": "yes"}},
		{"on", map[string]string{"mail_enabled": "on"}},
		{"任意非关闭值", map[string]string{"mail_enabled": "enabled"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !ChannelEnabled(c.cfg, "mail_enabled") {
				t.Errorf("%s: 应判定为启用", c.name)
			}
		})
	}
}

/* TestChannelEnabledExplicitOff 仅显式关闭值才判定为关闭，且大小写与首尾空白不敏感 */
func TestChannelEnabledExplicitOff(t *testing.T) {
	cases := []string{"0", "false", "no", "off", "FALSE", "Off", " 0 ", "NO"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			if ChannelEnabled(map[string]string{"mail_enabled": v}, "mail_enabled") {
				t.Errorf("%q: 应判定为关闭", v)
			}
		})
	}
}

/*
 * TestChannelEnabledAllTenKeys 十个渠道开关逐一覆盖
 *
 * 逐个列出而不是循环 FYI 生成，是为了让键名拼写错误在测试里可见：
 * 键名与前端 settings/page.tsx 的 config.<key> 一一对应
 */
func TestChannelEnabledAllTenKeys(t *testing.T) {
	keys := []string{
		"mail_enabled",
		"tgbot_enabled",
		"discord_enabled",
		"bark_enabled",
		"wechat_enabled",
		"dingtalk_enabled",
		"feishu_enabled",
		"wxwork_app_enabled",
		"wxtpl_enabled",
		"webhook_enabled",
	}
	for _, key := range keys {
		t.Run(key+"未配置为启用", func(t *testing.T) {
			if !ChannelEnabled(map[string]string{}, key) {
				t.Errorf("%s 未配置时应视为启用", key)
			}
		})
		t.Run(key+"显式关闭", func(t *testing.T) {
			if ChannelEnabled(map[string]string{key: "0"}, key) {
				t.Errorf("%s=0 时应视为关闭", key)
			}
		})
		t.Run(key+"显式开启", func(t *testing.T) {
			if !ChannelEnabled(map[string]string{key: "1"}, key) {
				t.Errorf("%s=1 时应视为启用", key)
			}
		})
	}
}

/* TestNotifyConfigKeysIncludeEnabledSwitches 十个开关必须登记进 notifyConfigKeys，否则 getter 装载时读不到 */
func TestNotifyConfigKeysIncludeEnabledSwitches(t *testing.T) {
	required := []string{
		"mail_enabled",
		"tgbot_enabled",
		"discord_enabled",
		"bark_enabled",
		"wechat_enabled",
		"dingtalk_enabled",
		"feishu_enabled",
		"wxwork_app_enabled",
		"wxtpl_enabled",
		"webhook_enabled",
	}
	have := make(map[string]bool)
	for _, k := range NotifyConfigKeys() {
		have[k] = true
	}
	for _, k := range required {
		if !have[k] {
			t.Errorf("配置键 %s 未登记进 notifyConfigKeys", k)
		}
	}
}

/*
 * TestLoadNotifiersFromConfigEnabledAndRequiresConfig 开关的 AND 语义
 *
 * 三个分支逐一验证：
 *   - 配置齐全 + 未配置 enabled → 装载（零倒退）
 *   - 配置齐全 + 显式关闭 → 不装载
 *   - 配置齐全 + 显式开启 → 装载
 *   - 开关开启但配置缺失 → 不装载（AND 的另一半）
 */
func TestLoadNotifiersFromConfigEnabledAndRequiresConfig(t *testing.T) {
	tgbotConfig := map[string]string{
		"tgbot_token":  "tk",
		"tgbot_chatid": "-100123",
	}

	cases := []struct {
		name string
		cfg  map[string]string
		want int
	}{
		{
			name: "配置齐全且未配置开关",
			cfg:  tgbotConfig,
			want: 1,
		},
		{
			name: "配置齐全且开关开启",
			cfg:  merge(tgbotConfig, map[string]string{"tgbot_enabled": "true"}),
			want: 1,
		},
		{
			name: "配置齐全但开关显式关闭",
			cfg:  merge(tgbotConfig, map[string]string{"tgbot_enabled": "false"}),
			want: 0,
		},
		{
			name: "开关开启但配置缺失",
			cfg:  map[string]string{"tgbot_enabled": "true"},
			want: 0,
		},
		{
			name: "开关关闭且配置缺失",
			cfg:  map[string]string{"tgbot_enabled": "0"},
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewManager()
			LoadNotifiersFromConfig(m, c.cfg)
			if got := loadedTypes(m)["TelegramNotifier"]; got != c.want {
				t.Errorf("TelegramNotifier = %d，期望 %d", got, c.want)
			}
		})
	}
}

/*
 * TestLoadMailNotifierHonoursEnabledSwitch 邮件开关的三分支
 * 调用 LoadMailNotifier 而非全量装载，锁定 mail_enabled 在共用函数内部判定这一实现选择：
 * monitor 也调用同一函数，判定若写在两处会分叉
 */
func TestLoadMailNotifierHonoursEnabledSwitch(t *testing.T) {
	base := map[string]string{
		"mail_host":     "smtp.example.com",
		"mail_port":     "465",
		"mail_user":     "u@example.com",
		"mail_password": "secret",
		"mail_from":     "noreply@example.com",
	}

	t.Run("未配置开关时装载", func(t *testing.T) {
		mgr := NewManager()
		if !LoadMailNotifier(mgr, base) {
			t.Fatal("未配置 mail_enabled 时应装载")
		}
	})
	t.Run("开关开启时装载", func(t *testing.T) {
		mgr := NewManager()
		if !LoadMailNotifier(mgr, merge(base, map[string]string{"mail_enabled": "1"})) {
			t.Fatal("mail_enabled=1 时应装载")
		}
	})
	t.Run("开关显式关闭时不装载", func(t *testing.T) {
		mgr := NewManager()
		if LoadMailNotifier(mgr, merge(base, map[string]string{"mail_enabled": "0"})) {
			t.Fatal("mail_enabled=0 时不应装载")
		}
		if len(mgr.notifiers) != 0 {
			t.Errorf("开关关闭时应零装载，实际 %d 个", len(mgr.notifiers))
		}
	})
	t.Run("开关关闭优先于配置齐全", func(t *testing.T) {
		mgr := NewManager()
		full := merge(base, map[string]string{
			"mail_enabled":  "off",
			"mail_type":     "1",
			"mail_api_user": "api",
			"mail_api_key":  "key",
		})
		if LoadMailNotifier(mgr, full) {
			t.Fatal("Sendcloud 后端在 mail_enabled=off 时也不应装载")
		}
	})
}

/*
 * TestLoadNotifiersAllChannelsDisabledIndividually 十个渠道逐个显式关闭后均不得装载
 * 同时覆盖「开关接线是否落到正确的渠道」：关错键会让别的渠道被误伤
 */
func TestLoadNotifiersAllChannelsDisabledIndividually(t *testing.T) {
	cases := []struct {
		key         string
		cfg         map[string]string
		wantType    string
		otherConfig map[string]string
	}{
		{"tgbot_enabled", map[string]string{"tgbot_token": "tk", "tgbot_chatid": "1"}, "TelegramNotifier", nil},
		{"webhook_enabled", map[string]string{"webhook_url": "https://example.com/hook"}, "WebhookNotifier", nil},
		{"discord_enabled", map[string]string{"discord_webhook": "https://discord.com/api/webhooks/x"}, "DiscordNotifier", nil},
		{"bark_enabled", map[string]string{"bark_server": "https://api.day.app", "bark_key": "k"}, "BarkNotifier", nil},
		{"wechat_enabled", map[string]string{"wechat_webhook": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x"}, "WechatWorkNotifier", nil},
		{"dingtalk_enabled", map[string]string{"dingtalk_webhook": "https://oapi.dingtalk.com/robot/send?access_token=x"}, "DingTalkNotifier", nil},
		{"feishu_enabled", map[string]string{"feishu_webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/x"}, "FeishuNotifier", nil},
		{
			"wxwork_app_enabled",
			map[string]string{"wxwork_corpid": "corp", "wxwork_agentid": "1", "wxwork_secret": "s"},
			"WxWorkAppNotifier", nil,
		},
		{
			"wxtpl_enabled",
			map[string]string{
				"wxtpl_appid": "appid", "wxtpl_appsecret": "secret",
				"wxtpl_template_id": "tpl", "wxtpl_users": "openid1",
			},
			"WxTplNotifier", nil,
		},
	}

	for _, c := range cases {
		t.Run(c.key+"关闭时不装载", func(t *testing.T) {
			m := NewManager()
			LoadNotifiersFromConfig(m, merge(c.cfg, map[string]string{c.key: "0"}))
			if got := loadedTypes(m)[c.wantType]; got != 0 {
				t.Errorf("%s=0 时 %s 不应装载，实际 %d 个", c.key, c.wantType, got)
			}
		})
		t.Run(c.key+"未配置时装载", func(t *testing.T) {
			m := NewManager()
			LoadNotifiersFromConfig(m, c.cfg)
			if got := loadedTypes(m)[c.wantType]; got != 1 {
				t.Errorf("%s 未配置时 %s 应装载 1 个，实际 %d 个", c.key, c.wantType, got)
			}
		})
	}
}

/* merge 返回 left 与 right 的并集，不改动入参 */
func merge(left, right map[string]string) map[string]string {
	out := make(map[string]string, len(left)+len(right))
	for k, v := range left {
		out[k] = v
	}
	for k, v := range right {
		out[k] = v
	}
	return out
}
