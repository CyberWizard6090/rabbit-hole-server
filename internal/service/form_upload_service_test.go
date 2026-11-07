package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/storage"
)

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func TestCheckFormUpload(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content []byte
		want    string
		wantErr bool
	}{
		{"png", "a.png", pngHeader, "image/png", false},
		{"jpeg", "a.jpg", []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), "image/jpeg", false},
		{"pdf", "a.pdf", []byte("%PDF-1.7\n1 0 obj"), "application/pdf", false},
		{"zip based office document", "a.docx", []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00"), "application/zip", false},
		{"plain text", "a.txt", []byte("just some notes"), "text/plain; charset=utf-8", false},
		{"html disguised as png", "a.png", []byte("<!DOCTYPE html><html><body><script>alert(1)</script>"), "", true},
		{"html with a leading space", "a.png", []byte("  <html><script>x</script></html>"), "", true},
		{"svg disguised as png", "a.png", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`), "", true},
		{"svg without a prolog", "a.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="x()"></svg>`), "", true},
		{"script in a text file", "a.txt", []byte("hello\n<SCRIPT>alert(1)</SCRIPT>"), "", true},
		{"windows executable", "a.png", []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff"), "", true},
		{"elf executable", "a.png", []byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00"), "", true},
		{"shell script", "a.txt", []byte("#!/bin/sh\nrm -rf /\n"), "text/plain; charset=utf-8", false},
		{"blocked extension with a harmless body", "run.exe", []byte("plain"), "", true},
		{"blocked html extension", "page.HTML", []byte("plain"), "", true},
		{"blocked svg extension", "logo.svg", pngHeader, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkFormUpload(tc.file, tc.content)
			if tc.wantErr {
				if !errors.Is(err, domain.ErrUnsupportedFileType) {
					t.Fatalf("err = %v, want unsupported type", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, err %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestQuestionMaxFiles(t *testing.T) {
	cases := map[string]int{
		``:                  defaultMaxFiles,
		`{}`:                defaultMaxFiles,
		`{"max_files":3}`:   3,
		`{"max_files":10}`:  10,
		`{"max_files":99}`:  hardMaxFiles,
		`{"max_files":0}`:   defaultMaxFiles,
		`{"max_files":"x"}`: defaultMaxFiles,
	}
	for settings, want := range cases {
		if got := questionMaxFiles(json.RawMessage(settings)); got != want {
			t.Errorf("settings %q: got %d, want %d", settings, got, want)
		}
	}
}

func TestValidateFileQuestionSettings(t *testing.T) {
	for _, valid := range []string{`{}`, `{"max_files":1}`, `{"max_files":10}`} {
		if err := validateFileQuestionSettings(domain.QuestionFile, json.RawMessage(valid)); err != nil {
			t.Errorf("%s rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{`{"max_files":0}`, `{"max_files":11}`, `{"max_files":2.5}`, `{"max_files":-1}`} {
		err := validateFileQuestionSettings(domain.QuestionFile, json.RawMessage(invalid))
		if formErrorCode(err) != settingsInvalidCode {
			t.Errorf("%s accepted, err = %v", invalid, err)
		}
	}
	if err := validateFileQuestionSettings(domain.QuestionShortText, json.RawMessage(`{"max_files":99}`)); err != nil {
		t.Errorf("non file questions ignore max_files: %v", err)
	}
}

const (
	idOne   = "11111111111111111111111111111111"
	idTwo   = "22222222222222222222222222222222"
	idThree = "33333333333333333333333333333333"
)

func fileQuestion(id uint, required bool, settings string) domain.FormQuestion {
	return domain.FormQuestion{
		ID: id, Type: domain.QuestionFile, Title: "files", Position: int(id), Required: required,
		Settings: json.RawMessage(settings),
	}
}

func TestValidateSubmissionFileAnswers(t *testing.T) {
	question := fileQuestion(1, true, `{"max_files":2}`)
	valid := func(value any) error {
		_, err := validateSubmission([]domain.FormQuestion{question}, answersInput(map[uint]any{1: value}))
		return err
	}
	if err := valid([]string{idOne, idTwo}); err != nil {
		t.Fatalf("two files within the limit: %v", err)
	}
	for name, value := range map[string]any{
		"too many files":         []string{idOne, idTwo, idThree},
		"duplicate ids":          []string{idOne, idOne},
		"not an array":           idOne,
		"not a public id":        []string{"42"},
		"upper case public id":   []string{strings.ToUpper("abcdef0123456789abcdef0123456789")},
		"numbers instead of ids": []int{1, 2},
		"empty array":            []string{},
	} {
		if err := valid(value); !errors.Is(err, domain.ErrInvalidSubmission) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := validateSubmission([]domain.FormQuestion{question}, nil); !errors.Is(err, domain.ErrInvalidSubmission) {
		t.Errorf("a required file question needs files, err = %v", err)
	}
	optional := fileQuestion(1, false, `{}`)
	if _, err := validateSubmission([]domain.FormQuestion{optional}, nil); err != nil {
		t.Errorf("an optional file question may stay empty: %v", err)
	}
	defaults := fileQuestion(1, false, `{}`)
	six := []string{idOne, idTwo, idThree, "44444444444444444444444444444444", "55555555555555555555555555555555", "66666666666666666666666666666666"}
	if _, err := validateSubmission([]domain.FormQuestion{defaults}, answersInput(map[uint]any{1: six})); !errors.Is(err, domain.ErrInvalidSubmission) {
		t.Errorf("the default limit is five files, err = %v", err)
	}
}

func TestFileClaims(t *testing.T) {
	questions := []domain.FormQuestion{fileQuestion(1, false, `{}`), fileQuestion(2, false, `{}`), textQuestion(3, 3, false, nil)}
	answers := []domain.SubmissionAnswer{
		{QuestionID: 1, Value: json.RawMessage(`["` + idOne + `","` + idTwo + `"]`)},
		{QuestionID: 2, Value: json.RawMessage(`["` + idThree + `"]`)},
		{QuestionID: 3, Value: json.RawMessage(`"text"`)},
	}
	claims, err := fileClaims(questions, answers)
	if err != nil || len(claims) != 3 || claims[2].QuestionID != 2 || claims[2].PublicID != idThree {
		t.Fatalf("claims = %+v, err = %v", claims, err)
	}
	answers[1].Value = json.RawMessage(`["` + idOne + `"]`)
	if _, err := fileClaims(questions, answers); formErrorCode(err) != domain.FormUploadInvalidCode {
		t.Fatalf("one upload cannot serve two questions, err = %v", err)
	}
}

type uploadsRepoFake struct {
	domain.FormUploadRepository
	mu      sync.Mutex
	created []domain.NewFormUpload
	expired []domain.ExpiredFormUpload
	deleted []uint
	err     error
}

func (r *uploadsRepoFake) Create(_ context.Context, upload domain.NewFormUpload) (*domain.FormUpload, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, upload)
	return &domain.FormUpload{
		PublicID: upload.PublicID, Name: upload.Name, ContentType: upload.ContentType, Size: upload.Size,
		ExpiresAt: upload.ExpiresAt,
	}, nil
}

func (r *uploadsRepoFake) ListExpired(_ context.Context, _ time.Time, limit int) ([]domain.ExpiredFormUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.expired) > limit {
		return r.expired[:limit], nil
	}
	return r.expired, nil
}

func (r *uploadsRepoFake) DeleteExpired(_ context.Context, _ time.Time, ids []uint) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, ids...)
	remaining := r.expired[:0:0]
	for _, item := range r.expired {
		removed := false
		for _, id := range ids {
			if id == item.ID {
				removed = true
			}
		}
		if !removed {
			remaining = append(remaining, item)
		}
	}
	r.expired = remaining
	return int64(len(ids)), nil
}

type uploadStoreFake struct {
	mu      sync.Mutex
	objects map[string][]byte
	failFor map[string]bool
}

func newUploadStoreFake() *uploadStoreFake {
	return &uploadStoreFake{objects: map[string][]byte{}, failFor: map[string]bool{}}
}

func (s *uploadStoreFake) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	return nil
}

func (s *uploadStoreFake) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *uploadStoreFake) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failFor[key] {
		return errors.New("storage down")
	}
	delete(s.objects, key)
	return nil
}

func (s *uploadStoreFake) Ping(context.Context) error { return nil }

type publishedFormFake struct {
	domain.FormRepository
	form *domain.Form
	err  error
}

func (f *publishedFormFake) GetPublishedBySlug(context.Context, string) (*domain.Form, error) {
	return f.form, f.err
}

func publishedForm(t *testing.T, questions []domain.FormQuestion, settings domain.FormSettings) *domain.Form {
	t.Helper()
	snapshot, err := json.Marshal(map[string]any{"questions": questions, "settings": settings})
	if err != nil {
		t.Fatal(err)
	}
	form := &domain.Form{}
	form.ID = 4
	form.CurrentVersion = &domain.FormVersion{SchemaSnapshot: snapshot}
	return form
}

func newUploadService(forms domain.FormRepository, repo *uploadsRepoFake, store *uploadStoreFake) *formUploadService {
	return NewFormUploadService(forms, repo, store, 64, time.Hour).(*formUploadService)
}

func TestFormUploadServiceStoresValidFile(t *testing.T) {
	repo, store := &uploadsRepoFake{}, newUploadStoreFake()
	forms := &publishedFormFake{form: publishedForm(t, []domain.FormQuestion{fileQuestion(1, false, `{}`)}, domain.FormSettings{})}
	svc := newUploadService(forms, repo, store)
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	result, err := svc.Upload(context.Background(), "slug", Upload{Name: `C:\Users\me\..\photo.png`, Size: int64(len(pngHeader)), Reader: bytes.NewReader(pngHeader)})
	if err != nil {
		t.Fatal(err)
	}
	if !publicIDPattern.MatchString(result.ID) || result.Name != "photo.png" || result.ContentType != "image/png" ||
		!result.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("result = %+v", result)
	}
	if len(repo.created) != 1 || repo.created[0].FormID != 4 || !strings.HasPrefix(repo.created[0].StorageKey, "formupload/") {
		t.Fatalf("created = %+v", repo.created)
	}
	if _, ok := store.objects[repo.created[0].StorageKey]; !ok {
		t.Fatal("the object was not stored")
	}
}

func TestFormUploadServiceRejectsBadUploads(t *testing.T) {
	withFile := publishedForm(t, []domain.FormQuestion{fileQuestion(1, false, `{}`)}, domain.FormSettings{})
	withoutFile := publishedForm(t, []domain.FormQuestion{textQuestion(1, 0, false, nil)}, domain.FormSettings{})
	past := time.Now().Add(-time.Minute)
	closed := publishedForm(t, []domain.FormQuestion{fileQuestion(1, false, `{}`)}, domain.FormSettings{ClosesAt: &past})
	big := bytes.Repeat([]byte("a"), 65)

	cases := []struct {
		name   string
		forms  *publishedFormFake
		upload Upload
		check  func(error) bool
	}{
		{"unpublished form", &publishedFormFake{err: domain.ErrFormNotPublished}, Upload{Name: "a.png", Size: 4, Reader: bytes.NewReader(pngHeader)},
			func(err error) bool { return errors.Is(err, domain.ErrFormNotPublished) }},
		{"form without a file question", &publishedFormFake{form: withoutFile}, Upload{Name: "a.png", Size: 4, Reader: bytes.NewReader(pngHeader)},
			func(err error) bool { return formErrorCode(err) == uploadsDisabledCode }},
		{"closed form", &publishedFormFake{form: closed}, Upload{Name: "a.png", Size: 4, Reader: bytes.NewReader(pngHeader)},
			func(err error) bool { return errors.Is(err, domain.ErrFormClosed) }},
		{"declared oversize", &publishedFormFake{form: withFile}, Upload{Name: "a.txt", Size: 65, Reader: bytes.NewReader(big)},
			func(err error) bool { return errors.Is(err, domain.ErrFileTooLarge) }},
		{"undeclared oversize", &publishedFormFake{form: withFile}, Upload{Name: "a.txt", Size: 1, Reader: bytes.NewReader(big)},
			func(err error) bool { return errors.Is(err, domain.ErrFileTooLarge) }},
		{"empty file", &publishedFormFake{form: withFile}, Upload{Name: "a.txt", Reader: bytes.NewReader(nil)},
			func(err error) bool { return errors.Is(err, domain.ErrEmptyFile) }},
		{"html disguised as png", &publishedFormFake{form: withFile}, Upload{Name: "a.png", Size: 20, Reader: strings.NewReader("<html><b>x</b></html>")},
			func(err error) bool { return errors.Is(err, domain.ErrUnsupportedFileType) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store := &uploadsRepoFake{}, newUploadStoreFake()
			_, err := newUploadService(tc.forms, repo, store).Upload(context.Background(), "slug", tc.upload)
			if err == nil || !tc.check(err) {
				t.Fatalf("err = %v", err)
			}
			if len(repo.created) != 0 || len(store.objects) != 0 {
				t.Fatal("a rejected upload must leave nothing behind")
			}
		})
	}
}

func TestFormUploadServiceRemovesObjectWhenRecordingFails(t *testing.T) {
	repo, store := &uploadsRepoFake{err: domain.ErrTooManyPendingUploads}, newUploadStoreFake()
	forms := &publishedFormFake{form: publishedForm(t, []domain.FormQuestion{fileQuestion(1, false, `{}`)}, domain.FormSettings{})}
	_, err := newUploadService(forms, repo, store).Upload(context.Background(), "slug",
		Upload{Name: "a.png", Size: int64(len(pngHeader)), Reader: bytes.NewReader(pngHeader)})
	if !errors.Is(err, domain.ErrTooManyPendingUploads) || len(store.objects) != 0 {
		t.Fatalf("err = %v, objects = %d", err, len(store.objects))
	}
}

func TestCleanupExpiredRemovesObjectsAndRows(t *testing.T) {
	repo, store := &uploadsRepoFake{}, newUploadStoreFake()
	for i := uint(1); i <= 250; i++ {
		key := "form-upload/" + string(rune('a'+i%26)) + strings.Repeat("x", int(i))
		store.objects[key] = []byte("x")
		repo.expired = append(repo.expired, domain.ExpiredFormUpload{ID: i, StorageKey: key})
	}
	svc := newUploadService(&publishedFormFake{}, repo, store)
	removed, err := svc.CleanupExpired(context.Background())
	if err != nil || removed != 250 || len(repo.deleted) != 250 || len(store.objects) != 0 {
		t.Fatalf("removed = %d, deleted = %d, objects = %d, err = %v", removed, len(repo.deleted), len(store.objects), err)
	}
}

func TestCleanupExpiredKeepsRowsWhoseObjectCannotBeRemoved(t *testing.T) {
	repo, store := &uploadsRepoFake{}, newUploadStoreFake()
	store.objects["form-upload/ok"] = []byte("x")
	store.objects["form-upload/stuck"] = []byte("x")
	store.failFor["form-upload/stuck"] = true
	repo.expired = []domain.ExpiredFormUpload{{ID: 1, StorageKey: "form-upload/ok"}, {ID: 2, StorageKey: "form-upload/stuck"}}
	removed, err := newUploadService(&publishedFormFake{}, repo, store).CleanupExpired(context.Background())
	if err != nil || removed != 1 || len(repo.deleted) != 1 || repo.deleted[0] != 1 {
		t.Fatalf("removed = %d, deleted = %v, err = %v", removed, repo.deleted, err)
	}
	if _, ok := store.objects["form-upload/stuck"]; !ok {
		t.Fatal("the stuck object is still there and its row must stay for the next run")
	}
}

func TestCleanupExpiredToleratesMissingObjects(t *testing.T) {
	repo, store := &uploadsRepoFake{}, newUploadStoreFake()
	repo.expired = []domain.ExpiredFormUpload{{ID: 5, StorageKey: "form-upload/gone"}}
	removed, err := newUploadService(&publishedFormFake{}, repo, store).CleanupExpired(context.Background())
	if err != nil || removed != 1 {
		t.Fatalf("removed = %d, err = %v", removed, err)
	}
}

type cleanerFake struct {
	calls chan struct{}
}

func (c *cleanerFake) CleanupExpired(context.Context) (int, error) {
	c.calls <- struct{}{}
	return 0, nil
}

func TestRunFormUploadCleanupHonoursShutdown(t *testing.T) {
	cleaner := &cleanerFake{calls: make(chan struct{}, 16)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunFormUploadCleanup(ctx, cleaner, 5*time.Millisecond)
		close(done)
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-cleaner.calls:
		case <-time.After(2 * time.Second):
			t.Fatal("the cleanup did not run repeatedly")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the cleanup did not stop on shutdown")
	}
}
