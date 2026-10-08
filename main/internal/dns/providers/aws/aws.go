package aws

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"

	"main/internal/dns"
)

func init() {
	dns.Register("aws", NewProvider, dns.ProviderConfig{
		Type: "aws",
		Name: "AWS Route 53",
		Icon: "aws.png",
		Note: "基于 Amazon Route 53 管理域名解析，AccessKey 需具备 Route 53 权限",
		Config: []dns.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 0, Status: false, Redirect: false, Log: false, Weight: false, Page: true, Add: true,
		},
	})
}

const (
	awsEndpoint = "route53.amazonaws.com"
	awsHost     = "https://" + awsEndpoint
	awsService  = "route53"
	awsVersion  = "2013-04-01"
	awsRegion   = "us-east-1"
	// recordSep 记录ID中分隔名称/类型/记录值的控制字符，取值须稳定否则历史记录ID解析错位
	recordSep = "\x1e"
	// maxPages 分页保护上限，防止服务端异常返回导致无限翻页
	maxPages = 200
)

/*
 * Provider AWS Route 53 服务商
 */
type Provider struct {
	accessKeyID     string
	secretAccessKey string
	domain          string
	domainID        string
	client          *http.Client
	lastErr         string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	return &Provider{
		accessKeyID:     config["AccessKeyId"],
		secretAccessKey: config["SecretAccessKey"],
		domain:          domain,
		domainID:        domainID,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

/* hostedZone Route53 托管区域 */
type hostedZone struct {
	ID                     string `xml:"Id"`
	Name                   string `xml:"Name"`
	ResourceRecordSetCount int    `xml:"ResourceRecordSetCount"`
}

/* resourceRecord 单条记录值 */
type resourceRecord struct {
	Value string `xml:"Value"`
}

/* resourceRecordSet 同一名称与类型的记录集合 */
type resourceRecordSet struct {
	Name            string           `xml:"Name"`
	Type            string           `xml:"Type"`
	TTL             int              `xml:"TTL"`
	ResourceRecords []resourceRecord `xml:"ResourceRecords>ResourceRecord"`
	AliasTarget     *struct{}        `xml:"AliasTarget"`
	SetIdentifier   string           `xml:"SetIdentifier"`
}

type listHostedZonesResponse struct {
	HostedZones []hostedZone `xml:"HostedZones>HostedZone"`
	IsTruncated bool         `xml:"IsTruncated"`
	NextMarker  string       `xml:"NextMarker"`
}

type listRecordSetsResponse struct {
	ResourceRecordSets   []resourceRecordSet `xml:"ResourceRecordSets>ResourceRecordSet"`
	IsTruncated          bool                `xml:"IsTruncated"`
	NextRecordName       string              `xml:"NextRecordName"`
	NextRecordType       string              `xml:"NextRecordType"`
	NextRecordIdentifier string              `xml:"NextRecordIdentifier"`
}

type createHostedZoneResponse struct {
	HostedZone hostedZone `xml:"HostedZone"`
}

/* errorResponse Route53 错误响应，Message 为空时回退到 HTTP 状态码 */
type errorResponse struct {
	Error struct {
		Message string `xml:"Message"`
	} `xml:"Error"`
}

/* change 一次记录变更，Action 取 CREATE/DELETE/UPSERT */
type change struct {
	Action string
	Name   string
	Type   string
	TTL    int
	Values []string
}

/* parsedValue 归一化后的记录值，MX 记录额外携带优先级 */
type parsedValue struct {
	Value string
	MX    int
}

/* awsEscape 按 SigV4 要求做 URI 编码，保留 RFC 3986 未保留字符 */
func awsEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteString(fmt.Sprintf("%%%02X", c))
	}
	return b.String()
}

/* canonicalURI 对路径逐段编码，斜杠保留为分隔符 */
func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = awsEscape(part)
	}
	return strings.Join(parts, "/")
}

