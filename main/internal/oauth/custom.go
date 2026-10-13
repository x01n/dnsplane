package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type custom struct{ cfg ProviderConfig }

func NewCustom(cfg ProviderConfig) Provider { return &custom{cfg} }

func (c *custom) Name() string { return "custom" }
func (c *custom) DisplayName() string {
	if c.cfg.DisplayName != "" {
		return c.cfg.DisplayName
	}
	return "OAuth2 登录"
}
func (c *custom) Icon() string { return "key" }

func (c *custom) AuthorizeURL(state, redirectURI string) string {
	scopes := c.cfg.Scopes
	if scopes == "" {
		scopes = "openid profile email"
	}
	return fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		c.cfg.AuthorizeURL,
		url.QueryEscape(c.cfg.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(scopes),
		url.QueryEscape(state))
}

func (c *custom) ExchangeToken(ctx context.Context, code, redirectURI string) (*TokenResult, error) {
	if c.cfg.TokenURL == "" {
		return nil, fmt.Errorf("custom oauth: token_url not configured")
	}
	data := url.Values{
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	body, err := httpPost(ctx, c.cfg.TokenURL, data, true)
	if err != nil {
		return nil, err
	}
	return parseTokenResponse(body)
}

func (c *custom) GetUserInfo(ctx context.Context, token *TokenResult) (*UserInfo, error) {
	if c.cfg.UserInfoURL == "" {
		return nil, fmt.Errorf("custom oauth: userinfo_url not configured")
	}
	body, err := httpGetJSON(ctx, c.cfg.UserInfoURL, token.AccessToken)
	if err != nil {
		return nil, err
	}

	// 尝试解析标准 OpenID Connect 格式
	var u struct {
		Sub     string `json:"sub"`
		ID      string `json:"id"`
		Name    string `json:"name"`
		Login   string `json:"login"`
		Email   string `json:"email"`
		Picture string `json:"picture"`
		Avatar  string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("parse custom user: %w", err)
	}

	uid := u.Sub
	if uid == "" {
		uid = u.ID
	}
	if uid == "" {
		return nil, fmt.Errorf("custom oauth: no user id in response: %s", string(body))
	}

	name := u.Name
	if name == "" {
		name = u.Login
	}
	avatar := u.Picture
	if avatar == "" {
		avatar = u.Avatar
	}

	return &UserInfo{ID: uid, Name: name, Email: u.Email, Avatar: avatar}, nil
}
