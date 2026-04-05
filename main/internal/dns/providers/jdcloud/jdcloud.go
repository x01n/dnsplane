package jdcloud

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/dns"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

func init() {
	dns.Register("jdcloud", NewProvider, dns.ProviderConfig{
		Type: "jdcloud",
		Name: "京东云",
		Icon: "jdcloud.png",
		Config: []dns.ConfigField{
			{Name: "AccessKey", Key: "access_key", Type: "input", Required: true},
			{Name: "SecretKey", Key: "secret_key", Type: "input", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{
				{Value: "0", Label: "否"},
				{Value: "1", Label: "是"},
			}, Value: "0"},
		},
		Features: dns.ProviderFeatures{
			Remark: 0, Status: true, Redirect: false, Log: false, Weight: false, Page: false, Add: true,
		},
	})
}

const (
	jdcloudHost    = "domainservice.jdcloud-api.com"
	jdcloudRegion  = "cn-north-1"
	jdcloudService = "domainservice"
)

type Provider struct {
	accessKey string
	secretKey string
	domain    string
	domainID  string
	proxy     bool
	client    *http.Client
	lastErr   string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		accessKey: config["access_key"],
		secretKey: config["secret_key"],
		domain:    domain,
		domainID:  domainID,
		proxy:     config["proxy"] == "1",
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func (p *Provider) sha256Hash(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func (p *Provider) sign(method, uri string, headers map[string]string, payload string, t time.Time) string {
	dateStamp := t.UTC().Format("20060102")
	amzDate := t.UTC().Format("20060102T150405Z")

	headers["x-jdcloud-date"] = amzDate
	headers["x-jdcloud-nonce"] = uuid.New().String()

	var signedHeaderKeys []string
	for k := range headers {
		signedHeaderKeys = append(signedHeaderKeys, strings.ToLower(k))
	}
	sort.Strings(signedHeaderKeys)
	signedHeaders := strings.Join(signedHeaderKeys, ";")

	var canonicalHeaders strings.Builder
	for _, k := range signedHeaderKeys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(headers[k]))
		canonicalHeaders.WriteString("\n")
	}

	payloadHash := p.sha256Hash(payload)
	canonicalRequest := fmt.Sprintf("%s\n%s\n\n%s\n%s\n%s",
		method, uri, canonicalHeaders.String(), signedHeaders, payloadHash)

	algorithm := "JDCLOUD2-HMAC-SHA256"
	credentialScope := fmt.Sprintf("%s/%s/%s/jdcloud2_request", dateStamp, jdcloudRegion, jdcloudService)
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s",
		algorithm, amzDate, credentialScope, p.sha256Hash(canonicalRequest))

	kDate := p.hmacSHA256([]byte("JDCLOUD2"+p.secretKey), dateStamp)
	kRegion := p.hmacSHA256(kDate, jdcloudRegion)
	kService := p.hmacSHA256(kRegion, jdcloudService)
	kSigning := p.hmacSHA256(kService, "jdcloud2_request")
	signature := hex.EncodeToString(p.hmacSHA256(kSigning, stringToSign))

	return fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, p.accessKey, credentialScope, signedHeaders, signature)
}

