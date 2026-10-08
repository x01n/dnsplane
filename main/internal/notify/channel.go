package notify

import "strings"

/*
 * ChannelEnabled 判定某个通知渠道的启用开关是否打开
 *
 * 语义：未配置视为启用，仅显式关闭值才为关闭。
 * 未配置按「关」处理会让所有现存实例的通知在一个升级后静默失效——
 * 这些 *_enabled 键此前从未被后端消费，绝大多数实例的库里根本没有它们，
 * 而渠道凭据（token / webhook / 密码）是齐的，凭据齐备即说明用户本意是启用。
 *
 * 与 monitor 的 noticeEnabled 判定值域一致但各自独立，不互相调用，因为二者语境不同：
 *   - noticeEnabled 判的是容灾告警开关 notice_*，前端默认关闭，用在「要不要发这条告警」这层
 *   - ChannelEnabled 判的是渠道启用开关 *_enabled，前端默认关闭，用在「这个渠道是否装载」这层
 * 混用会让容灾告警的开关语义被渠道开关改写，故保持两份实现、共用同一组关闭字面量
 *
 * @param configMap SysConfig 键值映射
 * @param key       渠道启用开关的配置键，如 mail_enabled
 * @returns 该渠道是否启用
 */
func ChannelEnabled(configMap map[string]string, key string) bool {
	v := strings.TrimSpace(strings.ToLower(configMap[key]))
	return v != "0" && v != "false" && v != "no" && v != "off"
}
