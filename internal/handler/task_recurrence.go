package handler

import (
	"context"
	"errors"

	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/realtime"
)

// recurrenceError answers an invalid repeat rule with VALIDATION_FAILED and one detail per problem.
func recurrenceError(err error) (*httperrors.AppError, bool) {
	var fieldErr *domain.FieldError
	if !errors.Is(err, domain.ErrInvalidRecurrence) || !errors.As(err, &fieldErr) {
		return nil, false
	}
	return httperrors.BadRequest("VALIDATION_FAILED", "invalid recurrence", err).
		WithDetails(problemDetails(fieldErr.Problems, "recurrence")...), true
}

// publishNextOccurrence announces the task a completed repeating task created in its place.
func (h *TaskHandler) publishNextOccurrence(ctx context.Context, userID uint, completed *domain.Task) {
	if completed == nil || completed.NextOccurrenceID == nil {
		return
	}
	next, err := h.service.GetTaskByID(ctx, *completed.NextOccurrenceID)
	if err != nil {
		return
	}
	h.publishTask(realtime.EventTaskCreated, userID, next)
}
