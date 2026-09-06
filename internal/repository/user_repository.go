package repository

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) CreateUser(user *domain.User) error {
	return r.db.Create(user).Error
}

func (r *UserRepository) GetByEmail(email string) (*domain.User, error) {
	var user domain.User
	err := r.db.Where("email = ?", email).First(&user).Error
	return &user, err
}

func (r *UserRepository) GetByID(id uint) (*domain.User, error) {
	var user domain.User
	if err := r.db.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) Update(user *domain.User) error {
	return r.db.Save(user).Error
}

func (r *UserRepository) CreateSession(session *domain.UserSession) error {
	return r.db.Create(session).Error
}

func (r *UserRepository) FindSession(tokenHash string) (*domain.UserSession, error) {
	var session domain.UserSession
	err := r.db.Where("token_hash = ? AND expires_at > NOW()", tokenHash).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *UserRepository) DeleteSession(tokenHash string) error {
	return r.db.Where("token_hash = ?", tokenHash).Delete(&domain.UserSession{}).Error
}

func (r *UserRepository) DeleteAllUserSessions(userID uint) error {
	return r.db.Where("user_id = ?", userID).Delete(&domain.UserSession{}).Error
}
