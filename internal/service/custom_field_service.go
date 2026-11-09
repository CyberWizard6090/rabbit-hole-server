package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
)

type CustomFieldService interface {
	Create(ctx context.Context, workspaceID uint, input CustomFieldInput) (*domain.CustomField, error)
	List(ctx context.Context, workspaceID uint) ([]domain.CustomField, error)
	Get(ctx context.Context, workspaceID, fieldID uint) (*domain.CustomField, error)
	Update(ctx context.Context, workspaceID, fieldID uint, input CustomFieldInput) (*domain.CustomField, error)
	Delete(ctx context.Context, workspaceID, fieldID uint) error
	SetTaskValue(ctx context.Context, taskID, fieldID uint, value any) error
	GetTaskValues(ctx context.Context, taskID uint) (map[uint]any, error)
	DeleteTaskValue(ctx context.Context, taskID, fieldID uint) error
}

type CustomFieldInput struct {
	Name        string
	Description string
	Type        domain.CustomFieldType
	Required    bool
	Config      map[string]any
	Options     []domain.CustomFieldOption
}

type customFieldService struct {
	fields domain.CustomFieldRepository
	values domain.CustomFieldValueRepository
}

func NewCustomFieldService(
	fields domain.CustomFieldRepository,
	values domain.CustomFieldValueRepository,
) CustomFieldService {
	return &customFieldService{fields: fields, values: values}
}

func (s *customFieldService) Create(
	ctx context.Context,
	workspaceID uint,
	input CustomFieldInput,
) (*domain.CustomField, error) {
	field, err := customFieldFromInput(workspaceID, 0, input)
	if err != nil {
		return nil, err
	}
	if err := s.fields.Create(ctx, field); err != nil {
		return nil, fmt.Errorf("create custom field: %w", err)
	}
	return field, nil
}

func (s *customFieldService) List(ctx context.Context, workspaceID uint) ([]domain.CustomField, error) {
	fields, err := s.fields.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list custom fields: %w", err)
	}
	return fields, nil
}

func (s *customFieldService) Get(ctx context.Context, workspaceID, fieldID uint) (*domain.CustomField, error) {
	field, err := s.fields.GetByID(ctx, fieldID)
	if err != nil {
		return nil, err
	}
	if field.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return field, nil
}

func (s *customFieldService) Update(
	ctx context.Context,
	workspaceID, fieldID uint,
	input CustomFieldInput,
) (*domain.CustomField, error) {
	existing, err := s.Get(ctx, workspaceID, fieldID)
	if err != nil {
		return nil, err
	}
	updated, err := customFieldFromInput(workspaceID, fieldID, input)
	if err != nil {
		return nil, err
	}
	updated.Model = existing.Model
	if err := s.fields.Update(ctx, updated); err != nil {
		return nil, fmt.Errorf("update custom field: %w", err)
	}
	return updated, nil
}

func (s *customFieldService) Delete(ctx context.Context, workspaceID, fieldID uint) error {
	if _, err := s.Get(ctx, workspaceID, fieldID); err != nil {
		return err
	}
	if err := s.fields.Delete(ctx, workspaceID, fieldID); err != nil {
		return fmt.Errorf("delete custom field: %w", err)
	}
	return nil
}

func (s *customFieldService) SetTaskValue(
	ctx context.Context,
	taskID, fieldID uint,
	value any,
) error {
	workspaceID, err := s.values.GetTaskWorkspaceID(ctx, taskID)
	if err != nil {
		return err
	}
	field, err := s.Get(ctx, workspaceID, fieldID)
	if err != nil {
		return err
	}
	if isEmptyCustomFieldValue(value) {
		if field.Required {
			return &domain.CustomFieldRequiredError{FieldIDs: []uint{field.ID}}
		}
		err := s.values.DeleteTaskValue(ctx, taskID, fieldID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := validateCustomFieldValue(*field, value); err != nil {
		return err
	}
	if field.Type == domain.CustomFieldPeople || field.Type == domain.CustomFieldFiles {
		ids, err := customFieldIDs(value)
		if err != nil {
			return err
		}
		if err := s.values.ValidateTaskReferences(ctx, taskID, field.Type, ids); err != nil {
			return err
		}
	}
	return s.values.SetTaskValue(ctx, taskID, fieldID, value)
}

func isEmptyCustomFieldValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case []uint:
		return len(typed) == 0
	default:
		return false
	}
}

func (s *customFieldService) GetTaskValues(
	ctx context.Context,
	taskID uint,
) (map[uint]any, error) {
	return s.values.GetTaskValues(ctx, taskID)
}

