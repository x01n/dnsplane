package utils

import (
	"runtime/debug"

	"main/internal/logger"
)

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

func RecoverPanic(name string) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		logger.Error("[Recover:%s] panic: %v\n%s", name, r, string(stack))
	}
}
