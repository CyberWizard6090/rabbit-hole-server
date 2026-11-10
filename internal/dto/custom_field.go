package dto

import "encoding/json"

type CustomFieldOptionRequest struct {
	Label string `json:"label" binding:"required,max=255"`
	Value string `json:"value" binding:"required,max=255"`
}

type CustomFieldRequest struct {
	Name        string                     `json:"name" binding:"required,max=255"`
	Description string                     `json:"description" binding:"max=5000"`
	Type        string                     `json:"type" binding:"required,max=50"`
	Required    bool                       `json:"required"`
	Config      map[string]any             `json:"config"`
	Options     []CustomFieldOptionRequest `json:"options" binding:"max=200,dive"`
}

type CustomFieldValueRequest struct {
	Value json.RawMessage `json:"value"`
}
