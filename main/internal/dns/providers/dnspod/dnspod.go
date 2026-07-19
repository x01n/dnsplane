package dnspod

import (
	"context"
	"errors"
	"fmt"
	"main/internal/dns"
	"strconv"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerr "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
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
			Remark: 1, Status: true, Redirect: true, Log: true, Weight: true, Page: false, Add: true, RecordGroup: true, DomainAlias: true,
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

/* normalizeRecordLine DNSPod v20210323 的 RecordLine 为线路名称（如「默认」），非数字 ID；历史配置曾用 "0" 表示默认 */
func normalizeRecordLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.EqualFold(line, "default") || line == "0" {
		return "默认"
	}
	return line
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
		request.RecordLine = common.StringPtr(normalizeRecordLine(line))
	}

	response, err := p.client.DescribeRecordList(request)
	if err != nil {
		/* 按子域名等条件查询时，若该条件下尚无任何记录，DNSPod 返回 ResourceNotFound.NoDataOfRecord
		   而非空列表；ACME 自动写 _acme-challenge 前会先 List，需当作 0 条否则无法 Create */
		var sdkErr *tcerr.TencentCloudSDKError
		if errors.As(err, &sdkErr) && sdkErr.Code == "ResourceNotFound.NoDataOfRecord" {
			return &dns.PageResult{
				Total:   0,
				Records: []dns.Record{},
			}, nil
		}
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
	line = normalizeRecordLine(line)
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
	line = normalizeRecordLine(line)
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

/* CreateSubdomainValidateTxtValue 获取子域托管验证 TXT 记录值 */
func (p *Provider) CreateSubdomainValidateTxtValue(ctx context.Context, domain string) (*dns.SubdomainValidateResult, error) {
	request := dnspod.NewCreateSubdomainValidateTXTValueRequest()
	request.DomainZone = common.StringPtr(domain)

	response, err := p.client.CreateSubdomainValidateTXTValueWithContext(ctx, request)
	if err != nil {
		return nil, err
	}

	r := response.Response
	result := &dns.SubdomainValidateResult{}
	if r.Domain != nil {
		result.Domain = strings.TrimSpace(*r.Domain)
	}
	if r.Subdomain != nil {
		result.Subdomain = strings.TrimSpace(*r.Subdomain)
	}
	if r.Value != nil {
		result.Value = strings.TrimSpace(*r.Value)
	}
	return result, nil
}

/* DescribeSubdomainValidateStatus 查询子域验证状态，返回 nil 表示验证通过 */
func (p *Provider) DescribeSubdomainValidateStatus(ctx context.Context, domain string) error {
	request := dnspod.NewDescribeSubdomainValidateStatusRequest()
	request.DomainZone = common.StringPtr(domain)

	_, err := p.client.DescribeSubdomainValidateStatusWithContext(ctx, request)
	return err
}

/* AddDomainWithNS 添加域名并返回 NS 列表 */
func (p *Provider) AddDomainWithNS(ctx context.Context, domain string) (domainID string, nameServers []string, err error) {
	request := dnspod.NewCreateDomainRequest()
	request.Domain = common.StringPtr(domain)

	response, err := p.client.CreateDomainWithContext(ctx, request)
	if err != nil {
		return "", nil, err
	}

	info := response.Response.DomainInfo
	if info.Id != nil {
		domainID = strconv.FormatUint(*info.Id, 10)
	}
	for _, ns := range info.GradeNsList {
		if ns != nil {
			v := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(*ns), "."))
			if v != "" {
				nameServers = append(nameServers, v)
			}
		}
	}
	return domainID, nameServers, nil
}

/* GetRecordGroups 获取解析记录分组列表 */
func (p *Provider) GetRecordGroups(ctx context.Context) ([]dns.RecordGroup, error) {
	request := dnspod.NewDescribeRecordGroupListRequest()
	request.Domain = common.StringPtr(p.domain)

	response, err := p.client.DescribeRecordGroupList(request)
	if err != nil {
		return nil, err
	}

	var groups []dns.RecordGroup
	for _, g := range response.Response.GroupList {
		groups = append(groups, dns.RecordGroup{
			ID:   strconv.FormatUint(*g.GroupId, 10),
			Name: *g.GroupName,
		})
	}
	return groups, nil
}

/* ChangeRecordGroup 修改解析记录所属分组 */
func (p *Provider) ChangeRecordGroup(ctx context.Context, recordIDs []string, groupID string) error {
	gid, err := strconv.ParseUint(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("无效的分组ID: %w", err)
	}

	recordIDStr := strings.Join(recordIDs, "|")
	request := dnspod.NewModifyRecordToGroupRequest()
	request.Domain = common.StringPtr(p.domain)
	request.GroupId = common.Uint64Ptr(gid)
	request.RecordId = common.StringPtr(recordIDStr)

	_, err = p.client.ModifyRecordToGroup(request)
	return err
}

/* GetDomainRecordsByGroup 按分组获取解析记录 */
func (p *Provider) GetDomainRecordsByGroup(ctx context.Context, groupID string, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	gid, err := strconv.ParseUint(groupID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("无效的分组ID: %w", err)
	}

	request := dnspod.NewDescribeRecordListRequest()
	request.Domain = common.StringPtr(p.domain)
	request.Offset = common.Uint64Ptr(uint64((page - 1) * pageSize))
	request.Limit = common.Uint64Ptr(uint64(pageSize))
	request.GroupId = common.Uint64Ptr(gid)

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
		request.RecordLine = common.StringPtr(normalizeRecordLine(line))
	}

	response, err := p.client.DescribeRecordList(request)
	if err != nil {
		var sdkErr *tcerr.TencentCloudSDKError
		if errors.As(err, &sdkErr) && sdkErr.Code == "ResourceNotFound.NoDataOfRecord" {
			return &dns.PageResult{
				Total:   0,
				Records: []dns.Record{},
			}, nil
		}
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

/* GetDomainAliasList 获取域名别名列表 */
func (p *Provider) GetDomainAliasList(ctx context.Context) ([]dns.DomainAlias, error) {
	request := dnspod.NewDescribeDomainAliasListRequest()
	request.Domain = common.StringPtr(p.domain)

	response, err := p.client.DescribeDomainAliasList(request)
	if err != nil {
		return nil, err
	}

	var aliases []dns.DomainAlias
	for _, a := range response.Response.DomainAliasList {
		aliases = append(aliases, dns.DomainAlias{
			ID:     strconv.FormatInt(*a.Id, 10),
			Name:   *a.DomainAlias,
			Status: int(*a.Status),
		})
	}
	return aliases, nil
}

/* AddDomainAlias 添加域名别名 */
func (p *Provider) AddDomainAlias(ctx context.Context, alias string) error {
	request := dnspod.NewCreateDomainAliasRequest()
	request.Domain = common.StringPtr(p.domain)
	request.DomainAlias = common.StringPtr(alias)

	_, err := p.client.CreateDomainAlias(request)
	return err
}

/* DeleteDomainAlias 删除域名别名 */
func (p *Provider) DeleteDomainAlias(ctx context.Context, aliasID string) error {
	aid, err := strconv.ParseInt(aliasID, 10, 64)
	if err != nil {
		return fmt.Errorf("无效的别名ID: %w", err)
	}

	request := dnspod.NewDeleteDomainAliasRequest()
	request.Domain = common.StringPtr(p.domain)
	request.DomainAliasId = common.Int64Ptr(aid)

	_, err = p.client.DeleteDomainAlias(request)
	return err
}
