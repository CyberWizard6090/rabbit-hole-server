package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type permissionChecker struct {
	db *gorm.DB
}

const spacesJoin = "JOIN spaces ON spaces.workspace_id = workspace_members.workspace_id"
const activeSpacesJoinFilter = " AND spaces.deleted_at IS NULL"

func NewPermissionChecker(db *gorm.DB) domain.PermissionChecker {
	return &permissionChecker{db: db}
}

func (p *permissionChecker) baseQuery(ctx context.Context, userID uint, code string) *gorm.DB {
	return p.db.WithContext(ctx).Table("workspace_members").
		Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id AND workspaces.deleted_at IS NULL").
		Joins("JOIN roles ON roles.id = workspace_members.role_id AND roles.deleted_at IS NULL").
		Joins("JOIN role_permissions ON role_permissions.role_id = roles.id AND role_permissions.deleted_at IS NULL").
		Joins("JOIN permissions ON permissions.id = role_permissions.permission_id AND permissions.deleted_at IS NULL").
		Where("workspace_members.user_id = ? AND workspace_members.deleted_at IS NULL AND permissions.code = ?", userID, code)
}

func (p *permissionChecker) HasWorkspacePermission(ctx context.Context, userID, workspaceID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(ctx, userID, code).
		Where("workspace_members.workspace_id = ?", workspaceID).
		Count(&count).Error
	return count > 0, err
}

func (p *permissionChecker) HasSpacePermission(ctx context.Context, userID, spaceID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(ctx, userID, code).
		Joins(spacesJoin+activeSpacesJoinFilter).
		Where("spaces.id = ?", spaceID).
		Count(&count).Error
	return count > 0, err
}

func (p *permissionChecker) HasFolderPermission(ctx context.Context, userID, folderID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(ctx, userID, code).
		Joins(spacesJoin+activeSpacesJoinFilter).
		Joins("JOIN folders ON folders.space_id = spaces.id AND folders.deleted_at IS NULL").
		Where("folders.id = ?", folderID).
		Count(&count).Error
	return count > 0, err
}

func (p *permissionChecker) HasListPermission(ctx context.Context, userID, listID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(ctx, userID, code).
		Joins(spacesJoin+activeSpacesJoinFilter).
		Joins("JOIN lists ON lists.space_id = spaces.id AND lists.deleted_at IS NULL").
		Where("lists.id = ?", listID).
		Count(&count).Error
	return count > 0, err
}

func (p *permissionChecker) HasTaskPermission(ctx context.Context, userID, taskID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(ctx, userID, code).
		Joins(spacesJoin+activeSpacesJoinFilter).
		Joins("JOIN tasks ON tasks.space_id = spaces.id AND tasks.deleted_at IS NULL").
		Where("tasks.id = ?", taskID).
		Count(&count).Error
	return count > 0, err
}
