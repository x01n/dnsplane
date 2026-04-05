package dns

import "context"

/* Record DNS记录 */
type Record struct {
	ID      string `json:"id"`
	Name    string `json:"name"`    // 主机记录
	Type    string `json:"type"`    // A, AAAA, CNAME, MX, TXT, NS, SRV, CAA
	Value   string `json:"value"`   // 记录值
	TTL     int    `json:"ttl"`     // TTL
	Line    string `json:"line"`    // 线路
	MX      int    `json:"mx"`      // MX优先级
	Weight  int    `json:"weight"`  // 权重
	Status  string `json:"status"`  // enable, disable
	Remark  string `json:"remark"`  // 备注
	Updated string `json:"updated"` // 更新时间
}

/* DomainInfo 域名信息 */
type DomainInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RecordCount int    `json:"record_count"`
	Status      string `json:"status"`
}

/* RecordLine 解析线路 */
type RecordLine struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

/* PageResult 分页结果 */
type PageResult struct {
	Total   int         `json:"total"`
	Records interface{} `json:"records"`
}

/* Provider DNS服务商接口 */
type Provider interface {
	// GetError 获取错误信息
	GetError() string

	// Check 检查账户配置
	Check(ctx context.Context) error

	// GetDomainList 获取域名列表
	GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*PageResult, error)

	// GetDomainRecords 获取域名解析记录
	GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*PageResult, error)

	// GetSubDomainRecords 获取子域名解析记录
	GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*PageResult, error)

	// GetDomainRecordInfo 获取单条解析记录
	GetDomainRecordInfo(ctx context.Context, recordID string) (*Record, error)

	// AddDomainRecord 添加解析记录
	AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error)

	// UpdateDomainRecord 更新解析记录
	UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error

	// UpdateDomainRecordRemark 更新解析记录备注
	UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error

	// DeleteDomainRecord 删除解析记录
	DeleteDomainRecord(ctx context.Context, recordID string) error

	// SetDomainRecordStatus 设置解析记录状态
	SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error

	// GetDomainRecordLog 获取解析记录日志
	GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*PageResult, error)

	// GetRecordLine 获取解析线路
	GetRecordLine(ctx context.Context) ([]RecordLine, error)

	// GetMinTTL 获取最小TTL
	GetMinTTL() int

	// AddDomain 添加域名
	AddDomain(ctx context.Context, domain string) error
}

/* ProviderConfig DNS服务商配置 */
type ProviderConfig struct {
	Type     string           `json:"type"`
	Name     string           `json:"name"`
	Icon     string           `json:"icon"`
	Note     string           `json:"note"`
	Config   []ConfigField    `json:"config"`
	Features ProviderFeatures `json:"features"`
}

/* ConfigField 配置字段 */
type ConfigField struct {
	Name        string         `json:"name"`
	Key         string         `json:"key"`
	Type        string         `json:"type"` // input, select, radio
	Placeholder string         `json:"placeholder"`
	Required    bool           `json:"required"`
	Options     []ConfigOption `json:"options,omitempty"`
	Value       string         `json:"value,omitempty"`
}

/* ConfigOption 配置选项 */
type ConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

/* ProviderFeatures 服务商特性 */
type ProviderFeatures struct {
	Remark   int  `json:"remark"`   // 0:不支持 1:单独设置 2:和记录一起设置
	Status   bool `json:"status"`   // 是否支持启用暂停
	Redirect bool `json:"redirect"` // 是否支持域名转发
	Log      bool `json:"log"`      // 是否支持查看日志
	Weight   bool `json:"weight"`   // 是否支持权重
	Page     bool `json:"page"`     // 是否客户端分页
	Add      bool `json:"add"`      // 是否支持添加域名
}
