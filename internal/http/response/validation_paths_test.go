package response

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	httperrors "rabbit-hole-server/internal/http/errors"
)

func TestValidationErrorOmitsEmbeddedStructNamesFromFieldPaths(t *testing.T) {
	type Base struct {
		Name string `json:"name" validate:"max=3"`
	}
	type request struct {
		Base
		Items []Base `json:"items" validate:"dive"`
	}
	v := validator.New()
	v.RegisterTagNameFunc(wireName)
	validationErr := v.Struct(request{Base: Base{Name: "toolong"}, Items: []Base{{Name: "ok"}, {Name: "toolong"}}})

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	ValidationError(context, validationErr)

	payload := decodeError(t, recorder)
	got := map[string]string{}
	for _, d := range payload.Details {
		got[d.Field] = d.Code
	}
	want := map[string]string{"name": "TOO_LONG", "items[1].name": "TOO_LONG"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details = %v, want %v", got, want)
	}
}

func TestFailWithDetailsRendersEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	FailWithDetails(context, http.StatusBadRequest, "VALIDATION_FAILED", nil,
		[]httperrors.Detail{{Field: "name", Code: "INVALID_VALUE"}})
	payload := decodeError(t, recorder)
	if recorder.Code != http.StatusBadRequest || payload.Code != "VALIDATION_FAILED" ||
		len(payload.Details) != 1 || payload.Details[0].Field != "name" {
		t.Fatalf("status=%d payload=%+v", recorder.Code, payload)
	}
}
