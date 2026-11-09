package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"rabbit-hole-server/internal/domain"
)

type TaskTemplateService interface {
	Create(ctx context.Context, workspaceID, userID uint, name, description string, config json.RawMessage) (*domain.TaskTemplate, error)
	List(ctx context.Context, workspaceID uint) ([]domain.TaskTemplate, error)
	Get(ctx context.Context, templateID uint) (*domain.TaskTemplate, error)
	Update(ctx context.Context, templateID uint, name, description string) (*domain.TaskTemplate, error)
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
	if len(raw) > 1<<18 {
		return nil, templateIssue("configuration", "TOO_LARGE", map[string]any{"max_bytes": 1 << 18})
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, templateIssue("configuration", "INVALID_VALUE", nil)
	}
	var config TaskTemplateConfiguration
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, templateIssue("configuration", "INVALID_TYPE", map[string]any{"expected": "object"})
	}
	if config.Task.Priority == "" {
		config.Task.Priority = "normal"
	}
	if !domain.ValidTaskPriorityName(config.Task.Priority) {
		return nil, templateIssue("configuration.task.priority", "INVALID_CHOICE",
			map[string]any{"allowed": []string{"low", "normal", "high", "urgent", "critical"}})
	}
	for i, field := range config.CustomFields {
		path := fmt.Sprintf("configuration.custom_fields[%d]", i)
		if field.FieldID == 0 {
			return nil, templateIssue(path+".field_id", "REQUIRED", nil)
		}
		if len(field.Value) == 0 || !json.Valid(field.Value) {
			return nil, templateIssue(path+".value", "REQUIRED", nil)
		}
	}
	for i, tagID := range config.Tags {
		if tagID == 0 {
			return nil, templateIssue(fmt.Sprintf("configuration.tags[%d]", i), "INVALID_VALUE", nil)
		}
	}
	if config.Task.StatusID != nil && *config.Task.StatusID == 0 {
		return nil, templateIssue("configuration.task.status_id", "INVALID_VALUE", nil)
	}
	if config.Task.AssigneeID != nil && *config.Task.AssigneeID == 0 {
		return nil, templateIssue("configuration.task.assignee_id", "INVALID_VALUE", nil)
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

func (s *taskTemplateService) Create(
	ctx context.Context,
	workspaceID, userID uint,
	name, description string,
	config json.RawMessage,
) (*domain.TaskTemplate, error) {
	name = strings.TrimSpace(name)
	if workspaceID == 0 || userID == 0 {
		return nil, domain.ErrInvalidTaskTemplate
	}
	if err := templateNameIssue(name, description); err != nil {
		return nil, err
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
	ctx context.Context, templateID uint, name, description string,
) (*domain.TaskTemplate, error) {
	template, err := s.Get(ctx, templateID)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if err := templateNameIssue(name, description); err != nil {
		return nil, err
	}
	template.Name, template.Description = name, strings.TrimSpace(description)
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

func templateIssue(field, reason string, params map[string]any) error {
	return domain.InvalidField(domain.ErrInvalidTaskTemplate, field, reason).WithParams(params)
}

func templateNameIssue(name, description string) error {
	switch {
	case name == "":
		return templateIssue("name", "REQUIRED", nil)
	case len(name) > 255:
		return templateIssue("name", "TOO_LONG", map[string]any{"max": 255})
	case len(description) > 10000:
		return templateIssue("description", "TOO_LONG", map[string]any{"max": 10000})
	}
	return nil
}

func (s *taskTemplateService) validateConfiguration(
	ctx context.Context,
	workspaceID uint,
	raw json.RawMessage,
) error {
	var config TaskTemplateConfiguration
	if err := json.Unmarshal(raw, &config); err != nil {
		return templateIssue("configuration", "INVALID_TYPE", map[string]any{"expected": "object"})
	}
	if config.Task.StatusID != nil {
		const field = "configuration.task.status_id"
		status, err := s.spaces.GetStatusByID(ctx, *config.Task.StatusID)
		if err != nil {
			return templateIssue(field, "NOT_FOUND", map[string]any{"status_id": *config.Task.StatusID})
		}
		space, err := s.spaces.GetByID(ctx, status.SpaceID)
		if err != nil || space.WorkspaceID != workspaceID {
			return templateIssue(field, "WORKSPACE_MISMATCH", nil)
		}
	}
	if config.Task.AssigneeID != nil {
		if _, err := s.workspaces.GetMember(ctx, workspaceID, *config.Task.AssigneeID); err != nil {
			return templateIssue("configuration.task.assignee_id", "NOT_A_MEMBER",
				map[string]any{"user_id": *config.Task.AssigneeID})
		}
	}
	for i, fieldValue := range config.CustomFields {
		path := fmt.Sprintf("configuration.custom_fields[%d]", i)
		field, err := s.fields.Get(ctx, workspaceID, fieldValue.FieldID)
		if err != nil {
			return templateIssue(path+".field_id", "NOT_FOUND", map[string]any{"field_id": fieldValue.FieldID})
		}
		var value any
		if err := json.Unmarshal(fieldValue.Value, &value); err != nil {
			return templateIssue(path+".value", "INVALID_VALUE", nil)
		}
		if issue := customFieldValueIssue(*field, value); issue != nil {
			return templateIssue(path+".value", issue.reason, issue.params)
		}
	}
	for i, tagID := range config.Tags {
		path := fmt.Sprintf("configuration.tags[%d]", i)
		tag, err := s.tags.GetByID(ctx, tagID)
		if err != nil {
			return templateIssue(path, "NOT_FOUND", map[string]any{"tag_id": tagID})
		}
		space, err := s.spaces.GetByID(ctx, tag.SpaceID)
		if err != nil || space.WorkspaceID != workspaceID {
			return templateIssue(path, "WORKSPACE_MISMATCH", nil)
		}
	}
	return nil
}
