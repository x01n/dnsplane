package handler

import (
	"testing"
	"time"
)

/* TestOptimizeRunState 覆盖运行状态判定：1 小时内更新为 ok，否则 error */
func TestOptimizeRunState(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)

	// 从未执行过：error
	if got := optimizeRunState(nil, now); got != "error" {
		t.Errorf("无执行记录应为 error，实际 %q", got)
	}

	// 59 分钟前更新：ok
	recent := now.Add(-59 * time.Minute)
	if got := optimizeRunState(&recent, now); got != "ok" {
		t.Errorf("59 分钟前更新应为 ok，实际 %q", got)
	}

	// 恰好 1 小时前更新：error（阈值判定为严格小于 1 小时才算 ok）
	exact := now.Add(-time.Hour)
	if got := optimizeRunState(&exact, now); got != "error" {
		t.Errorf("正好 1 小时前更新应为 error，实际 %q", got)
	}

	// 2 小时前更新：error
	stale := now.Add(-2 * time.Hour)
	if got := optimizeRunState(&stale, now); got != "error" {
		t.Errorf("2 小时前更新应为 error，实际 %q", got)
	}
}
