package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type matrixRole string

const (
	roleOwner   matrixRole = "owner"
	roleAdmin   matrixRole = "admin"
	roleMember  matrixRole = "member"
	roleGuest   matrixRole = "guest"
	roleOutside matrixRole = "outsider"
)

var (
	everyone       = []matrixRole{roleOwner, roleAdmin, roleMember, roleGuest}
	editors        = []matrixRole{roleOwner, roleAdmin, roleMember}
	managers       = []matrixRole{roleOwner, roleAdmin}
	matrixAllRoles = []matrixRole{roleOwner, roleAdmin, roleMember, roleGuest, roleOutside}
)

type matrixRoute struct {
	method  string
	path    string
	allowed []matrixRole
}

func (r matrixRoute) permits(role matrixRole) bool {
	for _, a := range r.allowed {
		if a == role {
			return true
		}
	}
	return false
}

const (
	wsForms   = "/api/v1/workspaces/:workspace_id/forms"
	wsForm    = wsForms + "/:form_id"
	wsFields  = "/api/v1/workspaces/:workspace_id/custom-fields"
	wsTpls    = "/api/v1/workspaces/:workspace_id/task-templates"
	wsMembers = "/api/v1/workspaces/:workspace_id/members"
)

func roleMatrixRoutes() []matrixRoute {
	return []matrixRoute{
		{http.MethodGet, "/api/v1/workspaces/:workspace_id", everyone},
		{http.MethodGet, "/api/v1/workspaces/:workspace_id/search?q=fixture", everyone},
		{http.MethodGet, wsMembers, everyone},
		{http.MethodGet, wsFields, everyone},
		{http.MethodGet, wsFields + "/:field_id", everyone},
		{http.MethodGet, "/api/v1/tasks/:task_id/custom-fields", everyone},

		{http.MethodGet, wsForms, editors},
		{http.MethodGet, wsForm, editors},
		{http.MethodGet, wsForm + "/submissions", editors},
		{http.MethodGet, wsForm + "/task-config", editors},
		{http.MethodGet, "/api/v1/workspaces/:workspace_id/submissions/:submission_id", editors},
		{http.MethodGet, "/api/v1/workspaces/:workspace_id/submissions/:submission_id/files/:file_id", editors},
		{http.MethodGet, "/api/v1/submissions/:submission_id/tasks", editors},
		{http.MethodGet, "/api/v1/submissions/:submission_id/task-status", editors},
		{http.MethodGet, wsTpls, editors},
		{http.MethodGet, "/api/v1/task-templates/:template_id", editors},
		{http.MethodGet, "/api/v1/task-templates/:template_id/versions", editors},
		{http.MethodPost, "/api/v1/task-templates/:template_id/apply", editors},

		{http.MethodPost, wsForms, managers},
		{http.MethodPatch, wsForm, managers},
		{http.MethodPost, wsForm + "/publish", managers},
		{http.MethodPost, wsForm + "/questions", managers},
		{http.MethodPatch, wsForm + "/questions/:question_id", managers},
		{http.MethodPut, wsForm + "/questions/reorder", managers},
		{http.MethodPut, wsForm + "/task-config", managers},
		{http.MethodPost, "/api/v1/submissions/:submission_id/retry-task-creation", managers},
		{http.MethodPost, wsFields, managers},
		{http.MethodPatch, wsFields + "/:field_id", managers},
		{http.MethodPost, wsTpls, managers},
		{http.MethodPatch, "/api/v1/task-templates/:template_id", managers},
		{http.MethodPost, "/api/v1/task-templates/:template_id/versions", managers},
		{http.MethodPost, wsForm + "/archive", managers},

		{http.MethodDelete, wsForm + "/questions/:question_id", managers},
		{http.MethodDelete, "/api/v1/task-templates/:template_id", managers},
		{http.MethodDelete, wsFields + "/:field_id", managers},
		{http.MethodDelete, wsForm, managers},
		{http.MethodPost, wsMembers, managers},
		{http.MethodPatch, wsMembers + "/:user_id", managers},
		{http.MethodDelete, wsMembers + "/:user_id", managers},
	}
}

type matrixWorld struct {
	fixture routeFixture
	tokens  map[matrixRole]string
}

