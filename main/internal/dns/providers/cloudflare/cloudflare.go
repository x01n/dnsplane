package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/dns"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func init() {
	dns.Register("cloudflare", NewProvider, dns.ProviderConfig{
		Type: "cloudflare",
		Name: "Cloudflare",
		Icon: "cloudflare.ico",
		Config: []dns.ConfigField{
			{Name: "邮箱地址", Key: "email", Type: "input", Required: false},
			{Name: "API密钥/令牌", Key: "apikey", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 2, Status: true, Redirect: false, Log: false, Weight: false, Page: false, Add: true,
		},
	})
}

const baseURL = "https://api.cloudflare.com/client/v4"

type Provider struct {
	email    string
	apiKey   string
	domain   string
	domainID string
	client   *http.Client
	lastErr  string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		email:    config["email"],
		apiKey:   config["apikey"],
		domain:   domain,
		domainID: domainID,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

/* isGlobalAPIKey 判断是否为 Global API Key（纯十六进制字符） */
func (p *Provider) isGlobalAPIKey() bool {
	matched, _ := regexp.MatchString(`^[0-9a-fA-F]+$`, p.apiKey)
	return matched
}

func (p *Provider) request(ctx context.Context, method, path string, params map[string]string, body interface{}) (map[string]interface{}, error) {
	reqURL := baseURL + path

	// GET/DELETE 请求：参数作为查询字符串
	if (method == "GET" || method == "DELETE") && len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			if v != "" {
				values.Set(k, v)
			}
		}
		if encoded := values.Encode(); encoded != "" {
			reqURL += "?" + encoded
		}
	}

	var reqBody io.Reader
	// POST/PUT/PATCH 请求：参数作为 JSON body
	if method == "POST" || method == "PUT" || method == "PATCH" {
		if body != nil {
			bodyBytes, _ := json.Marshal(body)
			reqBody = strings.NewReader(string(bodyBytes))
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	// 设置认证头
	if p.isGlobalAPIKey() {
		req.Header.Set("X-Auth-Email", p.email)
		req.Header.Set("X-Auth-Key", p.apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	if method == "POST" || method == "PUT" || method == "PATCH" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	// 检查 Cloudflare API 返回的 success 字段
	if success, ok := result["success"].(bool); ok && !success {
		if errors, ok := result["errors"].([]interface{}); ok && len(errors) > 0 {
			if errObj, ok := errors[0].(map[string]interface{}); ok {
				if msg, ok := errObj["message"].(string); ok {
					p.lastErr = msg
					return nil, fmt.Errorf("%s", msg)
				}
			}
		}
		p.lastErr = "未知错误"
		return nil, fmt.Errorf("未知错误")
	}

	return result, nil
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(pageSize),
	}
	if keyword != "" {
		params["name"] = keyword
	}

	result, err := p.request(ctx, "GET", "/zones", params, nil)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if data, ok := result["result"].([]interface{}); ok {
		for _, item := range data {
			if zone, ok := item.(map[string]interface{}); ok {
				domains = append(domains, dns.DomainInfo{
					ID:     zone["id"].(string),
					Name:   zone["name"].(string),
					Status: zone["status"].(string),
				})
			}
		}
	}

	total := 0
	if info, ok := result["result_info"].(map[string]interface{}); ok {
		if t, ok := info["total_count"].(float64); ok {
			total = int(t)
		}
	}

	return &dns.PageResult{
		Total:   total,
		Records: domains,
	}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(pageSize),
	}

	if keyword != "" {
		params["search"] = keyword
	}
	if value != "" {
		params["search"] = value
	}
	if recordType != "" {
		params["type"] = recordType
	}
	if subDomain != "" {
		if subDomain == "@" {
			params["name"] = p.domain
		} else {
			params["name"] = subDomain + "." + p.domain
		}
	}
	if line != "" {
		if line == "1" {
			params["proxied"] = "true"
		} else {
			params["proxied"] = "false"
		}
	}

	result, err := p.request(ctx, "GET", "/zones/"+p.domainID+"/dns_records", params, nil)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if data, ok := result["result"].([]interface{}); ok {
		for _, item := range data {
			if rec, ok := item.(map[string]interface{}); ok {
				name := rec["name"].(string)
				if name == p.domain {
					name = "@"
				} else {
					name = strings.TrimSuffix(name, "."+p.domain)
				}

				statusVal := "enable"
				if strings.HasSuffix(name, "_pause") {
					statusVal = "disable"
					name = strings.TrimSuffix(name, "_pause")
				}

				lineVal := "0"
				if proxied, ok := rec["proxied"].(bool); ok && proxied {
					lineVal = "1"
				}

				var mx int
				if priority, ok := rec["priority"].(float64); ok {
					mx = int(priority)
				}

				remark := ""
				if comment, ok := rec["comment"].(string); ok {
					remark = comment
				}

				records = append(records, dns.Record{
					ID:     rec["id"].(string),
					Name:   name,
					Type:   rec["type"].(string),
					Value:  rec["content"].(string),
					TTL:    int(rec["ttl"].(float64)),
					Line:   lineVal,
					Status: statusVal,
					MX:     mx,
					Remark: remark,
				})
			}
		}
	}

	total := 0
	if info, ok := result["result_info"].(map[string]interface{}); ok {
		if t, ok := info["total_count"].(float64); ok {
			total = int(t)
		}
	}

	return &dns.PageResult{
		Total:   total,
		Records: records,
	}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	result, err := p.request(ctx, "GET", "/zones/"+p.domainID+"/dns_records/"+recordID, nil, nil)
	if err != nil {
		return nil, err
	}

	rec, ok := result["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("解析记录信息失败")
	}

	name := rec["name"].(string)
	if name == p.domain {
		name = "@"
	} else {
		name = strings.TrimSuffix(name, "."+p.domain)
	}

	statusVal := "enable"
	if strings.HasSuffix(name, "_pause") {
		statusVal = "disable"
		name = strings.TrimSuffix(name, "_pause")
	}

	lineVal := "0"
	if proxied, ok := rec["proxied"].(bool); ok && proxied {
		lineVal = "1"
	}

	var mx int
	if priority, ok := rec["priority"].(float64); ok {
		mx = int(priority)
	}

	remark := ""
	if comment, ok := rec["comment"].(string); ok {
		remark = comment
	}

	return &dns.Record{
		ID:     rec["id"].(string),
		Name:   name,
		Type:   rec["type"].(string),
		Value:  rec["content"].(string),
		TTL:    int(rec["ttl"].(float64)),
		Line:   lineVal,
		Status: statusVal,
		MX:     mx,
		Remark: remark,
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	fullName := name
	if name == "@" || name == "" {
		fullName = p.domain
	} else {
		fullName = name + "." + p.domain
	}

	proxied := line == "1"
	body := map[string]interface{}{
		"name":    fullName,
		"type":    recordType,
		"content": value,
		"ttl":     ttl,
		"proxied": proxied,
		"comment": remark,
	}

	if recordType == "MX" {
		body["priority"] = mx
	}

	result, err := p.request(ctx, "POST", "/zones/"+p.domainID+"/dns_records", nil, body)
	if err != nil {
		return "", err
	}

	if rec, ok := result["result"].(map[string]interface{}); ok {
		return rec["id"].(string), nil
	}

	return "", fmt.Errorf("创建记录失败")
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	fullName := name
	if name == "@" || name == "" {
		fullName = p.domain
	} else {
		fullName = name + "." + p.domain
	}

	proxied := line == "1"
	body := map[string]interface{}{
		"name":    fullName,
		"type":    recordType,
		"content": value,
		"ttl":     ttl,
		"proxied": proxied,
		"comment": remark,
	}

	if recordType == "MX" {
		body["priority"] = mx
	}

	_, err := p.request(ctx, "PATCH", "/zones/"+p.domainID+"/dns_records/"+recordID, nil, body)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	info, err := p.GetDomainRecordInfo(ctx, recordID)
	if err != nil {
		return err
	}
	return p.UpdateDomainRecord(ctx, recordID, info.Name, info.Type, info.Value, info.Line, info.TTL, info.MX, nil, remark)
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	_, err := p.request(ctx, "DELETE", "/zones/"+p.domainID+"/dns_records/"+recordID, nil, nil)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	info, err := p.GetDomainRecordInfo(ctx, recordID)
	if err != nil {
		return err
	}

	targetName := info.Name
	if !enable {
		if !strings.HasSuffix(targetName, "_pause") {
			targetName += "_pause"
		}
	} else {
		targetName = strings.TrimSuffix(targetName, "_pause")
	}

	return p.UpdateDomainRecord(ctx, recordID, targetName, info.Type, info.Value, info.Line, info.TTL, info.MX, nil, info.Remark)
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("Cloudflare不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "0", Name: "仅DNS"},
		{ID: "1", Name: "已代理"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 60
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	body := map[string]interface{}{"name": domain}
	_, err := p.request(ctx, "POST", "/zones", nil, body)
	return err
}
