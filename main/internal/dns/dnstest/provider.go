/*
 * Package dnstest 提供内存版 DNS 服务商，供 handler 与 apiv1 两个包的用例
 * 走完真实的 dns.GetProvider 装配链
 *
 * 独立成子包而非放在 dns 包里：它是测试专用代码，与生产实现分开才能让
 * 「dns 包里有没有测试残留」一眼可辨。调用方只需 `dnstest.Register()`
 * （幂等），之后把账户的 type 写为 `memory` 即可
 */
package dnstest

import (
	"context"
	"strings"
	"sync"

	"main/internal/dns"
)

/*
 * 内存服务商只实现被记录写路径用到的那部分接口，其余返回零值。
 * 生产环境不会注册它：内存状态按 domainID 分域，仅存在于进程内
 */

/* MemoryRecord 内存服务商持有的一条记录 */
type MemoryRecord struct {
	ID     string
	Name   string
	Type   string
	Value  string
	Line   string
	TTL    int
	MX     int
	Weight int
	Status string
	Remark string
}

/* Store 内存服务商的共享状态，按 domainID 分域 */
type Store struct {
	mu      sync.Mutex
	records map[string]*MemoryRecord
	nextID  int
}

var (
	storesMu sync.Mutex
	stores   = map[string]*Store{}
	once     sync.Once
)

/* Register 注册内存服务商（幂等），账户 type 写 "memory" 即可命中 */
func Register() {
	once.Do(func() {
		dns.Register("memory", func(config map[string]string, domain, domainID string) dns.Provider {
			return &provider{store: For(domainID), domainID: domainID}
		}, dns.ProviderConfig{Type: "memory", Name: "内存测试服务商"})
	})
}

/* Reset 清空全部内存状态，用例之间互不影响 */
func Reset() {
	storesMu.Lock()
	defer storesMu.Unlock()
	stores = map[string]*Store{}
}

/* For 取（必要时新建）某个域名的内存存储，供用例预置与断言 */
func For(domainID string) *Store {
	storesMu.Lock()
	defer storesMu.Unlock()
	s, ok := stores[domainID]
	if !ok {
		s = &Store{records: map[string]*MemoryRecord{}}
		stores[domainID] = s
	}
	return s
}

/* Put 预置一条记录，id 传空则自动分配，返回其 ID */
func (s *Store) Put(id, name, recordType, value string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		s.nextID++
		id = "mem-" + itoa(s.nextID)
	}
	s.records[id] = &MemoryRecord{ID: id, Name: name, Type: recordType, Value: value, Status: "enable"}
	return id
}

/* Get 读取一条记录 */
func (s *Store) Get(id string) (MemoryRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return MemoryRecord{}, false
	}
	return *r, true
}

/* Count 当前记录条数 */
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

var errNotFound = errString("memory: 记录不存在")

type errString string

func (e errString) Error() string { return string(e) }

type provider struct {
	store    *Store
	domainID string
	err      string
}

func (p *provider) GetError() string { return p.err }

func (p *provider) Check(ctx context.Context) error { return nil }

func (p *provider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	r, ok := p.store.Get(recordID)
	if !ok {
		p.err = "记录不存在"
		return nil, errNotFound
	}
	return &dns.Record{
		ID: r.ID, Name: r.Name, Type: r.Type, Value: r.Value,
		Line: r.Line, TTL: r.TTL, MX: r.MX, Weight: r.Weight,
		Status: r.Status, Remark: r.Remark,
	}, nil
}

func (p *provider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	id := p.store.Put("", name, recordType, value)
	p.store.mu.Lock()
	if r, ok := p.store.records[id]; ok {
		r.Line, r.TTL, r.MX, r.Remark = line, ttl, mx, remark
		if weight != nil {
			r.Weight = *weight
		}
	}
	p.store.mu.Unlock()
	return id, nil
}

func (p *provider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	r, ok := p.store.records[recordID]
	if !ok {
		p.err = "记录不存在"
		return errNotFound
	}
	r.Name, r.Type, r.Value, r.Line, r.TTL, r.MX, r.Remark = name, recordType, value, line, ttl, mx, remark
	if weight != nil {
		r.Weight = *weight
	}
	return nil
}

func (p *provider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	r, ok := p.store.records[recordID]
	if !ok {
		return errNotFound
	}
	r.Remark = remark
	return nil
}

func (p *provider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	delete(p.store.records, recordID)
	return nil
}

func (p *provider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	r, ok := p.store.records[recordID]
	if !ok {
		return errNotFound
	}
	if enable {
		r.Status = "enable"
	} else {
		r.Status = "disable"
	}
	return nil
}

func (p *provider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	out := make([]dns.Record, 0, len(p.store.records))
	for _, r := range p.store.records {
		if subDomain != "" && !strings.EqualFold(r.Name, subDomain) {
			continue
		}
		if recordType != "" && !strings.EqualFold(r.Type, recordType) {
			continue
		}
		out = append(out, dns.Record{ID: r.ID, Name: r.Name, Type: r.Type, Value: r.Value, Status: r.Status})
	}
	return &dns.PageResult{Total: len(out), Records: out}, nil
}

/*
 * 以下方法不被记录写路径触达，返回零值以满足 dns.Provider 接口
 */

func (p *provider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	return &dns.PageResult{}, nil
}

func (p *provider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return p.GetDomainRecords(ctx, page, pageSize, "", subDomain, "", recordType, line, "")
}

func (p *provider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return &dns.PageResult{}, nil
}

func (p *provider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) { return nil, nil }

func (p *provider) GetMinTTL() int { return 1 }

func (p *provider) AddDomain(ctx context.Context, domain string) error { return nil }

func (p *provider) GetRecordGroups(ctx context.Context) ([]dns.RecordGroup, error) { return nil, nil }

func (p *provider) ChangeRecordGroup(ctx context.Context, recordIDs []string, groupID string) error {
	return nil
}

func (p *provider) GetDomainRecordsByGroup(ctx context.Context, groupID string, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	return &dns.PageResult{}, nil
}

func (p *provider) GetDomainAliasList(ctx context.Context) ([]dns.DomainAlias, error) {
	return nil, nil
}

func (p *provider) AddDomainAlias(ctx context.Context, alias string) error { return nil }

func (p *provider) DeleteDomainAlias(ctx context.Context, aliasID string) error { return nil }

func (p *provider) CreateSubdomainValidateTxtValue(ctx context.Context, domain string) (*dns.SubdomainValidateResult, error) {
	return nil, nil
}

func (p *provider) DescribeSubdomainValidateStatus(ctx context.Context, domain string) error {
	return nil
}

func (p *provider) AddDomainWithNS(ctx context.Context, domain string) (string, []string, error) {
	return "", nil, nil
}
