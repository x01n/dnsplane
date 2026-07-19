package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AccelConfig 加速配置
type AccelConfig struct {
	// CF
	CFEnabled       bool   `json:"cf_enabled"`
	CFPreferDomain  string `json:"cf_prefer_domain"`  // CF 优选域名
	CFZoneID        string `json:"cf_zone_id"`        // CF SaaS zone ID（复用 hostname 功能）
	CFDomainID      uint   `json:"cf_domain_id"`      // 面板域名 ID（CF SaaS 配置）

	// 腾讯云 EO
	EOEnabled   bool   `json:"eo_enabled"`
	EOSecretID  string `json:"eo_secret_id"`
	EOSecretKey string `json:"eo_secret_key"`
	EOZoneID    string `json:"eo_zone_id"`    // EO 站点 ID
	EOEndpoint  string `json:"eo_endpoint"`   // cn 或 intl
	EOPlanID    string `json:"eo_plan_id"`    // EO 套餐 ID

	// 阿里云 ESA
	ESAEnabled         bool   `json:"esa_enabled"`
	ESAAccessKeyID     string `json:"esa_access_key_id"`
	ESAAccessKeySecret string `json:"esa_access_key_secret"`
	ESASiteID          string `json:"esa_site_id"`   // ESA 站点 ID
	ESARegion          string `json:"esa_region"`    // cn-hangzhou 或 ap-southeast-1

	// 策略
	Strategy string `json:"strategy"` // "cf_only", "eo_only", "esa_only", "mixed_cf_eo", "mixed_cf_esa"
}

// AccelResult 加速操作结果
type AccelResult struct {
	Platform string `json:"platform"`
	Line     string `json:"line"`
	LineName string `json:"line_name"`
	CNAME    string `json:"cname"`
	Status   string `json:"status"`
	Msg      string `json:"msg,omitempty"`
}

// EOClient 腾讯云 EdgeOne 客户端
type EOClient struct {
	secretID  string
	secretKey string
	endpoint  string
	version   string
	client    *http.Client
}

func NewEOClient(secretID, secretKey, endpointType string) *EOClient {
	endpoint := "teo.tencentcloudapi.com"
	if endpointType == "intl" {
		endpoint = "teo.intl.tencentcloudapi.com"
	}
	return &EOClient{
		secretID:  secretID,
		secretKey: secretKey,
		endpoint:  endpoint,
		version:   "2022-09-01",
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *EOClient) Request(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
	timestamp := time.Now().Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	payload, _ := json.Marshal(params)
	payloadStr := string(payload)

	httpRequestMethod := "POST"
	canonicalURI := "/"
	canonicalQueryString := ""
	canonicalHeaders := "content-type:application/json; charset=utf-8\n" +
		"host:" + c.endpoint + "\n" +
		"x-tc-action:" + strings.ToLower(action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	hashedRequestPayload := sha256Hex(payloadStr)

	canonicalRequest := httpRequestMethod + "\n" +
		canonicalURI + "\n" +
		canonicalQueryString + "\n" +
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		hashedRequestPayload

	algorithm := "TC3-HMAC-SHA256"
	credentialScope := date + "/teo/tc3_request"
	stringToSign := algorithm + "\n" +
		strconv.FormatInt(timestamp, 10) + "\n" +
		credentialScope + "\n" +
		sha256Hex(canonicalRequest)

	secretDate := hmacSHA256Sign([]byte("TC3"+c.secretKey), date)
	secretService := hmacSHA256Sign(secretDate, "teo")
	secretSigning := hmacSHA256Sign(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256Sign(secretSigning, stringToSign))

	authorization := algorithm + " Credential=" + c.secretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + signature

	req, err := http.NewRequestWithContext(ctx, "POST", "https://"+c.endpoint, strings.NewReader(payloadStr))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", c.endpoint)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", c.version)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("Authorization", authorization)

	resp, err := c.client.Do(req)
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
			msg, _ := errInfo["Message"].(string)
			code, _ := errInfo["Code"].(string)
			return nil, fmt.Errorf("[%s] %s", code, msg)
		}
		return response, nil
	}

	return nil, fmt.Errorf("解析响应失败")
}

