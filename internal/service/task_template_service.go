package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
)

type TaskTemplateService interface {
	Create(ctx context.Context, workspaceID, userID uint, name, description string, config json.RawMessage) (*domain.TaskTemplate, error)
	List(ctx context.Context, workspaceID uint) ([]domain.TaskTemplate, error)
	Get(ctx context.Context, templateID uint) (*domain.TaskTemplate, error)
	Update(ctx context.Context, templateID uint, name, description *string) (*domain.TaskTemplate, error)
	Archive(ctx context.Context, templateID uint) error
	CreateVersion(ctx context.Context, templateID uint, config json.RawMessage) (*domain.TaskTemplateVersion, error)
	ListVersions(ctx context.Context, templateID uint) ([]domain.TaskTemplateVersion, error)
	GetVersion(ctx context.Context, templateID, versionID uint) (*domain.TaskTemplateVersion, error)
}

type taskTemplateService struct {
	repo       domain.TaskTemplateRepository
	fields     CustomFieldService
	spaces     domain.SpaceRepository
	tags       domain.TagRepository
	workspaces domain.WorkspaceRepository
}

func NewTaskTemplateService(
	repo domain.TaskTemplateRepository,
	fields CustomFieldService,
	spaces domain.SpaceRepository,
	tags domain.TagRepository,
	workspaces domain.WorkspaceRepository,
) TaskTemplateService {
	return &taskTemplateService{repo: repo, fields: fields, spaces: spaces, tags: tags, workspaces: workspaces}
}

type TaskTemplateConfiguration struct {
	Task struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Priority    string  `json:"priority"`
		StatusID    *uint   `json:"status_id"`
		AssigneeID  *uint   `json:"assignee_id"`
		DueDate     *string `json:"due_date"`
		DueInDays   *int    `json:"due_in_days"`
	} `json:"task"`
	CustomFields []TaskTemplateCustomField `json:"custom_fields"`
	Tags         []uint                    `json:"tags"`
	Subtasks     []json.RawMessage         `json:"subtasks"`
}

type TaskTemplateCustomField struct {
	FieldID uint            `json:"field_id"`
	Value   json.RawMessage `json:"value"`
}

func NormalizeTaskTemplateConfiguration(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || !json.Valid(raw) || len(raw) > 1<<18 {
		return nil, domain.ErrInvalidTaskTemplate
	}
	var config TaskTemplateConfiguration
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, domain.ErrInvalidTaskTemplate
	}
	if config.Task.Priority == "" {
		config.Task.Priority = "normal"
	}
	if !domain.ValidTaskPriorityName(config.Task.Priority) {
		return nil, domain.ErrInvalidTaskTemplate
	}
	for _, field := range config.CustomFields {
		if field.FieldID == 0 || len(field.Value) == 0 || !json.Valid(field.Value) {
			return nil, domain.ErrInvalidTaskTemplate
		}
	}
	for _, tagID := range config.Tags {
		if tagID == 0 {
			return nil, domain.ErrInvalidTaskTemplate
		}
	}
	if config.Task.StatusID != nil && *config.Task.StatusID == 0 ||
		config.Task.AssigneeID != nil && *config.Task.AssigneeID == 0 {
		return nil, domain.ErrInvalidTaskTemplate
	}
	if _, err := templateDueDate(config, time.Now()); err != nil {
		return nil, err
	}
	if config.Task.Title != nil {
		value := strings.TrimSpace(*config.Task.Title)
		config.Task.Title = &value
	}
	if config.Task.Description != nil {
		value := strings.TrimSpace(*config.Task.Description)
		config.Task.Description = &value
	}
	normalized, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode task template configuration: %w", err)
	}
	return normalized, nil
}

const maxTemplateDueInDays = 3650

func templateDueDate(config TaskTemplateConfiguration, reference time.Time) (*time.Time, error) {
	switch {
	case config.Task.DueDate != nil && config.Task.DueInDays != nil:
		return nil, domain.ErrInvalidTaskTemplate
	case config.Task.DueDate != nil:
		date, err := time.Parse("2006-01-02", *config.Task.DueDate)
		if err != nil {
			return nil, domain.ErrInvalidTaskTemplate
		}
		return &date, nil
	case config.Task.DueInDays != nil:
		days := *config.Task.DueInDays
		if days < 0 || days > maxTemplateDueInDays {
			return nil, domain.ErrInvalidTaskTemplate
		}
		day := reference.UTC()
		date := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, days)
		return &date, nil
	}
	return nil, nil
}

