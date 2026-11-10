package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/storage"
)

type avatarRepoFake struct {
	domain.FileRepository
	replaceErr error
	replaced   []domain.File
	clearErr   error
	cleared    []domain.File
	created    []domain.NewFile
	clearCalls []uint
	avatarURL  string
}

func (r *avatarRepoFake) ReplaceWorkspaceAvatar(_ context.Context, f domain.NewFile, url string) (*domain.File, []domain.File, error) {
	if r.replaceErr != nil {
		return nil, nil, r.replaceErr
	}
	r.created = append(r.created, f)
	r.avatarURL = url
	return &domain.File{ID: 1, StorageKey: f.StorageKey}, r.replaced, nil
}

func (r *avatarRepoFake) ClearWorkspaceAvatar(_ context.Context, workspaceID uint) ([]domain.File, error) {
	r.clearCalls = append(r.clearCalls, workspaceID)
	if r.clearErr != nil {
		return nil, r.clearErr
	}
	r.avatarURL = ""
	return r.cleared, nil
}

type recordingStore struct {
	putErr  error
	puts    []string
	deletes []string
}

func (s *recordingStore) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.puts = append(s.puts, key)
	_, err := io.Copy(io.Discard, body)
	return err
}

func (s *recordingStore) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, storage.ErrNotFound
}

func (s *recordingStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	return nil
}

func (s *recordingStore) Ping(context.Context) error { return nil }

func newAvatarService(repo domain.FileRepository, store storage.Store, limit int64) FileService {
	return NewFileService(repo, store, nil, FileLimits{Avatar: limit}, "http://cdn.test/", nil, RehostConfig{})
}

func pngUpload(t *testing.T) Upload {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 40, 30))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	return Upload{Name: "logo.png", Size: int64(len(data)), Reader: bytes.NewReader(data)}
}

