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
					return fmt.Errorf("permission %q not seeded (run database migrations first): %w", code, err)
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

func (r *workspaceRepository) ListMembers(workspaceID uint) ([]domain.MemberDetail, error) {
	var members []domain.MemberDetail
	err := r.db.Table("workspace_members").
		Select(`workspace_members.user_id AS user_id, users.username, users.first_name, users.last_name,
			users.avatar_url, workspace_members.role_id, roles.name AS role, workspace_members.created_at AS joined_at`).
		Joins("JOIN users ON users.id = workspace_members.user_id").
		Joins("JOIN roles ON roles.id = workspace_members.role_id AND roles.deleted_at IS NULL").
		Where("workspace_members.workspace_id = ? AND workspace_members.deleted_at IS NULL", workspaceID).
		Order("workspace_members.created_at ASC, workspace_members.id ASC").
		Scan(&members).Error
	return members, err
}

func (r *workspaceRepository) GetMember(workspaceID, userID uint) (*domain.Member, error) {
	var member domain.Member
	err := r.db.Preload("Role").
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		First(&member).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *workspaceRepository) GetRoleByName(workspaceID uint, name string) (*domain.Role, error) {
	var role domain.Role
	if err := r.db.Where("workspace_id = ? AND name = ?", workspaceID, name).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *workspaceRepository) AddMember(member *domain.Member) error {
	err := r.db.Create(member).Error
	if err != nil && isUniqueViolation(err) {
		return domain.ErrAlreadyMember
	}
	return err
}

func (r *workspaceRepository) UpdateMemberRole(workspaceID, userID, roleID uint) error {
	res := r.db.Model(&domain.Member{}).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Update("role_id", roleID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

func (r *workspaceRepository) RemoveMember(workspaceID, userID uint) error {
	res := r.db.Where("workspace_id = ? AND user_id = ?", workspaceID, userID).Delete(&domain.Member{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

func (r *workspaceRepository) IsContact(userID, otherID uint) (bool, error) {
	var count int64
	err := r.db.Table("user_contacts").
		Where("user_id = ? AND contact_id = ?", userID, otherID).
		Count(&count).Error
	return count > 0, err
}
