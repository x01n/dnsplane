package others

import (
	"main/internal/cert/deploy/base"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"main/internal/cert"
)

func init() {
	base.Register("west", NewWestProvider)
}

// WestProvider 西部数码部署器
type WestProvider struct {
	base.BaseProvider
}

func NewWestProvider(config map[string]interface{}) base.DeployProvider {
	return &WestProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *WestProvider) Check(ctx context.Context) error {
	username := p.GetString("username")
	apiPassword := p.GetString("api_password")

	client := p.getHTTPClient()

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sign := p.makeSign(username, timestamp, apiPassword)

	checkURL := fmt.Sprintf("https://api.west.cn/api/v2/host/?act=getlist&username=%s&time=%s&token=%s",
		username, timestamp, sign)

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

	if code, ok := result["result"].(float64); ok && code != 200 {
		return fmt.Errorf("API错误: %v", result["msg"])
	}

	return nil
}

func (p *WestProvider) makeSign(username, timestamp, apiPassword string) string {
	str := username + apiPassword + timestamp
	hash := md5.Sum([]byte(str))
	return hex.EncodeToString(hash[:])
}

func (p *WestProvider) getHTTPClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

func (p *WestProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	username := p.GetString("username")
	apiPassword := p.GetString("api_password")
	sitename := p.GetStringFrom(config, "sitename")

	p.Log(fmt.Sprintf("开始部署到西部数码: %s", sitename))

	client := p.getHTTPClient()
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sign := p.makeSign(username, timestamp, apiPassword)

	apiURL := "https://api.west.cn/api/v2/host/"

	data := url.Values{}
	data.Set("act", "setssl")
	data.Set("username", username)
	data.Set("time", timestamp)
	data.Set("token", sign)
	data.Set("sitename", sitename)
	data.Set("cert", fullchain)
	data.Set("key", privateKey)

	resp, err := client.PostForm(apiURL, data)
	if err != nil {
		return fmt.Errorf("部署请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	if code, ok := result["result"].(float64); ok && code != 200 {
		return fmt.Errorf("部署失败: %v", result["msg"])
	}

	p.Log("西部数码部署完成")
	return nil
}

func (p *WestProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
