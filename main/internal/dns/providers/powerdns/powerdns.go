package powerdns

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"main/internal/dns"
)

func init() {
	dns.Register("powerdns", NewProvider, dns.ProviderConfig{
		Type: "powerdns",
		Name: "PowerDNS",
		Icon: "powerdns.png",
		Config: []dns.ConfigField{
			{Name: "IP地址", Key: "ip", Type: "input", Required: true},
			{Name: "端口", Key: "port", Type: "input", Required: true},
			{Name: "API KEY", Key: "apikey", Type: "input", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 2, Status: true, Redirect: false, Log: false, Weight: false, Page: true, Add: true,
		},
	})
}

type Provider struct {
	url      string
	apiKey   string
	serverID string
	domain   string
	domainID string
	proxy    bool
	client   *http.Client
	lastErr  string
	cache    map[string][]rrset
	cacheMu  sync.RWMutex
}

type rrset struct {
	ID       int
	Name     string
	Host     string
	Type     string
	TTL      int
	Records  []record
	Comments []comment
}

type record struct {
	ID       int
	Content  string
	Disabled bool
}

type comment struct {
	Account string `json:"account"`
	Content string `json:"content"`
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		url:      "http://" + config["ip"] + ":" + config["port"] + "/api/v1",
		apiKey:   config["apikey"],
		serverID: "localhost",
		domain:   domain,
		domainID: domainID,
		proxy:    config["proxy"] == "1",
		client:   &http.Client{Timeout: 30 * time.Second},
		cache:    make(map[string][]rrset),
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) request(ctx context.Context, method, path string, body interface{}) (interface{}, error) {
	reqURL := p.url + path

	var reqBody io.Reader
	if body != nil {
		bodyBytes, _ := json.Marshal(body)
		reqBody = strings.NewReader(string(bodyBytes))
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", p.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

	if resp.StatusCode >= 400 {
		var errResult map[string]interface{}
		if json.Unmarshal(respBody, &errResult) == nil {
			if errMsg, ok := errResult["error"].(string); ok {
				p.lastErr = errMsg
				return nil, fmt.Errorf("%s", errMsg)
			}
		}
		p.lastErr = string(respBody)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if len(respBody) == 0 {
		return true, nil
	}

	var result interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	result, err := p.request(ctx, "GET", "/servers/"+p.serverID+"/zones", nil)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if zones, ok := result.([]interface{}); ok {
		for _, z := range zones {
			if zone, ok := z.(map[string]interface{}); ok {
				name := strings.TrimSuffix(zone["name"].(string), ".")
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

func (p *Provider) loadRRSets(ctx context.Context) ([]rrset, error) {
	p.cacheMu.RLock()
	if cached, ok := p.cache[p.domainID]; ok {
		p.cacheMu.RUnlock()
		return cached, nil
	}
	p.cacheMu.RUnlock()

	result, err := p.request(ctx, "GET", "/servers/"+p.serverID+"/zones/"+p.domainID, nil)
	if err != nil {
		return nil, err
	}

	var rrsets []rrset
	if zone, ok := result.(map[string]interface{}); ok {
		if rrsetsData, ok := zone["rrsets"].([]interface{}); ok {
			rrsetID := 0
			for _, rs := range rrsetsData {
				if r, ok := rs.(map[string]interface{}); ok {
					rrsetID++
					name := r["name"].(string)
					host := name
					if name == p.domainID {
						host = "@"
					} else {
						host = strings.TrimSuffix(name, "."+p.domainID)
					}

					var records []record
					recordID := 0
					if recsData, ok := r["records"].([]interface{}); ok {
						for _, rec := range recsData {
							if recMap, ok := rec.(map[string]interface{}); ok {
								recordID++
								disabled := false
								if d, ok := recMap["disabled"].(bool); ok {
									disabled = d
								}
								records = append(records, record{
									ID:       recordID,
									Content:  recMap["content"].(string),
									Disabled: disabled,
								})
							}
						}
					}

					var comments []comment
					if commentsData, ok := r["comments"].([]interface{}); ok {
						for _, c := range commentsData {
							if cMap, ok := c.(map[string]interface{}); ok {
								comments = append(comments, comment{
									Content: cMap["content"].(string),
								})
							}
						}
					}

					rrsets = append(rrsets, rrset{
						ID:       rrsetID,
						Name:     name,
						Host:     host,
						Type:     r["type"].(string),
						TTL:      int(r["ttl"].(float64)),
						Records:  records,
						Comments: comments,
					})
				}
			}
		}
	}

	p.cacheMu.Lock()
	p.cache[p.domainID] = rrsets
	p.cacheMu.Unlock()

	return rrsets, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	rrsets, err := p.loadRRSets(ctx)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	for _, rs := range rrsets {
		for _, rec := range rs.Records {
			recValue := rec.Content
			var mx int
			if rs.Type == "MX" {
				parts := strings.SplitN(rec.Content, " ", 2)
				if len(parts) == 2 {
					fmt.Sscanf(parts[0], "%d", &mx)
					recValue = parts[1]
				}
			}

			statusVal := "enable"
			if rec.Disabled {
				statusVal = "disable"
			}

			var remark string
			if len(rs.Comments) > 0 {
				remark = rs.Comments[0].Content
			}

			records = append(records, dns.Record{
				ID:     fmt.Sprintf("%d_%d", rs.ID, rec.ID),
				Name:   rs.Host,
				Type:   rs.Type,
				Value:  recValue,
				TTL:    rs.TTL,
				Line:   "default",
				MX:     mx,
				Status: statusVal,
				Remark: remark,
			})
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
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	return nil, fmt.Errorf("PowerDNS不支持获取单条记录")
}

func (p *Provider) rrsetReplace(ctx context.Context, host, recordType string, ttl int, records []map[string]interface{}, remark string) error {
	name := host
	if host == "@" {
		name = p.domainID
	} else {
		name = host + "." + p.domainID
	}

	rrset := map[string]interface{}{
		"name":       name,
		"type":       recordType,
		"ttl":        ttl,
		"changetype": "REPLACE",
		"records":    records,
		"comments":   []map[string]string{},
	}

	if remark != "" {
		rrset["comments"] = []map[string]string{
			{"account": "", "content": remark},
		}
	}

	body := map[string]interface{}{
		"rrsets": []map[string]interface{}{rrset},
	}

	_, err := p.request(ctx, "PATCH", "/servers/"+p.serverID+"/zones/"+p.domainID, body)
	if err == nil {
		p.cacheMu.Lock()
		delete(p.cache, p.domainID)
		p.cacheMu.Unlock()
	}
	return err
}

func (p *Provider) rrsetDelete(ctx context.Context, host, recordType string) error {
	name := host
	if host == "@" {
		name = p.domainID
	} else {
		name = host + "." + p.domainID
	}

	body := map[string]interface{}{
		"rrsets": []map[string]interface{}{
			{
				"name":       name,
				"type":       recordType,
				"changetype": "DELETE",
			},
		},
	}

	_, err := p.request(ctx, "PATCH", "/servers/"+p.serverID+"/zones/"+p.domainID, body)
	if err == nil {
		p.cacheMu.Lock()
		delete(p.cache, p.domainID)
		p.cacheMu.Unlock()
	}
	return err
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if recordType == "TXT" && !strings.HasPrefix(value, "\"") {
		value = "\"" + value + "\""
	}
	if (recordType == "CNAME" || recordType == "MX") && !strings.HasSuffix(value, ".") {
		value += "."
	}
	if recordType == "MX" {
		value = strconv.Itoa(mx) + " " + value
	}

	rrsets, _ := p.loadRRSets(ctx)
	var existingRecords []map[string]interface{}

	for _, rs := range rrsets {
		if rs.Host == name && rs.Type == recordType {
			for _, rec := range rs.Records {
				if rec.Content == value {
					p.lastErr = "已存在相同记录"
					return "", fmt.Errorf("已存在相同记录")
				}
				existingRecords = append(existingRecords, map[string]interface{}{
					"content":  rec.Content,
					"disabled": rec.Disabled,
				})
			}
			break
		}
	}

	existingRecords = append(existingRecords, map[string]interface{}{
		"content":  value,
		"disabled": false,
	})

	return "", p.rrsetReplace(ctx, name, recordType, ttl, existingRecords, remark)
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	if recordType == "TXT" && !strings.HasPrefix(value, "\"") {
		value = "\"" + value + "\""
	}
	if (recordType == "CNAME" || recordType == "MX") && !strings.HasSuffix(value, ".") {
		value += "."
	}
	if recordType == "MX" {
		value = strconv.Itoa(mx) + " " + value
	}

	parts := strings.Split(recordID, "_")
	if len(parts) != 2 {
		return fmt.Errorf("无效的记录ID")
	}
	rrsetID, _ := strconv.Atoi(parts[0])
	recID, _ := strconv.Atoi(parts[1])

	rrsets, _ := p.loadRRSets(ctx)

	for _, rs := range rrsets {
		if rs.ID == rrsetID {
			var newRecords []map[string]interface{}
			found := false
			needAdd := false

			for _, rec := range rs.Records {
				if rec.ID == recID {
					found = true
					if rs.Host == name && rs.Type == recordType {
						newRecords = append(newRecords, map[string]interface{}{
							"content":  value,
							"disabled": rec.Disabled,
						})
					} else {
						needAdd = true
					}
				} else {
					newRecords = append(newRecords, map[string]interface{}{
						"content":  rec.Content,
						"disabled": rec.Disabled,
					})
				}
			}

			if !found {
				return fmt.Errorf("记录不存在")
			}

			var err error
			if len(newRecords) > 0 {
				err = p.rrsetReplace(ctx, rs.Host, rs.Type, ttl, newRecords, remark)
			} else {
				err = p.rrsetDelete(ctx, rs.Host, rs.Type)
			}

			if err == nil && needAdd {
				_, err = p.AddDomainRecord(ctx, name, recordType, value, line, ttl, mx, weight, remark)
			}

			return err
		}
	}

	return fmt.Errorf("记录不存在")
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("PowerDNS不支持单独修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	parts := strings.Split(recordID, "_")
	if len(parts) != 2 {
		return fmt.Errorf("无效的记录ID")
	}
	rrsetID, _ := strconv.Atoi(parts[0])
	recID, _ := strconv.Atoi(parts[1])

	rrsets, _ := p.loadRRSets(ctx)

	for _, rs := range rrsets {
		if rs.ID == rrsetID {
			var newRecords []map[string]interface{}
			found := false

			for _, rec := range rs.Records {
				if rec.ID == recID {
					found = true
				} else {
					newRecords = append(newRecords, map[string]interface{}{
						"content":  rec.Content,
						"disabled": rec.Disabled,
					})
				}
			}

			if !found {
				return fmt.Errorf("记录不存在")
			}

			if len(newRecords) > 0 {
				return p.rrsetReplace(ctx, rs.Host, rs.Type, rs.TTL, newRecords, "")
			}
			return p.rrsetDelete(ctx, rs.Host, rs.Type)
		}
	}

	return fmt.Errorf("记录不存在")
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	parts := strings.Split(recordID, "_")
	if len(parts) != 2 {
		return fmt.Errorf("无效的记录ID")
	}
	rrsetID, _ := strconv.Atoi(parts[0])
	recID, _ := strconv.Atoi(parts[1])

	rrsets, _ := p.loadRRSets(ctx)

	for _, rs := range rrsets {
		if rs.ID == rrsetID {
			var newRecords []map[string]interface{}
			found := false

			for _, rec := range rs.Records {
				disabled := rec.Disabled
				if rec.ID == recID {
					found = true
					disabled = !enable
				}
				newRecords = append(newRecords, map[string]interface{}{
					"content":  rec.Content,
					"disabled": disabled,
				})
			}

			if !found {
				return fmt.Errorf("记录不存在")
			}

			return p.rrsetReplace(ctx, rs.Host, rs.Type, rs.TTL, newRecords, "")
		}
	}

	return fmt.Errorf("记录不存在")
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("PowerDNS不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "default", Name: "默认"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 60
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}
	body := map[string]interface{}{
		"name":         domain,
		"kind":         "Native",
		"soa_edit_api": "INCREASE",
	}
	_, err := p.request(ctx, "POST", "/servers/"+p.serverID+"/zones", body)
	return err
}
