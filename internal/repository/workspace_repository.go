package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type workspaceRepository struct {
	db *gorm.DB
}

func NewWorkspaceRepository(db *gorm.DB) domain.WorkspaceRepository {
	return &workspaceRepository{db: db}
}

func (r *workspaceRepository) Create(ctx context.Context, workspace *domain.Workspace, ownerID uint) error {
	m := workspaceFromDomain(*workspace)
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var activeUsers []uint
		if err := tx.Raw(
			"SELECT id FROM users WHERE id = ? AND password_hash <> '!' FOR UPDATE", ownerID,
		).Scan(&activeUsers).Error; err != nil {
			return err
		}
		if len(activeUsers) == 0 {
			return domain.ErrNotFound
		}
		if err := tx.Create(&m).Error; err != nil {
			return err
		}

		var ownerRoleID uint

		for _, def := range domain.DefaultRoles() {
			role := &persistence.Role{WorkspaceID: m.ID, Name: def.Name}
			if err := tx.Create(role).Error; err != nil {
				return fmt.Errorf("create role %q: %w", def.Name, err)
			}

			for _, code := range def.PermissionCodes {
				var perm persistence.Permission
				if err := tx.Where("code = ?", code).First(&perm).Error; err != nil {
					return fmt.Errorf("permission %q not seeded (run database migrations first): %w", code, err)
				}
				if err := tx.Create(&persistence.RolePermission{RoleID: role.ID, PermissionID: perm.ID}).Error; err != nil {
					return fmt.Errorf("attach permission %q to role %q: %w", code, def.Name, err)
				}
			}

			if def.Name == "Owner" {
				ownerRoleID = role.ID
			}
		}

		member := &persistence.Member{WorkspaceID: m.ID, UserID: ownerID, RoleID: ownerRoleID}
		return tx.Create(member).Error
	})
	if err != nil {
		return mapErr(err)
	}
	*workspace = workspaceToDomain(m)
	return nil
}

func (r *workspaceRepository) GetByID(ctx context.Context, id uint) (*domain.Workspace, error) {
	var m persistence.Workspace
	if err := database.DBFromContext(ctx, r.db).First(&m, id).Error; err != nil {
		return nil, mapErr(err)
	}
	workspace := workspaceToDomain(m)
	return &workspace, nil
}

func (r *workspaceRepository) GetAllForUser(ctx context.Context, userID uint) ([]domain.Workspace, error) {
	var rows []persistence.Workspace
	err := database.DBFromContext(ctx, r.db).
		Joins("JOIN workspace_members ON workspace_members.workspace_id = workspaces.id AND workspace_members.deleted_at IS NULL").
		Where("workspace_members.user_id = ?", userID).
		Find(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, workspaceToDomain), nil
}

func (r *workspaceRepository) ListMembers(ctx context.Context, workspaceID uint) ([]domain.MemberDetail, error) {
	var members []domain.MemberDetail
	err := database.DBFromContext(ctx, r.db).Table("workspace_members").
		Select(`workspace_members.user_id AS user_id, users.username, users.first_name, users.last_name,
			users.avatar_url, workspace_members.role_id, roles.name AS role, workspace_members.created_at AS joined_at`).
		Joins("JOIN users ON users.id = workspace_members.user_id").
		Joins("JOIN roles ON roles.id = workspace_members.role_id AND roles.deleted_at IS NULL").
		Where("workspace_members.workspace_id = ? AND workspace_members.deleted_at IS NULL", workspaceID).
		Order("workspace_members.created_at ASC, workspace_members.id ASC").
		Scan(&members).Error
	return members, mapErr(err)
}

func (r *workspaceRepository) GetMember(ctx context.Context, workspaceID, userID uint) (*domain.Member, error) {
	var m persistence.Member
	err := database.DBFromContext(ctx, r.db).Preload("Role").
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		First(&m).Error
	if err != nil {
		return nil, mapErr(err)
	}
	member := memberToDomain(m)
	return &member, nil
}

func (r *workspaceRepository) GetRoleByName(ctx context.Context, workspaceID uint, name string) (*domain.Role, error) {
	var m persistence.Role
	if err := database.DBFromContext(ctx, r.db).Where("workspace_id = ? AND name = ?", workspaceID, name).First(&m).Error; err != nil {
		return nil, mapErr(err)
	}
	role := roleToDomain(m)
	return &role, nil
}

func (r *workspaceRepository) AddMember(ctx context.Context, member *domain.Member) error {
	m := memberFromDomain(*member)
	err := database.DBFromContext(ctx, r.db).Omit("Role").Create(&m).Error
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrAlreadyMember
		}
		return mapErr(err)
	}
	member.Model = modelToDomain(m.Model)
	return nil
}

func (r *workspaceRepository) UpdateMemberRole(ctx context.Context, workspaceID, userID, roleID uint) error {
	res := database.DBFromContext(ctx, r.db).Model(&persistence.Member{}).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Update("role_id", roleID)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

func (r *workspaceRepository) RemoveMember(ctx context.Context, workspaceID, userID uint) error {
	res := database.DBFromContext(ctx, r.db).Where("workspace_id = ? AND user_id = ?", workspaceID, userID).Delete(&persistence.Member{})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

func (r *workspaceRepository) IsContact(ctx context.Context, userID, otherID uint) (bool, error) {
	var count int64
	err := database.DBFromContext(ctx, r.db).Table("user_contacts").
		Where("status = ? AND ((user_id = ? AND contact_id = ?) OR (user_id = ? AND contact_id = ?))",
			"accepted", userID, otherID, otherID, userID).
		Count(&count).Error
	return count > 0, mapErr(err)
}

func (r *workspaceRepository) Update(ctx context.Context, workspace *domain.Workspace) error {
	m := workspaceFromDomain(*workspace)
	if err := database.DBFromContext(ctx, r.db).Model(&m).Select("Name", "Description").Updates(&m).Error; err != nil {
		return mapErr(err)
	}
	workspace.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *workspaceRepository) Delete(ctx context.Context, id uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&persistence.Workspace{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("workspace_id = ?", id).Delete(&persistence.Member{}).Error; err != nil {
			return err
		}
		var spaceIDs []uint
		if err := tx.Raw(
			"SELECT id FROM spaces WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY id FOR UPDATE", id,
		).Scan(&spaceIDs).Error; err != nil {
			return err
		}
		return softDeleteSpaceTrees(tx, spaceIDs)
	}))
}
