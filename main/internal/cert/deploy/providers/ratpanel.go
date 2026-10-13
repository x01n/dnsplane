package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
	base.Register("ratpanel", NewRatPanelProvider)
}

// RatPanelProvider 耗子面板部署器
type RatPanelProvider struct {
	base.BaseProvider
	panelURL string
	id       string
	token    string
	client   *http.Client
}

// NewRatPanelProvider 创建耗子面板部署器
func NewRatPanelProvider(config map[string]interface{}) base.DeployProvider {
	return &RatPanelProvider{
		BaseProvider: base.BaseProvider{Config: config},
		panelURL:     strings.TrimSuffix(base.GetConfigString(config, "url"), "/"),
		id:           base.GetConfigString(config, "id"),
		token:        base.GetConfigString(config, "token"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *RatPanelProvider) Check(ctx context.Context) error {
	if p.panelURL == "" || p.id == "" || p.token == "" {
		return fmt.Errorf("请填写完整面板地址和访问令牌")
	}

	resp, err := p.request(ctx, "GET", "/user/info", nil)
	if err != nil {
		return fmt.Errorf("面板地址无法连接: %v", err)
	}

	if msg, ok := resp["msg"].(string); ok && msg == "success" {
		return nil
	}
	return fmt.Errorf("面板连接失败")
}

func (p *RatPanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "" {
		deployType = p.GetString("type")
	}

	if deployType == "1" {
		return p.deployPanel(ctx, fullchain, privateKey)
	}

	sites := base.GetConfigString(config, "sites")
	if sites == "" {
		sites = p.GetString("sites")
	}

	siteList := strings.Split(sites, "\n")
	var success int
	var lastErr error

	for _, site := range siteList {
		site = strings.TrimSpace(site)
		if site == "" {
			continue
		}

		err := p.deploySite(ctx, site, fullchain, privateKey)
		if err != nil {
			lastErr = err
			p.Log(fmt.Sprintf("网站 %s 证书部署失败：%v", site, err))
		} else {
			p.Log(fmt.Sprintf("网站 %s 证书部署成功", site))
			success++
		}
	}

	if success == 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("要部署的网站不存在")
	}
	return nil
}

func (p *RatPanelProvider) deployPanel(ctx context.Context, fullchain, privateKey string) error {
	params := map[string]interface{}{
		"cert": fullchain,
		"key":  privateKey,
	}

	resp, err := p.request(ctx, "POST", "/setting/cert", params)
	if err != nil {
		return err
	}

	if msg, ok := resp["msg"].(string); ok && msg == "success" {
		p.Log("面板证书部署成功")
		return nil
	}

	if msg, ok := resp["msg"].(string); ok {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("返回数据解析失败")
}

func (p *RatPanelProvider) deploySite(ctx context.Context, name, fullchain, privateKey string) error {
	params := map[string]interface{}{
		"name": name,
		"cert": fullchain,
		"key":  privateKey,
	}

	resp, err := p.request(ctx, "POST", "/website/cert", params)
	if err != nil {
		return err
	}

	if msg, ok := resp["msg"].(string); ok && msg == "success" {
		return nil
	}

	if msg, ok := resp["msg"].(string); ok {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("返回数据解析失败")
}

func (p *RatPanelProvider) request(ctx context.Context, method, path string, params map[string]interface{}) (map[string]interface{}, error) {
	reqURL := p.panelURL + "/api" + path

	var bodyReader io.Reader
	var bodyStr string
	if params != nil {
		bodyBytes, _ := json.Marshal(params)
		bodyStr = string(bodyBytes)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	sign := p.signRequest(method, reqURL, bodyStr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", sign.timestamp))
	req.Header.Set("Authorization", fmt.Sprintf("HMAC-SHA256 Credential=%s, Signature=%s", sign.id, sign.signature))

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}
	return result, nil
}

type ratPanelSign struct {
	timestamp int64
	signature string
	id        string
}

func (p *RatPanelProvider) signRequest(method, reqURL, body string) ratPanelSign {
	parsedURL, _ := url.Parse(reqURL)
	path := parsedURL.Path
	query := parsedURL.RawQuery

	canonicalPath := path
	if !strings.HasPrefix(path, "/api") {
		if idx := strings.Index(path, "/api"); idx >= 0 {
			canonicalPath = path[idx:]
		}
	}

	bodyHash := sha256.Sum256([]byte(body))
	canonicalRequest := strings.Join([]string{
		method,
		canonicalPath,
		query,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")

	timestamp := time.Now().Unix()
	canonicalRequestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"HMAC-SHA256",
		fmt.Sprintf("%d", timestamp),
		hex.EncodeToString(canonicalRequestHash[:]),
	}, "\n")

	h := hmac.New(sha256.New, []byte(p.token))
	h.Write([]byte(stringToSign))
	signature := hex.EncodeToString(h.Sum(nil))

	return ratPanelSign{
		timestamp: timestamp,
		signature: signature,
		id:        p.id,
	}
}

func (p *RatPanelProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
