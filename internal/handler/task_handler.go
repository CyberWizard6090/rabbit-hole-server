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
	"rabbit-hole-server/internal/http/taskquery"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

type TaskEventPublisher interface {
	Publish(event realtime.Event)
}

type TaskHandler struct {
	service     service.TaskService
	cursorCodec *pagination.Codec
	publisher   TaskEventPublisher
	permissions service.PermissionChecker
}

const invalidTaskIDMessage = "invalid task id"

func NewTaskHandler(
	service service.TaskService,
	cursorCodec *pagination.Codec,
) *TaskHandler {
	return &TaskHandler{
		service:     service,
		cursorCodec: cursorCodec,
	}
}

func (h *TaskHandler) WithPublisher(publisher TaskEventPublisher) *TaskHandler {
	h.publisher = publisher
	return h
}

func (h *TaskHandler) WithPermissionChecker(checker service.PermissionChecker) *TaskHandler {
	h.permissions = checker
	return h
}

func (h *TaskHandler) publishTask(eventType string, actorID uint, task *domain.Task) {
	if h.publisher == nil || task == nil {
		return
	}
	h.publisher.Publish(realtime.Event{
		Type:    eventType,
		SpaceID: task.SpaceID,
		ListID:  task.ListID,
		TaskID:  task.ID,
		Task:    task,
		ActorID: actorID,
	})
}

func (h *TaskHandler) Create(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil || listID <= 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", "invalid list_id in URL", err))
		return
	}

	var req dto.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	task, err := h.service.CreateTask(c.Request.Context(), uid, service.CreateTaskParams{
		Title:        req.Title,
		Description:  req.Description,
		ListID:       uint(listID),
		StatusID:     req.StatusID,
		ParentID:     req.ParentID,
		Priority:     req.Priority,
		StartDate:    req.StartDate,
		DueDate:      req.DueDate,
		AssigneeIDs:  req.AssigneeIDs,
		TagIDs:       req.TagIDs,
		NewTagNames:  req.NewTagNames,
		TimeEstimate: req.TimeEstimate,
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidDateRange) {
			response.HandleError(c, httperrors.BadRequest("INVALID_DATE_RANGE", err.Error(), err))
			return
		}
		if isInvalidTaskReference(err) {
			response.HandleError(c, httperrors.BadRequest("INVALID_TASK_REFERENCE", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Internal("TASK_CREATE_FAILED", "internal server error", err))
		return
	}

	h.publishTask(realtime.EventTaskCreated, uid, task)
	response.Success(c, http.StatusCreated, task)
}

func (h *TaskHandler) Update(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return
	}

	var req dto.UpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	task, err := h.service.UpdateTask(c.Request.Context(), uid, uint(taskID), service.UpdateTaskParams{
		Title:        req.Title,
		Description:  req.Description,
		StatusID:     req.StatusID,
		Priority:     req.Priority,
		ParentID:     req.ParentID,
		StartDate:    req.StartDate,
		DueDate:      req.DueDate,
		AssigneeIDs:  req.AssigneeIDs,
		TimeEstimate: req.TimeEstimate,
		AddTagIDs:    req.AddTagIDs,
		AddTagNames:  req.AddTagNames,
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidDateRange) {
			response.HandleError(c, httperrors.BadRequest("INVALID_DATE_RANGE", err.Error(), err))
			return
		}
		if isInvalidTaskReference(err) {
			response.HandleError(c, httperrors.BadRequest("INVALID_TASK_REFERENCE", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Internal("TASK_UPDATE_FAILED", "internal server error", err))
		return
	}

	h.publishTask(realtime.EventTaskUpdated, uid, task)
	response.Success(c, http.StatusOK, task)
}

func (h *TaskHandler) List(c *gin.Context) {
	_, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil || listID == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", "invalid list id", err))
		return
	}

	query, appErr := taskquery.Parse(c.Request.URL.Query())
	if appErr != nil {
		response.HandleError(c, appErr)
		return
	}

	params, err := pagination.ParseParams(c.Query("limit"), c.Query("cursor"), h.cursorCodec, query.CursorContext(uint(listID)))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", "invalid pagination parameters", err))
		return
	}

	tasks, err := h.service.ListTasks(c.Request.Context(), uint(listID), params, query)
	if err != nil {
		response.HandleError(c, httperrors.Internal("TASKS_FETCH_FAILED", "internal server error", err))
		return
	}
	response.Success(c, http.StatusOK, tasks)
}

