package notify

import (
	"strconv"
	"strings"
)

/*
 * 渠道适配器定义与注册
 *
 * 每个渠道在此声明自己的配置键、启用开关与构造方式，装载点只遍历注册表。
 * Build 必须装配该渠道配置结构的全部字段：monitor 与 dashboard 的测试端点此前
 * 各自只传部分字段（Webhook 只传 URL、Telegram 只传两项），同一份配置在三条装载路径上行为不一致
 */

/* mailChannel 邮件渠道适配器：mail_type 三路分发邮件后端，见 buildMailNotifier */
type mailChannel struct{}

func (mailChannel) Type() NotifyType { return NotifyEmail }
func (mailChannel) Label() string    { return "邮件" }
func (mailChannel) EnabledKey() string {
	return "mail_enabled"
}
func (mailChannel) NoticeKey() string { return "notice_mail" }
func (mailChannel) ConfigKeys() []string {
	return []string{
		"mail_host", "mail_port", "mail_user", "mail_password", "mail_from", "mail_recv",
		"mail_secure", "mail_tls", "mail_type", "mail_api_user", "mail_api_key", "mail_from_name",
		"mail_auth",
	}
}
func (mailChannel) Build(configMap map[string]string) (Notifier, bool) {
	return buildMailNotifier(configMap)
}

/* telegramChannel Telegram Bot 渠道适配器 */
type telegramChannel struct{}

func (telegramChannel) Type() NotifyType { return NotifyTelegram }
func (telegramChannel) Label() string    { return "Telegram" }
func (telegramChannel) EnabledKey() string {
	return "tgbot_enabled"
}
func (telegramChannel) NoticeKey() string { return "notice_tgbot" }
func (telegramChannel) ConfigKeys() []string {
	return []string{"tgbot_token", "tgbot_chatid", "tgbot_topicid", "tgbot_url", "tgbot_proxy"}
}
func (telegramChannel) Build(configMap map[string]string) (Notifier, bool) {
	if !ChannelEnabled(configMap, "tgbot_enabled") ||
		configMap["tgbot_token"] == "" || configMap["tgbot_chatid"] == "" {
		return nil, false
	}
	return NewTelegramNotifier(TelegramConfig{
		BotToken:  configMap["tgbot_token"],
		ChatID:    configMap["tgbot_chatid"],
		TopicID:   configMap["tgbot_topicid"],
		ServerURL: configMap["tgbot_url"],
		ProxyMode: configMap["tgbot_proxy"],
	}), true
}

/* webhookChannel 通用 Webhook 渠道适配器 */
type webhookChannel struct{}

func (webhookChannel) Type() NotifyType { return NotifyWebhook }
func (webhookChannel) Label() string    { return "Webhook" }
func (webhookChannel) EnabledKey() string {
	return "webhook_enabled"
}
func (webhookChannel) NoticeKey() string { return "notice_webhook" }
func (webhookChannel) ConfigKeys() []string {
	return []string{
		"webhook_url", "webhook_method", "webhook_content_type",
		"webhook_headers", "webhook_body", "webhook_content_format", "webhook_user",
	}
}
func (webhookChannel) Build(configMap map[string]string) (Notifier, bool) {
	if !ChannelEnabled(configMap, "webhook_enabled") || configMap["webhook_url"] == "" {
		return nil, false
	}
	return NewWebhookNotifier(WebhookConfig{
		URL:           configMap["webhook_url"],
		User:          configMap["webhook_user"],
		ContentFormat: configMap["webhook_content_format"],
		Method:        configMap["webhook_method"],
		Headers:       parseWebhookHeaders(configMap["webhook_headers"]),
		ContentType:   configMap["webhook_content_type"],
		Template:      configMap["webhook_body"],
	}), true
}

/* customWebhookChannel 自定义 Webhook 渠道适配器 */
type customWebhookChannel struct{}

