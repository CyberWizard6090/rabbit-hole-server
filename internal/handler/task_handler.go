package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
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
		response.BadRequest(c, "invalid list_id in URL")
		return
	}

	var req dto.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
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
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusCreated, task)
}

func (h *TaskHandler) Update(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidTaskIDMessage)
		return
	}

	var req dto.UpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	task, err := h.service.UpdateTask(uint(taskID), uid, service.UpdateTaskParams{
		Title:        req.Title,
		Description:  req.Description,
		StatusID:     req.StatusID,
		Priority:     req.Priority,
		TimeEstimate: req.TimeEstimate,
		AddTagIDs:    req.AddTagIDs,
		AddTagNames:  req.AddTagNames,
	})
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, task)
}

func (h *TaskHandler) GetAll(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	p := pagination.Parse(c)

	tasks, total, err := h.service.GetAllTasks(uid, p.Limit, p.Offset)
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	pages := (int(total) + p.Limit - 1) / p.Limit

	response.SuccessWithPagination(c, http.StatusOK, tasks, &response.Pagination{
		Total: int(total), Page: p.Page, Limit: p.Limit, Pages: pages,
	})
}

func (h *TaskHandler) GetByID(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidTaskIDMessage)
		return
	}

	task, err := h.service.GetTaskByID(uint(taskID), uid)
	if err != nil {
		log.Println(err)
		response.Internal(c)
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

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidTaskIDMessage)
		return
	}

	if err := h.service.DeleteTask(uint(taskID), uid); err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "task deleted"})
}
