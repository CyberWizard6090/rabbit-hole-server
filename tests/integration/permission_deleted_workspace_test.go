package integration

import (
	"net/http"
	"testing"
)

func TestPermissions_SoftDeletedWorkspaceGrantsNothing(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", tc.Token, map[string]any{"name": "S"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: %d: %s", rec.Code, rec.Body.String())
	}
	spaceID := responseID(t, rec)
	listID := createList(t, tc, spaceID)

	paths := []string{
		"/api/v1/workspaces/" + itoa(workspaceID) + "/spaces",
		"/api/v1/spaces/" + itoa(spaceID) + "/dashboard",
		"/api/v1/lists/" + itoa(listID) + "/tasks",
	}
	for _, p := range paths {
		if rec := request(t, tc.Router, http.MethodGet, p, tc.Token, nil); rec.Code != http.StatusOK {
			t.Fatalf("GET %s before delete: expected 200, got %d: %s", p, rec.Code, rec.Body.String())
		}
	}

	if err := tc.DB.Exec("UPDATE workspaces SET deleted_at = now() WHERE id = ?", workspaceID).Error; err != nil {
		t.Fatal(err)
	}

	for _, p := range paths {
		if rec := request(t, tc.Router, http.MethodGet, p, tc.Token, nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s after workspace soft-delete: expected 403, got %d", p, rec.Code)
		}
	}
}