func (p *Provider) request(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	var payload string
	if body != nil {
		bodyBytes, _ := json.Marshal(body)
		payload = string(bodyBytes)
	}

	url := fmt.Sprintf("https://%s%s", jdcloudHost, path)
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(payload))
	if err != nil {
		return nil, err
	}

	t := time.Now()
	headers := map[string]string{
		"host":         jdcloudHost,
		"content-type": "application/json",
	}

	auth := p.sign(method, path, headers, payload, t)

	req.Header.Set("Host", jdcloudHost)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-jdcloud-date", headers["x-jdcloud-date"])
	req.Header.Set("x-jdcloud-nonce", headers["x-jdcloud-nonce"])
	req.Header.Set("Authorization", auth)

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

	if errObj, ok := result["error"].(map[string]interface{}); ok {
		msg := errObj["message"].(string)
		p.lastErr = msg
		return nil, fmt.Errorf("京东云API错误: %s", msg)
	}

	if resultData, ok := result["result"].(map[string]interface{}); ok {
		return resultData, nil
	}

	return result, nil
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.request(ctx, "GET", "/v2/regions/cn-north-1/domain", nil)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	path := fmt.Sprintf("/v2/regions/%s/domain?pageNumber=%d&pageSize=%d", jdcloudRegion, page, pageSize)
	if keyword != "" {
		path += "&domainName=" + keyword
	}

	result, err := p.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	if dataList, ok := result["dataList"].([]interface{}); ok {
		for _, item := range dataList {
			if d, ok := item.(map[string]interface{}); ok {
				id := ""
				if idVal, ok := d["id"].(float64); ok {
					id = fmt.Sprintf("%.0f", idVal)
				}
				domains = append(domains, dns.DomainInfo{
					ID:   id,
					Name: d["domainName"].(string),
				})
			}
		}
	}

	total := 0
	if totalCount, ok := result["totalCount"].(float64); ok {
		total = int(totalCount)
	}

	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	path := fmt.Sprintf("/v2/regions/%s/domain/%s/RR?pageNumber=%d&pageSize=%d", jdcloudRegion, p.domainID, page, pageSize)

	result, err := p.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	if dataList, ok := result["dataList"].([]interface{}); ok {
		for _, item := range dataList {
			if r, ok := item.(map[string]interface{}); ok {
				record := dns.Record{
					Name:  getString(r, "hostRecord"),
					Type:  getString(r, "type"),
					Value: getString(r, "hostValue"),
					TTL:   getInt(r, "ttl"),
					Line:  getString(r, "viewName"),
				}
				if idVal, ok := r["id"].(float64); ok {
					record.ID = fmt.Sprintf("%.0f", idVal)
				}

				if subDomain != "" && record.Name != subDomain {
					continue
				}
				if recordType != "" && record.Type != recordType {
					continue
				}
				if value != "" && record.Value != value {
					continue
				}

				records = append(records, record)
			}
		}
	}

	total := 0
	if totalCount, ok := result["totalCount"].(float64); ok {
		total = int(totalCount)
	}

	return &dns.PageResult{Total: total, Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	path := fmt.Sprintf("/v2/regions/%s/domain/%s/RR/%s", jdcloudRegion, p.domainID, recordID)

	result, err := p.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	return &dns.Record{
		ID:    recordID,
		Name:  getString(result, "hostRecord"),
		Type:  getString(result, "type"),
		Value: getString(result, "hostValue"),
		TTL:   getInt(result, "ttl"),
		Line:  getString(result, "viewName"),
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	path := fmt.Sprintf("/v2/regions/%s/domain/%s/RR", jdcloudRegion, p.domainID)

	body := map[string]interface{}{
		"hostRecord": name,
		"type":       recordType,
		"hostValue":  value,
		"ttl":        ttl,
		"viewValue":  getViewValue(line),
	}
	if recordType == "MX" {
		body["priority"] = mx
	}

	result, err := p.request(ctx, "POST", path, body)
	if err != nil {
		return "", err
	}

	if dataList, ok := result["dataList"].([]interface{}); ok && len(dataList) > 0 {
		if first, ok := dataList[0].(map[string]interface{}); ok {
			if id, ok := first["id"].(float64); ok {
				return fmt.Sprintf("%.0f", id), nil
			}
		}
	}

	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	path := fmt.Sprintf("/v2/regions/%s/domain/%s/RR/%s", jdcloudRegion, p.domainID, recordID)

	body := map[string]interface{}{
		"hostRecord": name,
		"type":       recordType,
		"hostValue":  value,
		"ttl":        ttl,
		"viewValue":  getViewValue(line),
	}
	if recordType == "MX" {
		body["priority"] = mx
	}

	_, err := p.request(ctx, "PUT", path, body)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return nil
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	path := fmt.Sprintf("/v2/regions/%s/domain/%s/RR/%s", jdcloudRegion, p.domainID, recordID)
	_, err := p.request(ctx, "DELETE", path, nil)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	path := fmt.Sprintf("/v2/regions/%s/domain/%s/RR/%s/operate", jdcloudRegion, p.domainID, recordID)
	action := "disable"
	if enable {
		action = "enable"
	}
	_, err := p.request(ctx, "PUT", path, map[string]interface{}{"action": action})
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return &dns.PageResult{Total: 0, Records: []interface{}{}}, nil
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "default", Name: "默认"},
		{ID: "telecom", Name: "电信"},
		{ID: "unicom", Name: "联通"},
		{ID: "mobile", Name: "移动"},
		{ID: "oversea", Name: "海外"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 600
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	path := fmt.Sprintf("/v2/regions/%s/domain", jdcloudRegion)
	_, err := p.request(ctx, "POST", path, map[string]interface{}{
		"packId":     1,
		"domainName": domain,
	})
	return err
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getViewValue(line string) int {
	switch line {
	case "telecom":
		return 1
	case "unicom":
		return 2
	case "mobile":
		return 3
	case "oversea":
		return 4
	default:
		return -1
	}
}
