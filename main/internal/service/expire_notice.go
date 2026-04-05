package service

import (
	"context"
	"time"

	"main/internal/database"
	"main/internal/models"
	"main/internal/whois"
)

/*
 * CheckDomainWhoisNow 按需查询指定域名的 WHOIS 信息并更新到期时间
 * @param domainID - 域名记录 ID（UUID 字符串）
 * @returns error
 */
func CheckDomainWhoisNow(domainID string) error {
	var domain models.Domain
	if err := database.DB.Where("id = ?", domainID).First(&domain).Error; err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := whois.Query(ctx, domain.Name)
	if err != nil {
		return err
	}

	updates := map[string]interface{}{
		"check_time": time.Now(),
	}
	if info.ExpiryDate != nil {
		updates["expire_time"] = info.ExpiryDate
	}
	if info.CreatedDate != nil {
		updates["reg_time"] = info.CreatedDate
	}

	return database.DB.Model(&domain).Updates(updates).Error
}
