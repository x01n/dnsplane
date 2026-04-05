package huoshan

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
	"strconv"
	"strings"
	"time"

	"main/internal/dns"
)

func init() {
	dns.Register("huoshan", NewProvider, dns.ProviderConfig{
		Type: "huoshan",
		Name: "火山引擎",
		Icon: "huoshan.png",
		Config: []dns.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
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

const (
	endpoint = "open.volcengineapi.com"
	service  = "DNS"
	version  = "2018-08-01"
	region   = "cn-north-1"
)

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

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256Hash(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func (p *Provider) sign(method, action string, query map[string]string, body string, timestamp time.Time) (map[string]string, string) {
	date := timestamp.UTC().Format("20060102T150405Z")
	shortDate := timestamp.UTC().Format("20060102")

	query["Action"] = action
	query["Version"] = version

	var keys []string
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonicalQuery strings.Builder
	for i, k := range keys {
		if i > 0 {
			canonicalQuery.WriteString("&")
		}
		canonicalQuery.WriteString(url.QueryEscape(k) + "=" + url.QueryEscape(query[k]))
	}

	headers := map[string]string{
		"Host":             endpoint,
		"X-Date":           date,
		"X-Content-Sha256": sha256Hash(body),
	}
	if body != "" {
		headers["Content-Type"] = "application/json"
	}

	var headerKeys []string
	for k := range headers {
		headerKeys = append(headerKeys, strings.ToLower(k))
	}
	sort.Strings(headerKeys)

	var canonicalHeaders strings.Builder
	var signedHeaders strings.Builder
	for i, k := range headerKeys {
		canonicalHeaders.WriteString(k + ":" + headers[strings.Title(strings.ReplaceAll(k, "-", " "))] + "\n")
		if i > 0 {
			signedHeaders.WriteString(";")
		}
		signedHeaders.WriteString(k)
	}

	canonicalRequest := method + "\n" +
		"/" + "\n" +
		canonicalQuery.String() + "\n" +
		canonicalHeaders.String() + "\n" +
		signedHeaders.String() + "\n" +
		sha256Hash(body)

	credentialScope := shortDate + "/" + region + "/" + service + "/request"
	stringToSign := "HMAC-SHA256\n" + date + "\n" + credentialScope + "\n" + sha256Hash(canonicalRequest)

	kDate := hmacSHA256([]byte(p.secretAccessKey), shortDate)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	authorization := fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.accessKeyID, credentialScope, signedHeaders.String(), signature)

	headers["Authorization"] = authorization
	return headers, canonicalQuery.String()
}

func (p *Provider) request(ctx context.Context, method, action string, params map[string]string) (map[string]interface{}, error) {
	timestamp := time.Now()

	query := make(map[string]string)
	var body string

	if method == "GET" {
		for k, v := range params {
			query[k] = v
		}
	} else {
		bodyBytes, _ := json.Marshal(params)
		body = string(bodyBytes)
	}

	headers, queryString := p.sign(method, action, query, body, timestamp)

	reqURL := "https://" + endpoint + "/?" + queryString

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
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

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if respMeta, ok := result["ResponseMetadata"].(map[string]interface{}); ok {
		if errInfo, ok := respMeta["Error"].(map[string]interface{}); ok {
			msg := errInfo["Message"].(string)
			p.lastErr = msg
			return nil, fmt.Errorf("%s", msg)
		}
	}

	if resultData, ok := result["Result"].(map[string]interface{}); ok {
		return resultData, nil
	}

	return result, nil
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	params := map[string]string{
		"PageNumber": strconv.Itoa(page),
		"PageSize":   strconv.Itoa(pageSize),
	}
	if keyword != "" {
		params["Key"] = keyword
	}

	result, err := p.request(ctx, "GET", "ListZones", params)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if zones, ok := result["Zones"].([]interface{}); ok {
		for _, z := range zones {
			if zone, ok := z.(map[string]interface{}); ok {
				domains = append(domains, dns.DomainInfo{
					ID:          fmt.Sprintf("%v", zone["ZID"]),
					Name:        zone["ZoneName"].(string),
					RecordCount: int(zone["RecordCount"].(float64)),
				})
			}
		}
	}

	total := int(result["Total"].(float64))
	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	params := map[string]string{
		"ZID":         p.domainID,
		"PageNumber":  strconv.Itoa(page),
		"PageSize":    strconv.Itoa(pageSize),
		"SearchOrder": "desc",
	}

	if subDomain != "" || recordType != "" || line != "" || value != "" {
		if subDomain != "" {
			params["Host"] = subDomain
		}
		if value != "" {
			params["Value"] = value
		}
		if recordType != "" {
			params["Type"] = recordType
		}
		if line != "" {
			params["Line"] = line
		}
		params["SearchMode"] = "exact"
	} else if keyword != "" {
		params["Host"] = keyword
	}

	result, err := p.request(ctx, "GET", "ListRecords", params)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if recs, ok := result["Records"].([]interface{}); ok {
		for _, r := range recs {
			if rec, ok := r.(map[string]interface{}); ok {
				recType := rec["Type"].(string)
				recValue := rec["Value"].(string)
				var mx int

				if recType == "MX" {
					parts := strings.SplitN(recValue, " ", 2)
					if len(parts) == 2 {
						fmt.Sscanf(parts[0], "%d", &mx)
						recValue = parts[1]
					}
				}

				statusVal := "disable"
				if enable, ok := rec["Enable"].(bool); ok && enable {
					statusVal = "enable"
				}

				var weight int
				if w, ok := rec["Weight"].(float64); ok {
					weight = int(w)
				}

				var remark string
				if r, ok := rec["Remark"].(string); ok {
					remark = r
				}

				records = append(records, dns.Record{
					ID:     rec["RecordID"].(string),
					Name:   rec["Host"].(string),
					Type:   recType,
					Value:  recValue,
					TTL:    int(rec["TTL"].(float64)),
					Line:   rec["Line"].(string),
					MX:     mx,
					Weight: weight,
					Status: statusVal,
					Remark: remark,
				})
			}
		}
	}

	total := int(result["TotalCount"].(float64))
	return &dns.PageResult{Total: total, Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	params := map[string]string{"RecordID": recordID}
	result, err := p.request(ctx, "GET", "QueryRecord", params)
	if err != nil {
		return nil, err
	}

	recType := result["Type"].(string)
	recValue := result["Value"].(string)
	var mx int

	if recType == "MX" {
		parts := strings.SplitN(recValue, " ", 2)
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &mx)
			recValue = parts[1]
		}
	}

	statusVal := "disable"
	if enable, ok := result["Enable"].(bool); ok && enable {
		statusVal = "enable"
	}

	var weight int
	if w, ok := result["Weight"].(float64); ok {
		weight = int(w)
	}

	var remark string
	if r, ok := result["Remark"].(string); ok {
		remark = r
	}

	return &dns.Record{
		ID:     result["RecordID"].(string),
		Name:   result["Host"].(string),
		Type:   recType,
		Value:  recValue,
		TTL:    int(result["TTL"].(float64)),
		Line:   result["Line"].(string),
		MX:     mx,
		Weight: weight,
		Status: statusVal,
		Remark: remark,
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if line == "" || line == "default" {
		line = "default" // 火山引擎默认线路
	}
	params := map[string]string{
		"ZID":   p.domainID,
		"Host":  name,
		"Type":  recordType,
		"Value": value,
		"Line":  line,
		"TTL":   strconv.Itoa(ttl),
	}

	if recordType == "MX" {
		params["Value"] = fmt.Sprintf("%d %s", mx, value)
	}
	if weight != nil && *weight > 0 {
		params["Weight"] = strconv.Itoa(*weight)
	}
	if remark != "" {
		params["Remark"] = remark
	}

	result, err := p.request(ctx, "POST", "CreateRecord", params)
	if err != nil {
		return "", err
	}

	return result["RecordID"].(string), nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	params := map[string]string{
		"RecordID": recordID,
		"Host":     name,
		"Type":     recordType,
		"Value":    value,
		"Line":     line,
		"TTL":      strconv.Itoa(ttl),
	}

	if recordType == "MX" {
		params["Value"] = fmt.Sprintf("%d %s", mx, value)
	}
	if weight != nil && *weight > 0 {
		params["Weight"] = strconv.Itoa(*weight)
	}
	if remark != "" {
		params["Remark"] = remark
	}

	_, err := p.request(ctx, "POST", "UpdateRecord", params)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("火山引擎不支持单独修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	params := map[string]string{"RecordID": recordID}
	_, err := p.request(ctx, "POST", "DeleteRecord", params)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	params := map[string]string{
		"RecordID": recordID,
		"Enable":   strconv.FormatBool(enable),
	}
	_, err := p.request(ctx, "POST", "UpdateRecordStatus", params)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("火山引擎不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "default", Name: "默认"},
		{ID: "telecom", Name: "电信"},
		{ID: "unicom", Name: "联通"},
		{ID: "mobile", Name: "移动"},
		{ID: "edu", Name: "教育网"},
		{ID: "oversea", Name: "海外"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 600
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	params := map[string]string{"ZoneName": domain}
	_, err := p.request(ctx, "POST", "CreateZone", params)
	return err
}
