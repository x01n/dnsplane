package base

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"main/internal/cert"
)

/* snakeToCamel 将下划线命名转为驼峰命名: access_key_id → AccessKeyId */
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := range parts {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

/* camelToSnake 将驼峰命名转为下划线命名: AccessKeyId → access_key_id */
func camelToSnake(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

/* Logger 日志记录器类型 */
type Logger = cert.Logger

/* DeployProvider 证书部署接口 */
type DeployProvider interface {
	// Check 检查配置
	Check(ctx context.Context) error

	// Deploy 部署证书
	Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error

	// SetLogger 设置日志记录器
	SetLogger(logger Logger)
}

/*
 * DeployInfo 部署器回传的附加信息
 * Config 内的键会合并回部署任务配置，其余键序列化后存入任务 info 字段
 */
type DeployInfo struct {
	Config map[string]interface{}
	Data   map[string]interface{}
}

/*
 * DeployWithInfoProvider 可回传附加信息的部署器接口
 * 单独拆分而不加入 DeployProvider，是为了让现有部署器无需改动即可满足 DeployProvider
 */
type DeployWithInfoProvider interface {
	DeployProvider

	// DeployWithInfo 部署证书并回传附加信息
	DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *DeployInfo) error
}

/* ProviderFactory 部署器工厂函数类型 */
type ProviderFactory func(config map[string]interface{}) DeployProvider

var (
	providers = make(map[string]ProviderFactory)
	mu        sync.RWMutex
)

/* Register 注册部署器 */
func Register(name string, factory ProviderFactory) {
	mu.Lock()
	defer mu.Unlock()
	providers[name] = factory
}

/* GetProvider 获取部署器实例 */
func GetProvider(name string, config map[string]interface{}) (DeployProvider, error) {
	mu.RLock()
	factory, ok := providers[name]
	mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown deploy provider: %s", name)
	}
	if factory == nil {
		return nil, fmt.Errorf("deploy provider %s has no implementation (nil factory)", name)
	}

	return factory(config), nil
}

/* ListProviders 列出所有已注册的部署器 */
func ListProviders() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	return names
}

/* BaseProvider 部署器基类 */
type BaseProvider struct {
	Config map[string]interface{}
	Logger Logger
}

/* SetLogger 设置日志记录器 */
func (p *BaseProvider) SetLogger(logger Logger) {
	p.Logger = logger
}

/* Log 记录日志 */
func (p *BaseProvider) Log(msg string) {
	if p.Logger != nil {
		p.Logger(msg)
	}
}

/*
 * ProviderDeployWithInfo 执行部署并收集附加信息
 * BaseProvider 不提供 DeployWithInfo，否则嵌入它的部署器会因方法提升而误判为
 * 支持回传，导致实际部署被默认实现吞掉；未实现该接口时退化为普通部署
 */
func ProviderDeployWithInfo(ctx context.Context, provider DeployProvider, fullchain, privateKey string, config map[string]interface{}, info *DeployInfo) error {
	if info == nil {
		info = &DeployInfo{}
	}
	if withInfo, ok := provider.(DeployWithInfoProvider); ok {
		return withInfo.DeployWithInfo(ctx, fullchain, privateKey, config, info)
	}
	return provider.Deploy(ctx, fullchain, privateKey, config)
}

/*
 * InfoUpdates 把回传信息转换为 cert_deploy 表的更新字段
 * 返回 nil 表示本次没有需要落库的回传内容
 */
