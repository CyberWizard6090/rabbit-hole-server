package response

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"rabbit-hole-server/internal/domain"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
)

func TestHandleErrorRendersAllSupportedStatuses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   string
		err    error
	}{
		{name: "bad request", status: http.StatusBadRequest, code: "BAD_INPUT", err: httperrors.BadRequest("BAD_INPUT", "bad input", errors.New("input"))},
		{name: "unauthorized", status: http.StatusUnauthorized, code: "AUTH_REQUIRED", err: httperrors.Unauthorized("AUTH_REQUIRED", "authentication required", nil)},
		{name: "forbidden", status: http.StatusForbidden, code: "ACCESS_DENIED", err: httperrors.Forbidden("ACCESS_DENIED", "access denied", nil)},
		{name: "not found", status: http.StatusNotFound, code: "RESOURCE_NOT_FOUND", err: httperrors.NotFound("RESOURCE_NOT_FOUND", "resource not found", nil)},
		{name: "conflict", status: http.StatusConflict, code: "RESOURCE_CONFLICT", err: httperrors.Conflict("RESOURCE_CONFLICT", "resource conflict", nil)},
		{name: "unprocessable entity", status: http.StatusUnprocessableEntity, code: "DOMAIN_INVALID", err: httperrors.UnprocessableEntity("DOMAIN_INVALID", "domain rule failed", nil)},
		{name: "too many requests", status: http.StatusTooManyRequests, code: "RATE_LIMITED", err: httperrors.TooManyRequests("RATE_LIMITED", "too many requests", nil)},
		{name: "internal server error", status: http.StatusInternalServerError, code: "INTERNAL_FAILURE", err: httperrors.Internal("INTERNAL_FAILURE", "internal failure", nil)},
		{name: "service unavailable", status: http.StatusServiceUnavailable, code: "SERVICE_DOWN", err: httperrors.ServiceUnavailable("SERVICE_DOWN", "service unavailable", nil)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)

			HandleError(context, tc.err)

			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.status, recorder.Body.String())
			}

			var body APIResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
			}
			if body.Error == nil {
				t.Fatal("response.error is nil")
			}
			if body.Error.Code != tc.code {
				t.Fatalf("error.code = %q, want %q", body.Error.Code, tc.code)
			}
			if strings.Contains(recorder.Body.String(), `"message"`) {
				t.Fatalf("error body must not carry free text: %s", recorder.Body.String())
			}
		})
	}
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) *ErrorPayload {
	t.Helper()
	var body APIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error == nil {
		t.Fatalf("decode error body: %v; body=%s", err, recorder.Body.String())
	}
	return body.Error
}

func TestValidationErrorRendersCodedDetails(t *testing.T) {
	type item struct {
		Text string `json:"text" validate:"required"`
	}
	type request struct {
		Email    string   `json:"email" validate:"required,email"`
		Password string   `json:"password" validate:"min=8"`
		Title    string   `json:"title" validate:"max=3"`
		Count    int      `json:"count" validate:"gte=1"`
		Kind     string   `json:"kind" validate:"oneof=a b"`
		Items    []item   `json:"items" validate:"dive"`
		Tags     []string `json:"tags" validate:"max=1"`
	}
	v := validator.New()
	v.RegisterTagNameFunc(wireName)
	validationErr := v.Struct(request{
		Password: "short", Title: "toolong", Kind: "z",
		Items: []item{{Text: "ok"}, {}},
		Tags:  []string{"a", "b"},
	})

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	ValidationError(context, validationErr)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
	payload := decodeError(t, recorder)
	if payload.Code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q", payload.Code)
	}
	got := map[string]httperrors.Detail{}
	for _, d := range payload.Details {
		got[d.Field] = d
	}
	want := map[string]struct {
		code   string
		params map[string]any
	}{
		"email":         {"REQUIRED", nil},
		"password":      {"TOO_SHORT", map[string]any{"min": float64(8)}},
		"title":         {"TOO_LONG", map[string]any{"max": float64(3)}},
		"count":         {"TOO_SMALL", map[string]any{"min": float64(1)}},
		"kind":          {"INVALID_CHOICE", map[string]any{"allowed": []any{"a", "b"}}},
		"items[1].text": {"REQUIRED", nil},
		"tags":          {"TOO_MANY", map[string]any{"max": float64(1)}},
	}
	if len(got) != len(want) {
		t.Fatalf("details = %+v", payload.Details)
	}
	for field, w := range want {
		d, ok := got[field]
		if !ok || d.Code != w.code {
			t.Fatalf("%s: got %+v, want code %s", field, d, w.code)
		}
		if w.params != nil && !reflect.DeepEqual(roundTrip(t, d.Params), w.params) {
			t.Fatalf("%s: params %v, want %v", field, d.Params, w.params)
		}
	}
}

