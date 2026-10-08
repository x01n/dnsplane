package providers

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("cloudpanel", NewCloudPanelProvider)
}

var (
	cpSiteLinkPattern = regexp.MustCompile(`(?i)href="/site/([^"/]+)"`)
	cpAlertPattern = regexp.MustCompile(`(?is)class="alert alert-(?:danger|warning|success)"[^>]*>(.*?)</div>`)
	cpTagPattern = regexp.MustCompile(`(?s)<[^>]+>`)
	cpSpacePattern = regexp.MustCompile(`\s+`)
	cpSiteSplitPattern = regexp.MustCompile(`\r\n|\r|\n|,`)
)

/* CloudPanelProvider CloudPanel 证书部署，登录方式为表单登录加 CSRF 与 Cookie 会话 */
type CloudPanelProvider struct {
	base.BaseProvider
	baseURL  string
	username string
	password string
	cookie   string
	client   *http.Client
}

/*
 * NewCloudPanelProvider 创建 CloudPanel 部署器
 * http_request 固定 verify=false
 */
func NewCloudPanelProvider(config map[string]interface{}) base.DeployProvider {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext:     (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
	}
	return &CloudPanelProvider{
		BaseProvider: base.BaseProvider{Config: config},
		baseURL:      strings.TrimRight(base.GetConfigString(config, "url"), "/"),
		username:     base.GetConfigString(config, "username"),
		password:     base.GetConfigString(config, "password"),
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (p *CloudPanelProvider) Check(ctx context.Context) error {
	if err := p.assertAccountConfig(); err != nil {
		return err
	}
	return p.login(ctx)
}

func (p *CloudPanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	if err := p.assertAccountConfig(); err != nil {
		return err
	}
	if strings.TrimSpace(fullchain) == "" || strings.TrimSpace(privateKey) == "" {
		return fmt.Errorf("SSL 证书或私钥内容不能为空")
	}

	targets := cloudPanelParseSites(base.GetConfigString(config, "sites"))
	if len(targets) == 0 {
		return fmt.Errorf("没有设置要部署的网站域名")
	}

	if err := p.login(ctx); err != nil {
		return err
	}

	success := 0
	lastErr := ""
	for _, domain := range targets {
		if err := p.deploySite(ctx, domain, fullchain, privateKey); err != nil {
			lastErr = err.Error()
			p.Log("网站 " + domain + " 证书部署失败：" + lastErr)
			continue
		}
		p.Log("网站 " + domain + " 证书部署成功")
		success++
	}
	if success == 0 {
		if lastErr != "" {
			return errors.New(lastErr)
		}
		return fmt.Errorf("要部署的网站不存在")
	}
	return nil
}

func (p *CloudPanelProvider) assertAccountConfig() error {
	if p.baseURL == "" || p.username == "" || p.password == "" {
		return fmt.Errorf("请填写面板地址、用户名和密码")
	}
	parsed, err := url.Parse(p.baseURL)
	if err != nil {
		return fmt.Errorf("CloudPanel 面板地址格式无效")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("CloudPanel 面板地址格式无效")
	}
	return nil
}

func (p *CloudPanelProvider) login(ctx context.Context) error {
	page, err := p.request(ctx, "GET", "/login", nil, 15*time.Second, "")
	if err != nil {
		return err
	}
	if page.code != 200 {
		return fmt.Errorf("打开 CloudPanel 登录页失败(httpCode=%d)", page.code)
	}

	csrf := cloudPanelExtractValue(page.body, "_csrf_token", false)
	if csrf == "" {
		return fmt.Errorf("获取登录 CSRF 令牌失败")
	}
	form := orderedForm{
		{"userName", p.username},
		{"password", p.password},
		{"_csrf_token", csrf},
	}
	response, err := p.request(ctx, "POST", "/login", form, 20*time.Second, p.baseURL+"/login")
	if err != nil {
		return err
	}

	location := p.absoluteURL(response.redirectURL)
	if p.isTwoFactorLocation(location) || cloudPanelContainsTwoFactor(response.body) {
		return fmt.Errorf("当前 CloudPanel 账户启用了双因素认证，暂不支持")
	}

	if response.code >= 300 && response.code < 400 {
		if p.cookie == "" {
			return fmt.Errorf("CloudPanel 登录失败：未获取到登录会话")
		}
		path := cloudPanelPathFromURL(location)
		if path == "" {
			path = "/"
		}
		home, err := p.request(ctx, "GET", path, nil, 15*time.Second, "")
		if err != nil {
			return err
		}
		if cloudPanelIsLoginPage(home.body, home.redirectURL, p.baseURL) {
			return fmt.Errorf("CloudPanel 登录失败，请检查用户名和密码")
		}
		return nil
	}

	alerts := cloudPanelParseAlerts(response.body)
	if len(alerts) > 0 {
		return fmt.Errorf("CloudPanel 登录失败：%s", alerts[0])
	}
	if cloudPanelIsLoginPage(response.body, response.redirectURL, p.baseURL) {
		return fmt.Errorf("CloudPanel 登录失败，请检查用户名和密码")
	}
	return fmt.Errorf("CloudPanel 登录失败(httpCode=%d)", response.code)
}
func (p *CloudPanelProvider) deploySite(ctx context.Context, domain, certificate, privateKey string) error {
	path := "/site/" + cloudPanelRawURLEncode(domain) + "/certificate/import"
	page, err := p.request(ctx, "GET", path, nil, 15*time.Second, "")
	if err != nil {
		return err
	}
	if cloudPanelIsLoginPage(page.body, page.redirectURL, p.baseURL) {
		return fmt.Errorf("打开证书导入页失败，站点不存在或登录已失效")
	}
	if page.code != 200 {
		return fmt.Errorf("打开证书导入页失败(httpCode=%d)", page.code)
	}

	token := cloudPanelExtractValue(page.body, "site_import_certificate[_token]", false)
	if token == "" {
		token = cloudPanelExtractValue(page.body, "site_import_certificate__token", true)
	}
	if token == "" {
		return fmt.Errorf("获取证书导入表单令牌失败")
	}

	/* http_build_query 对嵌套数组生成 site_import_certificate[...] 形式且保持插入顺序 */
	form := orderedForm{
		{"site_import_certificate[privateKey]", privateKey},
		{"site_import_certificate[certificate]", certificate},
		{"site_import_certificate[certificateChain]", ""},
		{"site_import_certificate[submit]", "Import and Install"},
		{"site_import_certificate[_token]", token},
	}
	response, err := p.request(ctx, "POST", path, form, 15*time.Second, p.baseURL+path)
	if err != nil {
		return err
	}

	location := p.absoluteURL(response.redirectURL)
	if response.code >= 300 && response.code < 400 {
		if cloudPanelIsLoginPage("", location, p.baseURL) {
			return fmt.Errorf("登录已失效，证书导入未完成")
		}
		if strings.Contains(location, "/certificates") || strings.Contains(location, "/site/"+domain) {
			return nil
		}
	}

	alerts := cloudPanelParseAlerts(response.body)
	if len(alerts) > 0 {
		return errors.New(strings.Join(alerts, "；"))
	}
	if response.code == 200 && strings.Contains(response.body, "site_import_certificate") {
		return fmt.Errorf("证书导入失败，请检查证书与私钥是否匹配")
	}
	if response.code >= 500 {
		return fmt.Errorf("证书导入失败，面板返回 %d，请确认证书格式后在 CloudPanel 后台手动导入排查", response.code)
	}
	if response.code < 200 || response.code >= 300 {
		return fmt.Errorf("证书导入失败(httpCode=%d)", response.code)
	}
	return nil
}

type orderedForm [][2]string

/* Encode 生成 application/x-www-form-urlencoded 请求体 */
func (f orderedForm) Encode() string {
	parts := make([]string, 0, len(f))
	for _, pair := range f {
		parts = append(parts, url.QueryEscape(pair[0])+"="+url.QueryEscape(pair[1]))
	}
	return strings.Join(parts, "&")
}

type cloudPanelResponse struct {
	code        int
	redirectURL string
	body        string
}

func (p *CloudPanelProvider) request(ctx context.Context, method, path string, params interface{}, timeout time.Duration, referer string) (*cloudPanelResponse, error) {
	target := p.baseURL + path
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		target = path
	}

	var bodyReader io.Reader
	switch v := params.(type) {
	case nil:
	case orderedForm:
		bodyReader = strings.NewReader(v.Encode())
	case string:
		bodyReader = strings.NewReader(v)
	}

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("CloudPanel 请求失败：%v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	if strings.EqualFold(method, "POST") {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", p.baseURL)
	}
	if p.cookie != "" {
		req.Header.Set("Cookie", p.cookie)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	} else {
		req.Header.Set("Referer", p.baseURL+"/")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CloudPanel 请求失败：%v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("CloudPanel 请求失败：%v", err)
	}

	p.mergeCookies(resp.Header.Values("Set-Cookie"))

	result := &cloudPanelResponse{code: resp.StatusCode, body: string(raw)}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		result.redirectURL = resp.Header.Get("Location")
	}
	return result, nil
}

