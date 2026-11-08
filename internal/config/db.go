package config

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
)

func (c DBConfig) Options(environment string) database.Options {
	return database.Options{
		Environment:        environment,
		MaxIdleConnections: c.MaxIdleConnections,
		MaxOpenConnections: c.MaxOpenConnections,
		ConnMaxLifetime:    c.ConnMaxLifetime,
		ConnMaxIdleTime:    c.ConnMaxIdleTime,
		StatementTimeout:   c.StatementTimeout,
		LockTimeout:        c.LockTimeout,
		IdleInTxnTimeout:   c.IdleInTxTimeout,
		SlowQueryThreshold: c.SlowQueryThreshold,
	}
}

func InitDB(cfg *Config) (*gorm.DB, error) {
	return database.ConnectDB(cfg.DB.URL, cfg.DB.Options(cfg.Environment))
}
