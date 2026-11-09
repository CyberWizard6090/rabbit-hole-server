package service

import (
	"context"
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestSpaceServiceCreateSpaceBuildsDefaults(t *testing.T) {
	repo := newMockSpaceRepository()
	var gotList *domain.List
	var statuses []domain.TaskStatus
	repo.createWithDefaultsFn = func(space *domain.Space, list *domain.List, st []domain.TaskStatus) error {
		space.ID, list.ID = 100, 200
		gotList = list
		for i := range st {
			st[i].SpaceID, st[i].ListID = space.ID, list.ID
		}
		statuses = st
		return nil
	}

	got, err := NewSpaceService(repo).CreateSpace(context.Background(), 7, "Work", "USD", 9)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 100 || got.WorkspaceID != 7 || got.OwnerID != 9 || got.Name != "Work" || got.Currency != "USD" {
		t.Fatalf("space=%+v", got)
	}
	if gotList == nil || gotList.Name == "" {
		t.Fatalf("default list=%+v", gotList)
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

func TestSpaceServiceCreateSpaceReturnsTransactionError(t *testing.T) {
	want := errors.New("transaction failed")
	repo := newMockSpaceRepository()
	repo.createWithDefaultsFn = func(*domain.Space, *domain.List, []domain.TaskStatus) error { return want }

	got, err := NewSpaceService(repo).CreateSpace(context.Background(), 1, "x", "USD", 2)
	if !errors.Is(err, want) || got != nil {
		t.Fatalf("got=%v err=%v, want nil space and the repository error", got, err)
	}
}

func TestSpaceServiceSimpleOperations(t *testing.T) {
	repo := newMockSpaceRepository()
	repo.getByIDFn = func(id uint) (*domain.Space, error) {
		return &domain.Space{Model: domain.Model{ID: id}, Name: "S"}, nil
	}
	repo.getAllFn = func(id uint, l, o int) ([]domain.Space, int64, error) {
		if id != 2 || l != 10 || o != 5 {
			t.Fatalf("args=%d,%d,%d", id, l, o)
		}
		return []domain.Space{{WorkspaceID: id}}, 11, nil
	}
	repo.dashboardFn = func(id uint, perList int) (*domain.Space, error) { return &domain.Space{Model: gormModel(id)}, nil }
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
	if got, err := svc.GetSpaceByID(context.Background(), 1); err != nil || got.ID != 1 {
		t.Fatalf("GetSpaceByID: %+v %v", got, err)
	}
	if got, total, err := svc.GetAllSpaces(context.Background(), 2, 10, 5); err != nil || total != 11 || len(got) != 1 {
		t.Fatalf("GetAllSpaces: %+v %d %v", got, total, err)
	}
	if got, err := svc.GetDashboard(context.Background(), 3, 50); err != nil || got.ID != 3 {
		t.Fatalf("GetDashboard: %+v %v", got, err)
	}
	if got, err := svc.CreateList(context.Background(), 3, "L", nil); err != nil || got.ID != 4 {
		t.Fatalf("CreateList: %+v %v", got, err)
	}
	got, err := svc.CreateListInFolder(context.Background(), 3, 8, "L")
	if err != nil || got.FolderID == nil || *got.FolderID != 8 {
		t.Fatalf("CreateListInFolder: %+v %v", got, err)
	}
	if got, err := svc.GetListsByFolder(context.Background(), 8); err != nil || len(got) != 1 {
		t.Fatalf("GetListsByFolder: %+v %v", got, err)
	}
	name := "new"
	got, err = svc.UpdateList(context.Background(), 4, &name, domain.Nullable[uint]{})
	if err != nil || got.Name != "saved" {
		t.Fatalf("UpdateList: %+v %v", got, err)
	}
	if err := svc.DeleteList(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
}

func gormModel(id uint) domain.Model { return domain.Model{ID: id} }
