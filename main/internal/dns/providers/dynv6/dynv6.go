package dynv6

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"main/internal/cache"
	"main/internal/dns"
)

func init() {
	dns.Register("dynv6", NewProvider, dns.ProviderConfig{
		Type: "dynv6",
		Name: "dynv6",
		Icon: "dynv6.ico",
		Config: []dns.ConfigField{
			{Name: "API Token", Key: "token", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 0, Status: false, Redirect: false, Log: false, Weight: false, Page: false, Add: false,
		},
	})
}

const (
	baseURL = "https://dynv6.com/api/v2"
	// zoneCacheTTL zoneID 对同一凭据与域名稳定，缓存以减少列表接口的重复往返
	zoneCacheTTL = 10 * time.Minute
	zoneCacheKey = "dynv6_zone:"
	// defaultRecordTTL dynv6 记录接口不返回 TTL，展示与写回使用固定值
	defaultRecordTTL = 600
)

/*
 * Provider dynv6 服务商
 */
type Provider struct {
	token   string
	domain  string
	client  *http.Client
	lastErr string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		token:  config["token"],
		domain: domain,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

/* zone dynv6 区域，id 可能是数字或字符串，故按 JSON 原始字面量保留 */
type zone struct {
	ID   json.RawMessage `json:"id"`
	Name string          `json:"name"`
}

/* record dynv6 解析记录，id 与 priority 同样可能是数字或字符串 */
type record struct {
	ID       json.RawMessage `json:"id"`
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Data     string          `json:"data"`
	Priority json.RawMessage `json:"priority"`
}

/* idText 还原 ID 字段的文本形式，兼容数字与字符串两种返回 */
func idText(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		var unquoted string
		if json.Unmarshal(raw, &unquoted) == nil {
			return unquoted
		}
	}
	return text
}

/* decode 将任意 JSON 值解析为目标结构 */
func decode(data interface{}, dest interface{}) bool {
	encoded, err := json.Marshal(data)
	if err != nil {
		return false
	}
	return json.Unmarshal(encoded, dest) == nil
}

func (p *Provider) setError(err error) error {
	if err != nil {
		p.lastErr = err.Error()
	}
	return err
}

/*
 * request 发送 dynv6 API 请求
 * params 仅在 POST/PATCH 时作为 JSON 请求体；2xx 且无响应体时返回 nil 表示成功
 */
func (p *Provider) request(ctx context.Context, method, path string, params map[string]interface{}) (interface{}, error) {
	var body io.Reader
	if len(params) > 0 {
		encoded, err := json.Marshal(params)
		if err != nil {
			return nil, p.setError(err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, p.setError(err)
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, p.setError(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, p.setError(err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if len(bytes.TrimSpace(respBody)) == 0 {
			return nil, nil
		}
		var parsed interface{}
		if json.Unmarshal(respBody, &parsed) != nil {
			return true, nil
		}
		return parsed, nil
	}

	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(respBody, &errResp)
	switch {
	case errResp.Error != "":
		return nil, p.setError(fmt.Errorf("%s", errResp.Error))
	case errResp.Message != "":
		return nil, p.setError(fmt.Errorf("%s", errResp.Message))
	default:
		return nil, p.setError(fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody)))
	}
}

/* zoneCacheKeyOf 按凭据与域名隔离缓存，避免不同账户的同名域名互相覆盖 */
func (p *Provider) zoneCacheKeyOf() string {
	sum := sha256.Sum256([]byte(p.token))
	return zoneCacheKey + hex.EncodeToString(sum[:8]) + ":" + p.domain
}

/* zoneID 查询域名的区域ID，结果按 TTL 缓存 */
func (p *Provider) zoneID(ctx context.Context) (string, error) {
	cacheKey := p.zoneCacheKeyOf()
	if cache.C != nil {
		if id, ok := cache.C.Get(cacheKey); ok && id != "" {
			return id, nil
		}
	}

	data, err := p.request(ctx, "GET", "/zones/by-name/"+p.domain, nil)
	if err != nil {
		return "", err
	}
	var z zone
	if !decode(data, &z) {
		return "", p.setError(fmt.Errorf("无法获取域名的Zone ID，请确认域名已添加到dynv6"))
	}
	id := idText(z.ID)
	if id == "" {
		return "", p.setError(fmt.Errorf("无法获取域名的Zone ID，请确认域名已添加到dynv6"))
	}

	if cache.C != nil {
		// 缓存写入失败不影响本次请求结果
		_ = cache.C.Set(cacheKey, id, zoneCacheTTL)
	}
	return id, nil
}

/* toList 将响应体统一为对象列表，兼容空数组、单对象与数组三种返回形态 */
func toList(data interface{}) []interface{} {
	if list, ok := data.([]interface{}); ok {
		return list
	}
	if _, ok := data.(map[string]interface{}); ok {
		return []interface{}{data}
	}
	return nil
}

/* relativeName 将 dynv6 返回的完整域名还原为主机记录 */
func (p *Provider) relativeName(name string) string {
	if name == "" || name == p.domain {
		return "@"
	}
	suffix := "." + p.domain
	if len(name) > len(suffix) && strings.HasSuffix(name, suffix) {
		return strings.TrimSuffix(name, suffix)
	}
	return name
}

/* fullName 将主机记录还原为提交给 dynv6 的名称，其余情况交由服务端自动展开 */
func (p *Provider) fullName(name string) string {
	if name == "@" || name == "" {
		return p.domain
	}
	return name
}

/* formatValue CNAME/MX 指向的完整域名补末尾点，纯相对名称交由服务端展开 */
func formatValue(recordType, value string) string {
	if recordType != "CNAME" && recordType != "MX" {
		return value
	}
	if value == "" || strings.HasSuffix(value, ".") || !strings.Contains(value, ".") {
		return value
	}
	return value + "."
}

/* recordParams 构造记录写入参数，MX 记录额外携带优先级 */
func recordParams(name, recordType, value string, mx int) map[string]interface{} {
	params := map[string]interface{}{
		"name": name,
		"type": recordType,
		"data": value,
	}
	if recordType == "MX" {
		params["priority"] = mx
	}
	return params
}

func filterRecords(records []dns.Record, keep func(dns.Record) bool) []dns.Record {
	out := make([]dns.Record, 0, len(records))
	for _, r := range records {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func (p *Provider) Check(ctx context.Context) error {
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	data, err := p.request(ctx, "GET", "/zones", nil)
	if err != nil {
		return nil, err
	}

	domains := make([]dns.DomainInfo, 0)
	for _, item := range toList(data) {
		var z zone
		if !decode(item, &z) {
			continue
		}
		id := idText(z.ID)
		if id == "" || z.Name == "" {
			continue
		}
		if keyword != "" && !strings.Contains(z.Name, keyword) {
			continue
		}
		domains = append(domains, dns.DomainInfo{ID: id, Name: z.Name})
	}

	return &dns.PageResult{Total: len(domains), Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	zoneID, err := p.zoneID(ctx)
	if err != nil {
		return nil, err
	}

	data, err := p.request(ctx, "GET", "/zones/"+zoneID+"/records", nil)
	if err != nil {
		return nil, err
	}

	items := toList(data)
	records := make([]dns.Record, 0, len(items))
	for _, item := range items {
		var row record
		if !decode(item, &row) {
			continue
		}
		convertedType := strings.ToUpper(row.Type)
		mx := 0
		if convertedType == "MX" {
			mx, _ = strconv.Atoi(idText(row.Priority))
		}
		records = append(records, dns.Record{
			ID:     idText(row.ID),
			Name:   p.relativeName(row.Name),
			Type:   convertedType,
			Value:  row.Data,
			Line:   "default",
			TTL:    defaultRecordTTL,
			MX:     mx,
			Status: "enable",
		})
	}

	if subDomain != "" {
		records = filterRecords(records, func(r dns.Record) bool {
			return strings.EqualFold(r.Name, subDomain)
		})
	} else {
		if keyword != "" {
			records = filterRecords(records, func(r dns.Record) bool {
				return strings.Contains(r.Name, keyword) || strings.Contains(r.Value, keyword)
			})
		}
		if value != "" {
			records = filterRecords(records, func(r dns.Record) bool { return r.Value == value })
		}
		if recordType != "" {
			records = filterRecords(records, func(r dns.Record) bool { return r.Type == recordType })
		}
	}

	return &dns.PageResult{Total: len(records), Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	return nil, fmt.Errorf("dynv6不支持获取单条记录")
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if value == "" {
		p.lastErr = "记录值不能为空"
		return "", fmt.Errorf("记录值不能为空")
	}

	zoneID, err := p.zoneID(ctx)
	if err != nil {
		return "", err
	}

	data, err := p.request(ctx, "POST", "/zones/"+zoneID+"/records", recordParams(
		p.fullName(name), recordType, formatValue(recordType, value), mx))
	if err != nil {
		return "", err
	}
	var created record
	if !decode(data, &created) {
		return "", nil
	}
	return idText(created.ID), nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	if value == "" {
		p.lastErr = "记录值不能为空"
		return fmt.Errorf("记录值不能为空")
	}

	zoneID, err := p.zoneID(ctx)
	if err != nil {
		return err
	}

	_, err = p.request(ctx, "PATCH", "/zones/"+zoneID+"/records/"+recordID, recordParams(
		p.fullName(name), recordType, formatValue(recordType, value), mx))
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("dynv6不支持修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	zoneID, err := p.zoneID(ctx)
	if err != nil {
		return err
	}
	_, err = p.request(ctx, "DELETE", "/zones/"+zoneID+"/records/"+recordID, nil)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	return fmt.Errorf("dynv6不支持设置记录状态")
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("dynv6不支持查看解析日志")
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
	return fmt.Errorf("dynv6不支持添加域名")
}
