package service

import (
	"encoding/json"
	"main/internal/database"
	"main/internal/logstore"
	"main/internal/models"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func parseLogUint(s string) uint {
	u, _ := strconv.ParseUint(s, 10, 32)
	return uint(u)
}

/* saveLog 保存系统日志（Redis 优先，否则 SQLite） */
func saveLog(log models.Log) error {
	if logstore.Store != nil && logstore.Store.IsRedis() {
		logstore.Store.SaveSystemLog(log)
		return nil
	}
	return database.LogDB.Create(&log).Error
}

/* AuditService 审计日志服务，提供统一的操作日志记录接口 */
type AuditService struct{}

/* NewAuditService 创建审计服务实例 */
func NewAuditService() *AuditService {
	return &AuditService{}
}

/* LogAction 记录操作日志（简单版本，自动提取用户信息） */
func (s *AuditService) LogAction(c *gin.Context, action, domain, data string) error {
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	log := models.Log{
		UserID:    parseLogUint(userID),
		Username:  username,
		Action:    action,
		Domain:    domain,
		Data:      data,
		IP:        ip,
		UserAgent: ua,
		CreatedAt: time.Now(),
	}

	return saveLog(log)
}

/* LogChange 记录数据变更日志（完整审计版本，含变更前后数据） */
func (s *AuditService) LogChange(c *gin.Context, entity string, entityID string, action string, before, after interface{}) error {
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	var beforeJSON, afterJSON string

	if before != nil {
		if b, err := json.Marshal(before); err == nil {
			beforeJSON = string(b)
		}
	}

	if after != nil {
		if a, err := json.Marshal(after); err == nil {
			afterJSON = string(a)
		}
	}

	log := models.Log{
		UserID:     parseLogUint(userID),
		Username:   username,
		Action:     action,
		Entity:     entity,
		EntityID:   parseLogUint(entityID),
		BeforeData: beforeJSON,
		AfterData:  afterJSON,
		IP:         ip,
		UserAgent:  ua,
		CreatedAt:  time.Now(),
	}

	return saveLog(log)
}

/* LogDomainChange 记录域名相关变更日志 */
func (s *AuditService) LogDomainChange(c *gin.Context, domain string, action string, entityID string, before, after interface{}) error {
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	var beforeJSON, afterJSON string

	if before != nil {
		if b, err := json.Marshal(before); err == nil {
			beforeJSON = string(b)
		}
	}

	if after != nil {
		if a, err := json.Marshal(after); err == nil {
			afterJSON = string(a)
		}
	}

	log := models.Log{
		UserID:     parseLogUint(userID),
		Username:   username,
		Action:     action,
		Entity:     "domain_record",
		EntityID:   parseLogUint(entityID),
		Domain:     domain,
		BeforeData: beforeJSON,
		AfterData:  afterJSON,
		IP:         ip,
		UserAgent:  ua,
		CreatedAt:  time.Now(),
	}

	return saveLog(log)
}

/* LogUserAction 记录用户操作（登录/登出/重置Key等） */
func (s *AuditService) LogUserAction(c *gin.Context, userID string, username, action, data string) error {
	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	uid := parseLogUint(userID)
	log := models.Log{
		UserID:    uid,
		Username:  username,
		Action:    action,
		Entity:    "user",
		EntityID:  uid,
		Data:      data,
		IP:        ip,
		UserAgent: ua,
		CreatedAt: time.Now(),
	}

	return saveLog(log)
}

/* LogCertAction 记录证书相关操作日志 */
func (s *AuditService) LogCertAction(c *gin.Context, action string, orderID string, data string) error {
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	log := models.Log{
		UserID:    parseLogUint(userID),
		Username:  username,
		Action:    action,
		Entity:    "cert_order",
		EntityID:  parseLogUint(orderID),
		Data:      data,
		IP:        ip,
		UserAgent: ua,
		CreatedAt: time.Now(),
	}

	return saveLog(log)
}

/* LogDeployAction 记录部署相关操作日志 */
func (s *AuditService) LogDeployAction(c *gin.Context, action string, deployID string, data string) error {
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	log := models.Log{
		UserID:    parseLogUint(userID),
		Username:  username,
		Action:    action,
		Entity:    "cert_deploy",
		EntityID:  parseLogUint(deployID),
		Data:      data,
		IP:        ip,
		UserAgent: ua,
		CreatedAt: time.Now(),
	}

	return saveLog(log)
}

/*
 * LogActionDirect 直接记录操作日志（不依赖 gin.Context）
 * 功能：供异步 goroutine 中使用，需调用方提前提取用户信息
 */
func (s *AuditService) LogActionDirect(userID, username, ip, ua, action, domain, data string) {
	log := models.Log{
		UserID:    parseLogUint(userID),
		Username:  username,
		Action:    action,
		Domain:    domain,
		Data:      data,
		IP:        ip,
		UserAgent: ua,
		CreatedAt: time.Now(),
	}
	saveLog(log)
}

/* Audit 全局审计服务单例 */
var Audit = NewAuditService()
