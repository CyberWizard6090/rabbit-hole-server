package domain

import (
	"context"
	"errors"
	"time"
)

const MaxPendingContactRequests = 100

var (
	ErrContactRequestLimit = errors.New("too many pending contact requests")
	ErrCannotAddSelf       = errors.New("cannot add yourself as a contact")
)

type ContactRequest struct {
	User      CommentUser `json:"user"`
	CreatedAt time.Time   `json:"created_at"`
}

type ContactRepository interface {
	ListContacts(ctx context.Context, userID uint, limit, offset int) ([]CommentUser, int64, error)
	Request(ctx context.Context, requesterID, targetID uint) error
	ListRequests(ctx context.Context, userID uint, incoming bool, limit, offset int) ([]ContactRequest, int64, error)
	Accept(ctx context.Context, userID, requesterID uint) error
	Decline(ctx context.Context, userID, requesterID uint) error
	Cancel(ctx context.Context, userID, targetID uint) error
	Remove(ctx context.Context, userID, contactID uint) error
	SearchUsers(ctx context.Context, query string, excludeID uint) ([]CommentUser, error)
}
