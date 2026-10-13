package handler

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"main/internal/cache"
	"main/internal/captcha"
	"main/internal/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const captchaVerifyKeyPrefix = "captcha_verify:"
const captchaVerifyTTL = 10 * time.Minute

// pickBehavioralCaptchaType 解析查询参数 type，缺省则随机一种
func pickBehavioralCaptchaType(q string) string {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "click", "slide", "rotate":
		return q
	default:
		variants := []string{"click", "slide", "rotate"}
		return variants[rand.IntN(len(variants))]
	}
}

// GetBehavioralCaptcha 生成 go-captcha 行为验证码（点选 / 滑动 / 旋转）
// GET /api/auth/captcha/behavioral?type=click|slide|rotate（type 可选）
func GetBehavioralCaptcha(c *gin.Context) {
	capType := pickBehavioralCaptchaType(c.Query("type"))
	res, err := captcha.Generate(capType)
	if err != nil {
		logger.Error("[Captcha] 行为验证码生成失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码生成失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": res})
}

type verifyBehavioralCaptchaBody struct {
	CaptchaID   string          `json:"captcha_id"`
	CaptchaType string          `json:"captcha_type"`
	Answer      json.RawMessage `json:"answer"`
}

// VerifyBehavioralCaptcha 校验行为验证码答案，成功后返回短期 verify_token
// POST /api/auth/captcha/behavioral/verify
// 与 /api/auth/captcha/go/verify 共用实现
func VerifyBehavioralCaptcha(c *gin.Context) {
	var body verifyBehavioralCaptchaBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "参数错误"})
		return
	}
	body.CaptchaType = strings.ToLower(strings.TrimSpace(body.CaptchaType))
	if body.CaptchaID == "" || body.CaptchaType == "" || len(body.Answer) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "参数不完整"})
		return
	}
	if !captcha.Verify(body.CaptchaID, body.CaptchaType, body.Answer) {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证失败"})
		return
	}
	token := uuid.New().String()
	key := captchaVerifyKeyPrefix + token
	if cache.C == nil {
		logger.Error("[Captcha] 缓存未初始化，无法签发 verify_token")
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证失败"})
		return
	}
	if err := cache.C.Set(key, "1", captchaVerifyTTL); err != nil {
		logger.Error("[Captcha] 写入 verify_token 失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"verify_token": token},
	})
}

// ConsumeCaptchaVerifyToken 校验并一次性消费行为验证码通过令牌（登录等场景可调用）
func ConsumeCaptchaVerifyToken(token string) bool {
	if token == "" || cache.C == nil {
		return false
	}
	key := captchaVerifyKeyPrefix + token
	_, ok := cache.C.Get(key)
	if !ok {
		return false
	}
	_ = cache.C.Delete(key)
	return true
}
