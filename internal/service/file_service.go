package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/storage"
)

const maxFilenameBytes = 255

var publicIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type FileLimits struct {
	Attachment int64
	Image      int64
	Avatar     int64
}

type Upload struct {
	Name   string
	Size   int64
	Reader io.Reader
}

type ImageResult struct {
	URL         string
	ContentType string
	Size        int64
}

type FileService interface {
	UploadAttachment(ctx context.Context, userID, taskID uint, upload Upload) (*domain.File, error)
	ListAttachments(ctx context.Context, taskID uint) ([]domain.File, error)
	OpenAttachment(ctx context.Context, taskID, id uint) (*domain.File, io.ReadCloser, error)
	DeleteAttachment(ctx context.Context, userID, taskID, id uint) error
	UploadImage(ctx context.Context, userID, workspaceID uint, upload Upload) (*ImageResult, error)
	OpenPublic(ctx context.Context, publicID string) (*domain.File, io.ReadCloser, error)
	SetAvatar(ctx context.Context, userID uint, upload Upload) (string, error)
	RemoveAvatar(ctx context.Context, userID uint) error
}

type fileService struct {
	repo          domain.FileRepository
	store         storage.Store
	permissions   PermissionChecker
	limits        FileLimits
	publicBaseURL string
}

func NewFileService(
	repo domain.FileRepository,
	store storage.Store,
	permissions PermissionChecker,
	limits FileLimits,
	publicBaseURL string,
) FileService {
	return &fileService{
		repo: repo, store: store, permissions: permissions, limits: limits,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

func randomHex() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *fileService) publicURL(publicID string) string {
	return s.publicBaseURL + "/api/v1/images/" + publicID
}

func sanitizeFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	for len(name) > maxFilenameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if name == "" || name == "/" {
		return "file"
	}
	return name
}

type limitedReader struct {
	r    io.Reader
	left int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left < 0 {
		return 0, domain.ErrFileTooLarge
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n, domain.ErrFileTooLarge
	}
	return n, err
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(&limitedReader{r: r, left: max})
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, domain.ErrEmptyFile
	}
	return data, nil
}

func (s *fileService) newKeys(kind string) (key, publicID string, err error) {
	keyID, err := randomHex()
	if err != nil {
		return "", "", err
	}
	publicID, err = randomHex()
	if err != nil {
		return "", "", err
	}
	return kind + "/" + keyID, publicID, nil
}

func (s *fileService) discard(key string) {
	if err := s.store.Delete(context.Background(), key); err != nil {
		slog.Warn("files: cannot remove a stored object", "key", key, "error", err)
	}
}

