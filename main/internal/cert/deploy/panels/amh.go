package panels

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"main/internal/cert/deploy/base"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func init() { base.Register("amh", NewAMHProvider) }

type AMHProvider struct { base.BaseProvider; client *http.Client }

func NewAMHProvider(config map[string]interface{}) base.DeployProvider {
	return &AMHProvider{BaseProvider: base.BaseProvider{Config: config}, client: &http.Client{Timeout: 60 * time.Second}}
}

func (p *AMHProvider) Check(ctx context.Context) error { _, err := p.login(ctx); return err }

func (p *AMHProvider) login(ctx context.Context) (string, error) {
	baseURL := strings.TrimSuffix(p.GetString("url"), "/")
	apiKey := p.GetString("apikey")
	if baseURL == "" || apiKey == "" { return "", fmt.Errorf("amh: 请填写面板地址和接口密钥") }
	expires := time.Now().Unix() + 120
	postData := fmt.Sprintf("amapi_expires=%d", expires)
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(postData))
	postData += "&amapi_sign=" + fmt.Sprintf("%x", mac.Sum(nil))
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/?c=amapi&a=login", strings.NewReader(postData))
	if err != nil { return "", err }
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "PHPSESSID="+phpSessionID(apiKey))
	resp, err := p.client.Do(req)
	if err != nil { return "", fmt.Errorf("amh: %w", err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 302 || resp.StatusCode == 301 {
		loc := resp.Header.Get("Location")
		re := regexp.MustCompile(`amh_token=([A-Za-z0-9]+)`)
		m := re.FindStringSubmatch(loc)
		if len(m) == 2 { return m[1], nil }
	}
	if m := regexp.MustCompile(`<p id="error".*?>(.*?)</p>`).FindSubmatch(body); len(m) == 2 { return "", fmt.Errorf("amh: %s", stripHTML(string(m[1]))) }
	return "", fmt.Errorf("amh: 面板地址无法连接")
}

func (p *AMHProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	envName := base.GetConfigString(config, "env_name")
	vhostName := base.GetConfigString(config, "vhost_name")
	if envName == "" { return fmt.Errorf("amh: 环境名称不能为空") }
	if vhostName == "" { return fmt.Errorf("amh: 网站标识域名不能为空") }
	token, err := p.login(ctx)
	if err != nil { return err }
	for _, host := range base.SplitDomains(vhostName) {
		path := "/?c=amssl&a=admin_amssl&envs_name=" + url.QueryEscape(envName) + "&vhost_name=" + url.QueryEscape(host) + "&ModuleSort=app"
		form := url.Values{}
		form.Set("submit_key_crt", "y")
		form.Set("key_input1", "key_input1")
		form.Set("key_content1", privateKey)
		form.Set("crt_input1", "crt_input1")
		form.Set("crt_content1", fullchain)
		form.Set("amh_token", token)
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(p.GetString("url"), "/")+path, strings.NewReader(form.Encode()))
		if err != nil { return err }
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Cookie", "PHPSESSID="+phpSessionID(p.GetString("apikey")))
		resp, err := p.client.Do(req)
		if err != nil { return fmt.Errorf("amh: %w", err) }
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), `<p id="success"`) {
			p.Log("网站 " + host + " 证书部署成功")
			continue
		}
		if m := regexp.MustCompile(`<p id="error".*?>(.*?)</p>`).FindSubmatch(body); len(m) == 2 {
			err = fmt.Errorf("amh: %s", stripHTML(string(m[1])))
			p.Log("网站 " + host + " 证书部署失败: " + err.Error())
			return err
		}
		return fmt.Errorf("amh: 网站 %s 证书部署失败：未知错误", host)
	}
	return nil
}

func phpSessionID(apiKey string) string { mac := hmac.New(sha256.New, []byte(apiKey)); mac.Write([]byte("php_sessid=" + apiKey)); return fmt.Sprintf("%x", mac.Sum(nil))[:32] }
func stripHTML(s string) string { return strings.NewReplacer("<br />", " ", "<br/>", " ", "<br>", " ").Replace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")) }
