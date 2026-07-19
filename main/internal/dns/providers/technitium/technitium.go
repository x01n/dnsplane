package technitium

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/dns"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	dns.Register("technitium", NewProvider, dns.ProviderConfig{
		Type: "technitium",
		Name: "Technitium DNS",
		Icon: "technitium.ico",
		Config: []dns.ConfigField{
			{Name: "服务器地址", Key: "url", Type: "input", Required: true, Placeholder: "https://dns.example.com"},
			{Name: "API Token", Key: "token", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 2, Status: true, Redirect: false, Log: false, Weight: false, Page: false, Add: true,
		},
	})
}

type Provider struct {
	baseURL  string
	token    string
	domain   string
	domainID string
	proxy    bool
	client   *http.Client
	lastErr  string
	records  []map[string]interface{} // cached records
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	baseURL := strings.TrimRight(config["url"], "/") + "/api"
	return &Provider{
		baseURL:  baseURL,
		token:    config["token"],
		domain:   domain,
		domainID: domainID,
		proxy:    config["proxy"] == "1",
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) request(ctx context.Context, method, path string, params map[string]string, body map[string]string) (map[string]interface{}, error) {
	if params == nil {
		params = make(map[string]string)
	}
	params["token"] = p.token

	reqURL := p.baseURL + path

	if method == "GET" || method == "DELETE" {
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
	if method == "POST" && len(body) > 0 {
		values := url.Values{}
		for k, v := range body {
			values.Set(k, v)
		}
		reqBody = strings.NewReader(values.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

	if status, ok := result["status"].(string); ok && status != "ok" {
		if errMsg, ok := result["errorMessage"].(string); ok {
			p.lastErr = errMsg
			return nil, fmt.Errorf("%s", errMsg)
		}
		p.lastErr = "API 请求失败"
		return nil, fmt.Errorf("API 请求失败")
	}

	return result, nil
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	result, err := p.request(ctx, "GET", "/zones/list", nil, nil)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if response, ok := result["response"].(map[string]interface{}); ok {
		if zones, ok := response["zones"].([]interface{}); ok {
			for _, item := range zones {
				if zone, ok := item.(map[string]interface{}); ok {
					name := zone["name"].(string)
					if keyword != "" && !strings.Contains(name, keyword) {
						continue
					}
					domains = append(domains, dns.DomainInfo{
						ID:   name,
						Name: name,
					})
				}
			}
		}
	}

	return &dns.PageResult{
		Total:   len(domains),
		Records: domains,
	}, nil
}

func (p *Provider) loadRecords(ctx context.Context) error {
	params := map[string]string{
		"domain":   p.domain,
		"listZone": "true",
	}

	result, err := p.request(ctx, "GET", "/zones/records/get", params, nil)
	if err != nil {
		return err
	}

	if response, ok := result["response"].(map[string]interface{}); ok {
		if records, ok := response["records"].([]interface{}); ok {
			p.records = make([]map[string]interface{}, 0)
			for i, item := range records {
				if rec, ok := item.(map[string]interface{}); ok {
					rec["id"] = i
					p.records = append(p.records, rec)
				}
			}
		}
	}

	return nil
}

func (p *Provider) parseRecordValue(rec map[string]interface{}) (string, int) {
	recordType := rec["type"].(string)
	rData, _ := rec["rData"].(map[string]interface{})

	var value string
	var mx int

	switch recordType {
	case "A", "AAAA":
		if ip, ok := rData["ipAddress"].(string); ok {
			value = ip
		}
	case "CNAME":
		if cname, ok := rData["cname"].(string); ok {
			value = cname
		}
	case "NS":
		if ns, ok := rData["nameServer"].(string); ok {
			value = ns
		}
	case "MX":
		if exchange, ok := rData["exchange"].(string); ok {
			value = exchange
		}
		if pref, ok := rData["preference"].(float64); ok {
			mx = int(pref)
		}
	case "TXT":
		if text, ok := rData["text"].(string); ok {
			value = text
		}
	case "SRV":
		priority := 0
		weight := 0
		port := 0
		target := ""
		if v, ok := rData["priority"].(float64); ok {
			priority = int(v)
		}
		if v, ok := rData["weight"].(float64); ok {
			weight = int(v)
		}
		if v, ok := rData["port"].(float64); ok {
			port = int(v)
		}
		if t, ok := rData["target"].(string); ok {
			target = t
		}
		value = fmt.Sprintf("%d %d %d %s", priority, weight, port, target)
	case "PTR":
		if ptr, ok := rData["ptrName"].(string); ok {
			value = ptr
		}
	case "CAA":
		flags := 0
		tag := ""
		val := ""
		if v, ok := rData["flags"].(float64); ok {
			flags = int(v)
		}
		if t, ok := rData["tag"].(string); ok {
			tag = t
		}
		if v, ok := rData["value"].(string); ok {
			val = v
		}
		value = fmt.Sprintf("%d %s \"%s\"", flags, tag, val)
	case "ANAME":
		if aname, ok := rData["aname"].(string); ok {
			value = aname
		}
	case "DNAME":
		if dname, ok := rData["dname"].(string); ok {
			value = dname
		}
	}

	return value, mx
}

func (p *Provider) buildRecordParams(recordType, value string, mx int) map[string]string {
	params := make(map[string]string)

	switch recordType {
	case "A", "AAAA":
		params["ipAddress"] = value
	case "CNAME":
		params["cname"] = value
	case "NS":
		params["nameServer"] = value
	case "MX":
		params["exchange"] = value
		params["preference"] = strconv.Itoa(mx)
	case "TXT":
		params["text"] = value
	case "SRV":
		parts := strings.Split(value, " ")
		if len(parts) == 4 {
			params["priority"] = parts[0]
			params["weight"] = parts[1]
			params["port"] = parts[2]
			params["target"] = parts[3]
		}
	case "PTR":
		params["ptrName"] = value
	case "CAA":
		parts := strings.SplitN(value, " ", 3)
		if len(parts) == 3 {
			params["flags"] = parts[0]
			params["tag"] = parts[1]
			params["value"] = strings.Trim(parts[2], "\"")
		}
	case "ANAME":
		params["aname"] = value
	case "DNAME":
		params["dname"] = value
	}

	return params
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	if err := p.loadRecords(ctx); err != nil {
		return nil, err
	}

	var records []dns.Record
	for i, rec := range p.records {
		name := rec["name"].(string)
		if name == p.domain {
			name = "@"
		} else {
			name = strings.TrimSuffix(name, "."+p.domain)
		}

		recordVal, mx := p.parseRecordValue(rec)
		recordType := rec["type"].(string)
		ttl := 600
		if t, ok := rec["ttl"].(float64); ok {
			ttl = int(t)
		}

		recordStatus := "enable"
		if disabled, ok := rec["disabled"].(bool); ok && disabled {
			recordStatus = "disable"
		}

		remark := ""
		if comments, ok := rec["comments"].(string); ok {
			remark = comments
		}

		record := dns.Record{
			ID:     strconv.Itoa(i),
			Name:   name,
			Type:   recordType,
			Value:  recordVal,
			TTL:    ttl,
			Line:   "default",
			Status: recordStatus,
			MX:     mx,
			Remark: remark,
		}

		// Apply filters
		if subDomain != "" && !strings.EqualFold(record.Name, subDomain) {
			continue
		}
		if keyword != "" && !strings.Contains(record.Name, keyword) && !strings.Contains(record.Value, keyword) {
			continue
		}
		if value != "" && record.Value != value {
			continue
		}
		if recordType != "" && record.Type != recordType {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}

		records = append(records, record)
	}

	return &dns.PageResult{
		Total:   len(records),
		Records: records,
	}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	if err := p.loadRecords(ctx); err != nil {
		return nil, err
	}

	idx, err := strconv.Atoi(recordID)
	if err != nil || idx < 0 || idx >= len(p.records) {
		return nil, fmt.Errorf("记录不存在")
	}

	rec := p.records[idx]
	name := rec["name"].(string)
	if name == p.domain {
		name = "@"
	} else {
		name = strings.TrimSuffix(name, "."+p.domain)
	}

	recordVal, mx := p.parseRecordValue(rec)
	recordType := rec["type"].(string)
	ttl := 600
	if t, ok := rec["ttl"].(float64); ok {
		ttl = int(t)
	}

	recordStatus := "enable"
	if disabled, ok := rec["disabled"].(bool); ok && disabled {
		recordStatus = "disable"
	}

	remark := ""
	if comments, ok := rec["comments"].(string); ok {
		remark = comments
	}

	return &dns.Record{
		ID:     recordID,
		Name:   name,
		Type:   recordType,
		Value:  recordVal,
		TTL:    ttl,
		Line:   "default",
		Status: recordStatus,
		MX:     mx,
		Remark: remark,
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	domain := name
	if name == "@" || name == "" {
		domain = p.domain
	} else {
		domain = name + "." + p.domain
	}

	params := map[string]string{
		"domain": domain,
		"zone":   p.domain,
		"type":   recordType,
		"ttl":    strconv.Itoa(ttl),
	}

	if remark != "" {
		params["comments"] = remark
	}

	valParams := p.buildRecordParams(recordType, value, mx)
	for k, v := range valParams {
		params[k] = v
	}

	_, err := p.request(ctx, "POST", "/zones/records/add", nil, params)
	if err != nil {
		return "", err
	}

	// Reload records to get new index
	_ = p.loadRecords(ctx)

	return strconv.Itoa(len(p.records) - 1), nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	if err := p.loadRecords(ctx); err != nil {
		return err
	}

	idx, err := strconv.Atoi(recordID)
	if err != nil || idx < 0 || idx >= len(p.records) {
		return fmt.Errorf("记录不存在，请刷新页面重试")
	}

	oldRecord := p.records[idx]
	oldDomain := oldRecord["name"].(string)
	oldType := oldRecord["type"].(string)

	newDomain := name
	if name == "@" || name == "" {
		newDomain = p.domain
	} else {
		newDomain = name + "." + p.domain
	}

	// For APP records, we need to delete and recreate
	if oldType == "APP" {
		oldVal, _ := p.parseRecordValue(oldRecord)
		if oldVal != strings.TrimRight(value, " ") || oldDomain != newDomain {
			_ = p.DeleteDomainRecord(ctx, recordID)
			_, err := p.AddDomainRecord(ctx, name, recordType, value, line, ttl, mx, weight, remark)
			return err
		}
	}

	params := map[string]string{
		"domain": oldDomain,
		"zone":   p.domain,
		"type":   oldType,
		"ttl":    strconv.Itoa(ttl),
	}

	if oldDomain != newDomain {
		params["newDomain"] = newDomain
	}

	if remark != "" {
		params["comments"] = remark
	} else {
		params["comments"] = ""
	}

	// Old value params - use parseRecordValue to correctly extract old values for all types
	oldVal, oldMX := p.parseRecordValue(oldRecord)
	oldValParams := p.buildRecordParams(oldType, oldVal, oldMX)

	// New value params
	newValParams := p.buildRecordParams(recordType, value, mx)
	for k, v := range newValParams {
		params[k] = v
	}
	for k, v := range oldValParams {
		if _, exists := params[k]; !exists {
			params[k] = v
		}
	}

	_, err = p.request(ctx, "POST", "/zones/records/update", nil, params)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	if err := p.loadRecords(ctx); err != nil {
		return err
	}

	idx, err := strconv.Atoi(recordID)
	if err != nil || idx < 0 || idx >= len(p.records) {
		return fmt.Errorf("记录不存在，请刷新页面重试")
	}

	oldRecord := p.records[idx]
	oldDomain := oldRecord["name"].(string)
	oldType := oldRecord["type"].(string)

	params := map[string]string{
		"domain":   oldDomain,
		"zone":     p.domain,
		"type":     oldType,
		"comments": remark,
	}

	// Re-send old values
	oldValParams := p.buildRecordParams(oldType, "", 1)
	rData, _ := oldRecord["rData"].(map[string]interface{})
	for k, v := range rData {
		if str, ok := v.(string); ok {
			oldValParams[k] = str
		}
	}
	for k, v := range oldValParams {
		params[k] = v
	}

	_, err = p.request(ctx, "POST", "/zones/records/update", nil, params)
	return err
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	if err := p.loadRecords(ctx); err != nil {
		return err
	}

	idx, err := strconv.Atoi(recordID)
	if err != nil || idx < 0 || idx >= len(p.records) {
		return fmt.Errorf("记录不存在，请刷新页面重试")
	}

	oldRecord := p.records[idx]
	oldDomain := oldRecord["name"].(string)
	oldType := oldRecord["type"].(string)

	params := map[string]string{
		"domain": oldDomain,
		"zone":   p.domain,
		"type":   oldType,
	}

	// Use parseRecordValue + buildRecordParams to correctly handle all types
	oldVal, oldMX := p.parseRecordValue(oldRecord)
	oldValParams := p.buildRecordParams(oldType, oldVal, oldMX)
	for k, v := range oldValParams {
		params[k] = v
	}

	_, err = p.request(ctx, "POST", "/zones/records/delete", nil, params)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	if err := p.loadRecords(ctx); err != nil {
		return err
	}

	idx, err := strconv.Atoi(recordID)
	if err != nil || idx < 0 || idx >= len(p.records) {
		return fmt.Errorf("记录不存在，请刷新页面重试")
	}

	oldRecord := p.records[idx]
	oldDomain := oldRecord["name"].(string)
	oldType := oldRecord["type"].(string)

	disable := "false"
	if !enable {
		disable = "true"
	}

	params := map[string]string{
		"domain":  oldDomain,
		"zone":    p.domain,
		"type":    oldType,
		"disable": disable,
	}

	// Re-send old values
	rData, _ := oldRecord["rData"].(map[string]interface{})
	for k, v := range rData {
		if str, ok := v.(string); ok {
			params[k] = str
		}
	}

	_, err = p.request(ctx, "POST", "/zones/records/update", nil, params)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("Technitium DNS 不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "default", Name: "默认"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 0
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	params := map[string]string{
		"zone": domain,
		"type": "Primary",
	}

	result, err := p.request(ctx, "POST", "/zones/create", nil, params)
	if err != nil {
		return err
	}

	if response, ok := result["response"].(map[string]interface{}); ok {
		if _, ok := response["domain"].(string); ok {
			return nil
		}
	}

	return fmt.Errorf("添加域名失败")
}
