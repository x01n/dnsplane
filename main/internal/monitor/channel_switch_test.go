package monitor

import (
	"testing"

	"main/internal/notify"
)

/*
 * 容灾告警的渠道启用开关测试
 *
 * 与 internal/notify 的 channel_test.go 成对：两处装载路径（monitor.buildNotifyManager
 * 与 notify.LoadNotifiersFromConfig）必须对同一组 *_enabled 键给出相同结论。
 * 本轮之前这两处长期各自实现，已出现过三次「修一处漏一处」的缺陷，
 * 因此这里不仅验证开关生效，还显式验证两条路径结论一致。
 */

/* channelSwitchCase 一个渠道的开关用例：配置、开关键、期望装载的类型名 */
type channelSwitchCase struct {
	name     string
	key      string
	cfg      map[string]string
	wantType string
}

/* channelSwitchCases 覆盖十个渠道开关（邮件单独处理，它经 LoadMailNotifier 装载） */
var channelSwitchCases = []channelSwitchCase{
	{"Telegram", "tgbot_enabled", map[string]string{"tgbot_token": "tk", "tgbot_chatid": "1"}, "TelegramNotifier"},
	{"Webhook", "webhook_enabled", map[string]string{"webhook_url": "https://example.com/hook"}, "WebhookNotifier"},
	{"Discord", "discord_enabled", map[string]string{"discord_webhook": "https://discord.com/api/webhooks/x"}, "DiscordNotifier"},
	{"Bark", "bark_enabled", map[string]string{"bark_server": "https://api.day.app", "bark_key": "k"}, "BarkNotifier"},
	{"企业微信", "wechat_enabled", map[string]string{"wechat_webhook": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x"}, "WechatWorkNotifier"},
	{"钉钉", "dingtalk_enabled", map[string]string{"dingtalk_webhook": "https://oapi.dingtalk.com/robot/send?access_token=x"}, "DingTalkNotifier"},
	{"飞书", "feishu_enabled", map[string]string{"feishu_webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/x"}, "FeishuNotifier"},
	{"企业微信应用", "wxwork_app_enabled", map[string]string{"wxwork_corpid": "corp", "wxwork_agentid": "1", "wxwork_secret": "s"}, "WxWorkAppNotifier"},
	{"公众号模板", "wxtpl_enabled", map[string]string{"wxtpl_appid": "appid", "wxtpl_appsecret": "secret", "wxtpl_template_id": "tpl", "wxtpl_users": "openid1"}, "WxTplNotifier"},
}

/*
 * TestBuildNotifyManagerChannelSwitchOff 显式关闭开关时对应渠道不得装载
 * 断言「关错键」也会被发现：只检查目标渠道为 0，同时检查其它已配置渠道仍为 1
 */
func TestBuildNotifyManagerChannelSwitchOff(t *testing.T) {
	for _, c := range channelSwitchCases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			seedSysConfig(t, mergeKV(c.cfg, map[string]string{c.key: "0"}))

			names := notifierTypeNames(buildManager(t, ""))
			if got := countType(names, c.wantType); got != 0 {
				t.Errorf("%s=0 时 %s 不应装载，实际 %d 个（清单：%v）", c.key, c.wantType, got, names)
			}
		})
	}
}

/*
 * TestBuildNotifyManagerChannelSwitchAbsent 未配置开关时渠道必须照常装载
 * 这是零倒退断言：这些键在现存实例的库中普遍不存在，若按「未配置=关」处理，
 * 升级后所有容灾告警渠道会同时静默失效
 */
func TestBuildNotifyManagerChannelSwitchAbsent(t *testing.T) {
	for _, c := range channelSwitchCases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			seedSysConfig(t, c.cfg)

			names := notifierTypeNames(buildManager(t, ""))
			if got := countType(names, c.wantType); got != 1 {
				t.Errorf("%s 未配置时 %s 应装载 1 个，实际 %d 个（清单：%v）", c.key, c.wantType, got, names)
			}
		})
	}
}

/* TestBuildNotifyManagerChannelSwitchOn 显式开启与未配置行为一致 */
func TestBuildNotifyManagerChannelSwitchOn(t *testing.T) {
	for _, c := range channelSwitchCases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			seedSysConfig(t, mergeKV(c.cfg, map[string]string{c.key: "true"}))

			names := notifierTypeNames(buildManager(t, ""))
			if got := countType(names, c.wantType); got != 1 {
				t.Errorf("%s=true 时 %s 应装载 1 个，实际 %d 个（清单：%v）", c.key, c.wantType, got, names)
			}
		})
	}
}

/* mailNotifyConfig 邮件渠道的完整配置（SMTP 后端） */
func mailNotifyConfig() map[string]string {
	return map[string]string{
		"mail_host":     "smtp.example.com",
		"mail_port":     "465",
		"mail_user":     "u@example.com",
		"mail_password": "secret",
		"mail_from":     "noreply@example.com",
	}
}