/* canonicalQuery 按参数名排序后编码，URL 与签名共用同一结果以保证签名一致 */
func canonicalQuery(query map[string]string) string {
	if len(query) == 0 {
		return ""
	}
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(awsEscape(k))
		b.WriteByte('=')
		b.WriteString(awsEscape(query[k]))
	}
	return b.String()
}

/* canonicalHeaders 小写化并按名称排序，同时给出 SignedHeaders 列表 */
func canonicalHeaders(headers map[string]string) (string, string) {
	keys := make([]string, 0, len(headers))
	values := make(map[string]string, len(headers))
	for k, v := range headers {
		lower := strings.ToLower(k)
		keys = append(keys, lower)
		values[lower] = strings.TrimSpace(v)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(values[k])
		b.WriteByte('\n')
	}
	return b.String(), strings.Join(keys, ";")
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

/* sign 生成 AWS4-HMAC-SHA256 授权头 */
func (p *Provider) sign(method, path string, query, headers map[string]string, body, amzDate, shortDate string) string {
	canonicalHeaderBlock, signedHeaders := canonicalHeaders(headers)
	// canonicalHeaderBlock 自身以换行结尾，Join 产生的分隔换行即为规范要求的空行
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI(path),
		canonicalQuery(query),
		canonicalHeaderBlock,
		signedHeaders,
		sha256Hex(body),
	}, "\n")

	scope := strings.Join([]string{shortDate, awsRegion, awsService, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+p.secretAccessKey), shortDate)
	kRegion := hmacSHA256(kDate, awsRegion)
	kService := hmacSHA256(kRegion, awsService)
	kSigning := hmacSHA256(kService, "aws4_request")

	return "AWS4-HMAC-SHA256 Credential=" + p.accessKeyID + "/" + scope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + hex.EncodeToString(hmacSHA256(kSigning, stringToSign))
}

/* sendRequest 发送已签名的 Route53 请求并将响应解析到 out */
func (p *Provider) sendRequest(ctx context.Context, method, path string, query map[string]string, body string, out interface{}) error {
	if query == nil {
		query = map[string]string{}
	}
	fullPath := "/" + awsVersion + path

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")

	headers := map[string]string{
		"Host":       awsEndpoint,
		"X-Amz-Date": amzDate,
	}
	// Route53 仅在携带请求体时声明 Content-Type
	if method != "GET" && method != "DELETE" {
		headers["Content-Type"] = "application/xml"
	}

	reqURL := awsHost + fullPath
	if encoded := canonicalQuery(query); encoded != "" {
		reqURL += "?" + encoded
	}

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return err
	}
	for k, v := range headers {
		// Host 由 net/http 依据 URL 生成，重复设置不会生效
		if k == "Host" {
			continue
		}
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", p.sign(method, fullPath, query, headers, body, amzDate, shortDate))

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(respBody) == 0 {
			return nil
		}
		return xml.Unmarshal(respBody, out)
	}

	var errResp errorResponse
	if xml.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
		return fmt.Errorf("%s", errResp.Error.Message)
	}
	return fmt.Errorf("HTTP Code: %d", resp.StatusCode)
}

