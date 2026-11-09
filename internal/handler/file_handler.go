package handler

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

const (
	uploadFormField  = "file"
	transferDeadline = 10 * time.Minute
)

type FileHandler struct {
	service service.FileService
}

func NewFileHandler(s service.FileService) *FileHandler {
	return &FileHandler{service: s}
}

func extendDeadlines(c *gin.Context) {
	rc := http.NewResponseController(c.Writer)
	deadline := time.Now().Add(transferDeadline)
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline)
}

func (h *FileHandler) handleError(c *gin.Context, code string, err error) {
	writeFileError(c, code, err)
}

func writeFileError(c *gin.Context, code string, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig), errors.Is(err, domain.ErrFileTooLarge):
		response.HandleError(c, httperrors.New("FILE_TOO_LARGE", "file is larger than the allowed size", http.StatusRequestEntityTooLarge))
	case errors.Is(err, domain.ErrRehostRefused):
		response.HandleError(c, httperrors.UnprocessableEntity("REHOST_REFUSED", "the remote image cannot be rehosted", err))
	case errors.Is(err, domain.ErrEmptyFile):
		response.HandleError(c, httperrors.BadRequest("FILE_EMPTY", "file is empty", err))
	case errors.Is(err, domain.ErrUnsupportedFileType):
		response.HandleError(c, httperrors.New("UNSUPPORTED_FILE_TYPE", "file type is not supported", http.StatusUnsupportedMediaType))
	case errors.Is(err, domain.ErrInvalidImage):
		response.HandleError(c, httperrors.BadRequest("INVALID_IMAGE", "file is not a valid image", err))
	case errors.Is(err, domain.ErrTooManyAttachments):
		response.HandleError(c, httperrors.Conflict("TOO_MANY_ATTACHMENTS", err.Error(), err))
	case errors.Is(err, domain.ErrFileForbidden):
		response.HandleError(c, httperrors.Forbidden("FILE_FORBIDDEN", err.Error(), err))
	case errors.Is(err, domain.ErrNotFound):
		response.HandleError(c, httperrors.NotFound("FILE_NOT_FOUND", "file not found", err))
	default:
		response.HandleError(c, httperrors.Internal(code, "internal server error", err))
	}
}

func openUpload(c *gin.Context) (service.Upload, func(), bool) {
	extendDeadlines(c)
	header, err := c.FormFile(uploadFormField)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			response.HandleError(c, httperrors.New("FILE_TOO_LARGE", "file is larger than the allowed size", http.StatusRequestEntityTooLarge))
			return service.Upload{}, nil, false
		}
		response.HandleError(c, httperrors.BadRequest("FILE_REQUIRED", `send the file as multipart/form-data in the "file" field`, err))
		return service.Upload{}, nil, false
	}
	file, err := header.Open()
	if err != nil {
		response.HandleError(c, httperrors.Internal("FILE_READ_FAILED", "internal server error", err))
		return service.Upload{}, nil, false
	}
	closeFile := func() { closeQuietly(file) }
	return service.Upload{Name: header.Filename, Size: header.Size, Reader: file}, closeFile, true
}

func closeQuietly(f multipart.File) { _ = f.Close() }