func (p *CloudPanelProvider) mergeCookies(setCookies []string) {
	jar := map[string]string{}
	order := []string{}
	if p.cookie != "" {
		for _, pair := range strings.Split(p.cookie, "; ") {
			parts := strings.SplitN(pair, "=", 2)
			name := parts[0]
			value := ""
			if len(parts) == 2 {
				value = parts[1]
			}
			if name != "" {
				if _, exists := jar[name]; !exists {
					order = append(order, name)
				}
				jar[name] = value
			}
		}
	}
	for _, value := range setCookies {
		pair := strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		name := parts[0]
		cookieValue := ""
		if len(parts) == 2 {
			cookieValue = parts[1]
		}
		if name == "" {
			continue
		}
		if cookieValue == "" || strings.HasSuffix(pair, "=deleted") {
			delete(jar, name)
			continue
		}
		if _, exists := jar[name]; !exists {
			order = append(order, name)
		}
		jar[name] = cookieValue
	}

	parts := make([]string, 0, len(order))
	for _, name := range order {
		if value, ok := jar[name]; ok {
			parts = append(parts, name+"="+value)
		}
	}
	p.cookie = strings.Join(parts, "; ")
}

func cloudPanelExtractValue(page, name string, byID bool) string {
	quoted := regexp.QuoteMeta(name)
	attr := "name"
	if byID {
		attr = "id"
	}
	forward := regexp.MustCompile(`(?i)` + attr + `="` + quoted + `"[^>]*value="([^"]*)"`)
	if m := forward.FindStringSubmatch(page); len(m) == 2 {
		return html.UnescapeString(m[1])
	}
	reverse := regexp.MustCompile(`(?i)value="([^"]*)"[^>]*` + attr + `="` + quoted + `"`)
	if m := reverse.FindStringSubmatch(page); len(m) == 2 {
		return html.UnescapeString(m[1])
	}
	return ""
}

