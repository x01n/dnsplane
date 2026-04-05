package dnsla

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"main/internal/dns"
)

func init() {
	dns.Register("dnsla", NewProvider, dns.ProviderConfig{
		Type: "dnsla",
		Name: "DNSLA",
		Icon: "dnsla.png",
		Config: []dns.ConfigField{
			{Name: "APIID", Key: "apiid", Type: "input", Required: true},
			{Name: "API密钥", Key: "apisecret", Type: "input", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 0, Status: true, Redirect: true, Log: false, Weight: true, Page: false, Add: true,
		},
	})
}

const baseURL = "https://api.dns.la"

var typeList = map[int]string{
	1: "A", 2: "NS", 5: "CNAME", 15: "MX", 16: "TXT", 28: "AAAA", 33: "SRV", 257: "CAA", 256: "URL转发",
}

var typeListReverse = map[string]int{
	"A": 1, "NS": 2, "CNAME": 5, "MX": 15, "TXT": 16, "AAAA": 28, "SRV": 33, "CAA": 257,
	"REDIRECT_URL": 256, "FORWARD_URL": 256,
}

type Provider struct {
	apiID     string
	apiSecret string
	domain    string
	domainID  string
	proxy     bool
	client    *http.Client
	lastErr   string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		apiID:     config["apiid"],
		apiSecret: config["apisecret"],
		domain:    domain,
		domainID:  domainID,
		proxy:     config["proxy"] == "1",
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) request(ctx context.Context, method, path string, params map[string]string, body interface{}) (map[string]interface{}, error) {
	// 确保 apiID 和 apiSecret 不为空
	if p.apiID == "" || p.apiSecret == "" {
		return nil, fmt.Errorf("API ID 或 API Secret 不能为空")
	}

	// 去除可能的空格和换行符
	apiID := strings.TrimSpace(p.apiID)
	apiSecret := strings.TrimSpace(p.apiSecret)

	// 生成 Basic 认证 token
	authStr := apiID + ":" + apiSecret
	token := base64.StdEncoding.EncodeToString([]byte(authStr))

	reqURL := baseURL + path

	var reqBody io.Reader

	// POST 和 PUT 请求：参数作为 JSON body（与 PHP 版本一致）
	if method == "POST" || method == "PUT" {
		if params != nil {
			bodyBytes, _ := json.Marshal(params)
			reqBody = strings.NewReader(string(bodyBytes))
		} else if body != nil {
			bodyBytes, _ := json.Marshal(body)
			reqBody = strings.NewReader(string(bodyBytes))
		}
	} else {
		// GET/DELETE 请求：参数作为查询字符串（与 PHP 版本一致）
		if len(params) > 0 {
			values := url.Values{}
			for k, v := range params {
				values.Set(k, v)
			}
			reqURL += "?" + values.Encode()
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Basic "+token)
	// 只在有 body 的请求中设置 Content-Type
	if method == "POST" || method == "PUT" {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
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

	if resp.StatusCode == 401 {
		p.lastErr = "认证失败：请检查 API ID 和 API Secret 是否正确"
		return nil, fmt.Errorf("%s", p.lastErr)
	}

	if resp.StatusCode != 200 {
		p.lastErr = fmt.Sprintf("HTTP 错误: %d", resp.StatusCode)
		return nil, fmt.Errorf("%s", p.lastErr)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code != 200 {
		msg := "未知错误"
		if m, ok := result["msg"].(string); ok {
			msg = m
		}
		p.lastErr = msg
		return nil, fmt.Errorf("%s (错误码: %d)", msg, int(code))
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
		"pageIndex": strconv.Itoa(page),
		"pageSize":  strconv.Itoa(pageSize),
	}

	result, err := p.request(ctx, "GET", "/api/domainList", params, nil)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if results, ok := result["results"].([]interface{}); ok {
		for _, r := range results {
			if d, ok := r.(map[string]interface{}); ok {
				name := d["displayDomain"].(string)
				name = strings.TrimSuffix(name, ".")
				domains = append(domains, dns.DomainInfo{
					ID:          fmt.Sprintf("%v", d["id"]),
					Name:        name,
					RecordCount: 0,
				})
			}
		}
	}

	total := int(result["total"].(float64))
	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) convertTypeID(typeID int, dominant bool) string {
	if typeID == 256 {
		if dominant {
			return "REDIRECT_URL"
		}
		return "FORWARD_URL"
	}
	if name, ok := typeList[typeID]; ok {
		return name
	}
	return "A"
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	params := map[string]string{
		"domainId":  p.domainID,
		"pageIndex": strconv.Itoa(page),
		"pageSize":  strconv.Itoa(pageSize),
	}
	if keyword != "" {
		params["host"] = keyword
	}
	if recordType != "" {
		if typeID, ok := typeListReverse[recordType]; ok {
			params["type"] = strconv.Itoa(typeID)
		}
	}
	if line != "" {
		params["lineId"] = line
	}
	if subDomain != "" {
		params["host"] = subDomain
	}
	if value != "" {
		params["data"] = value
	}

	result, err := p.request(ctx, "GET", "/api/recordList", params, nil)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if results, ok := result["results"].([]interface{}); ok {
		for _, r := range results {
			if rec, ok := r.(map[string]interface{}); ok {
				typeID := int(rec["type"].(float64))
				dominant := false
				if d, ok := rec["dominant"].(bool); ok {
					dominant = d
				}

				statusVal := "enable"
				if disable, ok := rec["disable"].(bool); ok && disable {
					statusVal = "disable"
				}

				var mx int
				if pref, ok := rec["preference"].(float64); ok {
					mx = int(pref)
				}

				var weight int
				if w, ok := rec["weight"].(float64); ok {
					weight = int(w)
				}

				records = append(records, dns.Record{
					ID:     fmt.Sprintf("%v", rec["id"]),
					Name:   rec["host"].(string),
					Type:   p.convertTypeID(typeID, dominant),
					Value:  rec["data"].(string),
					TTL:    int(rec["ttl"].(float64)),
					Line:   fmt.Sprintf("%v", rec["lineId"]),
					MX:     mx,
					Weight: weight,
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
	return nil, fmt.Errorf("DNSLA不支持获取单条记录")
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if line == "" || line == "default" {
		line = "0" // DNSLA 默认线路ID
	}
	body := map[string]interface{}{
		"domainId": p.domainID,
		"type":     typeListReverse[recordType],
		"host":     name,
		"data":     value,
		"ttl":      ttl,
		"lineId":   line,
	}

	if recordType == "MX" {
		body["preference"] = mx
	}
	if recordType == "REDIRECT_URL" {
		body["type"] = 256
		body["dominant"] = true
	} else if recordType == "FORWARD_URL" {
		body["type"] = 256
		body["dominant"] = false
	}
	if weight != nil && *weight > 0 {
		body["weight"] = *weight
	}

	result, err := p.request(ctx, "POST", "/api/record", nil, body)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%v", result["id"]), nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	body := map[string]interface{}{
		"id":     recordID,
		"type":   typeListReverse[recordType],
		"host":   name,
		"data":   value,
		"ttl":    ttl,
		"lineId": line,
	}

	if recordType == "MX" {
		body["preference"] = mx
	}
	if recordType == "REDIRECT_URL" {
		body["type"] = 256
		body["dominant"] = true
	} else if recordType == "FORWARD_URL" {
		body["type"] = 256
		body["dominant"] = false
	}
	if weight != nil && *weight > 0 {
		body["weight"] = *weight
	}

	_, err := p.request(ctx, "PUT", "/api/record", nil, body)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("DNSLA不支持修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	// DELETE 请求使用查询参数（与 PHP 版本一致）
	params := map[string]string{"id": recordID}
	_, err := p.request(ctx, "DELETE", "/api/record", params, nil)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	body := map[string]interface{}{
		"id":      recordID,
		"disable": !enable,
	}
	_, err := p.request(ctx, "PUT", "/api/recordDisable", nil, body)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("DNSLA不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	params := map[string]string{"domain": p.domain}
	result, err := p.request(ctx, "GET", "/api/availableLine", params, nil)
	if err != nil {
		return nil, err
	}

	var lines []dns.RecordLine
	if data, ok := result["data"].([]interface{}); ok {
		for _, item := range data {
			if l, ok := item.(map[string]interface{}); ok {
				id := fmt.Sprintf("%v", l["id"])
				if id == "0" {
					id = ""
				}
				lines = append(lines, dns.RecordLine{
					ID:   id,
					Name: l["value"].(string),
				})
			}
		}
	}

	return lines, nil
}

func (p *Provider) GetMinTTL() int {
	return 60
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	body := map[string]interface{}{"domain": domain}
	_, err := p.request(ctx, "POST", "/api/domain", nil, body)
	return err
}
