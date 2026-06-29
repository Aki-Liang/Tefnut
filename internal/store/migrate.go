package store

import (
	"database/sql"
	"fmt"
)

// ensureColumn adds `column ddl` to `table` if it does not already exist.
// table/column/ddl are internal constants, never user input.
func ensureColumn(db *sql.DB, table, column, ddl string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("store: table_info %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("store: scan table_info: %w", err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + ddl); err != nil {
		return fmt.Errorf("store: add column %s.%s: %w", table, column, err)
	}
	return nil
}
