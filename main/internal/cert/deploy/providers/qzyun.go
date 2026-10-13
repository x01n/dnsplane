package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

/*
 * qzyunBaseURL 全栈云面板地址
 * qzyunRouterStateTree Next.js 路由状态，为前端构建产物硬编码
 * 前端更新后该常量可能失效，findAction 失败时会给出明确提示
 */
const (
	qzyunBaseURL         = "https://panel.qzyun.cn"
	qzyunRouterStateTree = "%5B%22%22%2C%7B%22children%22%3A%5B%22(app)%22%2C%7B%22children%22%3A%5B%22(product)%22%2C%7B%22children%22%3A%5B%22certificate%22%2C%7B%22children%22%3A%5B%22__PAGE__%22%2C%7B%7D%2Cnull%2Cnull%5D%7D%2Cnull%2Cnull%5D%7D%2Cnull%2Cnull%5D%7D%2Cnull%2Cnull%5D%7D%2Cnull%2Cnull%2Ctrue%5D"
)

var (
	qzyunRSCPattern          = regexp.MustCompile(`(?s)self\.__next_f\.push\((\[.*?\])\)</script>`)
	qzyunActionResultPattern = regexp.MustCompile(`(?i)(?:^|\n)[a-z0-9]+:(\{[^\r\n]*\})`)
	qzyunScriptSrcPattern    = regexp.MustCompile(`(?i)<script[^>]+src=["']([^"']+\.js[^"']*)["']`)
	qzyunDocumentIDPattern   = regexp.MustCompile(`(?i)^[a-z0-9]{24}$`)
	qzyunTLSErrorPattern     = regexp.MustCompile(`(?i)SSL|TLS|handshake|alert`)
)

func init() {
	base.Register("qzyun", NewQzyunProvider)
}

/* QzyunProvider 全栈云证书部署，走 Next.js Server Actions 加 Cookie 会话 */
type QzyunProvider struct {
	base.BaseProvider
	email    string
	password string
	cookie   string
	actions  map[string]string
	added    bool
	client   *http.Client
	fallback *http.Client
}

/*
 * NewQzyunProvider 创建全栈云部署器
 * 主客户端按 Go 默认 TLS 协商，降级客户端固定 TLS 1.2
 */
func NewQzyunProvider(config map[string]interface{}) base.DeployProvider {
	newClient := func(cfg *tls.Config, timeout time.Duration) *http.Client {
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: cfg,
				DialContext:     (&net.Dialer{Timeout: timeout}).DialContext,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &QzyunProvider{
		BaseProvider: base.BaseProvider{Config: config},
		email:        strings.TrimSpace(base.GetConfigString(config, "email")),
		password:     base.GetConfigString(config, "password"),
		actions:      map[string]string{},
		client:       newClient(&tls.Config{InsecureSkipVerify: true}, 30*time.Second),
		fallback:     newClient(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}, 30*time.Second),
	}
}

/* Check 登录全栈云验证账号 */
func (p *QzyunProvider) Check(ctx context.Context) error {
	if err := p.validateAccount(); err != nil {
		return err
	}
	return p.login(ctx)
}

/* Deploy 更新全栈云证书管理中的证书 */
func (p *QzyunProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	return p.deploy(ctx, fullchain, privateKey, config, nil)
}

/*
 * DeployWithInfo 部署证书并回传内部文档 ID
 */
func (p *QzyunProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	return p.deploy(ctx, fullchain, privateKey, config, info)
}

