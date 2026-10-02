package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
)

var externalShapeOnce sync.Once
var externalShapeErr error
var externalShape []schemaObject

func validateExternalObjects(ctx context.Context, q schemaQueryer) error {
	externalShapeOnce.Do(func() {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			externalShapeErr = err
			return
		}
		defer db.Close()
		if _, err = db.Exec(targetSchema + "\n" + externalSessionSchema); err != nil {
			externalShapeErr = err
			return
		}
		rows, err := db.Query("SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL")
		if err != nil {
			externalShapeErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var o schemaObject
			if err = rows.Scan(&o.Kind, &o.Name, &o.SQL); err != nil {
				externalShapeErr = err
				return
			}
			if strings.HasPrefix(o.Name, "external_") || strings.HasPrefix(o.Name, "uq_external_") || strings.HasPrefix(o.Name, "idx_external_") || strings.HasSuffix(o.Name, "_external_guard") {
				externalShape = append(externalShape, o)
			}
		}
		externalShapeErr = rows.Err()
	})
	if externalShapeErr != nil {
		return externalShapeErr
	}
	for _, o := range externalShape {
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type=? AND name=?", o.Kind, o.Name).Scan(&actual); err != nil {
			return fmt.Errorf("%w: missing external object %s", ErrIncompleteSchema, o.Name)
		}
		if compactSQL(actual) != compactSQL(o.SQL) {
			return fmt.Errorf("%w: invalid external object %s", ErrIncompleteSchema, o.Name)
		}
	}
	return nil
}
