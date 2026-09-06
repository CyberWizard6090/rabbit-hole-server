package repository

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type ContactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) *ContactRepository {
	return &ContactRepository{db: db}
}

func (r *ContactRepository) AddContact(userID, contactID uint) error {
	return r.db.Exec("INSERT INTO user_contacts (user_id, contact_id) VALUES (?, ?)", userID, contactID).Error
}

func (r *ContactRepository) GetContacts(userID uint) ([]domain.User, error) {
	var contacts []domain.User
	err := r.db.Table("users").
		Joins("JOIN user_contacts ON user_contacts.contact_id = users.id").
		Where("user_contacts.user_id = ?", userID).
		Find(&contacts).Error
	return contacts, err
}

func (r *ContactRepository) DeleteContact(userID, contactID uint) error {

	return r.db.Exec("DELETE FROM user_contacts WHERE user_id = ? AND contact_id = ?",
		userID, contactID).Error
}

func (r *ContactRepository) SearchUsers(query string, excludeID uint) ([]domain.User, error) {
	var users []domain.User

	err := r.db.Where("(username ILIKE ? OR email ILIKE ?) AND id != ?",
		"%"+query+"%", "%"+query+"%", excludeID).
		Limit(20).
		Find(&users).Error
	return users, err
}
