package huawei

import (
	"context"
	"fmt"
	"main/internal/dns"
	"strings"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	dns_sdk "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/model"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/region"
)

func init() {
	dns.Register("huawei", NewProvider, dns.ProviderConfig{
		Type: "huawei",
		Name: "华为云",
		Icon: "huawei.png",
		Config: []dns.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
		},
		Features: dns.ProviderFeatures{
			Remark: 2, Status: false, Redirect: false, Log: false, Weight: true, Page: false, Add: false,
		},
	})
}

type Provider struct {
	client   *dns_sdk.DnsClient
	domain   string
	domainID string
	lastErr  string
}

func NewProvider(config map[string]string, domain, domainID string) dns.Provider {
	auth := basic.NewCredentialsBuilder().
		WithAk(config["AccessKeyId"]).
		WithSk(config["SecretAccessKey"]).
		Build()

	client := dns_sdk.NewDnsClient(
		dns_sdk.DnsClientBuilder().
			WithRegion(region.CN_NORTH_1).
			WithCredential(auth).
			Build())

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
	request := &model.ListPublicZonesRequest{}
	limit := int32(pageSize)
	offset := int32((page - 1) * pageSize)
	request.Limit = &limit
	request.Offset = &offset

	if keyword != "" {
		request.Name = &keyword
	}

	response, err := p.client.ListPublicZones(request)
	if err != nil {
		return nil, err
	}

	var domains []dns.DomainInfo
	for _, z := range *response.Zones {
		name := *z.Name
		name = strings.TrimSuffix(name, ".")
		domains = append(domains, dns.DomainInfo{
			ID:          *z.Id,
			Name:        name,
			RecordCount: int(*z.RecordNum),
		})
	}

	total := 0
	if response.Metadata != nil && response.Metadata.TotalCount != nil {
		total = int(*response.Metadata.TotalCount)
	}

	return &dns.PageResult{
		Total:   total,
		Records: domains,
	}, nil
}

func (p *Provider) getHost(name string) string {
	if name == "@" {
		name = ""
	} else {
		name += "."
	}
	return name + p.domain + "."
}

func (p *Provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	request := &model.ListRecordSetsByZoneRequest{}
	request.ZoneId = p.domainID
	offset := int32((page - 1) * pageSize)
	limit := int32(pageSize)
	request.Offset = &offset
	request.Limit = &limit

	if recordType != "" {
		request.Type = &recordType
	}
	// if line != "" {
	// 	request.LineId = &line
	// }
	if keyword != "" {
		request.Name = &keyword
	}
	if status != "" {
		var s string
		if status == "enable" {
			s = "ACTIVE"
		} else {
			s = "DISABLE"
		}
		request.Status = &s
	}
	if subDomain != "" {
		name := p.getHost(subDomain)
		request.Name = &name
	}

	response, err := p.client.ListRecordSetsByZone(request)
	if err != nil {
		return nil, err
	}

	var records []dns.Record
	for _, r := range *response.Recordsets {
		zoneName := *r.ZoneName
		name := *r.Name
		name = strings.TrimSuffix(name, "."+zoneName)
		if name == "" {
			name = "@"
		}

		recType := *r.Type
		var recordValue string
		var mx int
		if r.Records != nil && len(*r.Records) > 0 {
			if recType == "MX" {
				parts := strings.SplitN((*r.Records)[0], " ", 2)
				if len(parts) == 2 {
					fmt.Sscanf(parts[0], "%d", &mx)
					recordValue = parts[1]
				}
			} else {
				recordValue = strings.Join(*r.Records, ",")
			}
		}

		statusVal := "disable"
		if *r.Status == "ACTIVE" {
			statusVal = "enable"
		}

		var weight int
		// if r.Weight != nil {
		// 	weight = int(*r.Weight)
		// }

		var remark string
		if r.Description != nil {
			remark = *r.Description
		}

		records = append(records, dns.Record{
			ID:    *r.Id,
			Name:  name,
			Type:  recType,
			Value: recordValue,
			TTL:   int(*r.Ttl),
			// Line:   *r.LineId,
			MX:     mx,
			Weight: weight,
			Status: statusVal,
			Remark: remark,
		})
	}

	total := 0
	if response.Metadata != nil && response.Metadata.TotalCount != nil {
		total = int(*response.Metadata.TotalCount)
	}

	return &dns.PageResult{
		Total:   total,
		Records: records,
	}, nil
}

