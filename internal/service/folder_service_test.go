package service

import (
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestFolderServiceCreateAndList(t *testing.T) {
	repo := &folderRepositoryMock{
		createFn:  func(f *domain.Folder) error { f.ID = 10; return nil },
		getByIDFn: func(uint) (*domain.Folder, error) { return nil, nil },
		getAllBySpaceFn: func(spaceID uint) ([]domain.Folder, error) {
			return []domain.Folder{{SpaceID: spaceID, Name: "docs"}}, nil
		},
		updateFn: func(*domain.Folder) error { return nil }, deleteFn: func(uint) error { return nil },
	}
	s := NewFolderService(repo)
	f, err := s.CreateFolder(5, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if f.SpaceID != 5 || f.Name != "docs" || f.ID != 10 {
		t.Fatalf("unexpected folder: %+v", f)
	}
	folders, err := s.GetAllFolders(5)
	if err != nil || len(folders) != 1 || folders[0].SpaceID != 5 {
		t.Fatalf("folders=%+v err=%v", folders, err)
	}
}

func TestFolderServiceUpdateAndDelete(t *testing.T) {
	folder := &domain.Folder{Name: "old"}
	updated := false
	deleted := uint(0)
	repo := &folderRepositoryMock{
		createFn: func(*domain.Folder) error { return nil },
		getByIDFn: func(id uint) (*domain.Folder, error) {
			if id != 3 {
				t.Fatalf("id=%d", id)
			}
			return folder, nil
		},
		getAllBySpaceFn: func(uint) ([]domain.Folder, error) { return nil, nil },
		updateFn: func(f *domain.Folder) error {
			updated = true
			if f.Name != "new" {
				t.Fatalf("name=%q", f.Name)
			}
			return nil
		},
		deleteFn: func(id uint) error { deleted = id; return nil },
	}
	s := NewFolderService(repo)
	name := "new"
	got, err := s.UpdateFolder(3, &name)
	if err != nil || got.Name != "new" || !updated {
		t.Fatalf("got=%+v err=%v updated=%v", got, err, updated)
	}
	if err := s.DeleteFolder(3); err != nil || deleted != 3 {
		t.Fatalf("err=%v deleted=%d", err, deleted)
	}
}

func TestFolderServicePropagatesErrors(t *testing.T) {
	want := errors.New("repo error")
	repo := &folderRepositoryMock{
		createFn:        func(*domain.Folder) error { return want },
		getByIDFn:       func(uint) (*domain.Folder, error) { return nil, want },
		getAllBySpaceFn: func(uint) ([]domain.Folder, error) { return nil, want },
		updateFn:        func(*domain.Folder) error { return want }, deleteFn: func(uint) error { return want },
	}
	s := NewFolderService(repo)
	if _, err := s.CreateFolder(1, "x"); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if _, err := s.GetAllFolders(1); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if _, err := s.GetFolderByID(1); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if _, err := s.UpdateFolder(1, nil); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(1); !errors.Is(err, want) {
		t.Fatal(err)
	}
}
