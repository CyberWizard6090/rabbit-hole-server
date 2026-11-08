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
	if err := s.resolveTemplateVersion(ctx, form.WorkspaceID, &config); err != nil {
		return err
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
	if config.Target.WorkspaceID != form.WorkspaceID {
		return domain.NewFormValidationError("target.workspace_id", "WORKSPACE_MISMATCH")
	}
	if len(config.Mappings) > 200 {
		return domain.NewFormValidationError("mappings", "TOO_MANY").WithParams(map[string]any{"max": 200})
	}
	if !config.Enabled {
		return nil
	}
	if config.Target.ListID == 0 {
		return domain.NewFormValidationError("target.list_id", "REQUIRED")
	}
	if config.TaskDefaults.Priority != "" && !domain.ValidTaskPriorityName(config.TaskDefaults.Priority) {
		return domain.NewFormValidationError("task_defaults.priority", "INVALID_CHOICE").WithParams(map[string]any{"allowed": []string{"low", "normal", "high", "urgent", "critical"}})
	}
	list, err := s.spaces.GetListByID(ctx, config.Target.ListID)
	if err != nil {
		return domain.NewFormValidationError("target.list_id", "NOT_FOUND")
	}
	space, err := s.spaces.GetByID(ctx, list.SpaceID)
	if err != nil || space.WorkspaceID != form.WorkspaceID {
		return domain.NewFormValidationError("target.list_id", "WORKSPACE_MISMATCH")
	}
	var templateConfig *TaskTemplateConfiguration
	if config.Template != nil {
		decoded, err := s.validateTemplate(ctx, form.WorkspaceID, *config.Template)
		if err != nil {
			return err
		}
		templateConfig = decoded
	}
	statusID := config.TaskDefaults.StatusID
	if statusID != nil && !listHasStatus(list, *statusID) {
		return statusNotInList("task_defaults.status_id", *statusID, list.ID)
	}
	if config.TaskDefaults.AssigneeID != nil {
		if _, err := s.workspaces.GetMember(ctx, form.WorkspaceID, *config.TaskDefaults.AssigneeID); err != nil {
			return domain.NewFormValidationError("task_defaults.assignee_id", "NOT_A_MEMBER")
		}
	}
	if templateConfig != nil && templateConfig.Task.StatusID != nil {
		if !listHasStatus(list, *templateConfig.Task.StatusID) {
			return statusNotInList("template.template_id", *templateConfig.Task.StatusID, list.ID)
		}
		statusID = templateConfig.Task.StatusID
	}
	if statusID == nil {
		return domain.NewFormValidationError("task_defaults.status_id", "REQUIRED_UNLESS_TEMPLATE_STATUS")
	}
	questions := make(map[uint]domain.FormQuestion, len(form.Questions))
	for _, question := range form.Questions {
		questions[question.ID] = question
	}
	seenQuestions := make(map[uint]struct{}, len(config.Mappings))
	seenTargets := make(map[string]struct{}, len(config.Mappings))
	hasTitle := templateConfig != nil && templateConfig.Task.Title != nil && *templateConfig.Task.Title != ""
	for i, mapping := range config.Mappings {
		field := fmt.Sprintf("mappings[%d]", i)
		if mapping.QuestionIndex != nil {
			return domain.NewFormValidationError(field+".question_index", "UNSUPPORTED_ON_UPDATE")
		}
		question, exists := questions[mapping.QuestionID]
		if mapping.QuestionID == 0 || !exists || isStaticQuestion(question.Type) {
			return domain.NewFormValidationError(field+".question_id", "INVALID_REFERENCE")
		}
		if _, exists := seenQuestions[mapping.QuestionID]; exists {
			return domain.NewFormValidationError(field+".question_id", "DUPLICATE")
		}
		seenQuestions[mapping.QuestionID] = struct{}{}
		targetKey, err := s.validateMapping(ctx, form.WorkspaceID, list.ID, question, mapping, field)
		if err != nil {
			return err
		}
		if _, exists := seenTargets[targetKey]; exists {
			return domain.NewFormValidationError(field+".target", "DUPLICATE")
		}
		seenTargets[targetKey] = struct{}{}
		if targetKey == "task:title" {
			hasTitle = true
		}
	}
	if !hasTitle {
		return domain.NewFormValidationError("mappings", "TITLE_MAPPING_REQUIRED")
	}
	if templateConfig != nil {
		for _, tagID := range templateConfig.Tags {
			tag, err := s.tags.GetByID(ctx, tagID)
			if err != nil || tag.SpaceID != space.ID {
				return domain.NewFormValidationError("template.template_id", "INVALID_REFERENCE").WithParams(map[string]any{"tag_id": tagID}).
					WithCode(domain.FormErrorTaskTemplateInvalid)
			}
		}
	}
	return nil
}

func (s *formTaskConfigurationService) validateTemplate(
	ctx context.Context,
	workspaceID uint,
	reference domain.FormTaskTemplate,
) (*TaskTemplateConfiguration, error) {
	invalid := func(field, reason string, params map[string]any) error {
		return domain.NewFormValidationError(field, reason).WithParams(params).WithCode(domain.FormErrorTaskTemplateInvalid)
	}
	if reference.TemplateID == 0 {
		return nil, invalid("template.template_id", "REQUIRED", nil)
	}
	if reference.VersionID == 0 {
		return nil, invalid("template.version_id", "REQUIRED", nil)
	}
	template, err := s.templates.Get(ctx, reference.TemplateID)
	if err != nil || template.WorkspaceID != workspaceID {
		return nil, invalid("template.template_id", "NOT_FOUND", nil)
	}
	if template.Status != "active" {
		return nil, invalid("template.template_id", "TEMPLATE_UNAVAILABLE", nil)
	}
	version, err := s.templates.GetVersion(ctx, template.ID, reference.VersionID)
	if err != nil {
		return nil, invalid("template.version_id", "INVALID_REFERENCE", nil)
	}
	var decoded TaskTemplateConfiguration
	if err := json.Unmarshal(version.Configuration, &decoded); err != nil {
		return nil, fmt.Errorf("decode task template version: %w", err)
	}
	for _, field := range decoded.CustomFields {
		if _, err := s.fields.Get(ctx, workspaceID, field.FieldID); err != nil {
			return nil, invalid("template.template_id", "INVALID_REFERENCE", map[string]any{"field_id": field.FieldID})
		}
	}
	return &decoded, nil
}

