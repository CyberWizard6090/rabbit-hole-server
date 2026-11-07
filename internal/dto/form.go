package dto

import (
	"encoding/json"

	"rabbit-hole-server/internal/domain"
)

type FormRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Slug        string `json:"slug" binding:"required"`
}

type FormQuestionOptionRequest struct {
	Label string `json:"label" binding:"required"`
	Value string `json:"value" binding:"required"`
}

type FormQuestionRequest struct {
	Type        domain.QuestionType         `json:"type" binding:"required"`
	Title       string                      `json:"title" binding:"required"`
	Description string                      `json:"description"`
	Required    bool                        `json:"required"`
	Position    int                         `json:"position"`
	Settings    json.RawMessage             `json:"settings"`
	Options     []FormQuestionOptionRequest `json:"options"`
}

type ReorderFormQuestionsRequest struct {
	IDs []uint `json:"ids" binding:"required,min=1,max=200,dive,gt=0"`
}

type FormSubmissionRequest struct {
	Answers map[uint]json.RawMessage `json:"answers"`
}
