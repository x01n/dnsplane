package monitor

import (
	"reflect"
	"testing"

	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
	"main/internal/notify"
)

/*
 * 容灾告警通知渠道装配测试
 *
 * 覆盖本轮修复的三个缺陷：
 * 1. 配置读取用手写前缀 LIKE（mail_% / wxwork_% 等），漏掉 qqbot_%，且与 notify 的键集合分叉
 * 2. WxPusher 与 QQ 机器人没有装载块，在容灾告警中完全不可达
 * 3. Telegram 只装配了 BotToken / ChatID，漏掉 tgbot_topicid / tgbot_url / tgbot_proxy
 *
 * 全部使用临时 SQLite 与内存缓存，不发起任何外部网络请求
 */

/* setupMonitorTestDB 初始化临时 SQLite 与内存缓存 */
func setupMonitorTestDB(t *testing.T) {
	t.Helper()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: t.TempDir() + "/monitor.db",
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

/* seedSysConfig 逐条写入系统配置 */
func seedSysConfig(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := database.DB.Create(&models.SysConfig{Key: k, Value: v}).Error; err != nil {
			t.Fatalf("写入配置 %s 失败: %v", k, err)
		}
	}
}

/* buildManager 按指定任务渠道白名单构建通知管理器 */
func buildManager(t *testing.T, notifyChannelsJSON string) *notify.NotifyManager {
	t.Helper()
	m := New()
	return m.buildNotifyManager(models.DMTask{NotifyChannels: notifyChannelsJSON})
}

/*
 * notifierTypeNames 用反射读出 NotifyManager 中各 notifier 的具体类型名
 *
 * NotifyManager 未导出 notifiers 字段，也没有遍历接口，故用反射读取，
 * 避免为了可测性在生产代码上新增导出方法。
 * 注意：reflect.Type.Elem() 对接口类型不适用（Elem of invalid type），
 * 必须走 Value.Elem() 取动态值再取其指针的元素类型
 */
func notifierTypeNames(m *notify.NotifyManager) []string {
	if m == nil {
		return nil
	}
	v := reflect.ValueOf(m).Elem().FieldByName("notifiers")
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return nil
	}
	names := make([]string, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i).Elem()
		if !elem.IsValid() {
			continue
		}
		names = append(names, elem.Type().Elem().Name())
	}
	return names
}

/* countType 统计指定类型名的 notifier 个数 */
func countType(names []string, want string) int {
	n := 0
	for _, name := range names {
		if name == want {
			n++
		}
	}
	return n
}

/*
 * telegramConfigField 读取已装载 TelegramNotifier 的 config 内某字段
 *
 * TelegramNotifier.config 在 notify 包内未导出，且 monitor 不能反向依赖 notify 的内部结构，
 * 故沿反射链直接取字符串值。
 * 注意：reflect.Value.Interface() 对未导出字段会 panic，只有 Type() 与 String() 不受限，
 * 因此本条链上全程不做 Interface() 转换
 */
func telegramConfigField(m *notify.NotifyManager, field string) (string, bool) {
	if m == nil {
		return "", false
	}
	v := reflect.ValueOf(m).Elem().FieldByName("notifiers")
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return "", false
	}
	for i := 0; i < v.Len(); i++ {
		dyn := v.Index(i).Elem() // 接口的动态值：*TelegramNotifier
		if !dyn.IsValid() || dyn.Kind() != reflect.Ptr || dyn.Type().Elem().Name() != "TelegramNotifier" {
			continue
		}
		f := dyn.Elem().FieldByName("config").FieldByName(field)
		if f.Kind() == reflect.String {
			return f.String(), true
		}
	}
	return "", false
}

/*
 * TestBuildNotifyManagerQQBotReachable useAll 路径下 QQ 机器人必须可达
 * 回归点：monitor 原先用手写前缀 LIKE 读配置，清单里没有 qqbot_%，
 * 配置读不出来即无法装载，QQ 机器人在容灾告警中完全不可达
 */
func TestBuildNotifyManagerQQBotReachable(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"qqbot_appid":     "102000001",
		"qqbot_appsecret": "secret",
		"qqbot_openid":    "openid_1",
	})

	names := notifierTypeNames(buildManager(t, ""))
	if got := countType(names, "QQBotNotifier"); got != 1 {
		t.Errorf("useAll 下 QQBotNotifier 应装载 1 个，实际 %d 个（渠道清单：%v）", got, names)
	}
}

