package providers

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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
	base.Register("xp", NewXPProvider)
}

// XPProvider 小皮面板部署器
type XPProvider struct {
	base.BaseProvider
}

// NewXPProvider 创建小皮面板部署器
func NewXPProvider(config map[string]interface{}) base.DeployProvider {
	return &XPProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *XPProvider) Check(ctx context.Context) error {
	panelURL := strings.TrimRight(p.GetString("url"), "/")
	apiKey := p.GetString("apikey")

	client := &http.Client{Timeout: 30 * time.Second}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sign := xpMakeSign(timestamp, apiKey)

	checkURL := fmt.Sprintf("%s/api/site/list?timestamp=%s&sign=%s", panelURL, timestamp, sign)

	resp, err := client.Get(checkURL)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	if code, ok := result["code"].(float64); ok && code != 0 {
		return fmt.Errorf("API错误: %v", result["msg"])
	}
	return nil
}

func (p *XPProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	panelURL := strings.TrimRight(p.GetString("url"), "/")
	apiKey := p.GetString("apikey")
	sites := strings.Split(p.GetStringFrom(config, "sites"), "\n")

	p.Log(fmt.Sprintf("开始部署到小皮面板: %s", panelURL))

	client := &http.Client{Timeout: 60 * time.Second}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sign := xpMakeSign(timestamp, apiKey)

	for _, site := range sites {
		site = strings.TrimSpace(site)
		if site == "" {
			continue
		}

		p.Log(fmt.Sprintf("正在部署网站: %s", site))

		apiURL := fmt.Sprintf("%s/api/site/ssl?timestamp=%s&sign=%s", panelURL, timestamp, sign)

		data := url.Values{}
		data.Set("site", site)
		data.Set("cert", fullchain)
		data.Set("key", privateKey)

		resp, err := client.PostForm(apiURL, data)
		if err != nil {
			return fmt.Errorf("部署请求失败: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("响应解析失败: %w", err)
		}

		if code, ok := result["code"].(float64); ok && code != 0 {
			return fmt.Errorf("部署失败: %v", result["msg"])
		}

		p.Log(fmt.Sprintf("网站 %s 部署成功", site))
	}

	p.Log("小皮面板部署完成")
	return nil
}

func xpMakeSign(timestamp, apiKey string) string {
	hash := md5.Sum([]byte(timestamp + apiKey))
	return hex.EncodeToString(hash[:])
}

func (p *XPProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
