package handler

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type FormUploadHandler struct {
	service service.FormUploadService
}

func NewFormUploadHandler(s service.FormUploadService) *FormUploadHandler {
	return &FormUploadHandler{service: s}
}

func isUploadContentError(err error) bool {
	var tooBig *http.MaxBytesError
	return errors.As(err, &tooBig) || errors.Is(err, domain.ErrFileTooLarge) ||
		errors.Is(err, domain.ErrEmptyFile) || errors.Is(err, domain.ErrUnsupportedFileType)
}

func (h *FormUploadHandler) Upload(c *gin.Context) {
	upload, closeFile, ok := openUpload(c)
	if !ok {
		return
	}
	defer closeFile()

	result, err := h.service.Upload(c.Request.Context(), c.Param("slug"), upload)
	switch {
	case err == nil:
		response.Success(c, http.StatusCreated, result)
	case isUploadContentError(err):
		writeFileError(c, "FORM_UPLOAD_FAILED", err)
	case errors.Is(err, domain.ErrTooManyPendingUploads):
		response.HandleError(c, httperrors.TooManyRequests("FORM_UPLOADS_LIMITED", err.Error(), err))
	default:
		writeFormError(c, "FORM_UPLOAD_FAILED", err)
	}
}

func (h *FormUploadHandler) DownloadSubmissionFile(c *gin.Context) {
	workspaceID, ok := parseFormID(c, "workspace_id")
	if !ok {
		return
	}
	submissionID, ok := parseFormID(c, "submission_id")
	if !ok {
		return
	}
	fileID, ok := parseFormID(c, "file_id")
	if !ok {
		return
	}
	extendDeadlines(c)

	upload, body, err := h.service.OpenSubmissionFile(c.Request.Context(), workspaceID, submissionID, fileID)
	if err != nil {
		writeFileError(c, "SUBMISSION_FILE_DOWNLOAD_FAILED", err)
		return
	}
	defer func() { _ = body.Close() }()

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": upload.Name})
	if disposition == "" {
		disposition = "attachment"
	}
	headers := c.Writer.Header()
	headers.Set("Content-Type", domain.ServedContentType(upload.ContentType))
	headers.Set("Content-Disposition", disposition)
	headers.Set("Content-Length", strconv.FormatInt(upload.Size, 10))
	headers.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, body)
}
