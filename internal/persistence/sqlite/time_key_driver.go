package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

const (
	openAgentXSQLiteDriver  = "openagentx_sqlite3"
	sqliteTimeKeyFunction   = "openagentx_rfc3339nano_key"
	sqliteTimeKeyTailLayout = "-01-02T15:04:05.000000000Z"
)

func init() {
	sql.Register(openAgentXSQLiteDriver, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			if err := conn.RegisterFunc(sqliteTimeKeyFunction, sqliteRFC3339NanoKey, true); err != nil {
				return fmt.Errorf("register %s: %w", sqliteTimeKeyFunction, err)
			}
			return nil
		},
	})
}

func sqliteRFC3339NanoKey(value string) (string, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", fmt.Errorf("parse RFC3339Nano timestamp: %w", err)
	}
	utc := parsed.UTC()
	return fmt.Sprintf("%05d%s", utc.Year()+1, utc.Format(sqliteTimeKeyTailLayout)), nil
}