/*
 * TestBuildNotifyManagerWxPusherReachable useAll 路径下 WxPusher 必须可达
 * 其配置键 wechat_apptoken / wechat_appuid 原先虽被 wechat_% 覆盖，
 * 但 monitor 缺少装载块，同样不可达
 */
func TestBuildNotifyManagerWxPusherReachable(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"wechat_apptoken": "AT_token",
		"wechat_appuid":   "UID_user",
	})

	names := notifierTypeNames(buildManager(t, ""))
	if got := countType(names, "WxPusherNotifier"); got != 1 {
		t.Errorf("useAll 下 WxPusherNotifier 应装载 1 个，实际 %d 个（渠道清单：%v）", got, names)
	}
}

/* TestBuildNotifyManagerQQBotRequiresAllKeys 缺任一必需键时不得装载 QQ 机器人 */
func TestBuildNotifyManagerQQBotRequiresAllKeys(t *testing.T) {
	cases := []struct {
		name string
		kv   map[string]string
	}{
		{"缺 appid", map[string]string{"qqbot_appsecret": "s", "qqbot_openid": "o"}},
		{"缺 appsecret", map[string]string{"qqbot_appid": "a", "qqbot_openid": "o"}},
		{"缺 openid", map[string]string{"qqbot_appid": "a", "qqbot_appsecret": "s"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			seedSysConfig(t, c.kv)
			if n := countType(notifierTypeNames(buildManager(t, "")), "QQBotNotifier"); n != 0 {
				t.Errorf("%s: 不应装载 QQBotNotifier，实际 %d 个", c.name, n)
			}
		})
	}
}

/*
 * TestBuildNotifyManagerTelegramAdvanced Telegram 三个高级字段必须传入 TelegramConfig
 * 回归点：monitor 原先只装配 BotToken / ChatID，话题 ID、自定义反代地址、代理模式全部丢失
 *
 * 断言的是配置字段本身而非基址计算结果：
 * 只断言基址时，TopicID 漏装配仍会全绿（它与基址无关）
 */
func TestBuildNotifyManagerTelegramAdvanced(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"tgbot_token":   "tk",
		"tgbot_chatid":  "-100123",
		"tgbot_topicid": "42",
		"tgbot_url":     "https://tg.example.com/",
		"tgbot_proxy":   "2",
	})

	mgr := buildManager(t, "")
	if n := countType(notifierTypeNames(mgr), "TelegramNotifier"); n != 1 {
		t.Fatalf("应装载 1 个 TelegramNotifier，实际 %d 个（渠道清单：%v）", n, notifierTypeNames(mgr))
	}

	want := map[string]string{
		"BotToken":  "tk",
		"ChatID":    "-100123",
		"TopicID":   "42",
		"ServerURL": "https://tg.example.com/",
		"ProxyMode": "2",
	}
	for field, wantVal := range want {
		got, ok := telegramConfigField(mgr, field)
		if !ok {
			t.Errorf("TelegramConfig.%s 未装配到（字段读不到）", field)
			continue
		}
		if got != wantVal {
			t.Errorf("TelegramConfig.%s = %q, want %q", field, got, wantVal)
		}
	}
}

/*
 * TestBuildNotifyManagerChannelWhitelist 按任务白名单筛选时其余渠道不得装载
 * 覆盖新增的 wxpusher / qqbot 两个白名单值
 */
func TestBuildNotifyManagerChannelWhitelist(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"qqbot_appid":     "a",
		"qqbot_appsecret": "s",
		"qqbot_openid":    "o",
		"wechat_apptoken": "AT",
		"wechat_appuid":   "UID",
		"tgbot_token":     "tk",
		"tgbot_chatid":    "1",
	})

	cases := []struct {
		name         string
		channels     string
		wantQQBot    int
		wantWxPusher int
		wantTelegram int
	}{
		{"仅 qqbot", `["qqbot"]`, 1, 0, 0},
		{"仅 wxpusher", `["wxpusher"]`, 0, 1, 0},
		{"仅 telegram", `["telegram"]`, 0, 0, 1},
		{"白名单不含两者", `["bark"]`, 0, 0, 0},
		{"同时指定三个", `["qqbot","wxpusher","telegram"]`, 1, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mgr := buildManager(t, c.channels)
			names := notifierTypeNames(mgr)
			if got := countType(names, "QQBotNotifier"); got != c.wantQQBot {
				t.Errorf("QQBotNotifier=%d, want %d（渠道清单：%v）", got, c.wantQQBot, names)
			}
			if got := countType(names, "WxPusherNotifier"); got != c.wantWxPusher {
				t.Errorf("WxPusherNotifier=%d, want %d（渠道清单：%v）", got, c.wantWxPusher, names)
			}
			if got := countType(names, "TelegramNotifier"); got != c.wantTelegram {
				t.Errorf("TelegramNotifier=%d, want %d（渠道清单：%v）", got, c.wantTelegram, names)
			}
		})
	}
}

