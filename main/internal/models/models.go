package models

import (
	"time"

	"gorm.io/gorm"
)

// User 用户表
type User struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Username    string         `gorm:"uniqueIndex;size:64;not null" json:"username"`
	Password    string         `gorm:"size:80;not null" json:"-"`
	Email       string         `gorm:"size:100" json:"email"` // 用于接收重置邮件
	IsAPI       bool           `gorm:"default:false" json:"is_api"`
	// size:64 对应安全审计 L-4：generateAPIKey 升级到 32 字节（hex 后 64 字符）。
	// GORM AutoMigrate 会自动把 varchar(32) 列扩展到 64，无需手工迁移。
	APIKey      string         `gorm:"size:64" json:"api_key,omitempty"`
	Level       int            `gorm:"default:0" json:"level"`       // 0: normal, 1: admin
	Status      int            `gorm:"default:1" json:"status"`      // 0: disabled, 1: enabled
	Permissions string         `gorm:"type:text" json:"permissions"` // 功能权限JSON
	TOTPOpen    bool           `gorm:"default:false" json:"totp_open"`
	TOTPSecret  string         `gorm:"size:100" json:"-"`
	ResetToken  string         `gorm:"size:64" json:"-"` // 密码/TOTP重置Token
	ResetType   string         `gorm:"size:20" json:"-"` // 重置类型: password, totp
	ResetExpire *time.Time     `json:"-"`                // Token过期时间
	RegTime     time.Time      `json:"reg_time"`
	LastTime    *time.Time     `json:"last_time"`
	GitHubID    int64          `gorm:"column:github_id;default:0" json:"-"` // 旧版 GitHub 绑定，迁移至 user_oauth 后可弃用
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// UserOAuth 第三方 OAuth 绑定（每用户每 provider 一条）
type UserOAuth struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	UserID         uint       `gorm:"column:user_id;index;uniqueIndex:idx_user_oauth_provider" json:"user_id"`
	Provider       string     `gorm:"size:32;uniqueIndex:idx_user_oauth_provider" json:"provider"`
	ProviderUserID string     `gorm:"size:191;index" json:"provider_user_id"`
	ProviderName   string     `gorm:"size:191" json:"provider_name"`
	ProviderEmail  string     `gorm:"size:191" json:"provider_email"`
	ProviderAvatar string     `gorm:"size:512" json:"provider_avatar"`
	AccessToken    string     `gorm:"type:text" json:"-"`
	RefreshToken   string     `gorm:"type:text" json:"-"`
	ExpiresAt      *time.Time `json:"expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Account DNS账户表
type Account struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	UserID    uint           `gorm:"column:uid;index" json:"uid"`
	Type      string         `gorm:"size:20;not null" json:"type"`
	Name      string         `gorm:"size:255;not null" json:"name"`
	Config    string         `gorm:"type:text" json:"-"`
	Remark    string         `gorm:"size:100" json:"remark"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// Domain 域名表
type Domain struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	AccountID   uint           `gorm:"column:aid;index" json:"aid"`
	Name        string         `gorm:"size:255;not null;index" json:"name"`
	ThirdID     string         `gorm:"size:60" json:"third_id"`
	IsHide      bool           `gorm:"default:false" json:"is_hide"`
	IsSSO       bool           `gorm:"default:false" json:"is_sso"`
	RecordCount int            `gorm:"default:0" json:"record_count"`
	Remark      string         `gorm:"size:100" json:"remark"`
	IsNotice    bool           `gorm:"default:false" json:"is_notice"`
	RegTime     *time.Time     `json:"reg_time"`
	ExpireTime  *time.Time     `json:"expire_time"`
	CheckTime   *time.Time     `json:"check_time"`
	NoticeTime  *time.Time     `json:"notice_time"`
	CheckStatus int            `gorm:"default:0" json:"check_status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// DomainNote 域名备注（按用户独立存储，含aid的域名为一行 uid+did）
type DomainNote struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"column:uid;index;uniqueIndex:idx_domain_note_user_domain" json:"uid"`
	DomainID  uint      `gorm:"column:did;index;uniqueIndex:idx_domain_note_user_domain" json:"did"`
	Remark    string    `gorm:"size:500" json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Permission 权限表
type Permission struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"column:uid;index" json:"uid"`
	DomainID   uint       `gorm:"column:did;index" json:"did"`     // 域名ID
	Domain     string     `gorm:"size:255;not null" json:"domain"` // 域名
	SubDomain  string     `gorm:"size:80" json:"sub"`              // 子域名限制，空表示所有
	ReadOnly   bool       `gorm:"default:false" json:"read_only"`  // 只读权限
	ExpireTime *time.Time `json:"expire_time"`                     // 权限过期时间
	CreatedAt  time.Time  `json:"created_at"`
}

