package dbcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const usersListPrefix = "db:v1:users:list:"

const certAccountsListPrefix = "db:v1:cert:accounts:list:"

// KeyAccountsAdmin DNS 账户列表缓存（管理员视角）
func KeyAccountsAdmin() string { return "db:v1:accounts:admin" }

// KeyAccountsUser DNS 账户列表缓存（普通用户仅自己的账户）
func KeyAccountsUser(uid string) string { return "db:v1:accounts:user:" + uid }

// KeyUsersList 用户列表分页缓存键（keyword 非空时用哈希避免特殊字符）
func KeyUsersList(page, pageSize int, keyword string) string {
	kwPart := "_"
	if keyword != "" {
		h := sha256.Sum256([]byte(keyword))
		kwPart = hex.EncodeToString(h[:8])
	}
	return fmt.Sprintf("%s%d:%d:%s", usersListPrefix, page, pageSize, kwPart)
}

// PrefixUsersList 用户列表缓存键前缀（批量失效）
func PrefixUsersList() string { return usersListPrefix }

// BustAccounts 账户增删改后失效管理员列表与指定用户列表
func BustAccounts(ownerUID string) {
	_ = Delete(context.Background(), KeyAccountsAdmin(), KeyAccountsUser(ownerUID))
}

// BustUserList 用户或域名权限等变更后失效所有用户列表分页缓存
func BustUserList() {
	_ = DeletePrefix(context.Background(), PrefixUsersList())
}

// KeyCertAccountsList 证书/部署账户列表缓存（管理员共用 admin 键，普通用户按 uid 分片）
func KeyCertAccountsList(deploy bool, admin bool, uid uint) string {
	d := 0
	if deploy {
		d = 1
	}
	if admin {
		return fmt.Sprintf("%sd%d:admin", certAccountsListPrefix, d)
	}
	return fmt.Sprintf("%sd%d:u%d", certAccountsListPrefix, d, uid)
}

// BustCertAccountsList 证书或部署账户增删改后失效列表缓存
func BustCertAccountsList() {
	_ = DeletePrefix(context.Background(), certAccountsListPrefix)
}
