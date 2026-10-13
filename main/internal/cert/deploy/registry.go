package deploy

import (
	"fmt"
	"main/internal/cert/deploy/base"
)

/* 类型别名 - 保持向后兼容 */
type DeployProvider = base.DeployProvider
type ProviderFactory = base.ProviderFactory
type BaseProvider = base.BaseProvider
type Logger = base.Logger

/* 函数包装 - 保持向后兼容 */
var (
	Register      = base.Register
	ListProviders = base.ListProviders
)

/*
 * GetProvider 获取部署器实例
 * @param accountType 账户类型（如 "tencent", "aliyun", "doge"）
 * @param accConfig   账户配置
 * @param taskConfig  可选部署任务配置（含 product 字段用于确定子产品）
 * 解析逻辑：tencent + product=cdn → tencent_cdn
 */
func GetProvider(accountType string, accConfig map[string]interface{}, taskConfig ...map[string]interface{}) (DeployProvider, error) {
	// 1. 先尝试直接用账户类型查找（doge, gcore, upyun, qiniu 等无子产品的类型）
	provider, err := base.GetProvider(accountType, accConfig)
	if err == nil {
		return provider, nil
	}

	// 2. 从 taskConfig 中获取 product 字段，组合为 "{type}_{product}"
	if len(taskConfig) > 0 && taskConfig[0] != nil {
		if product, ok := taskConfig[0]["product"]; ok {
			if productStr, ok := product.(string); ok && productStr != "" {
				combinedKey := accountType + "_" + productStr
				p, e := base.GetProvider(combinedKey, accConfig)
				if e == nil {
					return p, nil
				}
			}
		}
	}

	// 3. 从 accConfig 中获取 product（某些旧数据可能存在）
	if product, ok := accConfig["product"]; ok {
		if productStr, ok := product.(string); ok && productStr != "" {
			combinedKey := accountType + "_" + productStr
			p, e := base.GetProvider(combinedKey, accConfig)
			if e == nil {
				return p, nil
			}
		}
	}

	// 4. 常见子产品后缀回退（如 AWS 控制台默认 acm 而非 cdn；仅 _cdn 会解析失败）
	for _, suffix := range []string{"cdn", "acm", "cloudfront"} {
		key := accountType + "_" + suffix
		provider, err = base.GetProvider(key, accConfig)
		if err == nil {
			return provider, nil
		}
	}

	return nil, fmt.Errorf("unknown deploy provider: %s", accountType)
}

/* GetConfigDomains 从配置中获取域名列表（向后兼容包装） */
func GetConfigDomains(config map[string]interface{}) []string {
	return base.GetConfigDomains(config)
}
