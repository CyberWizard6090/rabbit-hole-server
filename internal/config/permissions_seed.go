package config

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

func SeedPermissions(db *gorm.DB) error {
	seen := make(map[string]bool)

	for _, role := range domain.DefaultRoles() {
		for _, code := range role.PermissionCodes {
			if seen[code] {
				continue
			}
			seen[code] = true

			perm := domain.Permission{Code: code}
			if err := db.Where("code = ?", code).FirstOrCreate(&perm).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