/*
 * TestBuildNotifyManagerAllChannelsReachable useAll 下 12 个渠道全部可达
 *
 * 这是对「配置键清单不再与 notify 分叉」最强的行为级断言：
 * 只要某个键从 notify.NotifyConfigKeys() 掉出去，monitor 的 IN 查询就读不到它，
 * 对应渠道随即装载失败，本用例转红 —— 不需要另维护一份键清单来比对
 */
func TestBuildNotifyManagerAllChannelsReachable(t *testing.T) {
	setupMonitorTestDB(t)
	// 每个渠道给足其装载守卫要求的全部键
	seedSysConfig(t, map[string]string{
		// 邮件（SMTP 分支）
		"mail_host": "smtp.example.com", "mail_from": "from@example.com",
		// Telegram
		"tgbot_token": "tk", "tgbot_chatid": "-100123",
		// 通用 Webhook
		"webhook_url": "https://example.com/hook",
		// Discord
		"discord_webhook": "https://discord.com/api/webhooks/x",
		// Bark
		"bark_url": "https://api.day.app", "bark_key": "bk",
		// 企业微信群机器人
		"wechat_webhook": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x",
		// 钉钉
		"dingtalk_webhook": "https://oapi.dingtalk.com/robot/send?access_token=x",
		// 飞书
		"feishu_webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/x",
		// 企业微信应用
		"wxwork_corpid": "corp", "wxwork_agentid": "1000002", "wxwork_secret": "s",
		// 微信公众号模板消息
		"wxtpl_appid": "wxappid", "wxtpl_appsecret": "wxsecret",
		"wxtpl_template_id": "tpl", "wxtpl_users": "user1",
		// WxPusher
		"wechat_apptoken": "AT_token", "wechat_appuid": "UID_user",
		// QQ 机器人
		"qqbot_appid": "102000001", "qqbot_appsecret": "secret", "qqbot_openid": "openid_1",
	})

	names := notifierTypeNames(buildManager(t, ""))
	// 邮件分支经 mailContentAdapter 包装，其类型名为未导出适配器的名字
	want := []string{
		"mailContentAdapter", "TelegramNotifier", "WebhookNotifier", "DiscordNotifier",
		"BarkNotifier", "WechatWorkNotifier", "DingTalkNotifier", "FeishuNotifier",
		"WxWorkAppNotifier", "WxTplNotifier", "WxPusherNotifier", "QQBotNotifier",
	}
	for _, typeName := range want {
		if got := countType(names, typeName); got != 1 {
			t.Errorf("%s 应装载 1 个，实际 %d 个（全部已装载：%v）", typeName, got, names)
		}
	}
	if len(names) != len(want) {
		t.Errorf("装载渠道总数 %d，期望 %d（全部已装载：%v）", len(names), len(want), names)
	}
}

/*
 * TestBuildNotifyManagerReadsAllNotifyKeys monitor 的 IN 查询必须覆盖 notify.NotifyConfigKeys() 全部键
 * 防止手写清单再次与 notify 分叉：任取一个键写入，useAll 下都应被读到
 * 这里用 qqbot_appid 与 wechat_apptoken 作为哨兵，二者正是本轮修复前读不到的键
 */
