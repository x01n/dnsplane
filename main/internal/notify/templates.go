package notify

import (
	"fmt"
	"strings"
)

// EmailTemplate 邮件模板类型
type EmailTemplate string

const (
	TemplatePasswordReset EmailTemplate = "password_reset"
	TemplateTOTPReset     EmailTemplate = "totp_reset"
)

// RenderPasswordResetEmail 渲染密码重置邮件
func RenderPasswordResetEmail(username, resetLink, expireMinutes string) (subject, body string) {
	subject = "DNSPlane - 密码重置"
	body = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>密码重置</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; background-color: #f5f5f5; }
        .container { max-width: 600px; margin: 40px auto; background: #fff; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); overflow: hidden; }
        .header { background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); padding: 30px; text-align: center; }
        .header h1 { color: #fff; margin: 0; font-size: 24px; }
        .content { padding: 30px; }
        .content h2 { color: #333; margin-top: 0; }
        .button { display: inline-block; padding: 12px 30px; background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); color: #fff !important; text-decoration: none; border-radius: 6px; font-weight: 500; margin: 20px 0; }
        .button:hover { opacity: 0.9; }
        .warning { background: #fff3cd; border: 1px solid #ffc107; border-radius: 6px; padding: 15px; margin: 20px 0; color: #856404; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; color: #666; font-size: 12px; border-top: 1px solid #eee; }
        .link-text { word-break: break-all; background: #f5f5f5; padding: 10px; border-radius: 4px; font-size: 12px; color: #666; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>DNSPlane</h1>
        </div>
        <div class="content">
            <h2>密码重置请求</h2>
            <p>您好，<strong>%s</strong>：</p>
            <p>我们收到了您的密码重置请求。请点击下方按钮重置您的密码：</p>
            <p style="text-align: center;">
                <a href="%s" class="button">重置密码</a>
            </p>
            <div class="warning">
                <strong>安全提示：</strong>
                <ul style="margin: 5px 0; padding-left: 20px;">
                    <li>此链接将在 <strong>%s 分钟</strong>后失效</li>
                    <li>如果您没有请求重置密码，请忽略此邮件</li>
                    <li>请勿将此链接分享给他人</li>
                </ul>
            </div>
            <p>如果按钮无法点击，请复制以下链接到浏览器：</p>
            <p class="link-text">%s</p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿直接回复</p>
            <p>&copy; DNSPlane - DNS管理系统</p>
        </div>
    </div>
</body>
</html>`, username, resetLink, expireMinutes, resetLink)

	return subject, body
}

// RenderTOTPResetEmail 渲染TOTP重置邮件
func RenderTOTPResetEmail(username, resetLink, expireMinutes string) (subject, body string) {
	subject = "DNSPlane - 二步验证重置"
	body = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>二步验证重置</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; background-color: #f5f5f5; }
        .container { max-width: 600px; margin: 40px auto; background: #fff; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); overflow: hidden; }
        .header { background: linear-gradient(135deg, #f093fb 0%%, #f5576c 100%%); padding: 30px; text-align: center; }
        .header h1 { color: #fff; margin: 0; font-size: 24px; }
        .content { padding: 30px; }
        .content h2 { color: #333; margin-top: 0; }
        .button { display: inline-block; padding: 12px 30px; background: linear-gradient(135deg, #f093fb 0%%, #f5576c 100%%); color: #fff !important; text-decoration: none; border-radius: 6px; font-weight: 500; margin: 20px 0; }
        .button:hover { opacity: 0.9; }
        .warning { background: #f8d7da; border: 1px solid #f5c6cb; border-radius: 6px; padding: 15px; margin: 20px 0; color: #721c24; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; color: #666; font-size: 12px; border-top: 1px solid #eee; }
        .link-text { word-break: break-all; background: #f5f5f5; padding: 10px; border-radius: 4px; font-size: 12px; color: #666; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>DNSPlane</h1>
        </div>
        <div class="content">
            <h2>二步验证(TOTP)重置请求</h2>
            <p>您好，<strong>%s</strong>：</p>
            <p>我们收到了您的二步验证重置请求。点击下方按钮将<strong>关闭</strong>您账户的二步验证功能：</p>
            <p style="text-align: center;">
                <a href="%s" class="button">重置二步验证</a>
            </p>
            <div class="warning">
                <strong>重要警告：</strong>
                <ul style="margin: 5px 0; padding-left: 20px;">
                    <li>此操作将<strong>关闭</strong>您账户的二步验证</li>
                    <li>此链接将在 <strong>%s 分钟</strong>后失效</li>
                    <li>如果您没有请求此操作，请立即修改密码</li>
                    <li>建议在登录后重新启用二步验证</li>
                </ul>
            </div>
            <p>如果按钮无法点击，请复制以下链接到浏览器：</p>
            <p class="link-text">%s</p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿直接回复</p>
            <p>&copy; DNSPlane - DNS管理系统</p>
        </div>
    </div>
</body>
</html>`, username, resetLink, expireMinutes, resetLink)

	return subject, body
}

// RenderAdminResetEmail 渲染管理员重置通知邮件
func RenderAdminResetEmail(username, resetType, resetLink, expireMinutes string) (subject, body string) {
	typeText := "密码"
	if resetType == "totp" {
		typeText = "二步验证"
	}

	subject = fmt.Sprintf("DNSPlane - 管理员已为您重置%s", typeText)
	body = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s重置</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; background-color: #f5f5f5; }
        .container { max-width: 600px; margin: 40px auto; background: #fff; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); overflow: hidden; }
        .header { background: linear-gradient(135deg, #11998e 0%%, #38ef7d 100%%); padding: 30px; text-align: center; }
        .header h1 { color: #fff; margin: 0; font-size: 24px; }
        .content { padding: 30px; }
        .content h2 { color: #333; margin-top: 0; }
        .button { display: inline-block; padding: 12px 30px; background: linear-gradient(135deg, #11998e 0%%, #38ef7d 100%%); color: #fff !important; text-decoration: none; border-radius: 6px; font-weight: 500; margin: 20px 0; }
        .info { background: #d1ecf1; border: 1px solid #bee5eb; border-radius: 6px; padding: 15px; margin: 20px 0; color: #0c5460; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; color: #666; font-size: 12px; border-top: 1px solid #eee; }
        .link-text { word-break: break-all; background: #f5f5f5; padding: 10px; border-radius: 4px; font-size: 12px; color: #666; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>DNSPlane</h1>
        </div>
        <div class="content">
            <h2>%s重置通知</h2>
            <p>您好，<strong>%s</strong>：</p>
            <p>系统管理员已为您发起%s重置请求。请点击下方按钮完成重置：</p>
            <p style="text-align: center;">
                <a href="%s" class="button">立即重置</a>
            </p>
            <div class="info">
                <strong>提示：</strong>
                <ul style="margin: 5px 0; padding-left: 20px;">
                    <li>此链接将在 <strong>%s 分钟</strong>后失效</li>
                    <li>如有疑问，请联系系统管理员</li>
                </ul>
            </div>
            <p>如果按钮无法点击，请复制以下链接到浏览器：</p>
            <p class="link-text">%s</p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿直接回复</p>
            <p>&copy; DNSPlane - DNS管理系统</p>
        </div>
    </div>
</body>
</html>`, typeText, typeText, username, typeText, resetLink, expireMinutes, resetLink)

	return subject, body
}

// BuildResetLink 构建重置链接
func BuildResetLink(baseURL, resetType, token string) string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if resetType == "totp" {
		return fmt.Sprintf("%s/reset-totp?token=%s", baseURL, token)
	}
	return fmt.Sprintf("%s/reset-password?token=%s", baseURL, token)
}

// RenderCertExpiryEmail 渲染证书到期提醒邮件
func RenderCertExpiryEmail(siteName string, domains []string, expireDays int, expireDate string) (subject, body string) {
	domainList := strings.Join(domains, ", ")
	subject = fmt.Sprintf("%s - 证书即将到期提醒", siteName)

	urgencyClass := "info"
	urgencyText := "提醒"
	if expireDays <= 7 {
		urgencyClass = "warning"
		urgencyText = "紧急"
	}
	if expireDays <= 3 {
		urgencyClass = "error"
		urgencyText = "警告"
	}

	body = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>证书到期提醒</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; background-color: #f5f5f5; }
        .container { max-width: 600px; margin: 40px auto; background: #fff; border-radius: 12px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); overflow: hidden; }
        .header { background: linear-gradient(135deg, #f093fb 0%%, #f5576c 100%%); padding: 30px; text-align: center; }
        .header h1 { color: #fff; margin: 0; font-size: 24px; }
        .content { padding: 30px; }
        .content h2 { color: #333; margin-top: 0; }
        .info { background: #e3f2fd; border-left: 4px solid #2196f3; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .warning { background: #fff3e0; border-left: 4px solid #ff9800; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .error { background: #ffebee; border-left: 4px solid #f44336; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .domain-list { background: #f8f9fa; border-radius: 8px; padding: 15px; margin: 15px 0; }
        .domain-item { padding: 8px 12px; background: #fff; border-radius: 4px; margin: 5px 0; border: 1px solid #e0e0e0; }
        .stats { display: flex; justify-content: space-around; margin: 20px 0; }
        .stat-item { text-align: center; padding: 15px; }
        .stat-value { font-size: 28px; font-weight: bold; color: #f5576c; }
        .stat-label { font-size: 12px; color: #666; margin-top: 5px; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; color: #666; font-size: 12px; border-top: 1px solid #eee; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>%s</h1>
        </div>
        <div class="content">
            <h2>🔔 证书到期%s</h2>
            <div class="%s">
                <strong>以下域名的SSL证书即将到期，请及时续期：</strong>
            </div>
            <div class="stats">
                <div class="stat-item">
                    <div class="stat-value">%d</div>
                    <div class="stat-label">剩余天数</div>
                </div>
                <div class="stat-item">
                    <div class="stat-value">%s</div>
                    <div class="stat-label">到期日期</div>
                </div>
            </div>
            <div class="domain-list">
                <strong>涉及域名：</strong>
                <div class="domain-item">%s</div>
            </div>
            <p>建议您尽快登录系统进行证书续期操作，以免影响网站正常访问。</p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿直接回复</p>
            <p>&copy; %s</p>
        </div>
    </div>
</body>
</html>`, siteName, urgencyText, urgencyClass, expireDays, expireDate, domainList, siteName)

	return subject, body
}

// RenderDeploySuccessEmail 渲染部署成功通知邮件
func RenderDeploySuccessEmail(siteName string, domains []string, deployType, deployTarget string) (subject, body string) {
	domainList := strings.Join(domains, ", ")
	subject = fmt.Sprintf("%s - 证书部署成功", siteName)

	body = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>证书部署成功</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; background-color: #f5f5f5; }
        .container { max-width: 600px; margin: 40px auto; background: #fff; border-radius: 12px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); overflow: hidden; }
        .header { background: linear-gradient(135deg, #11998e 0%%, #38ef7d 100%%); padding: 30px; text-align: center; }
        .header h1 { color: #fff; margin: 0; font-size: 24px; }
        .content { padding: 30px; }
        .content h2 { color: #333; margin-top: 0; }
        .success { background: #e8f5e9; border-left: 4px solid #4caf50; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .info-table { width: 100%%; border-collapse: collapse; margin: 20px 0; }
        .info-table td { padding: 12px; border-bottom: 1px solid #eee; }
        .info-table td:first-child { color: #666; width: 120px; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; color: #666; font-size: 12px; border-top: 1px solid #eee; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>%s</h1>
        </div>
        <div class="content">
            <h2>✅ 证书部署成功</h2>
            <div class="success">
                <strong>SSL证书已成功部署到目标服务！</strong>
            </div>
            <table class="info-table">
                <tr><td>部署类型</td><td><strong>%s</strong></td></tr>
                <tr><td>部署目标</td><td><strong>%s</strong></td></tr>
                <tr><td>涉及域名</td><td>%s</td></tr>
                <tr><td>部署时间</td><td>%s</td></tr>
            </table>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿直接回复</p>
            <p>&copy; %s</p>
        </div>
    </div>
</body>
</html>`, siteName, deployType, deployTarget, domainList,
		strings.Split(fmt.Sprintf("%v", []interface{}{ /* time placeholder */ }), " ")[0], siteName)

	return subject, body
}

// RenderDeployFailEmail 渲染部署失败通知邮件
func RenderDeployFailEmail(siteName string, domains []string, deployType, deployTarget, errorMsg string) (subject, body string) {
	domainList := strings.Join(domains, ", ")
	subject = fmt.Sprintf("%s - 证书部署失败", siteName)

	body = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>证书部署失败</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; background-color: #f5f5f5; }
        .container { max-width: 600px; margin: 40px auto; background: #fff; border-radius: 12px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); overflow: hidden; }
        .header { background: linear-gradient(135deg, #ff416c 0%%, #ff4b2b 100%%); padding: 30px; text-align: center; }
        .header h1 { color: #fff; margin: 0; font-size: 24px; }
        .content { padding: 30px; }
        .content h2 { color: #333; margin-top: 0; }
        .error { background: #ffebee; border-left: 4px solid #f44336; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .error-detail { background: #f5f5f5; border-radius: 8px; padding: 15px; margin: 15px 0; font-family: monospace; font-size: 13px; color: #d32f2f; overflow-x: auto; }
        .info-table { width: 100%%; border-collapse: collapse; margin: 20px 0; }
        .info-table td { padding: 12px; border-bottom: 1px solid #eee; }
        .info-table td:first-child { color: #666; width: 120px; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; color: #666; font-size: 12px; border-top: 1px solid #eee; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>%s</h1>
        </div>
        <div class="content">
            <h2>❌ 证书部署失败</h2>
            <div class="error">
                <strong>SSL证书部署过程中发生错误，请检查配置后重试。</strong>
            </div>
            <table class="info-table">
                <tr><td>部署类型</td><td><strong>%s</strong></td></tr>
                <tr><td>部署目标</td><td><strong>%s</strong></td></tr>
                <tr><td>涉及域名</td><td>%s</td></tr>
            </table>
            <div class="error-detail">%s</div>
            <p>请检查部署配置是否正确，或联系技术支持获取帮助。</p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿直接回复</p>
            <p>&copy; %s</p>
        </div>
    </div>
</body>
</html>`, siteName, deployType, deployTarget, domainList, errorMsg, siteName)

	return subject, body
}

// RenderTestEmail 渲染测试邮件
func RenderTestEmail(siteName string) (subject, body string) {
	subject = fmt.Sprintf("%s - 邮件配置测试", siteName)

	body = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>邮件配置测试</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; background-color: #f5f5f5; }
        .container { max-width: 600px; margin: 40px auto; background: #fff; border-radius: 12px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); overflow: hidden; }
        .header { background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); padding: 30px; text-align: center; }
        .header h1 { color: #fff; margin: 0; font-size: 24px; }
        .content { padding: 30px; text-align: center; }
        .success-icon { font-size: 64px; margin: 20px 0; }
        .message { font-size: 18px; color: #333; margin: 20px 0; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; color: #666; font-size: 12px; border-top: 1px solid #eee; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>%s</h1>
        </div>
        <div class="content">
            <div class="success-icon">✉️</div>
            <div class="message">
                <strong>恭喜！邮件配置测试成功！</strong>
            </div>
            <p>您的SMTP邮件服务已正确配置，系统可以正常发送通知邮件。</p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿直接回复</p>
            <p>&copy; %s</p>
        </div>
    </div>
</body>
</html>`, siteName, siteName)

	return subject, body
}

// RenderDomainExpiryEmail 渲染域名到期提醒邮件
func RenderDomainExpiryEmail(siteName, domain string, expireDays int, expireDate string) (subject, body string) {
	subject = fmt.Sprintf("%s - 域名即将到期提醒", siteName)

	urgencyClass := "info"
	if expireDays <= 30 {
		urgencyClass = "warning"
	}
	if expireDays <= 7 {
		urgencyClass = "error"
	}

	body = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>域名到期提醒</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; background-color: #f5f5f5; }
        .container { max-width: 600px; margin: 40px auto; background: #fff; border-radius: 12px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); overflow: hidden; }
        .header { background: linear-gradient(135deg, #ff9a56 0%%, #ff6b6b 100%%); padding: 30px; text-align: center; }
        .header h1 { color: #fff; margin: 0; font-size: 24px; }
        .content { padding: 30px; }
        .content h2 { color: #333; margin-top: 0; }
        .info { background: #e3f2fd; border-left: 4px solid #2196f3; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .warning { background: #fff3e0; border-left: 4px solid #ff9800; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .error { background: #ffebee; border-left: 4px solid #f44336; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .domain-box { background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); color: #fff; border-radius: 8px; padding: 20px; margin: 20px 0; text-align: center; }
        .domain-name { font-size: 24px; font-weight: bold; }
        .stats { display: flex; justify-content: space-around; margin: 20px 0; }
        .stat-item { text-align: center; padding: 15px; }
        .stat-value { font-size: 28px; font-weight: bold; color: #ff6b6b; }
        .stat-label { font-size: 12px; color: #666; margin-top: 5px; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; color: #666; font-size: 12px; border-top: 1px solid #eee; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>%s</h1>
        </div>
        <div class="content">
            <h2>🌐 域名到期提醒</h2>
            <div class="%s">
                <strong>您的域名即将到期，请及时续费！</strong>
            </div>
            <div class="domain-box">
                <div class="domain-name">%s</div>
            </div>
            <div class="stats">
                <div class="stat-item">
                    <div class="stat-value">%d</div>
                    <div class="stat-label">剩余天数</div>
                </div>
                <div class="stat-item">
                    <div class="stat-value">%s</div>
                    <div class="stat-label">到期日期</div>
                </div>
            </div>
            <p>域名过期后将无法正常解析，请尽快登录域名注册商进行续费操作。</p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿直接回复</p>
            <p>&copy; %s</p>
        </div>
    </div>
</body>
</html>`, siteName, urgencyClass, domain, expireDays, expireDate, siteName)

	return subject, body
}

// RenderVerificationCodeEmail 渲染验证码邮件
func RenderVerificationCodeEmail(code, scene, minutes string) (subject, body string) {
	sceneText := map[string]string{
		"register":        "注册账户",
		"bindmail":        "绑定邮箱",
		"forgot_password": "找回密码",
		"forgot_totp":     "重置二步验证",
	}
	text := sceneText[scene]
	if text == "" {
		text = "验证操作"
	}

	subject = fmt.Sprintf("DNSPlane - %s验证码", text)
	body = fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><style>
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;margin:0;padding:0;background:#f5f5f5;}
.container{max-width:480px;margin:40px auto;background:#fff;border-radius:12px;overflow:hidden;box-shadow:0 2px 12px rgba(0,0,0,.08);}
.header{background:linear-gradient(135deg,#6366f1,#8b5cf6);padding:32px;text-align:center;color:#fff;}
.header h1{margin:0;font-size:20px;}
.content{padding:32px;}
.code-box{background:#f8f9fa;border:2px dashed #6366f1;border-radius:8px;text-align:center;padding:20px;margin:24px 0;}
.code{font-size:32px;font-weight:bold;letter-spacing:6px;color:#6366f1;font-family:monospace;}
.tip{color:#666;font-size:13px;line-height:1.6;}
.footer{text-align:center;padding:16px;color:#999;font-size:12px;}
</style></head><body>
<div class="container">
    <div class="header"><h1>DNSPlane</h1></div>
    <div class="content">
        <p>您正在进行<strong>%s</strong>操作，请使用以下验证码：</p>
        <div class="code-box"><div class="code">%s</div></div>
        <p class="tip">验证码有效期 %s 分钟，请勿将验证码告知他人。<br>如果这不是您本人的操作，请忽略此邮件。</p>
    </div>
    <div class="footer"><p>此邮件由系统自动发送，请勿直接回复</p></div>
</div>
</body></html>`, text, code, minutes)

	return subject, body
}
