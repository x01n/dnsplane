package goedge

import (
	"bytes"
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
	dns.Register("goedge", NewProvider, dns.ProviderConfig{
		Type: "goedge",
		Name: "GoEdge智能DNS",
		Icon: "logo.png",
		Note: "需要填写GoEdge HTTP API节点地址，不是管理后台地址或gRPC地址",
		Config: []dns.ConfigField{
			{Name: "API节点地址", Key: "base_url", Type: "input", Placeholder: "例如：https://api.example.com:8004", Required: true},
			{Name: "AccessKey ID", Key: "accessKeyId", Type: "input", Required: true},
			{Name: "AccessKey", Key: "accessKey", Type: "input", Required: true},
			{Name: "AccessKey类型", Key: "type", Type: "radio", Options: []dns.ConfigOption{
				{Value: "admin", Label: "管理员"},
				{Value: "user", Label: "用户"},
			}, Value: "admin"},
			{Name: "DNS集群ID", Key: "nsClusterId", Type: "input", Placeholder: "例如：1", Required: true},
			{Name: "GoEdge用户ID（可选）", Key: "userId", Type: "input", Placeholder: "留空表示不指定用户"},
		},
		Features: dns.ProviderFeatures{
			Remark: 2, Status: true, Redirect: false, Log: false, Weight: true, Page: false, Add: true,
		},
	})
}

/*
 * Provider GoEdge智能DNS 服务商
 */
type Provider struct {
	baseURL     string
	accessKeyID string
	accessKey   string
	userType    string
	nsClusterID int
	nsUserID    int
	domain      string
	domainID    string
	client      *http.Client
	lastErr     string

	/* tokenMu 保护 accessToken 的惰性获取，避免并发请求重复换取令牌 */
	tokenMu     sync.Mutex
	accessToken string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	userType := strings.ToLower(strings.TrimSpace(config["type"]))
	if userType == "" {
		userType = "admin"
	}
	clusterID, _ := strconv.Atoi(strings.TrimSpace(config["nsClusterId"]))
	userID, _ := strconv.Atoi(strings.TrimSpace(config["userId"]))
	return &Provider{
		baseURL:     strings.TrimRight(config["base_url"], "/"),
		accessKeyID: strings.TrimSpace(config["accessKeyId"]),
		accessKey:   strings.TrimSpace(config["accessKey"]),
		userType:    userType,
		nsClusterID: clusterID,
		nsUserID:    userID,
		domain:      domain,
		domainID:    domainID,
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) setError(err error) error {
	if err != nil {
		p.lastErr = err.Error()
	}
	return err
}

/* jsonText 将 JSON 值转换为文本，数字与字符串均按字面量还原 */
func jsonText(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	}
	return ""
}

/* jsonInt 将 JSON 值转换为整数，无法识别时返回 0 */
func jsonInt(value interface{}) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

/* jsonBool 将 JSON 值转换为布尔，兼容 true / "1" / 非零数字三种表示 */
func jsonBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

/* postJSON 发送 JSON 请求并解析响应 envelopes，返回 data 字段 */
func (p *Provider) postJSON(ctx context.Context, path string, payload interface{}, headers map[string]string) (map[string]interface{}, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
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
	_ = json.Unmarshal(respBody, &result)
	if resp.StatusCode != 200 || result == nil || jsonInt(result["code"]) != 200 {
		message := jsonText(result["message"])
		if message == "" {
			message = "GoEdge API请求失败"
		}
		return nil, fmt.Errorf("%s", message)
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		return data, nil
	}
	return map[string]interface{}{}, nil
}

/* ensureToken 惰性获取并缓存 AccessToken */
func (p *Provider) ensureToken(ctx context.Context) error {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if p.accessToken != "" {
		return nil
	}

	data, err := p.postJSON(ctx, "/APIAccessTokenService/getAPIAccessToken", map[string]interface{}{
		"type":        p.userType,
		"accessKeyId": p.accessKeyID,
		"accessKey":   p.accessKey,
	}, nil)
	if err != nil {
		return fmt.Errorf("获取GoEdge AccessToken失败: %s", err.Error())
	}
	token := jsonText(data["token"])
	if token == "" {
		return fmt.Errorf("获取GoEdge AccessToken失败")
	}
	p.accessToken = token
	return nil
}

/*
 * request 调用 GoEdge HTTP API
 * payload 为空时仍发送空对象，GoEdge 的 protobuf 解码不接受 JSON 数组
 */
