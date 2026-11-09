package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

const rotatedSessionRetention = "48 hours"

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	m := userFromDomain(*user)
	err := database.DBFromContext(ctx, r.db).Create(&m).Error
	if err != nil && isEmailUniqueViolation(err) {
		return domain.ErrEmailTaken
	}
	if err != nil && isUsernameUniqueViolation(err) {
		return domain.ErrUsernameTaken
	}
	if err != nil {
		return mapErr(err)
	}
	*user = userToDomain(m)
	return nil
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

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var m persistence.User
	if err := database.DBFromContext(ctx, r.db).Where("lower(email) = lower(?)", email).First(&m).Error; err != nil {
		return nil, mapErr(err)
	}
	user := userToDomain(m)
	return &user, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id uint) (*domain.User, error) {
	var m persistence.User
	if err := database.DBFromContext(ctx, r.db).First(&m, id).Error; err != nil {
		return nil, mapErr(err)
	}
	user := userToDomain(m)
	return &user, nil
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	m := userFromDomain(*user)
	if err := database.DBFromContext(ctx, r.db).Omit("PasswordHash", "TokenVersion").Save(&m).Error; err != nil {
		return mapErr(err)
	}
	user.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *UserRepository) CreateSession(ctx context.Context, session *domain.UserSession) error {
	m := sessionFromDomain(*session)
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND expires_at < NOW()", session.UserID).
			Delete(&persistence.UserSession{}).Error; err != nil {
			return err
		}
		return tx.Create(&m).Error
	})
	if err != nil {
		return mapErr(err)
	}
	*session = sessionToDomain(m)
	return nil
}

func (r *UserRepository) FindSession(ctx context.Context, tokenHash string) (*domain.UserSession, error) {
	var m persistence.UserSession
	err := database.DBFromContext(ctx, r.db).Where("token_hash = ? AND expires_at > NOW()", tokenHash).First(&m).Error
	if err != nil {
		return nil, mapErr(err)
	}
	session := sessionToDomain(m)
	return &session, nil
}

func (r *UserRepository) DeleteSessionFamily(ctx context.Context, tokenHash string) error {
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		"DELETE FROM user_sessions WHERE family_id <> '' AND family_id = (SELECT family_id FROM user_sessions WHERE token_hash = ?)",
		tokenHash,
	).Error)
}

func (r *UserRepository) RevokeSessionFamily(ctx context.Context, familyID string) error {
	if familyID == "" {
		return nil
	}
	return mapErr(database.DBFromContext(ctx, r.db).Where("family_id = ?", familyID).Delete(&persistence.UserSession{}).Error)
}

func (r *UserRepository) RotateSession(ctx context.Context, tokenHash string, replacement *domain.UserSession) error {
	m := sessionFromDomain(*replacement)
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND (expires_at < NOW() OR rotated_at < NOW() - ?::interval)",
			replacement.UserID, rotatedSessionRetention).
			Delete(&persistence.UserSession{}).Error; err != nil {
			return err
		}
		var rows []struct {
			ID       uint
			FamilyID string
			Rotated  bool
			AgeSecs  float64
		}
		if err := tx.Raw(`SELECT id, family_id, rotated_at IS NOT NULL AS rotated,
				COALESCE(EXTRACT(EPOCH FROM (NOW() - rotated_at)), 0) AS age_secs
			FROM user_sessions WHERE token_hash = ? AND expires_at > NOW() FOR UPDATE`, tokenHash).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return domain.ErrNotFound
		}
		row := rows[0]
		if row.Rotated {
			return &domain.SessionReplay{FamilyID: row.FamilyID, Age: time.Duration(row.AgeSecs * float64(time.Second))}
		}
		if err := tx.Model(&persistence.UserSession{}).Where("id = ?", row.ID).
			UpdateColumn("rotated_at", gorm.Expr("NOW()")).Error; err != nil {
			return err
		}
		m.FamilyID = row.FamilyID
		return tx.Create(&m).Error
	})
	var replay *domain.SessionReplay
	if errors.As(err, &replay) {
		return replay
	}
	if err != nil {
		return mapErr(err)
	}
	*replacement = sessionToDomain(m)
	return nil
}

func (r *UserRepository) DeleteAllUserSessions(ctx context.Context, userID uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Where("user_id = ?", userID).Delete(&persistence.UserSession{}).Error)
}

func (r *UserRepository) GetTokenVersion(ctx context.Context, userID uint) (int, error) {
	var version int
	res := database.DBFromContext(ctx, r.db).Model(&persistence.User{}).Select("token_version").Where("id = ?", userID).Limit(1).Scan(&version)
	if res.Error != nil {
		return 0, mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return 0, domain.ErrNotFound
	}
	return version, nil
}

func (r *UserRepository) RevokeAll(ctx context.Context, userID uint) (int, error) {
	var version int
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		return revokeAll(tx, userID, &version)
	})
	return version, mapErr(err)
}

func revokeAll(tx *gorm.DB, userID uint, version *int) error {
	res := tx.Model(&persistence.User{}).Where("id = ?", userID).
		UpdateColumn("token_version", gorm.Expr("token_version + 1"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	if err := tx.Where("user_id = ?", userID).Delete(&persistence.UserSession{}).Error; err != nil {
		return err
	}
	return tx.Model(&persistence.User{}).Select("token_version").Where("id = ?", userID).Scan(version).Error
}

func (r *UserRepository) UpdatePasswordAndRevoke(ctx context.Context, userID uint, passwordHash string) (int, error) {
	var version int
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&persistence.User{}).Where("id = ?", userID).
			UpdateColumn("password_hash", passwordHash).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM email_tokens WHERE user_id = ? AND kind = 'reset_password'", userID).Error; err != nil {
			return err
		}
		return revokeAll(tx, userID, &version)
	})
	return version, mapErr(err)
}

func (r *UserRepository) AnonymizeIfNoOwnedWorkspaces(ctx context.Context, userID uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var users []struct {
			ID           uint
			PasswordHash string
		}
		if err := tx.Raw("SELECT id, password_hash FROM users WHERE id = ? FOR UPDATE", userID).Scan(&users).Error; err != nil {
			return err
		}
		if len(users) == 0 || users[0].PasswordHash == "!" {
			return domain.ErrNotFound
		}
		var owned int64
		if err := tx.Model(&persistence.Workspace{}).Where("owner_id = ?", userID).Count(&owned).Error; err != nil {
			return err
		}
		if owned > 0 {
			return domain.ErrOwnsWorkspaces
		}

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
		if err := tx.Exec("DELETE FROM email_tokens WHERE user_id = ?", userID).Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE users SET email_verified_at = NULL WHERE id = ?", userID).Error; err != nil {
			return err
		}
		if err := tx.Model(&persistence.User{}).Where("id = ?", userID).UpdateColumns(tombstone).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM user_contacts WHERE user_id = ? OR contact_id = ?", userID, userID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM task_assignees WHERE user_id = ?", userID).Error; err != nil {
			return err
		}
		for _, table := range []string{
			"notifications", "task_watchers", "notification_preferences", "notification_settings", "notification_channels",
		} {
			if err := tx.Exec("DELETE FROM "+table+" WHERE user_id = ?", userID).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("DELETE FROM task_comment_mentions WHERE user_id = ?", userID).Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE task_checklist_items SET assignee_id = NULL WHERE assignee_id = ?", userID).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&persistence.Member{}).Error
	}))
}
