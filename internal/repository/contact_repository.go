package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type ContactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) *ContactRepository {
	return &ContactRepository{db: db}
}

func (r *ContactRepository) AddContact(ctx context.Context, userID, contactID uint) error {
	err := database.DBFromContext(ctx, r.db).Exec("INSERT INTO user_contacts (user_id, contact_id) VALUES (?, ?)", userID, contactID).Error
	if err != nil && isContactDuplicate(err) {
		return domain.ErrContactAlreadyAdded
	}
	return mapErr(err)
}

func isContactDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *ContactRepository) GetContacts(ctx context.Context, userID uint) ([]domain.User, error) {
	contacts, _, err := r.GetContactsPage(ctx, userID, 1000, 0)
	return contacts, err
}

func (r *ContactRepository) GetContactsPage(ctx context.Context, userID uint, limit, offset int) ([]domain.User, int64, error) {
	var rows []persistence.User
	var total int64

	baseQuery := database.DBFromContext(ctx, r.db).Table("users").
		Joins("JOIN user_contacts ON user_contacts.contact_id = users.id").
		Where("user_contacts.user_id = ?", userID)

	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}

	err := baseQuery.
		Order("users.id ASC").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error
	if err != nil {
		return nil, 0, mapErr(err)
	}
	return mapSlice(rows, userToDomain), total, nil
}

func (r *ContactRepository) DeleteContact(ctx context.Context, userID, contactID uint) error {
	res := database.DBFromContext(ctx, r.db).Exec("DELETE FROM user_contacts WHERE user_id = ? AND contact_id = ?", userID, contactID)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (r *ContactRepository) SearchUsers(ctx context.Context, query string, excludeID uint) ([]domain.User, error) {
	var rows []persistence.User

	query = strings.ToLower(strings.TrimSpace(query))
	err := database.DBFromContext(ctx, r.db).
		Where(`(LOWER(username) LIKE ? ESCAPE '\' OR LOWER(email) = ?) AND id != ?`,
			escapeLike(query)+"%", query, excludeID).
		Order("username ASC, id ASC").
		Limit(20).
		Find(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, userToDomain), nil
}
