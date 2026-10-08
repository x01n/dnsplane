package providers

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math"
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

func init() {
	base.Register("directadmin", NewDirectAdminProvider)
}

const daTrimCutset = " \t\n\r\x00\x0b"

var daDomainSplitter = regexp.MustCompile("[ \t\n\v\f\r,]+")

/* DirectAdminProvider DirectAdmin 证书部署 */
type DirectAdminProvider struct {
	base.BaseProvider
	panelURL  string
	username  string
	password  string
	configErr error
	client    *http.Client
}

/*
 * NewDirectAdminProvider 创建 DirectAdmin 部署器
 * 面板地址必须是 https 且不带路径、凭据、查询或片段
 * 工厂函数无法返回错误，地址校验失败记录在 configErr，由 Check/Deploy 报出
 */
func NewDirectAdminProvider(config map[string]interface{}) base.DeployProvider {
	panelURL := strings.TrimRight(daTrim(base.GetConfigString(config, "url")), "/")
	var configErr error
	if panelURL != "" && !directAdminValidPanelURL(panelURL) {
		configErr = fmt.Errorf("DirectAdmin 面板地址格式无效，必须为不带路径、凭据、查询或片段的 HTTPS 地址")
		panelURL = ""
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext:     (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	}
	return &DirectAdminProvider{
		BaseProvider: base.BaseProvider{Config: config},
		panelURL:     panelURL,
		username:     daTrim(base.GetConfigString(config, "username")),
		password:     base.GetConfigString(config, "password"),
		configErr:    configErr,
		client: &http.Client{
			Timeout:   20 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (p *DirectAdminProvider) Check(ctx context.Context) error {
	if err := p.assertAccountConfig(); err != nil {
		return err
	}
	_, err := p.request(ctx, "GET", "/CMD_API_LOGIN_TEST", nil)
	return err
}

func (p *DirectAdminProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	if err := p.assertAccountConfig(); err != nil {
		return err
	}

	domains, err := directAdminParseDomains(base.GetConfigString(config, "domain"))
	if err != nil {
		return err
	}
	if len(domains) == 0 {
		return fmt.Errorf("没有设置要部署的 DirectAdmin 域名")
	}
	if daTrim(fullchain) == "" || daTrim(privateKey) == "" {
		return fmt.Errorf("SSL 证书或私钥内容不能为空")
	}

	normalizedKey, err := directAdminNormalizePrivateKey(privateKey)
	if err != nil {
		return err
	}
	certificate := strings.TrimRight(normalizedKey, daTrimCutset) + "\n" + strings.TrimLeft(fullchain, daTrimCutset)

	for _, domain := range domains {
		form := url.Values{}
		form.Set("domain", domain)
		form.Set("action", "save")
		form.Set("type", "paste")
		form.Set("certificate", certificate)
		if _, err := p.request(ctx, "POST", "/CMD_API_SSL", form); err != nil {
			return err
		}
		p.Log("DirectAdmin 域名 " + domain + " 证书部署成功")
	}
	return nil
}

func (p *DirectAdminProvider) assertAccountConfig() error {
	if p.configErr != nil {
		return p.configErr
	}
	if p.panelURL == "" || p.username == "" || p.password == "" {
		return fmt.Errorf("请填写 DirectAdmin 面板地址、用户名和认证密码")
	}
	return nil
}

/*
 * directAdminValidPanelURL 校验面板地址
 * 要求 scheme 为 https、host 非空、无用户凭据、路径为空或 "/"、无查询与片段
 */
func directAdminValidPanelURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	if parsed.Host == "" || parsed.Hostname() == "" || parsed.Hostname() == "0" {
		return false
	}
	if parsed.User != nil {
		return false
	}
	if port := parsed.Port(); port != "" {
		number, convErr := strconv.Atoi(port)
		if convErr != nil || number < 1 || number > 65535 {
			return false
		}
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.Path == "" || parsed.Path == "/"
}

/*
 * directAdminParseDomains 解析域名列表
 * 切分后转小写、按主机名校验，并按首次出现顺序去重
 */
func directAdminParseDomains(value string) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	for _, item := range daDomainSplitter.Split(value, -1) {
		domain := strings.ToLower(daTrim(item))
		if domain == "" {
			continue
		}
		if !directAdminValidDomain(domain) {
			return nil, fmt.Errorf("DirectAdmin 域名格式不正确：%s", domain)
		}
		if seen[domain] {
			continue
		}
		seen[domain] = true
		result = append(result, domain)
	}
	return result, nil
}

func directAdminValidDomain(domain string) bool {
	if domain == "" {
		return false
	}
	trimmed := strings.TrimSuffix(domain, ".")
	if trimmed == "" || len(trimmed) > 253 {
		return false
	}
	for _, label := range strings.Split(trimmed, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
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
	return true
}

/*
 * directAdminNormalizePrivateKey 把 PKCS#8 ECC 私钥转成 DirectAdmin 兼容的 SEC1 格式
 */
func directAdminNormalizePrivateKey(privateKey string) (string, error) {
	if !strings.HasPrefix(strings.TrimLeft(privateKey, daTrimCutset), "-----BEGIN PRIVATE KEY-----") {
		return privateKey, nil
	}

	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return "", fmt.Errorf("SSL 私钥格式无效")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("SSL 私钥格式无效")
	}
	if _, ok := parsed.(*ecdsa.PrivateKey); !ok {
		return privateKey, nil
	}

	sec1WithCurve, err := directAdminPKCS8ToSEC1(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("ECC 私钥转换为 DirectAdmin 兼容格式失败：%v", err)
	}
	return "-----BEGIN EC PRIVATE KEY-----\n" +
		chunkSplit(base64.StdEncoding.EncodeToString(sec1WithCurve), 64, "\n") +
		"-----END EC PRIVATE KEY-----\n", nil
}

func directAdminPKCS8ToSEC1(der []byte) ([]byte, error) {
	offset := 0
	tag, sequence, err := daReadDERElement(der, &offset)
	if err != nil {
		return nil, err
	}
	if tag != 0x30 || offset != len(der) {
		return nil, fmt.Errorf("PKCS#8 外层结构无效")
	}

	innerOffset := 0
	versionTag, _, err := daReadDERElement(sequence, &innerOffset)
	if err != nil {
		return nil, err
	}
	algorithmTag, algorithm, err := daReadDERElement(sequence, &innerOffset)
	if err != nil {
		return nil, err
	}
	keyTag, sec1, err := daReadDERElement(sequence, &innerOffset)
	if err != nil {
		return nil, err
	}
	if versionTag != 0x02 || algorithmTag != 0x30 || keyTag != 0x04 || len(sec1) == 0 {
		return nil, fmt.Errorf("PKCS#8 ECC 私钥结构无效")
	}

	algorithmOffset := 0
	algorithmOIDTag, _, err := daReadDERElement(algorithm, &algorithmOffset)
	if err != nil {
		return nil, err
	}
	curveStart := algorithmOffset
	curveOIDTag, _, err := daReadDERElement(algorithm, &algorithmOffset)
	if err != nil {
		return nil, err
	}
	curveElement := algorithm[curveStart:algorithmOffset]
	if algorithmOIDTag != 0x06 || curveOIDTag != 0x06 || len(curveElement) == 0 {
		return nil, fmt.Errorf("PKCS#8 ECC 曲线参数缺失")
	}

	sec1Offset := 0
	sec1Tag, sec1Content, err := daReadDERElement(sec1, &sec1Offset)
	if err != nil {
		return nil, err
	}
	if sec1Tag != 0x30 || sec1Offset != len(sec1) {
		return nil, fmt.Errorf("SEC1 ECC 私钥结构无效")
	}

	contentOffset := 0
	versionStart := contentOffset
	sec1VersionTag, _, err := daReadDERElement(sec1Content, &contentOffset)
	if err != nil {
		return nil, err
	}
	versionElement := sec1Content[versionStart:contentOffset]
	keyStart := contentOffset
	privateOctetTag, _, err := daReadDERElement(sec1Content, &contentOffset)
	if err != nil {
		return nil, err
	}
	privateOctetElement := sec1Content[keyStart:contentOffset]
	if sec1VersionTag != 0x02 || privateOctetTag != 0x04 {
		return nil, fmt.Errorf("SEC1 ECC 私钥字段无效")
	}

	remaining := sec1Content[contentOffset:]
	if len(remaining) > 0 && remaining[0] == 0xa0 {
		/* 已带曲线参数，直接沿用原 SEC1 */
		return sec1, nil
	}
	parameters := append([]byte{0xa0}, daEncodeDERLength(len(curveElement))...)
	parameters = append(parameters, curveElement...)
	newContent := make([]byte, 0, len(versionElement)+len(privateOctetElement)+len(parameters)+len(remaining))
	newContent = append(newContent, versionElement...)
	newContent = append(newContent, privateOctetElement...)
	newContent = append(newContent, parameters...)
	newContent = append(newContent, remaining...)
	return append(append([]byte{0x30}, daEncodeDERLength(len(newContent))...), newContent...), nil
}

func daReadDERElement(der []byte, offset *int) (byte, []byte, error) {
	total := len(der)
	if *offset+2 > total {
		return 0, nil, fmt.Errorf("ASN.1 数据不完整")
	}
	tag := der[*offset]
	*offset++
	firstLength := int(der[*offset])
	*offset++

	var length int
	if firstLength&0x80 == 0 {
		length = firstLength
	} else {
		lengthBytes := firstLength & 0x7f
		if lengthBytes < 1 || lengthBytes > 4 || *offset+lengthBytes > total {
			return 0, nil, fmt.Errorf("ASN.1 长度字段无效")
		}
		for i := 0; i < lengthBytes; i++ {
			length = (length << 8) | int(der[*offset])
			*offset++
		}
	}
	if length < 0 || *offset+length > total {
		return 0, nil, fmt.Errorf("ASN.1 内容长度越界")
	}
	value := der[*offset : *offset+length]
	*offset += length
	return tag, value, nil
}

func daEncodeDERLength(length int) []byte {
	if length < 0x80 {
		return []byte{byte(length)}
	}
	var out []byte
	for length > 0 {
		out = append([]byte{byte(length & 0xff)}, out...)
		length >>= 8
	}
	return append([]byte{0x80 | byte(len(out))}, out...)
}

func chunkSplit(s string, chunkLen int, separator string) string {
	if s == "" {
		return separator
	}
	if chunkLen <= 0 {
		return s
	}
	var builder strings.Builder
	for i := 0; i < len(s); i += chunkLen {
		end := i + chunkLen
		if end > len(s) {
			end = len(s)
		}
		builder.WriteString(s[i:end])
		builder.WriteString(separator)
	}
	return builder.String()
}

func (p *DirectAdminProvider) request(ctx context.Context, method, path string, form url.Values) (map[string]interface{}, error) {
	var bodyReader io.Reader
	contentType := ""
	if form != nil {
		bodyReader = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(ctx, method, p.panelURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("DirectAdmin 请求失败：%v", err)
	}
	req.Header.Set("Accept", "application/x-www-form-urlencoded, application/json")
	req.Header.Set("User-Agent", "DNSManager-DirectAdmin-Deploy/1.0")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.SetBasicAuth(p.username, p.password)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DirectAdmin 请求失败：%v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("DirectAdmin 请求失败：%v", err)
	}
	return directAdminParseResponse(resp.StatusCode, string(body))
}

func directAdminParseResponse(httpCode int, body string) (map[string]interface{}, error) {
	if httpCode == 401 || httpCode == 403 {
		return nil, fmt.Errorf("DirectAdmin 认证失败，请检查用户名和认证密码")
	}
	if httpCode < 200 || httpCode >= 300 {
		return nil, fmt.Errorf("DirectAdmin 请求失败（HTTP %d）", httpCode)
	}

	trimmed := daTrim(body)
	if trimmed == "" {
		return nil, fmt.Errorf("DirectAdmin 响应格式异常：响应内容为空")
	}

	data := map[string]interface{}{}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		var decoded interface{}
		if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
			return nil, fmt.Errorf("DirectAdmin 响应格式异常：JSON 无法解析")
		}
		if object, ok := decoded.(map[string]interface{}); ok {
			data = object
		}
	} else {
		data = directAdminParseQuery(body)
	}

	errorValue, ok := data["error"]
	if !ok {
		return nil, fmt.Errorf("DirectAdmin 响应格式异常：缺少 error 状态字段")
	}
	if daStringify(errorValue) != "0" && errorValue != false {
		message := directAdminErrorText(data)
		if message == "" {
			message = "DirectAdmin 返回未知错误"
		}
		return nil, fmt.Errorf("%s", message)
	}
	return data, nil
}

func directAdminErrorText(data map[string]interface{}) string {
	value, ok := data["details"]
	if !ok || value == nil {
		value, ok = data["text"]
		if !ok || value == nil {
			return "DirectAdmin 返回未知错误"
		}
	}
	return daTrim(daStringify(value))
}

func directAdminParseQuery(body string) map[string]interface{} {
	data := map[string]interface{}{}
	values, err := url.ParseQuery(body)
	if err != nil {
		return data
	}
	for key, vals := range values {
		if len(vals) == 0 {
			continue
		}
		if idx := strings.IndexByte(key, '['); idx >= 0 {
			data[key[:idx]] = []interface{}{vals[len(vals)-1]}
			continue
		}
		data[key] = vals[len(vals)-1]
	}
	return data
}

func daStringify(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "1"
		}
		return ""
	case string:
		return t
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case []interface{}, map[string]interface{}:
		return "Array"
	}
	return ""
}

func daTrim(s string) string { return strings.Trim(s, daTrimCutset) }

/* SetLogger 设置日志记录器 */
func (p *DirectAdminProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
