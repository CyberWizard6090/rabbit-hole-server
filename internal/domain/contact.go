package domain

import "context"

type ContactRepository interface {
	GetContacts(ctx context.Context, userID uint) ([]User, error)
	GetContactsPage(ctx context.Context, userID uint, limit, offset int) ([]User, int64, error)
	AddContact(ctx context.Context, userID uint, contactID uint) error
	DeleteContact(ctx context.Context, userID uint, contactID uint) error
	SearchUsers(ctx context.Context, query string, excludeID uint) ([]User, error)
}
