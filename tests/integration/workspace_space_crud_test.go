package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"rabbit-hole-server/internal/persistence"
)

func addMemberWithRole(t *testing.T, tc *TestContext, workspaceID, userID uint, roleName string) {
	t.Helper()
	var role persistence.Role
	if err := tc.DB.Where("workspace_id = ? AND name = ?", workspaceID, roleName).First(&role).Error; err != nil {
		t.Fatalf("find role %s: %v", roleName, err)
	}
	if err := tc.DB.Create(&persistence.Member{WorkspaceID: workspaceID, UserID: userID, RoleID: role.ID}).Error; err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func TestWorkspace_GetUpdateDeleteLifecycle(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", tc.Token, map[string]any{"name": "Inside"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: %d: %s", rec.Code, rec.Body.String())
	}
	spaceID := responseID(t, rec)
	path := "/api/v1/workspaces/" + itoa(workspaceID)

	rec = request(t, tc.Router, http.MethodGet, path, tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPatch, path, tc.Token, map[string]any{"name": "  Renamed  ", "description": "d"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d: %s", rec.Code, rec.Body.String())
	}
	if got := dataObject(t, rec)["name"]; got != "Renamed" {
		t.Fatalf("name = %v, want trimmed %q", got, "Renamed")
	}

	rec = request(t, tc.Router, http.MethodPatch, path, tc.Token, map[string]any{"name": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank name: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodDelete, path, tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d: %s", rec.Code, rec.Body.String())
	}

	for _, p := range []string{path, path + "/spaces", "/api/v1/spaces/" + itoa(spaceID) + "/dashboard"} {
		rec = request(t, tc.Router, http.MethodGet, p, tc.Token, nil)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s after delete: expected 403/404, got %d", p, rec.Code)
		}
	}
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/workspaces", tc.Token, nil)
	if data, _ := decodeJSON(t, rec)["data"].([]any); len(data) != 0 {
		t.Fatalf("deleted workspace is still listed: %v", data)
	}
}

func TestWorkspace_OnlyPermittedRolesCanUpdateOrDelete(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)
	path := "/api/v1/workspaces/" + itoa(workspaceID)
	suffix := time.Now().UnixNano()

	adminID, adminToken := newUser(t, tc, fmt.Sprintf("ws-admin-%d@example.test", suffix))
	memberID, memberToken := newUser(t, tc, fmt.Sprintf("ws-member-%d@example.test", suffix))
	_, outsiderToken := newUser(t, tc, fmt.Sprintf("ws-outsider-%d@example.test", suffix))
	addMemberWithRole(t, tc, workspaceID, adminID, "Admin")
	addMemberWithRole(t, tc, workspaceID, memberID, "Member")

	for name, token := range map[string]string{"member": memberToken, "outsider": outsiderToken} {
		rec := request(t, tc.Router, http.MethodPatch, path, token, map[string]any{"name": "pwned"})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s PATCH: expected 403, got %d", name, rec.Code)
		}
	}
	for name, token := range map[string]string{"admin": adminToken, "member": memberToken, "outsider": outsiderToken} {
		rec := request(t, tc.Router, http.MethodDelete, path, token, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s DELETE: expected 403, got %d", name, rec.Code)
		}
	}
	if rec := request(t, tc.Router, http.MethodPatch, path, adminToken, map[string]any{"name": "renamed by admin"}); rec.Code != http.StatusOK {
		t.Errorf("admin PATCH: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodGet, path, memberToken, nil); rec.Code != http.StatusOK {
		t.Errorf("member GET: expected 200, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, path, outsiderToken, nil); rec.Code != http.StatusForbidden {
		t.Errorf("outsider GET: expected 403, got %d", rec.Code)
	}

	var count int64
	tc.DB.Table("workspaces").Where("id = ? AND name = 'renamed by admin' AND deleted_at IS NULL", workspaceID).Count(&count)
	if count != 1 {
		t.Fatalf("workspace was modified by an unauthorized role, or the admin rename was lost, or it was deleted")
	}
}

func TestSpace_GetUpdateDeleteLifecycle(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)
	path := "/api/v1/spaces/" + itoa(spaceID)

	if rec := request(t, tc.Router, http.MethodGet, path, tc.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("get: %d: %s", rec.Code, rec.Body.String())
	}

	rec := request(t, tc.Router, http.MethodPatch, path, tc.Token, map[string]any{"name": "Renamed", "currency": "eur"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d: %s", rec.Code, rec.Body.String())
	}
	data := dataObject(t, rec)
	if data["name"] != "Renamed" || data["currency"] != "EUR" {
		t.Fatalf("space = %v, want Renamed/EUR", data)
	}

	for _, bad := range []map[string]any{{"name": "   "}, {"currency": "EURO"}, {"currency": "12"}} {
		rec = request(t, tc.Router, http.MethodPatch, path, tc.Token, bad)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PATCH %v: expected 400, got %d: %s", bad, rec.Code, rec.Body.String())
		}
	}

	rec = request(t, tc.Router, http.MethodDelete, path, tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d: %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{path, "/api/v1/spaces/" + itoa(spaceID) + "/dashboard", "/api/v1/lists/" + itoa(listID) + "/tasks"} {
		rec = request(t, tc.Router, http.MethodGet, p, tc.Token, nil)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s after delete: expected 403/404, got %d", p, rec.Code)
		}
	}
}

func TestSpace_MemberCannotUpdateOrDelete(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	var workspaceID uint
	tc.DB.Table("spaces").Select("workspace_id").Where("id = ?", spaceID).Scan(&workspaceID)

	memberID, memberToken := newUser(t, tc, fmt.Sprintf("space-member-%d@example.test", time.Now().UnixNano()))
	addMemberWithRole(t, tc, workspaceID, memberID, "Member")
	path := "/api/v1/spaces/" + itoa(spaceID)

	if rec := request(t, tc.Router, http.MethodPatch, path, memberToken, map[string]any{"name": "x"}); rec.Code != http.StatusForbidden {
		t.Errorf("member PATCH: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, path, memberToken, nil); rec.Code != http.StatusForbidden {
		t.Errorf("member DELETE: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, path, memberToken, nil); rec.Code != http.StatusOK {
		t.Errorf("member GET: expected 200, got %d", rec.Code)
	}
}
