package config

import (
	"fmt"

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

	if err := SeedPermissions(db); err != nil {
		return nil, fmt.Errorf("seed permissions failed: %w", err)
	}

	return db, nil
}
