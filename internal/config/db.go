package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"rabbit-hole-server/internal/domain"
)

func InitDB(cfg *Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DB.URL), &gorm.Config{
		Logger: newGormLogger(cfg.Environment),
	})
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

func newGormLogger(env string) gormlogger.Interface {
	level := gormlogger.Silent

	switch env {
	case "production":
		level = gormlogger.Error
	case "staging":
		level = gormlogger.Warn
	default:
		level = gormlogger.Info
	}

	return gormlogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  level,
			IgnoreRecordNotFoundError: true,
			Colorful:                  env != "production",
		},
	)
}
