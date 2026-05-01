package qingcloud

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/dns"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	dns.Register("qingcloud", NewProvider, dns.ProviderConfig{
		Type: "qingcloud",
		Name: "青云DNS",
		Icon: "qingcloud.png",
		Config: []dns.ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "secret_access_key", Type: "input", Required: true},
			{Name: "使用代理服务器", Key: "proxy", Type: "radio", Options: []dns.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		Features: dns.ProviderFeatures{Remark: 1, Status: true, Redirect: false, Log: false, Weight: true, Page: false, Add: true},
	})
}

const baseURL = "http://api.routewize.com"

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
	return &Provider{accessKeyID: config["access_key_id"], secretAccessKey: config["secret_access_key"], domain: domain, domainID: domainID, proxy: config["proxy"] == "1", client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *Provider) GetError() string { return p.lastErr }

func (p *Provider) request(ctx context.Context, method, path string, params map[string]string, body interface{}) (map[string]interface{}, int, error) {
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	stringToSign := method + "\n" + date + "\n" + path
	if method == "GET" && len(params) > 0 {
		keys := make([]string, 0, len(params))
		for k := range params { keys = append(keys, k) }
		sort.Strings(keys)
		q := url.Values{}
		for _, k := range keys { q.Set(k, params[k]) }
		stringToSign += "?" + q.Encode()
	}
	mac := hmac.New(sha256.New, []byte(p.secretAccessKey))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	authorization := "QC-HMAC-SHA256 " + p.accessKeyID + ":" + signature
	reqURL := baseURL + path
	var reqBody io.Reader
	if method == "GET" {
		if len(params) > 0 {
			q := url.Values{}
			for k, v := range params { q.Set(k, v) }
			reqURL += "?" + q.Encode()
		}
	} else if body != nil {
		b, _ := json.Marshal(body)
		reqBody = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil { return nil, 0, err }
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Date", date)
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := p.client.Do(req)
	if err != nil { return nil, 0, fmt.Errorf("请求失败: %w", err) }
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil { return nil, resp.StatusCode, err }
	if resp.StatusCode == 204 { return map[string]interface{}{}, resp.StatusCode, nil }
	var result map[string]interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil { return nil, resp.StatusCode, fmt.Errorf("解析响应失败: %w", err) }
	}
	if resp.StatusCode >= 400 {
		if msg, ok := result["message"].(string); ok && msg != "" { p.lastErr = msg } else if msg, ok := result["msg"].(string); ok && msg != "" { p.lastErr = msg } else { p.lastErr = fmt.Sprintf("HTTP %d", resp.StatusCode) }
		return nil, resp.StatusCode, fmt.Errorf("%s", p.lastErr)
	}
	if code, ok := result["code"].(float64); ok && int(code) != 0 {
		if msg, ok := result["message"].(string); ok && msg != "" { p.lastErr = msg } else if msg, ok := result["msg"].(string); ok && msg != "" { p.lastErr = msg } else { p.lastErr = "请求失败" }
		return nil, resp.StatusCode, fmt.Errorf("%s", p.lastErr)
	}
	return result, resp.StatusCode, nil
}

