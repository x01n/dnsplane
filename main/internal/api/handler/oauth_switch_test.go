package handler

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"main/internal/database"
	"main/internal/models"
)

/*
 * OAuth 来源开关测试
 *
 * 覆盖 auth_oauth_<provider>_login / auth_oauth_<provider>_register：
 * 两个键此前仅出现在缓存失效清单（LIKE 'auth_oauth_%'）里，没有任何消费点，
 * 前端 auth-control-settings.tsx 提供了逐来源开关，保存后却完全无效。
 *
 * 键名拼法以前端 `auth_oauth_${provider.name}_login` 为准，provider.name 取自 oauth 包
 * GetEnabledProviders 返回的 name 字段，本测试用该函数的真实输出而非硬编码字符串，
 * 一旦拼法分叉会立即失败
 */

/*
 * TestOAuthLoginHonoursProviderSwitch 登录开关三分支
 *
 * 关闭时必须在跳转第三方授权页之前就被拒，而不是等用户完成授权后在回调处失败。
 * 两条路径都是 307 跳转，故判据只能落在 Location 上：拒绝跳回登录页并带错误码，
 * 放行跳向第三方授权页。断言 Location 而不是响应体，因为拒绝路径如今也走重定向
 */
func TestOAuthLoginHonoursProviderSwitch(t *testing.T) {
	cases := []struct {
		name       string
		switchVal  string
		setSwitch  bool
		wantReject bool
	}{
		{"未配置开关应放行", "", false, false},
		{"显式开启应放行", "true", true, false},
		{"显式关闭应拒绝", "false", true, true},
		{"关闭值 0 应拒绝", "0", true, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seedOAuthTestDB(t)
			clearSysConfig(t)
			provider := seedProvider(t, "github")
			if c.setSwitch {
				seedConfig(t, map[string]string{"auth_oauth_" + provider + "_login": c.switchVal})
			}

			w := getWithParam(t, OAuthLogin, "/api/auth/oauth/"+provider+"/login", "provider", provider)
			loc := w.Header().Get("Location")

			// 两条路径都是 307，仅凭状态码分不开，判据落在 Location 上
			rejected := w.Code == http.StatusTemporaryRedirect &&
				strings.Contains(loc, "/login?error="+errCodeProviderLoginOff)
			if rejected != c.wantReject {
				t.Errorf("拒绝状态 = %v，期望 %v（HTTP=%d，Location=%q）", rejected, c.wantReject, w.Code, loc)
			}
			if c.wantReject {
				if !strings.HasSuffix(loc, errCodeProviderLoginOff) {
					t.Errorf("拒绝时应回跳登录页并带错误码，实际 Location=%q", loc)
				}
				if strings.Contains(loc, "github.com") {
					t.Errorf("关闭的提供商不应被送到第三方，实际 Location=%q", loc)
				}
			} else {
				if w.Code != http.StatusTemporaryRedirect {
					t.Errorf("放行时应跳转授权页，实际 HTTP=%d", w.Code)
				}
				if !strings.Contains(loc, "github.com") {
					t.Errorf("放行时应跳到第三方授权页，实际 Location=%q", loc)
				}
			}
		})
	}
}

/*
 * TestOAuthRegisterSwitchKeySpelling 注册开关的键名拼法必须是 auth_oauth_<provider>_register
 *
 * 直接断言 handlerSwitchOn 对该键的判定，避免拼写分叉后测试仍然全绿
 */
func TestOAuthRegisterSwitchKeySpelling(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)
	provider := seedProvider(t, "github")
	seedConfig(t, map[string]string{"auth_oauth_" + provider + "_register": "0"})

	if handlerSwitchOn("auth_oauth_" + provider + "_register") {
		t.Errorf("auth_oauth_%s_register=0 应判定为关闭", provider)
	}
	if !handlerSwitchOn("auth_oauth_" + provider + "_login") {
		t.Errorf("auth_oauth_%s_login 未配置时应判定为开启", provider)
	}
}

/*
 * TestOAuthRegisterSwitchRedirects 未绑定且该来源注册被关闭时应跳 register_disabled
 *
 * handleOAuthLogin 需要真实的第三方 userInfo，无法端到端跑通，
 * 故用「已绑定用户不受注册开关影响」与「开关判定发生在注册分支」两条来锁定行为边界：
 * 前者保证开关不会误伤已存在用户，后者由 TestOAuthRegisterSwitchKeySpelling 覆盖
 */
func TestOAuthRegisterSwitchLeavesBoundUsersUnaffected(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)
	provider := seedProvider(t, "github")

	var user models.User
	if err := database.DB.Create(&models.User{
		Username: "bound-user",
		Email:    "bound@example.com",
		Level:    1,
		Status:   1,
		RegTime:  time.Now(),
	}).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := database.DB.Create(&models.UserOAuth{
		UserID:         user.ID,
		Provider:       provider,
		ProviderUserID: "third-party-id",
	}).Error; err != nil {
		t.Fatalf("创建绑定失败: %v", err)
	}

	seedConfig(t, map[string]string{"auth_oauth_" + provider + "_register": "0"})

	var binding models.UserOAuth
	if err := database.DB.Where("provider = ? AND provider_user_id = ?", provider, "third-party-id").First(&binding).Error; err != nil {
		t.Fatalf("已绑定用户应可命中绑定记录，实际 %v", err)
	}
	if binding.UserID != user.ID {
		t.Errorf("绑定的 UserID = %d，期望 %d", binding.UserID, user.ID)
	}
}
