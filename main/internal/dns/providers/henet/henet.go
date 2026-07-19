package henet

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"main/internal/cache"
	"main/internal/dns"
)

func init() {
	dns.Register("henet", NewProvider, dns.ProviderConfig{
		Type: "henet",
		Name: "HE DNS",
		Icon: "henet.png",
		Config: []dns.ConfigField{
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 0, Status: false, Redirect: false, Log: false, Weight: false, Page: true, Add: true,
		},
	})
}

const (
	baseURL      = "https://dns.he.net/"
	sessionTTL   = 30 * time.Minute
	cachePrefix  = "henet_session:"
)

/* Provider HE DNS服务商 */
type Provider struct {
	username string
	password string
	domain   string
	domainID string
	client   *http.Client
	lastErr  string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	jar, _ := cookiejar.New(nil)
	return &Provider{
		username: config["username"],
		password: config["password"],
		domain:   domain,
		domainID: domainID,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

/* login 登录HE DNS获取会话cookie */
func (p *Provider) login(ctx context.Context) error {
	cacheKey := cachePrefix + p.username
	if cookies, ok := cache.C.Get(cacheKey); ok && cookies != "" {
		u, _ := url.Parse(baseURL)
		var parsed []*http.Cookie
		for _, part := range strings.Split(cookies, "; ") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				parsed = append(parsed, &http.Cookie{Name: kv[0], Value: kv[1]})
			}
		}
		p.client.Jar.SetCookies(u, parsed)
		return nil
	}

	data := url.Values{}
	data.Set("email", p.username)
	data.Set("pass", p.password)

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	u, _ := url.Parse(baseURL)
	cookies := p.client.Jar.Cookies(u)
	if len(cookies) == 0 {
		return fmt.Errorf("登录失败：未获取到会话cookie")
	}

	var parts []string
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	_ = cache.C.Set(cacheKey, strings.Join(parts, "; "), sessionTTL)
	return nil
}

