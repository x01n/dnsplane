package whois

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/domainr/whois"
)

/* whoisStatusURLPattern 预编译正则：去除 WHOIS status 行中的 URL 后缀 */
var whoisStatusURLPattern = regexp.MustCompile(`\s+https?://.*`)

type DomainInfo struct {
	Domain      string     `json:"domain"`
	Registrar   string     `json:"registrar"`
	NameServers []string   `json:"name_servers"`
	CreatedDate *time.Time `json:"created_date"`
	ExpiryDate  *time.Time `json:"expiry_date"`
	UpdatedDate *time.Time `json:"updated_date"`
	Status      []string   `json:"status"`
	RawData     string     `json:"raw_data"`
}

/*
 * Query 使用 domainr/whois 库查询域名 WHOIS 信息
 * 功能：通过 goroutine + channel 包装实现 context 超时/取消控制
 *       （domainr/whois 原生 Fetch 不支持 context）
 */
func Query(ctx context.Context, domain string) (*DomainInfo, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))

	req, err := whois.NewRequest(domain)
	if err != nil {
		return nil, err
	}

	type fetchResult struct {
		resp *whois.Response
		err  error
	}
	ch := make(chan fetchResult, 1)
	go func() {
		resp, err := whois.DefaultClient.Fetch(req)
		ch <- fetchResult{resp, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return parseWhoisResponse(domain, string(r.resp.Body)), nil
	}
}

func parseWhoisResponse(domain, rawData string) *DomainInfo {
	info := &DomainInfo{
		Domain:  domain,
		RawData: rawData,
	}

	lines := strings.Split(rawData, "\n")

	datePatterns := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
		"02-Jan-2006",
		"2006.01.02",
		"2006/01/02",
		"2006-01-02",
		"January 2, 2006",
		"02 Jan 2006",
		"2006-01-02T15:04:05-07:00",
		time.RFC3339,
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%") || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch {
		case strings.Contains(key, "registrar") && !strings.Contains(key, "abuse") && !strings.Contains(key, "url"):
			if info.Registrar == "" {
				info.Registrar = value
			}
		case strings.Contains(key, "name server") || strings.Contains(key, "nserver") || key == "nameserver":
			ns := strings.ToLower(strings.Fields(value)[0])
			if ns != "" {
				info.NameServers = append(info.NameServers, ns)
			}
		case strings.Contains(key, "creation") || strings.Contains(key, "created") ||
			key == "registration time" || key == "registered" || key == "domain registration date":
			if info.CreatedDate == nil {
				info.CreatedDate = parseDate(value, datePatterns)
			}
		case strings.Contains(key, "expir") || key == "registry expiry date" ||
			key == "expiration time" || key == "paid-till" || key == "renewal date":
			if info.ExpiryDate == nil {
				info.ExpiryDate = parseDate(value, datePatterns)
			}
		case strings.Contains(key, "updated") || strings.Contains(key, "modified") || key == "last update":
			if info.UpdatedDate == nil {
				info.UpdatedDate = parseDate(value, datePatterns)
			}
		case strings.Contains(key, "status") && !strings.Contains(key, "dnssec"):
			status := whoisStatusURLPattern.ReplaceAllString(value, "")
			if status != "" {
				info.Status = append(info.Status, status)
			}
		}
	}

	return info
}

func parseDate(value string, patterns []string) *time.Time {
	value = strings.TrimSpace(value)

	// 尝试所有模式
	for _, pattern := range patterns {
		t, err := time.Parse(pattern, value)
		if err == nil {
			return &t
		}
	}

	// 尝试只提取日期部分
	if idx := strings.Index(value, "T"); idx > 0 {
		dateOnly := value[:idx]
		t, err := time.Parse("2006-01-02", dateOnly)
		if err == nil {
			return &t
		}
	}

	// 尝试从空格分隔的字符串中提取
	if parts := strings.Fields(value); len(parts) > 0 {
		for _, pattern := range patterns {
			t, err := time.Parse(pattern, parts[0])
			if err == nil {
				return &t
			}
		}
	}

	return nil
}
