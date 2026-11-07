package handler

import (
	"errors"

	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
)

func formBadRequest(defaultCode string, err error) *httperrors.AppError {
	code := defaultCode
	var validation *domain.FormValidationError
	if !errors.As(err, &validation) {
		return httperrors.BadRequest(code, err.Error(), err)
	}
	if validation.Code != "" {
		code = validation.Code
	}
	appErr := httperrors.BadRequest(code, err.Error(), err)
	if validation.Field != "" {
		appErr.Fields = map[string]string{validation.Field: validation.Reason}
	}
	return appErr
}
