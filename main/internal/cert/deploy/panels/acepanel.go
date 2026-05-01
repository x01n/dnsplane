package panels

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert/deploy/base"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() { base.Register("acepanel", NewAcePanelProvider) }

type AcePanelProvider struct { base.BaseProvider; client *http.Client }

func NewAcePanelProvider(config map[string]interface{}) base.DeployProvider {
	return &AcePanelProvider{BaseProvider: base.BaseProvider{Config: config}, client: &http.Client{Timeout: 60 * time.Second}}
}

func (p *AcePanelProvider) Check(ctx context.Context) error {
	_, err := p.request(ctx, "GET", "/user/info", nil)
	return err
}

func (p *AcePanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "1" {
		_, err := p.request(ctx, "POST", "/setting/cert", map[string]interface{}{"cert": fullchain, "key": privateKey})
		if err != nil { return err }
		p.Log("面板证书部署成功")
		return nil
	}
	sites := base.SplitDomains(base.GetConfigString(config, "sites"))
	if len(sites) == 0 { return fmt.Errorf("要部署的网站不存在") }
	success := 0
	var lastErr error
	for _, site := range sites {
		_, err := p.request(ctx, "POST", "/website/cert", map[string]interface{}{"name": site, "cert": fullchain, "key": privateKey})
		if err != nil {
			lastErr = err
			p.Log("网站 " + site + " 证书部署失败: " + err.Error())
			continue
		}
		p.Log("网站 " + site + " 证书部署成功")
		success++
	}
	if success == 0 && lastErr != nil { return lastErr }
	if success == 0 { return fmt.Errorf("要部署的网站不存在") }
	return nil
}

func (p *AcePanelProvider) request(ctx context.Context, method, path string, payload map[string]interface{}) (map[string]interface{}, error) {
	baseURL := strings.TrimSuffix(p.GetString("url"), "/")
	id := p.GetString("id")
	token := p.GetString("token")
	if baseURL == "" || id == "" || token == "" { return nil, fmt.Errorf("acepanel: 请填写完整面板地址和访问令牌") }
	fullURL := baseURL + "/api" + path
	bodyBytes := []byte{}
	if method != "GET" && payload != nil { bodyBytes, _ = json.Marshal(payload) }
	ts := strconvFormat(time.Now().Unix())
	parsed, _ := url.Parse(fullURL)
	canonicalRequest := strings.Join([]string{method, parsed.Path, parsed.RawQuery, sha256Hex(bodyBytes)}, "\n")
	stringToSign := strings.Join([]string{"HMAC-SHA256", ts, sha256Hex([]byte(canonicalRequest))}, "\n")
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(stringToSign))
	signature := hex.EncodeToString(mac.Sum(nil))
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil { return nil, err }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("Authorization", "HMAC-SHA256 Credential="+id+", Signature="+signature)
	resp, err := p.client.Do(req)
	if err != nil { return nil, fmt.Errorf("acepanel: %w", err) }
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil { return nil, fmt.Errorf("acepanel: %s", string(respBody)) }
	if msg, ok := result["msg"].(string); ok && msg != "success" {
		return nil, fmt.Errorf("acepanel: %s", msg)
	}
	return result, nil
}

func sha256Hex(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func strconvFormat(v int64) string { return fmt.Sprintf("%d", v) }
