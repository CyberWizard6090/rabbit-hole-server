package service

import (
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestStatusServiceCreateClampsPositionAndShifts(t *testing.T) {
	repo := newMockStatusRepository()
	repo.getAllByListFn = func(uint) ([]domain.TaskStatus, error) {
		return []domain.TaskStatus{{Position: 1}, {Position: 2}, {Position: 3}}, nil
	}
	list := &domain.List{SpaceID: 42}
	repoCalls := []string{}
	repo.createFn = func(s *domain.TaskStatus) error { repoCalls = append(repoCalls, "create"); return nil }
	spaceRepo := newMockSpaceRepository()
	spaceRepo.getListByIDFn = func(id uint) (*domain.List, error) {
		if id != 7 {
			t.Fatalf("list id=%d", id)
		}
		return list, nil
	}

	svc := NewStatusService(repo, spaceRepo)
	position := 2
	got, err := svc.Create(CreateStatusParams{ListID: 7, Name: "Doing", Color: "#fff000", Position: &position, Type: int(domain.StatusInProgress)})
	if err != nil {
		t.Fatal(err)
	}
	if got.SpaceID != 42 || got.ListID != 7 || got.Position != 2 || got.Type != domain.StatusInProgress {
		t.Fatalf("status=%+v", got)
	}
	if len(repoCalls) != 1 || repoCalls[0] != "create" {
		t.Fatalf("calls=%v", repoCalls)
	}
}

func TestStatusServiceCreateWithoutPositionAppends(t *testing.T) {
	repo := newMockStatusRepository()
	repo.getAllByListFn = func(uint) ([]domain.TaskStatus, error) { return []domain.TaskStatus{{}, {}}, nil }
	created := &domain.TaskStatus{}
	repo.createFn = func(s *domain.TaskStatus) error { *created = *s; return nil }
	spaceRepo := newMockSpaceRepository()
	spaceRepo.getListByIDFn = func(uint) (*domain.List, error) { return &domain.List{SpaceID: 5}, nil }
	got, err := NewStatusService(repo, spaceRepo).Create(CreateStatusParams{ListID: 2, Name: "Done", Color: "#00ff00"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Position != 3 || created.Position != 3 {
		t.Fatalf("position got=%d created=%d", got.Position, created.Position)
	}
}

func TestStatusServiceGetAllByListChecksList(t *testing.T) {
	want := []domain.TaskStatus{{ListID: 8}}
	repo := newMockStatusRepository()
	repo.getAllByListFn = func(id uint) ([]domain.TaskStatus, error) {
		if id != 8 {
			t.Fatalf("id=%d", id)
		}
		return want, nil
	}
	spaceRepo := newMockSpaceRepository()
	spaceRepo.getListByIDFn = func(id uint) (*domain.List, error) { return &domain.List{SpaceID: 1}, nil }
	got, err := NewStatusService(repo, spaceRepo).GetAllByList(8)
	if err != nil || len(got) != 1 || got[0].ListID != 8 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestStatusServiceUpdateChecksListAndSupportsPosition(t *testing.T) {
	status := &domain.TaskStatus{ListID: 5, Name: "Old", Color: "#111111", Position: 1}
	repo := newMockStatusRepository()
	repo.getByIDFn = func(id uint) (*domain.TaskStatus, error) { return status, nil }
	called := false
	repo.updatePositionFn = func(s *domain.TaskStatus, p int) error {
		called = true
		if p != 3 || s.Name != "New" {
			t.Fatalf("status=%+v p=%d", s, p)
		}
		s.Position = p
		return nil
	}
	name := "New"
	position := 3
	got, err := NewStatusService(repo, newMockSpaceRepository()).Update(5, 9, UpdateStatusParams{Name: &name, Position: &position})
	if err != nil || !called || got.Position != 3 || got.Name != "New" {
		t.Fatalf("got=%+v err=%v called=%v", got, err, called)
	}
}

func TestStatusServiceUpdateRejectsForeignList(t *testing.T) {
	repo := newMockStatusRepository()
	repo.getByIDFn = func(uint) (*domain.TaskStatus, error) { return &domain.TaskStatus{ListID: 2}, nil }
	if _, err := NewStatusService(repo, newMockSpaceRepository()).Update(1, 9, UpdateStatusParams{}); err == nil {
		t.Fatal("expected foreign-list error")
	}
}

func TestStatusServiceDeleteChecksList(t *testing.T) {
	repo := newMockStatusRepository()
	repo.getByIDFn = func(uint) (*domain.TaskStatus, error) { return &domain.TaskStatus{ListID: 7, Position: 4}, nil }
	deleted := false
	repo.deleteFn = func(listID, statusID uint) error {
		deleted = true
		if listID != 7 || statusID != 9 {
			t.Fatalf("delete=%d,%d", listID, statusID)
		}
		return nil
	}
	if err := NewStatusService(repo, newMockSpaceRepository()).Delete(7, 9); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("repository delete was not called")
	}
}

func TestStatusServicePropagatesErrors(t *testing.T) {
	want := errors.New("failure")
	repo := newMockStatusRepository()
	repo.getByIDFn = func(uint) (*domain.TaskStatus, error) { return nil, want }
	if _, err := NewStatusService(repo, newMockSpaceRepository()).Update(1, 2, UpdateStatusParams{}); !errors.Is(err, want) {
		t.Fatal(err)
	}
	spaceRepo := newMockSpaceRepository()
	spaceRepo.getListByIDFn = func(uint) (*domain.List, error) { return nil, want }
	if _, err := NewStatusService(repo, spaceRepo).Create(CreateStatusParams{ListID: 1}); !errors.Is(err, want) {
		t.Fatal(err)
	}
}
