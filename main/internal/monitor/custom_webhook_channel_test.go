package monitor

import (
	"reflect"
	"testing"

	"main/internal/notify"
)

/*
 * 自定义 Webhook 渠道在容灾告警中的装配测试
 *
 * 该渠道此前完全缺失：设置页保存 custom_webhook_* 后无人消费，
 * 装载处的 notice_custom_webhook 是空判。
 * 本测试同时锁定它与通用 Webhook 渠道相互独立 —— 两条链路的开关、白名单与配置键不得互相影响
 */

/* TestBuildNotifyManagerCustomWebhookReachable useAll 路径下自定义 Webhook 必须可达 */
func TestBuildNotifyManagerCustomWebhookReachable(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{"custom_webhook_url": "https://example.com/hook"})

	names := notifierTypeNames(buildManager(t, ""))
	if got := countType(names, "CustomWebhookNotifier"); got != 1 {
		t.Errorf("useAll 下 CustomWebhookNotifier 应装载 1 个，实际 %d 个（渠道清单：%v）", got, names)
	}
	// 通用 Webhook 未配置，不得被自定义渠道的配置带动装载
	if got := countType(names, "WebhookNotifier"); got != 0 {
		t.Errorf("未配置 webhook_url 时 WebhookNotifier 不应装载，实际 %d 个（渠道清单：%v）", got, names)
	}
}

/* TestBuildNotifyManagerCustomWebhookNoticeSwitch notice_custom_webhook 真正生效 */
func TestBuildNotifyManagerCustomWebhookNoticeSwitch(t *testing.T) {
	cases := []struct {
		name      string
		noticeVal string
		want      int
	}{
		{"未配置开关时装载", "", 1},
		{"显式开启时装载", "1", 1},
		{"显式关闭时不装载", "0", 0},
		{"false 不装载", "false", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			kv := map[string]string{"custom_webhook_url": "https://example.com/hook"}
			if c.noticeVal != "" {
				kv["notice_custom_webhook"] = c.noticeVal
			}
			seedSysConfig(t, kv)

			names := notifierTypeNames(buildManager(t, ""))
			if got := countType(names, "CustomWebhookNotifier"); got != c.want {
				t.Errorf("notice_custom_webhook=%q 时 CustomWebhookNotifier=%d，期望 %d（渠道清单：%v）",
					c.noticeVal, got, c.want, names)
			}
		})
	}
}

/* TestBuildNotifyManagerCustomWebhookWhitelist 白名单值即渠道标识，与通用 webhook 互不串门 */
func TestBuildNotifyManagerCustomWebhookWhitelist(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"custom_webhook_url": "https://example.com/custom",
		"webhook_url":        "https://example.com/generic",
	})

	cases := []struct {
		name       string
		channels   string
		wantCustom int
		wantWeb    int
	}{
		{"仅自定义", `["custom_webhook"]`, 1, 0},
		{"仅通用", `["webhook"]`, 0, 1},
		{"两者同时指定", `["custom_webhook","webhook"]`, 1, 1},
		{"白名单不含两者", `["bark"]`, 0, 0},
		{"useAll", "", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			names := notifierTypeNames(buildManager(t, c.channels))
			if got := countType(names, "CustomWebhookNotifier"); got != c.wantCustom {
				t.Errorf("CustomWebhookNotifier=%d，期望 %d（渠道清单：%v）", got, c.wantCustom, names)
			}
			if got := countType(names, "WebhookNotifier"); got != c.wantWeb {
				t.Errorf("WebhookNotifier=%d，期望 %d（渠道清单：%v）", got, c.wantWeb, names)
			}
		})
	}
}

