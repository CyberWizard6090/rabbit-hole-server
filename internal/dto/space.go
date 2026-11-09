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
