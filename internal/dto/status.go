package dto

type CreateStatusRequest struct {
	Name     string `json:"name" binding:"required,min=1,max=50"`
	Color    string `json:"color" binding:"required,hexcolor,len=7"`
	Position *int   `json:"position" binding:"omitempty,gte=1"`
	Type     int    `json:"type" binding:"min=1,max=3"`
	ListID   uint   `json:"list_id" binding:"required"`
}

type UpdateStatusRequest struct {
	Name     *string `json:"name" binding:"omitempty,min=1,max=50"`
	Color    *string `json:"color" binding:"omitempty,hexcolor,len=7"`
	Position *int    `json:"position" binding:"omitempty,gte=1"`
	Type     *int    `json:"type" binding:"omitempty,min=1,max=3"`
}