func (p *Provider) request(ctx context.Context, path string, payload map[string]interface{}) (map[string]interface{}, error) {
	if err := p.ensureToken(ctx); err != nil {
		return nil, p.setError(err)
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	data, err := p.postJSON(ctx, path, payload, map[string]string{"X-Edge-Access-Token": p.accessToken})
	if err != nil {
		return nil, p.setError(err)
	}
	return data, nil
}

/* routeCodes 拆分线路代码，空值回落为 default */
func routeCodes(line string) []string {
	seen := make(map[string]struct{})
	codes := make([]string, 0)
	for _, item := range strings.Split(line, ",") {
		code := strings.TrimSpace(item)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return []string{"default"}
	}
	return codes
}

/* firstRouteCode 取首个线路代码，用于记录列表的服务端筛选 */
func firstRouteCode(line string) string {
	return routeCodes(line)[0]
}

/* statusMatches 判断记录状态是否命中筛选值，兼容 enable/1 与 disable/0 两种表示 */
func statusMatches(status, want string) bool {
	if want == "" {
		return true
	}
	enabled := status != "disable"
	switch strings.ToUpper(want) {
	case "1", "ENABLE", "ON":
		return enabled
	case "0", "DISABLE", "OFF", "PAUSE":
		return !enabled
	}
	return true
}

/* withUserID 管理员模式下按需限定用户范围 */
func (p *Provider) withUserID(payload map[string]interface{}) map[string]interface{} {
	if p.userType == "admin" && p.nsUserID > 0 {
		payload["userId"] = p.nsUserID
	}
	return payload
}

/* domainQuery 构造域名查询条件 */
func (p *Provider) domainQuery(keyword string) map[string]interface{} {
	payload := map[string]interface{}{"nsClusterId": p.nsClusterID}
	if keyword != "" {
		payload["keyword"] = keyword
	}
	return p.withUserID(payload)
}

/*
 * normalizeRecord 将 GoEdge 记录转换为统一结构
 */
func (p *Provider) normalizeRecord(rec map[string]interface{}) dns.Record {
	var codeList []string
	if routes, ok := rec["nsRoutes"].([]interface{}); ok {
		for _, item := range routes {
			route, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if code := jsonText(route["code"]); code != "" {
				codeList = append(codeList, code)
			}
		}
	}
	line := "default"
	if len(codeList) > 0 {
		line = strings.Join(codeList, ",")
	}

	status := "disable"
	if jsonBool(rec["isOn"]) {
		status = "enable"
	}

	return dns.Record{
		ID:     jsonText(rec["id"]),
		Name:   jsonText(rec["name"]),
		Type:   jsonText(rec["type"]),
		Value:  jsonText(rec["value"]),
		Line:   line,
		TTL:    jsonInt(rec["ttl"]),
		MX:     jsonInt(rec["mxPriority"]),
		Status: status,
		Weight: jsonInt(rec["weight"]),
		Remark: jsonText(rec["description"]),
	}
}

func (p *Provider) Check(ctx context.Context) error {
	if p.baseURL == "" || p.accessKeyID == "" || p.accessKey == "" {
		p.lastErr = "API地址、AccessKey ID和AccessKey不能为空"
		return fmt.Errorf("API地址、AccessKey ID和AccessKey不能为空")
	}
	if p.userType != "admin" && p.userType != "user" {
		p.lastErr = "AccessKey类型必须是admin或user"
		return fmt.Errorf("AccessKey类型必须是admin或user")
	}
	if p.nsClusterID <= 0 {
		p.lastErr = "DNS集群ID必须大于0"
		return fmt.Errorf("DNS集群ID必须大于0")
	}

	data, err := p.request(ctx, "/NSClusterService/findNSCluster", map[string]interface{}{
		"nsClusterId": p.nsClusterID,
	})
	if err != nil {
		return err
	}
	if _, ok := data["nsCluster"].(map[string]interface{}); !ok {
		p.lastErr = "DNS集群不存在"
		return fmt.Errorf("DNS集群不存在")
	}
	return nil
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	countData, err := p.request(ctx, "/NSDomainService/countAllNSDomains", p.domainQuery(keyword))
	if err != nil {
		return nil, err
	}

	listPayload := p.domainQuery(keyword)
	listPayload["offset"] = maxInt(0, (page-1)*pageSize)
	listPayload["size"] = maxInt(1, pageSize)
	listData, err := p.request(ctx, "/NSDomainService/listNSDomains", listPayload)
	if err != nil {
		return nil, err
	}

	domains := make([]dns.DomainInfo, 0)
	if rows, ok := listData["nsDomains"].([]interface{}); ok {
		for _, item := range rows {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name := jsonText(row["name"])
			if name == "" {
				continue
			}
			domains = append(domains, dns.DomainInfo{
				ID:   jsonText(row["id"]),
				Name: name,
			})
		}
	}

	total := jsonInt(countData["count"])
	if total == 0 {
		total = len(domains)
	}
	return &dns.PageResult{Total: total, Records: domains}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	if p.domainID == "" {
		p.lastErr = "GoEdge域名ID不能为空"
		return nil, fmt.Errorf("GoEdge域名ID不能为空")
	}

	// 子域名/记录值/状态无法在服务端筛选，先全量拉取再本地过滤与分页
	hasLocalFilter := subDomain != "" || value != "" || status != ""
	payload := map[string]interface{}{
		"nsDomainId": jsonInt(p.domainID),
	}
	if hasLocalFilter {
		payload["offset"] = 0
		payload["size"] = 1000
	} else {
		payload["offset"] = maxInt(0, (page-1)*pageSize)
		payload["size"] = maxInt(1, pageSize)
	}
	if keyword != "" {
		payload["keyword"] = keyword
	}
	if recordType != "" {
		payload["type"] = recordType
	}
	if line != "" {
		payload["nsRouteCode"] = firstRouteCode(line)
	}

	data, err := p.request(ctx, "/NSRecordService/listNSRecords", payload)
	if err != nil {
		return nil, err
	}

	records := make([]dns.Record, 0)
	if rows, ok := data["nsRecords"].([]interface{}); ok {
		for _, item := range rows {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			normalized := p.normalizeRecord(row)
			if subDomain != "" && !strings.EqualFold(normalized.Name, subDomain) {
				continue
			}
			if value != "" && !strings.Contains(strings.ToLower(normalized.Value), strings.ToLower(value)) {
				continue
			}
			if !statusMatches(normalized.Status, status) {
				continue
			}
			records = append(records, normalized)
		}
	}

	total := len(records)
	if hasLocalFilter {
		start := maxInt(0, (page-1)*pageSize)
		if start >= len(records) {
			records = records[:0]
		} else {
			end := start + maxInt(1, pageSize)
			if end > len(records) {
				end = len(records)
			}
			records = records[start:end]
		}
	}
	return &dns.PageResult{Total: total, Records: records}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	if subDomain == "" {
		subDomain = "@"
	}
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	recordIDInt, err := strconv.Atoi(strings.TrimSpace(recordID))
	if err != nil {
		p.lastErr = "GoEdge记录ID无效"
		return nil, fmt.Errorf("GoEdge记录ID无效")
	}
	data, err := p.request(ctx, "/NSRecordService/findNSRecord", map[string]interface{}{"nsRecordId": recordIDInt})
	if err != nil {
		return nil, err
	}
	row, ok := data["nsRecord"].(map[string]interface{})
	if !ok {
		p.lastErr = "记录不存在"
		return nil, fmt.Errorf("记录不存在")
	}
	normalized := p.normalizeRecord(row)
	return &normalized, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	weightValue := 0
	if weight != nil {
		weightValue = *weight
	}
	mxPriority := 0
	if recordType == "MX" {
		mxPriority = maxInt(0, mx)
	}
	payload := map[string]interface{}{
		"name":         name,
		"type":         recordType,
		"value":        value,
		"ttl":          maxInt(1, ttl),
		"nsRouteCodes": routeCodes(line),
		"weight":       weightValue,
		"mxPriority":   mxPriority,
		"description":  remark,
	}
	payload["nsDomainId"] = jsonInt(p.domainID)

	data, err := p.request(ctx, "/NSRecordService/createNSRecord", payload)
	if err != nil {
		return "", err
	}
	return jsonText(data["nsRecordId"]), nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	current, err := p.GetDomainRecordInfo(ctx, recordID)
	if err != nil {
		return err
	}

	weightValue := current.Weight
	if weight != nil {
		weightValue = *weight
	}
	mxPriority := 0
	if recordType == "MX" {
		mxPriority = maxInt(0, mx)
	}
	recordIDInt, err := strconv.Atoi(strings.TrimSpace(recordID))
	if err != nil {
		p.lastErr = "GoEdge记录ID无效"
		return fmt.Errorf("GoEdge记录ID无效")
	}
	payload := map[string]interface{}{
		"nsRecordId":   recordIDInt,
		"isOn":         current.Status != "disable",
		"name":         name,
		"type":         recordType,
		"value":        value,
		"ttl":          maxInt(1, ttl),
		"nsRouteCodes": routeCodes(line),
		"weight":       weightValue,
		"mxPriority":   mxPriority,
		"description":  remark,
	}
	payload["nsDomainId"] = jsonInt(p.domainID)

	_, err = p.request(ctx, "/NSRecordService/updateNSRecord", payload)
	return err
}

/* updateRecordFromNormalized 以现有记录为基准提交局部变更 */
func (p *Provider) updateRecordFromNormalized(ctx context.Context, current *dns.Record, changes map[string]interface{}) error {
	recordIDInt, err := strconv.Atoi(strings.TrimSpace(current.ID))
	if err != nil {
		p.lastErr = "GoEdge记录ID无效"
		return fmt.Errorf("GoEdge记录ID无效")
	}
	mxPriority := 0
	if strings.EqualFold(current.Type, "MX") {
		mxPriority = current.MX
	}
	payload := map[string]interface{}{
		"nsRecordId":   recordIDInt,
		"isOn":         current.Status != "disable",
		"name":         current.Name,
		"type":         current.Type,
		"value":        current.Value,
		"ttl":          maxInt(1, current.TTL),
		"nsRouteCodes": routeCodes(current.Line),
		"weight":       current.Weight,
		"mxPriority":   mxPriority,
		"description":  current.Remark,
	}
	for k, v := range changes {
		payload[k] = v
	}
	_, err = p.request(ctx, "/NSRecordService/updateNSRecord", payload)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	current, err := p.GetDomainRecordInfo(ctx, recordID)
	if err != nil {
		return err
	}
	return p.updateRecordFromNormalized(ctx, current, map[string]interface{}{"description": remark})
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	recordIDInt, err := strconv.Atoi(strings.TrimSpace(recordID))
	if err != nil {
		p.lastErr = "GoEdge记录ID无效"
		return fmt.Errorf("GoEdge记录ID无效")
	}
	_, err = p.request(ctx, "/NSRecordService/deleteNSRecord", map[string]interface{}{"nsRecordId": recordIDInt})
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	current, err := p.GetDomainRecordInfo(ctx, recordID)
	if err != nil {
		return err
	}
	return p.updateRecordFromNormalized(ctx, current, map[string]interface{}{"isOn": enable})
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("GoEdge智能DNS不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	lines := make(map[string]string)
	order := make([]string, 0)

	appendRoutes := func(data map[string]interface{}) {
		routes, ok := data["nsRoutes"].([]interface{})
		if !ok {
			return
		}
		for _, item := range routes {
			route, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			code := jsonText(route["code"])
			if code == "" {
				continue
			}
			if _, exists := lines[code]; exists {
				continue
			}
			name := jsonText(route["name"])
			if name == "" {
				name = code
			}
			lines[code] = name
			order = append(order, code)
		}
	}

	methods := []string{
		"/NSRouteService/findAllDefaultWorldRegionRoutes",
		"/NSRouteService/findAllDefaultChinaProvinceRoutes",
		"/NSRouteService/findAllDefaultISPRoutes",
		"/NSRouteService/findAllAgentNSRoutes",
	}
	for _, method := range methods {
		data, err := p.request(ctx, method, nil)
		if err != nil {
			return nil, err
		}
		appendRoutes(data)
	}

	// 带条件查询时 v1.3.x 会返回空结果，因此按空对象请求以取得域名可用线路的名称
	if p.domainID != "" {
		data, err := p.request(ctx, "/NSRouteService/findAllNSRoutes", nil)
		if err != nil {
			return nil, err
		}
		appendRoutes(data)
	}

	// 默认线路在 GoEdge 中隐式存在，始终置于首位
	result := make([]dns.RecordLine, 0, len(order)+1)
	defaultName := lines["default"]
	if defaultName == "" {
		defaultName = "默认线路"
	}
	result = append(result, dns.RecordLine{ID: "default", Name: defaultName})
	for _, code := range order {
		if code == "default" {
			continue
		}
		result = append(result, dns.RecordLine{ID: code, Name: lines[code]})
	}
	return result, nil
}

func (p *Provider) GetMinTTL() int {
	return 1
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	payload := p.withUserID(map[string]interface{}{
		"nsClusterId": p.nsClusterID,
		"name":        domain,
	})
	data, err := p.request(ctx, "/NSDomainService/createNSDomain", payload)
	if err != nil {
		return err
	}
	if jsonText(data["nsDomainId"]) == "" {
		p.lastErr = "创建域名失败"
		return fmt.Errorf("创建域名失败")
	}
	return nil
}
