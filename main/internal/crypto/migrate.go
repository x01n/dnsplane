package crypto

import (
	"fmt"

	"gorm.io/gorm"
)

type MigrateSpec struct {
	Table   string
	Columns []string
}

var DefaultMigrateSpecs = []MigrateSpec{
	{Table: "accounts", Columns: []string{"config"}},
	{Table: "cert_accounts", Columns: []string{"config", "ext"}},
	{Table: "cert_orders", Columns: []string{"private_key", "info"}},
	{Table: "cert_deploys", Columns: []string{"config", "info"}},
	{Table: "dm_tasks", Columns: []string{"proxy_password"}},
	{Table: "users", Columns: []string{"totp_secret"}},
	{Table: "user_oauths", Columns: []string{"access_token", "refresh_token"}},
}

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
			continue 
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
