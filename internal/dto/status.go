package dto

type CreateStatusRequest struct {
	Name     string `json:"name" binding:"required,min=2,max=50"`
	Color    string `json:"color" binding:"required,hexcolor"`
	Position int    `json:"position" binding:"gte=0"`
	Type     int    `json:"type" binding:"min=0,max=3"`
	ListID   uint   `json:"board_id" binding:"required"`
}

type UpdateStatusRequest struct {
	Name     *string `json:"name" binding:"omitempty,min=2,max=50"`
	Color    *string `json:"color" binding:"omitempty,hexcolor"`
	Position *int    `json:"position" binding:"omitempty,gte=0"`
	Type     *int    `json:"type" binding:"omitempty,min=0,max=3"`
}