func cloudPanelParseAlerts(page string) []string {
	var messages []string
	for _, match := range cpAlertPattern.FindAllStringSubmatch(page, -1) {
		text := strings.TrimSpace(html.UnescapeString(cpTagPattern.ReplaceAllString(match[1], "")))
		text = cpSpacePattern.ReplaceAllString(text, " ")
		if text != "" {
			messages = append(messages, text)
		}
	}
	return messages
}

func cloudPanelIsLoginPage(page, location, baseURL string) bool {
	if cloudPanelPathFromURL(cloudPanelAbsoluteURL(location, baseURL)) == "/login" {
		return true
	}
	return strings.Contains(page, `name="userName"`) && strings.Contains(page, `action="/login"`)
}

func (p *CloudPanelProvider) isTwoFactorLocation(location string) bool {
	path := strings.ToLower(cloudPanelPathFromURL(p.absoluteURL(location)))
	return strings.Contains(path, "2fa") || strings.Contains(path, "two-factor") || strings.Contains(path, "totp")
}

func cloudPanelContainsTwoFactor(page string) bool {
	lower := strings.ToLower(page)
	return strings.Contains(lower, "two-factor") || strings.Contains(lower, "authenticator") || strings.Contains(lower, "one-time password")
}

func (p *CloudPanelProvider) absoluteURL(location string) string {
	return cloudPanelAbsoluteURL(location, p.baseURL)
}

/* cloudPanelAbsoluteURL 把相对 Location 补全为绝对地址 */
func cloudPanelAbsoluteURL(location, baseURL string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
		return location
	}
	if strings.HasPrefix(location, "/") {
		return baseURL + location
	}
	return baseURL + "/" + strings.TrimLeft(location, "/")
}

func cloudPanelPathFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	path := parsed.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

func cloudPanelParseSites(sites string) []string {
	var list []string
	seen := map[string]bool{}
	for _, site := range cpSiteSplitPattern.Split(sites, -1) {
		site = strings.TrimSpace(site)
		if site == "" || seen[site] {
			continue
		}
		seen[site] = true
		list = append(list, site)
	}
	return list
}

func cloudPanelRawURLEncode(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
	var builder strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(unreserved, c) >= 0 {
			builder.WriteByte(c)
			continue
		}
		builder.WriteString(fmt.Sprintf("%%%02X", c))
	}
	return builder.String()
}

/* SetLogger 设置日志记录器 */
func (p *CloudPanelProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
