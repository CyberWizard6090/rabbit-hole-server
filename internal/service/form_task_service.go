package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"rabbit-hole-server/internal/domain"
)

type FormTaskConfigurationService interface {
	Get(ctx context.Context, workspaceID, formID uint) (*domain.FormTaskConfiguration, error)
	Set(ctx context.Context, workspaceID, formID uint, config domain.FormTaskConfiguration) error
	ValidatePublication(ctx context.Context, workspaceID, formID uint) error
}

type formTaskConfigurationService struct {
	forms      domain.FormRepository
	spaces     domain.SpaceRepository
	templates  domain.TaskTemplateRepository
	fields     CustomFieldService
	tags       domain.TagRepository
	workspaces domain.WorkspaceRepository
}

func NewFormTaskConfigurationService(
	forms domain.FormRepository,
	spaces domain.SpaceRepository,
	templates domain.TaskTemplateRepository,
	fields CustomFieldService,
	tags domain.TagRepository,
	workspaces domain.WorkspaceRepository,
) FormTaskConfigurationService {
	return &formTaskConfigurationService{
		forms: forms, spaces: spaces, templates: templates, fields: fields, tags: tags,
		workspaces: workspaces,
	}
}

func (s *formTaskConfigurationService) Get(
	ctx context.Context,
	workspaceID, formID uint,
) (*domain.FormTaskConfiguration, error) {
	form, err := s.forms.GetByID(ctx, formID)
	if err != nil {
		return nil, err
	}
	if form.WorkspaceID != workspaceID {
		return nil, domain.ErrFormNotFound
	}
	config := defaultFormTaskConfiguration(workspaceID)
	if len(form.TaskConfig) != 0 && string(form.TaskConfig) != "{}" {
		if err := json.Unmarshal(form.TaskConfig, &config); err != nil {
			return nil, fmt.Errorf("decode form task configuration: %w", err)
		}
	}
	return &config, nil
}

func (s *formTaskConfigurationService) Set(
	ctx context.Context,
	workspaceID, formID uint,
	config domain.FormTaskConfiguration,
) error {
	form, err := s.forms.GetByID(ctx, formID)
	if err != nil {
		return err
	}
	if form.WorkspaceID != workspaceID {
		return domain.ErrFormNotFound
	}
	if err := s.validate(ctx, form, config); err != nil {
		return err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode form task configuration: %w", err)
	}
	return s.forms.UpdateTaskConfig(ctx, workspaceID, formID, raw)
}

func (s *formTaskConfigurationService) ValidatePublication(
	ctx context.Context,
	workspaceID, formID uint,
) error {
	form, err := s.forms.GetByID(ctx, formID)
	if err != nil {
		return err
	}
	if form.WorkspaceID != workspaceID {
		return domain.ErrFormNotFound
	}
	config := defaultFormTaskConfiguration(workspaceID)
	if len(form.TaskConfig) != 0 && string(form.TaskConfig) != "{}" {
		if err := json.Unmarshal(form.TaskConfig, &config); err != nil {
			return fmt.Errorf("decode form task configuration: %w", err)
		}
	}
	return s.validate(ctx, form, config)
}

