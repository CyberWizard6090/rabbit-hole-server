package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestValidationErrors_UseSharedEnvelopeWithoutInternals(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)

	cases := []struct {
		name, method, path string
		body               map[string]any
	}{
		{"create tag without name", http.MethodPost, "/api/v1/spaces/" + itoa(spaceID) + "/tags", map[string]any{"color": "#123456"}},
		{"create tag with bad color", http.MethodPost, "/api/v1/spaces/" + itoa(spaceID) + "/tags", map[string]any{"name": "x", "color": "red"}},
		{"create workspace without name", http.MethodPost, "/api/v1/workspaces", map[string]any{}},
		{"update profile with wrong type", http.MethodPatch, "/api/v1/profile", map[string]any{"username": 123}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := request(t, tc.Router, c.method, c.path, tc.Token, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			errBody, _ := decodeJSON(t, rec)["error"].(map[string]any)
			if errBody["code"] != "VALIDATION_FAILED" {
				t.Fatalf("error code = %v, want VALIDATION_FAILED: %s", errBody["code"], rec.Body.String())
			}
			for _, leak := range []string{"Key:", "Field validation", "json: cannot unmarshal", "Go struct"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("response leaks internals (%q): %s", leak, rec.Body.String())
				}
			}
		})
	}
}

func TestValidationErrors_ReportJSONFieldNamesWithStableCodes(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token,
		map[string]any{"name": "x", "color": "red"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	errBody, _ := decodeJSON(t, rec)["error"].(map[string]any)
	if detail := errorDetail(errBody, "color"); detail == nil || detail["code"] != "INVALID_COLOR" {
		t.Fatalf("details = %v, want color/INVALID_COLOR: %s", errBody["details"], rec.Body.String())
	}
	if _, has := errBody["message"]; has {
		t.Fatalf("error carries free text: %s", rec.Body.String())
	}
	if _, has := errBody["fields"]; has {
		t.Fatalf("old fields map is still sent: %s", rec.Body.String())
	}
	if id, _ := errBody["request_id"].(string); id == "" || id != rec.Header().Get("X-Request-ID") {
		t.Fatalf("request_id %q does not match X-Request-ID %q", id, rec.Header().Get("X-Request-ID"))
	}

	missing := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", tc.Token, map[string]any{})
	missingBody, _ := decodeJSON(t, missing)["error"].(map[string]any)
	if detail := errorDetail(missingBody, "name"); detail == nil || detail["code"] != "REQUIRED" {
		t.Fatalf("details = %v, want name/REQUIRED: %s", missingBody["details"], missing.Body.String())
	}

	wrongType := request(t, tc.Router, http.MethodPatch, "/api/v1/profile", tc.Token, map[string]any{"username": 123})
	typeBody, _ := decodeJSON(t, wrongType)["error"].(map[string]any)
	if detail := errorDetail(typeBody, "username"); detail == nil || detail["code"] != "INVALID_TYPE" {
		t.Fatalf("details = %v, want username/INVALID_TYPE: %s", typeBody["details"], wrongType.Body.String())
	}
}

func TestSecurityHeaders_PresentOnAPIResponses(t *testing.T) {
	tc := newTestContext(t)
	rec := request(t, tc.Router, http.MethodGet, "/healthz", "", nil)
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-store",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}
