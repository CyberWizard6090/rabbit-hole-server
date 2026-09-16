package config

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

func InitDB(cfg *Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DB.URL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	err = db.AutoMigrate(
		&domain.User{},
		&domain.UserSession{},
		&domain.Workspace{},
		&domain.Space{},
		&domain.Folder{},
		&domain.List{},
		&domain.TaskStatus{},
		&domain.Tag{},
		&domain.Task{},
		&domain.Role{},
		&domain.Permission{},
		&domain.RolePermission{},
		&domain.Member{},
	)
	if err != nil {
		return nil, fmt.Errorf("auto-migration failed: %w", err)
	}
	if err := SeedPermissions(db); err != nil {
		return nil, fmt.Errorf("seed permissions failed: %w", err)
	}

	return db, nil
}
