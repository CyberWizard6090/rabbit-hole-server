package service

import "gorm.io/gorm"

type PermissionChecker interface {
	HasWorkspacePermission(userID, workspaceID uint, code string) (bool, error)
	HasSpacePermission(userID, spaceID uint, code string) (bool, error)
	HasFolderPermission(userID, folderID uint, code string) (bool, error)
	HasListPermission(userID, listID uint, code string) (bool, error)
	HasTaskPermission(userID, taskID uint, code string) (bool, error)
}

type permissionChecker struct {
	db *gorm.DB
}

const spacesJoin = "JOIN spaces ON spaces.workspace_id = workspace_members.workspace_id"

func NewPermissionChecker(db *gorm.DB) PermissionChecker {
	return &permissionChecker{db: db}
}

func (p *permissionChecker) baseQuery(userID uint, code string) *gorm.DB {
	return p.db.Table("workspace_members").
		Joins("JOIN roles ON roles.id = workspace_members.role_id").
		Joins("JOIN role_permissions ON role_permissions.role_id = roles.id").
		Joins("JOIN permissions ON permissions.id = role_permissions.permission_id").
		Where("workspace_members.user_id = ? AND permissions.code = ?", userID, code)
}

func (p *permissionChecker) HasWorkspacePermission(userID, workspaceID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(userID, code).
		Where("workspace_members.workspace_id = ?", workspaceID).
		Count(&count).Error
	return count > 0, err
}

func (p *permissionChecker) HasSpacePermission(userID, spaceID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(userID, code).
		Joins(spacesJoin).
		Where("spaces.id = ?", spaceID).
		Count(&count).Error
	return count > 0, err
}

func (p *permissionChecker) HasFolderPermission(userID, folderID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(userID, code).
		Joins(spacesJoin).
		Joins("JOIN folders ON folders.space_id = spaces.id").
		Where("folders.id = ?", folderID).
		Count(&count).Error
	return count > 0, err
}

func (p *permissionChecker) HasListPermission(userID, listID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(userID, code).
		Joins(spacesJoin).
		Joins("JOIN lists ON lists.space_id = spaces.id").
		Where("lists.id = ?", listID).
		Count(&count).Error
	return count > 0, err
}

func (p *permissionChecker) HasTaskPermission(userID, taskID uint, code string) (bool, error) {
	var count int64
	err := p.baseQuery(userID, code).
		Joins(spacesJoin).
		Joins("JOIN tasks ON tasks.space_id = spaces.id").
		Where("tasks.id = ?", taskID).
		Count(&count).Error
	return count > 0, err
}