func (s *fileService) UploadAttachment(ctx context.Context, userID, taskID uint, upload Upload) (*domain.File, error) {
	if upload.Size > s.limits.Attachment {
		return nil, domain.ErrFileTooLarge
	}
	count, err := s.repo.CountAttachments(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if count >= domain.MaxAttachmentsPerTask {
		return nil, domain.ErrTooManyAttachments
	}

	head := make([]byte, sniffLength)
	n, err := io.ReadFull(upload.Reader, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if n == 0 {
		return nil, domain.ErrEmptyFile
	}
	head = head[:n]
	contentType := sniffContentType(head)

	key, publicID, err := s.newKeys(domain.FileKindAttachment)
	if err != nil {
		return nil, err
	}
	counter := &countingReader{r: io.MultiReader(bytes.NewReader(head), &limitedReader{r: upload.Reader, left: s.limits.Attachment - int64(n)})}
	if err := s.store.Put(ctx, key, counter, upload.Size, contentType); err != nil {
		s.discard(key)
		if errors.Is(err, domain.ErrFileTooLarge) {
			return nil, domain.ErrFileTooLarge
		}
		return nil, fmt.Errorf("store attachment: %w", err)
	}

	file, err := s.repo.Create(ctx, domain.NewFile{
		Kind: domain.FileKindAttachment, PublicID: publicID, StorageKey: key, UserID: userID, TaskID: &taskID,
		Name: sanitizeFilename(upload.Name), ContentType: contentType, Size: counter.n,
	})
	if err != nil {
		s.discard(key)
		return nil, err
	}
	return file, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (s *fileService) ListAttachments(ctx context.Context, taskID uint) ([]domain.File, error) {
	return s.repo.ListAttachments(ctx, taskID)
}

func (s *fileService) open(ctx context.Context, file *domain.File) (io.ReadCloser, error) {
	body, err := s.store.Get(ctx, file.StorageKey)
	if errors.Is(err, storage.ErrNotFound) {
		slog.Error("files: the stored object is missing", "file_id", file.ID, "key", file.StorageKey)
		return nil, domain.ErrNotFound
	}
	return body, err
}

func (s *fileService) OpenAttachment(ctx context.Context, taskID, id uint) (*domain.File, io.ReadCloser, error) {
	file, err := s.repo.GetAttachment(ctx, taskID, id)
	if err != nil {
		return nil, nil, err
	}
	body, err := s.open(ctx, file)
	if err != nil {
		return nil, nil, err
	}
	return file, body, nil
}

func (s *fileService) DeleteAttachment(ctx context.Context, userID, taskID, id uint) error {
	file, err := s.repo.GetAttachment(ctx, taskID, id)
	if err != nil {
		return err
	}
	if file.Uploader.ID != userID {
		allowed, err := s.permissions.HasTaskPermission(ctx, userID, taskID, domain.PermTaskDelete)
		if err != nil {
			return err
		}
		if !allowed {
			return domain.ErrFileForbidden
		}
	}
	if err := s.repo.Delete(ctx, file.ID); err != nil {
		return err
	}
	s.discard(file.StorageKey)
	return nil
}

func (s *fileService) storeImage(ctx context.Context, kind string, data []byte, contentType string) (key, publicID string, err error) {
	key, publicID, err = s.newKeys(kind)
	if err != nil {
		return "", "", err
	}
	if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		s.discard(key)
		return "", "", fmt.Errorf("store image: %w", err)
	}
	return key, publicID, nil
}

func (s *fileService) UploadImage(ctx context.Context, userID, workspaceID uint, upload Upload) (*ImageResult, error) {
	if upload.Size > s.limits.Image {
		return nil, domain.ErrFileTooLarge
	}
	data, err := readLimited(upload.Reader, s.limits.Image)
	if err != nil {
		return nil, err
	}
	contentType, _, err := inspectImage(data, maxImagePixels)
	if err != nil {
		return nil, err
	}

	key, publicID, err := s.storeImage(ctx, domain.FileKindImage, data, contentType)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.Create(ctx, domain.NewFile{
		Kind: domain.FileKindImage, PublicID: publicID, StorageKey: key, UserID: userID, WorkspaceID: &workspaceID,
		Name: sanitizeFilename(upload.Name), ContentType: contentType, Size: int64(len(data)),
	}); err != nil {
		s.discard(key)
		return nil, err
	}
	return &ImageResult{URL: s.publicURL(publicID), ContentType: contentType, Size: int64(len(data))}, nil
}

func (s *fileService) OpenPublic(ctx context.Context, publicID string) (*domain.File, io.ReadCloser, error) {
	if !publicIDPattern.MatchString(publicID) {
		return nil, nil, domain.ErrNotFound
	}
	file, err := s.repo.GetPublic(ctx, publicID)
	if err != nil {
		return nil, nil, err
	}
	body, err := s.open(ctx, file)
	if err != nil {
		return nil, nil, err
	}
	return file, body, nil
}

func (s *fileService) SetAvatar(ctx context.Context, userID uint, upload Upload) (string, error) {
	if upload.Size > s.limits.Avatar {
		return "", domain.ErrFileTooLarge
	}
	data, err := readLimited(upload.Reader, s.limits.Avatar)
	if err != nil {
		return "", err
	}
	rendered, contentType, err := renderAvatar(data)
	if err != nil {
		return "", err
	}

	key, publicID, err := s.storeImage(ctx, domain.FileKindAvatar, rendered, contentType)
	if err != nil {
		return "", err
	}
	url := s.publicURL(publicID)
	_, replaced, err := s.repo.ReplaceAvatar(ctx, domain.NewFile{
		Kind: domain.FileKindAvatar, PublicID: publicID, StorageKey: key, UserID: userID,
		Name: "avatar", ContentType: contentType, Size: int64(len(rendered)),
	}, url)
	if err != nil {
		s.discard(key)
		return "", err
	}
	for _, old := range replaced {
		s.discard(old.StorageKey)
	}
	return url, nil
}

func (s *fileService) RemoveAvatar(ctx context.Context, userID uint) error {
	replaced, err := s.repo.ClearAvatar(ctx, userID)
	if err != nil {
		return err
	}
	for _, old := range replaced {
		s.discard(old.StorageKey)
	}
	return nil
}
