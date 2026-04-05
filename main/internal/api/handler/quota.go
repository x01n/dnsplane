package handler

import "github.com/gin-gonic/gin"

/*
 * 配额与功能开关（已移除套餐/订阅体系）
 * 保留函数签名，便于调用处无需大改；始终允许创建资源与使用功能。
 */

// CheckQuota 资源数量配额检查（已无套餐限制）
func CheckQuota(_ *gin.Context, _ string, _ string) bool {
	return true
}

// CheckFeature 功能模块开关检查（已无套餐限制）
func CheckFeature(_ *gin.Context, _ string, _ string) bool {
	return true
}
