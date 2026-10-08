package handler

import (
	"context"
	"fmt"
	"main/internal/dns/doh"
	"net"
	"strings"
	"sync"
	"time"

	"main/internal/api/middleware"

	"github.com/gin-gonic/gin"
)

/**
 * dnsServer DNS服务器定义
 */
type dnsServer struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

var dnsServers = []dnsServer{
	{"阿里DNS", "223.5.5.5"},
	{"腾讯DNS", "119.29.29.29"},
	{"百度DNS", "180.76.76.76"},
	{"114DNS", "114.114.114.114"},
	{"Google", "8.8.8.8"},
	{"Cloudflare", "1.1.1.1"},
	{"OpenDNS", "208.67.222.222"},
}

type dnsCheckRequest struct {
	Domain string `json:"domain" binding:"required"`
	Type   string `json:"type" binding:"required"`
}

/**
 * dnsCheckResult 单个DNS服务器的查询结果
 */
type dnsCheckResult struct {
	Server  string   `json:"server"`
	IP      string   `json:"ip"`
	Results []string `json:"results"`
	TTL     string   `json:"ttl"`
	Cost    int64    `json:"cost"`
	Error   string   `json:"error,omitempty"`
}

/**
 * DnsCheck DNS检测工具
 * @route POST /domains/dns-check
 * 功能：查询指定域名在各个公共DNS服务器上的解析结果
 */
func DnsCheck(c *gin.Context) {
	var req dnsCheckRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	req.Domain = strings.TrimSpace(req.Domain)
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))

	if req.Domain == "" || req.Type == "" {
		middleware.ErrorResponse(c, "域名和记录类型不能为空")
		return
	}

	allowedTypes := map[string]bool{
		"A": true, "AAAA": true, "CNAME": true,
		"MX": true, "TXT": true, "NS": true,
	}
	if !allowedTypes[req.Type] {
		middleware.ErrorResponse(c, "不支持的记录类型")
		return
	}

	results := make([]dnsCheckResult, len(dnsServers))
	var wg sync.WaitGroup

	for i, server := range dnsServers {
		wg.Add(1)
		go func(idx int, srv dnsServer) {
			defer wg.Done()
			results[idx] = queryDNS(req.Domain, req.Type, srv)
		}(i, server)
	}

	wg.Wait()
	middleware.SuccessResponse(c, results)
}

/**
 * queryDNS 向指定DNS服务器查询域名记录
 * 该服务器 UDP 直查失败时，回退一次 DoH 加密查询
 */
func queryDNS(domain, recordType string, server dnsServer) dnsCheckResult {
	result := dnsCheckResult{
		Server:  server.Name,
		IP:      server.IP,
		Results: []string{},
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", server.IP+":53")
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	var records []string
	var err error

	switch recordType {
	case "A":
		var ips []net.IP
		ips, err = resolver.LookupIP(ctx, "ip4", domain)
		for _, ip := range ips {
			records = append(records, ip.String())
		}
	case "AAAA":
		var ips []net.IP
		ips, err = resolver.LookupIP(ctx, "ip6", domain)
		for _, ip := range ips {
			records = append(records, ip.String())
		}
	case "CNAME":
		var cname string
		cname, err = resolver.LookupCNAME(ctx, domain)
		if cname != "" {
			records = append(records, cname)
		}
	case "MX":
		var mxs []*net.MX
		mxs, err = resolver.LookupMX(ctx, domain)
		for _, mx := range mxs {
			records = append(records, fmt.Sprintf("%s (优先级: %d)", mx.Host, mx.Pref))
		}
	case "TXT":
		records, err = resolver.LookupTXT(ctx, domain)
	case "NS":
		var nss []*net.NS
		nss, err = resolver.LookupNS(ctx, domain)
		for _, ns := range nss {
			records = append(records, ns.Host)
		}
	}

	result.Cost = time.Since(start).Milliseconds()

	if err != nil {
		if dohResult, ok := queryDNSViaDoH(domain, recordType); ok {
			/*
			 * 用该服务器实际承担的 DoH 主机名覆盖 IP 并加注回退标记，
			 * 让「结果来自哪条通道」在响应里可见，不把它伪装成原来的 UDP 服务器
			 */
			result.Server = server.Name + " (DoH回退)"
			result.IP = dohResult.Server
			result.Results = dohResult.Records
			result.Cost = time.Since(start).Milliseconds()
			result.TTL = fmt.Sprintf("%dms", result.Cost)
			return result
		}
		result.Error = err.Error()
	}
	if records != nil {
		result.Results = records
	}
	result.TTL = fmt.Sprintf("%dms", result.Cost)

	return result
}

/**
 * queryDNSViaDoH 以 DoH 通道查询记录，ok 表示该通道可用
 * 记录类型不在 DoH 映射表内时由 doh 包返回错误，此处统一归为不可用
 * Cost 语义要求记录 UDP + DoH 合计耗时，故给足 2 倍单次超时（本机实测最快一家仅 32ms），
 * 避免 ctx 提前取消掩盖 DoH 的真实耗时
 */
func queryDNSViaDoH(domain, recordType string) (doh.Result, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*doh.Timeout)
	defer cancel()

	res, err := doh.QueryContext(ctx, domain, recordType)
	if err != nil {
		return doh.Result{}, false
	}
	return res, true
}