func (h *TaskHandler) Get(c *gin.Context) {
	_, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return
	}

	task, err := h.service.GetTaskByID(c.Request.Context(), uint(taskID))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.HandleError(c, httperrors.NotFound("TASK_NOT_FOUND", "task not found", err))
			return
		}
		response.HandleError(c, httperrors.Internal("TASK_FETCH_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, task)
}

func (h *TaskHandler) Delete(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return
	}

	var existing *domain.Task
	if h.publisher != nil {
		existing, _ = h.service.GetTaskByID(c.Request.Context(), uint(taskID))
	}

	if err := h.service.DeleteTask(c.Request.Context(), uint(taskID)); err != nil {
		response.HandleError(c, httperrors.Internal("TASK_DELETE_FAILED", "internal server error", err))
		return
	}

	if existing != nil && h.publisher != nil {
		h.publisher.Publish(realtime.Event{
			Type:    realtime.EventTaskDeleted,
			SpaceID: existing.SpaceID,
			ListID:  existing.ListID,
			TaskID:  existing.ID,
			ActorID: uid,
		})
	}
	response.Success(c, http.StatusOK, gin.H{"message": "task deleted"})
}

func isInvalidTaskReference(err error) bool {
	return errors.Is(err, domain.ErrInvalidStatusReference) ||
		errors.Is(err, domain.ErrInvalidParentReference) ||
		errors.Is(err, domain.ErrInvalidTagReference) ||
		errors.Is(err, domain.ErrInvalidAssigneeReference) ||
		errors.Is(err, domain.ErrInvalidListReference) ||
		errors.Is(err, domain.ErrInvalidMoveAnchor)
}

func (h *TaskHandler) Move(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return
	}

	var req dto.MoveTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	before, err := h.service.GetTaskByID(c.Request.Context(), uint(taskID))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.HandleError(c, httperrors.NotFound("TASK_NOT_FOUND", "task not found", err))
			return
		}
		response.HandleError(c, httperrors.Internal("TASK_FETCH_FAILED", "internal server error", err))
		return
	}

	if req.ListID != nil && *req.ListID != before.ListID {
		if h.permissions == nil {
			response.HandleError(c, httperrors.Internal("TASK_MOVE_FAILED", "internal server error", nil))
			return
		}
		allowed, err := h.permissions.HasListPermission(c.Request.Context(), uid, *req.ListID, domain.PermTaskCreate)
		if err != nil {
			response.HandleError(c, httperrors.Internal("TASK_MOVE_FAILED", "internal server error", err))
			return
		}
		if !allowed {
			response.Forbidden(c)
			return
		}
	}

	task, err := h.service.MoveTask(c.Request.Context(), uid, uint(taskID), service.MoveTaskParams{
		ListID:   req.ListID,
		StatusID: req.StatusID,
		AfterID:  req.AfterID,
		BeforeID: req.BeforeID,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			response.HandleError(c, httperrors.NotFound("TASK_NOT_FOUND", "task not found", err))
		case errors.Is(err, domain.ErrTaskHasSubtasks):
			response.HandleError(c, httperrors.Conflict("TASK_HAS_SUBTASKS", err.Error(), err))
		case errors.Is(err, domain.ErrConcurrentMove):
			response.HandleError(c, httperrors.Conflict("TASK_MOVE_CONFLICT", err.Error(), err))
		case isInvalidTaskReference(err):
			response.HandleError(c, httperrors.BadRequest("INVALID_TASK_REFERENCE", err.Error(), err))
		default:
			response.HandleError(c, httperrors.Internal("TASK_MOVE_FAILED", "internal server error", err))
		}
		return
	}

	if before.ListID != task.ListID {
		if h.publisher != nil {
			h.publisher.Publish(realtime.Event{
				Type:    realtime.EventTaskDeleted,
				SpaceID: before.SpaceID,
				ListID:  before.ListID,
				TaskID:  before.ID,
				ActorID: uid,
			})
		}
		h.publishTask(realtime.EventTaskCreated, uid, task)
	} else {
		h.publishTask(realtime.EventTaskUpdated, uid, task)
	}
	response.Success(c, http.StatusOK, task)
}