/* deploy 执行部署，info 非空时回传证书文档 ID */
func (p *QzyunProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	if err := p.validateAccount(); err != nil {
		return err
	}

	certificate := strings.TrimSpace(base.GetConfigString(config, "certificate"))
	domain := strings.TrimSpace(base.GetConfigString(config, "domain"))
	if certificate == "" {
		return fmt.Errorf("证书名称或文档ID不能为空")
	}
	if strings.TrimSpace(fullchain) == "" || strings.TrimSpace(privateKey) == "" {
		return fmt.Errorf("证书或私钥内容为空")
	}

	if err := p.login(ctx); err != nil {
		return err
	}
	page, err := p.request(ctx, "/certificate", nil, nil, "GET", 15*time.Second)
	if err != nil {
		return err
	}
	if page.code != 200 {
		return fmt.Errorf("获取全栈云证书列表失败(httpCode=%d)", page.code)
	}

	var documentID string
	if qzyunDocumentIDPattern.MatchString(certificate) {
		documentID = certificate
	} else {
		documentID, err = p.findCertificateDocumentID(page.body, certificate)
		if err != nil {
			if !strings.Contains(err.Error(), "未找到证书") {
				return err
			}
			documentID, err = p.addCertificate(ctx, page.body, certificate, fullchain, privateKey)
			if err != nil {
				return err
			}
		}
	}

	if !p.added {
		actionID, err := p.findAction(ctx, page.body, "updateCertificateAction")
		if err != nil {
			return err
		}
		response, err := p.postAction(ctx, "/certificate", actionID, [][2]string{
			{"1_certDocumentId", documentID},
			{"1_serverCertificate", fullchain},
			{"1_privateKey", privateKey},
			{"0", `[{"success":false,"message":""},"$K1"]`},
		})
		if err != nil {
			return err
		}
		result := parseQzyunActionResult(response.body)
		if response.code != 200 || result == nil || !qzyunStrictTrue(result["success"]) {
			return fmt.Errorf("全栈云证书更新失败：%s", qzyunResultMessage(result, response.code))
		}
	}

	if domain != "" {
		if err := p.bindDomain(ctx, domain, documentID, certificate, fullchain, privateKey); err != nil {
			return err
		}
	}

	if info != nil {
		if info.Config == nil {
			info.Config = map[string]interface{}{}
		}
		info.Config["certificate"] = documentID
	}
	p.Log("全栈云证书处理成功，文档ID：" + documentID)
	return nil
}

func (p *QzyunProvider) validateAccount() error {
	if p.email == "" || p.password == "" {
		return fmt.Errorf("请填写全栈云登录邮箱和密码")
	}
	if !qzyunValidEmail(p.email) {
		return fmt.Errorf("全栈云登录邮箱格式不正确")
	}
	return nil
}

func (p *QzyunProvider) login(ctx context.Context) error {
	page, err := p.request(ctx, "/login", nil, nil, "GET", 15*time.Second)
	if err != nil {
		return err
	}
	if page.code != 200 {
		return fmt.Errorf("打开全栈云登录页失败(httpCode=%d)", page.code)
	}

	actionID, err := p.findAction(ctx, page.body, "login")
	if err != nil {
		return err
	}
	response, err := p.postAction(ctx, "/login", actionID, [][2]string{
		{"1_email", p.email},
		{"1_password", p.password},
		{"0", `[{"error":null,"success":false},"$K1"]`},
	})
	if err != nil {
		return err
	}
	p.cookie = qzyunExtractCookies(response.headers)

	/* Next.js Server Actions 登录成功后返回 303 重定向 */
	if response.code >= 300 && response.code < 400 {
		if p.cookie == "" {
			return fmt.Errorf("全栈云登录失败：重定向响应未返回登录会话")
		}
		return nil
	}

	result := parseQzyunActionResult(response.body)
	if response.code != 200 || qzyunStrictFalse(result["success"]) {
		return fmt.Errorf("全栈云登录失败：%s", qzyunLoginMessage(result, response.code))
	}
	if p.cookie == "" {
		return fmt.Errorf("全栈云登录失败：未获取到登录会话")
	}
	return nil
}

func (p *QzyunProvider) addCertificate(ctx context.Context, page, name, fullchain, privateKey string) (string, error) {
	actionID, err := p.findAction(ctx, page, "addCertificateAction")
	if err != nil {
		return "", err
	}
	response, err := p.postAction(ctx, "/certificate", actionID, [][2]string{
		{"1_certificateName", name},
		{"1_serverCertificate", fullchain},
		{"1_privateKey", privateKey},
		{"0", `[{"success":false,"message":""},"$K1"]`},
	})
	if err != nil {
		return "", err
	}
	result := parseQzyunActionResult(response.body)
	if response.code != 200 || result == nil || !qzyunStrictTrue(result["success"]) {
		return "", fmt.Errorf("全栈云证书添加失败：%s", qzyunResultMessage(result, response.code))
	}

	refreshed, err := p.request(ctx, "/certificate", nil, nil, "GET", 15*time.Second)
	if err != nil {
		return "", err
	}
	documentID, err := p.findCertificateDocumentID(refreshed.body, name)
	if err != nil {
		return "", err
	}
	p.added = true
	p.Log("全栈云证书添加成功，文档ID：" + documentID)
	return documentID, nil
}