/* doGet 发送GET请求并返回响应体 */
func (p *Provider) doGet(ctx context.Context, reqURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

/* doPost 发送POST请求并返回响应体 */
func (p *Provider) doPost(ctx context.Context, reqURL string, data url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (p *Provider) Check(ctx context.Context) error {
	return p.login(ctx)
}

var domainListRe = regexp.MustCompile(`(?s)<img[^>]*>\s*([^\s<]+)\s*</td>\s*<td[^>]*>.*?dns_zoneid=(\d+)`)

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	if err := p.login(ctx); err != nil {
		return nil, err
	}

	body, err := p.doGet(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	matches := domainListRe.FindAllStringSubmatch(body, -1)
	var domains []dns.DomainInfo
	for _, m := range matches {
		name := m[1]
		zoneID := m[2]
		if keyword != "" && !strings.Contains(name, keyword) {
			continue
		}
		domains = append(domains, dns.DomainInfo{
			ID:   zoneID,
			Name: name,
		})
	}

	total := len(domains)
	start := (page - 1) * pageSize
	if start >= total {
		return &dns.PageResult{Total: total, Records: []dns.DomainInfo{}}, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return &dns.PageResult{
		Total:   total,
		Records: domains[start:end],
	}, nil
}

var recordRowRe = regexp.MustCompile(`(?s)<tr class="dns_tr"[^>]*id="(\d+)"[^>]*>(.*?)</tr>`)
var recordTdRe = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)

/* parseRecords 从HTML中解析记录列表 */
func (p *Provider) parseRecords(body string) []dns.Record {
	rows := recordRowRe.FindAllStringSubmatch(body, -1)
	var records []dns.Record
	for _, row := range rows {
		id := row[1]
		tds := recordTdRe.FindAllStringSubmatch(row[2], -1)
		if len(tds) < 5 {
			continue
		}
		name := strings.TrimSpace(stripTags(tds[0][1]))
		rType := strings.TrimSpace(stripTags(tds[1][1]))
		ttlStr := strings.TrimSpace(stripTags(tds[2][1]))
		priority := strings.TrimSpace(stripTags(tds[3][1]))
		value := strings.TrimSpace(stripTags(tds[4][1]))

		ttl, _ := strconv.Atoi(ttlStr)
		mx, _ := strconv.Atoi(priority)

		if name == p.domain {
			name = "@"
		} else {
			name = strings.TrimSuffix(name, "."+p.domain)
		}

		records = append(records, dns.Record{
			ID:    id,
			Name:  name,
			Type:  rType,
			Value: value,
			TTL:   ttl,
			MX:    mx,
		})
	}
	return records
}

/* stripTags 去除HTML标签 */
func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	if err := p.login(ctx); err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%sindex.cgi?hosted_dns_zoneid=%s&menu=edit_zone&hosted_dns_editzone=", baseURL, p.domainID)
	body, err := p.doGet(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	records := p.parseRecords(body)

	var filtered []dns.Record
	for _, r := range records {
		if keyword != "" && !strings.Contains(r.Name, keyword) && !strings.Contains(r.Value, keyword) {
			continue
		}
		if subDomain != "" && r.Name != subDomain {
			continue
		}
		if value != "" && !strings.Contains(r.Value, value) {
			continue
		}
		if recordType != "" && r.Type != recordType {
			continue
		}
		filtered = append(filtered, r)
	}

	total := len(filtered)
	start := (page - 1) * pageSize
	if start >= total {
		return &dns.PageResult{Total: total, Records: []dns.Record{}}, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return &dns.PageResult{
		Total:   total,
		Records: filtered[start:end],
	}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	if err := p.login(ctx); err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%sindex.cgi?hosted_dns_zoneid=%s&menu=edit_zone&hosted_dns_editzone=", baseURL, p.domainID)
	body, err := p.doGet(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	records := p.parseRecords(body)
	for _, r := range records {
		if r.ID == recordID {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("记录不存在: %s", recordID)
}

/* fullName 将主机记录转为完整域名 */
func (p *Provider) fullName(name string) string {
	if name == "@" || name == "" {
		return p.domain
	}
	return name + "." + p.domain
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if err := p.login(ctx); err != nil {
		return "", err
	}

	data := url.Values{}
	data.Set("account", "")
	data.Set("menu", "edit_zone")
	data.Set("Type", recordType)
	data.Set("hosted_dns_zoneid", p.domainID)
	data.Set("Name", p.fullName(name))
	data.Set("Content", value)
	data.Set("TTL", strconv.Itoa(ttl))
	if mx > 0 {
		data.Set("Priority", strconv.Itoa(mx))
	}

	body, err := p.doPost(ctx, baseURL+"index.cgi", data)
	if err != nil {
		return "", err
	}

	if !strings.Contains(body, "successfully") && !strings.Contains(body, "dns_tr") {
		return "", fmt.Errorf("添加记录失败")
	}

	// HE DNS 添加后不直接返回记录ID，从返回的记录列表中匹配定位
	records := p.parseRecords(body)
	fullName := p.fullName(name)
	displayName := name
	if name == "@" || name == "" {
		displayName = "@"
	}
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if (r.Name == displayName || r.Name == fullName) && r.Type == recordType && r.Value == value {
			return r.ID, nil
		}
	}

	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	if err := p.login(ctx); err != nil {
		return err
	}

	data := url.Values{}
	data.Set("account", "")
	data.Set("menu", "edit_zone")
	data.Set("hosted_dns_editrecord", recordID)
	data.Set("hosted_dns_zoneid", p.domainID)
	data.Set("Type", recordType)
	data.Set("Name", p.fullName(name))
	data.Set("Content", value)
	data.Set("TTL", strconv.Itoa(ttl))
	if mx > 0 {
		data.Set("Priority", strconv.Itoa(mx))
	}

	body, err := p.doPost(ctx, baseURL+"index.cgi", data)
	if err != nil {
		return err
	}

	if strings.Contains(body, "successfully") || strings.Contains(body, "dns_tr") {
		return nil
	}
	return fmt.Errorf("更新记录失败")
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("HE DNS不支持设置备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	if err := p.login(ctx); err != nil {
		return err
	}

	data := url.Values{}
	data.Set("menu", "edit_zone")
	data.Set("hosted_dns_delrecord", recordID)
	data.Set("hosted_dns_delconfirm", "delete")
	data.Set("hosted_dns_zoneid", p.domainID)

	body, err := p.doPost(ctx, baseURL+"index.cgi", data)
	if err != nil {
		return err
	}

	if strings.Contains(body, "successfully") || !strings.Contains(body, recordID) {
		return nil
	}
	return fmt.Errorf("删除记录失败")
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	return fmt.Errorf("HE DNS不支持启用/暂停记录")
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("HE DNS不支持查看日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "default", Name: "默认"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 300
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	if err := p.login(ctx); err != nil {
		return err
	}

	data := url.Values{}
	data.Set("retmain", "0")
	data.Set("domain", domain)

	body, err := p.doPost(ctx, baseURL+"index.cgi", data)
	if err != nil {
		return err
	}

	if strings.Contains(body, "successfully") || strings.Contains(body, domain) {
		return nil
	}
	return fmt.Errorf("添加域名失败")
}
