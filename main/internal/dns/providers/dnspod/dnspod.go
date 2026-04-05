package dnspod

import (
	"context"
	"fmt"
	"main/internal/dns"
	"strconv"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

func init() {
	dns.Register("dnspod", NewProvider, dns.ProviderConfig{
		Type: "dnspod",
		Name: "腾讯云",
		Icon: "dnspod.ico",
		Config: []dns.ConfigField{
			{Name: "SecretId", Key: "SecretId", Type: "input", Required: true},
			{Name: "SecretKey", Key: "SecretKey", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 1, Status: true, Redirect: true, Log: true, Weight: true, Page: false, Add: true,
		},
	})
}

type Provider struct {
	client   *dnspod.Client
	domain   string
	domainID string
	lastErr  string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	credential := common.NewCredential(config["SecretId"], config["SecretKey"])
	cpf := profile.NewClientProfile()
	client, err := dnspod.NewClient(credential, "", cpf)
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
	_, err := p.GetDomainList(ctx, "", 1, 1)
	return err
}

func (p *Provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	request := dnspod.NewDescribeDomainListRequest()
	if keyword != "" {
		request.Keyword = common.StringPtr(keyword)
	}
	request.Offset = common.Int64Ptr(int64((page - 1) * pageSize))
	request.Limit = common.Int64Ptr(int64(pageSize))

	response, err := p.client.DescribeDomainList(request)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	for _, d := range response.Response.DomainList {
		domains = append(domains, dns.DomainInfo{
			ID:          strconv.FormatUint(*d.DomainId, 10),
			Name:        *d.Name,
			RecordCount: int(*d.RecordCount),
		})
	}

	return &dns.PageResult{
		Total:   int(*response.Response.DomainCountInfo.AllTotal),
		Records: domains,
	}, nil
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	request := dnspod.NewDescribeRecordListRequest()
	request.Domain = common.StringPtr(p.domain)
	request.Offset = common.Uint64Ptr(uint64((page - 1) * pageSize))
	request.Limit = common.Uint64Ptr(uint64(pageSize))

	if keyword != "" {
		request.Keyword = common.StringPtr(keyword)
	}
	if recordType != "" {
		request.RecordType = common.StringPtr(recordType)
	}
	if subDomain != "" {
		request.Subdomain = common.StringPtr(subDomain)
	}
	if line != "" {
		request.RecordLine = common.StringPtr(line)
	}

	response, err := p.client.DescribeRecordList(request)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	for _, r := range response.Response.RecordList {
		record := dns.Record{
			ID:     strconv.FormatUint(*r.RecordId, 10),
			Name:   *r.Name,
			Type:   *r.Type,
			Value:  *r.Value,
			TTL:    int(*r.TTL),
			Line:   *r.Line,
			Remark: *r.Remark,
		}
		if *r.Status == "ENABLE" {
			record.Status = "enable"
		} else {
			record.Status = "disable"
		}
		if r.Weight != nil {
			record.Weight = int(*r.Weight)
		}
		records = append(records, record)
	}

	return &dns.PageResult{
		Total:   int(*response.Response.RecordCountInfo.TotalCount),
		Records: records,
	}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	id, _ := strconv.ParseUint(recordID, 10, 64)
	request := dnspod.NewDescribeRecordRequest()
	request.Domain = common.StringPtr(p.domain)
	request.RecordId = common.Uint64Ptr(id)

	response, err := p.client.DescribeRecord(request)
	if err != nil {
		return nil, err
	}

	r := response.Response.RecordInfo
	return &dns.Record{
		ID:    recordID,
		Name:  *r.SubDomain,
		Type:  *r.RecordType,
		Value: *r.Value,
		TTL:   int(*r.TTL),
		Line:  *r.RecordLine,
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	if line == "" || line == "default" {
		line = "默认"
	}
	request := dnspod.NewCreateRecordRequest()
	request.Domain = common.StringPtr(p.domain)
	request.SubDomain = common.StringPtr(name)
	request.RecordType = common.StringPtr(recordType)
	request.RecordLine = common.StringPtr(line)
	request.Value = common.StringPtr(value)
	request.TTL = common.Uint64Ptr(uint64(ttl))

	if recordType == "MX" {
		request.MX = common.Uint64Ptr(uint64(mx))
	}
	if weight != nil {
		request.Weight = common.Uint64Ptr(uint64(*weight))
	}

	response, err := p.client.CreateRecord(request)
	if err != nil {
		return "", err
	}

	recordID := strconv.FormatUint(*response.Response.RecordId, 10)
	if remark != "" {
		_ = p.UpdateDomainRecordRemark(ctx, recordID, remark)
	}

	return recordID, nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	id, _ := strconv.ParseUint(recordID, 10, 64)
	request := dnspod.NewModifyRecordRequest()
	request.Domain = common.StringPtr(p.domain)
	request.RecordId = common.Uint64Ptr(id)
	request.SubDomain = common.StringPtr(name)
	request.RecordType = common.StringPtr(recordType)
	request.RecordLine = common.StringPtr(line)
	request.Value = common.StringPtr(value)
	request.TTL = common.Uint64Ptr(uint64(ttl))

	if recordType == "MX" {
		request.MX = common.Uint64Ptr(uint64(mx))
	}
	if weight != nil {
		request.Weight = common.Uint64Ptr(uint64(*weight))
	}

	_, err := p.client.ModifyRecord(request)
	if err != nil {
		return err
	}

	if remark != "" {
		_ = p.UpdateDomainRecordRemark(ctx, recordID, remark)
	}

	return nil
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	id, _ := strconv.ParseUint(recordID, 10, 64)
	request := dnspod.NewModifyRecordRemarkRequest()
	request.Domain = common.StringPtr(p.domain)
	request.RecordId = common.Uint64Ptr(id)
	request.Remark = common.StringPtr(remark)

	_, err := p.client.ModifyRecordRemark(request)
	return err
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	id, _ := strconv.ParseUint(recordID, 10, 64)
	request := dnspod.NewDeleteRecordRequest()
	request.Domain = common.StringPtr(p.domain)
	request.RecordId = common.Uint64Ptr(id)

	_, err := p.client.DeleteRecord(request)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	id, _ := strconv.ParseUint(recordID, 10, 64)
	request := dnspod.NewModifyRecordStatusRequest()
	request.Domain = common.StringPtr(p.domain)
	request.RecordId = common.Uint64Ptr(id)
	if enable {
		request.Status = common.StringPtr("ENABLE")
	} else {
		request.Status = common.StringPtr("DISABLE")
	}

	_, err := p.client.ModifyRecordStatus(request)
	return err
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	/*
			request := dnspod.NewDescribeRecordLogListRequest()
			request.Domain = common.StringPtr(p.domain)
			request.Offset = common.Int64Ptr(int64((page - 1) * pageSize))
		request.Limit = common.Int64Ptr(int64(pageSize))

			response, err := p.client.DescribeRecordLogList(request)
			if err != nil {
				return nil, err
			}

			var logs []interface{}
			for _, l := range response.Response.LogList {
				logs = append(logs, l)
			}

			return &dns.PageResult{
				Total:   int(*response.Response.TotalCount),
				Records: logs,
			}, nil
	*/
	return nil, fmt.Errorf("DNSPod log not supported")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	request := dnspod.NewDescribeRecordLineListRequest()
	request.Domain = common.StringPtr(p.domain)
	request.DomainGrade = common.StringPtr("DP_FREE")

	response, err := p.client.DescribeRecordLineList(request)
	if err != nil {
		return nil, err
	}

	var lines []dns.RecordLine
	for _, l := range response.Response.LineList {
		lines = append(lines, dns.RecordLine{
			ID:   *l.Name,
			Name: *l.Name,
		})
	}

	return lines, nil
}

func (p *Provider) GetMinTTL() int {
	return 600
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	request := dnspod.NewCreateDomainRequest()
	request.Domain = common.StringPtr(domain)
	_, err := p.client.CreateDomain(request)
	return err
}