func (s *customFieldService) DeleteTaskValue(
	ctx context.Context,
	taskID, fieldID uint,
) error {
	workspaceID, err := s.values.GetTaskWorkspaceID(ctx, taskID)
	if err != nil {
		return err
	}
	field, err := s.Get(ctx, workspaceID, fieldID)
	if err != nil {
		return err
	}
	if field.Required {
		return &domain.CustomFieldRequiredError{FieldIDs: []uint{field.ID}}
	}
	return s.values.DeleteTaskValue(ctx, taskID, fieldID)
}

func customFieldFromInput(workspaceID, fieldID uint, input CustomFieldInput) (*domain.CustomField, error) {
	name := strings.TrimSpace(input.Name)
	if workspaceID == 0 || name == "" || utf8.RuneCountInString(name) > 255 {
		return nil, domain.ErrInvalidCustomField
	}
	field := &domain.CustomField{
		WorkspaceID: workspaceID, Name: name, Description: strings.TrimSpace(input.Description),
		Type: input.Type, Required: input.Required, Config: input.Config,
		Options: append([]domain.CustomFieldOption(nil), input.Options...),
	}
	if field.Config == nil {
		field.Config = map[string]any{}
	}
	if !validCustomFieldConfig(field.Type, field.Config) {
		return nil, domain.ErrInvalidCustomField
	}
	if fieldID != 0 {
		field.ID = fieldID
	}
	if !supportedCustomFieldType(field.Type) {
		return nil, domain.ErrInvalidCustomField
	}
	if field.Type == domain.CustomFieldDropdown || field.Type == domain.CustomFieldMultiSelect {
		if len(field.Options) == 0 || len(field.Options) > 100 {
			return nil, domain.ErrInvalidCustomField
		}
		seen := make(map[string]struct{}, len(field.Options))
		for i := range field.Options {
			option := &field.Options[i]
			option.Label = strings.TrimSpace(option.Label)
			option.Value = strings.TrimSpace(option.Value)
			if option.Label == "" || option.Value == "" || utf8.RuneCountInString(option.Label) > 255 || utf8.RuneCountInString(option.Value) > 255 {
				return nil, domain.ErrInvalidCustomField
			}
			if _, exists := seen[option.Value]; exists {
				return nil, domain.ErrInvalidCustomField
			}
			seen[option.Value] = struct{}{}
			option.Position = i
		}
	} else if len(field.Options) != 0 {
		return nil, domain.ErrInvalidCustomField
	}
	return field, nil
}

func validCustomFieldConfig(fieldType domain.CustomFieldType, config map[string]any) bool {
	switch fieldType {
	case domain.CustomFieldNumber, domain.CustomFieldInteger:
		for key := range config {
			if key != "min" && key != "max" {
				return false
			}
		}
		var min, max float64
		var hasMin, hasMax bool
		if raw, ok := config["min"]; ok {
			min, hasMin = numericValue(raw)
			if !hasMin {
				return false
			}
		}
		if raw, ok := config["max"]; ok {
			max, hasMax = numericValue(raw)
			if !hasMax {
				return false
			}
		}
		return !hasMin || !hasMax || min <= max
	case domain.CustomFieldText:
		for key, raw := range config {
			if key != "min_length" && key != "max_length" {
				return false
			}
			bound, ok := numericValue(raw)
			if !ok || bound < 0 || bound != float64(int(bound)) {
				return false
			}
		}
		if min, minOK := numericValue(config["min_length"]); minOK {
			if max, maxOK := numericValue(config["max_length"]); maxOK && min > max {
				return false
			}
		}
		return true
	}
	return len(config) == 0
}

func supportedCustomFieldType(fieldType domain.CustomFieldType) bool {
	switch fieldType {
	case domain.CustomFieldText, domain.CustomFieldNumber, domain.CustomFieldInteger,
		domain.CustomFieldBoolean, domain.CustomFieldDateTime, domain.CustomFieldDate,
		domain.CustomFieldURL, domain.CustomFieldEmail, domain.CustomFieldPhone,
		domain.CustomFieldDropdown, domain.CustomFieldMultiSelect, domain.CustomFieldCheckbox,
		domain.CustomFieldPeople, domain.CustomFieldFiles:
		return true
	default:
		return false
	}
}