func (customWebhookChannel) Type() NotifyType { return NotifyCustomWebhook }
func (customWebhookChannel) Label() string    { return "自定义Webhook" }
func (customWebhookChannel) EnabledKey() string {
	return "custom_webhook_enabled"
}
func (customWebhookChannel) NoticeKey() string { return "notice_custom_webhook" }
func (customWebhookChannel) ConfigKeys() []string {
	return []string{
		"custom_webhook_url", "custom_webhook_method", "custom_webhook_content_type",
		"custom_webhook_headers", "custom_webhook_body", "custom_webhook_content_format",
	}
}
func (customWebhookChannel) Build(configMap map[string]string) (Notifier, bool) {
	url := configMap["custom_webhook_url"]
	if !ChannelEnabled(configMap, "custom_webhook_enabled") || url == "" {
		return nil, false
	}
	if err := ValidateOutboundURL(url); err != nil {
		return nil, false
	}
	return NewCustomWebhookNotifier(CustomWebhookConfig{
		URL:           url,
		Method:        configMap["custom_webhook_method"],
		Headers:       configMap["custom_webhook_headers"],
		Body:          configMap["custom_webhook_body"],
		ContentType:   configMap["custom_webhook_content_type"],
		ContentFormat: configMap["custom_webhook_content_format"],
	}), true
}

/* discordChannel Discord Webhook 渠道适配器 */
type discordChannel struct{}

func (discordChannel) Type() NotifyType { return NotifyDiscord }
func (discordChannel) Label() string    { return "Discord" }
func (discordChannel) EnabledKey() string {
	return "discord_enabled"
}
func (discordChannel) NoticeKey() string    { return "" }
func (discordChannel) ConfigKeys() []string { return []string{"discord_webhook"} }
func (discordChannel) Build(configMap map[string]string) (Notifier, bool) {
	if !ChannelEnabled(configMap, "discord_enabled") || configMap["discord_webhook"] == "" {
		return nil, false
	}
	return NewDiscordNotifier(DiscordConfig{WebhookURL: configMap["discord_webhook"]}), true
}

/* barkChannel Bark 推送渠道适配器 */
type barkChannel struct{}

func (barkChannel) Type() NotifyType { return NotifyBark }
func (barkChannel) Label() string    { return "Bark" }
func (barkChannel) EnabledKey() string {
	return "bark_enabled"
}
func (barkChannel) NoticeKey() string { return "" }
func (barkChannel) ConfigKeys() []string {
	return []string{"bark_url", "bark_server", "bark_key"}
}

/*
 * Build 构造 Bark 通知器
 * 设置页写入的键是 bark_server，bark_url 是早期键名，保留兜底以兼容手写配置
 */
func (barkChannel) Build(configMap map[string]string) (Notifier, bool) {
	barkServer := configMap["bark_url"]
	if barkServer == "" {
		barkServer = configMap["bark_server"]
	}
	if !ChannelEnabled(configMap, "bark_enabled") || barkServer == "" || configMap["bark_key"] == "" {
		return nil, false
	}
	return NewBarkNotifier(BarkConfig{ServerURL: barkServer, DeviceKey: configMap["bark_key"]}), true
}

/* wechatWorkChannel 企业微信群机器人渠道适配器 */
type wechatWorkChannel struct{}

func (wechatWorkChannel) Type() NotifyType { return NotifyWechat }
func (wechatWorkChannel) Label() string    { return "企业微信群机器人" }
func (wechatWorkChannel) EnabledKey() string {
	return "wechat_enabled"
}
func (wechatWorkChannel) NoticeKey() string    { return "" }
func (wechatWorkChannel) ConfigKeys() []string { return []string{"wechat_webhook"} }
func (wechatWorkChannel) Build(configMap map[string]string) (Notifier, bool) {
	if !ChannelEnabled(configMap, "wechat_enabled") || configMap["wechat_webhook"] == "" {
		return nil, false
	}
	return NewWechatWorkNotifier(WechatWorkConfig{WebhookURL: configMap["wechat_webhook"]}), true
}

/* dingTalkChannel 钉钉群机器人渠道适配器 */
type dingTalkChannel struct{}

