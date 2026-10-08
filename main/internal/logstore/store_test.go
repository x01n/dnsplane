package logstore

import (
	"testing"
	"time"

	"main/internal/cache"
	"main/internal/models"
)

/* initMemoryLogStore 让包级 Store 指向内存缓存；用例不触达 Redis */
func initMemoryLogStore(t *testing.T) {
	t.Helper()
	cache.Init(&cache.Config{})
	t.Cleanup(func() { _ = cache.Close() })
	Init()
	if Store.IsRedis() {
		t.Fatal("用例要求内存缓存，实际为 Redis")
	}
}

// TestPurgeSystemLogsClearsAll 清空后列表归零，返回值等于清空前的条数。
// 这条断言同时锁住实现方式：LTrim(key, 0, -1) 会保留全部条目而仍报告已删除
func TestPurgeSystemLogsClearsAll(t *testing.T) {
	initMemoryLogStore(t)

	for i := 0; i < 3; i++ {
		Store.SaveSystemLog(models.Log{Action: "probe", CreatedAt: time.Now()})
	}
	if n, _ := cache.C.LLen(systemLogKey); n != 3 {
		t.Fatalf("写入后长度 = %d，期望 3", n)
	}

	if got := Store.PurgeSystemLogs(); got != 3 {
		t.Errorf("清除条数 = %d，期望 3", got)
	}
	if n, _ := cache.C.LLen(systemLogKey); n != 0 {
		t.Errorf("清空后长度 = %d，期望 0", n)
	}
}

// TestPurgeSystemLogsOnEmpty 空列表返回 0，重复清空不得报错
func TestPurgeSystemLogsOnEmpty(t *testing.T) {
	initMemoryLogStore(t)

	if got := Store.PurgeSystemLogs(); got != 0 {
		t.Errorf("空列表清除条数 = %d，期望 0", got)
	}
}
