package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

const (
	bulkForbidden = "FORBIDDEN"
	bulkInternal  = "INTERNAL_ERROR"
)

func (h *TaskHandler) Bulk(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	var req dto.BulkTasksRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if appErr := validateBulk(req); appErr != nil {
		response.HandleError(c, appErr)
		return
	}
	ids := uniqueTaskIDs(req.TaskIDs)

	out := dto.BulkTasksResponse{Results: make([]dto.BulkTaskResult, 0, len(ids))}
	for _, id := range ids {
		code := h.bulkOne(c.Request.Context(), uid, id, req)
		out.Results = append(out.Results, dto.BulkTaskResult{TaskID: id, OK: code == "", Code: code})
		if code == "" {
			out.Succeeded++
		} else {
			out.Failed++
		}
	}
	response.Success(c, http.StatusOK, out)
}

func validateBulk(req dto.BulkTasksRequest) *httperrors.AppError {
	invalid := func(field, code string) *httperrors.AppError {
		return httperrors.BadRequest("VALIDATION_FAILED", "invalid request", nil).
			WithDetails(httperrors.Detail{Field: field, Code: code})
	}
	switch req.Action {
	case "update":
		if req.Update == nil || req.Update.Empty() {
			return invalid("update", "REQUIRED")
		}
		if req.Move != nil {
			return invalid("move", "NOT_ALLOWED")
		}
	case "move":
		if req.Move == nil {
			return invalid("move", "REQUIRED")
		}
		if req.Update != nil {
			return invalid("update", "NOT_ALLOWED")
		}
	case "delete":
		if req.Update != nil {
			return invalid("update", "NOT_ALLOWED")
		}
		if req.Move != nil {
			return invalid("move", "NOT_ALLOWED")
		}
	}
	return nil
}

func uniqueTaskIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (h *TaskHandler) bulkOne(ctx context.Context, uid, taskID uint, req dto.BulkTasksRequest) string {
	if h.permissions == nil {
		return bulkInternal
	}
	need := domain.PermTaskUpdate
	if req.Action == "delete" {
		need = domain.PermTaskDelete
	}
	allowed, err := h.permissions.HasTaskPermission(ctx, uid, taskID, need)
	if err != nil {
		return bulkInternal
	}
	if !allowed {
		return bulkForbidden
	}
	switch req.Action {
	case "update":
		return h.bulkUpdate(ctx, uid, taskID, *req.Update)
	case "move":
		return h.bulkMove(ctx, uid, taskID, *req.Move)
	default:
		return h.bulkDelete(ctx, uid, taskID)
	}
}

func (h *TaskHandler) bulkUpdate(ctx context.Context, uid, taskID uint, f dto.BulkUpdateFields) string {
	task, err := h.service.UpdateTask(ctx, uid, taskID, service.UpdateTaskParams{
		StatusID: f.StatusID, Priority: f.Priority, DueDate: f.DueDate, AssigneeIDs: f.AssigneeIDs,
		AddTagIDs: f.AddTagIDs, RemoveTagIDs: f.RemoveTagIDs,
	})
	if err != nil {
		return bulkErrorCode(err)
	}
	h.publishTask(realtime.EventTaskUpdated, uid, task)
	return ""
}

func (h *TaskHandler) bulkMove(ctx context.Context, uid, taskID uint, f dto.BulkMoveFields) string {
	before, err := h.service.GetTaskByID(ctx, taskID)
	if err != nil {
		return bulkErrorCode(err)
	}
	if f.ListID != before.ListID {
		allowed, err := h.permissions.HasListPermission(ctx, uid, f.ListID, domain.PermTaskCreate)
		if err != nil {
			return bulkInternal
		}
		if !allowed {
			return bulkForbidden
		}
	}
	listID := f.ListID
	task, err := h.service.MoveTask(ctx, uid, taskID, service.MoveTaskParams{ListID: &listID, StatusID: f.StatusID})
	if err != nil {
		return bulkErrorCode(err)
	}
	if before.ListID != task.ListID {
		if h.publisher != nil {
			h.publisher.Publish(realtime.Event{
				Type: realtime.EventTaskDeleted, SpaceID: before.SpaceID, ListID: before.ListID, TaskID: before.ID, ActorID: uid,
			})
		}
		h.publishTask(realtime.EventTaskCreated, uid, task)
	} else {
		h.publishTask(realtime.EventTaskUpdated, uid, task)
	}
	return ""
}

func (h *TaskHandler) bulkDelete(ctx context.Context, uid, taskID uint) string {
	var existing *domain.Task
	if h.publisher != nil {
		existing, _ = h.service.GetTaskByID(ctx, taskID)
	}
	if err := h.service.DeleteTask(ctx, taskID); err != nil {
		return bulkErrorCode(err)
	}
	if existing != nil && h.publisher != nil {
		h.publisher.Publish(realtime.Event{
			Type: realtime.EventTaskDeleted, SpaceID: existing.SpaceID, ListID: existing.ListID, TaskID: existing.ID, ActorID: uid,
		})
	}
	return ""
}

func bulkErrorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return bulkForbidden
	case errors.Is(err, domain.ErrConflictingTagChange):
		return "CONFLICTING_TAG_CHANGE"
	case errors.Is(err, domain.ErrInvalidDateRange):
		return "INVALID_DATE_RANGE"
	case errors.Is(err, domain.ErrTaskHasSubtasks):
		return "TASK_HAS_SUBTASKS"
	case errors.Is(err, domain.ErrConcurrentMove):
		return "TASK_MOVE_CONFLICT"
	case isInvalidTaskReference(err):
		return "INVALID_TASK_REFERENCE"
	default:
		return bulkInternal
	}
}
