package dto

import "rabbit-hole-server/internal/domain"

type CreateSpaceRequest struct {
	Name     string `json:"name" binding:"required,min=1,max=255"`
	Currency string `json:"currency" binding:"omitempty,len=3,alpha"`
	Template string `json:"template" binding:"omitempty,max=50"`
}

type CreateListRequest struct {
	Name           string `json:"name" binding:"required,min=1,max=255"`
	ParentStatusID *uint  `json:"parent_status_id"`
	Template       string `json:"template" binding:"omitempty,max=50"`
}

type UpdateListRequest struct {
	Name     *string               `json:"name" binding:"omitempty,min=1,max=255"`
	FolderID domain.Nullable[uint] `json:"folder_id"`
}

type UpdateSpaceRequest struct {
	Name     *string `json:"name" binding:"omitempty,min=1,max=255"`
	Currency *string `json:"currency" binding:"omitempty,len=3,alpha"`
}

type SavedViewRequest struct {
	Name    string            `json:"name" binding:"required,min=1,max=400"`
	ListID  *uint             `json:"list_id" binding:"omitempty,gt=0"`
	Shared  bool              `json:"shared"`
	Filters map[string]string `json:"filters" binding:"max=20,dive,keys,max=40,endkeys,max=500"`
	Sort    string            `json:"sort" binding:"max=200"`
}

type UpdateSavedViewRequest struct {
	Name    *string           `json:"name" binding:"omitempty,min=1,max=400"`
	Shared  *bool             `json:"shared"`
	Filters map[string]string `json:"filters" binding:"max=20,dive,keys,max=40,endkeys,max=500"`
	Sort    *string           `json:"sort" binding:"omitempty,max=200"`
}
