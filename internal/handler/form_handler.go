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
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type FormHandler struct {
	service service.FormService
}

func NewFormHandler(service service.FormService) *FormHandler {
	return &FormHandler{service: service}
}

func (h *FormHandler) Create(c *gin.Context) {
	workspaceID, ok := parseFormID(c, "workspace_id")
	if !ok {
		return
	}
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	var req dto.FormRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	form, err := h.service.Create(c.Request.Context(), workspaceID, userID, service.FormInput{
		Name: req.Name, Description: req.Description, Slug: req.Slug, Settings: req.Settings,
	})
	if err != nil {
		h.writeError(c, "FORM_CREATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, form)
}

func (h *FormHandler) List(c *gin.Context) {
	workspaceID, ok := parseFormID(c, "workspace_id")
	if !ok {
		return
	}
	forms, err := h.service.List(c.Request.Context(), workspaceID)
	if err != nil {
		response.HandleError(c, httperrors.Internal("FORMS_FETCH_FAILED", "internal server error", err))
		return
	}
	response.Success(c, http.StatusOK, forms)
}

func (h *FormHandler) Get(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	form, err := h.service.Get(c.Request.Context(), workspaceID, formID)
	if err != nil {
		h.writeError(c, "FORM_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, form)
}

func (h *FormHandler) Update(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	var req dto.FormRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	form, err := h.service.Update(c.Request.Context(), workspaceID, formID, service.FormInput{
		Name: req.Name, Description: req.Description, Slug: req.Slug, Settings: req.Settings,
	})
	if err != nil {
		h.writeError(c, "FORM_UPDATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, form)
}

func (h *FormHandler) Delete(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), workspaceID, formID); err != nil {
		h.writeError(c, "FORM_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "form deleted"})
}

func (h *FormHandler) Archive(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	if err := h.service.Archive(c.Request.Context(), workspaceID, formID); err != nil {
		h.writeError(c, "FORM_ARCHIVE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "form archived"})
}

func (h *FormHandler) CreateQuestion(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	var req dto.FormQuestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	question, err := h.service.CreateQuestion(
		c.Request.Context(), workspaceID, formID, formQuestionInput(req),
	)
	if err != nil {
		h.writeError(c, "FORM_QUESTION_CREATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, question)
}

func (h *FormHandler) UpdateQuestion(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	questionID, ok := parseFormID(c, "question_id")
	if !ok {
		return
	}
	var req dto.FormQuestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	question, err := h.service.UpdateQuestion(
		c.Request.Context(), workspaceID, formID, questionID, formQuestionInput(req),
	)
	if err != nil {
		h.writeError(c, "FORM_QUESTION_UPDATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, question)
}

func (h *FormHandler) DeleteQuestion(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	questionID, ok := parseFormID(c, "question_id")
	if !ok {
		return
	}
	if err := h.service.DeleteQuestion(c.Request.Context(), workspaceID, formID, questionID); err != nil {
		h.writeError(c, "FORM_QUESTION_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "form question deleted"})
}

func (h *FormHandler) ReorderQuestions(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	var req dto.ReorderFormQuestionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.service.ReorderQuestions(c.Request.Context(), workspaceID, formID, req.IDs); err != nil {
		h.writeError(c, "FORM_QUESTION_REORDER_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "form questions reordered"})
}

func (h *FormHandler) Publish(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	form, err := h.service.Publish(c.Request.Context(), workspaceID, formID)
	if err != nil {
		h.writeError(c, "FORM_PUBLISH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, form)
}

func (h *FormHandler) GetPublic(c *gin.Context) {
	form, err := h.service.GetPublic(c.Request.Context(), c.Param("slug"))
	if err != nil {
		h.writeError(c, "PUBLIC_FORM_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, form)
}

func (h *FormHandler) Submit(c *gin.Context) {
	var req dto.FormSubmissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	submission, replay, err := h.service.Submit(
		c.Request.Context(), c.Param("slug"), c.GetHeader("Idempotency-Key"), req.Answers,
	)
	if err != nil {
		h.writeError(c, "FORM_SUBMISSION_FAILED", err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	response.Success(c, status, submission)
}

func (h *FormHandler) ListSubmissions(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	submissions, err := h.service.ListSubmissions(c.Request.Context(), workspaceID, formID)
	if err != nil {
		h.writeError(c, "FORM_SUBMISSIONS_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, submissions)
}

func (h *FormHandler) GetSubmission(c *gin.Context) {
	workspaceID, ok := parseFormID(c, "workspace_id")
	if !ok {
		return
	}
	submissionID, ok := parseFormID(c, "submission_id")
	if !ok {
		return
	}
	submission, err := h.service.GetSubmission(c.Request.Context(), workspaceID, submissionID)
	if err != nil {
		h.writeError(c, "SUBMISSION_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, submission)
}

func (h *FormHandler) writeError(c *gin.Context, code string, err error) {
	writeFormError(c, code, err)
}

func writeFormError(c *gin.Context, code string, err error) {
	var formErr *domain.FormError
	switch {
	case errors.As(err, &formErr):
		status := formErr.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		response.HandleError(c, httperrors.Wrap(formErr.Code, formErr.Message, status, err))
	case errors.Is(err, domain.ErrFormClosed):
		response.HandleError(c, httperrors.Wrap("FORM_CLOSED", err.Error(), http.StatusGone, err))
	case errors.Is(err, domain.ErrFormFull):
		response.HandleError(c, httperrors.Conflict("FORM_FULL", err.Error(), err))
	case errors.Is(err, domain.ErrInvalidForm), errors.Is(err, domain.ErrInvalidSubmission):
		response.HandleError(c, httperrors.BadRequest("INVALID_FORM", err.Error(), err))
	case errors.Is(err, domain.ErrFormSubmissionsLimited):
		response.HandleError(c, httperrors.TooManyRequests("FORM_SUBMISSIONS_LIMITED", err.Error(), err))
	case errors.Is(err, domain.ErrIdempotencyConflict):
		response.HandleError(c, httperrors.Conflict("IDEMPOTENCY_CONFLICT", err.Error(), err))
	case errors.Is(err, domain.ErrFormSlugTaken):
		response.HandleError(c, httperrors.Conflict("FORM_SLUG_TAKEN", err.Error(), err))
	case errors.Is(err, domain.ErrFormHasSubmissions):
		response.HandleError(c, httperrors.Conflict("FORM_HAS_SUBMISSIONS", err.Error(), err))
	case errors.Is(err, domain.ErrQuestionInUse):
		response.HandleError(c, httperrors.Conflict("FORM_QUESTION_IN_USE", err.Error(), err))
	case errors.Is(err, domain.ErrFormNotPublished):
		response.HandleError(c, httperrors.NotFound("FORM_NOT_FOUND", "published form not found", err))
	case errors.Is(err, domain.ErrFormNotFound), errors.Is(err, domain.ErrNotFound):
		response.HandleError(c, httperrors.NotFound("FORM_NOT_FOUND", "form or submission not found", err))
	default:
		response.HandleError(c, httperrors.Internal(code, "internal server error", err))
	}
}

func formQuestionInput(req dto.FormQuestionRequest) service.QuestionInput {
	options := make([]domain.QuestionOption, len(req.Options))
	for i, option := range req.Options {
		options[i] = domain.QuestionOption{Label: option.Label, Value: option.Value, Position: i}
	}
	return service.QuestionInput{
		Type: req.Type, Title: req.Title, Description: req.Description,
		Required: req.Required, Position: req.Position, Settings: req.Settings, Options: options,
	}
}

func parseFormRouteIDs(c *gin.Context) (uint, uint, bool) {
	workspaceID, ok := parseFormID(c, "workspace_id")
	if !ok {
		return 0, 0, false
	}
	formID, ok := parseFormID(c, "form_id")
	return workspaceID, formID, ok
}

func parseFormID(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_ID", "invalid "+name, err))
		return 0, false
	}
	return uint(id), true
}
