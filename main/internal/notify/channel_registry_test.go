package notify

import (
	"slices"
	"testing"
)

/*
 * 渠道注册表的自动枚举测试
 *
 * 覆盖三件事：
 * 1. 渠道数量与标识唯一性（新增渠道必须注册，重复注册在注册时即 panic）
 * 2. Channels() 顺序稳定（装载次序不得依赖 map 遍历或注册次序）
 * 3. ChannelConfigKeys() 由注册表聚合，不得手工维护 —— 任一渠道的键从聚合结果掉出去，
 *    按 NotifyConfigKeys 做 IN 查询的装载点就读不到它，对应渠道静默失效
 */

/* TestChannelsRegistered 渠道数量与标识唯一 */
func TestChannelsRegistered(t *testing.T) {
	chs := Channels()
	if len(chs) != 13 {
		t.Fatalf("注册渠道数 = %d，期望 13（渠道清单：%v）", len(chs), channelTypes(chs))
	}

	seen := make(map[NotifyType]bool, len(chs))
	for _, c := range chs {
		if c.Type() == "" {
			t.Errorf("渠道 %s 的标识为空", c.Label())
		}
		if seen[c.Type()] {
			t.Errorf("渠道标识重复：%s", c.Type())
		}
		seen[c.Type()] = true
		if c.Label() == "" {
			t.Errorf("渠道 %s 的显示名为空", c.Type())
		}
		if len(c.ConfigKeys()) == 0 {
			t.Errorf("渠道 %s 未声明任何配置键", c.Type())
		}
	}
}

/*
 * TestChannelsOrderStable 顺序必须稳定且按标识排序
 * 装载结果的顺序由本函数决定，不稳定会让测试与前端枚举在不同进程间抖动
 */
func TestChannelsOrderStable(t *testing.T) {
	first := channelTypes(Channels())
	for i := 0; i < 5; i++ {
		if got := channelTypes(Channels()); !slices.Equal(got, first) {
			t.Fatalf("第 %d 次调用顺序不一致：\nfirst=%v\ngot  =%v", i+1, first, got)
		}
	}
	if !slices.IsSorted(first) {
		t.Errorf("渠道未按标识排序：%v", first)
	}
}

/* TestChannelTypesCoverAllNotifiers 现有 12 个渠道之外必须包含新增的 custom_webhook */
func TestChannelTypesCoverAllNotifiers(t *testing.T) {
	want := []NotifyType{
		NotifyEmail, NotifyTelegram, NotifyWebhook, NotifyCustomWebhook, NotifyDiscord,
		NotifyBark, NotifyWechat, NotifyDingTalk, NotifyFeishu, NotifyWxWorkApp,
		NotifyWxTpl, NotifyWxPusher, NotifyQQBot,
	}
	have := make(map[NotifyType]bool)
	for _, c := range Channels() {
		have[c.Type()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("渠道 %s 未注册", w)
		}
	}
}

/*
 * TestChannelConfigKeysAggregated 配置键必须由注册表聚合
 * 逐个渠道比对，任一键缺失即说明该渠道的键未并入聚合结果
 */
func TestChannelConfigKeysAggregated(t *testing.T) {
	agg := make(map[string]bool)
	for _, k := range ChannelConfigKeys() {
		if agg[k] {
			t.Errorf("配置键 %s 在聚合结果中重复", k)
		}
		agg[k] = true
	}

	for _, ch := range Channels() {
		for _, k := range ch.ConfigKeys() {
			if !agg[k] {
				t.Errorf("渠道 %s 的配置键 %s 未聚合进 ChannelConfigKeys", ch.Type(), k)
			}
		}
		if k := ch.EnabledKey(); k != "" && !agg[k] {
			t.Errorf("渠道 %s 的启用开关 %s 未聚合进 ChannelConfigKeys", ch.Type(), k)
		}
		if k := ch.NoticeKey(); k != "" && !agg[k] {
			t.Errorf("渠道 %s 的容灾告警开关 %s 未聚合进 ChannelConfigKeys", ch.Type(), k)
		}
	}

	// 容灾告警开关不属于任何渠道，但必须被登记，否则 monitor 的 IN 查询读不到
	for _, k := range noticeSwitchKeys {
		if !agg[k] {
			t.Errorf("容灾告警开关 %s 未聚合进 ChannelConfigKeys", k)
		}
	}
}

