package service

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

func TestSpaceServiceCreateSpaceBuildsDefaults(t *testing.T) {
	repo := newMockSpaceRepository()
	repo.createFn = func(s *domain.Space) error { s.ID = 100; return nil }
	repo.createListFn = func(l *domain.List) error { l.ID = 200; return nil }
	statuses := make([]domain.TaskStatus, 0, 3)
	repo.createStatusFn = func(s *domain.TaskStatus) error { statuses = append(statuses, *s); return nil }

	got, err := NewSpaceService(repo).CreateSpace(7, "Work", "USD", 9)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 100 || got.WorkspaceID != 7 || got.OwnerID != 9 || got.Name != "Work" || got.Currency != "USD" {
		t.Fatalf("space=%+v", got)
	}
	if len(statuses) != 3 {
		t.Fatalf("statuses=%d", len(statuses))
	}
	wantNames := []string{"К выполнению", "В работе", "Готово"}
	wantTypes := []domain.StatusType{domain.StatusTodo, domain.StatusInProgress, domain.StatusDone}
	for i, s := range statuses {
		if s.SpaceID != 100 || s.ListID != 200 || s.Position != i+1 || s.Name != wantNames[i] || s.Type != wantTypes[i] {
			t.Fatalf("status[%d]=%+v", i, s)
		}
	}
}

func TestSpaceServiceCreateSpaceStopsOnDependencyError(t *testing.T) {
	want := errors.New("create list failed")
	repo := newMockSpaceRepository()
	repo.createFn = func(s *domain.Space) error { s.ID = 1; return nil }
	repo.createListFn = func(*domain.List) error { return want }
	calls := 0
	repo.createStatusFn = func(*domain.TaskStatus) error { calls++; return nil }
	_, err := NewSpaceService(repo).CreateSpace(1, "x", "USD", 2)
	if !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
	if calls != 0 {
		t.Fatalf("status creates=%d", calls)
	}
}

func TestSpaceServiceSimpleOperations(t *testing.T) {
	repo := newMockSpaceRepository()
	repo.getByIDFn = func(id uint) (*domain.Space, error) { return &domain.Space{Model: gorm.Model{ID: id}, Name: "S"}, nil }
	repo.getAllFn = func(id uint, l, o int) ([]domain.Space, int64, error) {
		if id != 2 || l != 10 || o != 5 {
			t.Fatalf("args=%d,%d,%d", id, l, o)
		}
		return []domain.Space{{WorkspaceID: id}}, 11, nil
	}
	repo.dashboardFn = func(id uint) (*domain.Space, error) { return &domain.Space{Model: gormModel(id)}, nil }
	repo.getListByIDFn = func(uint) (*domain.List, error) { return &domain.List{Name: "old", SpaceID: 3}, nil }
	repo.createListFn = func(l *domain.List) error { l.ID = 4; return nil }
	repo.getListsByFolderFn = func(id uint) ([]domain.List, error) { return []domain.List{{FolderID: &id}}, nil }
	repo.updateListFn = func(l *domain.List) error { l.Name = "saved"; return nil }
	repo.deleteListFn = func(id uint) error {
		if id != 4 {
			t.Fatalf("id=%d", id)
		}
		return nil
	}

	svc := NewSpaceService(repo)
	if got, err := svc.GetSpaceByID(1); err != nil || got.ID != 1 {
		t.Fatalf("GetSpaceByID: %+v %v", got, err)
	}
	if got, total, err := svc.GetAllSpaces(2, 10, 5); err != nil || total != 11 || len(got) != 1 {
		t.Fatalf("GetAllSpaces: %+v %d %v", got, total, err)
	}
	if got, err := svc.GetDashboard(3); err != nil || got.ID != 3 {
		t.Fatalf("GetDashboard: %+v %v", got, err)
	}
	if got, err := svc.CreateList(3, "L", nil); err != nil || got.ID != 4 {
		t.Fatalf("CreateList: %+v %v", got, err)
	}
	got, err := svc.CreateListInFolder(3, 8, "L")
	if err != nil || got.FolderID == nil || *got.FolderID != 8 {
		t.Fatalf("CreateListInFolder: %+v %v", got, err)
	}
	if got, err := svc.GetListsByFolder(8); err != nil || len(got) != 1 {
		t.Fatalf("GetListsByFolder: %+v %v", got, err)
	}
	name := "new"
	got, err = svc.UpdateList(4, &name)
	if err != nil || got.Name != "saved" {
		t.Fatalf("UpdateList: %+v %v", got, err)
	}
	if err := svc.DeleteList(4); err != nil {
		t.Fatal(err)
	}
}

func gormModel(id uint) gorm.Model { return gorm.Model{ID: id} }
