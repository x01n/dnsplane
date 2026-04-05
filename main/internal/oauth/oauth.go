package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/database"
	"main/internal/models"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UserInfo 第三方用户标准化信息
type UserInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatar"`
}

// TokenResult OAuth2 Token 交换结果
type TokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	// 微信特有
	OpenID string `json:"openid"`
}

// ProviderConfig 提供商配置
type ProviderConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	// 自定义 OAuth2 额外字段
	AuthorizeURL string `json:"authorize_url"`
	TokenURL     string `json:"token_url"`
	UserInfoURL  string `json:"userinfo_url"`
	Scopes       string `json:"scopes"`
	DisplayName  string `json:"display_name"`
	// 微信/钉钉特有
	AppID     string `json:"app_id"`
	AppKey    string `json:"app_key"`
	AppSecret string `json:"app_secret"`
}

// Provider OAuth2 提供商接口
type Provider interface {
	Name() string                                                                      // 提供商标识 (github, google, wechat, dingtalk, custom)
	DisplayName() string                                                               // 显示名称
	Icon() string                                                                      // 图标名
	AuthorizeURL(state, redirectURI string) string                                     // 构建授权URL
	ExchangeToken(ctx context.Context, code, redirectURI string) (*TokenResult, error) // 用code换token
	GetUserInfo(ctx context.Context, token *TokenResult) (*UserInfo, error)            // 获取用户信息
}

// ProviderInfo 提供商公开信息（给前端用，不含密钥）
type ProviderInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Icon        string `json:"icon"`
	Enabled     bool   `json:"enabled"`
}

// 已注册的提供商工厂
var providerFactories = map[string]func(cfg ProviderConfig) Provider{
	"github":   NewGitHub,
	"google":   NewGoogle,
	"wechat":   NewWeChat,
	"dingtalk": NewDingTalk,
	"custom":   NewCustom,
}

// GetProvider 根据提供商名称和配置创建 Provider 实例
func GetProvider(name string) (Provider, error) {
	factory, ok := providerFactories[name]
	if !ok {
		return nil, fmt.Errorf("unknown oauth provider: %s", name)
	}

	// GitHub 有旧配置格式兼容
	var cfg ProviderConfig
	if name == "github" {
		cfg = LoadGitHubConfig()
	} else {
		cfg = LoadProviderConfig(name)
	}

	if cfg.ClientID == "" && cfg.AppID == "" && cfg.AppKey == "" {
		return nil, fmt.Errorf("oauth provider %s not configured", name)
	}

	return factory(cfg), nil
}

// LoadProviderConfig 从 SysConfig 加载提供商配置
func LoadProviderConfig(name string) ProviderConfig {
	prefix := "oauth_" + name + "_"
	var configs []models.SysConfig
	database.DB.Where("`key` LIKE ?", prefix+"%").Find(&configs)

	m := make(map[string]string)
	for _, c := range configs {
		field := strings.TrimPrefix(c.Key, prefix)
		m[field] = c.Value
	}

	return ProviderConfig{
		ClientID:     m["client_id"],
		ClientSecret: m["client_secret"],
		AuthorizeURL: m["authorize_url"],
		TokenURL:     m["token_url"],
		UserInfoURL:  m["userinfo_url"],
		Scopes:       m["scopes"],
		DisplayName:  m["name"],
		AppID:        m["app_id"],
		AppKey:       m["app_key"],
		AppSecret:    m["app_secret"],
	}
}

// GetEnabledProviders 获取所有已启用的提供商信息（供前端展示）
func GetEnabledProviders() []ProviderInfo {
	var result []ProviderInfo
	for name := range providerFactories {
		p, err := GetProvider(name)
		if err != nil {
			continue
		}
		result = append(result, ProviderInfo{
			Name:        p.Name(),
			DisplayName: p.DisplayName(),
			Icon:        p.Icon(),
			Enabled:     true,
		})
	}
	return result
}

// ==================== 通用 HTTP 工具 ====================

func httpPost(ctx context.Context, url string, data url.Values, acceptJSON bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if acceptJSON {
		req.Header.Set("Accept", "application/json")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func httpGetJSON(ctx context.Context, url string, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func parseTokenResponse(body []byte) (*TokenResult, error) {
	var result TokenResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if result.AccessToken == "" {
		return nil, fmt.Errorf("empty access_token in response: %s", string(body))
	}
	return &result, nil
}
