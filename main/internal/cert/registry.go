package cert

import (
	"fmt"
	"sync"
)

/* ProviderFactory 证书提供商工厂函数 */
type ProviderFactory func(config, ext map[string]interface{}) Provider

var (
	providersMu sync.RWMutex
	providers   = make(map[string]ProviderFactory)
	configs     = make(map[string]ProviderConfig)

	// 提供商在 init 中注册完毕后不变，快照只构建一次供管理接口读取
	apiProvidersOnce   sync.Once
	apiProvidersCert   map[string]ProviderConfig
	apiProvidersDeploy map[string]ProviderConfig
)

/* Register 注册证书提供商 */
func Register(name string, factory ProviderFactory, config ProviderConfig) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers[name] = factory
	configs[name] = config
}

/* GetProvider 获取证书提供商实例 */
func GetProvider(name string, config, ext map[string]interface{}) (Provider, error) {
	providersMu.RLock()
	factory, ok := providers[name]
	providersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown cert provider: %s", name)
	}
	if factory == nil {
		return nil, fmt.Errorf("cert provider %s has no implementation (nil factory)", name)
	}
	return factory(config, ext), nil
}

/* GetProviderConfig 获取提供商配置 */
func GetProviderConfig(name string) (ProviderConfig, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	cfg, ok := configs[name]
	return cfg, ok
}

/* GetAllProviderConfigs 获取所有提供商配置 */
func GetAllProviderConfigs() map[string]ProviderConfig {
	providersMu.RLock()
	defer providersMu.RUnlock()
	result := make(map[string]ProviderConfig)
	for k, v := range configs {
		result[k] = v
	}
	return result
}

/* GetCertProviderConfigs 获取证书申请提供商配置 */
func GetCertProviderConfigs() map[string]ProviderConfig {
	providersMu.RLock()
	defer providersMu.RUnlock()
	result := make(map[string]ProviderConfig)
	for k, v := range configs {
		if !v.IsDeploy {
			result[k] = v
		}
	}
	return result
}

/* GetDeployProviderConfig 获取单个部署提供商配置 */
func GetDeployProviderConfig(name string) (*ProviderConfig, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	cfg, ok := configs[name]
	if !ok || !cfg.IsDeploy {
		return nil, false
	}
	return &cfg, true
}

/* GetDeployProviderConfigs 获取证书部署提供商配置 */
func GetDeployProviderConfigs() map[string]ProviderConfig {
	providersMu.RLock()
	defer providersMu.RUnlock()
	result := make(map[string]ProviderConfig)
	for k, v := range configs {
		if v.IsDeploy {
			result[k] = v
		}
	}
	return result
}

// APIProvidersSnapshot 返回进程内固定的 cert/deploy 提供商配置映射（各 map 仅构建一次）
func APIProvidersSnapshot() (cert map[string]ProviderConfig, deploy map[string]ProviderConfig) {
	apiProvidersOnce.Do(func() {
		apiProvidersCert = GetCertProviderConfigs()
		apiProvidersDeploy = GetDeployProviderConfigs()
	})
	return apiProvidersCert, apiProvidersDeploy
}