func TestBuildNotifyManagerReadsAllNotifyKeys(t *testing.T) {
	setupMonitorTestDB(t)
	seeded := map[string]string{
		"qqbot_appid":     "a",
		"qqbot_appsecret": "s",
		"qqbot_openid":    "o",
		"wechat_apptoken": "AT",
		"wechat_appuid":   "UID",
	}
	seedSysConfig(t, seeded)

	var rows []models.SysConfig
	if err := database.DB.Where("`key` IN ?", notify.NotifyConfigKeys()).Find(&rows).Error; err != nil {
		t.Fatalf("IN 查询失败: %v", err)
	}
	if len(rows) != len(seeded) {
		t.Errorf("IN 查询读到 %d 条，期望 %d 条", len(rows), len(seeded))
	}
	read := make(map[string]bool, len(rows))
	for _, r := range rows {
		read[r.Key] = true
	}
	for k := range seeded {
		if !read[k] {
			t.Errorf("配置键 %s 未被 IN 查询读到（notify.NotifyConfigKeys 与实际读取已分叉）", k)
		}
	}
}

/* noticeToggleCases 每个渠道的：开关键名、启用该渠道所需的最小配置、该渠道的 notifier 类型名 */
var noticeToggleCases = []struct {
	name     string
	key      string
	config   map[string]string
	wantType string
}{
	{
		name: "邮件", key: "notice_mail", wantType: "mailContentAdapter",
		config: map[string]string{"mail_host": "smtp.example.com", "mail_from": "from@example.com"},
	},
	{
		name: "Telegram", key: "notice_tgbot", wantType: "TelegramNotifier",
		config: map[string]string{"tgbot_token": "tk", "tgbot_chatid": "-100123"},
	},
	{
		name: "Webhook", key: "notice_webhook", wantType: "WebhookNotifier",
		config: map[string]string{"webhook_url": "https://example.com/hook"},
	},
	{
		name: "WxPusher", key: "notice_wxtpl", wantType: "WxPusherNotifier",
		config: map[string]string{"wechat_apptoken": "AT", "wechat_appuid": "UID"},
	},
	{
		name: "QQ机器人", key: "notice_qqbot", wantType: "QQBotNotifier",
		config: map[string]string{"qqbot_appid": "a", "qqbot_appsecret": "s", "qqbot_openid": "o"},
	},
}

/* mergeConfig 合并渠道配置与开关配置 */
func mergeConfig(channel, toggle map[string]string) map[string]string {
	out := make(map[string]string, len(channel)+len(toggle))
	for k, v := range channel {
		out[k] = v
	}
	for k, v := range toggle {
		out[k] = v
	}
	return out
}

/*
 * TestNoticeSwitchUnconfiguredMeansEnabled 未配置 notice_* 时必须照常发送
 *
 * 这是本次实现最关键的回归点：若误用 sysconfigBoolExplicitOn（未配置=关）的语义，
 * 六个开关全为关，容灾告警会静默失效
 */
func TestNoticeSwitchUnconfiguredMeansEnabled(t *testing.T) {
	for _, c := range noticeToggleCases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			seedSysConfig(t, c.config)
			// 不写入任何 notice_ 键
			names := notifierTypeNames(buildManager(t, ""))
			if got := countType(names, c.wantType); got != 1 {
				t.Errorf("未配置 %s 时应视为开启并装载 %s，实际 %d 个（渠道清单：%v）", c.key, c.wantType, got, names)
			}
		})
	}
}

/* TestNoticeSwitchExplicitOff 显式写入关闭值时必须不装载 */
func TestNoticeSwitchExplicitOff(t *testing.T) {
	offValues := []string{"0", "false", "no", "off", "FALSE", " Off "}
	for _, c := range noticeToggleCases {
		for _, off := range offValues {
			t.Run(c.name+"/"+off, func(t *testing.T) {
				setupMonitorTestDB(t)
				seedSysConfig(t, mergeConfig(c.config, map[string]string{c.key: off}))
				names := notifierTypeNames(buildManager(t, ""))
				if got := countType(names, c.wantType); got != 0 {
					t.Errorf("%s=%q 时应关闭，实际装载 %d 个 %s（渠道清单：%v）", c.key, off, got, c.wantType, names)
				}
			})
		}
	}
}

/* TestNoticeSwitchExplicitOn 显式写入开启值时照常装载 */
func TestNoticeSwitchExplicitOn(t *testing.T) {
	onValues := []string{"1", "true", "yes", "on"}
	for _, c := range noticeToggleCases {
		for _, on := range onValues {
			t.Run(c.name+"/"+on, func(t *testing.T) {
				setupMonitorTestDB(t)
				seedSysConfig(t, mergeConfig(c.config, map[string]string{c.key: on}))
				names := notifierTypeNames(buildManager(t, ""))
				if got := countType(names, c.wantType); got != 1 {
					t.Errorf("%s=%q 时应开启并装载 1 个 %s，实际 %d 个（渠道清单：%v）", c.key, on, c.wantType, got, names)
				}
			})
		}
	}
}