func (dingTalkChannel) Type() NotifyType { return NotifyDingTalk }
func (dingTalkChannel) Label() string    { return "钉钉" }
func (dingTalkChannel) EnabledKey() string {
	return "dingtalk_enabled"
}
func (dingTalkChannel) NoticeKey() string    { return "" }
func (dingTalkChannel) ConfigKeys() []string { return []string{"dingtalk_webhook", "dingtalk_secret"} }
func (dingTalkChannel) Build(configMap map[string]string) (Notifier, bool) {
	if !ChannelEnabled(configMap, "dingtalk_enabled") || configMap["dingtalk_webhook"] == "" {
		return nil, false
	}
	return NewDingTalkNotifier(DingTalkConfig{
		WebhookURL: configMap["dingtalk_webhook"],
		Secret:     configMap["dingtalk_secret"],
	}), true
}

/* feishuChannel 飞书群机器人渠道适配器 */
type feishuChannel struct{}

func (feishuChannel) Type() NotifyType { return NotifyFeishu }
func (feishuChannel) Label() string    { return "飞书" }
func (feishuChannel) EnabledKey() string {
	return "feishu_enabled"
}
func (feishuChannel) NoticeKey() string    { return "" }
func (feishuChannel) ConfigKeys() []string { return []string{"feishu_webhook", "feishu_secret"} }
func (feishuChannel) Build(configMap map[string]string) (Notifier, bool) {
	if !ChannelEnabled(configMap, "feishu_enabled") || configMap["feishu_webhook"] == "" {
		return nil, false
	}
	return NewFeishuNotifier(FeishuConfig{
		WebhookURL: configMap["feishu_webhook"],
		Secret:     configMap["feishu_secret"],
	}), true
}

/* wxWorkAppChannel 企业微信应用消息渠道适配器 */
type wxWorkAppChannel struct{}

func (wxWorkAppChannel) Type() NotifyType { return NotifyWxWorkApp }
func (wxWorkAppChannel) Label() string    { return "企业微信应用" }
func (wxWorkAppChannel) EnabledKey() string {
	return "wxwork_app_enabled"
}
func (wxWorkAppChannel) NoticeKey() string { return "" }
func (wxWorkAppChannel) ConfigKeys() []string {
	return []string{"wxwork_corpid", "wxwork_agentid", "wxwork_secret", "wxwork_touser"}
}
func (wxWorkAppChannel) Build(configMap map[string]string) (Notifier, bool) {
	if !ChannelEnabled(configMap, "wxwork_app_enabled") ||
		configMap["wxwork_corpid"] == "" || configMap["wxwork_agentid"] == "" || configMap["wxwork_secret"] == "" {
		return nil, false
	}
	return NewWxWorkAppNotifier(WxWorkAppConfig{
		CorpID:  configMap["wxwork_corpid"],
		AgentID: configMap["wxwork_agentid"],
		Secret:  configMap["wxwork_secret"],
		ToUser:  configMap["wxwork_touser"],
	}), true
}

/* wxTplChannel 微信公众号模板消息渠道适配器 */
type wxTplChannel struct{}

func (wxTplChannel) Type() NotifyType { return NotifyWxTpl }
func (wxTplChannel) Label() string    { return "微信公众号模板消息" }
func (wxTplChannel) EnabledKey() string {
	return "wxtpl_enabled"
}
func (wxTplChannel) NoticeKey() string { return "" }
func (wxTplChannel) ConfigKeys() []string {
	return []string{"wxtpl_appid", "wxtpl_appsecret", "wxtpl_template_id", "wxtpl_users", "wxtpl_url"}
}
func (wxTplChannel) Build(configMap map[string]string) (Notifier, bool) {
	if !ChannelEnabled(configMap, "wxtpl_enabled") ||
		configMap["wxtpl_appid"] == "" || configMap["wxtpl_appsecret"] == "" ||
		configMap["wxtpl_template_id"] == "" || configMap["wxtpl_users"] == "" {
		return nil, false
	}
	return NewWxTplNotifier(WxTplConfig{
		AppID:      configMap["wxtpl_appid"],
		AppSecret:  configMap["wxtpl_appsecret"],
		TemplateID: configMap["wxtpl_template_id"],
		ToUsers:    configMap["wxtpl_users"],
		URL:        configMap["wxtpl_url"],
	}), true
}

/* wxPusherChannel WxPusher 渠道适配器 */
type wxPusherChannel struct{}

