package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

type TimeHandler struct {
	service     *service.TimeService
	tasks       service.TaskService
	permissions service.PermissionChecker
	publisher   TaskEventPublisher
}

func NewTimeHandler(svc *service.TimeService, tasks service.TaskService, permissions service.PermissionChecker) *TimeHandler {
	return &TimeHandler{service: svc, tasks: tasks, permissions: permissions}
}

func (h *TimeHandler) WithPublisher(p TaskEventPublisher) *TimeHandler {
	h.publisher = p
	return h
}

func (h *TimeHandler) fail(c *gin.Context, err error) {
	var fieldErr *domain.FieldError
	switch {
	case errors.Is(err, domain.ErrTimerRunning):
		response.HandleError(c, httperrors.Conflict("TIMER_ALREADY_RUNNING", err.Error(), err))
	case errors.Is(err, domain.ErrNoRunningTimer):
		response.HandleError(c, httperrors.NotFound("NO_RUNNING_TIMER", err.Error(), err))
	case errors.Is(err, domain.ErrTimeEntryMissing):
		response.HandleError(c, httperrors.NotFound("TIME_ENTRY_NOT_FOUND", "time entry not found", err))
	case errors.Is(err, domain.ErrTimeForbidden):
		response.HandleError(c, httperrors.Forbidden("TIME_ENTRY_FORBIDDEN", err.Error(), err))
	case errors.As(err, &fieldErr):
		details := timeFieldDetails(fieldErr)
		response.HandleError(c, httperrors.BadRequest("VALIDATION_FAILED", "invalid request", err).WithDetails(details...))
	case errors.Is(err, domain.ErrInvalidTimeEntry):
		response.HandleError(c, httperrors.BadRequest("INVALID_TIME_ENTRY", "a running timer cannot get a duration", err))
	default:
		response.HandleError(c, httperrors.Internal("TIME_FAILED", "internal server error", err))
	}
}

func (h *TimeHandler) publish(c *gin.Context, uid, taskID uint) {
	if h.publisher == nil {
		return
	}
	task, err := h.tasks.GetTaskByID(c.Request.Context(), taskID)
	if err != nil {
		return
	}
	h.publisher.Publish(realtime.Event{
		Type: realtime.EventTaskUpdated, SpaceID: task.SpaceID, ListID: task.ListID, TaskID: task.ID, Task: task, ActorID: uid,
	})
}

func taskIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return 0, false
	}
	return uint(id), true
}

func entryIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("entry_id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_TIME_ENTRY_ID", "invalid time entry id", err))
		return 0, false
	}
	return uint(id), true
}

func (h *TimeHandler) Start(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	var req dto.StartTimerRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ValidationError(c, err)
			return
		}
	}
	entry, err := h.service.Start(c.Request.Context(), taskID, uid, req.Note)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusCreated, entry)
}

func (h *TimeHandler) Stop(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	entry, err := h.service.Stop(c.Request.Context(), taskID, uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.publish(c, uid, taskID)
	response.Success(c, http.StatusOK, entry)
}

func (h *TimeHandler) Add(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	var req dto.AddTimeEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	entry, err := h.service.Add(c.Request.Context(), taskID, uid, req.Seconds, req.StartedAt, req.Note)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.publish(c, uid, taskID)
	response.Success(c, http.StatusCreated, entry)
}

func (h *TimeHandler) List(c *gin.Context) {
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	entries, total, err := h.service.List(c.Request.Context(), taskID)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"entries": entries, "total_seconds": total})
}

func (h *TimeHandler) mayChange(c *gin.Context, uid, taskID uint, entry *domain.TimeEntry) (bool, error) {
	if entry.UserID == uid {
		return true, nil
	}
	return h.permissions.HasTaskPermission(c.Request.Context(), uid, taskID, domain.PermSpaceUpdate)
}

func (h *TimeHandler) Update(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	entryID, ok := entryIDParam(c)
	if !ok {
		return
	}
	var req dto.UpdateTimeEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if req.Seconds == nil && req.Note == nil {
		response.HandleError(c, httperrors.BadRequest("VALIDATION_FAILED", "invalid request", nil).
			WithDetails(httperrors.Detail{Field: "seconds", Code: "REQUIRED"}))
		return
	}
	existing, err := h.service.Get(c.Request.Context(), taskID, entryID)
	if err != nil {
		h.fail(c, err)
		return
	}
	allowed, err := h.mayChange(c, uid, taskID, existing)
	if err != nil {
		h.fail(c, err)
		return
	}
	if !allowed {
		h.fail(c, domain.ErrTimeForbidden)
		return
	}
	entry, err := h.service.Update(c.Request.Context(), taskID, entryID, req.Seconds, req.Note)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.publish(c, uid, taskID)
	response.Success(c, http.StatusOK, entry)
}

func (h *TimeHandler) Delete(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	entryID, ok := entryIDParam(c)
	if !ok {
		return
	}
	existing, err := h.service.Get(c.Request.Context(), taskID, entryID)
	if err != nil {
		h.fail(c, err)
		return
	}
	allowed, err := h.mayChange(c, uid, taskID, existing)
	if err != nil {
		h.fail(c, err)
		return
	}
	if !allowed {
		h.fail(c, domain.ErrTimeForbidden)
		return
	}
	if err := h.service.Delete(c.Request.Context(), taskID, entryID); err != nil {
		h.fail(c, err)
		return
	}
	h.publish(c, uid, taskID)
	response.Success(c, http.StatusOK, gin.H{"message": "time entry deleted"})
}

func (h *TimeHandler) Running(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	timer, err := h.service.Running(c.Request.Context(), uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"timer": timer})
}

func reportDate(c *gin.Context, name string) (*time.Time, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_FILTER", "invalid date", err).
			WithDetails(httperrors.Detail{Field: name, Code: "INVALID_VALUE"}))
		return nil, false
	}
	return &parsed, true
}

func (h *TimeHandler) Report(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := workspaceIDParam(c)
	if !ok {
		return
	}
	from, ok := reportDate(c, "from")
	if !ok {
		return
	}
	to, ok := reportDate(c, "to")
	if !ok {
		return
	}
	manager, err := h.permissions.HasWorkspacePermission(c.Request.Context(), uid, workspaceID, domain.PermMemberManage)
	if err != nil {
		h.fail(c, err)
		return
	}
	var userFilter *uint
	if !manager {
		userFilter = &uid
	} else if raw := c.Query("user_id"); raw != "" {
		id, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil || id == 0 {
			response.HandleError(c, httperrors.BadRequest("INVALID_FILTER", "invalid user id", parseErr).
				WithDetails(httperrors.Detail{Field: "user_id", Code: "INVALID_VALUE"}))
			return
		}
		value := uint(id)
		userFilter = &value
	}
	rows, start, end, err := h.service.Report(c.Request.Context(), workspaceID, from, to, userFilter)
	if err != nil {
		h.fail(c, err)
		return
	}
	var total int64
	for _, row := range rows {
		total += row.Seconds
	}
	response.Success(c, http.StatusOK, gin.H{"from": start, "to": end, "total_seconds": total, "users": rows})
}

func timeFieldDetails(err *domain.FieldError) []httperrors.Detail {
	out := make([]httperrors.Detail, 0, len(err.Problems))
	for _, p := range err.Problems {
		out = append(out, httperrors.Detail{Field: p.Field, Code: p.Reason, Params: p.Params})
	}
	return out
}