func (s *taskTemplateService) Create(
	ctx context.Context,
	workspaceID, userID uint,
	name, description string,
	config json.RawMessage,
) (*domain.TaskTemplate, error) {
	name = strings.TrimSpace(name)
	if workspaceID == 0 || userID == 0 || name == "" || len(name) > 255 || len(description) > 10000 {
		return nil, domain.ErrInvalidTaskTemplate
	}
	normalized, err := NormalizeTaskTemplateConfiguration(config)
	if err != nil {
		return nil, err
	}
	if err := s.validateConfiguration(ctx, workspaceID, normalized); err != nil {
		return nil, err
	}
	template := &domain.TaskTemplate{
		WorkspaceID: workspaceID, Name: name, Description: strings.TrimSpace(description),
		CreatedBy: userID, Status: "active",
	}
	if err := s.repo.Create(ctx, template, normalized); err != nil {
		return nil, fmt.Errorf("create task template: %w", err)
	}
	return template, nil
}

func (s *taskTemplateService) List(ctx context.Context, workspaceID uint) ([]domain.TaskTemplate, error) {
	return s.repo.List(ctx, workspaceID)
}

func (s *taskTemplateService) Get(ctx context.Context, templateID uint) (*domain.TaskTemplate, error) {
	template, err := s.repo.Get(ctx, templateID)
	if err != nil {
		return nil, err
	}
	return template, nil
}

func (s *taskTemplateService) Update(
	ctx context.Context, templateID uint, name, description *string,
) (*domain.TaskTemplate, error) {
	template, err := s.Get(ctx, templateID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" || len(trimmed) > 255 {
			return nil, domain.ErrInvalidTaskTemplate
		}
		template.Name = trimmed
	}
	if description != nil {
		if len(*description) > 10000 {
			return nil, domain.ErrInvalidTaskTemplate
		}
		template.Description = strings.TrimSpace(*description)
	}
	if err := s.repo.Update(ctx, template); err != nil {
		return nil, err
	}
	return template, nil
}

func (s *taskTemplateService) Archive(ctx context.Context, templateID uint) error {
	if _, err := s.Get(ctx, templateID); err != nil {
		return err
	}
	return s.repo.Archive(ctx, templateID)
}

func (s *taskTemplateService) CreateVersion(
	ctx context.Context, templateID uint, config json.RawMessage,
) (*domain.TaskTemplateVersion, error) {
	template, err := s.Get(ctx, templateID)
	if err != nil {
		return nil, err
	}
	normalized, err := NormalizeTaskTemplateConfiguration(config)
	if err != nil {
		return nil, err
	}
	if err := s.validateConfiguration(ctx, template.WorkspaceID, normalized); err != nil {
		return nil, err
	}
	return s.repo.CreateVersion(ctx, templateID, normalized)
}

func (s *taskTemplateService) ListVersions(ctx context.Context, templateID uint) ([]domain.TaskTemplateVersion, error) {
	if _, err := s.Get(ctx, templateID); err != nil {
		return nil, err
	}
	return s.repo.ListVersions(ctx, templateID)
}

func (s *taskTemplateService) GetVersion(ctx context.Context, templateID, versionID uint) (*domain.TaskTemplateVersion, error) {
	return s.repo.GetVersion(ctx, templateID, versionID)
}

func (s *taskTemplateService) validateConfiguration(
	ctx context.Context,
	workspaceID uint,
	raw json.RawMessage,
) error {
	var config TaskTemplateConfiguration
	if err := json.Unmarshal(raw, &config); err != nil {
		return domain.ErrInvalidTaskTemplate
	}
	if config.Task.StatusID != nil {
		status, err := s.spaces.GetStatusByID(ctx, *config.Task.StatusID)
		if err != nil {
			return domain.ErrInvalidTaskTemplate
		}
		space, err := s.spaces.GetByID(ctx, status.SpaceID)
		if err != nil || space.WorkspaceID != workspaceID {
			return domain.ErrInvalidTaskTemplate
		}
	}
	if config.Task.AssigneeID != nil {
		if _, err := s.workspaces.GetMember(ctx, workspaceID, *config.Task.AssigneeID); err != nil {
			return domain.ErrInvalidTaskTemplate
		}
	}
	for _, fieldValue := range config.CustomFields {
		field, err := s.fields.Get(ctx, workspaceID, fieldValue.FieldID)
		if err != nil {
			return domain.ErrInvalidTaskTemplate
		}
		var value any
		if err := json.Unmarshal(fieldValue.Value, &value); err != nil ||
			validateCustomFieldValue(*field, value) != nil {
			return domain.ErrInvalidTaskTemplate
		}
	}
	for _, tagID := range config.Tags {
		tag, err := s.tags.GetByID(ctx, tagID)
		if err != nil {
			return domain.ErrInvalidTaskTemplate
		}
		space, err := s.spaces.GetByID(ctx, tag.SpaceID)
		if err != nil || space.WorkspaceID != workspaceID {
			return domain.ErrInvalidTaskTemplate
		}
	}
	return nil
}
