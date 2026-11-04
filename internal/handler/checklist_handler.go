package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type ChecklistHandler struct {
	service service.ChecklistService
}

func NewChecklistHandler(s service.ChecklistService) *ChecklistHandler {
	return &ChecklistHandler{service: s}
}

func (h *ChecklistHandler) handleError(c *gin.Context, code string, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		response.HandleError(c, httperrors.NotFound("CHECKLIST_NOT_FOUND", "checklist or item not found", err))
	case errors.Is(err, domain.ErrTooManyChecklists), errors.Is(err, domain.ErrTooManyChecklistItems):
		response.HandleError(c, httperrors.Conflict("CHECKLIST_LIMIT_REACHED", err.Error(), err))
	case errors.Is(err, domain.ErrInvalidChecklistOrder):
		response.HandleError(c, httperrors.BadRequest("INVALID_ORDER", err.Error(), err))
	case errors.Is(err, domain.ErrInvalidAssigneeReference):
		response.HandleError(c, httperrors.BadRequest("INVALID_ASSIGNEE", err.Error(), err))
	default:
		response.HandleError(c, httperrors.Internal(code, "internal server error", err))
	}
}

func checklistIDs(c *gin.Context, withChecklist, withItem bool) (taskID, checklistID, itemID uint, ok bool) {
	if taskID, ok = parseTaskID(c); !ok {
		return 0, 0, 0, false
	}
	if withChecklist {
		if checklistID, ok = parseUintParam(c, "checklist_id"); !ok {
			return 0, 0, 0, false
		}
	}
	if withItem {
		if itemID, ok = parseUintParam(c, "item_id"); !ok {
			return 0, 0, 0, false
		}
	}
	return taskID, checklistID, itemID, true
}

func (h *ChecklistHandler) Create(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, _, _, ok := checklistIDs(c, false, false)
	if !ok {
		return
	}
	var req dto.ChecklistNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	list, err := h.service.CreateChecklist(c.Request.Context(), uid, taskID, req.Name)
	if err != nil {
		h.handleError(c, "CHECKLIST_CREATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, list)
}

func (h *ChecklistHandler) List(c *gin.Context) {
	taskID, _, _, ok := checklistIDs(c, false, false)
	if !ok {
		return
	}
	lists, err := h.service.ListChecklists(c.Request.Context(), taskID)
	if err != nil {
		h.handleError(c, "CHECKLISTS_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, lists)
}

func (h *ChecklistHandler) Rename(c *gin.Context) {
	taskID, checklistID, _, ok := checklistIDs(c, true, false)
	if !ok {
		return
	}
	var req dto.ChecklistNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	list, err := h.service.RenameChecklist(c.Request.Context(), taskID, checklistID, req.Name)
	if err != nil {
		h.handleError(c, "CHECKLIST_UPDATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, list)
}

func (h *ChecklistHandler) Delete(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, checklistID, _, ok := checklistIDs(c, true, false)
	if !ok {
		return
	}
	if err := h.service.DeleteChecklist(c.Request.Context(), uid, taskID, checklistID); err != nil {
		h.handleError(c, "CHECKLIST_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "checklist deleted"})
}

func (h *ChecklistHandler) Reorder(c *gin.Context) {
	taskID, _, _, ok := checklistIDs(c, false, false)
	if !ok {
		return
	}
	var req dto.ReorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.service.ReorderChecklists(c.Request.Context(), taskID, req.IDs); err != nil {
		h.handleError(c, "CHECKLIST_REORDER_FAILED", err)
		return
	}
	h.respondWithLists(c, taskID)
}

func (h *ChecklistHandler) respondWithLists(c *gin.Context, taskID uint) {
	lists, err := h.service.ListChecklists(c.Request.Context(), taskID)
	if err != nil {
		h.handleError(c, "CHECKLISTS_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, lists)
}

func (h *ChecklistHandler) CreateItem(c *gin.Context) {
	taskID, checklistID, _, ok := checklistIDs(c, true, false)
	if !ok {
		return
	}
	var req dto.CreateChecklistItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	item, err := h.service.CreateItem(c.Request.Context(), taskID, checklistID, req.Text, req.AssigneeID)
	if err != nil {
		h.handleError(c, "CHECKLIST_ITEM_CREATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, item)
}

func (h *ChecklistHandler) UpdateItem(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, checklistID, itemID, ok := checklistIDs(c, true, true)
	if !ok {
		return
	}
	var req dto.UpdateChecklistItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	item, err := h.service.UpdateItem(c.Request.Context(), uid, taskID, checklistID, itemID, domain.ChecklistItemUpdate{
		Text: req.Text, Resolved: req.Resolved, Assignee: req.AssigneeID,
	})
	if err != nil {
		h.handleError(c, "CHECKLIST_ITEM_UPDATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, item)
}

func (h *ChecklistHandler) DeleteItem(c *gin.Context) {
	taskID, checklistID, itemID, ok := checklistIDs(c, true, true)
	if !ok {
		return
	}
	if err := h.service.DeleteItem(c.Request.Context(), taskID, checklistID, itemID); err != nil {
		h.handleError(c, "CHECKLIST_ITEM_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "item deleted"})
}

func (h *ChecklistHandler) ReorderItems(c *gin.Context) {
	taskID, checklistID, _, ok := checklistIDs(c, true, false)
	if !ok {
		return
	}
	var req dto.ReorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.service.ReorderItems(c.Request.Context(), taskID, checklistID, req.IDs); err != nil {
		h.handleError(c, "CHECKLIST_ITEM_REORDER_FAILED", err)
		return
	}
	h.respondWithLists(c, taskID)
}
