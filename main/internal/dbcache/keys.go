package dbcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const usersListPrefix = "db:v1:users:list:"
const usersAdminFullListKey = "db:v1:users:admin_full"
const certAccountsListPrefix = "db:v1:cert:accounts:list:"
func KeyAccountsAdmin() string { return "db:v1:accounts:admin" }
func KeyAccountsUser(uid string) string { return "db:v1:accounts:user:" + uid }
func KeyUsersList(page, pageSize int, keyword string) string {
	kwPart := "_"
	if keyword != "" {
		h := sha256.Sum256([]byte(keyword))
		kwPart = hex.EncodeToString(h[:8])
	}
	return fmt.Sprintf("%s%d:%d:%s", usersListPrefix, page, pageSize, kwPart)
}

func PrefixUsersList() string { return usersListPrefix }
func KeyUsersAdminFullList() string { return usersAdminFullListKey }
func BustAccounts(ownerUID string) {
	_ = Delete(context.Background(), KeyAccountsAdmin(), KeyAccountsUser(ownerUID))
}

func BustUserList() {
	_ = Delete(context.Background(), usersAdminFullListKey)
	_ = DeletePrefix(context.Background(), PrefixUsersList())
}

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

func BustCertAccountsList() {
	_ = DeletePrefix(context.Background(), certAccountsListPrefix)
}