/*
 * TestNoticeSwitchAndTaskWhitelist 全局开关与任务白名单是 AND 关系
 * 既要 notice_* 开着，也要该渠道在 task.NotifyChannels 内，缺一不发
 */
func TestNoticeSwitchAndTaskWhitelist(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"tgbot_token":  "tk",
		"tgbot_chatid": "-100123",
	})

	cases := []struct {
		name      string
		noticeVal string
		channels  string
		want      int
	}{
		{"开关开 + 白名单含", "1", `["telegram"]`, 1},
		{"开关开 + 白名单不含", "1", `["bark"]`, 0},
		{"开关开 + 白名单空(useAll)", "1", "", 1},
		{"开关关 + 白名单含", "0", `["telegram"]`, 0},
		{"开关关 + 白名单不含", "0", `["bark"]`, 0},
		{"开关关 + 白名单空(useAll)", "0", "", 0},
		{"开关未配置 + 白名单含", "", `["telegram"]`, 1},
		{"开关未配置 + 白名单不含", "", `["bark"]`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toggle := map[string]string{}
			if c.noticeVal != "" {
				toggle["notice_tgbot"] = c.noticeVal
			}
			// 重写配置：先清库再写，避免上一子例残留
			if err := database.DB.Where("`key` IN ?", []string{"notice_tgbot"}).Delete(&models.SysConfig{}).Error; err != nil {
				t.Fatalf("清理 notice_tgbot 失败: %v", err)
			}
			seedSysConfig(t, toggle)
			names := notifierTypeNames(buildManager(t, c.channels))
			if got := countType(names, "TelegramNotifier"); got != c.want {
				t.Errorf("%s: TelegramNotifier=%d, want %d（渠道清单：%v）", c.name, got, c.want, names)
			}
		})
	}
}

/*
 * TestNoticeSwitchAllOffReturnsNil 六个开关全部显式关闭时必须返回 nil
 *
 * 不能返回"非 nil 但装载数为 0"的 manager：那会让 sendNotification 的 nil 兜底不触发，
 * 日志打出「通知已发送」而实际一条都没发 —— 本轮要根治的同一类静默失效
 */
func TestNoticeSwitchAllOffReturnsNil(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"mail_host": "smtp.example.com", "mail_from": "from@example.com",
		"tgbot_token": "tk", "tgbot_chatid": "-100123",
		"webhook_url": "https://example.com/hook",
		"wechat_apptoken": "AT", "wechat_appuid": "UID",
		"qqbot_appid": "a", "qqbot_appsecret": "s", "qqbot_openid": "o",
		"notice_mail": "0", "notice_tgbot": "0", "notice_webhook": "0",
		"notice_wxtpl": "0", "notice_qqbot": "0", "notice_custom_webhook": "0",
	})
	if mgr := buildManager(t, ""); mgr != nil {
		t.Errorf("六开关全关时应返回 nil，实际返回非 nil（装载：%v）", notifierTypeNames(mgr))
	}
}

/*
 * TestNoticeCustomWebhookRegisteredOnly notice_custom_webhook 已登记但当前无渠道可管
 */
func TestNoticeCustomWebhookRegisteredOnly(t *testing.T) {
	found := false
	for _, k := range notify.NotifyConfigKeys() {
		if k == "notice_custom_webhook" {
			found = true
			break
		}
	}
	if !found {
		t.Error("notice_custom_webhook 未登记进 notify.NotifyConfigKeys")
	}
	// 即便显式关闭与显式开启，都不应有任何渠道因它而装载或消失（当前为空判）
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{"webhook_url": "https://example.com/hook"})
	for _, v := range []string{"0", "1"} {
		if err := database.DB.Where("`key` = ?", "notice_custom_webhook").Delete(&models.SysConfig{}).Error; err != nil {
			t.Fatalf("清理失败: %v", err)
		}
		seedSysConfig(t, map[string]string{"notice_custom_webhook": v})
		names := notifierTypeNames(buildManager(t, ""))
		if got := countType(names, "WebhookNotifier"); got != 1 {
			t.Errorf("notice_custom_webhook=%q 不应影响 webhook 渠道，实际装载 %d 个（渠道清单：%v）", v, got, names)
		}
	}
}