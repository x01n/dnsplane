package west

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"main/internal/dns"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func init() {
	dns.Register("west", NewProvider, dns.ProviderConfig{
		Type: "west",
		Name: "西部数码",
		Icon: "west.png",
		Config: []dns.ConfigField{
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "API密码", Key: "api_password", Type: "input", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 0, Status: true, Redirect: false, Log: false, Weight: false, Page: false, Add: false,
		},
	})
}

const baseURL = "https://api.west.cn/api/v2"

type Provider struct {
	username    string
	apiPassword string
	domain      string
	domainID    string
	proxy       bool
	client      *http.Client
	lastErr     string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		username:    config["username"],
		apiPassword: config["api_password"],
		domain:      domain,
		domainID:    domainID,
		proxy:       config["proxy"] == "1",
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) request(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)

	hash := md5.New()
	hash.Write([]byte(p.username + p.apiPassword + timestamp))
	token := hex.EncodeToString(hash.Sum(nil))

	params["username"] = p.username
	params["time"] = timestamp
	params["token"] = token

	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}

	reqURL := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Convert GBK to UTF-8
	reader := transform.NewReader(strings.NewReader(string(respBody)), simplifiedchinese.GBK.NewDecoder())
	utf8Body, err := io.ReadAll(reader)
	if err != nil {
		utf8Body = respBody
	}

	var result map[string]interface{}
	if err := json.Unmarshal(utf8Body, &result); err != nil {
		return nil, err
	}

	if code, ok := result["result"].(float64); ok && code != 200 {
		msg := result["msg"].(string)
		p.lastErr = msg
		return nil, fmt.Errorf("%s", msg)
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		return data, nil
	}

	return result, nil
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	params := map[string]string{
		"act":    "getdomains",
		"page":   strconv.Itoa(page),
		"limit":  strconv.Itoa(pageSize),
		"domain": keyword,
	}

	result, err := p.request(ctx, "/domain/", params)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if items, ok := result["items"].([]interface{}); ok {
		for _, item := range items {
			if d, ok := item.(map[string]interface{}); ok {
				domain := d["domain"].(string)
				domains = append(domains, dns.DomainInfo{
					ID:          domain,
					Name:        domain,
					RecordCount: 0,
				})
			}
		}
	}

	total := int(result["total"].(float64))
	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	params := map[string]string{
		"act":    "getdnsrecord",
		"domain": p.domain,
		"pageno": strconv.Itoa(page),
		"limit":  strconv.Itoa(pageSize),
	}
	if recordType != "" {
		params["type"] = recordType
	}
	if line != "" {
		params["line"] = line
	}
	if keyword != "" {
		params["host"] = keyword
	}
	if value != "" {
		params["value"] = value
	}
	if subDomain != "" {
		params["host"] = subDomain
	}

	result, err := p.request(ctx, "/domain/", params)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if items, ok := result["items"].([]interface{}); ok {
		for _, item := range items {
			if r, ok := item.(map[string]interface{}); ok {
				statusVal := "enable"
				if pause, ok := r["pause"].(float64); ok && pause == 1 {
					statusVal = "disable"
				}

				var mx int
				if level, ok := r["level"].(float64); ok {
					mx = int(level)
				}

				records = append(records, dns.Record{
					ID:     r["id"].(string),
					Name:   r["item"].(string),
					Type:   r["type"].(string),
					Value:  r["value"].(string),
					TTL:    int(r["ttl"].(float64)),
					Line:   r["line"].(string),
					MX:     mx,
					Status: statusVal,
				})
			}
		}
	}

	total := int(result["total"].(float64))
	return &dns.PageResult{Total: total, Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	if subDomain == "" {
		subDomain = "@"
	}
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	return nil, fmt.Errorf("西部数码不支持获取单条记录")
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if line == "" || line == "default" {
		line = "默认"
	}
	params := map[string]string{
		"act":    "adddnsrecord",
		"domain": p.domain,
		"host":   name,
		"type":   recordType,
		"value":  value,
		"level":  strconv.Itoa(mx),
		"ttl":    strconv.Itoa(ttl),
		"line":   line,
	}

	result, err := p.request(ctx, "/domain/", params)
	if err != nil {
		return "", err
	}

	if id, ok := result["id"].(string); ok {
		return id, nil
	}
	if id, ok := result["id"].(float64); ok {
		return strconv.Itoa(int(id)), nil
	}

	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	params := map[string]string{
		"act":    "moddnsrecord",
		"domain": p.domain,
		"id":     recordID,
		"type":   recordType,
		"value":  value,
		"level":  strconv.Itoa(mx),
		"ttl":    strconv.Itoa(ttl),
		"line":   line,
	}

	_, err := p.request(ctx, "/domain/", params)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("西部数码不支持修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	params := map[string]string{
		"act":    "deldnsrecord",
		"domain": p.domain,
		"id":     recordID,
	}

	_, err := p.request(ctx, "/domain/", params)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	val := "1"
	if enable {
		val = "0"
	}
	params := map[string]string{
		"act":    "pause",
		"domain": p.domain,
		"id":     recordID,
		"val":    val,
	}

	_, err := p.request(ctx, "/domain/", params)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("西部数码不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "", Name: "默认"},
		{ID: "LTEL", Name: "电信"},
		{ID: "LCNC", Name: "联通"},
		{ID: "LMOB", Name: "移动"},
		{ID: "LEDU", Name: "教育网"},
		{ID: "LSEO", Name: "搜索引擎"},
		{ID: "LFOR", Name: "境外"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 600
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	return fmt.Errorf("西部数码不支持添加域名")
}