func (s *formTaskConfigurationService) validate(
	ctx context.Context,
	form *domain.Form,
	config domain.FormTaskConfiguration,
) error {
	if config.Target.WorkspaceID != form.WorkspaceID || len(config.Mappings) > 200 {
		return domain.ErrInvalidForm
	}
	if !config.Enabled {
		return nil
	}
	if config.Target.ListID == 0 ||
		config.TaskDefaults.Priority != "" && !validTaskPriority(config.TaskDefaults.Priority) {
		return domain.ErrInvalidForm
	}
	list, err := s.spaces.GetListByID(ctx, config.Target.ListID)
	if err != nil {
		return domain.ErrInvalidForm
	}
	space, err := s.spaces.GetByID(ctx, list.SpaceID)
	if err != nil || space.WorkspaceID != form.WorkspaceID {
		return domain.ErrInvalidForm
	}
	var templateConfig *TaskTemplateConfiguration
	if config.Template != nil {
		if config.Template.TemplateID == 0 || config.Template.VersionID == 0 {
			return domain.ErrInvalidForm
		}
		template, err := s.templates.Get(ctx, config.Template.TemplateID)
		if err != nil || template.WorkspaceID != form.WorkspaceID || template.Status != "active" {
			return domain.ErrInvalidForm
		}
		version, err := s.templates.GetVersion(ctx, template.ID, config.Template.VersionID)
		if err != nil {
			return domain.ErrInvalidForm
		}
		var decoded TaskTemplateConfiguration
		if err := json.Unmarshal(version.Configuration, &decoded); err != nil {
			return fmt.Errorf("decode task template version: %w", err)
		}
		templateConfig = &decoded
		for _, field := range decoded.CustomFields {
			if _, err := s.fields.Get(ctx, form.WorkspaceID, field.FieldID); err != nil {
				return domain.ErrInvalidForm
			}
		}
	}
	statusID := config.TaskDefaults.StatusID
	if statusID != nil && !listHasStatus(list, *statusID) {
		return domain.ErrInvalidForm
	}
	if config.TaskDefaults.AssigneeID != nil {
		if _, err := s.workspaces.GetMember(ctx, form.WorkspaceID, *config.TaskDefaults.AssigneeID); err != nil {
			return domain.ErrInvalidForm
		}
	}
	if templateConfig != nil && templateConfig.Task.StatusID != nil {
		statusID = templateConfig.Task.StatusID
	}
	if statusID == nil || !listHasStatus(list, *statusID) {
		return domain.ErrInvalidForm
	}
	questions := make(map[uint]domain.FormQuestion, len(form.Questions))
	for _, question := range form.Questions {
		questions[question.ID] = question
	}
	seenQuestions := make(map[uint]struct{}, len(config.Mappings))
	seenTargets := make(map[string]struct{}, len(config.Mappings))
	hasTitle := templateConfig != nil && templateConfig.Task.Title != nil && *templateConfig.Task.Title != ""
	for _, mapping := range config.Mappings {
		question, exists := questions[mapping.QuestionID]
		if !exists || isStaticQuestion(question.Type) {
			return domain.ErrInvalidForm
		}
		if _, exists := seenQuestions[mapping.QuestionID]; exists {
			return domain.ErrInvalidForm
		}
		seenQuestions[mapping.QuestionID] = struct{}{}
		targetKey, err := s.validateMapping(ctx, form.WorkspaceID, list.ID, question, mapping)
		if err != nil {
			return err
		}
		if _, exists := seenTargets[targetKey]; exists {
			return domain.ErrInvalidForm
		}
		seenTargets[targetKey] = struct{}{}
		if targetKey == "task:title" {
			hasTitle = true
		}
	}
	if !hasTitle {
		return domain.ErrInvalidForm
	}
	if config.Template != nil && templateConfig != nil {
		for _, tagID := range templateConfig.Tags {
			tag, err := s.tags.GetByID(ctx, tagID)
			if err != nil || tag.SpaceID != space.ID {
				return domain.ErrInvalidForm
			}
		}
	}
	return nil
}

func listHasStatus(list *domain.List, statusID uint) bool {
	for _, status := range list.Statuses {
		if status.ID == statusID {
			return true
		}
	}
	return false
}

