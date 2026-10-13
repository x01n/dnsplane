package providers

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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
	base.Register("kangle", NewKangleProvider)
}

// KangleProvider Kangle用户面板部署器
type KangleProvider struct {
	base.BaseProvider
}

// NewKangleProvider 创建Kangle用户面板部署器
func NewKangleProvider(config map[string]interface{}) base.DeployProvider {
	return &KangleProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *KangleProvider) Check(ctx context.Context) error {
	panelURL := strings.TrimRight(p.GetString("url"), "/")
	username := p.GetString("username")
	authType := p.GetString("auth")

	if authType == "1" {
		skey := p.GetString("skey")
		return p.checkWithSkey(panelURL, username, skey)
	}

	password := p.GetString("password")
	return p.checkWithPassword(panelURL, username, password)
}

func (p *KangleProvider) checkWithPassword(panelURL, username, password string) error {
	client := &http.Client{Timeout: 30 * time.Second}

	loginURL := fmt.Sprintf("%s/vhost/index.php?c=login", panelURL)
	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)

	resp, err := client.PostForm(loginURL, data)
	if err != nil {
		return fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "success") && !strings.Contains(string(body), "1") {
		return fmt.Errorf("登录失败: %s", string(body))
	}
	return nil
}

func (p *KangleProvider) checkWithSkey(panelURL, username, skey string) error {
	client := &http.Client{Timeout: 30 * time.Second}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sign := kangleMakeSign(username, timestamp, skey)

	checkURL := fmt.Sprintf("%s/vhost/index.php?c=api&a=get_user_info&username=%s&timestamp=%s&sign=%s",
		panelURL, username, timestamp, sign)

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

func (p *KangleProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	panelURL := strings.TrimRight(p.GetString("url"), "/")
	username := p.GetString("username")
	authType := p.GetString("auth")
	deployType := p.GetStringFrom(config, "type")

	p.Log(fmt.Sprintf("开始部署到Kangle用户面板: %s", panelURL))

	client := &http.Client{Timeout: 60 * time.Second}

	if deployType == "1" {
		domains := strings.Split(p.GetStringFrom(config, "domains"), "\n")
		for _, domain := range domains {
			domain = strings.TrimSpace(domain)
			if domain == "" {
				continue
			}
			p.Log(fmt.Sprintf("正在部署域名: %s", domain))
			if err := p.deployCDNCert(client, panelURL, username, authType, domain, fullchain, privateKey); err != nil {
				return err
			}
		}
	} else {
		p.Log("正在部署网站SSL证书")
		if err := p.deploySiteCert(client, panelURL, username, authType, fullchain, privateKey); err != nil {
			return err
		}
	}

	p.Log("Kangle部署完成")
	return nil
}

func (p *KangleProvider) deploySiteCert(client *http.Client, panelURL, username, authType, fullchain, privateKey string) error {
	var apiURL string
	data := url.Values{}
	data.Set("cert", fullchain)
	data.Set("key", privateKey)

	if authType == "1" {
		skey := p.GetString("skey")
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		sign := kangleMakeSign(username, timestamp, skey)
		apiURL = fmt.Sprintf("%s/vhost/index.php?c=api&a=set_ssl&username=%s&timestamp=%s&sign=%s",
			panelURL, username, timestamp, sign)
	} else {
		apiURL = fmt.Sprintf("%s/vhost/index.php?c=ssl&a=save", panelURL)
	}

	resp, err := client.PostForm(apiURL, data)
	if err != nil {
		return fmt.Errorf("部署请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("部署失败: %s", string(body))
	}
	return nil
}

func (p *KangleProvider) deployCDNCert(client *http.Client, panelURL, username, authType, domain, fullchain, privateKey string) error {
	var apiURL string
	data := url.Values{}
	data.Set("domain", domain)
	data.Set("cert", fullchain)
	data.Set("key", privateKey)

	if authType == "1" {
		skey := p.GetString("skey")
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		sign := kangleMakeSign(username, timestamp, skey)
		apiURL = fmt.Sprintf("%s/vhost/index.php?c=api&a=set_cdn_ssl&username=%s&timestamp=%s&sign=%s",
			panelURL, username, timestamp, sign)
	} else {
		apiURL = fmt.Sprintf("%s/vhost/index.php?c=cdn&a=save_ssl", panelURL)
	}

	resp, err := client.PostForm(apiURL, data)
	if err != nil {
		return fmt.Errorf("部署请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("部署失败: %s", string(body))
	}
	return nil
}

// kangleMakeSign 生成Kangle签名
func kangleMakeSign(username, timestamp, skey string) string {
	hash := md5.Sum([]byte(username + timestamp + skey))
	return hex.EncodeToString(hash[:])
}

func (p *KangleProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
