package domain

type EmailChange struct {
	UserID   uint
	OldEmail string
	NewEmail string
}
