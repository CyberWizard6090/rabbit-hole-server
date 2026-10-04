package config

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
)

func InitDB(cfg *Config) (*gorm.DB, error) {
	db, err := database.ConnectDB(
		cfg.DB.URL,
		cfg.Environment,
		cfg.DB.MaxIdleConnections,
		cfg.DB.MaxOpenConnections,
		cfg.DB.ConnMaxLifetime,
	)
	if err != nil {
		return nil, err
	}

	return db, nil
}
