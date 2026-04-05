package bt

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"main/internal/dns"
)

func init() {
	dns.Register("bt", NewProvider, dns.ProviderConfig{
		Type: "bt",
		Name: "宝塔域名",
		Icon: "bt.png",
		Config: []dns.ConfigField{
			{Name: "Access Key", Key: "AccessKey", Type: "input", Required: true},
			{Name: "Secret Key", Key: "SecretKey", Type: "input", Required: true},
			{Name: "Account ID", Key: "AccountID", Type: "input", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 2, Status: true, Redirect: false, Log: false, Weight: true, Page: false, Add: true,
		},
	})
}

const baseURL = "https://dmp.bt.cn"

type Provider struct {
	accountID  string
	accessKey  string
	secretKey  string
	domain     string
	domainID   int
	domainType int
	proxy      bool
	client     *http.Client
	lastErr    string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	p := &Provider{
		accountID:  config["AccountID"],
		accessKey:  config["AccessKey"],
		secretKey:  config["SecretKey"],
		domain:     domain,
		domainType: 1,
		proxy:      config["proxy"] == "1",
		client:     &http.Client{Timeout: 30 * time.Second},
	}

	if domainID != "" {
		parts := strings.Split(domainID, "|")
		if len(parts) >= 1 {
			p.domainID, _ = strconv.Atoi(parts[0])
		}
		if len(parts) >= 2 {
			p.domainType, _ = strconv.Atoi(parts[1])
		}
	}

	return p
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) request(ctx context.Context, path string, params map[string]interface{}) (map[string]interface{}, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	body, _ := json.Marshal(params)

	signingString := strings.Join([]string{
		p.accountID,
		timestamp,
		"POST",
		path,
		string(body),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(p.secretKey))
	mac.Write([]byte(signingString))
	signature := hex.EncodeToString(mac.Sum(nil))

	reqURL := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Account-ID", p.accountID)
	req.Header.Set("X-Access-Key", p.accessKey)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", signature)

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

	if code, ok := result["code"].(float64); ok && code != 0 {
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
	params := map[string]interface{}{
		"p":       page,
		"rows":    pageSize,
		"keyword": keyword,
	}

	result, err := p.request(ctx, "/api/v1/dns/manage/list_domains", params)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if data, ok := result["data"].([]interface{}); ok {
		for _, d := range data {
			if domain, ok := d.(map[string]interface{}); ok {
				localID := int(domain["local_id"].(float64))
				domainType := int(domain["domain_type"].(float64))
				domains = append(domains, dns.DomainInfo{
					ID:          fmt.Sprintf("%d|%d", localID, domainType),
					Name:        domain["full_domain"].(string),
					RecordCount: int(domain["record_count"].(float64)),
				})
			}
		}
	}

	total := int(result["total"].(float64))
	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	params := map[string]interface{}{
		"domain_id":   p.domainID,
		"domain_type": p.domainType,
		"p":           page,
		"rows":        pageSize,
	}

	if subDomain != "" {
		params["searchKey"] = "record"
		params["searchValue"] = subDomain
	} else if keyword != "" {
		params["searchKey"] = "record"
		params["searchValue"] = keyword
	} else if value != "" {
		params["searchKey"] = "value"
		params["searchValue"] = value
	} else if recordType != "" {
		params["searchKey"] = "type"
		params["searchValue"] = recordType
	} else if status != "" {
		params["searchKey"] = "state"
		if status == "0" {
			params["searchValue"] = "1"
		} else {
			params["searchValue"] = "0"
		}
	} else if line != "" {
		params["searchKey"] = "line"
		params["searchValue"] = line
	}

	result, err := p.request(ctx, "/api/v1/dns/record/list", params)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if data, ok := result["data"].([]interface{}); ok {
		for _, r := range data {
			if rec, ok := r.(map[string]interface{}); ok {
				statusVal := "enable"
				if state, ok := rec["state"].(float64); ok && state == 1 {
					statusVal = "disable"
				}

				var mx, weight int
				if m, ok := rec["MX"].(float64); ok {
					mx = int(m)
					weight = mx
				}

				var remark string
				if rm, ok := rec["remark"].(string); ok {
					remark = rm
				}

				records = append(records, dns.Record{
					ID:     rec["record_id"].(string),
					Name:   rec["record"].(string),
					Type:   rec["type"].(string),
					Value:  rec["value"].(string),
					TTL:    int(rec["TTL"].(float64)),
					Line:   fmt.Sprintf("%v", rec["viewID"]),
					MX:     mx,
					Weight: weight,
					Status: statusVal,
					Remark: remark,
				})
			}
		}
	}

	total := int(result["count"].(float64))
	return &dns.PageResult{Total: total, Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	if subDomain == "" {
		subDomain = "@"
	}
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	return nil, fmt.Errorf("宝塔域名不支持获取单条记录")
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if line == "" || line == "default" {
		line = "0"
	}
	lineID, _ := strconv.Atoi(line)

	params := map[string]interface{}{
		"domain_id":   p.domainID,
		"domain_type": p.domainType,
		"type":        recordType,
		"record":      name,
		"value":       value,
		"ttl":         ttl,
		"view_id":     lineID,
		"remark":      remark,
	}

	if weight == nil || *weight == 0 {
		w := 1
		weight = &w
	}
	if recordType == "MX" {
		params["mx"] = mx
	} else {
		params["mx"] = *weight
	}

	_, err := p.request(ctx, "/api/v1/dns/record/create", params)
	return "", err
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	lineID, _ := strconv.Atoi(line)

	params := map[string]interface{}{
		"record_id":   recordID,
		"domain_id":   p.domainID,
		"domain_type": p.domainType,
		"type":        recordType,
		"record":      name,
		"value":       value,
		"ttl":         ttl,
		"view_id":     lineID,
		"remark":      remark,
	}

	if weight == nil || *weight == 0 {
		w := 1
		weight = &w
	}
	if recordType == "MX" {
		params["mx"] = mx
	} else {
		params["mx"] = *weight
	}

	_, err := p.request(ctx, "/api/v1/dns/record/update", params)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("宝塔域名不支持单独修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	params := map[string]interface{}{
		"id":          recordID,
		"domain_id":   p.domainID,
		"domain_type": p.domainType,
	}

	_, err := p.request(ctx, "/api/v1/dns/record/delete", params)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	params := map[string]interface{}{
		"record_id":   recordID,
		"domain_id":   p.domainID,
		"domain_type": p.domainType,
	}

	path := "/api/v1/dns/record/pause"
	if enable {
		path = "/api/v1/dns/record/start"
	}

	_, err := p.request(ctx, path, params)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("宝塔域名不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	result, err := p.request(ctx, "/api/v1/dns/record/get_views", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var lines []dns.RecordLine
	if data, ok := result["data"].([]interface{}); ok {
		p.processLines(&lines, data)
	}

	return lines, nil
}

func (p *Provider) processLines(lines *[]dns.RecordLine, data []interface{}) {
	for _, item := range data {
		if l, ok := item.(map[string]interface{}); ok {
			if free, ok := l["free"].(bool); ok && free {
				*lines = append(*lines, dns.RecordLine{
					ID:   fmt.Sprintf("%v", l["viewId"]),
					Name: l["name"].(string),
				})
				if children, ok := l["children"].([]interface{}); ok {
					p.processLines(lines, children)
				}
			}
		}
	}
}

func (p *Provider) GetMinTTL() int {
	return 300
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	params := map[string]interface{}{
		"full_domain": domain,
	}
	_, err := p.request(ctx, "/api/v1/dns/manage/add_external_domain", params)
	return err
}
