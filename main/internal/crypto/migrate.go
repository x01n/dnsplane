package crypto

import (
	"fmt"

	"gorm.io/gorm"
)

// MigrateSpec 描述一次迁移任务：表名 + 列名。
type MigrateSpec struct {
	Table   string
	Columns []string
}

// DefaultMigrateSpecs 与 models/hooks.go 里列出的加密字段保持一致。
// 新增加密字段时需同步在两处追加。
var DefaultMigrateSpecs = []MigrateSpec{
	{Table: "accounts", Columns: []string{"config"}},
	{Table: "cert_accounts", Columns: []string{"config", "ext"}},
	{Table: "cert_orders", Columns: []string{"private_key", "info"}},
	{Table: "cert_deploys", Columns: []string{"config", "info"}},
	{Table: "dm_tasks", Columns: []string{"proxy_password"}},
	// users.reset_token 不在此列表：采用 sha256 指纹而非可逆加密（见 handler/auth.go）
	{Table: "users", Columns: []string{"totp_secret"}},
	{Table: "user_oauths", Columns: []string{"access_token", "refresh_token"}},
}

// MigratePlaintext 扫描上述表中历史明文列并原地加密。
// 幂等：IsEncrypted 命中即跳过；支持 SQLite / MySQL（使用参数化 UPDATE）。
//
// 注意：此函数假设表结构已由 AutoMigrate 创建完毕，应在 migrate() 之后调用。
func MigratePlaintext(db *gorm.DB) (int, error) {
	total := 0
	for _, spec := range DefaultMigrateSpecs {
		if !db.Migrator().HasTable(spec.Table) {
			continue
		}
		for _, col := range spec.Columns {
			n, err := encryptColumn(db, spec.Table, col)
			if err != nil {
				return total, fmt.Errorf("migrate %s.%s: %w", spec.Table, col, err)
			}
			total += n
		}
	}
	return total, nil
}

// encryptColumn 扫描单列，找出未加密的非空值并加密写回。
// 使用主键 id 定位行；若个别表无 id 主键需单独处理（当前列表内均为 id 主键）。
func encryptColumn(db *gorm.DB, table, column string) (int, error) {
	type row struct {
		ID    uint
		Value string
	}

	query := fmt.Sprintf(
		"SELECT id AS id, %s AS value FROM %s WHERE %s IS NOT NULL AND %s != '' AND %s NOT LIKE ?",
		column, table, column, column, column,
	)
	var rows []row
	if err := db.Raw(query, encPrefix+"%").Scan(&rows).Error; err != nil {
		return 0, err
	}

	updated := 0
	for _, r := range rows {
		if IsEncrypted(r.Value) {
			continue // 双保险
		}
		enc, err := Encrypt(r.Value)
		if err != nil {
			return updated, err
		}
		upd := fmt.Sprintf("UPDATE %s SET %s = ? WHERE id = ?", table, column)
		if err := db.Exec(upd, enc, r.ID).Error; err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}
