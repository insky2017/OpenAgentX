package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
)

var externalShapeOnce [2]sync.Once
var externalShapeErr [2]error
var externalShape [2][]schemaObject

func validateExternalObjects(ctx context.Context, q schemaQueryer) error {
	return validateExternalSchema(ctx, q, true)
}
func validateExternalSchema(ctx context.Context, q schemaQueryer, current bool) error {
	index := 0
	schema := targetSchema + "\n" + externalSessionSchema
	if current {
		index = 1
		schema += "\n" + externalRolesSchema
	}
	externalShapeOnce[index].Do(func() {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			externalShapeErr[index] = err
			return
		}
		defer db.Close()
		if _, err = db.Exec(schema); err != nil {
			externalShapeErr[index] = err
			return
		}
		rows, err := db.Query("SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL")
		if err != nil {
			externalShapeErr[index] = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var o schemaObject
			if err = rows.Scan(&o.Kind, &o.Name, &o.SQL); err != nil {
				externalShapeErr[index] = err
				return
			}
			if strings.HasPrefix(o.Name, "external_") || strings.HasPrefix(o.Name, "uq_external_") || strings.HasPrefix(o.Name, "idx_external_") || strings.HasSuffix(o.Name, "_external_guard") {
				externalShape[index] = append(externalShape[index], o)
			}
		}
		externalShapeErr[index] = rows.Err()
	})
	if externalShapeErr[index] != nil {
		return externalShapeErr[index]
	}
	for _, o := range externalShape[index] {
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
