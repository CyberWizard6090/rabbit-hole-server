package service

import (
	"context"
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
		getByIDFn:     func(uint) (*domain.Workspace, error) { return nil, nil },
		listForUserFn: func(uint) ([]domain.WorkspaceSummary, int64, error) { return nil, 0, nil },
	}
	svc := NewWorkspaceService(repo, nil)

	got, err := svc.CreateWorkspace(context.Background(), 4, "  My work  ", "  description  ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "My work" || got.Description != "description" || got.OwnerID != 4 || got.ID != 10 {
		t.Fatalf("workspace=%+v", got)
	}
	if _, err := svc.CreateWorkspace(context.Background(), 4, "   ", "x"); !errors.Is(err, ErrWorkspaceNameRequired) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.CreateWorkspace(context.Background(), 0, "x", "y"); err == nil {
		t.Fatal("expected user id error")
	}
}

func TestWorkspaceServiceGetAndList(t *testing.T) {
	repo := &workspaceRepositoryMock{
		createFn:  func(*domain.Workspace, uint) error { return nil },
		getByIDFn: func(uint) (*domain.Workspace, error) { return &domain.Workspace{Name: "W"}, nil },
		listForUserFn: func(id uint) ([]domain.WorkspaceSummary, int64, error) {
			return []domain.WorkspaceSummary{{Workspace: domain.Workspace{OwnerID: id}, Role: "owner"}}, 1, nil
		},
	}
	svc := NewWorkspaceService(repo, nil)
	got, err := svc.GetWorkspaceByID(context.Background(), 1)
	if err != nil || got.Name != "W" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	list, total, err := svc.ListForUser(context.Background(), 2, 50, 0)
	if err != nil || total != 1 || len(list) != 1 || list[0].OwnerID != 2 || list[0].Role != "owner" {
		t.Fatalf("list=%+v total=%d err=%v", list, total, err)
	}
}

func TestWorkspaceServiceMapsNilWorkspaceAndPropagatesErrors(t *testing.T) {
	want := errors.New("db")
	repo := &workspaceRepositoryMock{
		createFn:      func(*domain.Workspace, uint) error { return want },
		getByIDFn:     func(uint) (*domain.Workspace, error) { return nil, nil },
		listForUserFn: func(uint) ([]domain.WorkspaceSummary, int64, error) { return nil, 0, want },
	}
	svc := NewWorkspaceService(repo, nil)
	if _, err := svc.GetWorkspaceByID(context.Background(), 1); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("err=%v", err)
	}
	if _, _, err := svc.ListForUser(context.Background(), 1, 50, 0); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.CreateWorkspace(context.Background(), 1, "x", ""); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}
