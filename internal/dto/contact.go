package dto

type AddContactRequest struct {
	ContactID uint `json:"contact_id" binding:"required"`
}
