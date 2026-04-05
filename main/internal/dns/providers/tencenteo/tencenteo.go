package tencenteo

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
	dns.Register("tencenteo", NewProvider, dns.ProviderConfig{
		Type: "tencenteo",
		Name: "腾讯云EO",
		Icon: "tencent.png",
		Note: "仅支持以NS方式接入腾讯云EO的域名",
		Config: []dns.ConfigField{
			{Name: "SecretId", Key: "SecretId", Type: "input", Required: true},
			{Name: "SecretKey", Key: "SecretKey", Type: "input", Required: true},
			{Name: "API接入点", Key: "site_type", Type: "select", Options: []dns.ConfigOption{
				{Value: "cn", Label: "中国内地"},
				{Value: "intl", Label: "非中国内地"},
			}, Value: "cn", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 0, Status: true, Redirect: false, Log: false, Weight: true, Page: false, Add: false,
		},
	})
}

type Provider struct {
	secretID  string
	secretKey string
	endpoint  string
	service   string
	version   string
	domain    string
	domainID  string
	proxy     bool
	client    *http.Client
	lastErr   string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	endpoint := "teo.tencentcloudapi.com"
	if config["site_type"] == "intl" {
		endpoint = "teo.intl.tencentcloudapi.com"
	}
	return &Provider{
		secretID:  config["SecretId"],
		secretKey: config["SecretKey"],
		endpoint:  endpoint,
		service:   "teo",
		version:   "2022-09-01",
		domain:    domain,
		domainID:  domainID,
		proxy:     config["proxy"] == "1",
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func sha256Hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func (p *Provider) request(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
	timestamp := time.Now().Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	payload, _ := json.Marshal(params)
	payloadStr := string(payload)

	// Build canonical request
	httpRequestMethod := "POST"
	canonicalURI := "/"
	canonicalQueryString := ""
	canonicalHeaders := "content-type:application/json; charset=utf-8\n" +
		"host:" + p.endpoint + "\n" +
		"x-tc-action:" + strings.ToLower(action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	hashedRequestPayload := sha256Hex(payloadStr)

	canonicalRequest := httpRequestMethod + "\n" +
		canonicalURI + "\n" +
		canonicalQueryString + "\n" +
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		hashedRequestPayload

	// Build string to sign
	algorithm := "TC3-HMAC-SHA256"
	credentialScope := date + "/" + p.service + "/tc3_request"
	stringToSign := algorithm + "\n" +
		strconv.FormatInt(timestamp, 10) + "\n" +
		credentialScope + "\n" +
		sha256Hex(canonicalRequest)

	// Calculate signature
	secretDate := hmacSHA256([]byte("TC3"+p.secretKey), date)
	secretService := hmacSHA256(secretDate, p.service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	// Build authorization
	authorization := algorithm + " Credential=" + p.secretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + signature

	req, err := http.NewRequestWithContext(ctx, "POST", "https://"+p.endpoint, strings.NewReader(payloadStr))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", p.endpoint)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", p.version)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("Authorization", authorization)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if response, ok := result["Response"].(map[string]interface{}); ok {
		if errInfo, ok := response["Error"].(map[string]interface{}); ok {
			msg := errInfo["Message"].(string)
			p.lastErr = msg
			return nil, fmt.Errorf("%s", msg)
		}
		return response, nil
	}

	return nil, fmt.Errorf("解析响应失败")
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	offset := (page - 1) * pageSize
	filters := []map[string]interface{}{
		{"Name": "zone-type", "Values": []string{"full"}},
	}
	if keyword != "" {
		filters = append(filters, map[string]interface{}{
			"Name": "zone-name", "Values": []string{keyword},
		})
	}

	params := map[string]interface{}{
		"Offset":  offset,
		"Limit":   pageSize,
		"Filters": filters,
	}

	result, err := p.request(ctx, "DescribeZones", params)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if zones, ok := result["Zones"].([]interface{}); ok {
		for _, z := range zones {
			if zone, ok := z.(map[string]interface{}); ok {
				domains = append(domains, dns.DomainInfo{
					ID:          zone["ZoneId"].(string),
					Name:        zone["ZoneName"].(string),
					RecordCount: 0,
				})
			}
		}
	}

	total := int(result["TotalCount"].(float64))
	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	offset := (page - 1) * pageSize

	filters := []map[string]interface{}{}
	if subDomain != "" {
		name := subDomain
		if subDomain == "@" {
			name = p.domain
		} else {
			name = subDomain + "." + p.domain
		}
		filters = append(filters, map[string]interface{}{
			"Name": "name", "Values": []string{name},
		})
	} else if keyword != "" {
		name := keyword
		if keyword == "@" {
			name = p.domain
		} else {
			name = keyword + "." + p.domain
		}
		filters = append(filters, map[string]interface{}{
			"Name": "name", "Values": []string{name},
		})
	}
	if value != "" {
		filters = append(filters, map[string]interface{}{
			"Name": "content", "Values": []string{value}, "Fuzzy": true,
		})
	}
	if recordType != "" {
		filters = append(filters, map[string]interface{}{
			"Name": "type", "Values": []string{recordType},
		})
	}

	params := map[string]interface{}{
		"ZoneId":  p.domainID,
		"Offset":  offset,
		"Limit":   pageSize,
		"Filters": filters,
	}

	result, err := p.request(ctx, "DescribeDnsRecords", params)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if recs, ok := result["DnsRecords"].([]interface{}); ok {
		for _, r := range recs {
			if rec, ok := r.(map[string]interface{}); ok {
				recordName := rec["Name"].(string)
				name := strings.TrimSuffix(recordName, "."+p.domain)
				if name == "" || name == p.domain {
					name = "@"
				}

				statusVal := "disable"
				if rec["Status"].(string) == "enable" {
					statusVal = "enable"
				}

				var weight int
				if w, ok := rec["Weight"].(float64); ok && w != -1 {
					weight = int(w)
				}

				var mx int
				if priority, ok := rec["Priority"].(float64); ok {
					mx = int(priority)
				}

				records = append(records, dns.Record{
					ID:     rec["RecordId"].(string),
					Name:   name,
					Type:   rec["Type"].(string),
					Value:  rec["Content"].(string),
					TTL:    int(rec["TTL"].(float64)),
					Line:   rec["Location"].(string),
					MX:     mx,
					Weight: weight,
					Status: statusVal,
				})
			}
		}
	}

	total := int(result["TotalCount"].(float64))
	return &dns.PageResult{Total: total, Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	if subDomain == "" {
		subDomain = "@"
	}
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	params := map[string]interface{}{
		"ZoneId": p.domainID,
		"Filters": []map[string]interface{}{
			{"Name": "id", "Values": []string{recordID}},
		},
	}

	result, err := p.request(ctx, "DescribeDnsRecords", params)
	if err != nil {
		return nil, err
	}

	if recs, ok := result["DnsRecords"].([]interface{}); ok && len(recs) > 0 {
		if rec, ok := recs[0].(map[string]interface{}); ok {
			recordName := rec["Name"].(string)
			name := strings.TrimSuffix(recordName, "."+p.domain)
			if name == "" || name == p.domain {
				name = "@"
			}

			statusVal := "disable"
			if rec["Status"].(string) == "enable" {
				statusVal = "enable"
			}

			var weight int
			if w, ok := rec["Weight"].(float64); ok && w != -1 {
				weight = int(w)
			}

			var mx int
			if priority, ok := rec["Priority"].(float64); ok {
				mx = int(priority)
			}

			return &dns.Record{
				ID:     rec["RecordId"].(string),
				Name:   name,
				Type:   rec["Type"].(string),
				Value:  rec["Content"].(string),
				TTL:    int(rec["TTL"].(float64)),
				Line:   rec["Location"].(string),
				MX:     mx,
				Weight: weight,
				Status: statusVal,
			}, nil
		}
	}

	return nil, fmt.Errorf("记录不存在")
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	recordName := name
	if name == "@" {
		recordName = p.domain
	} else {
		recordName = name + "." + p.domain
	}

	weightVal := -1
	if weight != nil {
		weightVal = *weight
	}

	params := map[string]interface{}{
		"ZoneId":   p.domainID,
		"Name":     recordName,
		"Type":     recordType,
		"Content":  value,
		"Location": line,
		"TTL":      ttl,
		"Weight":   weightVal,
	}
	if recordType == "MX" {
		params["Priority"] = mx
	}

	result, err := p.request(ctx, "CreateDnsRecord", params)
	if err != nil {
		return "", err
	}

	if recordID, ok := result["RecordId"].(string); ok {
		return recordID, nil
	}

	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	recordName := name
	if name == "@" {
		recordName = p.domain
	} else {
		recordName = name + "." + p.domain
	}

	weightVal := -1
	if weight != nil {
		weightVal = *weight
	}

	params := map[string]interface{}{
		"ZoneId":      p.domainID,
		"DnsRecordId": recordID,
		"Name":        recordName,
		"Type":        recordType,
		"Content":     value,
		"Location":    line,
		"TTL":         ttl,
		"Weight":      weightVal,
	}
	if recordType == "MX" {
		params["Priority"] = mx
	}

	_, err := p.request(ctx, "ModifyDnsRecord", params)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("腾讯云EO不支持修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	params := map[string]interface{}{
		"ZoneId":    p.domainID,
		"RecordIds": []string{recordID},
	}

	_, err := p.request(ctx, "DeleteDnsRecords", params)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	params := map[string]interface{}{
		"ZoneId": p.domainID,
	}
	if enable {
		params["RecordsToEnable"] = []string{recordID}
	} else {
		params["RecordsToDisable"] = []string{recordID}
	}

	_, err := p.request(ctx, "ModifyDnsRecordsStatus", params)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("腾讯云EO不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "Default", Name: "默认"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 60
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	return fmt.Errorf("腾讯云EO不支持添加域名")
}