func (s *formTaskConfigurationService) validateMapping(
	ctx context.Context,
	workspaceID uint,
	listID uint,
	question domain.FormQuestion,
	mapping domain.FormTaskMapping,
) (string, error) {
	if mapping.Transform != nil {
		if mapping.Transform.Type != "enum" || len(mapping.Transform.Values) == 0 {
			return "", domain.ErrInvalidForm
		}
		for _, option := range question.Options {
			if _, ok := mapping.Transform.Values[option.Value]; !ok {
				return "", domain.ErrInvalidForm
			}
		}
	}
	switch mapping.Target.Type {
	case "task_field":
		key := "task:" + mapping.Target.Field
		switch mapping.Target.Field {
		case "title", "description":
			if mapping.Transform != nil || !questionIsText(question.Type) {
				return "", domain.ErrInvalidForm
			}
		case "priority":
			if !isChoiceQuestion(question.Type) || mapping.Transform == nil {
				return "", domain.ErrInvalidForm
			}
			for _, value := range mapping.Transform.Values {
				if !validTaskPriority(value) {
					return "", domain.ErrInvalidForm
				}
			}
		case "status_id", "assignee_id":
			if !isChoiceQuestion(question.Type) || mapping.Transform == nil {
				return "", domain.ErrInvalidForm
			}
			for _, value := range mapping.Transform.Values {
				id, err := strconv.ParseUint(value, 10, 64)
				if err != nil || id == 0 {
					return "", domain.ErrInvalidForm
				}
				if mapping.Target.Field == "status_id" {
					status, err := s.spaces.GetStatusByID(ctx, uint(id))
					if err != nil || status.ListID != listID {
						return "", domain.ErrInvalidForm
					}
				} else if _, err := s.workspaces.GetMember(ctx, workspaceID, uint(id)); err != nil {
					return "", domain.ErrInvalidForm
				}
			}
		case "due_date":
			if question.Type != domain.QuestionDate || mapping.Transform != nil {
				return "", domain.ErrInvalidForm
			}
		default:
			return "", domain.ErrInvalidForm
		}
		return key, nil
	case "custom_field":
		if mapping.Target.FieldID == 0 || mapping.Target.Field != "" || mapping.Transform != nil {
			return "", domain.ErrInvalidForm
		}
		field, err := s.fields.Get(ctx, workspaceID, mapping.Target.FieldID)
		if err != nil || !questionCompatibleWithCustomField(question.Type, field.Type) {
			return "", domain.ErrInvalidForm
		}
		return fmt.Sprintf("custom:%d", field.ID), nil
	default:
		return "", domain.ErrInvalidForm
	}
}

func questionIsText(questionType domain.QuestionType) bool {
	switch questionType {
	case domain.QuestionShortText, domain.QuestionLongText, domain.QuestionEmail,
		domain.QuestionPhone, domain.QuestionURL:
		return true
	default:
		return false
	}
}

func questionCompatibleWithCustomField(questionType domain.QuestionType, fieldType domain.CustomFieldType) bool {
	switch fieldType {
	case domain.CustomFieldText, domain.CustomFieldEmail, domain.CustomFieldPhone, domain.CustomFieldURL:
		return questionIsText(questionType)
	case domain.CustomFieldNumber, domain.CustomFieldInteger:
		return questionType == domain.QuestionNumber
	case domain.CustomFieldBoolean, domain.CustomFieldCheckbox:
		return questionType == domain.QuestionCheckbox
	case domain.CustomFieldDate:
		return questionType == domain.QuestionDate
	case domain.CustomFieldDateTime:
		return questionType == domain.QuestionDateTime
	case domain.CustomFieldDropdown:
		return isChoiceQuestion(questionType) && questionType != domain.QuestionMultipleChoice
	case domain.CustomFieldMultiSelect:
		return questionType == domain.QuestionMultipleChoice
	default:
		return false
	}
}

func defaultFormTaskConfiguration(workspaceID uint) domain.FormTaskConfiguration {
	return domain.FormTaskConfiguration{
		Target:       domain.FormTaskTarget{WorkspaceID: workspaceID},
		TaskDefaults: domain.FormTaskDefaults{Priority: "normal"},
		Mappings:     []domain.FormTaskMapping{},
	}
}

func validTaskPriority(priority string) bool {
	switch priority {
	case "low", "normal", "high", "urgent":
		return true
	default:
		return false
	}
}
