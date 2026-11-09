package integration

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"
)

type rulesWorld struct {
	tc          *TestContext
	workspaceID uint
	tokens      map[string]string
	ids         map[string]uint
}

func newRulesWorld(t *testing.T, only ...string) rulesWorld {
	t.Helper()
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, fmt.Sprintf("rules%d", time.Now().UnixNano()))
	w := rulesWorld{
		tc: tc, workspaceID: owner.workspaceID,
		tokens: map[string]string{"owner": owner.token},
		ids:    map[string]uint{"owner": owner.userID},
	}
	for _, spec := range []struct{ name, role string }{
		{"admin", "Admin"}, {"other_admin", "Admin"}, {"member", "Member"}, {"guest", "Guest"},
	} {
		if len(only) > 0 && !slices.Contains(only, spec.name) {
			continue
		}
		id, token := newUser(t, tc, fmt.Sprintf("rules-%s-%d@example.test", spec.name, time.Now().UnixNano()))
		addMemberWithRole(t, tc, owner.workspaceID, id, spec.role)
		w.tokens[spec.name] = token
		w.ids[spec.name] = id
	}
	return w
}

func (w rulesWorld) do(t *testing.T, actor, method, suffix string, body any) (int, string) {
	t.Helper()
	rec := request(t, w.tc.Router, method, membersPath(w.workspaceID, suffix), w.tokens[actor], body)
	code := ""
	if rec.Code >= 400 {
		if e, ok := decodeJSON(t, rec)["error"].(map[string]any); ok {
			code, _ = e["code"].(string)
		}
	}
	return rec.Code, code
}

func (w rulesWorld) roleOf(t *testing.T, name string) string {
	t.Helper()
	var role string
	err := w.tc.DB.Raw(`SELECT r.name FROM workspace_members m JOIN roles r ON r.id = m.role_id
		WHERE m.workspace_id = ? AND m.user_id = ? AND m.deleted_at IS NULL`, w.workspaceID, w.ids[name]).Scan(&role).Error
	if err != nil {
		t.Fatalf("role of %s: %v", name, err)
	}
	return role
}

func TestAdminRules_OnlyTheOwnerManagesAdmins(t *testing.T) {
	type step struct {
		name       string
		actor      string
		method     string
		target     string
		body       map[string]any
		wantStatus int
		wantCode   string
		wantRole   string
	}
	steps := []step{
		{name: "admin cannot promote a member to admin", actor: "admin", method: http.MethodPatch, target: "member",
			body: map[string]any{"role": "Admin"}, wantStatus: http.StatusForbidden, wantCode: "ADMIN_ROLE_OWNER_ONLY", wantRole: "Member"},
		{name: "admin cannot demote another admin", actor: "admin", method: http.MethodPatch, target: "other_admin",
			body: map[string]any{"role": "Member"}, wantStatus: http.StatusForbidden, wantCode: "ADMIN_ROLE_OWNER_ONLY", wantRole: "Admin"},
		{name: "admin cannot demote themself", actor: "admin", method: http.MethodPatch, target: "admin",
			body: map[string]any{"role": "Guest"}, wantStatus: http.StatusForbidden, wantCode: "ADMIN_ROLE_OWNER_ONLY", wantRole: "Admin"},
		{name: "admin cannot remove another admin", actor: "admin", method: http.MethodDelete, target: "other_admin",
			wantStatus: http.StatusForbidden, wantCode: "ADMIN_ROLE_OWNER_ONLY", wantRole: "Admin"},
		{name: "admin cannot remove themself through the member route", actor: "admin", method: http.MethodDelete, target: "admin",
			wantStatus: http.StatusForbidden, wantCode: "ADMIN_ROLE_OWNER_ONLY", wantRole: "Admin"},
		{name: "admin cannot demote the owner", actor: "admin", method: http.MethodPatch, target: "owner",
			body: map[string]any{"role": "Guest"}, wantStatus: http.StatusForbidden, wantCode: "OWNER_IMMUTABLE", wantRole: "Owner"},
		{name: "admin cannot remove the owner", actor: "admin", method: http.MethodDelete, target: "owner",
			wantStatus: http.StatusForbidden, wantCode: "OWNER_IMMUTABLE", wantRole: "Owner"},
		{name: "admin demotes a member to guest", actor: "admin", method: http.MethodPatch, target: "member",
			body: map[string]any{"role": "Guest"}, wantStatus: http.StatusOK, wantRole: "Guest"},
		{name: "admin promotes a guest to member", actor: "admin", method: http.MethodPatch, target: "guest",
			body: map[string]any{"role": "Member"}, wantStatus: http.StatusOK, wantRole: "Member"},
		{name: "admin removes a member", actor: "admin", method: http.MethodDelete, target: "member",
			wantStatus: http.StatusOK, wantRole: ""},
		{name: "owner promotes a member to admin", actor: "owner", method: http.MethodPatch, target: "member",
			body: map[string]any{"role": "Admin"}, wantStatus: http.StatusOK, wantRole: "Admin"},
		{name: "owner demotes an admin", actor: "owner", method: http.MethodPatch, target: "other_admin",
			body: map[string]any{"role": "Member"}, wantStatus: http.StatusOK, wantRole: "Member"},
		{name: "owner cannot change their own role", actor: "owner", method: http.MethodPatch, target: "owner",
			body: map[string]any{"role": "Admin"}, wantStatus: http.StatusForbidden, wantCode: "OWNER_IMMUTABLE", wantRole: "Owner"},
		{name: "owner cannot remove themself", actor: "owner", method: http.MethodDelete, target: "owner",
			wantStatus: http.StatusForbidden, wantCode: "OWNER_IMMUTABLE", wantRole: "Owner"},
		{name: "owner removes an admin", actor: "owner", method: http.MethodDelete, target: "admin",
			wantStatus: http.StatusOK, wantRole: ""},
		{name: "member cannot manage members", actor: "member", method: http.MethodPatch, target: "guest",
			body: map[string]any{"role": "Guest"}, wantStatus: http.StatusForbidden, wantCode: "FORBIDDEN", wantRole: "Guest"},
		{name: "guest cannot remove anyone", actor: "guest", method: http.MethodDelete, target: "member",
			wantStatus: http.StatusForbidden, wantCode: "FORBIDDEN", wantRole: "Member"},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			w := newRulesWorld(t)
			status, code := w.do(t, s.actor, s.method, "/"+itoa(w.ids[s.target]), s.body)
			if status != s.wantStatus || code != s.wantCode {
				t.Fatalf("got %d %q, want %d %q", status, code, s.wantStatus, s.wantCode)
			}
			if got := w.roleOf(t, s.target); got != s.wantRole {
				t.Fatalf("role of %s = %q, want %q", s.target, got, s.wantRole)
			}
		})
	}
}

