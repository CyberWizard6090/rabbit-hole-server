package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/storage"
)

const (
	defaultMaxFiles       = 5
	hardMaxFiles          = 10
	formUploadKeyPrefix   = "formupload"
	cleanupBatchSize      = 100
	settingsInvalidCode   = "FORM_QUESTION_SETTINGS_INVALID"
	uploadsDisabledCode   = "FORM_UPLOADS_NOT_ACCEPTED"
	formUploadCleanupStep = 10 * time.Minute
)

var allowedFormUploadTypes = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/gif": {}, "image/webp": {}, "image/avif": {},
	"application/pdf": {}, "application/zip": {}, "text/plain; charset=utf-8": {},
}

var blockedUploadExtensions = map[string]struct{}{
	".exe": {}, ".dll": {}, ".bat": {}, ".cmd": {}, ".com": {}, ".msi": {}, ".scr": {}, ".sh": {},
	".js": {}, ".mjs": {}, ".jar": {}, ".html": {}, ".htm": {}, ".xhtml": {}, ".svg": {}, ".php": {},
	".ps1": {}, ".vbs": {}, ".apk": {}, ".app": {}, ".bin": {}, ".lnk": {}, ".hta": {},
}

var activeMarkup = [][]byte{
	[]byte("<script"), []byte("<svg"), []byte("<html"), []byte("<!doctype html"), []byte("<iframe"),
	[]byte("<body"), []byte("<object"), []byte("<embed"),
}

type FormUploadResult struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type FormUploadService interface {
	Upload(ctx context.Context, slug string, upload Upload) (*FormUploadResult, error)
	OpenSubmissionFile(ctx context.Context, workspaceID, submissionID, uploadID uint) (*domain.FormUpload, io.ReadCloser, error)
	CleanupExpired(ctx context.Context) (int, error)
}

type formUploadService struct {
	forms    domain.FormRepository
	uploads  domain.FormUploadRepository
	store    storage.Store
	maxBytes int64
	ttl      time.Duration
	now      func() time.Time
}

func NewFormUploadService(
	forms domain.FormRepository,
	uploads domain.FormUploadRepository,
	store storage.Store,
	maxBytes int64,
	ttl time.Duration,
) FormUploadService {
	return &formUploadService{
		forms: forms, uploads: uploads, store: store, maxBytes: maxBytes, ttl: ttl, now: time.Now,
	}
}

func (s *formUploadService) Upload(ctx context.Context, slug string, upload Upload) (*FormUploadResult, error) {
	form, err := s.forms.GetPublishedBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if form.CurrentVersion == nil {
		return nil, domain.ErrFormNotPublished
	}
	var snapshot struct {
		Questions []domain.FormQuestion `json:"questions"`
		Settings  domain.FormSettings   `json:"settings"`
	}
	if err := json.Unmarshal(form.CurrentVersion.SchemaSnapshot, &snapshot); err != nil {
		return nil, err
	}
	if !hasFileQuestion(snapshot.Questions) {
		return nil, domain.NewFormFieldError(uploadsDisabledCode, "this form has no file question",
			"form", "NOT_ALLOWED", nil)
	}
	if formClosed(snapshot.Settings, s.now()) {
		return nil, domain.ErrFormClosed
	}
	if upload.Size > s.maxBytes {
		return nil, domain.ErrFileTooLarge
	}
	data, err := readLimited(upload.Reader, s.maxBytes)
	if err != nil {
		return nil, err
	}
	name := sanitizeFilename(upload.Name)
	contentType, err := checkFormUpload(name, data)
	if err != nil {
		return nil, err
	}
	keyID, err := randomHex()
	if err != nil {
		return nil, err
	}
	publicID, err := randomHex()
	if err != nil {
		return nil, err
	}
	key := formUploadKeyPrefix + "/" + keyID
	if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		s.discard(key)
		return nil, err
	}
	created, err := s.uploads.Create(ctx, domain.NewFormUpload{
		PublicID: publicID, FormID: form.ID, StorageKey: key, Name: name, ContentType: contentType,
		Size: int64(len(data)), ExpiresAt: s.now().UTC().Add(s.ttl),
	})
	if err != nil {
		s.discard(key)
		return nil, err
	}
	return &FormUploadResult{
		ID: created.PublicID, Name: created.Name, ContentType: created.ContentType,
		Size: created.Size, ExpiresAt: created.ExpiresAt,
	}, nil
}

func (s *formUploadService) discard(key string) {
	if err := s.store.Delete(context.Background(), key); err != nil {
		slog.Warn("form uploads: cannot remove a stored object", "key", key, "error", err)
	}
}