func InfoUpdates(rawConfig, rawInfo string, info *DeployInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	updates := make(map[string]interface{})
	if len(info.Config) > 0 {
		merged := decodeJSONObject(rawConfig)
		for k, v := range info.Config {
			merged[k] = v
		}
		if configJSON, err := json.Marshal(merged); err == nil {
			updates["config"] = string(configJSON)
		}
	}
	if len(info.Data) > 0 {
		merged := decodeJSONObject(rawInfo)
		for k, v := range info.Data {
			merged[k] = v
		}
		if infoJSON, err := json.Marshal(merged); err == nil {
			updates["info"] = string(infoJSON)
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return updates
}

/* decodeJSONObject 解析 JSON 对象字符串，空串或非对象内容返回空 map */
func decodeJSONObject(raw string) map[string]interface{} {
	merged := map[string]interface{}{}
	if raw == "" {
		return merged
	}
	_ = json.Unmarshal([]byte(raw), &merged)
	if merged == nil {
		merged = map[string]interface{}{}
	}
	return merged
}

/* GetString 从配置中获取字符串值（支持大小写不敏感 + 下划线/驼峰互转） */
func (p *BaseProvider) GetString(key string) string {
	// 1. 精确匹配
	if v, ok := p.Config[key].(string); ok {
		return v
	}
	// 2. 大小写不敏感匹配
	keyLower := strings.ToLower(key)
	for k, v := range p.Config {
		if strings.ToLower(k) == keyLower {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	// 3. 下划线转驼峰匹配: access_key_id → AccessKeyId
	camelKey := snakeToCamel(key)
	if camelKey != key {
		if v, ok := p.Config[camelKey].(string); ok {
			return v
		}
	}
	// 4. 驼峰转下划线匹配: AccessKeyId → access_key_id
	snakeKey := camelToSnake(key)
	if snakeKey != key {
		if v, ok := p.Config[snakeKey].(string); ok {
			return v
		}
	}
	return ""
}

/* GetInt 从配置中获取整数值 */
func (p *BaseProvider) GetInt(key string, defaultVal int) int {
	if v, ok := p.Config[key].(float64); ok {
		return int(v)
	}
	if v, ok := p.Config[key].(int); ok {
		return v
	}
	return defaultVal
}

/* Check 默认检查实现 */
func (p *BaseProvider) Check(ctx context.Context) error {
	return nil
}

/* GetStringFrom 从指定配置中获取字符串值，不存在则回退到默认配置 */
func (p *BaseProvider) GetStringFrom(config map[string]interface{}, key string) string {
	if config != nil {
		if v, ok := config[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return p.GetString(key)
}

/* GetConfigString 从配置中获取字符串值 */
func GetConfigString(config map[string]interface{}, key string) string {
	if config == nil {
		return ""
	}
	if v, ok := config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

/* GetConfigBool 从配置中获取布尔值 */
func GetConfigBool(config map[string]interface{}, key string) bool {
	if config == nil {
		return false
	}
	if v, ok := config[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
		if s, ok := v.(string); ok {
			return s == "1" || strings.EqualFold(s, "true")
		}
	}
	return false
}

/* SplitDomains 分割域名字符串 */
func SplitDomains(input string) []string {
	if input == "" {
		return nil
	}
	replacer := strings.NewReplacer("\r\n", "\n", "\r", "\n", ",", "\n")
	normalized := replacer.Replace(input)
	parts := strings.Split(normalized, "\n")
	var domains []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			domains = append(domains, p)
		}
	}
	return domains
}

/* GetConfigDomains 从配置中获取域名列表 */
func GetConfigDomains(config map[string]interface{}) []string {
	if config == nil {
		return nil
	}
	if v, ok := config["domainList"]; ok {
		switch d := v.(type) {
		case []string:
			return d
		case []interface{}:
			var result []string
			for _, item := range d {
				if s, ok := item.(string); ok && s != "" {
					result = append(result, s)
				}
			}
			if len(result) > 0 {
				return result
			}
		case string:
			return SplitDomains(d)
		}
	}
	if v, ok := config["domains"]; ok {
		if s, ok := v.(string); ok {
			return SplitDomains(s)
		}
	}
	if v, ok := config["domain"]; ok {
		if s, ok := v.(string); ok {
			return SplitDomains(s)
		}
	}
	return nil
}
