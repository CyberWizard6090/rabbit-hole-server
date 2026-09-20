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

type TaskHandler struct {
	service service.TaskService
}

const invalidTaskIDMessage = "invalid task id"

func NewTaskHandler(
	service service.TaskService,
) *TaskHandler {
	return &TaskHandler{
		service: service,
	}
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

	task, err := h.service.CreateTask(uid, service.CreateTaskParams{
		Title:        req.Title,
		Description:  req.Description,
		ListID:       uint(listID),
		StatusID:     req.StatusID,
		ParentID:     req.ParentID,
		Priority:     req.Priority,
		AssigneeIDs:  req.AssigneeIDs,
		TagIDs:       req.TagIDs,
		NewTagNames:  req.NewTagNames,
		TimeEstimate: req.TimeEstimate,
	})
	if err != nil {
		if isInvalidTaskReference(err) {
			response.HandleError(c, httperrors.BadRequest("INVALID_TASK_REFERENCE", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Internal("TASK_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, task)
}

func (h *TaskHandler) Update(c *gin.Context) {
	_, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return
	}

	var req dto.UpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	task, err := h.service.UpdateTask(uint(taskID), service.UpdateTaskParams{
		Title:        req.Title,
		Description:  req.Description,
		StatusID:     req.StatusID,
		Priority:     req.Priority,
		TimeEstimate: req.TimeEstimate,
		AddTagIDs:    req.AddTagIDs,
		AddTagNames:  req.AddTagNames,
	})
	if err != nil {
		if isInvalidTaskReference(err) {
			response.HandleError(c, httperrors.BadRequest("INVALID_TASK_REFERENCE", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Internal("TASK_UPDATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, task)
}

func (h *TaskHandler) GetAll(c *gin.Context) {
	_, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	p := pagination.Parse(c)

	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", "invalid list id", err))
		return
	}

	tasks, total, err := h.service.GetAllTasks(uint(listID), p.Limit, p.Offset)
	if err != nil {
		response.HandleError(c, httperrors.Internal("TASKS_FETCH_FAILED", "internal server error", err))
		return
	}

	pages := (int(total) + p.Limit - 1) / p.Limit

	response.SuccessWithPagination(c, http.StatusOK, tasks, &response.Pagination{
		Total: int(total), Page: p.Page, Limit: p.Limit, Pages: pages,
	})
}

func (h *TaskHandler) GetByID(c *gin.Context) {
	_, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return
	}

	task, err := h.service.GetTaskByID(uint(taskID))
	if err != nil {
		response.HandleError(c, httperrors.NotFound("TASK_NOT_FOUND", "task not found", err))
		return
	}

	response.Success(c, http.StatusOK, task)
}

func (h *TaskHandler) Delete(c *gin.Context) {
	_, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return
	}

	if err := h.service.DeleteTask(uint(taskID)); err != nil {
		response.HandleError(c, httperrors.Internal("TASK_DELETE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "task deleted"})
}

func isInvalidTaskReference(err error) bool {
	return errors.Is(err, domain.ErrInvalidStatusReference) ||
		errors.Is(err, domain.ErrInvalidParentReference) ||
		errors.Is(err, domain.ErrInvalidTagReference) ||
		errors.Is(err, domain.ErrInvalidAssigneeReference)
}