// CreateAccelerationDomain 在 EO 上创建加速域名
func (c *EOClient) CreateAccelerationDomain(ctx context.Context, zoneID, domainName, originAddr string) (string, error) {
	originType := "IP_DOMAIN"

	params := map[string]interface{}{
		"ZoneId":     zoneID,
		"DomainName": domainName,
		"OriginInfo": map[string]interface{}{
			"OriginType": originType,
			"Origin":     originAddr,
		},
	}

	_, err := c.Request(ctx, "CreateAccelerationDomain", params)
	if err != nil {
		return "", err
	}

	// EO CNAME 格式: <domain>.eo.dnse5.com (取 dnse5 作为默认)
	cname := domainName + ".eo.dnse5.com"
	return cname, nil
}

// DescribeAccelerationDomains 查询加速域名详情，获取实际 CNAME
func (c *EOClient) DescribeAccelerationDomains(ctx context.Context, zoneID, domainName string) (string, string, error) {
	params := map[string]interface{}{
		"ZoneId": zoneID,
		"Filters": []map[string]interface{}{
			{"Name": "domain-name", "Values": []string{domainName}},
		},
		"Offset": 0,
		"Limit":  1,
	}

	result, err := c.Request(ctx, "DescribeAccelerationDomains", params)
	if err != nil {
		return "", "", err
	}

	if domains, ok := result["AccelerationDomains"].([]interface{}); ok && len(domains) > 0 {
		if d, ok := domains[0].(map[string]interface{}); ok {
			cname, _ := d["Cname"].(string)
			status, _ := d["DomainStatus"].(string)
			return cname, status, nil
		}
	}

	return "", "", fmt.Errorf("未找到加速域名 %s", domainName)
}

// EOZone 腾讯云 EO 站点信息
type EOZone struct {
	ZoneID   string `json:"zone_id"`
	ZoneName string `json:"zone_name"`
	Status   string `json:"status"`
	PlanType string `json:"plan_type"`
}

// DescribeZones 枚举所有 EO 站点
func (c *EOClient) DescribeZones(ctx context.Context) ([]EOZone, error) {
	params := map[string]interface{}{
		"Offset": 0,
		"Limit":  100,
	}

	result, err := c.Request(ctx, "DescribeZones", params)
	if err != nil {
		return nil, err
	}

	var zones []EOZone
	if zoneList, ok := result["Zones"].([]interface{}); ok {
		for _, z := range zoneList {
			if zMap, ok := z.(map[string]interface{}); ok {
				zone := EOZone{
					ZoneID:   fmt.Sprintf("%v", zMap["ZoneId"]),
					ZoneName: fmt.Sprintf("%v", zMap["ZoneName"]),
				}
				if s, ok := zMap["Status"].(string); ok {
					zone.Status = s
				}
				if pt, ok := zMap["PlanType"].(string); ok {
					zone.PlanType = pt
				} else if plan, ok := zMap["Plan"].(map[string]interface{}); ok {
					if pt2, ok := plan["PlanType"].(string); ok {
						zone.PlanType = pt2
					}
				}
				zones = append(zones, zone)
			}
		}
	}

	return zones, nil
}

// ESAClient 阿里云 ESA 客户端
type ESAClient struct {
	accessKeyID     string
	accessKeySecret string
	endpoint        string
	version         string
	client          *http.Client
}