func (p *Provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *Provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	request := &model.ShowRecordSetRequest{}
	request.ZoneId = p.domainID
	request.RecordsetId = recordID

	response, err := p.client.ShowRecordSet(request)
	if err != nil {
		return nil, err
	}

	zoneName := *response.ZoneName
	name := *response.Name
	name = strings.TrimSuffix(name, "."+zoneName)
	if name == "" {
		name = "@"
	}

	recType := *response.Type
	var recordValue string
	var mx int
	if response.Records != nil && len(*response.Records) > 0 {
		if recType == "MX" {
			parts := strings.SplitN((*response.Records)[0], " ", 2)
			if len(parts) == 2 {
				fmt.Sscanf(parts[0], "%d", &mx)
				recordValue = parts[1]
			}
		} else {
			recordValue = strings.Join(*response.Records, ",")
		}
	}

	statusVal := "disable"
	if *response.Status == "ACTIVE" {
		statusVal = "enable"
	}

	var weight int
	// if response.Weight != nil {
	// 	weight = int(*response.Weight)
	// }

	var remark string
	if response.Description != nil {
		remark = *response.Description
	}

	return &dns.Record{
		ID:    *response.Id,
		Name:  name,
		Type:  recType,
		Value: recordValue,
		TTL:   int(*response.Ttl),
		// Line:   *response.LineId,
		MX:     mx,
		Weight: weight,
		Status: statusVal,
		Remark: remark,
	}, nil
}

func (p *Provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	request := &model.CreateRecordSetRequest{}
	request.ZoneId = p.domainID

	hostName := p.getHost(name)
	if recordType == "TXT" && !strings.HasPrefix(value, "\"") {
		value = "\"" + value + "\""
	}

	records := strings.Split(value, ",")
	if recordType == "MX" {
		records = []string{fmt.Sprintf("%d %s", mx, value)}
	}

	body := &model.CreateRecordSetRequestBody{
		Name:    hostName,
		Type:    recordType,
		Records: records,
		Ttl:     int32Ptr(int32(ttl)),
	}

	// if line != "" {
	// 	body.LineId = &line
	// }
	if remark != "" {
		body.Description = &remark
	}
	// if weight != nil && *weight > 0 {
	// 	body.Weight = int32Ptr(int32(*weight))
	// }

	request.Body = body
	response, err := p.client.CreateRecordSet(request)
	if err != nil {
		return "", err
	}

	return *response.Id, nil
}

func (p *Provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	request := &model.UpdateRecordSetRequest{}
	request.ZoneId = p.domainID
	request.RecordsetId = recordID

	hostName := p.getHost(name)
	if recordType == "TXT" && !strings.HasPrefix(value, "\"") {
		value = "\"" + value + "\""
	}

	records := strings.Split(value, ",")
	if recordType == "MX" {
		records = []string{fmt.Sprintf("%d %s", mx, value)}
	}

	body := &model.UpdateRecordSetReq{
		Name:    &hostName,
		Type:    &recordType,
		Records: &records,
		Ttl:     int32Ptr(int32(ttl)),
	}

	if remark != "" {
		body.Description = &remark
	}

	request.Body = body
	_, err := p.client.UpdateRecordSet(request)
	return err
}

func (p *Provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return p.UpdateDomainRecord(ctx, recordID, "", "", "", "", 0, 0, nil, remark)
}

func (p *Provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	request := &model.DeleteRecordSetRequest{}
	request.ZoneId = p.domainID
	request.RecordsetId = recordID

	_, err := p.client.DeleteRecordSet(request)
	return err
}

func (p *Provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	return fmt.Errorf("华为云不支持设置记录状态")
}

func (p *Provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, fmt.Errorf("华为云不支持查看解析日志")
}

func (p *Provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) {
	return []dns.RecordLine{
		{ID: "default_view", Name: "默认"},
		{ID: "Dianxin", Name: "电信"},
		{ID: "Liantong", Name: "联通"},
		{ID: "Yidong", Name: "移动"},
		{ID: "Jiaoyuwang", Name: "教育网"},
		{ID: "Abroad", Name: "海外"},
	}, nil
}

func (p *Provider) GetMinTTL() int {
	return 1
}

func (p *Provider) AddDomain(ctx context.Context, domain string) error {
	return fmt.Errorf("华为云不支持添加域名")
	/*
		request := &model.CreatePublicZoneRequest{}
		request.Body = &model.CreatePublicZoneRequestBody{
			Name: domain,
		}
		_, err := p.client.CreatePublicZone(request)
		return err
	*/
}
