package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type dingTalk struct{ cfg ProviderConfig }

func NewDingTalk(cfg ProviderConfig) Provider { return &dingTalk{cfg} }

func (d *dingTalk) Name() string        { return "dingtalk" }
func (d *dingTalk) DisplayName() string  { return "钉钉" }
func (d *dingTalk) Icon() string         { return "dingtalk" }

func (d *dingTalk) appKey() string {
	if d.cfg.AppKey != "" {
		return d.cfg.AppKey
	}
	return d.cfg.ClientID
}

func (d *dingTalk) appSecret() string {
	if d.cfg.AppSecret != "" {
		return d.cfg.AppSecret
	}
	return d.cfg.ClientSecret
}

// 钉钉新版 OAuth2 (SNS扫码登录)
func (d *dingTalk) AuthorizeURL(state, redirectURI string) string {
	return fmt.Sprintf("https://login.dingtalk.com/oauth2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid%%20corpid&state=%s&prompt=consent",
		url.QueryEscape(d.appKey()), url.QueryEscape(redirectURI), url.QueryEscape(state))
}

func (d *dingTalk) ExchangeToken(ctx context.Context, code, redirectURI string) (*TokenResult, error) {
	// 钉钉新版 OAuth2 用 JSON body POST
	body := map[string]string{
		"clientId":     d.appKey(),
		"clientSecret": d.appSecret(),
		"code":         code,
		"grantType":    "authorization_code",
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.dingtalk.com/v1.0/oauth2/userAccessToken", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expireIn"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.AccessToken == "" {
		return nil, fmt.Errorf("dingtalk token error: %s", string(respBody))
	}
	return &TokenResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	}, nil
}

func (d *dingTalk) GetUserInfo(ctx context.Context, token *TokenResult) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.dingtalk.com/v1.0/contact/users/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-acs-dingtalk-access-token", token.AccessToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var u struct {
		OpenID    string `json:"openId"`
		Nick      string `json:"nick"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatarUrl"`
	}
	if err := json.Unmarshal(body, &u); err != nil || u.OpenID == "" {
		return nil, fmt.Errorf("dingtalk user error: %s", string(body))
	}
	return &UserInfo{ID: u.OpenID, Name: u.Nick, Email: u.Email, Avatar: u.AvatarURL}, nil
}