// Log 操作日志表
type Log struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"column:uid;index" json:"uid"`
	Username   string    `gorm:"size:64" json:"username"` // 操作者用户名
	Action     string    `gorm:"size:40;not null" json:"action"`
	Entity     string    `gorm:"size:40" json:"entity"` // 操作实体类型
	EntityID   uint      `json:"entity_id"`             // 实体ID
	Domain     string    `gorm:"size:255;index" json:"domain"`
	Data       string    `gorm:"size:500" json:"data"`
	BeforeData string    `gorm:"type:text" json:"before_data"` // 修改前数据JSON
	AfterData  string    `gorm:"type:text" json:"after_data"`  // 修改后数据JSON
	IP         string    `gorm:"size:45" json:"ip"`            // 操作IP
	UserAgent  string    `gorm:"size:200" json:"user_agent"`   // UA
	CreatedAt  time.Time `json:"created_at"`
}

// DMTask 容灾切换任务表
type DMTask struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	DomainID       uint   `gorm:"column:did;index" json:"did"`
	RR             string `gorm:"size:128;not null" json:"rr"`
	RecordID       string `gorm:"size:60;not null" json:"record_id"`
	RecordType     string `gorm:"size:20" json:"record_type"`
	RecordLine     string `gorm:"size:64" json:"record_line"`
	Type           int    `gorm:"default:0" json:"type"` // 0: pause/resume, 1: delete, 2: switch backup
	MainValue      string `gorm:"size:128" json:"main_value"`
	BackupValue    string `gorm:"size:128" json:"backup_value"`
	BackupValues   string `gorm:"size:1024" json:"backup_values"` // 多备用，逗号分隔；非空时优先于 BackupValue
	BackupType     string `gorm:"size:16" json:"backup_type"`     // ip / cname 等
	CheckType      int    `gorm:"default:0" json:"check_type"`    // 0: ping, 1: tcp, 2: http, 3: https
	CheckURL       string `gorm:"size:512" json:"check_url"`
	TCPPort        int    `json:"tcp_port"`
	Frequency      int    `gorm:"not null" json:"frequency"` // seconds
	Cycle          int    `gorm:"default:3" json:"cycle"`    // fail count before switch
	Timeout        int    `gorm:"default:2" json:"timeout"`  // seconds
	Remark         string `gorm:"size:100" json:"remark"`
	UseProxy       bool   `gorm:"default:false" json:"use_proxy"`
	CDN            bool   `gorm:"default:false" json:"cdn"`
	AddTime        int64  `json:"add_time"`
	CheckTime      int64  `json:"check_time"`
	CheckNextTime  int64  `json:"check_next_time"`
	SwitchTime     int64  `json:"switch_time"`
	ErrCount       int    `gorm:"default:0" json:"err_count"`
	Status         int    `gorm:"default:0" json:"status"`         // 0: normal, 1: switched
	MainHealth     bool   `gorm:"default:true" json:"main_health"` // 最后一次探测主源是否健康
	Active         bool   `gorm:"default:false" json:"active"`
	RecordInfo     string `gorm:"size:200" json:"record_info"`
	ExpectStatus   string `gorm:"size:64" json:"expect_status"`
	ExpectKeyword  string `gorm:"size:200" json:"expect_keyword"`
	MaxRedirects   int    `gorm:"default:0" json:"max_redirects"`
	ProxyType      string `gorm:"size:16" json:"proxy_type"`
	ProxyHost      string `gorm:"size:255" json:"proxy_host"`
	ProxyPort      int    `gorm:"default:0" json:"proxy_port"`
	ProxyUsername  string `gorm:"size:128" json:"proxy_username"`
	ProxyPassword  string `gorm:"size:256" json:"-"`
	NotifyEnabled  bool   `gorm:"default:false" json:"notify_enabled"`
	NotifyChannels string `gorm:"type:text" json:"notify_channels"` // JSON 数组字符串，如 ["mail","webhook"]
	AutoRestore    bool   `gorm:"default:false" json:"auto_restore"`
	// AllowInsecureTLS 允许 HTTPS 探测跳过证书校验（自签 / 内网场景下手动勾选）；
	// 默认 false 对应安全审计 H-3，拒绝静默放过证书错误。
	AllowInsecureTLS bool `gorm:"default:false" json:"allow_insecure_tls"`
}

// DMCheckLog 容灾监控探测历史
type DMCheckLog struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TaskID         uint      `gorm:"column:task_id;index" json:"task_id"`
	Success        bool      `gorm:"index" json:"success"`
	Duration       int64     `json:"duration"`
	Error          string    `gorm:"size:500" json:"error"`
	MainHealth     bool      `json:"main_health"`
	BackupHealths  string    `gorm:"type:text" json:"backup_healths"`
	MainDuration   int64     `json:"main_duration"`
	BackupDuration int64     `json:"backup_duration"`
	CreatedAt      time.Time `gorm:"index" json:"created_at"`
}

// DMLog 容灾切换日志表
type DMLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TaskID    uint      `gorm:"index" json:"task_id"`
	Action    int       `gorm:"default:0" json:"action"` // 1: switch to backup, 2: restore
	ErrMsg    string    `gorm:"size:100" json:"err_msg"`
	CreatedAt time.Time `json:"created_at"`
}

// CertAccount 证书账户表
type CertAccount struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	UserID    uint           `gorm:"column:uid;index" json:"uid"`
	Type      string         `gorm:"size:20;not null" json:"type"`
	Name      string         `gorm:"size:255;not null" json:"name"`
	Config    string         `gorm:"type:text" json:"-"`
	Ext       string         `gorm:"type:text" json:"-"`
	Remark    string         `gorm:"size:100" json:"remark"`
	IsDeploy  bool           `gorm:"default:false" json:"is_deploy"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// CertOrder 证书订单表
