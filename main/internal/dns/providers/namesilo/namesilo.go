package namesilo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"main/internal/dns"
)

func init() {
	dns.Register("namesilo", NewProvider, dns.ProviderConfig{
		Type: "namesilo",
		Name: "NameSilo",
		Icon: "namesilo.png",
		Config: []dns.ConfigField{
			{Name: "账户名", Key: "username", Type: "input", Required: true},
			{Name: "API Key", Key: "apikey", Type: "input", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 0, Status: false, Redirect: false, Log: false, Weight: false, Page: true, Add: false,
		},
	})
}

const baseURL = "https://www.namesilo.com/api/"

type Provider struct {
	apiKey  string
	domain  string
	proxy   bool
	client  *http.Client
	lastErr string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		apiKey: config["apikey"],
		domain: domain,
		proxy:  config["proxy"] == "1",
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) request(ctx context.Context, operation string, params map[string]string) (map[string]interface{}, error) {
	values := url.Values{}
	values.Set("version", "1")
	values.Set("type", "json")
	values.Set("key", p.apiKey)

	for k, v := range params {
		values.Set(k, v)
	}

	reqURL := baseURL + operation + "?" + values.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if reply, ok := result["reply"].(map[string]interface{}); ok {
		if code, ok := reply["code"].(float64); ok {
			if code == 300 {
				return reply, nil
			}
			detail := "未知错误"
			if d, ok := reply["detail"].(string); ok {
				detail = d
			}
			p.lastErr = detail
			return nil, fmt.Errorf("%s", detail)
		}
	}

	p.lastErr = string(respBody)
	return nil, fmt.Errorf("解析响应失败")
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	params := map[string]string{
		"page":     fmt.Sprintf("%d", page),
		"pageSize": fmt.Sprintf("%d", pageSize),
	}

	result, err := p.request(ctx, "listDomains", params)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if domainsData, ok := result["domains"].(map[string]interface{}); ok {
		if domainList, ok := domainsData["domain"].([]interface{}); ok {
			for _, d := range domainList {
				if domain, ok := d.(string); ok {
					domains = append(domains, dns.DomainInfo{
						ID:          domain,
						Name:        domain,
						RecordCount: 0,
					})
				}
			}
		}
	}

	total := len(domains)
	if pager, ok := result["pager"].(map[string]interface{}); ok {
		if t, ok := pager["total"].(float64); ok {
			total = int(t)
		}
	}

	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	params := map[string]string{"domain": p.domain}

	result, err := p.request(ctx, "dnsListRecords", params)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if rrData, ok := result["resource_record"].([]interface{}); ok {
		for _, r := range rrData {
			if rec, ok := r.(map[string]interface{}); ok {
				host := rec["host"].(string)
				name := host
				if host == p.domain {
					name = "@"
				} else {
					name = strings.TrimSuffix(host, "."+p.domain)
				}

				var mx int
				if distance, ok := rec["distance"].(float64); ok {
					mx = int(distance)
				}

				records = append(records, dns.Record{
					ID:     rec["record_id"].(string),
					Name:   name,
					Type:   rec["type"].(string),
					Value:  rec["value"].(string),
					TTL:    int(rec["ttl"].(float64)),
					Line:   "default",
					MX:     mx,
					Status: "enable",
				})
			}
		}
	}

	// Client-side filtering
	if subDomain != "" {
		var filtered []dns.Record
		for _, r := range records {
			if strings.EqualFold(r.Name, subDomain) {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	} else {
		if keyword != "" {
			var filtered []dns.Record
			for _, r := range records {
				if strings.Contains(r.Name, keyword) || strings.Contains(r.Value, keyword) {
					filtered = append(filtered, r)
				}
			}
			records = filtered
		}
		if value != "" {
			var filtered []dns.Record
			for _, r := range records {
				if r.Value == value {
					filtered = append(filtered, r)
				}
			}
			records = filtered
		}
		if recordType != "" {
			var filtered []dns.Record
			for _, r := range records {
				if r.Type == recordType {
					filtered = append(filtered, r)
				}
			}
			records = filtered
		}
	}

	return &dns.PageResult{Total: len(records), Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	return nil, fmt.Errorf("NameSilo不支持获取单条记录")
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if name == "@" {
		name = ""
	}

	params := map[string]string{
		"domain":  p.domain,
		"rrtype":  recordType,
		"rrhost":  name,
		"rrvalue": value,
		"rrttl":   fmt.Sprintf("%d", ttl),
	}

	if recordType == "MX" {
		params["rrdistance"] = fmt.Sprintf("%d", mx)
	}

	result, err := p.request(ctx, "dnsAddRecord", params)
	if err != nil {
		return "", err
	}

	if recordID, ok := result["record_id"].(string); ok {
		return recordID, nil
	}

	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	if name == "@" {
		name = ""
	}

	params := map[string]string{
		"domain":  p.domain,
		"rrid":    recordID,
		"rrtype":  recordType,
		"rrhost":  name,
		"rrvalue": value,
		"rrttl":   fmt.Sprintf("%d", ttl),
	}

	if recordType == "MX" {
		params["rrdistance"] = fmt.Sprintf("%d", mx)
	}

	_, err := p.request(ctx, "dnsUpdateRecord", params)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("NameSilo不支持修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	params := map[string]string{
		"domain": p.domain,
		"rrid":   recordID,
	}

	_, err := p.request(ctx, "dnsDeleteRecord", params)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	return fmt.Errorf("NameSilo不支持设置记录状态")
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("NameSilo不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "default", Name: "默认"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 3600
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	return fmt.Errorf("NameSilo不支持添加域名")
}
