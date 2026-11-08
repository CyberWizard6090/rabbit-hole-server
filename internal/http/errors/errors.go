package errors

import (
	"fmt"
	"net/http"
)

type Detail struct {
	Field  string         `json:"field"`
	Code   string         `json:"code"`
	Params map[string]any `json:"params,omitempty"`
}

type AppError struct {
	Code    string
	Message string
	Status  int
	Params  map[string]any
	Details []Detail
	Err     error
}

func (e *AppError) WithParams(params map[string]any) *AppError {
	e.Params = params
	return e
}

func (e *AppError) WithDetails(details ...Detail) *AppError {
	e.Details = details
	return e
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(code, message string, status int) *AppError {
	return &AppError{Code: code, Message: message, Status: status}
}

func BadRequest(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusBadRequest, Err: err}
}

func Unauthorized(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusUnauthorized, Err: err}
}

func Forbidden(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusForbidden, Err: err}
}

func NotFound(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusNotFound, Err: err}
}

func Conflict(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusConflict, Err: err}
}

func UnprocessableEntity(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusUnprocessableEntity, Err: err}
}

func TooManyRequests(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusTooManyRequests, Err: err}
}

func Internal(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusInternalServerError, Err: err}
}

func ServiceUnavailable(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusServiceUnavailable, Err: err}
}

func ValidationFailed(details []Detail, err error) *AppError {
	return &AppError{
		Code:    "VALIDATION_FAILED",
		Message: "invalid request",
		Status:  http.StatusBadRequest,
		Details: details,
		Err:     err,
	}
}

func Wrap(code, message string, status int, err error) *AppError {
	if message == "" {
		message = err.Error()
	}
	return &AppError{Code: code, Message: message, Status: status, Err: err}
}

func Wrapf(code, message string, status int, err error, args ...any) *AppError {
	message = fmt.Sprintf(message, args...)
	return &AppError{
		Code:    code,
		Message: message,
		Status:  status,
		Err:     err,
	}
}
