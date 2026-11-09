package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"rabbit-hole-server/db/migrations"

	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	migratedb "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const ActionDownAll = "down-all"

var ErrDownNeedsSteps = errors.New("down needs a positive number of steps; roll back everything only with an explicit full rollback")

type Options struct {
	Environment string

	MaxIdleConnections int
	MaxOpenConnections int
	ConnMaxLifetime    time.Duration
	ConnMaxIdleTime    time.Duration

	StatementTimeout   time.Duration
	LockTimeout        time.Duration
	IdleInTxnTimeout   time.Duration
	SlowQueryThreshold time.Duration
	Logger             *slog.Logger
}

func ConnectDB(dsn string, opts Options) (*gorm.DB, error) {
	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	applySessionTimeouts(connConfig, opts)

	sqlDB := stdlib.OpenDB(*connConfig)
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: gormLogger(opts),
		NowFunc: func() time.Time {
			return time.Now().Truncate(time.Microsecond)
		},
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	configurePool(sqlDB, opts)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func applySessionTimeouts(connConfig *pgx.ConnConfig, opts Options) {
	if connConfig.RuntimeParams == nil {
		connConfig.RuntimeParams = map[string]string{}
	}
	for name, value := range map[string]time.Duration{
		"statement_timeout":                   opts.StatementTimeout,
		"lock_timeout":                        opts.LockTimeout,
		"idle_in_transaction_session_timeout": opts.IdleInTxnTimeout,
	} {
		if _, set := connConfig.RuntimeParams[name]; set || value <= 0 {
			continue
		}
		connConfig.RuntimeParams[name] = strconv.FormatInt(value.Milliseconds(), 10)
	}
}

func configurePool(db *sql.DB, opts Options) {
	db.SetMaxIdleConns(opts.MaxIdleConnections)
	db.SetMaxOpenConns(opts.MaxOpenConnections)
	db.SetConnMaxLifetime(opts.ConnMaxLifetime)
	if opts.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(opts.ConnMaxIdleTime)
	}
}

func RunMigrations(dsn, action string, steps, version int) (uint, bool, error) {
	if err := migrateWithDedicatedConnection(dsn, action, steps, version); err != nil {
		return 0, false, err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, false, fmt.Errorf("open migration database: %w", err)
	}
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()
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
		if steps <= 0 {
			return 0, false, ErrDownNeedsSteps
		}
		err = migration.Steps(-steps)
	case ActionDownAll:
		err = migration.Down()
	case "force":
		err = migration.Force(version)
	case "version":
		// handled below
	default:
		return 0, false, fmt.Errorf("unsupported migration action %q", action)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, false, err
	}

	current, dirty, verr := migration.Version()
	if errors.Is(verr, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if verr != nil {
		return 0, false, fmt.Errorf("read migration version: %w", verr)
	}
	return current, dirty, nil
}

func gormLogger(opts Options) gormlogger.Interface {
	level := gormlogger.Info
	switch opts.Environment {
	case "production":
		level = gormlogger.Error
	case "staging", "test":
		level = gormlogger.Warn
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return gormlogger.NewSlogLogger(logger, gormlogger.Config{
		SlowThreshold:             opts.SlowQueryThreshold,
		LogLevel:                  level,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
	})
}
