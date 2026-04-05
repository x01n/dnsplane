package baidu

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"main/internal/dns"

	"github.com/google/uuid"
)

func init() {
	dns.Register("baidu", NewProvider, dns.ProviderConfig{
		Type: "baidu",
		Name: "百度云",
		Icon: "baidu.png",
		Config: []dns.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 2, Status: false, Redirect: false, Log: false, Weight: false, Page: true, Add: true,
		},
	})
}

const endpoint = "dns.baidubce.com"

type Provider struct {
	accessKeyID     string
	secretAccessKey string
	domain          string
	domainID        string
	proxy           bool
	client          *http.Client
	lastErr         string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		accessKeyID:     config["AccessKeyId"],
		secretAccessKey: config["SecretAccessKey"],
		domain:          domain,
		domainID:        domainID,
		proxy:           config["proxy"] == "1",
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) escape(str string) string {
	escaped := url.QueryEscape(str)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func (p *Provider) getCanonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	uri := strings.ReplaceAll(p.escape(path), "%2F", "/")
	if !strings.HasPrefix(uri, "/") {
		uri = "/" + uri
	}
	return uri
}

func (p *Provider) getCanonicalQueryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	var keys []string
	for k := range params {
		if k == "authorization" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, p.escape(k)+"="+p.escape(params[k]))
	}
	return strings.Join(parts, "&")
}