type CertOrder struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	AccountID         uint       `gorm:"column:aid" json:"aid"`
	KeyType           string     `gorm:"size:20" json:"key_type"`              // RSA, EC
	KeySize           string     `gorm:"size:20" json:"key_size"`              // 2048, 4096, 256, 384
	OrderKind         string     `gorm:"size:12;default:''" json:"order_kind"` // dns | ip | mixed（由域名/IP 自动判定）
	ChallengeType     string     `gorm:"size:16;default:''" json:"challenge_type"` // 空|dns-01|http-01（ACME 域名验证方式；通配符仅 dns-01）
	ProcessID         string     `gorm:"size:32" json:"process_id"`
	IssueTime         *time.Time `json:"issue_time"`
	ExpireTime        *time.Time `json:"expire_time"`
	Issuer            string     `gorm:"size:100" json:"issuer"`
	Status            int        `gorm:"default:0" json:"status"` // 0:pending 1:validating 2:validated 3:issued 4:revoked -1~-7:errors
	Error             string     `gorm:"size:300" json:"error"`
	IsAuto            bool       `gorm:"default:false" json:"is_auto"`
	Retry             int        `gorm:"default:0" json:"retry"`
	Retry2            int        `gorm:"default:0" json:"retry2"`
	RetryTime         *time.Time `json:"retry_time"`
	IsLock            bool       `gorm:"default:false" json:"is_lock"`
	LockTime          *time.Time `json:"lock_time"`
	IsSend            bool       `gorm:"default:false" json:"is_send"`
	Info              string     `gorm:"type:text" json:"-"`
	DNS               string     `gorm:"type:text" json:"-"`
	FullChain         string     `gorm:"type:text" json:"fullchain,omitempty"`
	PrivateKey        string     `gorm:"type:text" json:"private_key,omitempty"`
	RenewFailNoticeAt *time.Time `json:"renew_fail_notice_at"`
	ExpireNoticeAt    *time.Time `json:"expire_notice_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// CertDomain 证书域名表
type CertDomain struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	OrderID uint   `gorm:"column:oid;index" json:"oid"`
	Domain  string `gorm:"size:255;not null" json:"domain"`
	Sort    int    `gorm:"default:0" json:"sort"`
}

// CertDeploy 证书部署任务表
type CertDeploy struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"column:uid;index" json:"uid"`
	AccountID  uint       `gorm:"column:aid" json:"aid"`
	OrderID    uint       `gorm:"column:oid" json:"oid"`
	IssueTime  *time.Time `json:"issue_time"`
	Config     string     `gorm:"type:text" json:"-"`
	Remark     string     `gorm:"size:100" json:"remark"`
	LastTime   *time.Time `json:"last_time"`
	ProcessID  string     `gorm:"size:32" json:"process_id"`
	Status     int        `gorm:"default:0" json:"status"`
	Error      string     `gorm:"size:300" json:"error"`
	Active     bool       `gorm:"default:false" json:"active"`
	Retry      int        `gorm:"default:0" json:"retry"`
	MaxRetry   int        `gorm:"default:3" json:"max_retry"`    // 最大重试次数
	RetryDelay int        `gorm:"default:60" json:"retry_delay"` // 重试间隔(秒)
	RetryTime  *time.Time `json:"retry_time"`
	IsLock     bool       `gorm:"default:false" json:"is_lock"`
	LockTime   *time.Time `json:"lock_time"`
	IsSend     bool       `gorm:"default:false" json:"is_send"`
	Info       string     `gorm:"type:text" json:"-"`
	LogContent string     `gorm:"type:text" json:"log_content"` // 部署实时日志
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// CertCNAME CNAME代理表
type CertCNAME struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Domain    string    `gorm:"size:255;not null" json:"domain"`
	DomainID  uint      `gorm:"column:did" json:"did"`
	RR        string    `gorm:"size:128;not null" json:"rr"`
	Status    int       `gorm:"default:0" json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ScheduleTask 定时任务表
type ScheduleTask struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	DomainID   uint   `gorm:"column:did;index" json:"did"`
	RR         string `gorm:"size:128;not null" json:"rr"`
	RecordID   string `gorm:"size:60;not null" json:"record_id"`
	Type       int    `gorm:"default:0" json:"type"`  // 0: modify, 1: enable, 2: pause, 3: delete
	Cycle      int    `gorm:"default:0" json:"cycle"` // 0: once, 1: daily, 2: weekly, 3: monthly
	SwitchType int    `gorm:"default:0" json:"switch_type"`
	SwitchDate string `gorm:"size:10" json:"switch_date"`
	SwitchTime string `gorm:"size:20" json:"switch_time"`
	Value      string `gorm:"size:128" json:"value"`
	Line       string `gorm:"size:20" json:"line"`
	AddTime    int64  `json:"add_time"`
	UpdateTime int64  `json:"update_time"`
	NextTime   int64  `json:"next_time"`
	Active     bool   `gorm:"default:false" json:"active"`
	RecordInfo string `gorm:"size:200" json:"record_info"`
	Remark     string `gorm:"size:100" json:"remark"`
}

// Config 系统配置表
type SysConfig struct {
	Key   string `gorm:"primaryKey;size:32" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

