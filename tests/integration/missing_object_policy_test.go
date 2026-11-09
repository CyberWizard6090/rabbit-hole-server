package integration

import (
	"net/http"
	"testing"
)

func errorCodeOf(t *testing.T, tc *TestContext, method, path, token string) (int, string) {
	t.Helper()
	rec := request(t, tc.Router, method, path, token, nil)
	body := decodeJSON(t, rec)
	if e, ok := body["error"].(map[string]any); ok {
		code, _ := e["code"].(string)
		return rec.Code, code
	}
	return rec.Code, ""
}

func TestMissingObjectPolicy_UnreachableObjectsAnswer403ForEveryone(t *testing.T) {
	tc := newTestContext(t)
	ownerID, ownerToken := newUser(t, tc, "policy-owner@example.test")
	fixture := buildRouteFixture(t, tc, ownerToken, ownerID)
	_, outsiderToken := newUser(t, tc, "policy-outsider@example.test")
	p := fixture.params

	paths := []struct{ name, method, path string }{
		{"workspace", http.MethodGet, "/api/v1/workspaces/99999"},
		{"space", http.MethodGet, "/api/v1/spaces/99999"},
		{"folder", http.MethodPatch, "/api/v1/folders/99999"},
		{"list", http.MethodGet, "/api/v1/lists/99999/tasks"},
		{"task", http.MethodGet, "/api/v1/tasks/99999"},
		{"submission status", http.MethodGet, "/api/v1/submissions/99999/task-status"},
		{"submission tasks", http.MethodGet, "/api/v1/submissions/99999/tasks"},
		{"submission retry", http.MethodPost, "/api/v1/submissions/99999/retry-task-creation"},
		{"task template", http.MethodGet, "/api/v1/task-templates/99999"},
		{"task template versions", http.MethodGet, "/api/v1/task-templates/99999/versions"},
		{"task template archive", http.MethodDelete, "/api/v1/task-templates/99999"},
	}
	for _, c := range paths {
		for who, token := range map[string]string{"owner": ownerToken, "outsider": outsiderToken} {
			t.Run(c.name+" as "+who, func(t *testing.T) {
				status, code := errorCodeOf(t, tc, c.method, c.path, token)
				if status != http.StatusForbidden || code != "FORBIDDEN" {
					t.Fatalf("missing %s answered %d %q, want 403 FORBIDDEN", c.name, status, code)
				}
			})
		}
	}

	t.Run("existing but foreign objects look exactly like missing ones", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/submissions/" + p["submission_id"] + "/task-status",
			"/api/v1/task-templates/" + p["template_id"],
			"/api/v1/tasks/" + p["task_id"],
		} {
			status, code := errorCodeOf(t, tc, http.MethodGet, path, outsiderToken)
			if status != http.StatusForbidden || code != "FORBIDDEN" {
				t.Fatalf("%s: %d %q, want 403 FORBIDDEN", path, status, code)
			}
		}
	})
}

func TestMissingObjectPolicy_ChildObjectsOfAnAccessibleContainerAnswer404(t *testing.T) {
	tc := newTestContext(t)
	ownerID, ownerToken := newUser(t, tc, "policy-child-owner@example.test")
	fixture := buildRouteFixture(t, tc, ownerToken, ownerID)
	_, outsiderToken := newUser(t, tc, "policy-child-outsider@example.test")
	ws := "/api/v1/workspaces/" + fixture.params["workspace_id"]

	for _, path := range []string{
		ws + "/forms/99999",
		ws + "/submissions/99999",
		ws + "/custom-fields/99999",
		"/api/v1/spaces/" + fixture.params["space_id"] + "/tags/99999",
	} {
		status, code := errorCodeOf(t, tc, http.MethodGet, path, ownerToken)
		if status != http.StatusNotFound || code == "" {
			t.Fatalf("%s as a member: %d %q, want 404 with a code", path, status, code)
		}
		status, code = errorCodeOf(t, tc, http.MethodGet, path, outsiderToken)
		if status != http.StatusForbidden || code != "FORBIDDEN" {
			t.Fatalf("%s as an outsider: %d %q, want 403 FORBIDDEN", path, status, code)
		}
	}
}
