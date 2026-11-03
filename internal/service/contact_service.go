package service

import (
	"context"

	"rabbit-hole-server/internal/domain"
)

type ContactService interface {
	GetContacts(ctx context.Context, uid uint) ([]domain.User, error)
	GetContactsPage(ctx context.Context, uid uint, limit, offset int) ([]domain.User, int64, error)
	AddContact(ctx context.Context, uid uint, contactID uint) error
	DeleteContact(ctx context.Context, uid uint, contactID uint) error
	SearchUsers(ctx context.Context, uid uint, query string) ([]domain.User, error)
}

type contactService struct {
	repo domain.ContactRepository
}

func NewContactService(repo domain.ContactRepository) ContactService {
	return &contactService{repo: repo}
}

func (s *contactService) GetContacts(ctx context.Context, uid uint) ([]domain.User, error) {
	return s.repo.GetContacts(ctx, uid)
}

func (s *contactService) GetContactsPage(ctx context.Context, uid uint, limit, offset int) ([]domain.User, int64, error) {
	return s.repo.GetContactsPage(ctx, uid, limit, offset)
}

func (s *contactService) AddContact(ctx context.Context, uid uint, contactID uint) error {
	return s.repo.AddContact(ctx, uid, contactID)
}

func (s *contactService) DeleteContact(ctx context.Context, uid uint, contactID uint) error {
	return s.repo.DeleteContact(ctx, uid, contactID)
}

func (s *contactService) SearchUsers(ctx context.Context, uid uint, query string) ([]domain.User, error) {
	return s.repo.SearchUsers(ctx, query, uid)
}
