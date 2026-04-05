package cache

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// goRedisCache 基于 github.com/redis/go-redis/v9 的缓存实现
type goRedisCache struct {
	rdb    *redis.Client
	prefix string
}

func newGoRedisCache(rdb *redis.Client, keyPrefix string) *goRedisCache {
	return &goRedisCache{rdb: rdb, prefix: keyPrefix}
}

func (g *goRedisCache) fk(key string) string {
	if g.prefix == "" {
		return key
	}
	return g.prefix + key
}

func (g *goRedisCache) Close() error {
	return g.rdb.Close()
}

func (g *goRedisCache) IsRedis() bool { return true }

func (g *goRedisCache) Set(key, value string, ttl time.Duration) error {
	ctx := context.Background()
	k := g.fk(key)
	if ttl > 0 {
		return g.rdb.Set(ctx, k, value, ttl).Err()
	}
	return g.rdb.Set(ctx, k, value, 0).Err()
}

func (g *goRedisCache) Get(key string) (string, bool) {
	ctx := context.Background()
	val, err := g.rdb.Get(ctx, g.fk(key)).Result()
	if err == redis.Nil {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return val, true
}

func (g *goRedisCache) Delete(key string) error {
	return g.rdb.Del(context.Background(), g.fk(key)).Err()
}

func (g *goRedisCache) DeletePrefix(logicalPrefix string) error {
	ctx := context.Background()
	pattern := g.fk(logicalPrefix) + "*"
	iter := g.rdb.Scan(ctx, 0, pattern, 100).Iterator()
	var batch []string
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) >= 500 {
			if err := g.rdb.Del(ctx, batch...).Err(); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := g.rdb.Del(ctx, batch...).Err(); err != nil {
			return err
		}
	}
	return iter.Err()
}

func (g *goRedisCache) Incr(key string, ttl time.Duration) (int64, error) {
	ctx := context.Background()
	k := g.fk(key)
	n, err := g.rdb.Incr(ctx, k).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 && ttl > 0 {
		_ = g.rdb.Expire(ctx, k, ttl).Err()
	}
	return n, nil
}

func (g *goRedisCache) SetJSON(key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return g.Set(key, string(data), ttl)
}

func (g *goRedisCache) GetJSON(key string, dest interface{}) bool {
	val, ok := g.Get(key)
	if !ok {
		return false
	}
	return json.Unmarshal([]byte(val), dest) == nil
}

func (g *goRedisCache) LPush(key, value string) error {
	return g.rdb.LPush(context.Background(), g.fk(key), value).Err()
}

func (g *goRedisCache) LLen(key string) (int64, error) {
	return g.rdb.LLen(context.Background(), g.fk(key)).Result()
}

func (g *goRedisCache) LRange(key string, start, stop int64) ([]string, error) {
	return g.rdb.LRange(context.Background(), g.fk(key), start, stop).Result()
}

func (g *goRedisCache) LTrim(key string, start, stop int64) error {
	return g.rdb.LTrim(context.Background(), g.fk(key), start, stop).Err()
}

// NormalizeKeyPrefix 确保前缀以冒号结尾（若非空）
func NormalizeKeyPrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasSuffix(p, ":") {
		return p + ":"
	}
	return p
}