func TestSetWorkspaceAvatarStoresAndReplaces(t *testing.T) {
	repo := &avatarRepoFake{replaced: []domain.File{{StorageKey: "workspace_avatar/old"}}}
	store := &recordingStore{}
	svc := newAvatarService(repo, store, 1<<20)

	url, err := svc.SetWorkspaceAvatar(context.Background(), 7, 3, pngUpload(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "http://cdn.test/api/v1/images/") || len(url) != len("http://cdn.test/api/v1/images/")+32 {
		t.Fatalf("url = %q", url)
	}
	if len(store.puts) != 1 || !strings.HasPrefix(store.puts[0], "workspace_avatar/") {
		t.Fatalf("stored = %v", store.puts)
	}
	if len(store.deletes) != 1 || store.deletes[0] != "workspace_avatar/old" {
		t.Fatalf("deleted = %v, want only the previous object", store.deletes)
	}
	got := repo.created[0]
	if got.Kind != domain.FileKindWorkspaceAvatar || got.UserID != 7 || got.WorkspaceID == nil || *got.WorkspaceID != 3 || got.StorageKey != store.puts[0] {
		t.Fatalf("new file = %+v", got)
	}
	if repo.avatarURL != url {
		t.Fatalf("repository got %q, service returned %q", repo.avatarURL, url)
	}
}

func TestSetWorkspaceAvatarRejectsBeforeTouchingStorage(t *testing.T) {
	big := bytes.Repeat([]byte{1}, 2000)
	cases := map[string]struct {
		upload Upload
		want   error
	}{
		"declared too large": {Upload{Size: 2000, Reader: bytes.NewReader(big)}, domain.ErrFileTooLarge},
		"streamed too large": {Upload{Size: 0, Reader: bytes.NewReader(big)}, domain.ErrFileTooLarge},
		"empty":              {Upload{Reader: bytes.NewReader(nil)}, domain.ErrEmptyFile},
		"not an image":       {Upload{Size: 9, Reader: strings.NewReader("plain txt")}, domain.ErrUnsupportedFileType},
		"svg":                {Upload{Size: 40, Reader: strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg"/>`)}, domain.ErrUnsupportedFileType},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc := newAvatarService(nil, nil, 1000)
			if _, err := svc.SetWorkspaceAvatar(context.Background(), 1, 1, tc.upload); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSetWorkspaceAvatarStorageFailureKeepsTheOldAvatar(t *testing.T) {
	repo := &avatarRepoFake{replaced: []domain.File{{StorageKey: "workspace_avatar/old"}}}
	store := &recordingStore{putErr: errors.New("disk full")}
	svc := newAvatarService(repo, store, 1<<20)

	if _, err := svc.SetWorkspaceAvatar(context.Background(), 1, 1, pngUpload(t)); err == nil {
		t.Fatal("expected the storage error")
	}
	if len(repo.created) != 0 || repo.avatarURL != "" {
		t.Fatal("the database was touched although the object was not stored")
	}
	for _, key := range store.deletes {
		if key == "workspace_avatar/old" {
			t.Fatal("the previous object was deleted")
		}
	}
}

func TestSetWorkspaceAvatarDatabaseFailureRemovesTheNewObjectOnly(t *testing.T) {
	repo := &avatarRepoFake{replaceErr: domain.ErrNotFound, replaced: []domain.File{{StorageKey: "workspace_avatar/old"}}}
	store := &recordingStore{}
	svc := newAvatarService(repo, store, 1<<20)

	if _, err := svc.SetWorkspaceAvatar(context.Background(), 1, 1, pngUpload(t)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(store.puts) != 1 || len(store.deletes) != 1 || store.deletes[0] != store.puts[0] {
		t.Fatalf("puts %v deletes %v: the new object must be cleaned up and nothing else", store.puts, store.deletes)
	}
}

func TestRemoveWorkspaceAvatar(t *testing.T) {
	t.Run("deletes stored objects", func(t *testing.T) {
		repo := &avatarRepoFake{cleared: []domain.File{{StorageKey: "workspace_avatar/a"}, {StorageKey: "workspace_avatar/b"}}}
		store := &recordingStore{}
		svc := newAvatarService(repo, store, 1)
		if err := svc.RemoveWorkspaceAvatar(context.Background(), 5); err != nil {
			t.Fatal(err)
		}
		if len(store.deletes) != 2 || len(repo.clearCalls) != 1 || repo.clearCalls[0] != 5 {
			t.Fatalf("deletes %v clears %v", store.deletes, repo.clearCalls)
		}
	})
	t.Run("idempotent without an avatar and without touching storage", func(t *testing.T) {
		repo := &avatarRepoFake{}
		svc := newAvatarService(repo, nil, 1)
		for range 2 {
			if err := svc.RemoveWorkspaceAvatar(context.Background(), 5); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("repository failure keeps the objects", func(t *testing.T) {
		repo := &avatarRepoFake{clearErr: errors.New("db down"), cleared: []domain.File{{StorageKey: "workspace_avatar/a"}}}
		store := &recordingStore{}
		svc := newAvatarService(repo, store, 1)
		if err := svc.RemoveWorkspaceAvatar(context.Background(), 5); err == nil {
			t.Fatal("expected the error")
		}
		if len(store.deletes) != 0 {
			t.Fatalf("deleted %v", store.deletes)
		}
	})
}

type deleteRepoFake struct {
	workspaceRepositoryMock
	err error
}

func (r *deleteRepoFake) Delete(context.Context, uint) error { return r.err }

type avatarRemoverFake struct {
	ids []uint
	err error
}

func (r *avatarRemoverFake) RemoveWorkspaceAvatar(_ context.Context, id uint) error {
	r.ids = append(r.ids, id)
	return r.err
}

func TestDeleteWorkspaceRemovesTheAvatarBestEffort(t *testing.T) {
	for name, remover := range map[string]*avatarRemoverFake{
		"removed":        {},
		"cleanup failed": {err: errors.New("storage down")},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &deleteRepoFake{}
			svc := NewWorkspaceServiceWithAvatars(repo, nil, nil, remover)
			if err := svc.DeleteWorkspace(context.Background(), 9); err != nil {
				t.Fatal(err)
			}
			if len(remover.ids) != 1 || remover.ids[0] != 9 {
				t.Fatalf("remover calls = %v", remover.ids)
			}
		})
	}

	failing := &deleteRepoFake{err: errors.New("db")}
	remover := &avatarRemoverFake{}
	if err := NewWorkspaceServiceWithAvatars(failing, nil, nil, remover).DeleteWorkspace(context.Background(), 9); err == nil {
		t.Fatal("expected the delete error")
	}
	if len(remover.ids) != 0 {
		t.Fatal("the avatar was removed although the workspace was not deleted")
	}
	passing := &deleteRepoFake{}
	if err := NewWorkspaceServiceWithAvatars(passing, nil, nil, nil).DeleteWorkspace(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
}
