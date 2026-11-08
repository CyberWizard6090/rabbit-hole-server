package service

import (
	"context"
	"strings"

	"rabbit-hole-server/internal/domain"
)

type ContactService interface {
	ListContacts(ctx context.Context, uid uint, limit, offset int) ([]domain.CommentUser, int64, error)
	RequestContact(ctx context.Context, uid, targetID uint) error
	ListRequests(ctx context.Context, uid uint, incoming bool, limit, offset int) ([]domain.ContactRequest, int64, error)
	AcceptRequest(ctx context.Context, uid, requesterID uint) error
	DeclineRequest(ctx context.Context, uid, requesterID uint) error
	CancelRequest(ctx context.Context, uid, targetID uint) error
	RemoveContact(ctx context.Context, uid, contactID uint) error
	SearchUsers(ctx context.Context, uid uint, query string) ([]domain.CommentUser, error)
}

type contactService struct {
	repo domain.ContactRepository
}

func NewContactService(repo domain.ContactRepository) ContactService {
	return &contactService{repo: repo}
}

func (s *contactService) ListContacts(ctx context.Context, uid uint, limit, offset int) ([]domain.CommentUser, int64, error) {
	return s.repo.ListContacts(ctx, uid, limit, offset)
}

func (s *contactService) RequestContact(ctx context.Context, uid, targetID uint) error {
	if uid == targetID {
		return domain.ErrCannotAddSelf
	}
	return s.repo.Request(ctx, uid, targetID)
}

func (s *contactService) ListRequests(ctx context.Context, uid uint, incoming bool, limit, offset int) ([]domain.ContactRequest, int64, error) {
	return s.repo.ListRequests(ctx, uid, incoming, limit, offset)
}

func (s *contactService) AcceptRequest(ctx context.Context, uid, requesterID uint) error {
	return s.repo.Accept(ctx, uid, requesterID)
}

func (s *contactService) DeclineRequest(ctx context.Context, uid, requesterID uint) error {
	return s.repo.Decline(ctx, uid, requesterID)
}

func (s *contactService) CancelRequest(ctx context.Context, uid, targetID uint) error {
	return s.repo.Cancel(ctx, uid, targetID)
}

func (s *contactService) RemoveContact(ctx context.Context, uid, contactID uint) error {
	return s.repo.Remove(ctx, uid, contactID)
}

func (s *contactService) SearchUsers(ctx context.Context, uid uint, query string) ([]domain.CommentUser, error) {
	return s.repo.SearchUsers(ctx, strings.TrimSpace(query), uid)
}