func (s *formTaskConfigurationService) resolveTemplateVersion(
	ctx context.Context,
	workspaceID uint,
	config *domain.FormTaskConfiguration,
) error {
	if !config.Enabled || config.Template == nil || config.Template.VersionID != 0 || config.Template.TemplateID == 0 {
		return nil
	}
	template, err := s.templates.Get(ctx, config.Template.TemplateID)
	if err != nil || template.WorkspaceID != workspaceID {
		return domain.NewFormValidationError("template.template_id", "NOT_FOUND").
			WithCode(domain.FormErrorTaskTemplateInvalid)
	}
	if template.Status != "active" || len(template.Versions) == 0 {
		return domain.NewFormValidationError("template.template_id", "TEMPLATE_UNAVAILABLE").
			WithCode(domain.FormErrorTaskTemplateInvalid)
	}
	resolved := *config.Template
	resolved.VersionID = template.Versions[len(template.Versions)-1].ID
	config.Template = &resolved
	return nil
}

func statusNotInList(field string, statusID, listID uint) error {
	return domain.NewFormValidationError(field, "INVALID_REFERENCE").WithParams(map[string]any{"status_id": statusID, "list_id": listID}).
		WithCode(domain.FormErrorTaskStatusNotInList)
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
	field string,
) (string, error) {
	invalid := func(path, reason string, params map[string]any) (string, error) {
		return "", domain.NewFormValidationError(field+path, reason).WithParams(params)
	}
	if mapping.Transform != nil {
		if mapping.Transform.Type != "enum" || len(mapping.Transform.Values) == 0 {
			return invalid(".transform", "INVALID_TRANSFORM", nil)
		}
		for _, option := range question.Options {
			if _, ok := mapping.Transform.Values[option.Value]; !ok {
				return invalid(".transform.values", "MISSING_TRANSFORM_ENTRY", map[string]any{"option": option.Value})
			}
		}
	}
	switch mapping.Target.Type {
	case "task_field":
		key := "task:" + mapping.Target.Field
		switch mapping.Target.Field {
		case "title", "description":
			if mapping.Transform != nil || !questionIsText(question.Type) {
				return invalid(".target", "INCOMPATIBLE_QUESTION", map[string]any{"target": mapping.Target.Field})
			}
		case "priority":
			if !isChoiceQuestion(question.Type) || mapping.Transform == nil {
				return invalid(".target", "INCOMPATIBLE_QUESTION", map[string]any{"target": mapping.Target.Field})
			}
			for _, value := range mapping.Transform.Values {
				if !domain.ValidTaskPriorityName(value) {
					return invalid(".transform.values", "INVALID_CHOICE", map[string]any{"value": value, "allowed": []string{"low", "normal", "high", "urgent", "critical"}})
				}
			}
		case "status_id", "assignee_id":
			if !isChoiceQuestion(question.Type) || mapping.Transform == nil {
				return invalid(".target", "INCOMPATIBLE_QUESTION", map[string]any{"target": mapping.Target.Field})
			}
			for _, value := range mapping.Transform.Values {
				id, err := strconv.ParseUint(value, 10, 64)
				if err != nil || id == 0 {
					return invalid(".transform.values", "INVALID_ID", map[string]any{"value": value})
				}
				if mapping.Target.Field == "status_id" {
					status, err := s.spaces.GetStatusByID(ctx, uint(id))
					if err != nil || status.ListID != listID {
						return "", statusNotInList(field+".transform.values", uint(id), listID)
					}
				} else if _, err := s.workspaces.GetMember(ctx, workspaceID, uint(id)); err != nil {
					return invalid(".transform.values", "NOT_A_MEMBER", map[string]any{"user_id": id})
				}
			}
		case "due_date":
			if question.Type != domain.QuestionDate || mapping.Transform != nil {
				return invalid(".target", "INCOMPATIBLE_QUESTION", map[string]any{"target": mapping.Target.Field})
			}
		default:
			return invalid(".target.field", "INVALID_CHOICE", nil)
		}
		return key, nil
	case "custom_field":
		if mapping.Target.FieldID == 0 || mapping.Target.Field != "" || mapping.Transform != nil {
			return invalid(".target", "INVALID_TARGET", nil)
		}
		customField, err := s.fields.Get(ctx, workspaceID, mapping.Target.FieldID)
		if err != nil {
			return invalid(".target.field_id", "NOT_FOUND", nil)
		}
		if !questionCompatibleWithCustomField(question.Type, customField.Type) {
			return invalid(".target.field_id", "INCOMPATIBLE_QUESTION", map[string]any{"target": "custom_field"})
		}
		return fmt.Sprintf("custom:%d", customField.ID), nil
	default:
		return invalid(".target.type", "INVALID_CHOICE", map[string]any{"allowed": []string{"task_field", "custom_field"}})
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