func (s *formUploadService) OpenSubmissionFile(
	ctx context.Context, workspaceID, submissionID, uploadID uint,
) (*domain.FormUpload, io.ReadCloser, error) {
	upload, err := s.uploads.GetForSubmission(ctx, workspaceID, submissionID, uploadID)
	if err != nil {
		return nil, nil, err
	}
	body, err := s.store.Get(ctx, upload.StorageKey)
	if errors.Is(err, storage.ErrNotFound) {
		slog.Error("form uploads: the stored object is missing", "upload_id", upload.ID, "key", upload.StorageKey)
		return nil, nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return upload, body, nil
}

func (s *formUploadService) CleanupExpired(ctx context.Context) (int, error) {
	total := 0
	for {
		expired, err := s.uploads.ListExpired(ctx, s.now().UTC(), cleanupBatchSize)
		if err != nil {
			return total, err
		}
		if len(expired) == 0 {
			return total, nil
		}
		removable := make([]uint, 0, len(expired))
		for _, item := range expired {
			if err := s.store.Delete(ctx, item.StorageKey); err != nil && !errors.Is(err, storage.ErrNotFound) {
				slog.Warn("form uploads: cannot remove an expired object", "key", item.StorageKey, "error", err)
				continue
			}
			removable = append(removable, item.ID)
		}
		deleted, err := s.uploads.DeleteExpired(ctx, s.now().UTC(), removable)
		if err != nil {
			return total, err
		}
		total += int(deleted)
		if len(removable) == 0 || len(expired) < cleanupBatchSize {
			return total, nil
		}
	}
}

func RunFormUploadCleanup(ctx context.Context, cleaner interface {
	CleanupExpired(context.Context) (int, error)
}, interval time.Duration) {
	if interval <= 0 {
		interval = formUploadCleanupStep
	}
	run := func() {
		removed, err := cleaner.CleanupExpired(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Warn("form uploads: cleanup failed", "error", err)
			return
		}
		if removed > 0 {
			slog.Info("form uploads: removed expired uploads", "count", removed)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func hasFileQuestion(questions []domain.FormQuestion) bool {
	for _, question := range questions {
		if question.Type == domain.QuestionFile {
			return true
		}
	}
	return false
}

func checkFormUpload(name string, data []byte) (string, error) {
	if _, blocked := blockedUploadExtensions[strings.ToLower(path.Ext(name))]; blocked {
		return "", domain.ErrUnsupportedFileType
	}
	contentType := sniffContentType(data)
	if _, allowed := allowedFormUploadTypes[contentType]; !allowed {
		return "", domain.ErrUnsupportedFileType
	}
	if strings.HasPrefix(contentType, "text/plain") {
		lowered := bytes.ToLower(data)
		for _, marker := range activeMarkup {
			if bytes.Contains(lowered, marker) {
				return "", domain.ErrUnsupportedFileType
			}
		}
	}
	return contentType, nil
}

func questionMaxFiles(settings json.RawMessage) int {
	var parsed struct {
		MaxFiles *float64 `json:"max_files"`
	}
	if err := json.Unmarshal(settings, &parsed); err != nil || parsed.MaxFiles == nil {
		return defaultMaxFiles
	}
	limit := int(*parsed.MaxFiles)
	if limit < 1 {
		return defaultMaxFiles
	}
	return min(limit, hardMaxFiles)
}

func validateFileQuestionSettings(questionType domain.QuestionType, settings json.RawMessage) error {
	if questionType != domain.QuestionFile {
		return nil
	}
	var parsed struct {
		MaxFiles *float64 `json:"max_files"`
	}
	if err := json.Unmarshal(settings, &parsed); err != nil {
		return domain.ErrInvalidForm
	}
	if parsed.MaxFiles == nil {
		return nil
	}
	if *parsed.MaxFiles != float64(int(*parsed.MaxFiles)) || *parsed.MaxFiles < 1 || *parsed.MaxFiles > hardMaxFiles {
		return domain.NewFormFieldError(settingsInvalidCode, "settings.max_files must be an integer between 1 and 10",
			"settings.max_files", "OUT_OF_RANGE", map[string]any{"min": 1, "max": hardMaxFiles})
	}
	return nil
}

func fileClaims(questions []domain.FormQuestion, answers []domain.SubmissionAnswer) ([]domain.FormUploadClaim, error) {
	fileQuestions := make(map[uint]struct{})
	for _, question := range questions {
		if question.Type == domain.QuestionFile {
			fileQuestions[question.ID] = struct{}{}
		}
	}
	var claims []domain.FormUploadClaim
	seen := make(map[string]struct{})
	for _, answer := range answers {
		if _, isFile := fileQuestions[answer.QuestionID]; !isFile {
			continue
		}
		var ids []string
		if err := json.Unmarshal(answer.Value, &ids); err != nil {
			return nil, domain.ErrInvalidSubmission
		}
		for _, id := range ids {
			if _, duplicate := seen[id]; duplicate {
				return nil, domain.NewFormFieldError(domain.FormUploadInvalidCode, "an uploaded file can only be used once",
					answerField(answer.QuestionID), "DUPLICATE", nil)
			}
			seen[id] = struct{}{}
			claims = append(claims, domain.FormUploadClaim{QuestionID: answer.QuestionID, PublicID: id})
		}
	}
	return claims, nil
}

func (s *formService) WithUploads(uploads domain.FormUploadRepository) *formService {
	s.uploads = uploads
	return s
}

func (s *formService) attachSubmissionFiles(ctx context.Context, submissions []domain.FormSubmission) error {
	if s.uploads == nil || len(submissions) == 0 {
		return nil
	}
	ids := make([]uint, len(submissions))
	for i, submission := range submissions {
		ids[i] = submission.ID
	}
	files, err := s.uploads.ListSubmissionFiles(ctx, ids)
	if err != nil {
		return err
	}
	for i := range submissions {
		submissions[i].Files = files[submissions[i].ID]
	}
	return nil
}

func questionReferencedError() error {
	return &domain.FormError{
		Code: questionReferencedCode, Status: http.StatusConflict,
		Message: "another question depends on this question through visible_if",
	}
}