/* TestNotifyConfigKeysDelegatesToRegistry NotifyConfigKeys 必须与聚合结果一致 */
func TestNotifyConfigKeysDelegatesToRegistry(t *testing.T) {
	if !slices.Equal(NotifyConfigKeys(), ChannelConfigKeys()) {
		t.Errorf("NotifyConfigKeys 与 ChannelConfigKeys 不一致\n%v\n%v", NotifyConfigKeys(), ChannelConfigKeys())
	}
}

/*
 * TestNoticeKeyMappingFrozen 容灾告警开关到渠道的映射不得变动
 *
 * 用户已裁决：notice_mail→邮件、notice_tgbot→Telegram、notice_webhook→通用 Webhook、
 * notice_wxtpl→WxPusher、notice_qqbot→QQ 机器人、notice_custom_webhook→自定义 Webhook；
 * 其余渠道不受容灾告警管辖。映射一改，容灾告警会静默多发或不发
 */
func TestNoticeKeyMappingFrozen(t *testing.T) {
	want := map[NotifyType]string{
		NotifyEmail:         "notice_mail",
		NotifyTelegram:      "notice_tgbot",
		NotifyWebhook:       "notice_webhook",
		NotifyWxPusher:      "notice_wxtpl",
		NotifyQQBot:         "notice_qqbot",
		NotifyCustomWebhook: "notice_custom_webhook",
	}
	for _, ch := range Channels() {
		if got, ok := want[ch.Type()]; ok {
			if ch.NoticeKey() != got {
				t.Errorf("渠道 %s 的容灾告警开关 = %q，期望 %q", ch.Type(), ch.NoticeKey(), got)
			}
			continue
		}
		if ch.NoticeKey() != "" {
			t.Errorf("渠道 %s 不应受容灾告警开关管辖，实际声明了 %q", ch.Type(), ch.NoticeKey())
		}
	}
}

/* TestWxTplAndWxPusherAreSeparateChannels 公众号模板消息与 WxPusher 是两个渠道，不得合并 */
func TestWxTplAndWxPusherAreSeparateChannels(t *testing.T) {
	wxtpl, ok := LookupChannel(NotifyWxTpl)
	if !ok {
		t.Fatal("公众号模板消息渠道未注册")
	}
	wxpusher, ok := LookupChannel(NotifyWxPusher)
	if !ok {
		t.Fatal("WxPusher 渠道未注册")
	}
	if wxtpl.NoticeKey() != "" {
		t.Errorf("公众号模板消息不应受容灾告警开关管辖，实际 %q", wxtpl.NoticeKey())
	}
	if wxpusher.NoticeKey() != "notice_wxtpl" {
		t.Errorf("WxPusher 的容灾告警开关 = %q，期望 notice_wxtpl", wxpusher.NoticeKey())
	}
	for _, k := range wxtpl.ConfigKeys() {
		for _, p := range wxpusher.ConfigKeys() {
			if k == p {
				t.Errorf("两个渠道共用配置键 %s，说明未按渠道拆分", k)
			}
		}
	}
}

/* TestRegisterChannelRejectsDuplicate 重复注册必须 panic，避免两个同名渠道静默取其一 */
func TestRegisterChannelRejectsDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("重复注册同一渠道标识应 panic")
		}
	}()
	RegisterChannel(telegramChannel{})
}

/* channelTypes 取出渠道标识列表 */
func channelTypes(chs []Channel) []string {
	out := make([]string, 0, len(chs))
	for _, c := range chs {
		out = append(out, string(c.Type()))
	}
	return out
}