func (p *QzyunProvider) bindDomain(ctx context.Context, domain, documentID, certificate, fullchain, privateKey string) error {
	domainPage, err := p.request(ctx, "/cdn/domain/"+cloudPanelRawURLEncode(domain), nil, nil, "GET", 15*time.Second)
	if err != nil {
		return err
	}
	if domainPage.code != 200 {
		return fmt.Errorf("获取全栈云域名信息失败(httpCode=%d)", domainPage.code)
	}
	domainInfo, err := findQzyunDomainInfo(domainPage.body, domain)
	if err != nil {
		return err
	}
	if domainInfo == nil {
		return fmt.Errorf("全栈云中未找到域名“%s”", domain)
	}

	certificatePage, err := p.request(ctx, "/certificate", nil, nil, "GET", 15*time.Second)
	if err != nil {
		return err
	}
	if qzyunIsCertificateBound(certificatePage.body, documentID, domain) {
		p.Log("全栈云证书已绑定域名，跳过：" + domain)
		return nil
	}
	actionID, err := p.findAction(ctx, certificatePage.body, "bindDomainCdnServer")
	if err != nil {
		return err
	}
	certificateName := certificate
	if qzyunDocumentIDPattern.MatchString(certificate) {
		certificateName, err = findQzyunCertificateNameByDocumentID(certificatePage.body, documentID)
		if err != nil {
			return err
		}
	}
	response, err := p.postAction(ctx, "/certificate", actionID, [][2]string{
		{"1_domainName", domain},
		{"1_Id", qzyunStringify(domainInfo["id"])},
		{"1_documentId", qzyunStringify(domainInfo["documentId"])},
		{"1_CertificateName", certificateName},
		{"1_ServerCertificate", fullchain},
		{"1_CertificateDocumentId", documentID},
		{"1_PrivateKey", privateKey},
		{"0", `[{"success":false,"message":""},"$K1"]`},
	})
	if err != nil {
		return err
	}
	result := parseQzyunActionResult(response.body)
	if response.code != 200 || result == nil || !qzyunStrictTrue(result["success"]) {
		return fmt.Errorf("全栈云证书绑定域名失败：%s", qzyunResultMessage(result, response.code))
	}
	p.Log("全栈云证书已绑定域名：" + domain)
	return nil
}

func qzyunIsCertificateBound(page, documentID, domain string) bool {
	rsc := qzyunExtractRSC(page)
	needle := `"documentId":` + jsonEscapedString(documentID)
	position := strings.Index(rsc, needle)
	if position < 0 {
		return false
	}
	start := strings.LastIndex(rsc[:position], `{"id":`)
	if start < 0 {
		return false
	}
	object := qzyunExtractJSONObject(rsc, start)
	if object == "" {
		return false
	}
	var certificate map[string]interface{}
	if err := json.Unmarshal([]byte(object), &certificate); err != nil {
		return false
	}
	cdnDomains, _ := certificate["cdnDomains"].([]interface{})
	for _, item := range cdnDomains {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if name, ok := entry["domain"].(string); ok && name == domain {
			return true
		}
	}
	return false
}

func findQzyunDomainInfo(page, domain string) (map[string]interface{}, error) {
	rsc := qzyunExtractRSC(page)
	needle := `"domain":` + jsonEscapedString(domain)
	offset := 0
	for {
		position := strings.Index(rsc[offset:], needle)
		if position < 0 {
			break
		}
		position += offset
		start := strings.LastIndex(rsc[:position], `{"id":`)
		if start >= 0 {
			object := qzyunExtractJSONObject(rsc, start)
			if object != "" {
				var domainData map[string]interface{}
				if err := json.Unmarshal([]byte(object), &domainData); err == nil {
					name, _ := domainData["domain"].(string)
					_, hasID := domainData["id"]
					_, hasDocumentID := domainData["documentId"]
					if name == domain && hasID && hasDocumentID {
						return domainData, nil
					}
				}
			}
		}
		offset = position + len(needle)
	}
	return nil, nil
}

func findQzyunCertificateNameByDocumentID(page, documentID string) (string, error) {
	rsc := qzyunExtractRSC(page)
	needle := `"documentId":` + jsonEscapedString(documentID)
	offset := 0
	for {
		position := strings.Index(rsc[offset:], needle)
		if position < 0 {
			break
		}
		position += offset
		start := strings.LastIndex(rsc[:position], `{"id":`)
		if start >= 0 {
			object := qzyunExtractJSONObject(rsc, start)
			if object != "" {
				var certificate map[string]interface{}
				if err := json.Unmarshal([]byte(object), &certificate); err == nil {
					name, hasName := certificate["name"].(string)
					_, hasContent := certificate["content"]
					if hasName && hasContent {
						return name, nil
					}
				}
			}
		}
		offset = position + len(needle)
	}
	return "", fmt.Errorf("全栈云中未找到证书名称")
}

