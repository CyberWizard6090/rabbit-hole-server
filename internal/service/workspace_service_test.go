package service

import (
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestWorkspaceServiceCreateTrimsAndValidates(t *testing.T) {
	repo := &workspaceRepositoryMock{
		createFn: func(w *domain.Workspace, owner uint) error {
			if owner != 4 {
				t.Fatalf("owner=%d", owner)
			}
			w.ID = 10
			return nil
		},
		getByIDFn:       func(uint) (*domain.Workspace, error) { return nil, nil },
		getAllForUserFn: func(uint) ([]domain.Workspace, error) { return nil, nil },
	}
	svc := NewWorkspaceService(repo)

	got, err := svc.CreateWorkspace(4, "  My work  ", "  description  ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "My work" || got.Description != "description" || got.OwnerID != 4 || got.ID != 10 {
		t.Fatalf("workspace=%+v", got)
	}
	if _, err := svc.CreateWorkspace(4, "   ", "x"); !errors.Is(err, ErrWorkspaceNameRequired) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.CreateWorkspace(0, "x", "y"); err == nil {
		t.Fatal("expected user id error")
	}
}

func TestWorkspaceServiceGetAndList(t *testing.T) {
	repo := &workspaceRepositoryMock{
		createFn:        func(*domain.Workspace, uint) error { return nil },
		getByIDFn:       func(uint) (*domain.Workspace, error) { return &domain.Workspace{Name: "W"}, nil },
		getAllForUserFn: func(id uint) ([]domain.Workspace, error) { return []domain.Workspace{{OwnerID: id}}, nil },
	}
	svc := NewWorkspaceService(repo)
	got, err := svc.GetWorkspaceByID(1)
	if err != nil || got.Name != "W" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	list, err := svc.GetAllForUser(2)
	if err != nil || len(list) != 1 || list[0].OwnerID != 2 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func TestWorkspaceServiceMapsNilWorkspaceAndPropagatesErrors(t *testing.T) {
	want := errors.New("db")
	repo := &workspaceRepositoryMock{
		createFn:        func(*domain.Workspace, uint) error { return want },
		getByIDFn:       func(uint) (*domain.Workspace, error) { return nil, nil },
		getAllForUserFn: func(uint) ([]domain.Workspace, error) { return nil, want },
	}
	svc := NewWorkspaceService(repo)
	if _, err := svc.GetWorkspaceByID(1); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.GetAllForUser(1); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.CreateWorkspace(1, "x", ""); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}
