package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const cfBaseURL = "https://api.cloudflare.com/client/v4"

// EnhanceService Cloudflare 增强服务（自定义主机名、Tunnels、CIDR、主机名路由、DCV）
type EnhanceService struct {
	email     string
	apiKey    string
	auth      int // 0=Global API Key, 1=API Token
	proxy     bool
	accountID string
	client    *http.Client
}

// NewEnhanceService 创建增强服务
func NewEnhanceService(email, apiKey string, auth int, proxy bool, accountID string) *EnhanceService {
	return &EnhanceService{
		email:     email,
		apiKey:    strings.ReplaceAll(apiKey, " ", ""),
		auth:      auth,
		proxy:     proxy,
		accountID: accountID,
		client:    &http.Client{Timeout: 20 * time.Second},
	}
}

// DetectAuthMode 自动检测认证模式
func DetectAuthMode(apiKey string) int {
	matched, _ := regexp.MatchString(`^[0-9a-fA-F]+$`, apiKey)
	if matched {
		return 0 // Global API Key
	}
	return 1 // API Token
}

// requestRaw 原始 API 请求
func (s *EnhanceService) requestRaw(method, path string, query map[string]string, body *map[string]interface{}, allowNotFound bool) (*http.Response, []byte, error) {
	reqURL := cfBaseURL + path

	if len(query) > 0 {
		values := url.Values{}
		for k, v := range query {
			if v != "" {
				values.Set(k, v)
			}
		}
		if encoded := values.Encode(); encoded != "" {
			reqURL += "?" + encoded
		}
	}

	var reqBody io.Reader
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		reqBody = strings.NewReader(string(jsonBytes))
	}

	req, err := http.NewRequest(method, reqURL, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("创建请求失败: %v", err)
	}

	if s.auth == 0 {
		req.Header.Set("X-Auth-Email", s.email)
		req.Header.Set("X-Auth-Key", s.apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	if method == "POST" || method == "PUT" || method == "PATCH" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("请求失败: %v", err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode == 404 && allowNotFound {
		return resp, respBody, nil
	}

	if resp.StatusCode >= 400 {
		var errMsg string
		if resp.StatusCode == 401 {
			errMsg = "认证失败：凭证无效"
		} else if resp.StatusCode == 403 {
			errMsg = "权限不足"
		} else if resp.StatusCode == 429 {
			errMsg = "请求频率超限"
		} else if resp.StatusCode >= 500 {
			errMsg = "服务器不可用"
		} else {
			var result map[string]interface{}
			if json.Unmarshal(respBody, &result) == nil {
				if errors, ok := result["errors"].([]interface{}); ok && len(errors) > 0 {
					if errObj, ok := errors[0].(map[string]interface{}); ok {
						if msg, ok := errObj["message"].(string); ok {
							errMsg = msg
						}
					}
				}
			}
			if errMsg == "" {
				errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
		}
		return resp, respBody, fmt.Errorf(errMsg)
	}

	return resp, respBody, nil
}

// requestResult 解析 API 响应并返回 result 数组
func (s *EnhanceService) requestResult(method, path string, query map[string]string, body *map[string]interface{}, allowNotFound bool) ([]interface{}, error) {
	_, respBody, err := s.requestRaw(method, path, query, body, allowNotFound)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if success, ok := result["success"].(bool); ok && !success {
		if errors, ok := result["errors"].([]interface{}); ok && len(errors) > 0 {
			if errObj, ok := errors[0].(map[string]interface{}); ok {
				if msg, ok := errObj["message"].(string); ok {
					return nil, fmt.Errorf(msg)
				}
			}
		}
		return nil, fmt.Errorf("请求失败")
	}

	if data, ok := result["result"].([]interface{}); ok {
		return data, nil
	}

	return nil, nil
}

// requestResultMap 解析 API 响应并返回 result map
func (s *EnhanceService) requestResultMap(method, path string, query map[string]string, body *map[string]interface{}, allowNotFound bool) (map[string]interface{}, error) {
	_, respBody, err := s.requestRaw(method, path, query, body, allowNotFound)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if success, ok := result["success"].(bool); ok && !success {
		if errors, ok := result["errors"].([]interface{}); ok && len(errors) > 0 {
			if errObj, ok := errors[0].(map[string]interface{}); ok {
				if msg, ok := errObj["message"].(string); ok {
					return nil, fmt.Errorf(msg)
				}
			}
		}
		return nil, fmt.Errorf("请求失败")
	}

	if data, ok := result["result"].(map[string]interface{}); ok {
		return data, nil
	}

	return nil, nil
}

// paginate 自动分页获取所有数据
func (s *EnhanceService) paginate(method, path string, query map[string]string, body *map[string]interface{}, maxPages int) ([]interface{}, error) {
	if maxPages <= 0 {
		maxPages = 200
	}

	var allResults []interface{}
	page := 1

	for {
		q := make(map[string]string)
		for k, v := range query {
			q[k] = v
		}
		q["page"] = fmt.Sprintf("%d", page)
		q["per_page"] = "100"

		resp, respBody, err := s.requestRaw(method, path, q, body, false)
		if err != nil {
			return nil, err
		}
		resp.Body.Close()

		var result map[string]interface{}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("解析响应失败: %v", err)
		}

		if success, ok := result["success"].(bool); ok && !success {
			if errors, ok := result["errors"].([]interface{}); ok && len(errors) > 0 {
				if errObj, ok := errors[0].(map[string]interface{}); ok {
					if msg, ok := errObj["message"].(string); ok {
						return nil, fmt.Errorf(msg)
					}
				}
			}
			return nil, fmt.Errorf("请求失败")
		}

		if data, ok := result["result"].([]interface{}); ok {
			allResults = append(allResults, data...)
			if len(data) < 100 {
				break
			}
		}

		totalPages := 1
		if info, ok := result["result_info"].(map[string]interface{}); ok {
			if tp, ok := info["total_pages"].(float64); ok {
				totalPages = int(tp)
			}
		}

		if page >= totalPages || page >= maxPages {
			break
		}
		page++
	}

	return allResults, nil
}

// ========== 自定义主机名 API ==========

// ListCustomHostnames 列出自定义主机名
func (s *EnhanceService) ListCustomHostnames(zoneID string, page, pageSize int) ([]interface{}, int, error) {
	query := map[string]string{
		"page":     fmt.Sprintf("%d", page),
		"per_page": fmt.Sprintf("%d", pageSize),
	}

	resp, respBody, err := s.requestRaw("GET", "/zones/"+zoneID+"/custom_hostnames", query, nil, false)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, 0, fmt.Errorf("解析响应失败: %v", err)
	}

	total := 0
	if info, ok := result["result_info"].(map[string]interface{}); ok {
		if t, ok := info["total_count"].(float64); ok {
			total = int(t)
		}
	}

	if data, ok := result["result"].([]interface{}); ok {
		return data, total, nil
	}

	return nil, 0, nil
}

// GetCustomHostname 获取单个自定义主机名
func (s *EnhanceService) GetCustomHostname(zoneID, hostnameID string) (map[string]interface{}, error) {
	return s.requestResultMap("GET", "/zones/"+zoneID+"/custom_hostnames/"+hostnameID, nil, nil, false)
}

// CreateCustomHostname 创建自定义主机名
func (s *EnhanceService) CreateCustomHostname(zoneID, hostname, customOrigin, sslMethod, minTLSVersion string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"hostname": hostname,
		"ssl": map[string]interface{}{
			"method": sslMethod,
			"type":   "dv",
			"settings": map[string]interface{}{
				"min_tls_version": minTLSVersion,
			},
		},
	}

	if customOrigin != "" {
		body["custom_origin_server"] = customOrigin
	}

	return s.requestResultMap("POST", "/zones/"+zoneID+"/custom_hostnames", nil, &body, false)
}

