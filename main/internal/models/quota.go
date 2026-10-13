package models

import "time"

/*
 * 配额与套餐
 *
 * 遗留的 plans 表来自 PHP 版的一体化设计，其中 price/duration 属计费范畴，
 * Go 侧重写时整套被丢弃只留了恒真的 CheckQuota 空壳。这里重建为纯配额模型：
 * 套餐 = 一组资源上限，不含价格与时长——当前的诉求是「限制用量」而非「收费」
 *
 * 上限取值约定（三张表一致）：
 *   -1  不限
 *    0  禁止创建
 *   >0  该维度的上限
 */

/* Plan 配额套餐 */
type Plan struct {
	ID          string `gorm:"primaryKey;size:64" json:"id"`
	Name        string `gorm:"size:100;not null" json:"name"`
	Description string `gorm:"size:500" json:"description"`
	IsDefault   bool   `json:"is_default"` // 新用户默认授予
	Sort        int    `gorm:"default:0" json:"sort"`
	/*
	 * Status 不带 gorm default 标签
	 *
	 * 带 default 的字段在 Create 时若为「停用」(0)，GORM 会跳过该列交由数据库填默认值，
	 * 结果 0 被静默改写成 1 —— 管理员停用套餐的意图会丢失。默认值改由业务代码显式赋值
	 */
	Status   int    `json:"status"` // 0 停用 1 启用
	Quotas   string `gorm:"type:text" json:"quotas"`
	Features string `gorm:"type:text" json:"features"`
	/*
	 * IncludedDomains 套餐自带的域名，逗号分隔
	 *
	 * 与 max_domains 是两回事：后者限「用户自己接入多少域名」，
	 * 这里是「套餐连同这几个域名一起给用户」。持有套餐的用户可以在
	 * 这些域名下建记录，配额（max_records）按他占用的子域名前缀计
	 */
	IncludedDomains string    `gorm:"size:1000" json:"included_domains"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

/*
 * UserSubscription 用户的套餐订阅
 *
 * PlanName 冗余存储：套餐被改名或删除后，用户当前享有的是什么仍需可追溯，
 * 只留 PlanID 会让历史记录在套餐删除后无法解读
 */
type UserSubscription struct {
	ID         string     `gorm:"primaryKey;size:64" json:"id"`
	UserID     uint       `gorm:"column:user_id;index" json:"user_id"`
	PlanID     string     `gorm:"size:64;index" json:"plan_id"`
	PlanName   string     `gorm:"size:100" json:"plan_name"`
	Status     int        `json:"status"` // 0 停用 1 启用，理由同 Plan.Status
	AutoRenew  bool       `gorm:"default:false" json:"auto_renew"`
	StartTime  time.Time  `json:"start_time"`
	ExpireTime *time.Time `json:"expire_time"` // 空表示永久
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

/* UserQuota 管理员对单个用户的配额覆盖，优先级高于套餐 */
type UserQuota struct {
	UserID    uint      `gorm:"column:user_id;primaryKey" json:"user_id"`
	Quotas    string    `gorm:"type:text" json:"quotas"`
	Remark    string    `gorm:"size:200" json:"remark"`
	UpdatedAt time.Time `json:"updated_at"`
}

/*
 * DNSRecord 云端解析记录的本地副本
 *
 * 配额判定需要在创建前知道「该域名下现有多少条记录」，若每次都查云端会消耗
 * 服务商的 API 配额，且在配额紧张时连带影响正常的增删改。故本地留一份副本，
 * 由定时任务增量同步；创建前的判定只读本地，冲突与超限都能即时给出结论
 *
 * RecordID 是云端的记录标识，本地主键自增，两者都保留：云端 ID 用于对账与删除，
 * 本地 ID 仅供内部引用
 */
type DNSRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DomainID  uint      `gorm:"column:did;index;uniqueIndex:idx_dns_record_remote" json:"did"`
	RecordID  string    `gorm:"size:64;uniqueIndex:idx_dns_record_remote" json:"record_id"`
	Name      string    `gorm:"size:255;index" json:"name"`
	Type      string    `gorm:"size:16" json:"type"`
	Value     string    `gorm:"size:1024" json:"value"`
	Line      string    `gorm:"size:64" json:"line"`
	TTL       int       `json:"ttl"`
	MX        int       `json:"mx"`
	Weight    int       `json:"weight"`
	Status    string    `gorm:"size:16" json:"status"` // enable / disable
	Remark    string    `gorm:"size:255" json:"remark"`
	Domain    string    `gorm:"size:255;index" json:"domain"` // 冗余域名，便于跨域名统计
	CreatedBy uint      `gorm:"column:uid;index" json:"uid"`  // 本面板内创建者，0 表示云端直接创建
	SyncedAt  time.Time `gorm:"index" json:"synced_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
