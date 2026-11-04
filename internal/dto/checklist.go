package dto

import (
	"strings"

	"rabbit-hole-server/internal/domain"
)

type ChecklistNameRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
}

type CreateChecklistItemRequest struct {
	Text       string `json:"text" binding:"required,min=1,max=500"`
	AssigneeID *uint  `json:"assignee_id,omitempty"`
}

type UpdateChecklistItemRequest struct {
	Text       *string               `json:"text,omitempty" binding:"omitempty,min=1,max=500"`
	Resolved   *bool                 `json:"resolved,omitempty"`
	AssigneeID domain.Nullable[uint] `json:"assignee_id"`
}

type ReorderRequest struct {
	IDs []uint `json:"ids" binding:"required,min=1,max=200,dive,gt=0"`
}

func (request *ChecklistNameRequest) UnmarshalJSON(data []byte) error {
	type plain ChecklistNameRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Name = strings.TrimSpace(value.Name)
	}); err != nil {
		return err
	}
	*request = ChecklistNameRequest(decoded)
	return nil
}

func (request *CreateChecklistItemRequest) UnmarshalJSON(data []byte) error {
	type plain CreateChecklistItemRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Text = strings.TrimSpace(value.Text)
	}); err != nil {
		return err
	}
	*request = CreateChecklistItemRequest(decoded)
	return nil
}

func (request *UpdateChecklistItemRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateChecklistItemRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		trimOptional(value.Text)
	}); err != nil {
		return err
	}
	*request = UpdateChecklistItemRequest(decoded)
	return nil
}
