package dnsmgr

import (
	"context"
	"crypto/md5"
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
	dns.Register("dnsmgr", NewProvider, dns.ProviderConfig{
		Type: "dnsmgr",
		Name: "DNSmgr(同系统对接)",
		Icon: "dnsmgr.ico",
		Config: []dns.ConfigField{
			{Name: "接口地址", Key: "url", Type: "input", Placeholder: "如 http://xxx.com", Required: true},
			{Name: "用户ID", Key: "uid", Type: "input", Required: true},
			{Name: "API Key", Key: "apikey", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 1, Status: true, Redirect: false, Log: false, Weight: false, Page: false, Add: false,
		},
	})
}

/* Provider dnsmgr 同系统对接服务商 */
type Provider struct {
	baseURL  string
	uid      string
	apiKey   string
	domain   string
	domainID string
	client   *http.Client
	lastErr  string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		baseURL:  strings.TrimRight(config["url"], "/"),
		uid:      config["uid"],
		apiKey:   config["apikey"],
		domain:   domain,
		domainID: domainID,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 10)
	return err
}

/* sign 生成请求签名 */
func (p *Provider) sign(timestamp string) string {
	h := md5.Sum([]byte(p.uid + timestamp + p.apiKey))
	return fmt.Sprintf("%x", h)
}

/* request 发送API请求 */
func (p *Provider) request(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sign := p.sign(timestamp)

	form := url.Values{}
	form.Set("uid", p.uid)
	form.Set("timestamp", timestamp)
	form.Set("sign", sign)
	for k, v := range params {
		if v != "" {
			form.Set(k, v)
		}
	}

	reqURL := p.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
		return nil, fmt.Errorf("响应解析失败: %s", string(body))
	}

	if code, ok := result["code"]; ok {
		codeVal, _ := toFloat64(code)
		if int(codeVal) != 0 {
			msg := "未知错误"
			if m, ok := result["msg"].(string); ok {
				msg = m
			}
			p.lastErr = msg
			return nil, fmt.Errorf("%s", msg)
		}
	}

	return result, nil
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	offset := (page - 1) * pageSize
	params := map[string]string{
		"offset": strconv.Itoa(offset),
		"limit":  strconv.Itoa(pageSize),
	}
	if keyword != "" {
		params["kw"] = keyword
	}

	result, err := p.request(ctx, "/api/domain", params)
	if err != nil {
		return nil, err
	}

	total := 0
	if t, ok := result["total"]; ok {
		if f, ok := toFloat64(t); ok {
			total = int(f)
		}
	}

	var records []dns.DomainInfo
	if rows, ok := result["rows"].([]interface{}); ok {
		for _, item := range rows {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			info := dns.DomainInfo{
				ID:   fmt.Sprintf("%v", row["id"]),
				Name: fmt.Sprintf("%v", row["name"]),
			}
			if rc, ok := row["recordcount"]; ok {
				if f, ok := toFloat64(rc); ok {
					info.RecordCount = int(f)
				}
			}
			records = append(records, info)
		}
	} else if data, ok := result["data"]; ok {
		if dataMap, ok := data.(map[string]interface{}); ok {
			if t, ok := dataMap["total"]; ok {
				if f, ok := toFloat64(t); ok {
					total = int(f)
				}
			}
			if rows, ok := dataMap["rows"].([]interface{}); ok {
				for _, item := range rows {
					row, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					info := dns.DomainInfo{
						ID:   fmt.Sprintf("%v", row["id"]),
						Name: fmt.Sprintf("%v", row["name"]),
					}
					if rc, ok := row["recordcount"]; ok {
						if f, ok := toFloat64(rc); ok {
							info.RecordCount = int(f)
						}
					}
					records = append(records, info)
				}
			}
		}
	}

	return &dns.PageResult{Total: total, Records: records}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	offset := (page - 1) * pageSize
	params := map[string]string{
		"offset": strconv.Itoa(offset),
		"limit":  strconv.Itoa(pageSize),
	}
	if keyword != "" {
		params["keyword"] = keyword
	}
	if subDomain != "" {
		params["subdomain"] = subDomain
	}
	if value != "" {
		params["value"] = value
	}
	if recordType != "" {
		params["type"] = recordType
	}
	if line != "" {
		params["line"] = line
	}
	if status != "" {
		params["status"] = status
	}

	result, err := p.request(ctx, fmt.Sprintf("/api/domain/record?id=%s", p.domainID), params)
	if err != nil {
		return nil, err
	}

	return p.parseRecordList(result)
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

/* parseRecordList 解析记录列表响应 */
func (p *Provider) parseRecordList(result map[string]interface{}) (*dns.PageResult, error) {
	total := 0
	var rows []interface{}

	if t, ok := result["total"]; ok {
		if f, ok := toFloat64(t); ok {
			total = int(f)
		}
	}
	if r, ok := result["rows"].([]interface{}); ok {
		rows = r
	} else if data, ok := result["data"].(map[string]interface{}); ok {
		if t, ok := data["total"]; ok {
			if f, ok := toFloat64(t); ok {
				total = int(f)
			}
		}
		if r, ok := data["rows"].([]interface{}); ok {
			rows = r
		}
	}

	var records []dns.Record
	for _, item := range rows {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		records = append(records, p.mapRecord(row))
	}

	return &dns.PageResult{Total: total, Records: records}, nil
}