/* TestBuildNotifyManagerCustomWebhookNoticeKeysIndependent 两个开关各管各的渠道 */
func TestBuildNotifyManagerCustomWebhookNoticeKeysIndependent(t *testing.T) {
	cases := []struct {
		name       string
		kv         map[string]string
		wantCustom int
		wantWeb    int
	}{
		{
			"关闭通用开关不影响自定义",
			map[string]string{"notice_webhook": "0"},
			1, 0,
		},
		{
			"关闭自定义开关不影响通用",
			map[string]string{"notice_custom_webhook": "0"},
			0, 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMonitorTestDB(t)
			kv := map[string]string{
				"custom_webhook_url": "https://example.com/custom",
				"webhook_url":        "https://example.com/generic",
			}
			for k, v := range c.kv {
				kv[k] = v
			}
			seedSysConfig(t, kv)

			names := notifierTypeNames(buildManager(t, ""))
			if got := countType(names, "CustomWebhookNotifier"); got != c.wantCustom {
				t.Errorf("CustomWebhookNotifier=%d，期望 %d（渠道清单：%v）", got, c.wantCustom, names)
			}
			if got := countType(names, "WebhookNotifier"); got != c.wantWeb {
				t.Errorf("WebhookNotifier=%d，期望 %d（渠道清单：%v）", got, c.wantWeb, names)
			}
		})
	}
}

/* TestBuildNotifyManagerCustomWebhookFullConfig 五个可选字段必须全部装配，不得只传 URL */
func TestBuildNotifyManagerCustomWebhookFullConfig(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"custom_webhook_url":            "https://example.com/hook",
		"custom_webhook_method":         "PUT",
		"custom_webhook_content_type":   "application/x-www-form-urlencoded",
		"custom_webhook_headers":        "X-Token: abc",
		"custom_webhook_body":           "t={title}&c={content}",
		"custom_webhook_content_format": "text",
	})

	mgr := buildManager(t, "")
	if got := countType(notifierTypeNames(mgr), "CustomWebhookNotifier"); got != 1 {
		t.Fatalf("应装载 1 个 CustomWebhookNotifier，实际 %d 个", got)
	}

	want := map[string]string{
		"URL":           "https://example.com/hook",
		"Method":        "PUT",
		"ContentType":   "application/x-www-form-urlencoded",
		"Headers":       "X-Token: abc",
		"Body":          "t={title}&c={content}",
		"ContentFormat": "text",
	}
	for field, wantVal := range want {
		got, ok := notifierConfigField(mgr, "CustomWebhookNotifier", field)
		if !ok {
			t.Errorf("CustomWebhookConfig.%s 未装配到（字段读不到）", field)
			continue
		}
		if got != wantVal {
			t.Errorf("CustomWebhookConfig.%s = %q, want %q", field, got, wantVal)
		}
	}
}

/*
 * TestBuildNotifyManagerWebhookFullConfig 通用 Webhook 的六个可选字段必须全部装配
 *
 * 回归点：monitor 原先只装配 URL，webhook_user / content_format / method /
 * headers / content_type / body 全部丢失 —— 钉钉地址的 @用户、正文模板在容灾告警里一律不生效，
 * 而同一条 webhook 在证书通知里却正常工作
 */
func TestBuildNotifyManagerWebhookFullConfig(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"webhook_url":            "https://oapi.dingtalk.com/robot/send?access_token=x",
		"webhook_user":           "13800000000",
		"webhook_content_format": "markdown",
		"webhook_method":         "PUT",
		"webhook_headers":        "X-A: 1",
		"webhook_content_type":   "application/json",
		"webhook_body":           `{"k":"{title}"}`,
	})

	mgr := buildManager(t, "")
	if got := countType(notifierTypeNames(mgr), "WebhookNotifier"); got != 1 {
		t.Fatalf("应装载 1 个 WebhookNotifier，实际 %d 个", got)
	}

	want := map[string]string{
		"URL":           "https://oapi.dingtalk.com/robot/send?access_token=x",
		"User":          "13800000000",
		"ContentFormat": "markdown",
		"Method":        "PUT",
		"ContentType":   "application/json",
		"Template":      `{"k":"{title}"}`,
	}
	for field, wantVal := range want {
		got, ok := notifierConfigField(mgr, "WebhookNotifier", field)
		if !ok {
			t.Errorf("WebhookConfig.%s 未装配到（字段读不到）", field)
			continue
		}
		if got != wantVal {
			t.Errorf("WebhookConfig.%s = %q, want %q", field, got, wantVal)
		}
	}

	headers, ok := notifierConfigMapField(mgr, "WebhookNotifier", "Headers")
	if !ok || headers["X-A"] != "1" {
		t.Errorf("WebhookConfig.Headers 未装配: %v", headers)
	}
}