func (wxPusherChannel) Type() NotifyType { return NotifyWxPusher }
func (wxPusherChannel) Label() string    { return "WxPusher" }
func (wxPusherChannel) EnabledKey() string {
	return ""
}

/*
 * NoticeKey WxPusher 受 notice_wxtpl 管辖
 * 该键名沿用设置页既有字段，其含义在本轮裁决中明确为「WxPusher 的容灾告警开关」，
 * 与微信公众号模板消息渠道（NotifyWxTpl）无关，后者不受容灾告警管辖
 */
func (wxPusherChannel) NoticeKey() string { return "notice_wxtpl" }
func (wxPusherChannel) ConfigKeys() []string {
	return []string{"wechat_apptoken", "wechat_appuid"}
}
func (wxPusherChannel) Build(configMap map[string]string) (Notifier, bool) {
	if configMap["wechat_apptoken"] == "" || configMap["wechat_appuid"] == "" {
		return nil, false
	}
	return NewWxPusherNotifier(WxPusherConfig{
		AppToken: configMap["wechat_apptoken"],
		UID:      configMap["wechat_appuid"],
	}), true
}

/* qqBotChannel QQ 机器人渠道适配器 */
type qqBotChannel struct{}

func (qqBotChannel) Type() NotifyType { return NotifyQQBot }
func (qqBotChannel) Label() string    { return "QQ机器人" }
func (qqBotChannel) EnabledKey() string {
	return ""
}
func (qqBotChannel) NoticeKey() string { return "notice_qqbot" }
func (qqBotChannel) ConfigKeys() []string {
	return []string{"qqbot_appid", "qqbot_appsecret", "qqbot_openid"}
}
func (qqBotChannel) Build(configMap map[string]string) (Notifier, bool) {
	if configMap["qqbot_appid"] == "" || configMap["qqbot_appsecret"] == "" || configMap["qqbot_openid"] == "" {
		return nil, false
	}
	return NewQQBotNotifier(QQBotConfig{
		AppID:     configMap["qqbot_appid"],
		AppSecret: configMap["qqbot_appsecret"],
		OpenID:    configMap["qqbot_openid"],
	}), true
}

func init() {
	RegisterChannel(mailChannel{})
	RegisterChannel(telegramChannel{})
	RegisterChannel(webhookChannel{})
	RegisterChannel(customWebhookChannel{})
	RegisterChannel(discordChannel{})
	RegisterChannel(barkChannel{})
	RegisterChannel(wechatWorkChannel{})
	RegisterChannel(dingTalkChannel{})
	RegisterChannel(feishuChannel{})
	RegisterChannel(wxWorkAppChannel{})
	RegisterChannel(wxTplChannel{})
	RegisterChannel(wxPusherChannel{})
	RegisterChannel(qqBotChannel{})
}

/*
 * buildMailNotifier 构造邮件通知器，覆盖 mail_recv→mail_from 的收件人回退与 mail_type 的三路后端分发
 * （0 或空为 SMTP、1 为 Sendcloud、2 为阿里云邮件推送）
 *
 * 收件人回退与三路分发是本渠道内部的分支，不拆成三个独立渠道：
 * 用户在设置页选的是同一个「邮件」渠道，三个后端只是投递通道的差异，
 * 拆开会让前端选单与任务白名单出现三个互斥选项，与实际配置语义不符
 *
 * 三路分发的守卫必须用回退后的 mailTo 而非 mail_recv：
 * 仅配发件人时应由回退值兜当收件人，用 mail_recv 会让回退不可达而静默失效
 *
 * SMTP 加密与鉴权取值见 MailSecureFromConfig / MailAuthFromConfig：
 * 此前 SMTP 分支只读 mail_secure，而设置页写入的 mail_secure 允许 none/tls，
 * 读取侧只认 ssl 会让这两档静默退化成明文投递；鉴权方式更是从未被装配，恒为 PLAIN
 *
 * @param configMap 系统配置键值
 * @returns 经 mailContentAdapter 包装的通知器与是否装载成功
 */
