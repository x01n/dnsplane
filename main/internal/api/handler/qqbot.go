package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/notify"
	"main/internal/sysconfig"

	"github.com/gin-gonic/gin"
)

/*
 * qqBotWebhookPayload QQ 机器人回调报文（仅声明本控制器消费的字段）
 */
type qqBotWebhookPayload struct {
	// Op 官方回调为数字 13，但需按任意 JSON 标量接收：字符串形态的 "13" 同样要能识别
	Op interface{} `json:"op"`
	T  string      `json:"t"`
	D  struct {
		EventTs    string `json:"event_ts"`
		PlainToken string `json:"plain_token"`
		ID         string `json:"id"`
		Author     struct {
			UserOpenID string `json:"user_openid"`
		} `json:"author"`
	} `json:"d"`
}

/* qqBotOpValue 按整数语义解析 op：数字直接取整，数字字符串解析为整数，其余为 0 */
func qqBotOpValue(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		var i int
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%d", &i); err == nil {
			return i
		}
	}
	return 0
}

/* qqBotWebhookConfig 读取 QQ 机器人配置（appid/appsecret/openid），任一缺失时返回 ok=false */
func qqBotWebhookConfig() (appID, appSecret, openID string, ok bool) {
	appID = strings.TrimSpace(sysconfig.GetValue("qqbot_appid"))
	appSecret = strings.TrimSpace(sysconfig.GetValue("qqbot_appsecret"))
	if appID == "" || appSecret == "" {
		return "", "", "", false
	}
	return appID, appSecret, strings.TrimSpace(sysconfig.GetValue("qqbot_openid")), true
}

/*
 * QQBotWebhook 处理 QQ 开放平台的 Webhook 回调
 *   - op=13 为回调地址验证，直接返回 {plain_token, signature}（不验签）
 *   - 其余请求先校验 x-signature-ed25519 / x-signature-timestamp（验签对象 timestamp+body）
 *   - C2C_MESSAGE_CREATE 事件用于绑定 openid 并回复绑定成功
 */
func QQBotWebhook(c *gin.Context) {
	// 限制读取体积，避免超大报文打满内存
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	body := string(raw)

	var payload qqBotWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		c.String(http.StatusBadRequest, "bad request")
		return
	}

	appID, appSecret, openID, ok := qqBotWebhookConfig()
	if !ok {
		c.String(http.StatusBadRequest, "not configured")
		return
	}

	// 声明了 x-bot-appid 时必须与本地配置一致
	if headerAppID := strings.TrimSpace(c.GetHeader("x-bot-appid")); headerAppID != "" && headerAppID != appID {
		c.String(http.StatusForbidden, "appid mismatch")
		return
	}

	// op 为回调地址验证事件（官方取值 13），此处按整数语义判定，兼容数字与数字字符串两种形态
	if qqBotOpValue(payload.Op) == 13 {
		signature, err := notify.QQBotSignValidation(appSecret, payload.D.EventTs, payload.D.PlainToken)
		if err != nil {
			logger.Error("[QqBot] 回调校验失败: %v", err)
			c.String(http.StatusInternalServerError, "error")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"plain_token": payload.D.PlainToken,
			"signature":   signature,
		})
		return
	}

	timestamp := c.GetHeader("x-signature-timestamp")
	signature := c.GetHeader("x-signature-ed25519")
	if !notify.QQBotVerifySignature(appSecret, timestamp, signature, body) {
		c.String(http.StatusUnauthorized, "invalid signature")
		return
	}

	if strings.EqualFold(strings.TrimSpace(payload.T), "C2C_MESSAGE_CREATE") {
		qqBotHandleC2CMessage(appID, appSecret, openID, payload.D.Author.UserOpenID, payload.D.ID)
	}
	c.String(http.StatusOK, "")
}

/*
 * qqBotHandleC2CMessage 首次收到私聊消息时把 user_openid 落库为绑定账号，并回复绑定成功
 */
func qqBotHandleC2CMessage(appID, appSecret, oldOpenID, newOpenID, msgID string) {
	openID := strings.TrimSpace(newOpenID)
	if openID == "" || openID == oldOpenID {
		return
	}

	// 绑定结果落库到 SysConfig（key 为 qqbot_openid），与渠道装载读取的是同一份配置
	var config models.SysConfig
	if err := database.DB.Where("`key` = ?", "qqbot_openid").First(&config).Error; err != nil {
		database.DB.Create(&models.SysConfig{Key: "qqbot_openid", Value: openID})
	} else {
		database.DB.Model(&config).Update("value", openID)
	}
	sysconfig.Invalidate("qqbot_openid")

	// 绑定回复失败不影响绑定结果本身，仅记录日志
	siteName := strings.TrimSpace(sysconfig.GetValue("site_name"))
	if siteName == "" {
		siteName = "DNSPlane"
	}
	bot := notify.NewQQBotNotifier(notify.QQBotConfig{AppID: appID, AppSecret: appSecret, OpenID: openID})
	content := fmt.Sprintf("# 绑定成功\n\n您已成功绑定QQ机器人消息通知，%s通知将通过本机器人发送。", siteName)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := bot.SendMarkdownTo(ctx, openID, content, msgID); err != nil {
		logger.Error("[QqBot] 绑定回复发送失败: %v", err)
	}
	logger.Info("[QqBot] 已绑定 openid: %s", openID)
}