func (p *Provider) Check(ctx context.Context) error { _, err := p.GetDomainList(ctx, "", 1, 1); return err }

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	params := map[string]string{"offset": strconv.Itoa((page-1)*pageSize), "limit": strconv.Itoa(pageSize)}
	if keyword != "" { params["zone_name"] = keyword }
	result, _, err := p.request(ctx, "GET", "/v1/user/zones", params, nil)
	if err != nil { return nil, err }
	zones, _ := result["zones"].([]interface{})
	items := make([]dns.DomainInfo, 0, len(zones))
	for _, z := range zones {
		row, _ := z.(map[string]interface{})
		name := strings.TrimSuffix(fmt.Sprintf("%v", row["zone_name"]), ".")
		items = append(items, dns.DomainInfo{ID: name, Name: name})
	}
	total := 0
	if v, ok := result["total_count"].(float64); ok { total = int(v) }
	return &dns.PageResult{Total: total, Records: items}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	if subDomain != "" { return p.GetSubDomainRecords(ctx, subDomain, page, pageSize, recordType, line) }
	params := map[string]string{"zone_name": p.domainID, "offset": strconv.Itoa((page-1)*pageSize), "limit": strconv.Itoa(pageSize)}
	if keyword != "" { params["search_word"] = keyword }
	result, _, err := p.request(ctx, "GET", "/v1/dns/host/", params, nil)
	if err != nil { return nil, err }
	domains, _ := result["domains"].([]interface{})
	items := make([]dns.Record, 0, len(domains))
	for _, item := range domains {
		row, _ := item.(map[string]interface{})
		zoneName := fmt.Sprintf("%v", row["zone_name"])
		domainName := fmt.Sprintf("%v", row["domain_name"])
		name := strings.TrimSuffix(domainName, "."+strings.TrimSuffix(zoneName, "."))
		name = strings.TrimSuffix(name, ".")
		if name == "" { name = "@" }
		statusVal := "disable"
		if fmt.Sprintf("%v", row["status"]) == "enabled" { statusVal = "enable" }
		items = append(items, dns.Record{ID: domainName, Name: name, Status: statusVal, Remark: fmt.Sprintf("%v", row["description"]), Updated: fmt.Sprintf("%v", row["create_time"])})
	}
	total := 0
	if v, ok := result["total_count"].(float64); ok { total = int(v) }
	return &dns.PageResult{Total: total, Records: items}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	host := p.host(subDomain)
	params := map[string]string{"zone_name": p.domainID, "domain_name": host}
	result, _, err := p.request(ctx, "GET", "/v1/dns/host_info/", params, nil)
	if err != nil { return nil, err }
	recordsRaw, _ := result["records"].([]interface{})
	items := make([]dns.Record, 0)
	for _, item := range recordsRaw {
		record, _ := item.(map[string]interface{})
		rdType := fmt.Sprintf("%v", record["rd_type"])
		if recordType != "" && rdType != recordType { continue }
		name := strings.TrimSuffix(fmt.Sprintf("%v", record["domain_name"]), "."+strings.TrimSuffix(fmt.Sprintf("%v", record["zone_name"]), "."))
		name = strings.TrimSuffix(name, ".")
		if name == "" { name = "@" }
		viewID := fmt.Sprintf("%v", record["view_id"])
		if line != "" && line != viewID { continue }
		ttl := 0
		if v, ok := record["ttl"].(float64); ok { ttl = int(v) }
		recordID := fmt.Sprintf("%v", record["domain_record_id"])
		groups, _ := record["record"].([]interface{})
		for _, g := range groups {
			group, _ := g.(map[string]interface{})
			weight := 0
			if v, ok := group["weight"].(float64); ok { weight = int(v) }
			dataList, _ := group["data"].([]interface{})
			for _, d := range dataList {
				row, _ := d.(map[string]interface{})
				value := fmt.Sprintf("%v", row["value"])
				mx := 0
				if rdType == "MX" {
					parts := strings.SplitN(value, " ", 2)
					if len(parts) == 2 { mx, _ = strconv.Atoi(parts[0]); value = parts[1] }
				}
				if rdType == "TXT" { value = strings.Trim(value, "\"") }
				statusVal := "disable"
				if v, ok := row["status"].(float64); ok && int(v) == 1 { statusVal = "enable" }
				items = append(items, dns.Record{ID: recordID + "_" + fmt.Sprintf("%v", row["record_value_id"]), Name: name, Type: rdType, Value: value, Line: viewID, TTL: ttl, MX: mx, Weight: weight, Status: statusVal, Updated: fmt.Sprintf("%v", record["create_time"])})
			}
		}
	}
	return &dns.PageResult{Total: len(items), Records: items}, nil
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	parts := strings.Split(recordID, "_")
	if len(parts) != 2 { return nil, fmt.Errorf("无效记录ID") }
	result, err := p.GetSubDomainRecords(ctx, "@", 1, 500, "", "")
	if err == nil {
		if list, ok := result.Records.([]dns.Record); ok {
			for _, r := range list { if r.ID == recordID { rr := r; return &rr, nil } }
		}
	}
	params := map[string]string{"zone_name": p.domainID, "domain_name": parts[0]}
	res, _, err := p.request(ctx, "GET", "/v1/dns/host_info/", params, nil)
	if err != nil { return nil, err }
	recordsRaw, _ := res["records"].([]interface{})
	for _, item := range recordsRaw {
		record, _ := item.(map[string]interface{})
		rdType := fmt.Sprintf("%v", record["rd_type"])
		name := strings.TrimSuffix(fmt.Sprintf("%v", record["domain_name"]), "."+strings.TrimSuffix(fmt.Sprintf("%v", record["zone_name"]), "."))
		name = strings.TrimSuffix(name, ".")
		if name == "" { name = "@" }
		viewID := fmt.Sprintf("%v", record["view_id"])
		ttl := 0
		if v, ok := record["ttl"].(float64); ok { ttl = int(v) }
		groups, _ := record["record"].([]interface{})
		for _, g := range groups {
			group, _ := g.(map[string]interface{})
			weight := 0
			if v, ok := group["weight"].(float64); ok { weight = int(v) }
			dataList, _ := group["data"].([]interface{})
			for _, d := range dataList {
				row, _ := d.(map[string]interface{})
				if fmt.Sprintf("%v", row["record_value_id"]) != parts[1] { continue }
				value := fmt.Sprintf("%v", row["value"])
				mx := 0
				if rdType == "MX" {
					ps := strings.SplitN(value, " ", 2)
					if len(ps) == 2 { mx, _ = strconv.Atoi(ps[0]); value = ps[1] }
				}
				if rdType == "TXT" { value = strings.Trim(value, "\"") }
				statusVal := "disable"
				if v, ok := row["status"].(float64); ok && int(v) == 1 { statusVal = "enable" }
				return &dns.Record{ID: recordID, Name: name, Type: rdType, Value: value, Line: viewID, TTL: ttl, MX: mx, Weight: weight, Status: statusVal}, nil
			}
		}
	}
	return nil, fmt.Errorf("记录不存在")
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	mode := 1
	if (recordType == "A" || recordType == "CNAME") && weight != nil && *weight > 0 { mode = 3 }
	if recordType == "MX" { value = strconv.Itoa(mx) + " " + value } else if recordType == "TXT" && !strings.HasPrefix(value, "\"") { value = "\"" + value + "\"" }
	values := make([]map[string]interface{}, 0)
	for _, val := range strings.Split(value, ",") { values = append(values, map[string]interface{}{"value": strings.TrimSpace(val), "status": 1}) }
	w := 0
	if (recordType == "A" || recordType == "CNAME") && mode == 3 && weight != nil { w = *weight }
	record := []map[string]interface{}{{"weight": w, "values": values}}
	if line == "" || line == "default" { line = "0" }
	body := map[string]interface{}{"zone_name": p.domainID, "domain_name": name, "view_id": atoi(line), "type": recordType, "ttl": ttl, "record": mustJSON(record), "mode": mode, "auto_merge": 2}
	result, _, err := p.request(ctx, "POST", "/v1/record/", nil, body)
	if err != nil { return "", err }
	id := fmt.Sprintf("%v", result["domain_record_id"])
	if remark != "" { _ = p.UpdateDomainRecordRemark(ctx, p.host(name), remark) }
	return id, nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	parts := strings.Split(recordID, "_")
	if len(parts) != 2 { return fmt.Errorf("无效记录ID") }
	if recordType == "MX" { value = strconv.Itoa(mx) + " " + value } else if recordType == "TXT" && !strings.HasPrefix(value, "\"") { value = "\"" + value + "\"" }
	origin, _, err := p.request(ctx, "GET", "/v1/dr_id/"+parts[0], nil, nil)
	if err != nil { return err }
	data := origin["data"].(map[string]interface{})
	groups, _ := data["record"].([]interface{})
	mode := 1
	if (recordType == "A" || recordType == "CNAME") && weight != nil && *weight > 0 { mode = 3 }
	record := make([]map[string]interface{}, 0)
	for _, g := range groups {
		group, _ := g.(map[string]interface{})
		values := make([]map[string]interface{}, 0)
		flag := false
		dataList, _ := group["data"].([]interface{})
		for _, d := range dataList {
			row, _ := d.(map[string]interface{})
			rowValue := fmt.Sprintf("%v", row["value"])
			if fmt.Sprintf("%v", row["record_value_id"]) == parts[1] { rowValue = value; flag = true }
			values = append(values, map[string]interface{}{"value": rowValue, "status": row["status"]})
		}
		if len(values) > 0 {
			gw := 0
			if flag && weight != nil && *weight > 0 { gw = *weight } else if v, ok := group["weight"].(float64); ok { gw = int(v) }
			record = append(record, map[string]interface{}{"weight": gw, "values": values})
		}
	}
	if line == "" || line == "default" { line = "0" }
	body := map[string]interface{}{"zone_name": p.domainID, "domain_name": name, "view_id": atoi(line), "type": recordType, "ttl": ttl, "record": mustJSON(record), "mode": mode}
	_, _, err = p.request(ctx, "POST", "/v1/dr_id/"+parts[0], nil, body)
	if err == nil && remark != "" { _ = p.UpdateDomainRecordRemark(ctx, p.host(name), remark) }
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	body := map[string]interface{}{"zone_name": p.domainID, "domain_name": recordID, "description": remark}
	_, _, err := p.request(ctx, "POST", "/v1/dns/host/", nil, body)
	return err
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	if strings.Contains(recordID, p.domainID) {
		body := map[string]interface{}{"domain_names": mustJSON([]string{recordID}), "zone_name": p.domainID}
		_, _, err := p.request(ctx, "DELETE", "/v1/domain/", nil, body)
		return err
	}
	parts := strings.Split(recordID, "_")
	if len(parts) != 2 { return fmt.Errorf("无效记录ID") }
	origin, _, err := p.request(ctx, "GET", "/v1/dr_id/"+parts[0], nil, nil)
	if err != nil { return err }
	data := origin["data"].(map[string]interface{})
	groups, _ := data["record"].([]interface{})
	record := make([]map[string]interface{}, 0)
	for _, g := range groups {
		group, _ := g.(map[string]interface{})
		values := make([]map[string]interface{}, 0)
		dataList, _ := group["data"].([]interface{})
		for _, d := range dataList {
			row, _ := d.(map[string]interface{})
			if fmt.Sprintf("%v", row["record_value_id"]) == parts[1] { continue }
			values = append(values, map[string]interface{}{"value": row["value"], "status": row["status"]})
		}
		if len(values) > 0 {
			gw := 0
			if v, ok := group["weight"].(float64); ok { gw = int(v) }
			record = append(record, map[string]interface{}{"weight": gw, "values": values})
		}
	}
	if len(record) == 0 {
		body := map[string]interface{}{"ids": mustJSON([]string{parts[0]}), "target": "record", "action": "delete"}
		_, _, err := p.request(ctx, "POST", "/v1/change_record_status/", nil, body)
		return err
	}
	name := strings.TrimSuffix(fmt.Sprintf("%v", data["domain_name"]), "."+strings.TrimSuffix(fmt.Sprintf("%v", data["zone_name"]), "."))
	name = strings.TrimSuffix(name, ".")
	if name == "" { name = "@" }
	viewID := atoi(fmt.Sprintf("%v", data["view_id"]))
	ttl := atoi(fmt.Sprintf("%v", data["ttl"]))
	body := map[string]interface{}{"zone_name": p.domainID, "domain_name": name, "view_id": viewID, "type": fmt.Sprintf("%v", data["rd_type"]), "ttl": ttl, "record": mustJSON(record), "mode": atoi(fmt.Sprintf("%v", data["mode"]))}
	_, _, err = p.request(ctx, "POST", "/v1/dr_id/"+parts[0], nil, body)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	parts := strings.Split(recordID, "_")
	if len(parts) != 2 { return fmt.Errorf("无效记录ID") }
	action := "stop"
	if enable { action = "enable" }
	body := map[string]interface{}{"ids": mustJSON([]string{parts[1]}), "target": "value", "action": action}
	_, _, err := p.request(ctx, "POST", "/v1/change_record_status/", nil, body)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) { return nil, fmt.Errorf("青云DNS不支持记录日志") }

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	params := map[string]string{"zone_name": p.domainID, "type": "GET_FULL"}
	result, _, err := p.request(ctx, "GET", "/v1/zone/view/", params, nil)
	if err != nil { return nil, err }
	views, _ := result["zone_views"].([]interface{})
	lines := make([]dns.RecordLine, 0, len(views))
	for _, item := range views {
		row, _ := item.(map[string]interface{})
		name := fmt.Sprintf("%v", row["name"])
		if name == "*" { name = "默认" }
		lines = append(lines, dns.RecordLine{ID: fmt.Sprintf("%v", row["id"]), Name: name})
	}
	return lines, nil
}

func (p *Provider) GetMinTTL() int { return 60 }

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	body := map[string]interface{}{"zone_name": domain}
	_, _, err := p.request(ctx, "POST", "/v1/zone/", nil, body)
	return err
}

func (p *Provider) host(name string) string {
	if name == "@" || name == "" { return p.domain + "." }
	return name + "." + p.domain + "."
}

func atoi(s string) int { v, _ := strconv.Atoi(strings.TrimSpace(s)); return v }
func mustJSON(v interface{}) string { b, _ := json.Marshal(v); return string(b) }