func (p *QzyunProvider) findCertificateDocumentID(page, certificateName string) (string, error) {
	rsc := qzyunExtractRSC(page)
	needle := `"name":` + jsonEscapedString(certificateName)
	offset := 0
	var documentIDs []string
	seen := map[string]bool{}

	for {
		position := strings.Index(rsc[offset:], needle)
		if position < 0 {
			break
		}
		position += offset
		start := strings.LastIndex(rsc[:position], `{"id":`)
		if start >= 0 {
			object := qzyunExtractJSONObject(rsc, start)
			if object != "" {
				var certificate map[string]interface{}
				if err := json.Unmarshal([]byte(object), &certificate); err == nil {
					name, _ := certificate["name"].(string)
					_, hasStart := certificate["startTime"]
					_, hasKey := certificate["privateKey"]
					_, hasContent := certificate["content"]
					id, hasID := certificate["documentId"].(string)
					if name == certificateName && hasStart && hasKey && hasContent && hasID && !seen[id] {
						seen[id] = true
						documentIDs = append(documentIDs, id)
					}
				}
			}
		}
		offset = position + len(needle)
	}

	switch len(documentIDs) {
	case 1:
		return documentIDs[0], nil
	case 0:
		return "", fmt.Errorf("全栈云中未找到证书“%s”", certificateName)
	default:
		return "", fmt.Errorf("全栈云中存在多张同名证书，请改为填写证书文档ID")
	}
}

func (p *QzyunProvider) findAction(ctx context.Context, page, actionName string) (string, error) {
	if cached, ok := p.actions[actionName]; ok {
		return cached, nil
	}

	matches := qzyunScriptSrcPattern.FindAllStringSubmatch(page, -1)
	visited := map[string]bool{}
	for _, match := range matches {
		src := htmlUnescape(match[1])
		if visited[src] {
			continue
		}
		visited[src] = true
		target := qzyunSameOriginURL(src)
		if target == "" {
			continue
		}
		response, err := p.request(ctx, target, nil, nil, "GET", 15*time.Second)
		if err != nil || response.code != 200 {
			continue
		}
		pattern := regexp.MustCompile(`(?s)createServerReference\)\("([a-f0-9]{32,64})".{0,300},"` + regexp.QuoteMeta(actionName) + `"\)`)
		if found := pattern.FindStringSubmatch(response.body); len(found) == 2 {
			p.actions[actionName] = found[1]
			return found[1], nil
		}
	}
	return "", fmt.Errorf("无法识别全栈云接口动作：%s，可能是全栈云前端已更新", actionName)
}

func qzyunExtractRSC(page string) string {
	var builder strings.Builder
	for _, match := range qzyunRSCPattern.FindAllStringSubmatch(page, -1) {
		var data []interface{}
		if err := json.Unmarshal([]byte(match[1]), &data); err != nil {
			continue
		}
		if len(data) < 2 {
			continue
		}
		flag, ok := data[0].(float64)
		if !ok || flag != 1 {
			continue
		}
		if payload, ok := data[1].(string); ok {
			builder.WriteString(payload)
		}
	}
	return builder.String()
}

