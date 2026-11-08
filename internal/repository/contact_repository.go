package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

const (
	contactPending  = "pending"
	contactAccepted = "accepted"
	contactDeclined = "declined"

	searchResultLimit = 20
)

type ContactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) *ContactRepository {
	return &ContactRepository{db: db}
}

type publicUserRow struct {
	ID        uint
	Username  string
	FirstName string
	LastName  string
	AvatarURL string
}

func (u publicUserRow) toDomain() domain.CommentUser {
	return domain.CommentUser{ID: u.ID, Username: u.Username, FirstName: u.FirstName, LastName: u.LastName, AvatarURL: u.AvatarURL}
}

const publicUserColumns = "users.id, users.username, users.first_name, users.last_name, users.avatar_url"

func isContactDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *ContactRepository) ListContacts(ctx context.Context, userID uint, limit, offset int) ([]domain.CommentUser, int64, error) {
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Table("users").Where(`users.id IN (
			SELECT contact_id FROM user_contacts WHERE user_id = ? AND status = ?
			UNION
			SELECT user_id FROM user_contacts WHERE contact_id = ? AND status = ?)`,
			userID, contactAccepted, userID, contactAccepted)
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	var rows []publicUserRow
	if err := base().Select(publicUserColumns).Order("users.id ASC").Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	return nonNilUsers(mapSlice(rows, publicUserRow.toDomain)), total, nil
}

func (r *ContactRepository) Request(ctx context.Context, requesterID, targetID uint) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var pending int64
		if err := tx.Table("user_contacts").Where("user_id = ? AND status = ?", requesterID, contactPending).Count(&pending).Error; err != nil {
			return err
		}
		if pending >= domain.MaxPendingContactRequests {
			return domain.ErrContactRequestLimit
		}

		var known int64
		if err := tx.Table("users").Where("id = ? AND email NOT LIKE ?", targetID, "%@deleted.invalid").Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return nil
		}

		type pairRow struct {
			UserID uint
			Status string
		}
		var existing []pairRow
		if err := tx.Raw(`SELECT user_id, status FROM user_contacts
			WHERE (user_id = ? AND contact_id = ?) OR (user_id = ? AND contact_id = ?) FOR UPDATE`,
			requesterID, targetID, targetID, requesterID).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 0 {
			return tx.Exec("INSERT INTO user_contacts (user_id, contact_id, status) VALUES (?, ?, ?)",
				requesterID, targetID, contactPending).Error
		}
		row := existing[0]
		if row.UserID == requesterID {
			return nil
		}
		switch row.Status {
		case contactPending:
			return tx.Exec("UPDATE user_contacts SET status = ?, responded_at = ? WHERE user_id = ? AND contact_id = ?",
				contactAccepted, time.Now().UTC(), targetID, requesterID).Error
		case contactDeclined:
			if err := tx.Exec("DELETE FROM user_contacts WHERE user_id = ? AND contact_id = ?", targetID, requesterID).Error; err != nil {
				return err
			}
			return tx.Exec("INSERT INTO user_contacts (user_id, contact_id, status) VALUES (?, ?, ?)",
				requesterID, targetID, contactPending).Error
		}
		return nil
	})
	if errors.Is(err, domain.ErrContactRequestLimit) {
		return err
	}
	if err != nil && isContactDuplicate(err) {
		return nil
	}
	return mapErr(err)
}

func (r *ContactRepository) ListRequests(ctx context.Context, userID uint, incoming bool, limit, offset int) ([]domain.ContactRequest, int64, error) {
	ownColumn, otherColumn := "user_contacts.user_id", "user_contacts.contact_id"
	if incoming {
		ownColumn, otherColumn = otherColumn, ownColumn
	}
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Table("user_contacts").
			Joins("JOIN users ON users.id = "+otherColumn).
			Where(ownColumn+" = ? AND user_contacts.status = ?", userID, contactPending)
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	var rows []struct {
		ID          uint
		Username    string
		FirstName   string
		LastName    string
		AvatarURL   string
		RequestedAt time.Time
	}
	err := base().Select(publicUserColumns + ", user_contacts.created_at AS requested_at").
		Order("user_contacts.created_at DESC, users.id ASC").Limit(limit).Offset(offset).Scan(&rows).Error
	if err != nil {
		return nil, 0, mapErr(err)
	}
	out := make([]domain.ContactRequest, len(rows))
	for i, row := range rows {
		out[i] = domain.ContactRequest{
			User:      domain.CommentUser{ID: row.ID, Username: row.Username, FirstName: row.FirstName, LastName: row.LastName, AvatarURL: row.AvatarURL},
			CreatedAt: row.RequestedAt,
		}
	}
	return out, total, nil
}

func (r *ContactRepository) respond(ctx context.Context, userID, requesterID uint, status string) error {
	res := r.db.WithContext(ctx).Exec(
		"UPDATE user_contacts SET status = ?, responded_at = ? WHERE user_id = ? AND contact_id = ? AND status = ?",
		status, time.Now().UTC(), requesterID, userID, contactPending)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ContactRepository) Accept(ctx context.Context, userID, requesterID uint) error {
	return r.respond(ctx, userID, requesterID, contactAccepted)
}

func (r *ContactRepository) Decline(ctx context.Context, userID, requesterID uint) error {
	return r.respond(ctx, userID, requesterID, contactDeclined)
}

func (r *ContactRepository) Cancel(ctx context.Context, userID, targetID uint) error {
	res := r.db.WithContext(ctx).Exec(
		"DELETE FROM user_contacts WHERE user_id = ? AND contact_id = ? AND status = ?", userID, targetID, contactPending)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ContactRepository) Remove(ctx context.Context, userID, contactID uint) error {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM user_contacts WHERE status = ?
		AND ((user_id = ? AND contact_id = ?) OR (user_id = ? AND contact_id = ?))`,
		contactAccepted, userID, contactID, contactID, userID)
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

func (r *ContactRepository) SearchUsers(ctx context.Context, query string, excludeID uint) ([]domain.CommentUser, error) {
	var rows []publicUserRow
	pattern := escapeLike(strings.ToLower(strings.TrimSpace(query))) + "%"
	err := r.db.WithContext(ctx).Table("users").Select(publicUserColumns).
		Where(`users.id <> ? AND users.email NOT LIKE '%@deleted.invalid' AND (
			LOWER(users.username) LIKE ? ESCAPE '\' OR LOWER(users.first_name) LIKE ? ESCAPE '\' OR LOWER(users.last_name) LIKE ? ESCAPE '\')`,
			excludeID, pattern, pattern, pattern).
		Order("users.username ASC, users.id ASC").
		Limit(searchResultLimit).
		Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return nonNilUsers(mapSlice(rows, publicUserRow.toDomain)), nil
}

func nonNilUsers(users []domain.CommentUser) []domain.CommentUser {
	if users == nil {
		return []domain.CommentUser{}
	}
	return users
}
