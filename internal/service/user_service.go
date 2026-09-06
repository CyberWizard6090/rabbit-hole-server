package service

import (
	"errors"
	"fmt"

	"rabbit-hole-server/internal/domain"
)

var ErrUserNotFound = errors.New("user not found")

type UpdateProfileParams struct {
	Username  *string
	FirstName *string
	LastName  *string
	Bio       *string
	TimeZone  *string
}

type UserService interface {
	GetUserByID(uid uint) (*domain.User, error)
	UpdateProfile(uid uint, params UpdateProfileParams) (*domain.User, error)
}

type userService struct {
	repo domain.UserRepository
}

func NewUserService(repo domain.UserRepository) UserService {
	return &userService{repo: repo}
}

func (s *userService) GetUserByID(uid uint) (*domain.User, error) {
	user, err := s.repo.GetByID(uid)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *userService) UpdateProfile(uid uint, params UpdateProfileParams) (*domain.User, error) {
	user, err := s.repo.GetByID(uid)
	if err != nil {
		return nil, ErrUserNotFound
	}

	if params.Username != nil {
		user.Username = *params.Username
	}
	if params.FirstName != nil {
		user.FirstName = *params.FirstName
	}
	if params.LastName != nil {
		user.LastName = *params.LastName
	}
	if params.Bio != nil {
		user.Bio = *params.Bio
	}
	if params.TimeZone != nil {
		user.TimeZone = *params.TimeZone
	}

	if err := s.repo.Update(user); err != nil {
		return nil, fmt.Errorf("update user in repo: %w", err)
	}

	return user, nil
}
