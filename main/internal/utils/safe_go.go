package utils

import (
	"runtime/debug"

	"main/internal/logger"
)

/*
 * SafeGo 安全启动 goroutine
 * 功能：包装 goroutine 添加 panic recover 保护，防止子协程 panic 导致整个进程崩溃
 * 使用方式：utils.SafeGo(func() { ... })
 */
func SafeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				logger.Error("[SafeGo] goroutine panic recovered: %v\n%s", r, string(stack))
			}
		}()
		fn()
	}()
}

/*
 * SafeGoWithName 带名称标识的安全 goroutine
 * 功能：recover 时附带 goroutine 名称，便于排查问题来源
 */
func SafeGoWithName(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				logger.Error("[SafeGo:%s] goroutine panic recovered: %v\n%s", name, r, string(stack))
			}
		}()
		fn()
	}()
}

/*
 * RecoverPanic 用于 defer 的 panic 恢复
 * 功能：在已有 goroutine 的 defer 中调用，例如 defer utils.RecoverPanic("task_name")
 */
func RecoverPanic(name string) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		logger.Error("[Recover:%s] panic: %v\n%s", name, r, string(stack))
	}
}
