package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type DocumentHandler struct {
	documents *service.DocumentService
}

func NewDocumentHandler(documents *service.DocumentService) *DocumentHandler {
	return &DocumentHandler{documents: documents}
}

func (h *DocumentHandler) Get(c *gin.Context) {
	id, ok := documentIDParam(c)
	if !ok {
		return
	}
	document, err := h.documents.Get(c.Request.Context(), id)
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, document)
}

func (h *DocumentHandler) Update(c *gin.Context) {
	id, ok := documentIDParam(c)
	if !ok {
		return
	}
	var req dto.UpdateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	document, err := h.documents.Update(c.Request.Context(), id, service.DocumentPatch{
		Title: req.Title, Position: req.Position, NotebookID: req.NotebookID,
	})
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, document)
}

func (h *DocumentHandler) Delete(c *gin.Context) {
	id, ok := documentIDParam(c)
	if !ok {
		return
	}
	if err := h.documents.Delete(c.Request.Context(), id); err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "document deleted"})
}

// State returns what a client needs to open the document: the compacted state and the updates after it.
func (h *DocumentHandler) State(c *gin.Context) {
	id, ok := documentIDParam(c)
	if !ok {
		return
	}
	state, err := h.documents.State(c.Request.Context(), id)
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, state)
}

// AppendUpdate is the intake of collaborative edits: one Yjs update from one client.
func (h *DocumentHandler) AppendUpdate(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	id, ok := documentIDParam(c)
	if !ok {
		return
	}
	var req dto.DocumentUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	seq, err := h.documents.AppendUpdate(c.Request.Context(), id, uid, req.Update, req.Touched)
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusCreated, dto.DocumentUpdateResult{Seq: seq})
}

func (h *DocumentHandler) ReplaceSnapshot(c *gin.Context) {
	id, ok := documentIDParam(c)
	if !ok {
		return
	}
	var req dto.DocumentSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.documents.ReplaceSnapshot(c.Request.Context(), id, req.UptoSeq, req.State); err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"upto_seq": req.UptoSeq})
}

// BlockActivity says who created and who last edited blocks of the document.
func (h *DocumentHandler) BlockActivity(c *gin.Context) {
	id, ok := documentIDParam(c)
	if !ok {
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > domain.MaxActivityBlocks {
			response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", "invalid limit", err))
			return
		}
		limit = parsed
	}
	items, err := h.documents.BlockActivity(c.Request.Context(), id, c.Query("block_id"), limit)
	if err != nil {
		notebookFail(c, err)
		return
	}
	response.Success(c, http.StatusOK, items)
}
