package sysconfig

import (
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
