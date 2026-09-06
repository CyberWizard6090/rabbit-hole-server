package domain

type ContactRepository interface {
	GetContacts(userID uint) ([]User, error)
	AddContact(userID uint, contactID uint) error
	DeleteContact(userID uint, contactID uint) error
	SearchUsers(query string, excludeID uint) ([]User, error)
}
