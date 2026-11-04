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
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

type CommentHandler struct {
	comments  service.CommentService
	tasks     service.TaskService
	publisher TaskEventPublisher
}

func NewCommentHandler(comments service.CommentService, tasks service.TaskService) *CommentHandler {
	return &CommentHandler{comments: comments, tasks: tasks}
}

func (h *CommentHandler) WithPublisher(publisher TaskEventPublisher) *CommentHandler {
	h.publisher = publisher
	return h
}

func (h *CommentHandler) publish(eventType string, actorID uint, task *domain.Task, commentID uint, comment *domain.Comment) {
	if h.publisher == nil || task == nil {
		return
	}
	h.publisher.Publish(realtime.Event{
		Type:      eventType,
		SpaceID:   task.SpaceID,
		ListID:    task.ListID,
		TaskID:    task.ID,
		CommentID: commentID,
		Comment:   comment,
		ActorID:   actorID,
	})
}

func parseTaskID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return 0, false
	}
	return uint(id), true
}

func parseCommentID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("comment_id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_COMMENT_ID", "invalid comment id", err))
		return 0, false
	}
	return uint(id), true
}

func (h *CommentHandler) lookupTask(c *gin.Context, taskID uint) *domain.Task {
	if h.publisher == nil {
		return nil
	}
	task, _ := h.tasks.GetTaskByID(c.Request.Context(), taskID)
	return task
}

func (h *CommentHandler) handleError(c *gin.Context, code string, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		response.HandleError(c, httperrors.NotFound("COMMENT_NOT_FOUND", "comment not found", err))
	case errors.Is(err, domain.ErrCommentForbidden):
		response.HandleError(c, httperrors.Forbidden("COMMENT_FORBIDDEN", err.Error(), err))
	case errors.Is(err, domain.ErrInvalidMentionReference):
		response.HandleError(c, httperrors.BadRequest("INVALID_MENTION", err.Error(), err))
	default:
		response.HandleError(c, httperrors.Internal(code, "internal server error", err))
	}
}

func (h *CommentHandler) Create(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	var req dto.CreateCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	comment, err := h.comments.CreateComment(c.Request.Context(), uid, taskID, req.Body, req.MentionIDs)
	if err != nil {
		h.handleError(c, "COMMENT_CREATE_FAILED", err)
		return
	}
	h.publish(realtime.EventCommentCreated, uid, h.lookupTask(c, taskID), comment.ID, comment)
	response.Success(c, http.StatusCreated, comment)
}

func (h *CommentHandler) List(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	params, err := pagination.ParseOffset(c.Query("page"), c.Query("limit"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", err.Error(), err))
		return
	}

	comments, total, err := h.comments.ListComments(c.Request.Context(), taskID, params.Limit, params.Offset)
	if err != nil {
		h.handleError(c, "COMMENTS_FETCH_FAILED", err)
		return
	}
	pages := (int(total) + params.Limit - 1) / params.Limit
	response.SuccessWithPagination(c, http.StatusOK, comments, &response.Pagination{
		Total: int(total), Page: params.Page, Limit: params.Limit, Pages: pages,
	})
}

func (h *CommentHandler) Update(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	commentID, ok := parseCommentID(c)
	if !ok {
		return
	}
	var req dto.UpdateCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	comment, err := h.comments.UpdateComment(c.Request.Context(), uid, taskID, commentID, req.Body, req.MentionIDs)
	if err != nil {
		h.handleError(c, "COMMENT_UPDATE_FAILED", err)
		return
	}
	h.publish(realtime.EventCommentUpdated, uid, h.lookupTask(c, taskID), commentID, comment)
	response.Success(c, http.StatusOK, comment)
}

func (h *CommentHandler) Delete(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	commentID, ok := parseCommentID(c)
	if !ok {
		return
	}

	if err := h.comments.DeleteComment(c.Request.Context(), uid, taskID, commentID); err != nil {
		h.handleError(c, "COMMENT_DELETE_FAILED", err)
		return
	}
	h.publish(realtime.EventCommentDeleted, uid, h.lookupTask(c, taskID), commentID, nil)
	response.Success(c, http.StatusOK, gin.H{"message": "comment deleted"})
}
