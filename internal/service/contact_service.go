package service

import "rabbit-hole-server/internal/domain"

type ContactService interface {
	GetContacts(uid uint) ([]domain.User, error)
	AddContact(uid uint, contactID uint) error
	DeleteContact(uid uint, contactID uint) error
	SearchUsers(uid uint, query string) ([]domain.User, error)
}

type contactService struct {
	repo domain.ContactRepository
}

func NewContactService(repo domain.ContactRepository) ContactService {
	return &contactService{repo: repo}
}

func (s *contactService) GetContacts(uid uint) ([]domain.User, error) {
	return s.repo.GetContacts(uid)
}

func (s *contactService) AddContact(uid uint, contactID uint) error {
	return s.repo.AddContact(uid, contactID)
}

func (s *contactService) DeleteContact(uid uint, contactID uint) error {
	return s.repo.DeleteContact(uid, contactID)
}

func (s *contactService) SearchUsers(uid uint, query string) ([]domain.User, error) {
	return s.repo.SearchUsers(query, uid)
}