func newMatrixWorld(t *testing.T, tc *TestContext) matrixWorld {
	t.Helper()
	stamp := time.Now().UnixNano()
	ownerID, ownerToken := newUser(t, tc, fmt.Sprintf("matrix-owner-%d@example.test", stamp))
	fixture := buildRouteFixture(t, tc, ownerToken, ownerID)
	var workspaceID uint
	if _, err := fmt.Sscanf(fixture.params["workspace_id"], "%d", &workspaceID); err != nil {
		t.Fatalf("workspace id: %v", err)
	}
	tokens := map[matrixRole]string{roleOwner: ownerToken}
	roleNames := map[matrixRole]string{roleAdmin: "Admin", roleMember: "Member", roleGuest: "Guest"}
	for _, role := range []matrixRole{roleAdmin, roleMember, roleGuest} {
		id, token := newUser(t, tc, fmt.Sprintf("matrix-%s-%d@example.test", role, stamp))
		addMemberWithRole(t, tc, workspaceID, id, roleNames[role])
		tokens[role] = token
		if role == roleMember {
			fixture.params["user_id"] = itoa(id)
		}
	}
	_, tokens[roleOutside] = newUser(t, tc, fmt.Sprintf("matrix-outsider-%d@example.test", stamp))
	return matrixWorld{fixture: fixture, tokens: tokens}
}

func TestRoleMatrix_FormTemplateFieldAndMemberRoutes(t *testing.T) {
	routes := roleMatrixRoutes()
	for _, role := range matrixAllRoles {
		t.Run(string(role), func(t *testing.T) {
			tc := newTestContext(t)
			world := newMatrixWorld(t, tc)
			for _, route := range routes {
				path := world.fixture.expand(t, route.path)
				rec := request(t, tc.Router, route.method, path, world.tokens[role], nil)
				denied := rec.Code == http.StatusForbidden
				if route.permits(role) && denied {
					t.Errorf("%s %s as %s: got 403, want access: %s", route.method, route.path, role, rec.Body.String())
				}
				if !route.permits(role) && !denied {
					t.Errorf("%s %s as %s: got %d, want 403: %s", route.method, route.path, role, rec.Code, rec.Body.String())
				}
			}
		})
	}
}

func TestRoleMatrix_CoversEveryManagementRoute(t *testing.T) {
	tc := newTestContext(t)
	covered := map[string]bool{}
	for _, r := range roleMatrixRoutes() {
		path, _, _ := strings.Cut(r.path, "?")
		covered[r.method+" "+path] = true
	}
	exempt := map[string]string{
		"DELETE /api/v1/workspaces/:workspace_id/members/me":    "every member may leave; covered by TestAdminRules_LeavingTheWorkspace",
		"PUT /api/v1/tasks/:task_id/custom-fields/:field_id":    "task value, guarded by task.update",
		"DELETE /api/v1/tasks/:task_id/custom-fields/:field_id": "task value, guarded by task.update",
		"GET /api/v1/form-templates":                            "static catalog of presets, no workspace data",
		"GET /api/v1/list-templates":                            "static catalog of list templates, no workspace data",
	}
	for _, route := range apiRoutes(t, tc) {
		path := route.Path
		if strings.Contains(path, "/public/") {
			continue
		}
		relevant := strings.Contains(path, "/forms") || strings.Contains(path, "/submissions") ||
			strings.Contains(path, "/task-templates") || strings.Contains(path, "/custom-fields") ||
			strings.HasSuffix(path, "/members") || strings.Contains(path, "/members/")
		key := route.Method + " " + path
		if !relevant || covered[key] {
			continue
		}
		if _, ok := exempt[key]; ok {
			continue
		}
		t.Errorf("%s is a form, template, field, submission or member route missing from roleMatrixRoutes", key)
	}
}

func TestRoleMatrix_GuestInboxHidesFormSubmissionNotices(t *testing.T) {
	tc := newTestContext(t)
	world := newMatrixWorld(t, tc)
	var workspaceID uint
	if _, err := fmt.Sscanf(world.fixture.params["workspace_id"], "%d", &workspaceID); err != nil {
		t.Fatal(err)
	}
	var users []struct {
		ID    uint
		Email string
	}
	if err := tc.DB.Raw("SELECT id, email FROM users").Scan(&users).Error; err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if err := tc.DB.Exec(
			"INSERT INTO notifications (user_id, type, workspace_id, payload) VALUES (?, 'form_submission', ?, '{}'::jsonb)",
			u.ID, workspaceID,
		).Error; err != nil {
			t.Fatal(err)
		}
	}
	counts := map[matrixRole]int{}
	for _, role := range []matrixRole{roleOwner, roleAdmin, roleMember, roleGuest} {
		rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications", world.tokens[role], nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s inbox: %d", role, rec.Code)
		}
		counts[role] = len(decodeJSON(t, rec)["data"].([]any))
	}
	if counts[roleOwner] != 1 || counts[roleAdmin] != 1 || counts[roleMember] != 1 {
		t.Fatalf("editors must see the submission notice: %v", counts)
	}
	if counts[roleGuest] != 0 {
		t.Fatalf("guest sees %d submission notices, want 0", counts[roleGuest])
	}
}