/*
 * TestBuildNotifyManagerChannelTypeMatchesRegistry monitor 装载的渠道标识必须与注册表一致
 * 白名单筛选按渠道标识匹配，标识一旦与注册表脱节，任务指定的渠道就会筛不到
 */
func TestBuildNotifyManagerChannelTypeMatchesRegistry(t *testing.T) {
	setupMonitorTestDB(t)
	seedSysConfig(t, map[string]string{
		"tgbot_token": "tk", "tgbot_chatid": "1",
		"custom_webhook_url": "https://example.com/hook",
		"webhook_url":        "https://example.com/generic",
	})

	byType := map[notify.NotifyType]string{
		notify.NotifyTelegram:      "TelegramNotifier",
		notify.NotifyWebhook:       "WebhookNotifier",
		notify.NotifyCustomWebhook: "CustomWebhookNotifier",
	}
	for typ, typeName := range byType {
		t.Run(string(typ), func(t *testing.T) {
			names := notifierTypeNames(buildManager(t, `["`+string(typ)+`"]`))
			if got := countType(names, typeName); got != 1 {
				t.Errorf("白名单 %s 下应装载 1 个 %s，实际 %d 个（渠道清单：%v）", typ, typeName, got, names)
			}
		})
	}
}

/*
 * notifierConfigField 读取指定类型 notifier 的 config 内字符串字段
 *
 * config 在 notify 包内未导出，monitor 不能反向依赖其内部结构，故沿反射链取值。
 * reflect.Value.Interface() 对未导出字段会 panic，本条链上只用 Kind()/String()，不做 Interface() 转换
 */
func notifierConfigField(m *notify.NotifyManager, typeName, field string) (string, bool) {
	v, ok := notifierConfigValue(m, typeName, field)
	if !ok || v.Kind() != reflect.String {
		return "", false
	}
	return v.String(), true
}

/* notifierConfigMapField 读取指定类型 notifier 的 config 内 map[string]string 字段 */
func notifierConfigMapField(m *notify.NotifyManager, typeName, field string) (map[string]string, bool) {
	v, ok := notifierConfigValue(m, typeName, field)
	if !ok || v.Kind() != reflect.Map {
		return nil, false
	}
	out := make(map[string]string, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		k, val := iter.Key(), iter.Value()
		if k.Kind() == reflect.String && val.Kind() == reflect.String {
			out[k.String()] = val.String()
		}
	}
	return out, true
}

/* notifierConfigValue 定位指定类型 notifier 的 config 字段值 */
func notifierConfigValue(m *notify.NotifyManager, typeName, field string) (reflect.Value, bool) {
	if m == nil {
		return reflect.Value{}, false
	}
	v := reflect.ValueOf(m).Elem().FieldByName("notifiers")
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return reflect.Value{}, false
	}
	for i := 0; i < v.Len(); i++ {
		dyn := v.Index(i).Elem() // 接口的动态值
		if !dyn.IsValid() || dyn.Kind() != reflect.Ptr || dyn.Type().Elem().Name() != typeName {
			continue
		}
		f := dyn.Elem().FieldByName("config").FieldByName(field)
		if f.IsValid() {
			return f, true
		}
	}
	return reflect.Value{}, false
}
