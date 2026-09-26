package service

import (
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestUserServiceGetAndUpdateProfile(t *testing.T) {
	user := &domain.User{ID: 7, Username: "old"}
	updated := false
	repo := &mockUserRepository{
		getByIDFn: func(id uint) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { updated = true; return nil },
	}
	svc := NewUserService(repo)

	got, err := svc.GetUserByID(7)
	if err != nil || got != user {
		t.Fatalf("got=%+v err=%v", got, err)
	}

	username, first, last, bio, tz := "new", "A", "B", "about", "Europe/Berlin"
	got, err = svc.UpdateProfile(7, UpdateProfileParams{
		Username: &username, FirstName: &first, LastName: &last, Bio: &bio, TimeZone: &tz,
	})
	if err != nil || !updated {
		t.Fatalf("got=%+v err=%v updated=%v", got, err, updated)
	}
	if got.Username != "new" || got.FirstName != "A" || got.LastName != "B" || got.Bio != "about" || got.TimeZone != "Europe/Berlin" {
		t.Fatalf("unexpected user=%+v", got)
	}
}

func TestUserServiceMapsGetErrorToNotFound(t *testing.T) {
	repo := &mockUserRepository{
		getByIDFn: func(uint) (*domain.User, error) { return nil, errors.New("db") },
		updateFn:  func(*domain.User) error { return nil },
	}
	svc := NewUserService(repo)
	if _, err := svc.GetUserByID(1); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.UpdateProfile(1, UpdateProfileParams{}); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestUserServiceWrapsUpdateError(t *testing.T) {
	want := errors.New("db")
	repo := &mockUserRepository{
		getByIDFn: func(uint) (*domain.User, error) { return &domain.User{ID: 1}, nil },
		updateFn:  func(*domain.User) error { return want },
	}
	if _, err := NewUserService(repo).UpdateProfile(1, UpdateProfileParams{}); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}
