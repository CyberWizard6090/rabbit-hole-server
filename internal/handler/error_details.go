package handler

import (
	"errors"

	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
)

func problemDetails(problems []domain.FieldProblem, defaultField string) []httperrors.Detail {
	details := make([]httperrors.Detail, len(problems))
	for i, problem := range problems {
		field := problem.Field
		if field == "" {
			field = defaultField
		}
		details[i] = httperrors.Detail{Field: field, Code: problem.Reason, Params: problem.Params}
	}
	return details
}

func withFieldDetails(appErr *httperrors.AppError, err error, defaultField string) *httperrors.AppError {
	var fieldErr *domain.FieldError
	if errors.As(err, &fieldErr) {
		appErr.Details = problemDetails(fieldErr.Problems, defaultField)
	}
	return appErr
}
