package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type wechat struct{ cfg ProviderConfig }

func NewWeChat(cfg ProviderConfig) Provider { return &wechat{cfg} }

func (w *wechat) Name() string        { return "wechat" }
func (w *wechat) DisplayName() string  { return "微信" }
func (w *wechat) Icon() string         { return "wechat" }

func (w *wechat) appID() string {
	if w.cfg.AppID != "" {
		return w.cfg.AppID
	}
	return w.cfg.ClientID
}

func (w *wechat) appSecret() string {
	if w.cfg.AppSecret != "" {
		return w.cfg.AppSecret
	}
	return w.cfg.ClientSecret
}

// 微信 OAuth2 使用 appid 而非 client_id
func (w *wechat) AuthorizeURL(state, redirectURI string) string {
	return fmt.Sprintf("https://open.weixin.qq.com/connect/qrconnect?appid=%s&redirect_uri=%s&response_type=code&scope=snsapi_login&state=%s#wechat_redirect",
		url.QueryEscape(w.appID()), url.QueryEscape(redirectURI), url.QueryEscape(state))
}

func (w *wechat) ExchangeToken(ctx context.Context, code, redirectURI string) (*TokenResult, error) {
	// 微信 token 接口用 GET，不是 POST
	tokenURL := fmt.Sprintf("https://api.weixin.qq.com/sns/oauth2/access_token?appid=%s&secret=%s&code=%s&grant_type=authorization_code",
		w.appID(), w.appSecret(), code)
	body, err := httpGetJSON(ctx, tokenURL, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		OpenID       string `json:"openid"`
		ErrCode      int    `json:"errcode"`
		ErrMsg       string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse wechat token: %w", err)
	}
	if resp.ErrCode != 0 {
		return nil, fmt.Errorf("wechat token error: %d %s", resp.ErrCode, resp.ErrMsg)
	}
	return &TokenResult{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresIn:    resp.ExpiresIn,
		OpenID:       resp.OpenID,
	}, nil
}

func (w *wechat) GetUserInfo(ctx context.Context, token *TokenResult) (*UserInfo, error) {
	infoURL := fmt.Sprintf("https://api.weixin.qq.com/sns/userinfo?access_token=%s&openid=%s&lang=zh_CN",
		token.AccessToken, token.OpenID)
	body, err := httpGetJSON(ctx, infoURL, "")
	if err != nil {
		return nil, err
	}
	var u struct {
		OpenID   string `json:"openid"`
		Nickname string `json:"nickname"`
		HeadImg  string `json:"headimgurl"`
		ErrCode  int    `json:"errcode"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("parse wechat user: %w", err)
	}
	if u.ErrCode != 0 || u.OpenID == "" {
		return nil, fmt.Errorf("wechat user error: %s", string(body))
	}
	return &UserInfo{ID: u.OpenID, Name: u.Nickname, Avatar: u.HeadImg}, nil
}
