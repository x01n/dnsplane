package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"main/internal/notify"
	"main/internal/sysconfig"
)

const (
	OptimizeCfgAPI   = "optimize_ip_api"   // 数据接口：0=wetest 1=hostmonit 2=xingpingcn
	OptimizeCfgKey   = "optimize_ip_key"   // 接口密钥，仅 hostmonit 与 wetest 使用
	OptimizeCfgProxy = "optimize_ip_proxy" // xingpingcn 专用代理前缀，用户输入
	OptimizeCfgMin   = "optimize_ip_min"   // 自动更新间隔（分钟）
	OptimizeCfgTime  = "optimize_ip_time"  // 上次执行时间，用于间隔判断
)

/* 数据接口标识，取自 opipset.html 中 select 的 option value */
const (
	OptimizeAPIWetest     = 0
	OptimizeAPIHostmonit  = 1
	OptimizeAPIXingpingcn = 2
)

/* 优选线路代码，顺序与 OptimizeService::get_ip_address2 构建结果的顺序一致，遍历时不可打乱 */
var optimizeLineKeys = []string{"DEF", "CT", "CU", "CM"}

/* wetest 默认密钥，与 opipset.html 中 optimize_ip_key 的默认值一致 */
const defaultOptimizeIPKey = "o1zrmHAF"

/* xingpingcn 仓库内数据文件路径 */
const xingpingcnDataPath = "/xingpingcn/enhanced-FaaS-in-China/refs/heads/main/Cf.json"

/* 优选接口超时 */
const optimizeHTTPTimeout = 10 * time.Second

/* 单次响应体上限，避免异常响应耗尽内存 */
const optimizeMaxBodySize = 4 << 20
const optimizeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36"

var optimizeHTTPClient = &http.Client{Timeout: optimizeHTTPTimeout}

/* GetOptimizeLicense 查询数据接口剩余可用次数 */
func GetOptimizeLicense(ctx context.Context, api int, key string) (string, error) {
	var endpoint string
	switch api {
	case OptimizeAPIXingpingcn:
		return "", fmt.Errorf("xingpingcn.top 接口免费使用，无需密钥，无积分限制")
	case OptimizeAPIHostmonit:
		endpoint = "https://api.hostmonit.com/get_license?license=" + url.QueryEscape(key)
	default:
		endpoint = "https://www.wetest.vip/api/cf2dns/get_license?license=" + url.QueryEscape(key)
	}

	body, err := optimizeHTTPGet(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("接口请求失败")
	}
	var arr map[string]interface{}
	if err := json.Unmarshal(body, &arr); err != nil {
		return "", fmt.Errorf("获取剩余请求次数失败")
	}
	code, hasCode := arr["code"]
	count, hasCount := arr["count"]
	if hasCode && optimizeJSONInt(code) == 200 && hasCount {
		return fmt.Sprintf("%v", count), nil
	}
	if info, ok := arr["info"]; ok {
		return "", fmt.Errorf("获取剩余请求次数失败，%v", info)
	}
	return "", fmt.Errorf("获取剩余请求次数失败")
}

/* GetOptimizeIPData 获取优选IP，返回「线路代码 -> IP列表」*/
func GetOptimizeIPData(ctx context.Context, cdnType int, ipType string) (map[string][]string, error) {
	api := optimizeAPIConfig()
	if api == OptimizeAPIXingpingcn {
		return getOptimizeIPDataXingpingcn(ctx, ipType)
	}

	var endpoint string
	if api == OptimizeAPIHostmonit {
		endpoint = "https://api.hostmonit.com/get_optimization_ip"
	} else {
		endpoint = "https://www.wetest.vip/api/cf2dns/" + wetestOptimizePath(cdnType)
	}

	key := strings.TrimSpace(sysconfig.GetValue(OptimizeCfgKey))
	if key == "" {
		key = defaultOptimizeIPKey
	}
	payload, err := json.Marshal(map[string]string{"key": key, "type": ipType})
	if err != nil {
		return nil, fmt.Errorf("获取优选IP数据失败，原因未知")
	}

	body, err := optimizeHTTPPostJSON(ctx, endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("接口请求失败")
	}
	var arr map[string]interface{}
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("获取优选IP数据失败，原因未知")
	}

	// code==200 时 info 是数据对象，其余情况下 info/msg 是错误描述
	code, hasCode := arr["code"]
	if hasCode && optimizeJSONInt(code) == 200 {
		return normalizeOptimizeInfo(arr["info"]), nil
	}
	if info, ok := arr["info"]; ok {
		return nil, fmt.Errorf("获取优选IP数据失败，%v", info)
	}
	if msg, ok := arr["msg"]; ok {
		return nil, fmt.Errorf("获取优选IP数据失败，%v", msg)
	}
	return nil, fmt.Errorf("获取优选IP数据失败，原因未知")
}

