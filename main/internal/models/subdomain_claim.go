package models

import "time"

/*
 * SubdomainClaim 子域名前缀的占用记录
 *
 * 一个域名下的同一个子域名前缀（如 www）在任一时刻只归一个用户使用。
 * 这既是配额计量的单位（一个前缀占一条 max_records 额度），
 * 也是排他性的依据（他人不能再在该前缀建任何记录）
 *
 * 为什么不直接从 dns_records 派生：
 *   dns_records 是云端副本，被定时对账整表覆盖，本地派生字段（uid）在其中
 *   没有权威性——云端记录一律 uid=0，对账还会把 status 等字段写回去。
 *   占用关系是面板侧的事实，必须有自己的表，否则一次对账就能把它抹掉
 *
 * 由写入路径在云端写成功后登记，由删除路径在最后一条记录消失时释放
 */
type SubdomainClaim struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DomainID  uint      `gorm:"column:did;not null;uniqueIndex:idx_subdomain_claim" json:"did"`
	Prefix    string    `gorm:"size:255;not null;uniqueIndex:idx_subdomain_claim" json:"prefix"`
	UserID    uint      `gorm:"column:uid;not null;index" json:"uid"`
	Domain    string    `gorm:"size:255" json:"domain"` // 冗余，便于展示
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