func NewESAClient(accessKeyID, accessKeySecret, region string) *ESAClient {
	if region == "" {
		region = "cn-hangzhou"
	}
	return &ESAClient{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		endpoint:        "esa." + region + ".aliyuncs.com",
		version:         "2024-09-10",
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ESAClient) sign(params map[string]string, method string) string {
	params["Format"] = "JSON"
	params["Version"] = c.version
	params["AccessKeyId"] = c.accessKeyID
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
		query.WriteString(aliEncode(k))
		query.WriteString("=")
		query.WriteString(aliEncode(params[k]))
	}

	stringToSign := method + "&" + aliEncode("/") + "&" + aliEncode(query.String())
	mac := hmac.New(sha256.New, []byte(c.accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return query.String() + "&Signature=" + aliEncode(signature)
}

func (c *ESAClient) Request(ctx context.Context, params map[string]string, method string) (map[string]interface{}, error) {
	queryString := c.sign(params, method)
	reqURL := "https://" + c.endpoint + "/?" + queryString

	var reqBody io.Reader
	if method == "POST" {
		reqBody = strings.NewReader(queryString)
		reqURL = "https://" + c.endpoint + "/"
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.client.Do(req)
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
		msg, _ := result["Message"].(string)
		return nil, fmt.Errorf("[%s] %s", code, msg)
	}

	return result, nil
}

// CreateESARecord 在 ESA 上创建代理加速记录
func (c *ESAClient) CreateESARecord(ctx context.Context, siteID, domainName, recordType, originAddr string) (string, string, error) {
	// ESA 中 CNAME 接入的记录必须开启代理(Proxied=true)
	data := map[string]interface{}{"Value": originAddr}
	dataJSON, _ := json.Marshal(data)

	// 对于 ESA，recordName 是完整域名
	if recordType == "A" || recordType == "AAAA" {
		recordType = "A/AAAA"
	}

	params := map[string]string{
		"Action":     "CreateRecord",
		"SiteId":     siteID,
		"RecordName": domainName,
		"Type":       recordType,
		"Data":       string(dataJSON),
		"Proxied":    "true",
		"BizName":    "web",
		"Ttl":        "1",
	}

	result, err := c.Request(ctx, params, "POST")
	if err != nil {
		return "", "", err
	}

	recordID := ""
	if rid, ok := result["RecordId"].(string); ok {
		recordID = rid
	} else if rid, ok := result["RecordId"].(float64); ok {
		recordID = strconv.FormatInt(int64(rid), 10)
	}

	// ESA CNAME 格式: <domain>.cnamezone.com
	cname := domainName + ".cnamezone.com"
	if rc, ok := result["RecordCname"].(string); ok && rc != "" {
		cname = rc
	}

	return recordID, cname, nil
}

// GetESARecordCname 查询 ESA 记录获取 CNAME
func (c *ESAClient) GetESARecordCname(ctx context.Context, siteID, domainName string) (string, error) {
	params := map[string]string{
		"Action":     "ListRecords",
		"SiteId":     siteID,
		"RecordName": domainName,
		"PageNumber": "1",
		"PageSize":   "10",
	}

	result, err := c.Request(ctx, params, "GET")
	if err != nil {
		return "", err
	}

	if records, ok := result["Records"].([]interface{}); ok {
		for _, r := range records {
			if rec, ok := r.(map[string]interface{}); ok {
				if rn, _ := rec["RecordName"].(string); strings.EqualFold(rn, domainName) {
					if cname, ok := rec["RecordCname"].(string); ok && cname != "" {
						return cname, nil
					}
				}
			}
		}
	}

	return domainName + ".cnamezone.com", nil
}

// ESASite 阿里云 ESA 站点信息
type ESASite struct {
	SiteID   string `json:"site_id"`
	SiteName string `json:"site_name"`
	Status   string `json:"status"`
	PlanName string `json:"plan_name"`
}

// ListSites 枚举所有 ESA 站点
func (c *ESAClient) ListSites(ctx context.Context) ([]ESASite, error) {
	params := map[string]string{
		"Action":     "ListSites",
		"PageNumber": "1",
		"PageSize":   "100",
	}

	result, err := c.Request(ctx, params, "GET")
	if err != nil {
		return nil, err
	}

	var sites []ESASite
	if siteList, ok := result["Sites"].([]interface{}); ok {
		for _, s := range siteList {
			if sMap, ok := s.(map[string]interface{}); ok {
				site := ESASite{
					SiteName: fmt.Sprintf("%v", sMap["SiteName"]),
				}
				if id, ok := sMap["SiteId"].(float64); ok {
					site.SiteID = strconv.FormatInt(int64(id), 10)
				} else {
					site.SiteID = fmt.Sprintf("%v", sMap["SiteId"])
				}
				if st, ok := sMap["Status"].(string); ok {
					site.Status = st
				}
				if pn, ok := sMap["PlanName"].(string); ok {
					site.PlanName = pn
				}
				sites = append(sites, site)
			}
		}
	}

	return sites, nil
}

func sha256Hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func hmacSHA256Sign(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

func aliEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}