func TestAdminRules_InvitingByContact(t *testing.T) {
	cases := []struct {
		name       string
		actor      string
		role       string
		wantStatus int
		wantCode   string
	}{
		{"owner invites an admin", "owner", "Admin", http.StatusCreated, ""},
		{"owner invites a member", "owner", "Member", http.StatusCreated, ""},
		{"admin invites a member", "admin", "Member", http.StatusCreated, ""},
		{"admin invites a guest", "admin", "Guest", http.StatusCreated, ""},
		{"admin cannot invite an admin", "admin", "Admin", http.StatusForbidden, "ADMIN_ROLE_OWNER_ONLY"},
		{"member cannot invite", "member", "Guest", http.StatusForbidden, "FORBIDDEN"},
		{"guest cannot invite", "guest", "Guest", http.StatusForbidden, "FORBIDDEN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newRulesWorld(t, c.actor)
			inviteeID, _ := newUser(t, w.tc, fmt.Sprintf("invitee-%d@example.test", time.Now().UnixNano()))
			addContact(t, w.tc, w.tokens[c.actor], inviteeID)
			status, code := w.do(t, c.actor, http.MethodPost, "", map[string]any{"user_id": inviteeID, "role": c.role})
			if status != c.wantStatus || code != c.wantCode {
				t.Fatalf("got %d %q, want %d %q", status, code, c.wantStatus, c.wantCode)
			}
		})
	}
}

func TestAdminRules_LeavingTheWorkspace(t *testing.T) {
	cases := []struct {
		actor      string
		wantStatus int
		wantCode   string
	}{
		{"admin", http.StatusOK, ""},
		{"member", http.StatusOK, ""},
		{"guest", http.StatusOK, ""},
		{"owner", http.StatusForbidden, "OWNER_IMMUTABLE"},
	}
	for _, c := range cases {
		t.Run(c.actor, func(t *testing.T) {
			w := newRulesWorld(t, c.actor)
			status, code := w.do(t, c.actor, http.MethodDelete, "/me", nil)
			if status != c.wantStatus || code != c.wantCode {
				t.Fatalf("got %d %q, want %d %q", status, code, c.wantStatus, c.wantCode)
			}
		})
	}
}
