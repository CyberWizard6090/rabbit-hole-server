package repository

import (
	"fmt"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type workspaceRepository struct {
	db *gorm.DB
}

func NewWorkspaceRepository(db *gorm.DB) domain.WorkspaceRepository {
	return &workspaceRepository{db: db}
}

func (r *workspaceRepository) Create(workspace *domain.Workspace, ownerID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(workspace).Error; err != nil {
			return err
		}

		var ownerRoleID uint

		for _, def := range domain.DefaultRoles() {
			role := &domain.Role{WorkspaceID: workspace.ID, Name: def.Name}
			if err := tx.Create(role).Error; err != nil {
				return fmt.Errorf("create role %q: %w", def.Name, err)
			}

			for _, code := range def.PermissionCodes {
				var perm domain.Permission
				if err := tx.Where("code = ?", code).First(&perm).Error; err != nil {
					return fmt.Errorf("permission %q not seeded (run config.SeedPermissions first): %w", code, err)
				}
				if err := tx.Create(&domain.RolePermission{RoleID: role.ID, PermissionID: perm.ID}).Error; err != nil {
					return fmt.Errorf("attach permission %q to role %q: %w", code, def.Name, err)
				}
			}

			if def.Name == "Owner" {
				ownerRoleID = role.ID
			}
		}

		member := &domain.Member{WorkspaceID: workspace.ID, UserID: ownerID, RoleID: ownerRoleID}
		return tx.Create(member).Error
	})
}

func (r *workspaceRepository) GetByID(id uint) (*domain.Workspace, error) {
	var workspace domain.Workspace
	if err := r.db.First(&workspace, id).Error; err != nil {
		return nil, err
	}
	return &workspace, nil
}

func (r *workspaceRepository) GetAllForUser(userID uint) ([]domain.Workspace, error) {
	var workspaces []domain.Workspace
	err := r.db.
		Joins("JOIN workspace_members ON workspace_members.workspace_id = workspaces.id").
		Where("workspace_members.user_id = ?", userID).
		Find(&workspaces).Error
	return workspaces, err
}
