package dto

import "encoding/json"

type CustomFieldOptionRequest struct {
	Label string `json:"label" binding:"required"`
	Value string `json:"value" binding:"required"`
}

type CustomFieldRequest struct {
	Name        string                     `json:"name" binding:"required"`
	Description string                     `json:"description"`
	Type        string                     `json:"type" binding:"required"`
	Required    bool                       `json:"required"`
	Config      map[string]any             `json:"config"`
	Options     []CustomFieldOptionRequest `json:"options"`
}

type CustomFieldValueRequest struct {
	Value json.RawMessage `json:"value"`
}
