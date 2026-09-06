package integration

import (
	"net/http"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestWorkspace_CreateCreatesOwnerRolesAndMember(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/", tc.Token, map[string]any{
		"name":        "Integration Workspace",
		"description": "integration test",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	workspace := dataObject(t, rec)

	var ws domain.Workspace
	if err := tc.DB.First(&ws, uint(workspace["ID"].(float64))).Error; err != nil {
		t.Fatalf("find workspace: %v", err)
	}

	if ws.OwnerID != tc.UserID {
		t.Fatalf("owner_id = %d, want %d", ws.OwnerID, tc.UserID)
	}

	var roles []domain.Role
	if err := tc.DB.Where("workspace_id = ?", ws.ID).Find(&roles).Error; err != nil {
		t.Fatalf("find roles: %v", err)
	}
	if len(roles) != 4 {
		t.Fatalf("roles count = %d, want 4", len(roles))
	}

	var member domain.Member
	if err := tc.DB.Where("workspace_id = ? AND user_id = ?", ws.ID, tc.UserID).First(&member).Error; err != nil {
		t.Fatalf("find owner member: %v", err)
	}

	var ownerRole domain.Role
	if err := tc.DB.First(&ownerRole, member.RoleID).Error; err != nil {
		t.Fatalf("find owner role: %v", err)
	}
	if ownerRole.Name != "Owner" {
		t.Fatalf("owner role = %q, want Owner", ownerRole.Name)
	}
}

func TestWorkspace_CreateValidatesName(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/", tc.Token, map[string]any{
		"name": " ",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkspace_ListForUser(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)

	for _, name := range []string{"Workspace A", "Workspace B"} {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/", tc.Token, map[string]any{
			"name": name,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create workspace: %d: %s", rec.Code, rec.Body.String())
		}
	}

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/workspaces/", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list workspaces: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	data, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %v", body["data"])
	}
	if len(data) < 2 {
		t.Fatalf("workspace count = %d, want at least 2", len(data))
	}
}
