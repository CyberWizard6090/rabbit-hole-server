package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

const accessSyncLockKey = 7311902

type accessSyncRepository struct {
	db *gorm.DB
}

func NewAccessSyncRepository(db *gorm.DB) domain.AccessSyncRepository {
	return &accessSyncRepository{db: db}
}

func (r *accessSyncRepository) SyncDefaultRoles(ctx context.Context) (domain.AccessSyncResult, error) {
	var result domain.AccessSyncResult
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(?)`, accessSyncLockKey).Error; err != nil {
			return err
		}
		res := tx.Exec(
			`INSERT INTO permissions (code) SELECT unnest(?::text[]) ON CONFLICT (code) WHERE deleted_at IS NULL DO NOTHING`,
			pgTextArray(domain.AllPermissions()),
		)
		if res.Error != nil {
			return res.Error
		}
		result.Permissions = res.RowsAffected
		for _, def := range domain.DefaultRoles() {
			codes := pgTextArray(def.PermissionCodes)
			created := tx.Exec(`
				INSERT INTO roles (workspace_id, name)
				SELECT w.id, ? FROM workspaces w
				WHERE w.deleted_at IS NULL AND NOT EXISTS (
					SELECT 1 FROM roles r WHERE r.workspace_id = w.id AND r.name = ? AND r.deleted_at IS NULL)`,
				def.Name, def.Name)
			if created.Error != nil {
				return created.Error
			}
			result.Roles += created.RowsAffected
			added := tx.Exec(`
				INSERT INTO role_permissions (role_id, permission_id)
				SELECT r.id, p.id FROM roles r
				JOIN permissions p ON p.code = ANY(?::text[]) AND p.deleted_at IS NULL
				WHERE r.name = ? AND r.deleted_at IS NULL AND NOT EXISTS (
					SELECT 1 FROM role_permissions rp
					WHERE rp.role_id = r.id AND rp.permission_id = p.id AND rp.deleted_at IS NULL)`,
				codes, def.Name)
			if added.Error != nil {
				return added.Error
			}
			result.Granted += added.RowsAffected
			removed := tx.Exec(`
				DELETE FROM role_permissions rp USING roles r, permissions p
				WHERE rp.role_id = r.id AND rp.permission_id = p.id
				  AND r.name = ? AND r.deleted_at IS NULL AND rp.deleted_at IS NULL
				  AND NOT (p.code = ANY(?::text[]))`,
				def.Name, codes)
			if removed.Error != nil {
				return removed.Error
			}
			result.Revoked += removed.RowsAffected
		}
		return nil
	})
	return result, mapErr(err)
}

func pgTextArray(values []string) string {
	out := "{"
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += `"` + v + `"`
	}
	return out + "}"
}