func (h *FileHandler) UploadAttachment(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	upload, closeFile, ok := openUpload(c)
	if !ok {
		return
	}
	defer closeFile()

	file, err := h.service.UploadAttachment(c.Request.Context(), uid, taskID, upload)
	if err != nil {
		h.handleError(c, "ATTACHMENT_UPLOAD_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, file)
}

func (h *FileHandler) ListAttachments(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	files, err := h.service.ListAttachments(c.Request.Context(), taskID)
	if err != nil {
		h.handleError(c, "ATTACHMENTS_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, files)
}

func (h *FileHandler) DownloadAttachment(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	attachmentID, ok := parseUintParam(c, "attachment_id")
	if !ok {
		return
	}
	extendDeadlines(c)

	file, body, err := h.service.OpenAttachment(c.Request.Context(), taskID, attachmentID)
	if err != nil {
		h.handleError(c, "ATTACHMENT_DOWNLOAD_FAILED", err)
		return
	}
	defer func() { _ = body.Close() }()

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.Name})
	if disposition == "" {
		disposition = "attachment"
	}
	headers := c.Writer.Header()
	headers.Set("Content-Type", domain.ServedContentType(file.ContentType))
	headers.Set("Content-Disposition", disposition)
	headers.Set("Content-Length", strconv.FormatInt(file.Size, 10))
	headers.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, body)
}

func (h *FileHandler) DeleteAttachment(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	attachmentID, ok := parseUintParam(c, "attachment_id")
	if !ok {
		return
	}
	if err := h.service.DeleteAttachment(c.Request.Context(), uid, taskID, attachmentID); err != nil {
		h.handleError(c, "ATTACHMENT_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "attachment deleted"})
}

func (h *FileHandler) UploadImage(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}
	upload, closeFile, ok := openUpload(c)
	if !ok {
		return
	}
	defer closeFile()

	result, err := h.service.UploadImage(c.Request.Context(), uid, workspaceID, upload)
	if err != nil {
		h.handleError(c, "IMAGE_UPLOAD_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, gin.H{
		"url": result.URL, "content_type": result.ContentType, "size": result.Size,
	})
}

func (h *FileHandler) RehostImage(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}
	var req dto.RehostImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	result, err := h.service.RehostImage(c.Request.Context(), uid, workspaceID, req.URL)
	if err != nil {
		h.handleError(c, "IMAGE_REHOST_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, gin.H{
		"url": result.URL, "content_type": result.ContentType, "size": result.Size,
	})
}

func (h *FileHandler) ServeImage(c *gin.Context) {
	publicID := c.Param("public_id")
	etag := `"` + publicID + `"`
	if c.GetHeader("If-None-Match") == etag {
		c.Header("ETag", etag)
		c.Status(http.StatusNotModified)
		return
	}

	file, body, err := h.service.OpenPublic(c.Request.Context(), publicID)
	if err != nil {
		h.handleError(c, "IMAGE_FETCH_FAILED", err)
		return
	}
	defer func() { _ = body.Close() }()

	headers := c.Writer.Header()
	headers.Set("Content-Type", file.ContentType)
	headers.Set("Content-Length", strconv.FormatInt(file.Size, 10))
	headers.Set("Cache-Control", "public, max-age=31536000, immutable")
	headers.Set("ETag", etag)
	headers.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	headers.Set("Cross-Origin-Resource-Policy", "cross-origin")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, body)
}

func (h *FileHandler) SetAvatar(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	upload, closeFile, ok := openUpload(c)
	if !ok {
		return
	}
	defer closeFile()

	url, err := h.service.SetAvatar(c.Request.Context(), uid, upload)
	if err != nil {
		h.handleError(c, "AVATAR_UPLOAD_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"avatar_url": url})
}

func (h *FileHandler) RemoveAvatar(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	if err := h.service.RemoveAvatar(c.Request.Context(), uid); err != nil {
		h.handleError(c, "AVATAR_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"avatar_url": ""})
}

func (h *FileHandler) SetWorkspaceAvatar(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}
	upload, closeFile, ok := openUpload(c)
	if !ok {
		return
	}
	defer closeFile()

	url, err := h.service.SetWorkspaceAvatar(c.Request.Context(), uid, workspaceID, upload)
	if err != nil {
		h.handleError(c, "WORKSPACE_AVATAR_UPLOAD_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"avatar_url": url})
}

func (h *FileHandler) RemoveWorkspaceAvatar(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}
	if err := h.service.ClearWorkspaceAvatar(c.Request.Context(), uid, workspaceID); err != nil {
		h.handleError(c, "WORKSPACE_AVATAR_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"avatar_url": ""})
}