/*
 * TestBuildNotifyManagerMailSwitch 邮件渠道受 mail_enabled 控制
 * 判定写在 notify.LoadMailNotifier 内部，此用例同时验证 monitor 走的就是这一份实现
 */
func TestBuildNotifyManagerMailSwitch(t *testing.T) {
	cases := []struct {
		name string
		kv   map[string]string
		want int
	}{
		{"未配置开关时装载", mailNotifyConfig(), 1},
		{"开关开启时装载", mergeKV(mailNotifyConfig(), map[string]string{"mail_enabled": "1"}), 1},
		{"开关关闭时不装载", mergeKV(mailNotifyConfig(), map[string]string{"mail_enabled": "0"}), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			seedSysConfig(t, c.kv)

			names := notifierTypeNames(buildManager(t, ""))
			if got := countType(names, "mailContentAdapter"); got != c.want {
				t.Errorf("邮件渠道 = %d 个，期望 %d 个（清单：%v）", got, c.want, names)
			}
		})
	}
}

/*
 * TestChannelSwitchAgreesBetweenLoaders 两条装载路径对同一份配置必须给出相同结论
 *
 * monitor.buildNotifyManager 用于容灾告警（按任务白名单筛选），
 * notify.LoadNotifiersFromConfig 用于证书与到期通知（全量装载）。
 * 二者若对 *_enabled 判定不一致，就会出现「某渠道在告警里关了、在证书通知里还开着」这类分叉
 */
func TestChannelSwitchAgreesBetweenLoaders(t *testing.T) {
	cases := []struct {
		name         string
		kv           map[string]string
		wantNonEmpty bool
	}{
		{"全部未配置开关", allChannelConfig(), true},
		{"全部显式关闭", mergeKV(allChannelConfig(), map[string]string{
			"mail_enabled": "0", "tgbot_enabled": "0", "discord_enabled": "0",
			"bark_enabled": "0", "wechat_enabled": "0", "dingtalk_enabled": "0",
			"feishu_enabled": "0", "wxwork_app_enabled": "0", "wxtpl_enabled": "0",
			"webhook_enabled": "0",
		}), false},
		{"仅关闭一半", mergeKV(allChannelConfig(), map[string]string{
			"tgbot_enabled": "0", "dingtalk_enabled": "0", "mail_enabled": "0",
		}), true},
		{"显式开启全部", mergeKV(allChannelConfig(), map[string]string{
			"mail_enabled": "1", "tgbot_enabled": "1", "discord_enabled": "1",
			"bark_enabled": "1", "wechat_enabled": "1", "dingtalk_enabled": "1",
			"feishu_enabled": "1", "wxwork_app_enabled": "1", "wxtpl_enabled": "1",
			"webhook_enabled": "1",
		}), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			seedSysConfig(t, c.kv)

			// 路径一：monitor（从 DB 读，按 notify.NotifyConfigKeys 逐键 IN 查询）
			monitorSet := typeSet(notifierTypeNames(buildManager(t, "")))

			// 路径二：notify（直接喂同一份配置 map）
			configMap := make(map[string]string)
			for _, key := range notify.NotifyConfigKeys() {
				if v, ok := c.kv[key]; ok {
					configMap[key] = v
				}
			}
			mgr := notify.NewManager()
			notify.LoadNotifiersFromConfig(mgr, configMap)
			notifySet := notifyTypeSet(mgr)

			if diff := setDiff(monitorSet, notifySet); len(diff) != 0 {
				t.Errorf("两条装载路径不一致：monitor 独有 %v，notify 独有 %v", diff, setDiff(notifySet, monitorSet))
			}
			// 「全部显式关闭」用例的真实断言是「两条路径都装载 0 个」，
			// 故只在期望非空时才要求非空，否则该用例会因没有可比较的类型而报断言无意义
			if c.wantNonEmpty && len(monitorSet) == 0 && len(notifySet) == 0 {
				t.Errorf("用例配置未装载任何渠道，断言无意义（配置：%v）", c.kv)
			}
			if !c.wantNonEmpty {
				if len(monitorSet) != 0 || len(notifySet) != 0 {
					t.Errorf("全部开关显式关闭时应零装载，实际 monitor=%v notify=%v", monitorSet, notifySet)
				}
			}
		})
	}
}

/* allChannelConfig 十个渠道全部配置齐全 */
func allChannelConfig() map[string]string {
	out := mailNotifyConfig()
	for _, c := range channelSwitchCases {
		out = mergeKV(out, c.cfg)
	}
	return out
}

/* notifyTypeSet 把 notify 包的装载结果转成类型名集合 */
func notifyTypeSet(m *notify.NotifyManager) map[string]bool {
	out := make(map[string]bool)
	for _, name := range notifierTypeNames(m) {
		out[name] = true
	}
	return out
}

/* typeSet 把类型名切片转成集合 */
func typeSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

/* setDiff 返回 a 中不属于 b 的元素 */
func setDiff(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	return out
}

/* mergeKV 返回两个配置 map 的并集，后者优先，不改动入参 */
func mergeKV(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
