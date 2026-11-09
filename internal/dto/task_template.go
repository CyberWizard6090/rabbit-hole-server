package dto

import "encoding/json"

type TaskTemplateRequest struct {
	Name          string          `json:"name" binding:"required,max=255"`
	Description   string          `json:"description" binding:"max=5000"`
	Configuration json.RawMessage `json:"configuration" binding:"required"`
}

type UpdateTaskTemplateRequest struct {
	Name        string `json:"name" binding:"required,max=255"`
	Description string `json:"description" binding:"max=5000"`
}

type TaskTemplateVersionRequest struct {
	Configuration json.RawMessage `json:"configuration" binding:"required"`
}

type ApplyTaskTemplateRequest struct {
	ListID      uint   `json:"list_id" binding:"required,gt=0"`
	StatusID    uint   `json:"status_id" binding:"omitempty,gt=0"`
	Title       string `json:"title" binding:"max=500"`
	Description string `json:"description" binding:"max=50000"`
}

type FormTaskConfigurationRequest struct {
	Enabled      bool                     `json:"enabled"`
	OwnerID      *uint                    `json:"owner_id" binding:"omitempty,gt=0"`
	Target       FormTaskTargetRequest    `json:"target"`
	Template     *FormTaskTemplateRequest `json:"template"`
	TaskDefaults FormTaskDefaultsRequest  `json:"task_defaults"`
	Mappings     []FormTaskMappingRequest `json:"mappings" binding:"max=200,dive"`
}

type FormTaskTargetRequest struct {
	WorkspaceID uint `json:"workspace_id" binding:"required,gt=0"`
	ListID      uint `json:"list_id"`
}

type FormTaskTemplateRequest struct {
	TemplateID uint `json:"template_id" binding:"required,gt=0"`
	VersionID  uint `json:"version_id" binding:"omitempty,gt=0"`
}

type FormTaskDefaultsRequest struct {
	Priority   string `json:"priority" binding:"max=32"`
	StatusID   *uint  `json:"status_id"`
	AssigneeID *uint  `json:"assignee_id"`
}

type FormTaskMappingRequest struct {
	QuestionID    uint                         `json:"question_id" binding:"omitempty,gt=0"`
	QuestionIndex *int                         `json:"question_index" binding:"omitempty,gte=0,lte=100000"`
	Target        FormTaskMappingTargetRequest `json:"target" binding:"required"`
	Transform     *FormTaskTransformRequest    `json:"transform"`
}

type FormTaskMappingTargetRequest struct {
	Type    string `json:"type" binding:"required,max=64"`
	Field   string `json:"field" binding:"max=100"`
	FieldID uint   `json:"field_id"`
}

type FormTaskTransformRequest struct {
	Type   string            `json:"type" binding:"required,max=64"`
	Values map[string]string `json:"values" binding:"required,max=200,dive,keys,max=255,endkeys,max=255"`
}
