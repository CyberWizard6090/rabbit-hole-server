package domain

import (
	"context"
	"errors"
)

var (
	ErrInvalidCustomField   = errors.New("invalid custom field")
	ErrCustomFieldInUse     = errors.New("custom field has values and cannot be deleted")
	ErrCustomFieldNameTaken = errors.New("custom field name already exists in this workspace")
)

type CustomFieldType string

const (
	CustomFieldText        CustomFieldType = "text"
	CustomFieldNumber      CustomFieldType = "number"
	CustomFieldInteger     CustomFieldType = "integer"
	CustomFieldBoolean     CustomFieldType = "boolean"
	CustomFieldDateTime    CustomFieldType = "datetime"
	CustomFieldDate        CustomFieldType = "date"
	CustomFieldURL         CustomFieldType = "url"
	CustomFieldEmail       CustomFieldType = "email"
	CustomFieldPhone       CustomFieldType = "phone"
	CustomFieldDropdown    CustomFieldType = "dropdown"
	CustomFieldMultiSelect CustomFieldType = "multi_select"
	CustomFieldCheckbox    CustomFieldType = "checkbox"
	CustomFieldPeople      CustomFieldType = "people"
	CustomFieldFiles       CustomFieldType = "files"
)

type CustomFieldOption struct {
	ID       uint   `json:"id"`
	Label    string `json:"label"`
	Value    string `json:"value"`
	Position int    `json:"position"`
}

type CustomField struct {
	Model
	WorkspaceID uint                `json:"workspace_id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Type        CustomFieldType     `json:"type"`
	Required    bool                `json:"required"`
	Config      map[string]any      `json:"config"`
	Options     []CustomFieldOption `json:"options"`
}

type CustomFieldRepository interface {
	Create(ctx context.Context, field *CustomField) error
	ListByWorkspace(ctx context.Context, workspaceID uint) ([]CustomField, error)
	GetByID(ctx context.Context, fieldID uint) (*CustomField, error)
	Update(ctx context.Context, field *CustomField) error
	Delete(ctx context.Context, workspaceID, fieldID uint) error
}

type CustomFieldValueRepository interface {
	GetTaskWorkspaceID(ctx context.Context, taskID uint) (uint, error)
	SetTaskValue(ctx context.Context, taskID, fieldID uint, value any) error
	GetTaskValues(ctx context.Context, taskID uint) (map[uint]any, error)
	DeleteTaskValue(ctx context.Context, taskID, fieldID uint) error
	ValidateTaskReferences(ctx context.Context, taskID uint, fieldType CustomFieldType, ids []uint) error
}
