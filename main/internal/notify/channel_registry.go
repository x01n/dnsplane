package notify

import (
	"slices"
	"strings"
	"sync"
)

/*
 * Channel 通知渠道适配器
 *
 * 把「渠道标识 / 显示名 / 配置键 / 启用开关 / 容灾告警开关 / 构造方式」聚合到一处，
 * 使装载方遍历注册表而不是逐个硬编码 if 块：
 * 新增渠道只需实现本接口并注册，任何装载函数都不必改动
 *
 * 装载点共有三处，此前各自维护一份渠道清单且已发生分叉
 * （monitor 的 Webhook 只传 URL、dashboard 的测试端点只传两三个字段）：
 * 三处统一走注册表后，某渠道构造缺字段这类缺陷不可能只在一处出现
 */
type Channel interface {
	/* Type 渠道唯一标识，任务通知白名单与前端选单以此为准 */
	Type() NotifyType
	/* Label 渠道显示名，供错误提示与 API 枚举使用 */
	Label() string
	/* ConfigKeys 该渠道依赖的全部业务配置键，不含启用开关与容灾告警开关 */
	ConfigKeys() []string
	/* EnabledKey 渠道启用开关的配置键，无独立开关时返回空串 */
	EnabledKey() string
	/* NoticeKey 渠道的容灾告警开关配置键，不受容灾告警管辖时返回空串 */
	NoticeKey() string
	/* Build 从配置构造通知器；配置不完整或渠道被显式关闭时返回 ok=false */
	Build(configMap map[string]string) (Notifier, bool)
}

var (
	registryMu         sync.RWMutex
	registeredChannels []Channel
	registeredTypes    = make(map[NotifyType]bool)
)

/*
 * noticeSwitchKeys 容灾告警专属开关
 *
 * 不属于任何渠道：同一渠道在证书、到期、容灾三条链路上各有各的开关，
 * 本组键只由 internal/monitor 消费，登记在此仅为让按 NotifyConfigKeys 做的 IN 查询读到它们
 */
var noticeSwitchKeys = []string{
	"notice_mail", "notice_wxtpl", "notice_tgbot", "notice_qqbot", "notice_webhook", "notice_custom_webhook",
}

/*
 * RegisterChannel 注册一个渠道适配器
 * 重复注册同一标识时 panic：这是编程错误，启动阶段失败比运行期静默取其一更可查
 * @param c 渠道适配器
 */
func RegisterChannel(c Channel) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if c == nil {
		panic("notify: 注册了 nil 渠道")
	}
	if registeredTypes[c.Type()] {
		panic("notify: 渠道标识重复注册: " + string(c.Type()))
	}
	registeredTypes[c.Type()] = true
	registeredChannels = append(registeredChannels, c)
}

/*
 * Channels 返回全部已注册渠道，按渠道标识排序
 *
 * 排序而非按注册顺序暴露：装载结果与注册次序解耦，
 * 各装载点与测试据此得到确定顺序，注册表内部调整不会改变对外可见的装载次序
 * @returns 渠道列表副本
 */
func Channels() []Channel {
	registryMu.RLock()
	out := make([]Channel, len(registeredChannels))
	copy(out, registeredChannels)
	registryMu.RUnlock()
	slices.SortStableFunc(out, func(a, b Channel) int {
		return strings.Compare(string(a.Type()), string(b.Type()))
	})
	return out
}

/*
 * LookupChannel 按标识查找渠道
 * @param t 渠道标识
 * @returns 渠道适配器与是否存在
 */
func LookupChannel(t NotifyType) (Channel, bool) {
	for _, c := range Channels() {
		if c.Type() == t {
			return c, true
		}
	}
	return nil, false
}

/*
 * ChannelConfigKeys 聚合全部渠道的配置键
 *
 * 由注册表推导而非手工维护：键清单与渠道定义分家正是本轮修复的分叉类型，
 * 新增渠道时键自动并入，各装载点的按需读取随之生效
 * @returns 配置键列表，按渠道标识排序且无重复
 */
func ChannelConfigKeys() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 64)
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	for _, c := range Channels() {
		for _, k := range c.ConfigKeys() {
			add(k)
		}
		add(c.EnabledKey())
		add(c.NoticeKey())
	}
	for _, k := range noticeSwitchKeys {
		add(k)
	}
	return out
}
