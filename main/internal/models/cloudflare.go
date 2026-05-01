package models

import "time"

// CloudflareHostname Cloudflare 自定义主机名表
type CloudflareHostname struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	DomainID             uint      `gorm:"column:did;index" json:"did"`
	HostnameID           string    `gorm:"size:64;not null" json:"hostname_id"` // Cloudflare hostname ID
	Hostname             string    `gorm:"size:255;not null;index" json:"hostname"`
	CustomOriginServer   string    `gorm:"size:255" json:"custom_origin_server"`
	SSLMethod            string    `gorm:"size:10" json:"ssl_method"` // txt | http
	SSLMinTLSVersion     string    `gorm:"size:10" json:"ssl_min_tls_version"`
	SSLStatus            string    `gorm:"size:32" json:"ssl_status"`
	SSLValidationStatus  string    `gorm:"size:32" json:"ssl_validation_status"`
	VerificationStatus   string    `gorm:"size:32" json:"verification_status"`
	ValidationErrors     string    `gorm:"type:text" json:"validation_errors"`
	OwnershipVerification string   `gorm:"type:text" json:"ownership_verification"`
	CreatedOn            *time.Time `json:"created_on"`
	SyncedAt             *time.Time `json:"synced_at"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// CloudflareTunnel Cloudflare Tunnel 表
type CloudflareTunnel struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	AccountID        uint       `gorm:"column:aid;index" json:"aid"` // 本地 DNS 账户 ID
	TunnelID         string     `gorm:"size:64;not null" json:"tunnel_id"`
	Name             string     `gorm:"size:255;not null" json:"name"`
	Status           string     `gorm:"size:32" json:"status"`
	Token            string     `gorm:"size:512" json:"-"`
	ConnectionCount  int        `gorm:"default:0" json:"connection_count"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at"`
}

// CloudflareCIDRRoute Cloudflare CIDR 路由表
type CloudflareCIDRRoute struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	TunnelID         uint      `gorm:"column:tid;index" json:"tid"` // 本地 tunnel ID
	RouteID          string    `gorm:"size:64;not null" json:"route_id"`
	Network          string    `gorm:"size:64;not null" json:"network"`
	Comment          string    `gorm:"size:255" json:"comment"`
	VirtualNetworkID string    `gorm:"size:64" json:"virtual_network_id"`
	CreatedAt        time.Time `json:"created_at"`
}

// CloudflareHostnameRoute Cloudflare 主机名路由表
type CloudflareHostnameRoute struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TunnelID  uint      `gorm:"column:tid;index" json:"tid"`
	RouteID   string    `gorm:"size:64;not null" json:"route_id"`
	Hostname  string    `gorm:"size:255;not null" json:"hostname"`
	Comment   string    `gorm:"size:255" json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// DomainAlias 域名别名表
type DomainAlias struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DomainID  uint      `gorm:"column:did;index" json:"did"`
	Name      string    `gorm:"column:alias;size:255;not null;index" json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RecordLog DNS 解析日志表
type RecordLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DomainID  uint      `gorm:"column:did;index" json:"did"`
	RecordID  string    `gorm:"size:64" json:"record_id"`
	Action    string    `gorm:"size:32" json:"action"` // add, update, delete, enable, disable
	OldData   string    `gorm:"type:text" json:"old_data"`
	NewData   string    `gorm:"type:text" json:"new_data"`
	UserID    uint      `gorm:"column:uid" json:"uid"`
	CreatedAt time.Time `json:"created_at"`
}
