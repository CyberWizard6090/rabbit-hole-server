package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type taskTemplateRepository struct {
	db *gorm.DB
}

func NewTaskTemplateRepository(db *gorm.DB) domain.TaskTemplateRepository {
	return &taskTemplateRepository{db: db}
}

func (r *taskTemplateRepository) Create(ctx context.Context, template *domain.TaskTemplate, configuration json.RawMessage) error {
	now := time.Now().UTC()
	row := persistence.TaskTemplate{
		WorkspaceID: template.WorkspaceID, Name: template.Name, Description: template.Description,
		Status: "active", CreatedBy: template.CreatedBy, CreatedAt: now, UpdatedAt: now,
	}
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		version := persistence.TaskTemplateVersion{
			TemplateID: row.ID, Version: 1, Configuration: configuration, CreatedAt: now,
		}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		template.ID, template.CreatedAt, template.UpdatedAt = row.ID, now, now
		return nil
	})
	return mapErr(err)
}

func (r *taskTemplateRepository) List(ctx context.Context, workspaceID uint) ([]domain.TaskTemplate, error) {
	var rows []persistence.TaskTemplate
	if err := database.DBFromContext(ctx, r.db).Where("workspace_id = ?", workspaceID).
		Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	result := make([]domain.TaskTemplate, len(rows))
	for i, row := range rows {
		result[i] = templateToDomain(row)
	}
	return result, nil
}

func (r *taskTemplateRepository) Get(ctx context.Context, templateID uint) (*domain.TaskTemplate, error) {
	var row persistence.TaskTemplate
	if err := database.DBFromContext(ctx, r.db).First(&row, "id = ?", templateID).Error; err != nil {
		return nil, mapErr(err)
	}
	template := templateToDomain(row)
	versions, err := r.ListVersions(ctx, templateID)
	if err != nil {
		return nil, err
	}
	template.Versions = versions
	return &template, nil
}

func (r *taskTemplateRepository) Update(ctx context.Context, template *domain.TaskTemplate) error {
	result := database.DBFromContext(ctx, r.db).Model(&persistence.TaskTemplate{}).
		Where("id = ? AND status = 'active'", template.ID).
		Updates(map[string]any{"name": template.Name, "description": template.Description})
	if result.Error != nil {
		return mapErr(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrTaskTemplateNotFound
	}
	var updatedAt time.Time
	if err := database.DBFromContext(ctx, r.db).Model(&persistence.TaskTemplate{}).
		Select("updated_at").Where("id = ?", template.ID).Scan(&updatedAt).Error; err != nil {
		return mapErr(err)
	}
	template.UpdatedAt = updatedAt
	return nil
}

func (r *taskTemplateRepository) Archive(ctx context.Context, templateID uint) error {
	result := database.DBFromContext(ctx, r.db).Model(&persistence.TaskTemplate{}).
		Where("id = ?", templateID).Update("status", "archived")
	if result.Error != nil {
		return mapErr(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrTaskTemplateNotFound
	}
	return nil
}

func (r *taskTemplateRepository) CreateVersion(ctx context.Context, templateID uint, configuration json.RawMessage) (*domain.TaskTemplateVersion, error) {
	var result persistence.TaskTemplateVersion
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var template persistence.TaskTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&template, "id = ? AND status = 'active'", templateID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrTaskTemplateNotFound
			}
			return err
		}
		var latest int
		if err := tx.Model(&persistence.TaskTemplateVersion{}).
			Where("template_id = ?", templateID).Select("COALESCE(MAX(version), 0)").Scan(&latest).Error; err != nil {
			return err
		}
		result = persistence.TaskTemplateVersion{
			TemplateID: templateID, Version: latest + 1,
			Configuration: configuration, CreatedAt: time.Now().UTC(),
		}
		return tx.Create(&result).Error
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return templateVersionToDomain(result), nil
}

func (r *taskTemplateRepository) ListVersions(ctx context.Context, templateID uint) ([]domain.TaskTemplateVersion, error) {
	var rows []persistence.TaskTemplateVersion
	if err := database.DBFromContext(ctx, r.db).Where("template_id = ?", templateID).
		Order("version ASC").Find(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	result := make([]domain.TaskTemplateVersion, len(rows))
	for i := range rows {
		result[i] = *templateVersionToDomain(rows[i])
	}
	return result, nil
}

func (r *taskTemplateRepository) GetVersion(ctx context.Context, templateID, versionID uint) (*domain.TaskTemplateVersion, error) {
	var row persistence.TaskTemplateVersion
	if err := database.DBFromContext(ctx, r.db).First(&row, "id = ? AND template_id = ?", versionID, templateID).Error; err != nil {
		return nil, mapErr(err)
	}
	return templateVersionToDomain(row), nil
}

func templateToDomain(row persistence.TaskTemplate) domain.TaskTemplate {
	return domain.TaskTemplate{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name, Description: row.Description,
		Status: row.Status, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func templateVersionToDomain(row persistence.TaskTemplateVersion) *domain.TaskTemplateVersion {
	return &domain.TaskTemplateVersion{
		ID: row.ID, TemplateID: row.TemplateID, Version: row.Version,
		Configuration: append(json.RawMessage(nil), row.Configuration...), CreatedAt: row.CreatedAt,
	}
}