// OptimizeIP 优选IP任务表
type OptimizeIP struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	DomainID   uint       `gorm:"column:did;index" json:"did"`
	RR         string     `gorm:"size:128;not null" json:"rr"`
	Type       int        `gorm:"default:0" json:"type"`  // 0: A记录, 1: CNAME记录
	IPType     string     `gorm:"size:50" json:"ip_type"` // cloudflare, cloudfront等
	CDNType    int        `gorm:"default:0" json:"cdn_type"`
	RecordNum  int        `gorm:"default:1" json:"recordnum"`
	TTL        int        `gorm:"default:600" json:"ttl"`
	Remark     string     `gorm:"size:100" json:"remark"`
	AddTime    time.Time  `json:"addtime"`
	UpdateTime *time.Time `json:"updatetime"`
	Status     int        `gorm:"default:0" json:"status"` // 0: 未执行, 1: 成功, 2: 失败
	ErrMsg     string     `gorm:"size:200" json:"errmsg"`
	Active     bool       `gorm:"default:true" json:"active"`
}

// CertLog 证书操作日志表
type CertLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrderID   uint      `gorm:"column:oid;index" json:"oid"`
	Type      string    `gorm:"size:40" json:"type"`
	Content   string    `gorm:"size:500" json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// RequestLog API 请求日志（独立 SQLite 库）
type RequestLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	RequestID   string    `gorm:"size:64;index" json:"request_id"`
	ErrorID     string    `gorm:"size:64;index" json:"error_id"`
	UserID      uint      `gorm:"index" json:"user_id"`
	Username    string    `gorm:"size:64" json:"username"`
	Method      string    `gorm:"size:16" json:"method"`
	Path        string    `gorm:"size:1024" json:"path"`
	Query       string    `gorm:"size:4096" json:"query"`
	Body        string    `gorm:"type:text" json:"body"`
	Headers     string    `gorm:"type:text" json:"headers"`
	IP          string    `gorm:"size:45" json:"ip"`
	UserAgent   string    `gorm:"size:500" json:"user_agent"`
	StatusCode  int       `json:"status_code"`
	Response    string    `gorm:"type:text" json:"response"`
	Duration    int64     `json:"duration"`
	IsError     bool      `gorm:"index" json:"is_error"`
	ErrorMsg    string    `gorm:"size:2000" json:"error_msg"`
	ErrorStack  string    `gorm:"type:text" json:"error_stack"`
	DBQueries   string    `gorm:"type:text" json:"db_queries"`
	DBQueryTime int64     `json:"db_query_time"`
	Extra       string    `gorm:"type:text" json:"extra"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}