// UpdateCustomHostname 更新自定义主机名
func (s *EnhanceService) UpdateCustomHostname(zoneID, hostnameID string, updates map[string]interface{}) (map[string]interface{}, error) {
	return s.requestResultMap("PATCH", "/zones/"+zoneID+"/custom_hostnames/"+hostnameID, nil, &updates, false)
}

// DeleteCustomHostname 删除自定义主机名
func (s *EnhanceService) DeleteCustomHostname(zoneID, hostnameID string) error {
	_, _, err := s.requestRaw("DELETE", "/zones/"+zoneID+"/custom_hostnames/"+hostnameID, nil, nil, true)
	return err
}

// ========== Fallback Origin API ==========

// GetFallbackOrigin 获取 fallback origin
func (s *EnhanceService) GetFallbackOrigin(zoneID string) (string, error) {
	_, respBody, err := s.requestRaw("GET", "/zones/"+zoneID+"/custom_hostnames/fallback_origin", nil, nil, true)
	if err != nil {
		return "", err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if origin, ok := result["result"].(map[string]interface{}); ok {
		if o, ok := origin["origin"].(string); ok {
			return o, nil
		}
	}

	return "", nil
}

// UpdateFallbackOrigin 更新 fallback origin
func (s *EnhanceService) UpdateFallbackOrigin(zoneID, origin string) error {
	body := map[string]interface{}{"origin": origin}
	_, _, err := s.requestRaw("PUT", "/zones/"+zoneID+"/custom_hostnames/fallback_origin", nil, &body, false)
	return err
}

// DeleteFallbackOrigin 删除 fallback origin
func (s *EnhanceService) DeleteFallbackOrigin(zoneID string) error {
	_, _, err := s.requestRaw("DELETE", "/zones/"+zoneID+"/custom_hostnames/fallback_origin", nil, nil, true)
	return err
}

// ========== DCV Delegation API ==========

// GetDcvDelegationUUID 获取 DCV 委派 UUID
func (s *EnhanceService) GetDcvDelegationUUID(zoneID string) (string, error) {
	data, err := s.requestResultMap("GET", "/zones/"+zoneID+"/dcv_delegation/uuid", nil, nil, false)
	if err != nil {
		return "", err
	}

	if uuid, ok := data["uuid"].(string); ok {
		return uuid, nil
	}

	return "", nil
}

// ========== Cloudflare Tunnels API ==========

// GetDefaultAccountID 获取默认账户 ID
func (s *EnhanceService) GetDefaultAccountID() (string, error) {
	// 尝试从账户列表获取
	data, err := s.requestResult("GET", "/accounts", nil, nil, false)
	if err == nil && len(data) > 0 {
		if account, ok := data[0].(map[string]interface{}); ok {
			if id, ok := account["id"].(string); ok {
				return id, nil
			}
		}
	}

	// 尝试从 Zone 列表获取
	data, err = s.requestResult("GET", "/zones", map[string]string{"per_page": "1"}, nil, false)
	if err == nil && len(data) > 0 {
		if zone, ok := data[0].(map[string]interface{}); ok {
			if account, ok := zone["account"].(map[string]interface{}); ok {
				if id, ok := account["id"].(string); ok {
					return id, nil
				}
			}
		}
	}

	return "", fmt.Errorf("无法获取账户 ID")
}

// GetAccounts 获取账户列表
func (s *EnhanceService) GetAccounts() ([]interface{}, error) {
	return s.requestResult("GET", "/accounts", nil, nil, false)
}

// GetZone 获取 Zone 详情
func (s *EnhanceService) GetZone(zoneID string) (map[string]interface{}, error) {
	return s.requestResultMap("GET", "/zones/"+zoneID, nil, nil, false)
}

// GetConfiguredAccountID 返回配置的账户 ID（不自动解析）
func (s *EnhanceService) GetConfiguredAccountID() string {
	return s.accountID
}

// IsApiTokenAuth 检查是否使用 API Token 认证
func (s *EnhanceService) IsApiTokenAuth() bool {
	return s.auth == 1
}

// ListTunnels 列出 Tunnels
func (s *EnhanceService) ListTunnels(accountID string) ([]interface{}, error) {
	query := map[string]string{"is_deleted": "false"}
	return s.paginate("GET", "/accounts/"+accountID+"/cfd_tunnel", query, nil, 0)
}

// CreateTunnel 创建 Tunnel
func (s *EnhanceService) CreateTunnel(accountID, name string) (map[string]interface{}, error) {
	// 生成随机 secret
	secret := base64.StdEncoding.EncodeToString([]byte(name))
	_ = secret // 实际上 Cloudflare 会自动处理

	body := map[string]interface{}{
		"name":    name,
		"tunnel_secret": base64.StdEncoding.EncodeToString(generateRandomBytes(32)),
	}

	return s.requestResultMap("POST", "/accounts/"+accountID+"/cfd_tunnel", nil, &body, false)
}

// DeleteTunnel 删除 Tunnel
func (s *EnhanceService) DeleteTunnel(accountID, tunnelID string) error {
	_, _, err := s.requestRaw("DELETE", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID, nil, nil, true)
	return err
}

// GetTunnelToken 获取 Tunnel Token
func (s *EnhanceService) GetTunnelToken(accountID, tunnelID string) (string, error) {
	data, err := s.requestResultMap("GET", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/token", nil, nil, false)
	if err != nil {
		return "", err
	}

	if token, ok := data["token"].(string); ok {
		return token, nil
	}

	return "", nil
}

// GetTunnelConfig 获取 Tunnel 配置
func (s *EnhanceService) GetTunnelConfig(accountID, tunnelID string) (map[string]interface{}, error) {
	return s.requestResultMap("GET", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations", nil, nil, false)
}

// UpdateTunnelConfig 更新 Tunnel 配置
func (s *EnhanceService) UpdateTunnelConfig(accountID, tunnelID string, config map[string]interface{}) error {
	body := map[string]interface{}{"config": config}
	_, _, err := s.requestRaw("PUT", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations", nil, &body, false)
	return err
}

// ========== CIDR Routes API ==========

// ListCidrRoutes 列出 CIDR 路由
func (s *EnhanceService) ListCidrRoutes(accountID, tunnelID string) ([]interface{}, error) {
	query := map[string]string{"is_deleted": "false"}
	if tunnelID != "" {
		query["tunnel_id"] = tunnelID
	}
	return s.paginate("GET", "/accounts/"+accountID+"/teamnet/routes", query, nil, 0)
}

// CreateCidrRoute 创建 CIDR 路由
func (s *EnhanceService) CreateCidrRoute(accountID, tunnelID, network, comment, virtualNetworkID string) (map[string]interface{}, error) {
	// 验证 CIDR 格式
	if _, _, err := net.ParseCIDR(network); err != nil {
		return nil, fmt.Errorf("无效的 CIDR 格式: %s", network)
	}

	body := map[string]interface{}{
		"tunnel_id": tunnelID,
		"network":   network,
		"comment":   comment,
	}

	if virtualNetworkID != "" {
		body["virtual_network_id"] = virtualNetworkID
	}

	return s.requestResultMap("POST", "/accounts/"+accountID+"/teamnet/routes", nil, &body, false)
}

// DeleteCidrRoute 删除 CIDR 路由
func (s *EnhanceService) DeleteCidrRoute(accountID, routeID string) error {
	_, _, err := s.requestRaw("DELETE", "/accounts/"+accountID+"/teamnet/routes/"+routeID, nil, nil, true)
	return err
}

// ========== Hostname Routes API ==========

// ListHostnameRoutes 列出主机名路由
func (s *EnhanceService) ListHostnameRoutes(accountID, tunnelID string) ([]interface{}, error) {
	query := map[string]string{}
	if tunnelID != "" {
		query["tunnel_id"] = tunnelID
	}
	return s.paginate("GET", "/accounts/"+accountID+"/zerotrust/routes/hostname", query, nil, 0)
}

// CreateHostnameRoute 创建主机名路由
func (s *EnhanceService) CreateHostnameRoute(accountID, tunnelID, hostname, comment string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"tunnel_id": tunnelID,
		"hostname":  hostname,
		"comment":   comment,
	}

	return s.requestResultMap("POST", "/accounts/"+accountID+"/zerotrust/routes/hostname", nil, &body, false)
}

// DeleteHostnameRoute 删除主机名路由
func (s *EnhanceService) DeleteHostnameRoute(accountID, routeID string) error {
	_, _, err := s.requestRaw("DELETE", "/accounts/"+accountID+"/zerotrust/routes/hostname/"+routeID, nil, nil, true)
	return err
}

// ========== Tunnel CNAME Record Sync API ==========

// UpsertTunnelCnameRecord 创建或更新 Tunnel CNAME DNS 记录
// 如果存在非 CNAME 记录则返回错误
// 返回: map[string]string{"action": "created"|"updated"|"unchanged"}
func (s *EnhanceService) UpsertTunnelCnameRecord(zoneID, hostname, tunnelID string) (map[string]string, error) {
	hostname = normalizeHostname(hostname)
	target := normalizeHostname(tunnelID) + ".cfargotunnel.com"

	// 获取所有该名称的 DNS 记录
	allRecords, err := s.requestResult("GET", "/zones/"+zoneID+"/dns_records", map[string]string{
		"name":    hostname,
		"page":    "1",
		"per_page": "100",
	}, nil, false)
	if err != nil {
		return nil, fmt.Errorf("同步 Tunnel CNAME 记录: %v", err)
	}

	// 检查是否存在非 CNAME 记录
	var otherTypes []string
	for _, record := range allRecords {
		rec, ok := record.(map[string]interface{})
		if !ok {
			continue
		}
		recType := ""
		if t, ok := rec["type"].(string); ok {
			recType = strings.ToUpper(t)
		}
		recName := ""
		if n, ok := rec["name"].(string); ok {
			recName = normalizeHostname(n)
		}
		if recName == hostname && recType != "CNAME" {
			otherTypes = append(otherTypes, recType)
		}
	}

	// 去重
	uniqueTypes := uniqueStrings(otherTypes)
	if len(uniqueTypes) > 0 {
		return nil, fmt.Errorf("主机名已存在非 CNAME 记录（%s），无法同步 Tunnel CNAME", strings.Join(uniqueTypes, ", "))
	}

	// 查找 CNAME 记录
	var existingCNAME map[string]interface{}
	for _, record := range allRecords {
		rec, ok := record.(map[string]interface{})
		if !ok {
			continue
		}
		recType := ""
		if t, ok := rec["type"].(string); ok {
			recType = strings.ToUpper(t)
		}
		recName := ""
		if n, ok := rec["name"].(string); ok {
			recName = normalizeHostname(n)
		}
		if recName == hostname && recType == "CNAME" {
			existingCNAME = rec
			break
		}
	}

	if existingCNAME != nil {
		// 检查是否需要更新
		currentContent := ""
		if c, ok := existingCNAME["content"].(string); ok {
			currentContent = normalizeHostname(c)
		}
		proxied := false
		if p, ok := existingCNAME["proxied"].(bool); ok {
			proxied = p
		}

		if currentContent == target && proxied {
			return map[string]string{"action": "unchanged"}, nil
		}

		// 更新记录
		recordID := ""
		if id, ok := existingCNAME["id"].(string); ok {
			recordID = id
		}
		body := map[string]interface{}{
			"type":    "CNAME",
			"name":    hostname,
			"content": target,
			"proxied": true,
			"ttl":     1,
		}
		_, err = s.requestResultMap("PUT", "/zones/"+zoneID+"/dns_records/"+recordID, nil, &body, false)
		if err != nil {
			return nil, fmt.Errorf("同步 Tunnel CNAME 记录: %v", err)
		}
		return map[string]string{"action": "updated"}, nil
	}

	// 创建新记录
	body := map[string]interface{}{
		"type":    "CNAME",
		"name":    hostname,
		"content": target,
		"proxied": true,
		"ttl":     1,
	}
	_, err = s.requestResultMap("POST", "/zones/"+zoneID+"/dns_records", nil, &body, false)
	if err != nil {
		return nil, fmt.Errorf("同步 Tunnel CNAME 记录: %v", err)
	}
	return map[string]string{"action": "created"}, nil
}

// DeleteTunnelCnameRecordIfMatch 删除匹配的 Tunnel CNAME DNS 记录
// 只有当记录存在且内容匹配时才删除
// 返回: map[string]bool{"deleted": true/false}
func (s *EnhanceService) DeleteTunnelCnameRecordIfMatch(zoneID, hostname, tunnelID string) (map[string]bool, error) {
	hostname = normalizeHostname(hostname)
	target := normalizeHostname(tunnelID) + ".cfargotunnel.com"

	records, err := s.requestResult("GET", "/zones/"+zoneID+"/dns_records", map[string]string{
		"name":    hostname,
		"type":    "CNAME",
		"page":    "1",
		"per_page": "100",
	}, nil, false)
	if err != nil {
		return nil, fmt.Errorf("删除 Tunnel CNAME 记录: %v", err)
	}

	for _, record := range records {
		rec, ok := record.(map[string]interface{})
		if !ok {
			continue
		}
		recName := ""
		if n, ok := rec["name"].(string); ok {
			recName = normalizeHostname(n)
		}
		recContent := ""
		if c, ok := rec["content"].(string); ok {
			recContent = normalizeHostname(c)
		}

		if recName == hostname && recContent == target {
			recordID := ""
			if id, ok := rec["id"].(string); ok {
				recordID = id
			}
			_, err = s.requestResultMap("DELETE", "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil, false)
			if err != nil {
				return nil, fmt.Errorf("删除 Tunnel CNAME 记录: %v", err)
			}
			return map[string]bool{"deleted": true}, nil
		}
	}

	return map[string]bool{"deleted": false}, nil
}

// ========== 工具函数 ==========

// normalizeHostname 标准化主机名（转小写，去除前后空格和点）
func normalizeHostname(hostname string) string {
	hostname = strings.TrimSpace(hostname)
	hostname = strings.ToLower(hostname)
	hostname = strings.Trim(hostname, ".")
	return hostname
}

// uniqueStrings 字符串去重
func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func generateRandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}