/*
 * getOptimizeIPDataXingpingcn 从 GitHub 原始文件取优选IP
 * 该接口只提供电信/联通/移动三网数据，无默认线路
 */
func getOptimizeIPDataXingpingcn(ctx context.Context, ipType string) (map[string][]string, error) {
	if ipType == "v6" {
		return nil, fmt.Errorf("xingpingcn.top 接口暂不支持IPv6")
	}

	endpoint := "https://raw.githubusercontent.com" + xingpingcnDataPath
	if proxy := strings.TrimSpace(sysconfig.GetValue(OptimizeCfgProxy)); proxy != "" {
		// 代理前缀由用户配置，直接参与出站 URL 拼接，拼接后必须先过 SSRF 校验
		endpoint = strings.TrimRight(proxy, "/") + xingpingcnDataPath
		if err := notify.ValidateOutboundURL(endpoint); err != nil {
			return nil, fmt.Errorf("无效的代理地址配置：%v", err)
		}
	}

	body, err := optimizeHTTPGet(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("获取优选IP数据失败，网络请求失败，请检查网络连接或代理地址")
	}
	var arr map[string]interface{}
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("获取优选IP数据失败，接口返回数据格式错误")
	}
	cf, ok := arr["Cf"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("获取优选IP数据失败，接口返回数据格式错误")
	}
	result, ok := cf["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("获取优选IP数据失败，接口返回数据格式错误")
	}

	// 源字段到线路代码的映射，default 线路刻意不取，由执行层按任务类型决定是否复用 CT
	info := make(map[string][]string, 3)
	for _, m := range []struct{ src, dst string }{
		{"dianxin", "CT"},
		{"liantong", "CU"},
		{"yidong", "CM"},
	} {
		list, ok := result[m.src].([]interface{})
		if !ok {
			continue
		}
		ips := make([]string, 0, len(list))
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				ips = append(ips, s)
			}
		}
		info[m.dst] = ips
	}
	return info, nil
}

/* normalizeOptimizeInfo 把接口 info 对象规范化为「线路代码 -> IP列表」，单个线路结构异常时跳过该线路 */
func normalizeOptimizeInfo(info interface{}) map[string][]string {
	result := make(map[string][]string, len(optimizeLineKeys))
	obj, ok := info.(map[string]interface{})
	if !ok {
		return result
	}
	for _, line := range optimizeLineKeys {
		entries, ok := obj[line].([]interface{})
		if !ok {
			continue
		}
		ips := make([]string, 0, len(entries))
		for _, e := range entries {
			item, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			if ip, ok := item["ip"].(string); ok && ip != "" {
				ips = append(ips, ip)
			}
		}
		result[line] = ips
	}
	return result
}

/* wetestOptimizePath 返回 wetest 接口的 CDN 子路径，未匹配时保持空串 */
func wetestOptimizePath(cdnType int) string {
	switch cdnType {
	case 1:
		return "get_cloudflare_ip"
	case 2:
		return "get_cloudfront_ip"
	case 3:
		return "get_gcore_ip"
	case 4:
		return "get_edgeone_ip"
	}
	return ""
}

func optimizeAPIConfig() int {
	v := strings.TrimSpace(sysconfig.GetValue(OptimizeCfgAPI))
	if v == "" {
		return OptimizeAPIWetest
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return OptimizeAPIWetest
	}
	return n
}

func optimizeJSONInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return -1
		}
		return int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return -1
		}
		return n
	}
	return -1
}

/* optimizeHTTPGet 发起 GET 请求，出站前统一做 SSRF 校验 */
func optimizeHTTPGet(ctx context.Context, rawURL string) ([]byte, error) {
	if err := notify.ValidateOutboundURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", optimizeUserAgent)
	return optimizeDo(req)
}

/* optimizeHTTPPostJSON 发起 JSON POST 请求，出站前统一做 SSRF 校验 */
func optimizeHTTPPostJSON(ctx context.Context, rawURL string, payload []byte) ([]byte, error) {
	if err := notify.ValidateOutboundURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", optimizeUserAgent)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	return optimizeDo(req)
}

func optimizeDo(req *http.Request) ([]byte, error) {
	resp, err := optimizeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, optimizeMaxBodySize))
}
