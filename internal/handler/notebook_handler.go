package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type NotebookHandler struct {
	notebooks *service.NotebookService
	documents *service.DocumentService
}

func NewNotebookHandler(notebooks *service.NotebookService, documents *service.DocumentService) *NotebookHandler {
	return &NotebookHandler{notebooks: notebooks, documents: documents}
}

func notebookIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("notebook_id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_NOTEBOOK_ID", "invalid notebook id", err))
		return 0, false
	}
	return uint(id), true
}

func documentIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("document_id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_DOCUMENT_ID", "invalid document id", err))
		return 0, false
	}
	return uint(id), true
}

// notebookFail maps notebook and document errors to the API envelope.
func notebookFail(c *gin.Context, err error) {
	var fieldErr *domain.FieldError
	switch {
	case errors.Is(err, domain.ErrNotebookNotFound):
		response.HandleError(c, httperrors.NotFound("NOTEBOOK_NOT_FOUND", "notebook not found", err))
	case errors.Is(err, domain.ErrDocumentNotFound):
		response.HandleError(c, httperrors.NotFound("DOCUMENT_NOT_FOUND", "document not found", err))
	case errors.Is(err, domain.ErrNotebookLimitReached):
		response.HandleError(c, httperrors.Conflict("NOTEBOOK_LIMIT_REACHED", err.Error(), err))
	case errors.Is(err, domain.ErrDocumentLimitReached):
		response.HandleError(c, httperrors.Conflict("DOCUMENT_LIMIT_REACHED", err.Error(), err))
	case errors.Is(err, domain.ErrSnapshotConflict):
		response.HandleError(c, httperrors.Conflict("DOCUMENT_SNAPSHOT_CONFLICT", err.Error(), err))
	case errors.Is(err, domain.ErrUpdateTooLarge):
		response.HandleError(c, httperrors.New("DOCUMENT_UPDATE_TOO_LARGE", err.Error(), http.StatusRequestEntityTooLarge).
			WithParams(map[string]any{"max": domain.MaxDocumentUpdateBytes}))
	case errors.Is(err, domain.ErrStateTooLarge):
		response.HandleError(c, httperrors.New("DOCUMENT_STATE_TOO_LARGE", err.Error(), http.StatusRequestEntityTooLarge).
			WithParams(map[string]any{"max": domain.MaxDocumentStateBytes}))
	case errors.As(err, &fieldErr):
		response.HandleError(c, httperrors.BadRequest("VALIDATION_FAILED", "invalid request", err).WithDetails(fieldDetails(fieldErr)...))
	default:
		response.HandleError(c, httperrors.Internal("NOTEBOOK_FAILED", "internal server error", err))
	}
}

func (h *NotebookHandler) List(c *gin.Context) {
	workspaceID, ok := workspaceIDParam(c)
	if !ok {
		return
	}
	notebooks, err := h.notebooks.List(c.Request.Context(), workspaceID)
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, notebooks)
}

func (h *NotebookHandler) Create(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := workspaceIDParam(c)
	if !ok {
		return
	}
	var req dto.CreateNotebookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	notebook, err := h.notebooks.Create(c.Request.Context(), workspaceID, uid, service.NotebookInput{
		Name: req.Name, Description: req.Description,
	})
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusCreated, notebook)
}

func (h *NotebookHandler) Get(c *gin.Context) {
	id, ok := notebookIDParam(c)
	if !ok {
		return
	}
	notebook, err := h.notebooks.Get(c.Request.Context(), id)
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, notebook)
}

func (h *NotebookHandler) Update(c *gin.Context) {
	id, ok := notebookIDParam(c)
	if !ok {
		return
	}
	var req dto.UpdateNotebookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	notebook, err := h.notebooks.Update(c.Request.Context(), id, service.NotebookPatch{
		Name: req.Name, Description: req.Description, Position: req.Position,
	})
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, notebook)
}

func (h *NotebookHandler) Delete(c *gin.Context) {
	id, ok := notebookIDParam(c)
	if !ok {
		return
	}
	if err := h.notebooks.Delete(c.Request.Context(), id); err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "notebook deleted"})
}

func (h *NotebookHandler) CreateDocument(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	notebookID, ok := notebookIDParam(c)
	if !ok {
		return
	}
	var req dto.CreateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	document, err := h.documents.Create(c.Request.Context(), notebookID, uid, req.Title)
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusCreated, document)
}

func (h *NotebookHandler) ListDocuments(c *gin.Context) {
	notebookID, ok := notebookIDParam(c)
	if !ok {
		return
	}
	params, err := pagination.ParseOffset(c.Query("page"), c.Query("limit"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", err.Error(), err))
		return
	}
	documents, total, err := h.documents.List(c.Request.Context(), notebookID, params.Offset, params.Limit)
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.SuccessWithPagination(c, http.StatusOK, documents, &response.Pagination{
		Total: int(total), Page: params.Page, Limit: params.Limit, Pages: (int(total) + params.Limit - 1) / params.Limit,
	})
}
