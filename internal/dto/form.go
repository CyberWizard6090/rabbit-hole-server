package dto

import (
	"encoding/json"

	"rabbit-hole-server/internal/domain"
)

type FormRequest struct {
	Name        string `json:"name" binding:"required,max=255"`
	Description string `json:"description" binding:"max=5000"`
	Slug        string `json:"slug" binding:"required,max=255"`

	Settings *domain.FormSettings `json:"settings"`
}

type CreateFormRequest struct {
	FormRequest
	Questions  []FormQuestionRequest         `json:"questions" binding:"dive"`
	TaskConfig *FormTaskConfigurationRequest `json:"task_config"`
}

type FormQuestionOptionRequest struct {
	Label string `json:"label" binding:"required,max=255"`
	Value string `json:"value" binding:"required,max=255"`
}

type FormQuestionRequest struct {
	Type        domain.QuestionType         `json:"type" binding:"required"`
	Title       string                      `json:"title" binding:"required,max=500"`
	Description string                      `json:"description" binding:"max=5000"`
	Required    bool                        `json:"required"`
	Position    *int                        `json:"position" binding:"omitempty,lte=100000"`
	Settings    json.RawMessage             `json:"settings"`
	Options     []FormQuestionOptionRequest `json:"options" binding:"max=200,dive"`
}

type ReorderFormQuestionsRequest struct {
	IDs []uint `json:"ids" binding:"required,min=1,max=200,dive,gt=0"`
}

type FormSubmissionRequest struct {
	Answers map[uint]json.RawMessage `json:"answers"`
}