func qzyunExtractJSONObject(text string, start int) string {
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(text); i++ {
		char := text[i]
		if inString {
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

func (p *QzyunProvider) postAction(ctx context.Context, path, actionID string, fields [][2]string) (*qzyunResponse, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, fmt.Errorf("全栈云请求失败：%v", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("全栈云请求失败：%v", err)
	}

	headers := map[string]string{
		"Accept":                 "text/x-component",
		"next-action":            actionID,
		"next-router-state-tree": qzyunRouterStateTree,
		"Content-Type":           writer.FormDataContentType(),
	}
	return p.requestRaw(ctx, qzyunBaseURL+path, buffer.Bytes(), headers, qzyunBaseURL+path, "POST", 30*time.Second)
}

func parseQzyunActionResult(body string) map[string]interface{} {
	matches := qzyunActionResultPattern.FindAllStringSubmatch(body, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(matches[i][1]), &result); err != nil {
			continue
		}
		_, hasSuccess := result["success"]
		_, hasError := result["error"]
		_, hasMessage := result["message"]
		if hasSuccess || hasError || hasMessage {
			return result
		}
	}
	return nil
}

func qzyunExtractCookies(headers http.Header) string {
	var cookies []string
	for _, value := range headers.Values("Set-Cookie") {
		pair := strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
		if pair == "" || strings.HasSuffix(pair, "=") || strings.HasSuffix(pair, "=deleted") {
			continue
		}
		cookies = append(cookies, pair)
	}
	return strings.Join(cookies, "; ")
}

func qzyunSameOriginURL(raw string) string {
	if strings.HasPrefix(raw, "/") {
		return qzyunBaseURL + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base, err := url.Parse(qzyunBaseURL)
	if err != nil {
		return ""
	}
	if parsed.Host == base.Host {
		return raw
	}
	return ""
}

/* qzyunResponse 请求结果，headers 用于收集 Set-Cookie */
type qzyunResponse struct {
	code    int
	body    string
	headers http.Header
}

/*
 * request 发送请求，TLS 相关失败时降级到 TLS 1.2 重试
 */
func (p *QzyunProvider) request(ctx context.Context, path string, data []byte, headers map[string]string, method string, timeout time.Duration) (*qzyunResponse, error) {
	absolute := strings.HasPrefix(path, "http")
	target := qzyunBaseURL + path
	if absolute {
		target = path
	}
	referer := qzyunBaseURL + path
	if absolute {
		referer = qzyunBaseURL + "/"
	}

	response, err := p.requestRaw(ctx, target, data, headers, referer, method, timeout)
	if err == nil {
		return response, nil
	}
	if !qzyunTLSErrorPattern.MatchString(err.Error()) {
		return nil, err
	}
	return p.requestRawWithClient(ctx, p.fallback, target, data, headers, referer, method, timeout)
}

/* requestRaw 用默认客户端发送请求 */
func (p *QzyunProvider) requestRaw(ctx context.Context, target string, data []byte, headers map[string]string, referer, method string, timeout time.Duration) (*qzyunResponse, error) {
	return p.requestRawWithClient(ctx, p.client, target, data, headers, referer, method, timeout)
}

func (p *QzyunProvider) requestRawWithClient(ctx context.Context, client *http.Client, target string, data []byte, headers map[string]string, referer, method string, timeout time.Duration) (*qzyunResponse, error) {
	var bodyReader io.Reader
	if data != nil {
		bodyReader = bytes.NewReader(data)
	}

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("全栈云请求失败：%v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if p.cookie != "" {
		req.Header.Set("Cookie", p.cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("全栈云请求失败：%v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("全栈云请求失败：%v", err)
	}
	return &qzyunResponse{code: resp.StatusCode, body: string(raw), headers: resp.Header}, nil
}

func qzyunStrictTrue(v interface{}) bool {
	b, ok := v.(bool)
	return ok && b
}

func qzyunStrictFalse(v interface{}) bool {
	b, ok := v.(bool)
	return ok && !b
}

func qzyunResultMessage(result map[string]interface{}, httpCode int) string {
	if result != nil {
		if message, ok := result["message"].(string); ok && message != "" {
			return message
		}
	}
	return fmt.Sprintf("请求失败(httpCode=%d)", httpCode)
}

func qzyunLoginMessage(result map[string]interface{}, httpCode int) string {
	if result != nil {
		if message, ok := result["error"].(string); ok && message != "" {
			return message
		}
		if message, ok := result["message"].(string); ok && message != "" {
			return message
		}
	}
	return fmt.Sprintf("请求失败(httpCode=%d)", httpCode)
}
func qzyunStringify(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "1"
		}
		return ""
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}

func jsonEscapedString(s string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			if r < 0x20 {
				builder.WriteString(fmt.Sprintf(`\u%04x`, r))
				continue
			}
			builder.WriteRune(r)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func htmlUnescape(s string) string {
	return html.UnescapeString(s)
}

func qzyunValidEmail(value string) bool {
	if value == "" || len(value) > 254 {
		return false
	}
	at := strings.LastIndex(value, "@")
	if at <= 0 || at == len(value)-1 {
		return false
	}
	local := value[:at]
	domain := value[at+1:]
	if local == "" || len(local) > 64 || domain == "" || !strings.Contains(domain, ".") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
			if !isAlnum && c != '-' {
				return false
			}
		}
	}
	topLabel := domain[strings.LastIndex(domain, ".")+1:]
	if topLabel[0] >= '0' && topLabel[0] <= '9' {
		return false
	}
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return false
	}
	const localExtra = "!#$%&'*+-/=?^_`{|}~"
	for i := 0; i < len(local); i++ {
		c := local[i]
		isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlnum && !strings.ContainsRune(localExtra, rune(c)) && c != '.' {
			return false
		}
	}
	return true
}

/* SetLogger 设置日志记录器 */
func (p *QzyunProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
