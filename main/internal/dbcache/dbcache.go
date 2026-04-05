package dbcache

import (
	"context"
	"encoding/json"
	"time"

	"main/internal/cache"
)

// DefaultTTL 热点列表读穿缓存默认存活时间
const DefaultTTL = 60 * time.Second

// GetOrSetJSON 读穿缓存：命中则反序列化到 dest；未命中则执行 load，写入缓存并填充 dest
func GetOrSetJSON(ctx context.Context, key string, ttl time.Duration, load func() (interface{}, error), dest interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cache.C == nil {
		v, err := load()
		if err != nil {
			return err
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, dest)
	}
	if cache.C.GetJSON(key, dest) {
		return nil
	}
	v, err := load()
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := cache.C.Set(key, string(b), ttl); err != nil {
		_ = err
	}
	return json.Unmarshal(b, dest)
}

// Delete 删除一个或多个逻辑 key（与 Cache 层 key 前缀规则一致）
func Delete(ctx context.Context, keys ...string) error {
	_ = ctx
	if cache.C == nil {
		return nil
	}
	for _, k := range keys {
		if err := cache.C.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

// DeletePrefix 按逻辑前缀删除（Redis 使用 SCAN；内存遍历）
func DeletePrefix(ctx context.Context, logicalPrefix string) error {
	_ = ctx
	if cache.C == nil {
		return nil
	}
	return cache.C.DeletePrefix(logicalPrefix)
}