/* mapRecord 将响应数据映射为 Record 结构 */
func (p *Provider) mapRecord(row map[string]interface{}) dns.Record {
	r := dns.Record{
		ID:    fmt.Sprintf("%v", row["id"]),
		Name:  fmt.Sprintf("%v", row["name"]),
		Type:  fmt.Sprintf("%v", row["type"]),
		Value: fmt.Sprintf("%v", row["value"]),
		Line:  fmt.Sprintf("%v", row["line"]),
	}
	if ttl, ok := row["ttl"]; ok {
		if f, ok := toFloat64(ttl); ok {
			r.TTL = int(f)
		}
	}
	if mx, ok := row["mx"]; ok {
		if f, ok := toFloat64(mx); ok {
			r.MX = int(f)
		}
	}
	if weight, ok := row["weight"]; ok {
		if f, ok := toFloat64(weight); ok {
			r.Weight = int(f)
		}
	}
	if status, ok := row["status"].(string); ok {
		r.Status = status
	}
	if remark, ok := row["remark"].(string); ok {
		r.Remark = remark
	}
	if updated, ok := row["updated"].(string); ok {
		r.Updated = updated
	}
	return r
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	params := map[string]string{"record_id": recordID}
	result, err := p.request(ctx, fmt.Sprintf("/api/domain/record/info?id=%s", p.domainID), params)
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	if d, ok := result["data"].(map[string]interface{}); ok {
		data = d
	} else {
		return nil, fmt.Errorf("解析记录详情失败")
	}

	record := p.mapRecord(data)
	return &record, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	params := map[string]string{
		"name":  name,
		"type":  recordType,
		"value": value,
		"line":  line,
		"ttl":   strconv.Itoa(ttl),
		"mx":    strconv.Itoa(mx),
	}
	if weight != nil {
		params["weight"] = strconv.Itoa(*weight)
	}
	if remark != "" {
		params["remark"] = remark
	}

	result, err := p.request(ctx, fmt.Sprintf("/api/domain/record/add?id=%s", p.domainID), params)
	if err != nil {
		return "", err
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		if id, ok := data["id"]; ok {
			return fmt.Sprintf("%v", id), nil
		}
	}
	return "", nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	params := map[string]string{
		"record_id": recordID,
		"name":      name,
		"type":      recordType,
		"value":     value,
		"line":      line,
		"ttl":       strconv.Itoa(ttl),
		"mx":        strconv.Itoa(mx),
	}
	if weight != nil {
		params["weight"] = strconv.Itoa(*weight)
	}
	if remark != "" {
		params["remark"] = remark
	}

	_, err := p.request(ctx, fmt.Sprintf("/api/domain/record/edit?id=%s", p.domainID), params)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	params := map[string]string{
		"record_id": recordID,
		"remark":    remark,
	}
	_, err := p.request(ctx, fmt.Sprintf("/api/domain/record/remark?id=%s", p.domainID), params)
	return err
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	params := map[string]string{"record_id": recordID}
	_, err := p.request(ctx, fmt.Sprintf("/api/domain/record/del?id=%s", p.domainID), params)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	status := "disable"
	if enable {
		status = "enable"
	}
	params := map[string]string{
		"record_id": recordID,
		"status":    status,
	}
	_, err := p.request(ctx, fmt.Sprintf("/api/domain/record/status?id=%s", p.domainID), params)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	offset := (page - 1) * pageSize
	params := map[string]string{
		"offset": strconv.Itoa(offset),
		"limit":  strconv.Itoa(pageSize),
	}
	if keyword != "" {
		params["keyword"] = keyword
	}

	result, err := p.request(ctx, fmt.Sprintf("/api/domain/record/log?id=%s", p.domainID), params)
	if err != nil {
		return nil, err
	}

	return p.parseRecordList(result)
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	result, err := p.request(ctx, fmt.Sprintf("/api/domain/record/line?id=%s", p.domainID), nil)
	if err != nil {
		return nil, err
	}

	var lines []dns.RecordLine
	var dataList []interface{}
	if data, ok := result["data"].([]interface{}); ok {
		dataList = data
	} else if dataMap, ok := result["data"].(map[string]interface{}); ok {
		if rows, ok := dataMap["rows"].([]interface{}); ok {
			dataList = rows
		}
	}

	for _, item := range dataList {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		lines = append(lines, dns.RecordLine{
			ID:   fmt.Sprintf("%v", row["id"]),
			Name: fmt.Sprintf("%v", row["name"]),
		})
	}

	if len(lines) == 0 {
		lines = append(lines, dns.RecordLine{ID: "0", Name: "默认"})
	}
	return lines, nil
}

func (p *Provider) GetMinTTL() int {
	return 60
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	return fmt.Errorf("dnsmgr对接模式不支持添加域名")
}