func validateCustomFieldValue(field domain.CustomField, value any) error {
	invalid := func() error {
		return fmt.Errorf("%w: value does not match type %q", domain.ErrInvalidCustomField, field.Type)
	}
	switch field.Type {
	case domain.CustomFieldText, domain.CustomFieldPhone:
		text, ok := value.(string)
		if !ok {
			return invalid()
		}
		if field.Type == domain.CustomFieldText && !withinStringBounds(field.Config, len([]rune(text))) {
			return invalid()
		}
	case domain.CustomFieldURL:
		text, ok := value.(string)
		if !ok {
			return invalid()
		}
		parsed, err := url.ParseRequestURI(text)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return invalid()
		}
	case domain.CustomFieldEmail:
		text, ok := value.(string)
		if !ok {
			return invalid()
		}
		address, err := mail.ParseAddress(text)
		if err != nil || address.Address != text {
			return invalid()
		}
	case domain.CustomFieldNumber:
		number, ok := numericValue(value)
		if !ok || !withinBounds(field.Config, number) {
			return invalid()
		}
	case domain.CustomFieldInteger:
		number, ok := numericValue(value)
		if !ok || number != float64(int64(number)) || !withinBounds(field.Config, number) {
			return invalid()
		}
	case domain.CustomFieldBoolean, domain.CustomFieldCheckbox:
		if _, ok := value.(bool); !ok {
			return invalid()
		}
	case domain.CustomFieldDate, domain.CustomFieldDateTime:
		text, ok := value.(string)
		if !ok {
			return invalid()
		}
		layout := time.RFC3339
		if field.Type == domain.CustomFieldDate {
			layout = "2006-01-02"
		}
		if _, err := time.Parse(layout, text); err != nil {
			return invalid()
		}
	case domain.CustomFieldDropdown:
		text, ok := value.(string)
		if !ok || !containsCustomFieldOption(field.Options, text) {
			return invalid()
		}
	case domain.CustomFieldMultiSelect:
		items, ok := value.([]any)
		if !ok {
			if strings, stringSlice := value.([]string); stringSlice {
				items = make([]any, len(strings))
				for i := range strings {
					items[i] = strings[i]
				}
			} else {
				return invalid()
			}
		}
		for _, item := range items {
			text, ok := item.(string)
			if !ok || !containsCustomFieldOption(field.Options, text) {
				return invalid()
			}
		}
	case domain.CustomFieldPeople, domain.CustomFieldFiles:
		if !isNumericIDList(value) {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

func withinStringBounds(config map[string]any, length int) bool {
	for key, actual := range map[string]int{"min_length": length, "max_length": length} {
		if raw, exists := config[key]; exists {
			bound, ok := numericValue(raw)
			if !ok || bound < 0 || bound != float64(int(bound)) {
				return false
			}
			if key == "min_length" && actual < int(bound) || key == "max_length" && actual > int(bound) {
				return false
			}
		}
	}
	return true
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case json.Number:
		value, err := number.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}

func withinBounds(config map[string]any, value float64) bool {
	if config == nil {
		return true
	}
	if min, exists := config["min"]; exists {
		bound, ok := numericValue(min)
		if !ok || value < bound {
			return false
		}
	}
	if max, exists := config["max"]; exists {
		bound, ok := numericValue(max)
		if !ok || value > bound {
			return false
		}
	}
	return true
}

func containsCustomFieldOption(options []domain.CustomFieldOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func isNumericIDList(value any) bool {
	switch values := value.(type) {
	case []any:
		seen := make(map[uint]struct{}, len(values))
		for _, item := range values {
			number, ok := numericValue(item)
			if !ok || number <= 0 || number != float64(uint(number)) {
				return false
			}
			id := uint(number)
			if _, exists := seen[id]; exists {
				return false
			}
			seen[id] = struct{}{}
		}
		return true
	case []uint:
		seen := make(map[uint]struct{}, len(values))
		for _, id := range values {
			if id == 0 {
				return false
			}
			if _, exists := seen[id]; exists {
				return false
			}
			seen[id] = struct{}{}
		}
		return true
	case []int64:
		seen := make(map[uint]struct{}, len(values))
		for _, item := range values {
			if item <= 0 {
				return false
			}
			id := uint(item)
			if _, exists := seen[id]; exists {
				return false
			}
			seen[id] = struct{}{}
		}
		return true
	default:
		return false
	}
}

func customFieldIDs(value any) ([]uint, error) {
	if !isNumericIDList(value) {
		return nil, domain.ErrInvalidCustomField
	}
	switch values := value.(type) {
	case []uint:
		return values, nil
	case []int64:
		ids := make([]uint, len(values))
		for i, value := range values {
			if value <= 0 {
				return nil, domain.ErrInvalidCustomField
			}
			ids[i] = uint(value)
		}
		return ids, nil
	case []any:
		ids := make([]uint, len(values))
		for i, value := range values {
			if number, ok := value.(json.Number); ok {
				parsed, err := strconv.ParseUint(number.String(), 10, 64)
				if err != nil || parsed == 0 {
					return nil, domain.ErrInvalidCustomField
				}
				ids[i] = uint(parsed)
				continue
			}
			number, _ := numericValue(value)
			ids[i] = uint(number)
		}
		return ids, nil
	default:
		return nil, domain.ErrInvalidCustomField
	}
}
