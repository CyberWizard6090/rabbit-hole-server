package database

import (
	"context"
	"fmt"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"

	"rabbit-hole-server/db/migrations"
)

type SchemaError struct {
	Current  uint
	Expected uint
	Dirty    bool
	Missing  bool
}

func (e *SchemaError) Error() string {
	switch {
	case e.Missing:
		return fmt.Sprintf("the database has no migration history: run `migrate -action up` (expected schema version %d)", e.Expected)
	case e.Dirty:
		return fmt.Sprintf("the database schema is dirty at version %d, a migration failed half way: fix it and run `migrate -action force -version N`, then `migrate -action up` (expected schema version %d)", e.Current, e.Expected)
	default:
		return fmt.Sprintf("the database schema is at version %d but this build expects version %d: run `migrate -action up` before starting the server", e.Current, e.Expected)
	}
}

func ExpectedSchemaVersion() (uint, error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("open migration source: %w", err)
	}
	defer func() { _ = source.Close() }()
	version, err := source.First()
	if err != nil {
		return 0, fmt.Errorf("read the first migration: %w", err)
	}
	for {
		next, err := source.Next(version)
		if err != nil {
			return version, nil
		}
		version = next
	}
}

func CheckSchema(ctx context.Context, db *gorm.DB) error {
	expected, err := ExpectedSchemaVersion()
	if err != nil {
		return err
	}
	var rows []struct {
		Version int64
		Dirty   bool
	}
	if err := db.WithContext(ctx).Raw("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&rows).Error; err != nil {
		var exists bool
		probe := db.WithContext(ctx).Raw("SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists)
		if probe.Error == nil && !exists {
			return &SchemaError{Expected: expected, Missing: true}
		}
		return fmt.Errorf("read the schema version: %w", err)
	}
	if len(rows) == 0 || rows[0].Version < 0 {
		return &SchemaError{Expected: expected, Missing: true}
	}
	current := uint(rows[0].Version) //nolint:gosec // checked to be non-negative above
	if rows[0].Dirty || current < expected {
		return &SchemaError{Current: current, Expected: expected, Dirty: rows[0].Dirty}
	}
	return nil
}
