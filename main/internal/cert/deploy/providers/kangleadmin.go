package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("kangleadmin", NewKangleAdminProvider)
}

// KangleAdminProvider Kangle管理员面板部署器
type KangleAdminProvider struct {
	base.BaseProvider
}

// NewKangleAdminProvider 创建Kangle管理员部署器
func NewKangleAdminProvider(config map[string]interface{}) base.DeployProvider {
	return &KangleAdminProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *KangleAdminProvider) Check(ctx context.Context) error {
	panelURL := strings.TrimRight(p.GetString("url"), "/")
	adminPath := p.GetString("path")
	if adminPath == "" {
		adminPath = "/admin"
	}
	username := p.GetString("username")
	skey := p.GetString("skey")

	client := &http.Client{Timeout: 30 * time.Second}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sign := kangleMakeSign(username, timestamp, skey)

	checkURL := fmt.Sprintf("%s%s/index.php?c=api&a=get_user_list&username=%s&timestamp=%s&sign=%s",
		panelURL, adminPath, username, timestamp, sign)

	resp, err := client.Get(checkURL)
	if err != nil {
		return fmt.Errorf("检查请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("检查失败: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (p *KangleAdminProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	panelURL := strings.TrimRight(p.GetString("url"), "/")
	adminPath := p.GetString("path")
	if adminPath == "" {
		adminPath = "/admin"
	}
	adminUser := p.GetString("username")
	skey := p.GetString("skey")
	targetUser := p.GetStringFrom(config, "name")
	deployType := p.GetStringFrom(config, "type")

	p.Log(fmt.Sprintf("开始部署到Kangle管理员面板: %s", panelURL))

	client := &http.Client{Timeout: 60 * time.Second}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sign := kangleMakeSign(adminUser, timestamp, skey)

	if deployType == "1" {
		domains := strings.Split(p.GetStringFrom(config, "domains"), "\n")
		for _, domain := range domains {
			domain = strings.TrimSpace(domain)
			if domain == "" {
				continue
			}
			p.Log(fmt.Sprintf("正在部署域名: %s", domain))

			apiURL := fmt.Sprintf("%s%s/index.php?c=api&a=set_cdn_ssl&username=%s&timestamp=%s&sign=%s",
				panelURL, adminPath, adminUser, timestamp, sign)

			data := url.Values{}
			data.Set("user", targetUser)
			data.Set("domain", domain)
			data.Set("cert", fullchain)
			data.Set("key", privateKey)

			resp, err := client.PostForm(apiURL, data)
			if err != nil {
				return fmt.Errorf("部署请求失败: %w", err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				return fmt.Errorf("部署域名 %s 失败: HTTP %d", domain, resp.StatusCode)
			}
		}
	} else {
		p.Log(fmt.Sprintf("正在部署用户 %s 的SSL证书", targetUser))

		apiURL := fmt.Sprintf("%s%s/index.php?c=api&a=set_ssl&username=%s&timestamp=%s&sign=%s",
			panelURL, adminPath, adminUser, timestamp, sign)

		data := url.Values{}
		data.Set("user", targetUser)
		data.Set("cert", fullchain)
		data.Set("key", privateKey)

		resp, err := client.PostForm(apiURL, data)
		if err != nil {
			return fmt.Errorf("部署请求失败: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err == nil {
			if code, ok := result["code"].(float64); ok && code != 0 {
				return fmt.Errorf("部署失败: %v", result["msg"])
			}
		}
	}

	p.Log("Kangle管理员部署完成")
	return nil
}

func (p *KangleAdminProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