func roundTrip(t *testing.T, in map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(in)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestValidationErrorReportsMalformedJSON(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
		code   string
		field  string
	}{
		"syntax": {`{"a":`, http.StatusBadRequest, "INVALID_JSON", ""},
		"type":   {`{"n":"x"}`, http.StatusBadRequest, "VALIDATION_FAILED", "n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var dst struct {
				N int `json:"n"`
			}
			err := json.NewDecoder(strings.NewReader(tc.body)).Decode(&dst)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			ValidationError(context, err)
			payload := decodeError(t, recorder)
			if recorder.Code != tc.status || payload.Code != tc.code {
				t.Fatalf("status=%d payload=%+v", recorder.Code, payload)
			}
			if tc.field != "" && (len(payload.Details) != 1 || payload.Details[0].Field != tc.field ||
				payload.Details[0].Code != "INVALID_TYPE") {
				t.Fatalf("details = %+v", payload.Details)
			}
		})
	}
}

func TestErrorBodyCarriesRequestID(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set(contextutil.RequestIDKey, "req-12345678")

	HandleError(context, httperrors.TooManyRequests("RATE_LIMIT_EXCEEDED", "slow down", nil).
		WithParams(map[string]any{"retry_after": 30}))

	payload := decodeError(t, recorder)
	if payload.RequestID != "req-12345678" || payload.Code != "RATE_LIMIT_EXCEEDED" || payload.Params["retry_after"] != float64(30) {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHandleErrorHidesUnknownInternalError(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	HandleError(context, errors.New("database password leaked"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Body.String() == "" || strings.Contains(recorder.Body.String(), "database password leaked") {
		t.Fatalf("internal error leaked in response: %s", recorder.Body.String())
	}
}

func TestHandleErrorClassifiesRecordNotFound(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	HandleError(context, httperrors.Internal("FETCH_FAILED", "fetch failed", domain.ErrNotFound))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	var body APIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error == nil || body.Error.Code != "NOT_FOUND" {
		t.Fatalf("unexpected error payload: %+v", body.Error)
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestHandleErrorLogsServerErrorsAtErrorAndClientErrorsAtDebug(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name      string
		err       error
		wantLevel string
	}{
		{"unknown error is a 500", errors.New("db exploded"), "ERROR"},
		{"internal app error is a 500", httperrors.Internal("X", "x", errors.New("cause")), "ERROR"},
		{"validation-style 400", httperrors.BadRequest("BAD", "bad", errors.New("cause")), "DEBUG"},
		{"missing row is a 404", domain.ErrNotFound, "DEBUG"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLogs(t)
			ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ginCtx.Set("request_id", "req-1234567")

			HandleError(ginCtx, tc.err)

			var line map[string]any
			if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
				t.Fatalf("no JSON log line: %q", buf.String())
			}
			if line["level"] != tc.wantLevel || line["request_id"] != "req-1234567" {
				t.Fatalf("log = %v, want level %s with the request ID", line, tc.wantLevel)
			}
		})
	}
}

func TestHandleErrorWithNilErrorLogsNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := captureLogs(t)
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	HandleError(ginCtx, nil)
	if buf.Len() != 0 {
		t.Fatalf("unexpected log output: %s", buf.String())
	}
}
