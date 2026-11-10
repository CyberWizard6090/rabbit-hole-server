package dto

type RehostImageRequest struct {
	URL string `json:"url" binding:"required,max=2048"`
}