func (p *Provider) getCanonicalHeaders(headers map[string]string) (string, string) {
	lowerHeaders := make(map[string]string)
	for k, v := range headers {
		lowerHeaders[strings.ToLower(k)] = strings.TrimSpace(v)
	}

	var keys []string
	for k := range lowerHeaders {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonicalParts []string
	var signedParts []string
	for _, k := range keys {
		canonicalParts = append(canonicalParts, p.escape(k)+":"+p.escape(lowerHeaders[k]))
		signedParts = append(signedParts, k)
	}
	return strings.Join(canonicalParts, "\n"), strings.Join(signedParts, ";")
}

func (p *Provider) generateSign(method, path string, query map[string]string, headers map[string]string, timestamp time.Time) string {
	algorithm := "bce-auth-v1"
	date := timestamp.UTC().Format("2006-01-02T15:04:05Z")
	expirationInSeconds := 1800

	authString := fmt.Sprintf("%s/%s/%s/%d", algorithm, p.accessKeyID, date, expirationInSeconds)

	canonicalURI := p.getCanonicalURI(path)
	canonicalQueryString := p.getCanonicalQueryString(query)
	canonicalHeaders, signedHeaders := p.getCanonicalHeaders(headers)

	canonicalRequest := method + "\n" + canonicalURI + "\n" + canonicalQueryString + "\n" + canonicalHeaders

	signingKey := hmacSHA256(p.secretAccessKey, authString)
	signature := hex.EncodeToString(hmacSHA256Bytes([]byte(signingKey), canonicalRequest))

	return authString + "/" + signedHeaders + "/" + signature
}

func hmacSHA256(key, data string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func hmacSHA256Bytes(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func (p *Provider) request(ctx context.Context, method, path string, query map[string]string, body interface{}) (map[string]interface{}, error) {
	timestamp := time.Now()
	date := timestamp.UTC().Format("2006-01-02T15:04:05Z")

	var bodyStr string
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyStr = string(bodyBytes)
	}

	headers := map[string]string{
		"Host":       endpoint,
		"x-bce-date": date,
	}
	if bodyStr != "" {
		headers["Content-Type"] = "application/json"
	}

	authorization := p.generateSign(method, path, query, headers, timestamp)
	headers["Authorization"] = authorization

	reqURL := "https://" + endpoint + path
	if len(query) > 0 {
		values := url.Values{}
		for k, v := range query {
			values.Set(k, v)
		}
		reqURL += "?" + values.Encode()
	}

	var reqBody io.Reader
	if bodyStr != "" {
		reqBody = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
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

	if len(respBody) == 0 {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil, nil
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if code, ok := result["code"].(string); ok {
		if msg, ok := result["message"].(string); ok {
			p.lastErr = msg
			return nil, fmt.Errorf("%s: %s", code, msg)
		}
	}

	return result, nil
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	query := map[string]string{}
	if keyword != "" {
		query["name"] = keyword
	}

	result, err := p.request(ctx, "GET", "/v1/dns/zone", query, nil)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if zones, ok := result["zones"].([]interface{}); ok {
		for _, z := range zones {
			if zone, ok := z.(map[string]interface{}); ok {
				name := zone["name"].(string)
				name = strings.TrimSuffix(name, ".")
				domains = append(domains, dns.DomainInfo{
					ID:          zone["id"].(string),
					Name:        name,
					RecordCount: 0,
				})
			}
		}
	}

	return &dns.PageResult{Total: len(domains), Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	query := map[string]string{}
	if subDomain != "" {
		query["rr"] = strings.ToLower(subDomain)
	}

	result, err := p.request(ctx, "GET", "/v1/dns/zone/"+p.domain+"/record", query, nil)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if recs, ok := result["records"].([]interface{}); ok {
		for _, r := range recs {
			if rec, ok := r.(map[string]interface{}); ok {
				statusVal := "disable"
				if rec["status"].(string) == "running" {
					statusVal = "enable"
				}

				var mx int
				if priority, ok := rec["priority"].(float64); ok {
					mx = int(priority)
				}

				var remark string
				if desc, ok := rec["description"].(string); ok {
					remark = desc
				}

				record := dns.Record{
					ID:     rec["id"].(string),
					Name:   rec["rr"].(string),
					Type:   rec["type"].(string),
					Value:  rec["value"].(string),
					TTL:    int(rec["ttl"].(float64)),
					Line:   rec["line"].(string),
					MX:     mx,
					Status: statusVal,
					Remark: remark,
				}
				records = append(records, record)
			}
		}
	}

	// 客户端过滤
	if subDomain != "" {
		subDomain = strings.ToLower(subDomain)
		var filtered []dns.Record
		for _, r := range records {
			if strings.ToLower(r.Name) == subDomain {
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
		if status != "" {
			var filtered []dns.Record
			for _, r := range records {
				if (status == "1" && r.Status == "enable") || (status == "0" && r.Status == "disable") {
					filtered = append(filtered, r)
				}
			}
			records = filtered
		}
	}

	return &dns.PageResult{Total: len(records), Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	if subDomain == "" {
		subDomain = "@"
	}
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	query := map[string]string{"id": recordID}
	result, err := p.request(ctx, "GET", "/v1/dns/zone/"+p.domain+"/record", query, nil)
	if err != nil {
		return nil, err
	}

	if recs, ok := result["records"].([]interface{}); ok && len(recs) > 0 {
		if rec, ok := recs[0].(map[string]interface{}); ok {
			statusVal := "disable"
			if rec["status"].(string) == "running" {
				statusVal = "enable"
			}

			var mx int
			if priority, ok := rec["priority"].(float64); ok {
				mx = int(priority)
			}

			var remark string
			if desc, ok := rec["description"].(string); ok {
				remark = desc
			}

			return &dns.Record{
				ID:     rec["id"].(string),
				Name:   rec["rr"].(string),
				Type:   rec["type"].(string),
				Value:  rec["value"].(string),
				TTL:    int(rec["ttl"].(float64)),
				Line:   rec["line"].(string),
				MX:     mx,
				Status: statusVal,
				Remark: remark,
			}, nil
		}
	}

	return nil, fmt.Errorf("记录不存在")
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if line == "" {
		line = "default"
	}
	params := map[string]interface{}{
		"rr":    name,
		"type":  recordType,
		"value": value,
		"line":  line,
		"ttl":   ttl,
	}
	if remark != "" {
		params["description"] = remark
	}
	if recordType == "MX" {
		params["priority"] = mx
	}

	query := map[string]string{"clientToken": uuid.New().String()}
	_, err := p.request(ctx, "POST", "/v1/dns/zone/"+p.domain+"/record", query, params)
	if err != nil {
		return "", err
	}

	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	params := map[string]interface{}{
		"rr":    name,
		"type":  recordType,
		"value": value,
		"line":  line,
		"ttl":   ttl,
	}
	if remark != "" {
		params["description"] = remark
	}
	if recordType == "MX" {
		params["priority"] = mx
	}

	query := map[string]string{"clientToken": uuid.New().String()}
	_, err := p.request(ctx, "PUT", "/v1/dns/zone/"+p.domain+"/record/"+recordID, query, params)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("百度云不支持单独修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	query := map[string]string{"clientToken": uuid.New().String()}
	_, err := p.request(ctx, "DELETE", "/v1/dns/zone/"+p.domain+"/record/"+recordID, query, nil)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	action := "disable"
	if enable {
		action = "enable"
	}
	query := map[string]string{
		action:        "",
		"clientToken": uuid.New().String(),
	}
	_, err := p.request(ctx, "PUT", "/v1/dns/zone/"+p.domain+"/record/"+recordID, query, nil)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("百度云不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "default", Name: "默认"},
		{ID: "ct", Name: "电信"},
		{ID: "cnc", Name: "联通"},
		{ID: "cmnet", Name: "移动"},
		{ID: "edu", Name: "教育网"},
		{ID: "search", Name: "搜索引擎(百度)"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 60
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	query := map[string]string{
		"clientToken": uuid.New().String(),
		"name":        domain,
	}
	_, err := p.request(ctx, "POST", "/v1/dns/zone", nil, query)
	return err
}