func buildMailNotifier(configMap map[string]string) (Notifier, bool) {
	mailTo := configMap["mail_recv"]
	if mailTo == "" {
		mailTo = configMap["mail_from"]
	}
	// 邮件渠道启用开关：未配置视为启用（零倒退），仅显式关闭时拒载
	if !ChannelEnabled(configMap, "mail_enabled") {
		return nil, false
	}
	switch configMap["mail_type"] {
	case "1":
		if mailTo != "" && configMap["mail_api_user"] != "" && configMap["mail_api_key"] != "" && configMap["mail_from"] != "" {
			return &mailContentAdapter{inner: NewSendcloudNotifier(SendcloudConfig{
				APIUser: configMap["mail_api_user"], APIKey: configMap["mail_api_key"],
				From: configMap["mail_from"], FromName: configMap["mail_from_name"], To: mailTo,
			})}, true
		}
	case "2":
		if mailTo != "" && configMap["mail_api_user"] != "" && configMap["mail_api_key"] != "" && configMap["mail_from"] != "" {
			return &mailContentAdapter{inner: NewAliyunDirectMailNotifier(AliyunDirectMailConfig{
				AccessKeyID: configMap["mail_api_user"], AccessKeySecret: configMap["mail_api_key"],
				From: configMap["mail_from"], FromName: configMap["mail_from_name"], To: mailTo,
			})}, true
		}
	default:
		if configMap["mail_host"] != "" && mailTo != "" {
			return &mailContentAdapter{inner: NewEmailNotifier(EmailConfig{
				Host: configMap["mail_host"], Port: mailPortOrDefault(configMap["mail_port"]),
				Username: configMap["mail_user"], Password: configMap["mail_password"],
				From: configMap["mail_from"], To: mailTo,
				Secure:   MailSecureFromConfig(configMap),
				AuthType: MailAuthFromConfig(configMap),
			})}, true
		}
	}
	return nil, false
}

/*
 * MailSecureFromConfig 解析 SMTP 加密方式，返回值域与 EmailConfig.Secure 一致（none/ssl/tls）
 *
 * 两个键并存是历史原因：mail_secure 是设置页写入的键，值域 none/ssl/tls；
 * mail_tls 是早期键，只有 "1"/"true" 两种真值，语义等同于 SSL。
 * 两者互不覆盖：mail_secure 显式给出 none 时不能被 mail_tls 改写成加密，
 * 否则用户选择的「无加密」会被历史键静默推翻
 *
 * 导出供 handler 的邮件接口复用：该侧此前只读 mail_tls 且把真值映射成 UseTLS（仅 SSL 分支生效），
 * 既取不到设置页的选择，也无法表达 STARTTLS
 *
 * @param configMap 系统配置键值
 * @returns none / ssl / tls，未配置返回 none（与无加密选项一致）
 */
func MailSecureFromConfig(configMap map[string]string) string {
	switch strings.ToLower(strings.TrimSpace(configMap["mail_secure"])) {
	case "ssl":
		return "ssl"
	case "tls":
		return "tls"
	case "none":
		return "none"
	}
	if v := strings.ToLower(strings.TrimSpace(configMap["mail_tls"])); v == "1" || v == "true" {
		return "ssl"
	}
	return "none"
}

/*
 * MailAuthFromConfig 解析 SMTP 鉴权方式，返回值域与 EmailConfig.AuthType 一致（plain/login/crammd5）
 *
 * 导出供 handler 的邮件接口复用：设置页的「验证方式」写入 mail_auth，
 * 而该键在此前从未被任何装配点读取，AuthType 恒为默认的 PLAIN ——
 * 仅支持 LOGIN 或 CRAM-MD5 的服务端会静默认证失败。
 * 两个装配点共用本函数，取值口径不可能再分叉
 *
 * @param configMap 系统配置键值
 * @returns plain / login / crammd5，未配置或未识别返回 plain
 */
func MailAuthFromConfig(configMap map[string]string) string {
	switch strings.ToLower(strings.TrimSpace(configMap["mail_auth"])) {
	case "login":
		return "login"
	case "crammd5":
		return "crammd5"
	default:
		return "plain"
	}
}

/* mailPortOrDefault 解析邮件端口，未配置或非法时回退 25 */
func mailPortOrDefault(raw string) int {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port <= 0 {
		return 25
	}
	return port
}
