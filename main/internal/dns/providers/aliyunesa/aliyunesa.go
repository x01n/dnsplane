package aliyunesa

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

	"github.com/google/uuid"
)

func init() {
	dns.Register("aliyunesa", NewProvider, dns.ProviderConfig{
		Type: "aliyunesa",
		Name: "阿里云ESA",
		Icon: "aliyun.png",
		Note: "仅支持以NS方式接入阿里云ESA的域名",
		Config: []dns.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "AccessKeySecret", Type: "input", Required: true},
			{Name: "API接入点", Key: "region", Type: "select", Options: []dns.ConfigOption{
				{Value: "cn-hangzhou", Label: "中国内地"},
				{Value: "ap-southeast-1", Label: "非中国内地"},
			}, Value: "cn-hangzhou", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 2, Status: false, Redirect: false, Log: false, Weight: false, Page: false, Add: false,
		},
	})
}

type Provider struct {
	accessKeyID     string
	accessKeySecret string
	endpoint        string
	version         string
	domain          string
	domainID        string
	proxy           bool
	client          *http.Client
	lastErr         string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	region := config["region"]
	if region == "" {
		region = "cn-hangzhou"
	}
	return &Provider{
		accessKeyID:     config["AccessKeyId"],
		accessKeySecret: config["AccessKeySecret"],
		endpoint:        "esa." + region + ".aliyuncs.com",
		version:         "2024-09-10",
		domain:          domain,
		domainID:        domainID,
		proxy:           config["proxy"] == "1",
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) sign(params map[string]string, method string) string {
	params["Format"] = "JSON"
	params["Version"] = p.version
	params["AccessKeyId"] = p.accessKeyID
	params["SignatureMethod"] = "HMAC-SHA256"
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = uuid.New().String()

	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var query strings.Builder
	for i, k := range keys {
		if i > 0 {
			query.WriteString("&")
		}
		query.WriteString(url.QueryEscape(k))
		query.WriteString("=")
		query.WriteString(url.QueryEscape(params[k]))
	}

	stringToSign := method + "&%2F&" + url.QueryEscape(query.String())
	mac := hmac.New(sha256.New, []byte(p.accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	signature := hex.EncodeToString(mac.Sum(nil))

	return query.String() + "&Signature=" + url.QueryEscape(signature)
}

func (p *Provider) request(ctx context.Context, params map[string]string, method string) (map[string]interface{}, error) {
	queryString := p.sign(params, method)
	reqURL := "https://" + p.endpoint + "/?" + queryString

	var reqBody io.Reader
	if method == "POST" {
		reqBody = strings.NewReader(queryString)
		reqURL = "https://" + p.endpoint + "/"
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

	if code, ok := result["Code"].(string); ok {
		msg := result["Message"].(string)
		p.lastErr = msg
		return nil, fmt.Errorf("%s: %s", code, msg)
	}

	return result, nil
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	params := map[string]string{
		"Action":     "ListSites",
		"PageNumber": strconv.Itoa(page),
		"PageSize":   strconv.Itoa(pageSize),
		"AccessType": "NS",
	}
	if keyword != "" {
		params["SiteName"] = keyword
	}

	result, err := p.request(ctx, params, "GET")
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if sites, ok := result["Sites"].([]interface{}); ok {
		for _, s := range sites {
			if site, ok := s.(map[string]interface{}); ok {
				domains = append(domains, dns.DomainInfo{
					ID:          fmt.Sprintf("%v", site["SiteId"]),
					Name:        site["SiteName"].(string),
					RecordCount: 0,
				})
			}
		}
	}

	total := int(result["TotalCount"].(float64))
	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	params := map[string]string{
		"Action":     "ListRecords",
		"SiteId":     p.domainID,
		"PageNumber": strconv.Itoa(page),
		"PageSize":   strconv.Itoa(pageSize),
	}

	if subDomain != "" {
		recordName := subDomain
		if subDomain == "@" {
			recordName = p.domain
		} else {
			recordName = subDomain + "." + p.domain
		}
		params["RecordName"] = recordName
	} else if keyword != "" {
		recordName := keyword
		if keyword == "@" {
			recordName = p.domain
		} else {
			recordName = keyword + "." + p.domain
		}
		params["RecordName"] = recordName
	}

	if recordType != "" {
		if recordType == "A" || recordType == "AAAA" {
			recordType = "A/AAAA"
		}
		params["Type"] = recordType
	}

	if line != "" {
		if line == "1" {
			params["Proxied"] = "true"
		} else {
			params["Proxied"] = "false"
		}
	}

	result, err := p.request(ctx, params, "GET")
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if recs, ok := result["Records"].([]interface{}); ok {
		for _, r := range recs {
			if rec, ok := r.(map[string]interface{}); ok {
				recordName := rec["RecordName"].(string)
				name := strings.TrimSuffix(recordName, "."+p.domain)
				if name == "" || name == p.domain {
					name = "@"
				}

				recType := rec["RecordType"].(string)
				var recValue string
				var mx int

				if data, ok := rec["Data"].(map[string]interface{}); ok {
					recValue = data["Value"].(string)

					if recType == "A/AAAA" {
						if strings.Contains(recValue, ":") {
							recType = "AAAA"
						} else {
							recType = "A"
						}
					}

					if priority, ok := data["Priority"].(float64); ok {
						mx = int(priority)
					}
				}

				lineVal := "0"
				if proxied, ok := rec["Proxied"].(bool); ok && proxied {
					lineVal = "1"
				}

				var remark string
				if comment, ok := rec["Comment"].(string); ok {
					remark = comment
				}

				records = append(records, dns.Record{
					ID:     rec["RecordId"].(string),
					Name:   name,
					Type:   recType,
					Value:  recValue,
					TTL:    int(rec["Ttl"].(float64)),
					Line:   lineVal,
					MX:     mx,
					Status: "enable",
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
	params := map[string]string{
		"Action":   "GetRecord",
		"RecordId": recordID,
	}

	result, err := p.request(ctx, params, "GET")
	if err != nil {
		return nil, err
	}

	if rec, ok := result["RecordModel"].(map[string]interface{}); ok {
		recordName := rec["RecordName"].(string)
		name := strings.TrimSuffix(recordName, "."+p.domain)
		if name == "" || name == p.domain {
			name = "@"
		}

		recType := rec["RecordType"].(string)
		var recValue string
		var mx int

		if data, ok := rec["Data"].(map[string]interface{}); ok {
			recValue = data["Value"].(string)

			if recType == "A/AAAA" {
				if strings.Contains(recValue, ":") {
					recType = "AAAA"
				} else {
					recType = "A"
				}
			}

			if priority, ok := data["Priority"].(float64); ok {
				mx = int(priority)
			}
		}

		lineVal := "0"
		if proxied, ok := rec["Proxied"].(bool); ok && proxied {
			lineVal = "1"
		}

		var remark string
		if comment, ok := rec["Comment"].(string); ok {
			remark = comment
		}

		return &dns.Record{
			ID:     rec["RecordId"].(string),
			Name:   name,
			Type:   recType,
			Value:  recValue,
			TTL:    int(rec["Ttl"].(float64)),
			Line:   lineVal,
			MX:     mx,
			Status: "enable",
			Remark: remark,
		}, nil
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

	if recordType == "A" || recordType == "AAAA" {
		recordType = "A/AAAA"
	}

	data := map[string]interface{}{"Value": value}
	if recordType == "MX" {
		data["Priority"] = mx
	}
	dataJSON, _ := json.Marshal(data)

	proxied := "false"
	if line == "1" {
		proxied = "true"
	}

	params := map[string]string{
		"Action":     "CreateRecord",
		"SiteId":     p.domainID,
		"RecordName": recordName,
		"Type":       recordType,
		"Proxied":    proxied,
		"Ttl":        strconv.Itoa(ttl),
		"Data":       string(dataJSON),
	}
	if remark != "" {
		params["Comment"] = remark
	}
	if line == "1" {
		params["BizName"] = "web"
	}

	result, err := p.request(ctx, params, "POST")
	if err != nil {
		return "", err
	}

	if recordID, ok := result["RecordId"].(string); ok {
		return recordID, nil
	}

	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	if recordType == "A" || recordType == "AAAA" {
		recordType = "A/AAAA"
	}

	data := map[string]interface{}{"Value": value}
	if recordType == "MX" {
		data["Priority"] = mx
	}
	dataJSON, _ := json.Marshal(data)

	proxied := "false"
	if line == "1" {
		proxied = "true"
	}

	params := map[string]string{
		"Action":   "UpdateRecord",
		"RecordId": recordID,
		"Type":     recordType,
		"Proxied":  proxied,
		"Ttl":      strconv.Itoa(ttl),
		"Data":     string(dataJSON),
	}
	if remark != "" {
		params["Comment"] = remark
	}
	if line == "1" {
		params["BizName"] = "web"
	}

	_, err := p.request(ctx, params, "POST")
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("阿里云ESA不支持单独修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	params := map[string]string{
		"Action":   "DeleteRecord",
		"RecordId": recordID,
	}

	_, err := p.request(ctx, params, "POST")
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	return fmt.Errorf("阿里云ESA不支持设置记录状态")
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("阿里云ESA不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "0", Name: "仅DNS"},
		{ID: "1", Name: "已代理"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 1
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	return fmt.Errorf("阿里云ESA不支持添加域名")
}
