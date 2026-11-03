package repository

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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
	err := r.db.Create(user).Error
	if err != nil && isEmailUniqueViolation(err) {
		return domain.ErrEmailTaken
	}
	if err != nil && isUsernameUniqueViolation(err) {
		return domain.ErrUsernameTaken
	}
	return err
}

func isEmailUniqueViolation(err error) bool {
	return isUniqueViolationForConstraints(err,
		"uq_users_email",
	)
}

func isUsernameUniqueViolation(err error) bool {
	return isUniqueViolationForConstraints(err,
		"uq_users_username",
	)
}

func isUniqueViolationForConstraints(err error, constraints ...string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}

	for _, constraint := range constraints {
		if pgErr.ConstraintName == constraint {
			return true
		}
	}
	return false
}

func (r *UserRepository) GetByEmail(email string) (*domain.User, error) {
	var user domain.User
	err := r.db.Where("lower(email) = lower(?)", email).First(&user).Error
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
	return r.db.Omit("PasswordHash", "TokenVersion").Save(user).Error
}

func (r *UserRepository) GetTokenVersion(userID uint) (int, error) {
	var version int
	res := r.db.Model(&domain.User{}).Select("token_version").Where("id = ?", userID).Limit(1).Scan(&version)
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, gorm.ErrRecordNotFound
	}
	return version, nil
}

func (r *UserRepository) RevokeAll(userID uint) (int, error) {
	var version int
	err := r.db.Transaction(func(tx *gorm.DB) error {
		return revokeAll(tx, userID, &version)
	})
	return version, err
}

func revokeAll(tx *gorm.DB, userID uint, version *int) error {
	res := tx.Model(&domain.User{}).Where("id = ?", userID).
		UpdateColumn("token_version", gorm.Expr("token_version + 1"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	if err := tx.Where("user_id = ?", userID).Delete(&domain.UserSession{}).Error; err != nil {
		return err
	}
	return tx.Model(&domain.User{}).Select("token_version").Where("id = ?", userID).Scan(version).Error
}

func (r *UserRepository) UpdatePasswordAndRevoke(userID uint, passwordHash string) (int, error) {
	var version int
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.User{}).Where("id = ?", userID).
			UpdateColumn("password_hash", passwordHash).Error; err != nil {
			return err
		}
		return revokeAll(tx, userID, &version)
	})
	return version, err
}

func (r *UserRepository) CountOwnedWorkspaces(userID uint) (int64, error) {
	var count int64
	err := r.db.Model(&domain.Workspace{}).Where("owner_id = ?", userID).Count(&count).Error
	return count, err
}

func (r *UserRepository) Anonymize(userID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var version int
		if err := revokeAll(tx, userID, &version); err != nil {
			return err
		}
		tombstone := map[string]any{
			"email":         fmt.Sprintf("deleted-%d@deleted.invalid", userID),
			"username":      fmt.Sprintf("deleted-%d", userID),
			"password_hash": "!",
			"first_name":    "",
			"last_name":     "",
			"avatar_url":    "",
			"bio":           "",
		}
		if err := tx.Model(&domain.User{}).Where("id = ?", userID).UpdateColumns(tombstone).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM user_contacts WHERE user_id = ? OR contact_id = ?", userID, userID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM task_assignees WHERE user_id = ?", userID).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&domain.Member{}).Error
	})
}

func (r *UserRepository) CreateSession(session *domain.UserSession) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND expires_at < NOW()", session.UserID).
			Delete(&domain.UserSession{}).Error; err != nil {
			return err
		}
		return tx.Create(session).Error
	})
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

func (r *UserRepository) RotateSession(tokenHash string, replacement *domain.UserSession) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND expires_at < NOW()", replacement.UserID).
			Delete(&domain.UserSession{}).Error; err != nil {
			return err
		}
		result := tx.Where("token_hash = ? AND expires_at > NOW()", tokenHash).
			Delete(&domain.UserSession{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Create(replacement).Error
	})
}

func (r *UserRepository) DeleteAllUserSessions(userID uint) error {
	return r.db.Where("user_id = ?", userID).Delete(&domain.UserSession{}).Error
}

func (r *UserRepository) HasSessionCreatedSince(userID uint, since time.Time) (bool, error) {
	var count int64
	err := r.db.Model(&domain.UserSession{}).
		Where("user_id = ? AND created_at >= ?", userID, since).
		Count(&count).Error
	return count > 0, err
}
