package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/storage"
)

type fileRepositoryMock struct {
	domain.FileRepository
	existing  *domain.File
	createErr error
	deleteErr error
}

func (m *fileRepositoryMock) create(_ context.Context, f domain.NewFile) (*domain.File, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return &domain.File{ID: 31, TaskID: f.TaskID, Name: f.Name, Size: f.Size, Uploader: domain.CommentUser{ID: f.UserID}}, nil
}

func (m *fileRepositoryMock) Create(ctx context.Context, f domain.NewFile) (*domain.File, error) {
	return m.create(ctx, f)
}

func (m *fileRepositoryMock) CreateAttachment(ctx context.Context, f domain.NewFile) (*domain.File, error) {
	return m.create(ctx, f)
}

func (m *fileRepositoryMock) GetAttachment(context.Context, uint, uint) (*domain.File, error) {
	return m.existing, nil
}

func (m *fileRepositoryMock) Delete(context.Context, uint) error { return m.deleteErr }

type storeStub struct {
	putErr error
}

func (s *storeStub) Put(_ context.Context, _ string, body io.Reader, _ int64, _ string) error {
	if s.putErr != nil {
		return s.putErr
	}
	_, err := io.Copy(io.Discard, body)
	return err
}

func (s *storeStub) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, storage.ErrNotFound
}

func (s *storeStub) Delete(context.Context, string) error { return nil }

func (s *storeStub) Ping(context.Context) error { return nil }

type recorderFake struct {
	entries []domain.ActivityEntry
}

func (r *recorderFake) Record(_ context.Context, entries ...domain.ActivityEntry) error {
	r.entries = append(r.entries, entries...)
	return nil
}

func newTestFileService(repo *fileRepositoryMock, store *storeStub, perms PermissionChecker, rec ActivityRecorder) FileService {
	return NewFileService(repo, store, perms, FileLimits{Attachment: 1 << 20}, "http://localhost", rec, RehostConfig{})
}

func attachmentBy(uploader uint) *domain.File {
	taskID := uint(9)
	return &domain.File{ID: 4, TaskID: &taskID, Name: "report.txt", Size: 17, Uploader: domain.CommentUser{ID: uploader}}
}

func decodeValue(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func TestFileServiceUploadAttachmentRecordsActivity(t *testing.T) {
	rec := &recorderFake{}
	svc := newTestFileService(&fileRepositoryMock{}, &storeStub{}, &taskPermissionStub{}, rec)

	file, err := svc.UploadAttachment(context.Background(), 7, 9, Upload{Name: "dir/report.txt", Size: 17, Reader: strings.NewReader("quarterly numbers")})
	if err != nil {
		t.Fatalf("UploadAttachment error=%v", err)
	}
	if len(rec.entries) != 1 {
		t.Fatalf("entries=%d, want 1", len(rec.entries))
	}
	entry := rec.entries[0]
	if entry.Action != domain.ActivityAttachmentCreated || entry.TaskID != 9 || entry.Actor.ID != 7 || entry.OldValue != nil {
		t.Fatalf("entry=%+v", entry)
	}
	value := decodeValue(t, entry.NewValue)
	if value["id"] != float64(file.ID) || value["name"] != "report.txt" || value["size"] != float64(17) {
		t.Fatalf("new_value=%v", value)
	}
}

func TestFileServiceUploadAttachmentFailureRecordsNothing(t *testing.T) {
	boom := errors.New("boom")
	tests := map[string]struct {
		repo  *fileRepositoryMock
		store *storeStub
	}{
		"store failure":      {&fileRepositoryMock{}, &storeStub{putErr: boom}},
		"repository failure": {&fileRepositoryMock{createErr: boom}, &storeStub{}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rec := &recorderFake{}
			svc := newTestFileService(tc.repo, tc.store, &taskPermissionStub{}, rec)
			if _, err := svc.UploadAttachment(context.Background(), 7, 9, Upload{Name: "a.txt", Size: 1, Reader: strings.NewReader("a")}); err == nil {
				t.Fatal("expected an error")
			}
			if len(rec.entries) != 0 {
				t.Fatalf("entries=%d, want none", len(rec.entries))
			}
		})
	}
}

func TestFileServiceDeleteAttachmentRecordsActivity(t *testing.T) {
	tests := []struct {
		name  string
		actor uint
		perms *taskPermissionStub
	}{
		{"uploader", 7, &taskPermissionStub{}},
		{"moderator", 8, &taskPermissionStub{allow: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorderFake{}
			svc := newTestFileService(&fileRepositoryMock{existing: attachmentBy(7)}, &storeStub{}, tc.perms, rec)

			if err := svc.DeleteAttachment(context.Background(), tc.actor, 9, 4); err != nil {
				t.Fatalf("DeleteAttachment error=%v", err)
			}
			if len(rec.entries) != 1 {
				t.Fatalf("entries=%d, want 1", len(rec.entries))
			}
			entry := rec.entries[0]
			if entry.Action != domain.ActivityAttachmentDeleted || entry.TaskID != 9 || entry.Actor.ID != tc.actor || entry.NewValue != nil {
				t.Fatalf("entry=%+v", entry)
			}
			value := decodeValue(t, entry.OldValue)
			if value["id"] != float64(4) || value["name"] != "report.txt" || value["size"] != float64(17) || value["deleted_by"] != float64(tc.actor) {
				t.Fatalf("old_value=%v", value)
			}
		})
	}
}

func TestFileServiceDeleteAttachmentFailureRecordsNothing(t *testing.T) {
	t.Run("forbidden", func(t *testing.T) {
		rec := &recorderFake{}
		svc := newTestFileService(&fileRepositoryMock{existing: attachmentBy(7)}, &storeStub{}, &taskPermissionStub{}, rec)
		if err := svc.DeleteAttachment(context.Background(), 8, 9, 4); !errors.Is(err, domain.ErrFileForbidden) {
			t.Fatalf("error=%v, want ErrFileForbidden", err)
		}
		if len(rec.entries) != 0 {
			t.Fatalf("entries=%d, want none", len(rec.entries))
		}
	})
	t.Run("repository failure", func(t *testing.T) {
		rec := &recorderFake{}
		repo := &fileRepositoryMock{existing: attachmentBy(7), deleteErr: errors.New("boom")}
		svc := newTestFileService(repo, &storeStub{}, &taskPermissionStub{}, rec)
		if err := svc.DeleteAttachment(context.Background(), 7, 9, 4); err == nil {
			t.Fatal("expected an error")
		}
		if len(rec.entries) != 0 {
			t.Fatalf("entries=%d, want none", len(rec.entries))
		}
	})
}

func TestFileServiceNilRecorderIsSafe(t *testing.T) {
	svc := newTestFileService(&fileRepositoryMock{existing: attachmentBy(7)}, &storeStub{}, &taskPermissionStub{}, nil)
	if _, err := svc.UploadAttachment(context.Background(), 7, 9, Upload{Name: "a.txt", Size: 1, Reader: strings.NewReader("a")}); err != nil {
		t.Fatalf("UploadAttachment error=%v", err)
	}
	if err := svc.DeleteAttachment(context.Background(), 7, 9, 4); err != nil {
		t.Fatalf("DeleteAttachment error=%v", err)
	}
}
