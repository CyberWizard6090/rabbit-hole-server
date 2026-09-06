package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
)

func newUser(t *testing.T, tc *TestContext, email string) (uint, string) {
	t.Helper()

	password := "password-123"
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register %s: %d: %s", email, rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: %d: %s", email, rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	token := body["access_token"].(string)

	var user domain.User
	if err := tc.DB.Where("email = ?", email).First(&user).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}
	return user.ID, token
}

func TestPermissions_AdminCanCreateSpaceMemberCannot(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	uniqueSuffix := time.Now().UnixNano()
	adminID, adminToken := newUser(t, tc, fmt.Sprintf("admin-%s-%d@example.test", t.Name(), uniqueSuffix))
	memberID, memberToken := newUser(t, tc, fmt.Sprintf("member-%s-%d@example.test", t.Name(), uniqueSuffix))

	var adminRole, memberRole domain.Role
	if err := tc.DB.Where("workspace_id = ? AND name = ?", workspaceID, "Admin").First(&adminRole).Error; err != nil {
		t.Fatalf("admin role: %v", err)
	}
	if err := tc.DB.Where("workspace_id = ? AND name = ?", workspaceID, "Member").First(&memberRole).Error; err != nil {
		t.Fatalf("member role: %v", err)
	}

	if err := tc.DB.Create(&domain.Member{WorkspaceID: workspaceID, UserID: adminID, RoleID: adminRole.ID}).Error; err != nil {
		t.Fatalf("add admin: %v", err)
	}
	if err := tc.DB.Create(&domain.Member{WorkspaceID: workspaceID, UserID: memberID, RoleID: memberRole.ID}).Error; err != nil {
		t.Fatalf("add member: %v", err)
	}

	rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/workspaces/"+itoa(workspaceID)+"/spaces",
		adminToken,
		map[string]any{"name": "Admin Space"},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create space: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost,
		"/api/v1/workspaces/"+itoa(workspaceID)+"/spaces",
		memberToken,
		map[string]any{"name": "Member Space"},
	)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member create space: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
