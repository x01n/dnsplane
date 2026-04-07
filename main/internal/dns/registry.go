package dns

import (
	"fmt"
	"sync"
)

/* ProviderFactory 服务商工厂函数 */
type ProviderFactory func(config map[string]string, domain, domainID string) Provider

var (
	providersMu sync.RWMutex
	providers   = make(map[string]ProviderFactory)
	configs     = make(map[string]ProviderConfig)
)

/* Register 注册DNS服务商 */
func Register(name string, factory ProviderFactory, config ProviderConfig) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers[name] = factory
	configs[name] = config
}

/* GetProvider 获取DNS服务商实例 */
func GetProvider(name string, config map[string]string, domain, domainID string) (Provider, error) {
	providersMu.RLock()
	factory, ok := providers[name]
	providersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown DNS provider: %s", name)
	}
	if factory == nil {
		return nil, fmt.Errorf("DNS provider %s has no implementation (nil factory)", name)
	}
	return factory(config, domain, domainID), nil
}

/* GetProviderConfig 获取服务商配置 */
func GetProviderConfig(name string) (ProviderConfig, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	cfg, ok := configs[name]
	return cfg, ok
}

/* GetAllProviderConfigs 获取所有服务商配置 */
func GetAllProviderConfigs() map[string]ProviderConfig {
	providersMu.RLock()
	defer providersMu.RUnlock()
	result := make(map[string]ProviderConfig)
	for k, v := range configs {
		result[k] = v
	}
	return result
}

var DefaultLineMapping = map[string]map[string]string{
	"aliyun": {"DEF": "default", "CT": "telecom", "CU": "unicom", "CM": "mobile", "AB": "oversea"},
	// DEF 须为空：CreateRecord 的 RecordLine 为线路名称（如「默认」），传 "0" 会报 RecordLineInvalid
	"dnspod":     {"DEF": "", "CT": "10=0", "CU": "10=1", "CM": "10=3", "AB": "3=0"},
	"huawei":     {"DEF": "default_view", "CT": "Dianxin", "CU": "Liantong", "CM": "Yidong", "AB": "Abroad"},
	"west":       {"DEF": "", "CT": "LTEL", "CU": "LCNC", "CM": "LMOB", "AB": "LFOR"},
	"dnsla":      {"DEF": "", "CT": "84613316902921216", "CU": "84613316923892736", "CM": "84613316953252864", "AB": ""},
	"huoshan":    {"DEF": "default", "CT": "telecom", "CU": "unicom", "CM": "mobile", "AB": "oversea"},
	"baidu":      {"DEF": "default", "CT": "ct", "CU": "cnc", "CM": "cmnet", "AB": ""},
	"jdcloud":    {"DEF": "-1", "CT": "1", "CU": "2", "CM": "3", "AB": "4"},
	"bt":         {"DEF": "0", "CT": "285344768", "CU": "285345792", "CM": "285346816"},
	"cloudflare": {"DEF": "0"},
	"namesilo":   {"DEF": "default"},
	"powerdns":   {"DEF": "default"},
	"spaceship":  {"DEF": "default"},
	"aliyunesa":  {"DEF": "0"},
	"tencenteo":  {"DEF": "Default"},
}

func DefaultDNSLine(providerType string) string {
	if m, ok := DefaultLineMapping[providerType]; ok {
		if v, ok2 := m["DEF"]; ok2 {
			return v
		}
	}
	return "default"
}
