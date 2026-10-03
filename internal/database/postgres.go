package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"rabbit-hole-server/db/migrations"

	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	migratedb "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func ConnectDB(dsn, environment string, maxIdleConnections, maxOpenConnections int, connectionLifetime time.Duration) (*gorm.DB, error) {
	db, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{
		Logger: gormLogger(environment),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database connection: %w", err)
	}
	configurePool(sqlDB, maxIdleConnections, maxOpenConnections, connectionLifetime)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

func configurePool(db *sql.DB, maxIdleConnections, maxOpenConnections int, connectionLifetime time.Duration) {
	db.SetMaxIdleConns(maxIdleConnections)
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetConnMaxLifetime(connectionLifetime)
}

func RunMigrations(dsn, action string, steps, version int) (uint, bool, error) {
	if err := migrateWithDedicatedConnection(dsn, action, steps, version); err != nil {
		return 0, false, err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, false, fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return 0, false, fmt.Errorf("ping migration database: %w", err)
	}
	driver, err := migratedb.WithInstance(db, &migratedb.Config{})
	if err != nil {
		return 0, false, fmt.Errorf("create migration driver: %w", err)
	}
	currentVersion, dirty, err := driver.Version()
	if errors.Is(err, migrate.ErrNilVersion) || currentVersion == migratedatabase.NilVersion {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read migration version: %w", err)
	}
	return uint(currentVersion), dirty, nil
}

func migrateWithDedicatedConnection(dsn, action string, steps, version int) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping migration database: %w", err)
	}
	_, _, runErr := runMigrations(db, action, steps, version)
	return runErr
}

func runMigrations(db *sql.DB, action string, steps, version int) (uint, bool, error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		_ = db.Close()
		return 0, false, fmt.Errorf("open migration source: %w", err)
	}

	driver, err := migratedb.WithInstance(db, &migratedb.Config{})
	if err != nil {
		_ = db.Close()
		return 0, false, fmt.Errorf("create migration driver: %w", err)
	}

	migration, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		_ = db.Close()
		return 0, false, fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		_, _ = migration.Close()
	}()

	switch action {
	case "up":
		err = migration.Up()
	case "down":
		if steps > 0 {
			err = migration.Steps(-steps)
		} else {
			err = migration.Down()
		}
	case "force":
		err = migration.Force(version)
	case "version":
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("unsupported migration action %q", action)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, false, err
	}
	return 0, false, nil
}

func gormLogger(environment string) gormlogger.Interface {
	level := gormlogger.Info
	if environment == "production" {
		level = gormlogger.Error
	} else if environment == "staging" {
		level = gormlogger.Warn
	}

	return gormlogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  level,
			IgnoreRecordNotFoundError: true,
			Colorful:                  environment != "production",
		},
	)
}
