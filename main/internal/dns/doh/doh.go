package doh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var dohServers = []string{
	"https://dns.alidns.com/resolve",
	"https://doh.pub/resolve",
	"https://doh.360.cn/resolve",
}

var dohTypeNumbers = map[string]int{
	"A":     1,
	"AAAA":  28,
	"CNAME": 5,
	"MX":    15,
	"TXT":   16,
	"SOA":   6,
	"NS":    2,
	"PTR":   12,
	"SRV":   33,
	"CAA":   257,
}

const (
	typeCNAME = 5
	typeTXT   = 16
)

const Timeout = 3 * time.Second

/* dohUserAgent 部分 DoH 服务端会校验该头部 */
const dohUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36"

/* maxRespBytes 响应体读取上限，防止异常响应耗尽内存 */
const maxRespBytes = 1 << 20

var errMalformed = errors.New("doh: malformed response")

/* httpClient 全局复用，保留连接池；单次请求超时由此兜底 */
var httpClient = &http.Client{Timeout: Timeout}

/* dohAnswer DoH JSON 响应中的单条 Answer */
type dohAnswer struct {
	Type int    `json:"type"`
	Data string `json:"data"`
}

/* dohResponse DoH JSON 响应体，只保留 Answer 字段 */
type dohResponse struct {
	Answer []dohAnswer `json:"Answer"`
}

/*
Result 一次 DoH 查询的结果。

Server 是实际响应的 DoH 服务器主机名（发生故障切换时为切换后的那家），
供调用方如实标注结果来源，避免把随机选中的服务器写成固定值。
*/
type Result struct {
	Server  string
	Records []string
}

func QueryContext(ctx context.Context, domain, recordType string) (Result, error) {
	typeNum, ok := dohTypeNumbers[strings.ToUpper(strings.TrimSpace(recordType))]
	if !ok {
		return Result{}, fmt.Errorf("doh: unsupported record type %q", recordType)
	}

	first := rand.IntN(len(dohServers))
	records, err := queryServer(ctx, dohServers[first], domain, typeNum)
	if err == nil {
		return Result{Server: hostOf(dohServers[first]), Records: records}, nil
	}
	if !errors.Is(err, errMalformed) {
		return Result{}, err
	}

	// 解析失败：剔除该服务器，从剩余池中随机重试一次
	rest := make([]string, 0, len(dohServers)-1)
	for i, server := range dohServers {
		if i != first {
			rest = append(rest, server)
		}
	}
	if len(rest) == 0 {
		return Result{}, err
	}

	next := rest[rand.IntN(len(rest))]
	records, err = queryServer(ctx, next, domain, typeNum)
	if err != nil {
		return Result{}, err
	}
	return Result{Server: hostOf(next), Records: records}, nil
}

/* hostOf 从服务器 URL 提取主机名，用于结果来源标注 */
func hostOf(serverURL string) string {
	if u, err := url.Parse(serverURL); err == nil && u.Host != "" {
		return u.Host
	}
	return serverURL
}

func queryServer(ctx context.Context, server, domain string, typeNum int) ([]string, error) {
	endpoint := server + "?name=" + url.QueryEscape(domain) + "&type=" + strconv.Itoa(typeNum)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", dohUserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("doh: empty response from %s", server)
	}

	var parsed dohResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: %v", errMalformed, err)
	}

	/*
	 * 合法 JSON 但无 Answer 键时返回空结果且不重试 
	 * 注：doh.pub 对 CAA(257)/PTR(12) 返回 {"code":"UrlParameterError"}，属合法 JSON，
	 * 故这些类型在该服务器上会得到空结果而不触发换服务器重试。
	 */
	records := make([]string, 0, len(parsed.Answer))
	for _, row := range parsed.Answer {
		value := row.Data
		if row.Type == typeCNAME {
			value = strings.Trim(value, ".")
		}
		if row.Type == typeTXT {
			value = strings.Trim(value, "\"")
		}
		records = append(records, value)
	}
	return records, nil
}
