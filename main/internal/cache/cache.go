package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"main/internal/logger"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache 通用缓存接口
type Cache interface {
	Set(key string, value string, ttl time.Duration) error
	Get(key string) (string, bool)
	Delete(key string) error
	DeletePrefix(logicalPrefix string) error
	Incr(key string, ttl time.Duration) (int64, error)
	SetJSON(key string, value interface{}, ttl time.Duration) error
	GetJSON(key string, dest interface{}) bool
	// 列表（供 logstore 等）；内存与 Redis 后端均需实现
	IsRedis() bool
	LPush(key, value string) error
	LLen(key string) (int64, error)
	LRange(key string, start, stop int64) ([]string, error)
	LTrim(key string, start, stop int64) error
	Close() error
}

// 全局缓存实例
var C Cache

// Config Redis / 缓存初始化配置
type Config struct {
	Enable       bool   `json:"enable"`
	Addr         string `json:"addr"` // host:port
	Password     string `json:"password"`
	DB           int    `json:"db"`
	PoolSize     int    `json:"pool_size"`
	MinIdleConns int    `json:"min_idle_conns"`
	KeyPrefix    string `json:"key_prefix"` // 逻辑 key 前追加，避免多环境共 Redis 冲突
}

// Init 初始化缓存（配置了 Redis 就用 Redis，否则用内存）
func Init(cfg *Config) {
	prefix := ""
	if cfg != nil {
		prefix = NormalizeKeyPrefix(cfg.KeyPrefix)
	}

	if cfg != nil && cfg.Enable && cfg.Addr != "" {
		pool := cfg.PoolSize
		if pool <= 0 {
			pool = 10
		}
		minIdle := cfg.MinIdleConns
		if minIdle < 0 {
			minIdle = 0
		}

		rdb := redis.NewClient(&redis.Options{
			Addr:         cfg.Addr,
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     pool,
			MinIdleConns: minIdle,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := rdb.Ping(ctx).Err()
		cancel()
		if err != nil {
			logger.Warn("[Cache] Redis 连接失败(%s)，回退到内存缓存: %v", cfg.Addr, err)
			_ = rdb.Close()
			C = NewMemoryCache(prefix)
		} else {
			C = newGoRedisCache(rdb, prefix)
			logger.Info("[Cache] 使用 Redis 缓存: %s (pool=%d)", cfg.Addr, pool)
		}
	} else {
		C = NewMemoryCache(prefix)
		logger.Info("[Cache] 使用内存缓存")
	}
}

// Close 关闭全局缓存（主要为释放 Redis 连接池）
func Close() error {
	if C == nil {
		return nil
	}
	return C.Close()
}

// ==================== 内存缓存实现 ====================

type memoryEntry struct {
	Value    string
	ExpireAt time.Time
}

type memoryCache struct {
	mu     sync.RWMutex
	prefix string
	store  map[string]memoryEntry
	lists  map[string][]string // Redis 风格 list：LPush 在头部插入
}

func NewMemoryCache(keyPrefix string) Cache {
	mc := &memoryCache{
		prefix: NormalizeKeyPrefix(keyPrefix),
		store:  make(map[string]memoryEntry),
		lists:  make(map[string][]string),
	}
	go mc.cleanupLoop()
	return mc
}

func (m *memoryCache) fk(key string) string {
	if m.prefix == "" {
		return key
	}
	return m.prefix + key
}

func (m *memoryCache) Close() error { return nil }

func (m *memoryCache) IsRedis() bool { return false }

func (m *memoryCache) DeletePrefix(logicalPrefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fp := m.fk(logicalPrefix)
	for k := range m.store {
		if strings.HasPrefix(k, fp) {
			delete(m.store, k)
		}
	}
	return nil
}

func (m *memoryCache) LPush(key, value string) error {
	k := m.fk(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lists[k] = append([]string{value}, m.lists[k]...)
	return nil
}

func (m *memoryCache) LLen(key string) (int64, error) {
	k := m.fk(key)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.lists[k])), nil
}

func (m *memoryCache) lIndexToPos(n int, idx int64) int {
	if idx < 0 {
		idx += int64(n)
	}
	if idx < 0 || int(idx) >= n {
		return -1
	}
	return int(idx)
}

func (m *memoryCache) LRange(key string, start, stop int64) ([]string, error) {
	k := m.fk(key)
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.lists[k]
	n := len(list)
	if n == 0 {
		return nil, nil
	}
	s := m.lIndexToPos(n, start)
	e := m.lIndexToPos(n, stop)
	if s < 0 {
		s = 0
	}
	if e < 0 {
		e = n - 1
	}
	if s > e || s >= n {
		return nil, nil
	}
	if e >= n {
		e = n - 1
	}
	out := make([]string, e-s+1)
	copy(out, list[s:e+1])
	return out, nil
}

func (m *memoryCache) LTrim(key string, start, stop int64) error {
	k := m.fk(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.lists[k]
	n := len(list)
	if n == 0 {
		return nil
	}
	s := m.lIndexToPos(n, start)
	e := m.lIndexToPos(n, stop)
	if s < 0 {
		s = 0
	}
	if e < 0 || s > e || s >= n {
		delete(m.lists, k)
		return nil
	}
	if e >= n {
		e = n - 1
	}
	m.lists[k] = append([]string(nil), list[s:e+1]...)
	return nil
}

func (m *memoryCache) Set(key, value string, ttl time.Duration) error {
	k := m.fk(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	exp := time.Now().Add(ttl)
	if ttl <= 0 {
		// 无过期：使用远未来时间，避免 cleanup 立即删除
		exp = time.Now().Add(365 * 24 * time.Hour * 100)
	}
	m.store[k] = memoryEntry{Value: value, ExpireAt: exp}
	return nil
}

func (m *memoryCache) Get(key string) (string, bool) {
	k := m.fk(key)
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.store[k]
	if !ok || time.Now().After(entry.ExpireAt) {
		return "", false
	}
	return entry.Value, true
}

func (m *memoryCache) Delete(key string) error {
	k := m.fk(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, k)
	return nil
}

func (m *memoryCache) Incr(key string, ttl time.Duration) (int64, error) {
	k := m.fk(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.store[k]
	var count int64 = 1
	if ok && time.Now().Before(entry.ExpireAt) {
		fmt.Sscanf(entry.Value, "%d", &count)
		count++
		var exp time.Time
		if ttl > 0 {
			exp = time.Now().Add(ttl)
		} else {
			exp = entry.ExpireAt
		}
		m.store[k] = memoryEntry{Value: fmt.Sprintf("%d", count), ExpireAt: exp}
	} else {
		exp := time.Now().Add(ttl)
		if ttl <= 0 {
			exp = time.Now().Add(365 * 24 * time.Hour * 100)
		}
		m.store[k] = memoryEntry{Value: "1", ExpireAt: exp}
	}
	return count, nil
}

func (m *memoryCache) SetJSON(key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return m.Set(key, string(data), ttl)
}

func (m *memoryCache) GetJSON(key string, dest interface{}) bool {
	val, ok := m.Get(key)
	if !ok {
		return false
	}
	return json.Unmarshal([]byte(val), dest) == nil
}

func (m *memoryCache) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for k, v := range m.store {
			if now.After(v.ExpireAt) {
				delete(m.store, k)
			}
		}
		m.mu.Unlock()
	}
}
