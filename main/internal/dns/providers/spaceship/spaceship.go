package spaceship

import (
	"bytes"
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

const (
	defaultAPIUrl = "https://spaceship.dev/api/v1"
)

func init() {
	dns.Register("spaceship", NewProvider, dns.ProviderConfig{
		Type: "spaceship",
		Name: "Spaceship",
		Icon: "spaceship.ico",
		Config: []dns.ConfigField{
			{Name: "API Key", Key: "apikey", Type: "input", Required: true},
			{Name: "API Secret", Key: "apisecret", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []dns.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		Features: dns.ProviderFeatures{
			Remark:   0,
			Status:   false,
			Redirect: false,
			Log:      false,
			Weight:   false,
			Page:     true,
			Add:      false,
		},
	})
}

type Provider struct {
	apiKey    string
	apiSecret string
	domain    string
	proxy     bool
	lastError string
	client    *http.Client
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	p := &Provider{
		apiKey:    config["apikey"],
		apiSecret: config["apisecret"],
		domain:    domain,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
	if v, ok := config["proxy"]; ok {
		p.proxy = v == "1"
	}
	return p
}

func (p *Provider) GetError() string {
	return p.lastError
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	skip := (page - 1) * pageSize
	params := url.Values{}
	params.Set("take", strconv.Itoa(pageSize))
	params.Set("skip", strconv.Itoa(skip))

	var resp struct {
		Total int `json:"total"`
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}

	if err := p.request(ctx, "GET", "/domains", params, nil, &resp); err != nil {
		return nil, err
	}

	list := make([]dns.DomainInfo, 0, len(resp.Items))
	for _, item := range resp.Items {
		list = append(list, dns.DomainInfo{
			ID:          item.Name,
			Name:        item.Name,
			RecordCount: 0,
			Status:      "enable",
		})
	}

	return &dns.PageResult{
		Total:   resp.Total,
		Records: list,
	}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	params := url.Values{}
	if subDomain != "" {
		params.Set("take", "100")
		params.Set("skip", "0")
	} else {
		params.Set("take", strconv.Itoa(pageSize))
		params.Set("skip", strconv.Itoa((page-1)*pageSize))
	}

	var resp struct {
		Total int `json:"total"`
		Items []struct {
			Type       string `json:"type"`
			Name       string `json:"name"`
			TTL        int    `json:"ttl"`
			Address    string `json:"address"`
			Exchange   string `json:"exchange"`
			Preference int    `json:"preference"`
			Cname      string `json:"cname"`
			Value      string `json:"value"`
			Pointer    string `json:"pointer"`
			Nameserver string `json:"nameserver"`
			Flag       int    `json:"flag"`
			Tag        string `json:"tag"`
			Priority   int    `json:"priority"`
			Weight     int    `json:"weight"`
			Port       int    `json:"port"`
			Target     string `json:"target"`
			AliasName  string `json:"aliasName"`
		} `json:"items"`
	}

	if err := p.request(ctx, "GET", "/dns/records/"+p.domain, params, nil, &resp); err != nil {
		return nil, err
	}

	list := make([]dns.Record, 0)
	for _, item := range resp.Items {
		var address string
		mx := 0

		switch item.Type {
		case "MX":
			address = item.Exchange
			mx = item.Preference
		case "CNAME":
			address = item.Cname
		case "TXT":
			address = item.Value
		case "PTR":
			address = item.Pointer
		case "NS":
			address = item.Nameserver
		case "CAA":
			address = fmt.Sprintf("%d %s %s", item.Flag, item.Tag, item.Value)
		case "SRV":
			address = fmt.Sprintf("%d %d %d %s", item.Priority, item.Weight, item.Port, item.Target)
		case "ALIAS":
			address = item.AliasName
		default:
			address = item.Address
		}

		// Filter by SubDomain if provided
		if subDomain != "" && !strings.EqualFold(item.Name, subDomain) {
			continue
		}

		recordID := fmt.Sprintf("%s|%s|%s|%d", item.Type, item.Name, address, mx)

		list = append(list, dns.Record{
			ID:      recordID,
			Name:    item.Name,
			Type:    item.Type,
			Value:   address,
			TTL:     item.TTL,
			Line:    "default",
			MX:      mx,
			Status:  "enable",
			Updated: "",
		})
	}

	return &dns.PageResult{
		Total:   resp.Total,
		Records: list,
	}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	if subDomain == "" {
		subDomain = "@"
	}
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	/* recordID 格式: type|name|address|mx，直接解析无需 API 调用 */
	parts := strings.SplitN(recordID, "|", 4)
	if len(parts) < 3 {
		return nil, fmt.Errorf("无效的记录ID格式")
	}
	mx := 0
	if len(parts) >= 4 {
		mx, _ = strconv.Atoi(parts[3])
	}
	return &dns.Record{
		ID:     recordID,
		Type:   parts[0],
		Name:   parts[1],
		Value:  parts[2],
		MX:     mx,
		TTL:    600,
		Line:   "default",
		Status: "enable",
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	item := p.convertRecordItem(name, recordType, value, mx)
	item["ttl"] = ttl

	payload := map[string]interface{}{
		"force": false,
		"items": []map[string]interface{}{item},
	}

	if err := p.request(ctx, "PUT", "/dns/records/"+p.domain, nil, payload, nil); err != nil {
		return "", err
	}
	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	item := p.convertRecordItem(name, recordType, value, mx)
	item["ttl"] = ttl

	payload := map[string]interface{}{
		"force": true,
		"items": []map[string]interface{}{item},
	}

	return p.request(ctx, "PUT", "/dns/records/"+p.domain, nil, payload, nil)
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("Spaceship不支持修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	/* recordID 格式: type|name|address|mx */
	parts := strings.SplitN(recordID, "|", 4)
	if len(parts) < 3 {
		return fmt.Errorf("无效的记录ID格式")
	}
	recordType := parts[0]
	name := parts[1]
	value := parts[2]
	mx := 0
	if len(parts) >= 4 {
		mx, _ = strconv.Atoi(parts[3])
	}

	item := p.convertRecordItem(name, recordType, value, mx)
	payload := map[string]interface{}{
		"items": []map[string]interface{}{item},
	}
	return p.request(ctx, "POST", "/dns/records/"+p.domain+"/delete", nil, payload, nil)
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	return fmt.Errorf("Spaceship不支持设置记录状态")
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return &dns.PageResult{Total: 0, Records: []interface{}{}}, nil
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{{ID: "default", Name: "默认"}}, nil
}

func (p *Provider) GetMinTTL() int {
	return 60
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	return fmt.Errorf("Spaceship不支持添加域名")
}

func (p *Provider) convertRecordItem(name, recordType, value string, mx int) map[string]interface{} {
	item := map[string]interface{}{
		"type": recordType,
		"name": name,
	}
	switch recordType {
	case "MX":
		item["exchange"] = value
		item["preference"] = mx
	case "TXT":
		item["value"] = value
	case "CNAME":
		item["cname"] = value
	case "ALIAS":
		item["aliasName"] = value
	case "NS":
		item["nameserver"] = value
	case "PTR":
		item["pointer"] = value
	case "CAA":
		parts := strings.SplitN(value, " ", 3)
		if len(parts) >= 3 {
			flag, _ := strconv.Atoi(parts[0])
			item["flag"] = flag
			item["tag"] = parts[1]
			item["value"] = strings.Trim(parts[2], "\"")
		}
	case "SRV":
		parts := strings.SplitN(value, " ", 4)
		if len(parts) >= 4 {
			priority, _ := strconv.Atoi(parts[0])
			weight, _ := strconv.Atoi(parts[1])
			port, _ := strconv.Atoi(parts[2])
			item["priority"] = priority
			item["weight"] = weight
			item["port"] = port
			item["target"] = parts[3]
		}
	default:
		item["address"] = value
	}
	return item
}

func (p *Provider) request(ctx context.Context, method, path string, params url.Values, body interface{}, result interface{}) error {
	u := defaultAPIUrl + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return err
	}

	req.Header.Set("X-API-Key", p.apiKey)
	req.Header.Set("X-API-Secret", p.apiSecret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		p.lastError = err.Error()
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.lastError = err.Error()
		return err
	}

	if resp.StatusCode >= 400 {
		p.lastError = string(respBody)
		return fmt.Errorf("API 返回错误: %s", p.lastError)
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			p.lastError = err.Error()
			return err
		}
	}

	return nil
}
