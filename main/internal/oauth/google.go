package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type google struct{ cfg ProviderConfig }

func NewGoogle(cfg ProviderConfig) Provider { return &google{cfg} }

func (g *google) Name() string        { return "google" }
func (g *google) DisplayName() string  { return "Google" }
func (g *google) Icon() string         { return "google" }

func (g *google) AuthorizeURL(state, redirectURI string) string {
	scopes := "openid email profile"
	return fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&access_type=offline",
		url.QueryEscape(g.cfg.ClientID), url.QueryEscape(redirectURI), url.QueryEscape(scopes), url.QueryEscape(state))
}

func (g *google) ExchangeToken(ctx context.Context, code, redirectURI string) (*TokenResult, error) {
	data := url.Values{
		"client_id":     {g.cfg.ClientID},
		"client_secret": {g.cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	body, err := httpPost(ctx, "https://oauth2.googleapis.com/token", data, true)
	if err != nil {
		return nil, err
	}
	return parseTokenResponse(body)
}

func (g *google) GetUserInfo(ctx context.Context, token *TokenResult) (*UserInfo, error) {
	body, err := httpGetJSON(ctx, "https://www.googleapis.com/oauth2/v2/userinfo", token.AccessToken)
	if err != nil {
		return nil, err
	}
	var u struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal(body, &u); err != nil || u.ID == "" {
		return nil, fmt.Errorf("parse google user: %w", err)
	}
	return &UserInfo{ID: u.ID, Name: u.Name, Email: u.Email, Avatar: u.Picture}, nil
}
