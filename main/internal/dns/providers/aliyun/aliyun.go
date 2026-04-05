package aliyun

import (
	"context"
	"main/internal/dns"
	"strings"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/alidns"
)

func init() {
	dns.Register("aliyun", NewProvider, dns.ProviderConfig{
		Type: "aliyun",
		Name: "阿里云",
		Icon: "aliyun.png",
		Config: []dns.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "AccessKeySecret", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 1, Status: true, Redirect: true, Log: true, Weight: false, Page: false, Add: true,
		},
	})
}

type Provider struct {
	client   *alidns.Client
	domain   string
	domainID string
	lastErr  string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	cred := credentials.NewAccessKeyCredential(config["AccessKeyId"], config["AccessKeySecret"])
	conf := sdk.NewConfig()
	client, err := alidns.NewClientWithOptions("cn-hangzhou", conf, cred)
	if err != nil {
		return &Provider{lastErr: err.Error()}
	}

	return &Provider{
		client:   client,
		domain:   domain,
		domainID: domainID,
	}
}

func (p *Provider) GetError() string {
	return p.lastErr
}

func (p *Provider) Check(ctx context.Context) error {
	request := alidns.CreateDescribeDomainsRequest()
	request.PageNumber = requests.NewInteger(1)
	request.PageSize = requests.NewInteger(1)
	_, err := p.client.DescribeDomains(request)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	request := alidns.CreateDescribeDomainsRequest()
	request.PageNumber = requests.NewInteger(page)
	request.PageSize = requests.NewInteger(pageSize)
	if keyword != "" {
		request.KeyWord = keyword
	}

	response, err := p.client.DescribeDomains(request)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	for _, d := range response.Domains.Domain {
		domains = append(domains, dns.DomainInfo{
			ID:          d.DomainId,
			Name:        d.DomainName,
			RecordCount: int(d.RecordCount),
		})
	}

	return &dns.PageResult{
		Total:   int(response.TotalCount),
		Records: domains,
	}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	request := alidns.CreateDescribeDomainRecordsRequest()
	request.DomainName = p.domain
	request.PageNumber = requests.NewInteger(page)
	request.PageSize = requests.NewInteger(pageSize)

	if keyword != "" {
		request.KeyWord = keyword
	}
	if recordType != "" {
		request.TypeKeyWord = recordType
	}
	if subDomain != "" {
		request.RRKeyWord = subDomain
	}
	if value != "" {
		request.ValueKeyWord = value
	}
	if line != "" {
		request.Line = line
	}
	if status != "" {
		request.Status = strings.ToUpper(status)
	}

	response, err := p.client.DescribeDomainRecords(request)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	for _, r := range response.DomainRecords.Record {
		record := dns.Record{
			ID:     r.RecordId,
			Name:   r.RR,
			Type:   r.Type,
			Value:  r.Value,
			TTL:    int(r.TTL),
			Line:   r.Line,
			Remark: r.Remark,
		}
		if r.Status == "ENABLE" {
			record.Status = "enable"
		} else {
			record.Status = "disable"
		}
		records = append(records, record)
	}

	return &dns.PageResult{
		Total:   int(response.TotalCount),
		Records: records,
	}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	request := alidns.CreateDescribeSubDomainRecordsRequest()
	request.SubDomain = subDomain + "." + p.domain
	request.PageNumber = requests.NewInteger(page)
	request.PageSize = requests.NewInteger(pageSize)
	if recordType != "" {
		request.Type = recordType
	}
	if line != "" {
		request.Line = line
	}

	response, err := p.client.DescribeSubDomainRecords(request)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	for _, r := range response.DomainRecords.Record {
		record := dns.Record{
			ID:    r.RecordId,
			Name:  r.RR,
			Type:  r.Type,
			Value: r.Value,
			TTL:   int(r.TTL),
			Line:  r.Line,
		}
		records = append(records, record)
	}

	return &dns.PageResult{
		Total:   int(response.TotalCount),
		Records: records,
	}, nil
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	request := alidns.CreateDescribeDomainRecordInfoRequest()
	request.RecordId = recordID

	response, err := p.client.DescribeDomainRecordInfo(request)
	if err != nil {
		return nil, err
	}

	return &dns.Record{
		ID:    response.RecordId,
		Name:  response.RR,
		Type:  response.Type,
		Value: response.Value,
		TTL:   int(response.TTL),
		Line:  response.Line,
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	request := alidns.CreateAddDomainRecordRequest()
	request.DomainName = p.domain
	request.RR = name
	request.Type = recordType
	request.Value = value
	request.TTL = requests.NewInteger(ttl)
	if line != "" && line != "default" {
		request.Line = line
	}
	if recordType == "MX" {
		request.Priority = requests.NewInteger(mx)
	}

	response, err := p.client.AddDomainRecord(request)
	if err != nil {
		return "", err
	}

	if remark != "" {
		_ = p.UpdateDomainRecordRemark(ctx, response.RecordId, remark)
	}

	return response.RecordId, nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	request := alidns.CreateUpdateDomainRecordRequest()
	request.RecordId = recordID
	request.RR = name
	request.Type = recordType
	request.Value = value
	request.TTL = requests.NewInteger(ttl)
	if line != "" && line != "default" {
		request.Line = line
	}
	if recordType == "MX" {
		request.Priority = requests.NewInteger(mx)
	}

	_, err := p.client.UpdateDomainRecord(request)
	if err != nil {
		return err
	}

	if remark != "" {
		_ = p.UpdateDomainRecordRemark(ctx, recordID, remark)
	}

	return nil
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	request := alidns.CreateUpdateDomainRecordRemarkRequest()
	request.RecordId = recordID
	request.Remark = remark

	_, err := p.client.UpdateDomainRecordRemark(request)
	return err
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	request := alidns.CreateDeleteDomainRecordRequest()
	request.RecordId = recordID

	_, err := p.client.DeleteDomainRecord(request)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	request := alidns.CreateSetDomainRecordStatusRequest()
	request.RecordId = recordID
	if enable {
		request.Status = "Enable"
	} else {
		request.Status = "Disable"
	}

	_, err := p.client.SetDomainRecordStatus(request)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	request := alidns.CreateDescribeRecordLogsRequest()
	request.DomainName = p.domain
	request.PageNumber = requests.NewInteger(page)
	request.PageSize = requests.NewInteger(pageSize)
	if keyword != "" {
		request.KeyWord = keyword
	}
	if startDate != "" {
		request.StartDate = startDate
	}
	if endDate != "" {
		request.EndDate = endDate
	}

	response, err := p.client.DescribeRecordLogs(request)
	if err != nil {
		return nil, err
	}

	var logs []interface{}
	for _, l := range response.RecordLogs.RecordLog {
		logs = append(logs, l)
	}

	return &dns.PageResult{
		Total:   int(response.TotalCount),
		Records: logs,
	}, nil
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	request := alidns.CreateDescribeSupportLinesRequest()
	request.DomainName = p.domain

	response, err := p.client.DescribeSupportLines(request)
	if err != nil {
		return nil, err
	}

	var lines []dns.RecordLine
	for _, l := range response.RecordLines.RecordLine {
		lines = append(lines, dns.RecordLine{
			ID:   l.LineCode,
			Name: l.LineName,
		})
	}

	return lines, nil
}

func (p *Provider) GetMinTTL() int {
	return 600
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	request := alidns.CreateAddDomainRequest()
	request.DomainName = domain
	_, err := p.client.AddDomain(request)
	return err
}
