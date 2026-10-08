package sysconfig

import (
	"strings"
	"time"

	"main/internal/cache"
	"main/internal/database"
	"main/internal/models"
)


const (
	cachePrefix = "syscfg:"
	cacheTTL    = 60 * time.Second
)

func GetValue(key string) string {
	cacheKey := cachePrefix + key
	if val, ok := cache.C.Get(cacheKey); ok {
		return val
	}
	var value string
	database.DB.Model(&models.SysConfig{}).Where("`key` = ?", key).Pluck("value", &value)
	cache.C.Set(cacheKey, value, cacheTTL)
	return value
}

func Invalidate(keys ...string) {
	for _, key := range keys {
		cache.C.Delete(cachePrefix + key)
	}
}

func SetValue(key, value string) {
	database.DB.Where("`key` = ?", key).Assign(models.SysConfig{Value: value}).FirstOrCreate(&models.SysConfig{Key: key})
	cache.C.Set(cachePrefix+key, value, cacheTTL)
}

/* ExplicitOn 开关判据：仅 true/1/yes/on 为开，其余（含未配置的空串）一律为关 */
func ExplicitOn(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "true" || v == "1" || v == "yes" || v == "on"
}
