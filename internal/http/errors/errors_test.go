package errors

import (
	"errors"
	"net/http"
	"testing"
)

func TestAppErrorErrorAndUnwrap(t *testing.T) {
	cause := errors.New("cause")
	err := New("CODE", "message", http.StatusBadRequest)
	if err.Error() != "message" {
		t.Fatalf("Error=%q", err.Error())
	}

	wrapped := Wrap("WRAPPED", "wrapped message", http.StatusInternalServerError, cause)
	if wrapped.Error() != cause.Error() {
		t.Fatalf("wrapped Error=%q", wrapped.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("wrapped error does not expose cause")
	}
	if !errors.Is(wrapped.Unwrap(), cause) {
		t.Fatal("Unwrap did not return cause")
	}
}

func TestAppErrorNilReceiver(t *testing.T) {
	var err *AppError
	if err.Error() != "" {
		t.Fatalf("nil Error=%q", err.Error())
	}
	if err.Unwrap() != nil {
		t.Fatalf("nil Unwrap=%v", err.Unwrap())
	}
}

func TestStandardErrorConstructorsSetHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		got  *AppError
		want int
	}{
		{"bad request", BadRequest("X", "x", nil), http.StatusBadRequest},
		{"unauthorized", Unauthorized("X", "x", nil), http.StatusUnauthorized},
		{"forbidden", Forbidden("X", "x", nil), http.StatusForbidden},
		{"not found", NotFound("X", "x", nil), http.StatusNotFound},
		{"conflict", Conflict("X", "x", nil), http.StatusConflict},
		{"unprocessable", UnprocessableEntity("X", "x", nil), http.StatusUnprocessableEntity},
		{"too many", TooManyRequests("X", "x", nil), http.StatusTooManyRequests},
		{"internal", Internal("X", "x", nil), http.StatusInternalServerError},
		{"unavailable", ServiceUnavailable("X", "x", nil), http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.Status != tc.want {
				t.Fatalf("status=%d want %d", tc.got.Status, tc.want)
			}
			if tc.got.Code != "X" || tc.got.Message != "x" {
				t.Fatalf("error=%+v", tc.got)
			}
		})
	}
}

func TestValidationFailedSetsFields(t *testing.T) {
	cause := errors.New("invalid")
	fields := map[string]string{"Email": "email"}
	err := ValidationFailed(fields, cause)
	if err.Code != "VALIDATION_FAILED" || err.Message != "invalid request" || err.Status != http.StatusBadRequest {
		t.Fatalf("error=%+v", err)
	}
	if !errors.Is(err, cause) || err.Fields["Email"] != "email" {
		t.Fatalf("error=%+v", err)
	}
}

func TestWrapfFormatsMessageWithCause(t *testing.T) {
	cause := errors.New("database failure")
	err := Wrapf(
		"TASK_UPDATE_FAILED",
		"failed to update task %d",
		http.StatusInternalServerError,
		cause,
		5,
	)

	if err.Code != "TASK_UPDATE_FAILED" {
		t.Fatalf("code=%q", err.Code)
	}
	if err.Message != "failed to update task 5" {
		t.Fatalf("message=%q", err.Message)
	}
	if err.Status != http.StatusInternalServerError {
		t.Fatalf("status=%d", err.Status)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Wrapf does not expose cause")
	}
}

func TestWrapfHandlesNilCause(t *testing.T) {
	err := Wrapf("CODE", "failed %s", http.StatusConflict, nil, "now")
	if err.Code != "CODE" || err.Message != "failed now" || err.Status != http.StatusConflict {
		t.Fatalf("error=%+v", err)
	}
	if err.Unwrap() != nil {
		t.Fatalf("unwrap=%v", err.Unwrap())
	}
}
