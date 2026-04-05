package sysconfig

import (
	"time"

	"main/internal/cache"
	"main/internal/database"
	"main/internal/models"
)

/*
 * 共享系统配置缓存层
 * 功能：为后台任务（cert deploy executor / task_runner / monitor 等）
 *       提供带缓存的 SysConfig 读取，避免每次都直接查 DB
 *       handler 包中的 GetSysConfigValue 委托到此处，消除重复实现
 */

const (
	cachePrefix = "syscfg:"
	cacheTTL    = 60 * time.Second
)

/*
 * GetValue 获取系统配置值（带缓存）
 * 功能：先查缓存，命中直接返回；未命中则查 DB 并写入缓存（60秒 TTL）
 */
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

/*
 * Invalidate 清除指定配置项的缓存
 * 功能：配置更新后调用，确保下次读取获取最新值
 */
func Invalidate(keys ...string) {
	for _, key := range keys {
		cache.C.Delete(cachePrefix + key)
	}
}