/* escapeXML 转义将写入请求体的文本内容 */
func escapeXML(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

/* zoneID 去掉 Route53 返回的 /hostedzone/ 前缀 */
func (p *Provider) zoneID() string {
	return strings.ReplaceAll(p.domainID, "/hostedzone/", "")
}

func (p *Provider) listAllHostedZones(ctx context.Context) ([]hostedZone, error) {
	var all []hostedZone
	marker := ""
	for i := 0; i < maxPages; i++ {
		query := map[string]string{"maxitems": "100"}
		if marker != "" {
			query["marker"] = marker
		}
		var resp listHostedZonesResponse
		if err := p.sendRequest(ctx, "GET", "/hostedzone", query, "", &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.HostedZones...)
		if !resp.IsTruncated || resp.NextMarker == "" {
			break
		}
		marker = resp.NextMarker
	}
	return all, nil
}

func (p *Provider) listAllRecordSets(ctx context.Context) ([]resourceRecordSet, error) {
	var all []resourceRecordSet
	query := map[string]string{"maxitems": "300"}
	for i := 0; i < maxPages; i++ {
		var resp listRecordSetsResponse
		if err := p.sendRequest(ctx, "GET", "/hostedzone/"+p.zoneID()+"/rrset", query, "", &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.ResourceRecordSets...)
		if !resp.IsTruncated {
			break
		}
		next := map[string]string{"maxitems": "300"}
		if resp.NextRecordName != "" {
			next["name"] = resp.NextRecordName
		}
		if resp.NextRecordType != "" {
			next["type"] = resp.NextRecordType
		}
		if resp.NextRecordIdentifier != "" {
			next["identifier"] = resp.NextRecordIdentifier
		}
		query = next
	}
	return all, nil
}

/* getRecordSet 精确查询单条记录集合，不存在或为别名/加权记录时返回 nil */
func (p *Provider) getRecordSet(ctx context.Context, fqdn, recordType string) (*resourceRecordSet, error) {
	var resp listRecordSetsResponse
	err := p.sendRequest(ctx, "GET", "/hostedzone/"+p.zoneID()+"/rrset", map[string]string{
		"name":     fqdn,
		"type":     recordType,
		"maxitems": "1",
	}, "", &resp)
	if err != nil {
		return nil, err
	}
	if len(resp.ResourceRecordSets) == 0 {
		return nil, nil
	}
	set := resp.ResourceRecordSets[0]
	if !strings.EqualFold(strings.TrimRight(set.Name, "."), strings.TrimRight(fqdn, ".")) ||
		!strings.EqualFold(set.Type, recordType) {
		return nil, nil
	}
	if set.AliasTarget != nil || set.SetIdentifier != "" {
		return nil, nil
	}
	return &set, nil
}

func (p *Provider) changeRecordSets(ctx context.Context, changes []change) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<ChangeResourceRecordSetsRequest xmlns="https://route53.amazonaws.com/doc/2013-04-01/">`)
	b.WriteString(`<ChangeBatch><Changes>`)
	for _, c := range changes {
		b.WriteString(`<Change><Action>` + escapeXML(c.Action) + `</Action>`)
		b.WriteString(`<ResourceRecordSet>`)
		b.WriteString(`<Name>` + escapeXML(c.Name) + `</Name>`)
		b.WriteString(`<Type>` + escapeXML(c.Type) + `</Type>`)
		b.WriteString(`<TTL>` + strconv.Itoa(c.TTL) + `</TTL>`)
		b.WriteString(`<ResourceRecords>`)
		for _, v := range c.Values {
			b.WriteString(`<ResourceRecord><Value>` + escapeXML(v) + `</Value></ResourceRecord>`)
		}
		b.WriteString(`</ResourceRecords></ResourceRecordSet></Change>`)
	}
	b.WriteString(`</Changes></ChangeBatch></ChangeResourceRecordSetsRequest>`)
	return p.sendRequest(ctx, "POST", "/hostedzone/"+p.zoneID()+"/rrset", nil, b.String(), nil)
}

/* extractValues 取出集合内的原始记录值列表 */
func extractValues(set resourceRecordSet) []string {
	values := make([]string, 0, len(set.ResourceRecords))
	for _, rr := range set.ResourceRecords {
		values = append(values, rr.Value)
	}
	return values
}

/* convertType Route53 无 SPF 类型，统一落到 TXT */
func convertType(recordType string) string {
	if recordType == "SPF" {
		return "TXT"
	}
	return recordType
}

/* formatValue 将界面的记录值转换为 Route53 要求的原始格式 */
func formatValue(recordType, value string, mx int) string {
	switch recordType {
	case "TXT":
		if value == "" || value[0] != '"' {
			return `"` + value + `"`
		}
		return value
	case "MX":
		return strconv.Itoa(mx) + " " + strings.TrimRight(value, ".") + "."
	case "CNAME", "NS", "PTR":
		return strings.TrimRight(value, ".") + "."
	case "SRV":
		parts := strings.Fields(value)
		if len(parts) >= 4 {
			parts[3] = strings.TrimRight(parts[3], ".") + "."
			return strings.Join(parts, " ")
		}
		return value
	}
	return value
}

/* parseValue 将 Route53 的原始记录值还原为界面展示格式 */
func parseValue(recordType, raw string) parsedValue {
	switch recordType {
	case "TXT":
		// 仅当整体被引号包裹且未分片时才剥离引号，分片 TXT 维持原样
		if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
			inner := raw[1 : len(raw)-1]
			if !strings.Contains(inner, `" "`) {
				return parsedValue{Value: strings.ReplaceAll(inner, `\"`, `"`)}
			}
		}
		return parsedValue{Value: raw}
	case "MX":
		parts := strings.SplitN(raw, " ", 2)
		mx, _ := strconv.Atoi(parts[0])
		value := ""
		if len(parts) > 1 {
			value = strings.TrimRight(parts[1], ".")
		}
		return parsedValue{Value: value, MX: mx}
	case "CNAME", "NS", "PTR":
		return parsedValue{Value: strings.TrimRight(raw, ".")}
	case "SRV":
		parts := strings.Fields(raw)
		if len(parts) >= 4 {
			parts[3] = strings.TrimRight(parts[3], ".")
			return parsedValue{Value: strings.Join(parts, " ")}
		}
		return parsedValue{Value: raw}
	}
	return parsedValue{Value: raw}
}

/* domainAscii 域名存在 IDN 时转换为 punycode，Route53 仅接受 ASCII 域名 */
func (p *Provider) domainAscii() string {
	domain := strings.TrimRight(p.domain, ".")
	if ascii, err := idna.ToASCII(domain); err == nil && ascii != "" {
		return ascii
	}
	return domain
}

/* toFqdn 将主机记录转为带末尾点的完整域名 */
func (p *Provider) toFqdn(name string) string {
	domain := p.domainAscii()
	if name == "@" || name == "" {
		return domain + "."
	}
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "." + domain + "."
}

/* fromFqdn 将完整域名还原为主机记录，不属于本域时原样返回 */
func (p *Provider) fromFqdn(fqdn string) string {
	fqdn = strings.TrimRight(fqdn, ".")
	domainAscii := p.domainAscii()
	if strings.EqualFold(fqdn, domainAscii) || strings.EqualFold(fqdn, strings.TrimRight(p.domain, ".")) {
		return "@"
	}
	if suffix := "." + domainAscii; len(fqdn) > len(suffix) && strings.HasSuffix(fqdn, suffix) {
		return strings.TrimSuffix(fqdn, suffix)
	}
	if suffix := "." + strings.TrimRight(p.domain, "."); len(fqdn) > len(suffix) && strings.HasSuffix(fqdn, suffix) {
		return strings.TrimSuffix(fqdn, suffix)
	}
	return fqdn
}

/* encodeRecordID 将名称、类型与原始记录值打包为可逆的记录ID */
func encodeRecordID(name, recordType, value string) string {
	raw := base64.URLEncoding.EncodeToString([]byte(name + recordSep + recordType + recordSep + value))
	return strings.TrimRight(raw, "=")
}

/* decodedRecord 记录ID解码结果 */
type decodedRecord struct {
	Name  string
	Type  string
	Value string
}

func decodeRecordID(recordID string) (decodedRecord, bool) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(recordID, "="))
	if err != nil {
		return decodedRecord{}, false
	}
	parts := strings.SplitN(string(data), recordSep, 3)
	if len(parts) != 3 {
		return decodedRecord{}, false
	}
	return decodedRecord{Name: parts[0], Type: parts[1], Value: parts[2]}, true
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
	zones, err := p.listAllHostedZones(ctx)
	if err != nil {
		p.lastErr = err.Error()
		return nil, err
	}

	domains := make([]dns.DomainInfo, 0, len(zones))
	for _, z := range zones {
		name := strings.TrimRight(z.Name, ".")
		if keyword != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(keyword)) {
			continue
		}
		domains = append(domains, dns.DomainInfo{
			ID:          strings.ReplaceAll(z.ID, "/hostedzone/", ""),
			Name:        name,
			RecordCount: z.ResourceRecordSetCount,
		})
	}

	return &dns.PageResult{Total: len(domains), Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	sets, err := p.listAllRecordSets(ctx)
	if err != nil {
		p.lastErr = err.Error()
		return nil, err
	}

	var records []dns.Record
	for _, set := range sets {
		// 别名记录与加权/延迟路由记录由 Route53 托管，不参与编辑
		if set.AliasTarget != nil || set.SetIdentifier != "" || set.Type == "SOA" {
			continue
		}
		name := p.fromFqdn(set.Name)
		ttl := set.TTL
		if ttl == 0 {
			ttl = 300
		}
		for _, rr := range set.ResourceRecords {
			parsed := parseValue(set.Type, rr.Value)
			records = append(records, dns.Record{
				ID:     encodeRecordID(name, set.Type, rr.Value),
				Name:   name,
				Type:   set.Type,
				Value:  parsed.Value,
				Line:   "default",
				TTL:    ttl,
				MX:     parsed.MX,
				Status: "enable",
			})
		}
	}

	if subDomain != "" {
		records = filterRecords(records, func(r dns.Record) bool {
			return strings.EqualFold(r.Name, subDomain)
		})
	} else {
		if keyword != "" {
			lower := strings.ToLower(keyword)
			records = filterRecords(records, func(r dns.Record) bool {
				return strings.Contains(strings.ToLower(r.Name), lower) ||
					strings.Contains(strings.ToLower(r.Value), lower)
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
	if subDomain == "" {
		subDomain = "@"
	}
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	old, ok := decodeRecordID(recordID)
	if !ok {
		p.lastErr = "记录ID无效"
		return nil, fmt.Errorf("记录ID无效")
	}
	parsed := parseValue(old.Type, old.Value)
	return &dns.Record{
		ID:     recordID,
		Name:   old.Name,
		Type:   old.Type,
		Value:  parsed.Value,
		Line:   "default",
		TTL:    300,
		MX:     parsed.MX,
		Status: "enable",
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	convertedType := convertType(recordType)
	fqdn := p.toFqdn(name)
	raw := formatValue(convertedType, value, mx)

	set, err := p.getRecordSet(ctx, fqdn, convertedType)
	if err != nil {
		p.lastErr = err.Error()
		return "", err
	}

	var values []string
	if set != nil {
		values = extractValues(*set)
	}
	for _, item := range values {
		if item == raw {
			p.lastErr = "已存在相同记录"
			return "", fmt.Errorf("已存在相同记录")
		}
	}
	values = append(values, raw)

	// 已有集合沿用其 TTL，显式传入的 TTL 优先
	effectiveTTL := ttl
	if set != nil && ttl == 0 {
		effectiveTTL = set.TTL
	}

	err = p.changeRecordSets(ctx, []change{{
		Action: "UPSERT",
		Name:   fqdn,
		Type:   convertedType,
		TTL:    effectiveTTL,
		Values: values,
	}})
	if err != nil {
		p.lastErr = err.Error()
		return "", err
	}
	return encodeRecordID(p.fromFqdn(fqdn), convertedType, raw), nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	old, ok := decodeRecordID(recordID)
	if !ok {
		p.lastErr = "记录ID无效"
		return fmt.Errorf("记录ID无效")
	}

	convertedType := convertType(recordType)
	fqdn := p.toFqdn(name)
	newRaw := formatValue(convertedType, value, mx)
	oldFqdn := p.toFqdn(old.Name)

	// 名称与类型未变时原地替换记录值，避免触发全量删除重建
	if strings.EqualFold(oldFqdn, fqdn) && strings.EqualFold(old.Type, convertedType) {
		set, err := p.getRecordSet(ctx, fqdn, convertedType)
		if err != nil {
			p.lastErr = err.Error()
			return err
		}

		var values []string
		if set != nil {
			values = extractValues(*set)
		}
		replaced := false
		for i, item := range values {
			if item == old.Value {
				values[i] = newRaw
				replaced = true
				break
			}
		}
		if !replaced {
			values = append(values, newRaw)
		}
		values = uniqueStrings(values)

		err = p.changeRecordSets(ctx, []change{{
			Action: "UPSERT",
			Name:   fqdn,
			Type:   convertedType,
			TTL:    ttl,
			Values: values,
		}})
		if err != nil {
			p.lastErr = err.Error()
		}
		return err
	}

	if err := p.DeleteDomainRecord(ctx, recordID); err != nil {
		return err
	}
	_, err := p.AddDomainRecord(ctx, name, recordType, value, line, ttl, mx, weight, remark)
	return err
}

/* uniqueStrings 去重并保持原有顺序 */
func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return fmt.Errorf("AWS Route 53不支持修改备注")
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	old, ok := decodeRecordID(recordID)
	if !ok {
		p.lastErr = "记录ID无效"
		return fmt.Errorf("记录ID无效")
	}

	fqdn := p.toFqdn(old.Name)
	set, err := p.getRecordSet(ctx, fqdn, old.Type)
	if err != nil {
		p.lastErr = err.Error()
		return err
	}
	if set == nil {
		p.lastErr = "记录不存在"
		return fmt.Errorf("记录不存在")
	}

	values := extractValues(*set)
	remaining := make([]string, 0, len(values))
	for _, item := range values {
		if item != old.Value {
			remaining = append(remaining, item)
		}
	}
	if len(remaining) == len(values) {
		p.lastErr = "记录不存在"
		return fmt.Errorf("记录不存在")
	}

	// 集合内仅剩最后一条时整体删除，否则仅移除目标值
	if len(remaining) == 0 {
		err = p.changeRecordSets(ctx, []change{{
			Action: "DELETE",
			Name:   fqdn,
			Type:   old.Type,
			TTL:    set.TTL,
			Values: values,
		}})
	} else {
		err = p.changeRecordSets(ctx, []change{{
			Action: "UPSERT",
			Name:   fqdn,
			Type:   old.Type,
			TTL:    set.TTL,
			Values: remaining,
		}})
	}
	if err != nil {
		p.lastErr = err.Error()
	}
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	return fmt.Errorf("AWS Route 53不支持设置记录状态")
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("AWS Route 53不支持查看解析日志")
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
	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<CreateHostedZoneRequest xmlns="https://route53.amazonaws.com/doc/2013-04-01/">` +
		`<Name>` + escapeXML(domain) + `</Name>` +
		`<CallerReference>` + escapeXML("dnsmgr-"+domain+"-"+strconv.FormatInt(time.Now().UnixNano(), 10)) + `</CallerReference>` +
		`</CreateHostedZoneRequest>`

	var resp createHostedZoneResponse
	if err := p.sendRequest(ctx, "POST", "/hostedzone", nil, body, &resp); err != nil {
		p.lastErr = err.Error()
		return err
	}
	if resp.HostedZone.ID == "" {
		p.lastErr = "创建托管区域失败"
		return fmt.Errorf("创建托管区域失败")
	}
	return nil
}
