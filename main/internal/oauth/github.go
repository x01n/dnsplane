package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"main/internal/database"
	"main/internal/models"
	"net/url"
)

type gitHub struct{ cfg ProviderConfig }

func NewGitHub(cfg ProviderConfig) Provider { return &gitHub{cfg} }

func (g *gitHub) Name() string        { return "github" }
func (g *gitHub) DisplayName() string { return "GitHub" }
func (g *gitHub) Icon() string        { return "github" }

func (g *gitHub) AuthorizeURL(state, redirectURI string) string {
	return fmt.Sprintf("https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=user:email&state=%s",
		url.QueryEscape(g.cfg.ClientID), url.QueryEscape(redirectURI), url.QueryEscape(state))
}

func (g *gitHub) ExchangeToken(ctx context.Context, code, redirectURI string) (*TokenResult, error) {
	data := url.Values{
		"client_id":     {g.cfg.ClientID},
		"client_secret": {g.cfg.ClientSecret},
		"code":          {code},
	}
	body, err := httpPost(ctx, "https://github.com/login/oauth/access_token", data, true)
	if err != nil {
		return nil, err
	}
	return parseTokenResponse(body)
}

func (g *gitHub) GetUserInfo(ctx context.Context, token *TokenResult) (*UserInfo, error) {
	body, err := httpGetJSON(ctx, "https://api.github.com/user", token.AccessToken)
	if err != nil {
		return nil, err
	}
	var u struct {
		ID     int64  `json:"id"`
		Login  string `json:"login"`
		Email  string `json:"email"`
		Avatar string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &u); err != nil || u.ID == 0 {
		return nil, fmt.Errorf("parse github user: %w (body: %s)", err, string(body))
	}
	return &UserInfo{
		ID:     fmt.Sprintf("%d", u.ID),
		Name:   u.Login,
		Email:  u.Email,
		Avatar: u.Avatar,
	}, nil
}

// LoadGitHubConfig 加载 GitHub 配置（兼容旧的 github_* 和新的 oauth_github_* 两种 key 格式）
func LoadGitHubConfig() ProviderConfig {
	// 先尝试新格式 oauth_github_*
	cfg := LoadProviderConfig("github")
	if cfg.ClientID != "" || cfg.AppID != "" {
		return cfg
	}

	// 回退到旧格式 github_*
	var configs []models.SysConfig
	database.DB.Where("`key` IN ?", []string{
		"github_mode", "github_client_id", "github_client_secret",
		"github_app_id", "github_app_private_key",
	}).Find(&configs)

	m := make(map[string]string)
	for _, c := range configs {
		m[c.Key] = c.Value
	}

	// 不管 OAuth App 还是 GitHub App 模式，登录都走 OAuth Web Flow
	// 都需要 Client ID + Client Secret（GitHub App 设置页面也有这两个字段）
	return ProviderConfig{
		ClientID:     m["github_client_id"],
		ClientSecret: m["github_client_secret"],
		// App 模式额外保存 App ID（用于 API，登录不用）
		AppID:     m["github_app_id"],
		AppSecret: m["github_app_private_key"],
	}
}
