package integration

import (
	"net/http"
	"strings"
	"testing"
)

func auditPath(workspaceID uint, query string) string {
	return "/api/v1/workspaces/" + itoa(workspaceID) + "/audit-log" + query
}

func auditEntries(t *testing.T, tc *TestContext, token string, workspaceID uint, query string) []map[string]any {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, auditPath(workspaceID, query), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit log: %d: %s", rec.Code, rec.Body.String())
	}
	raw, _ := decodeJSON(t, rec)["data"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(map[string]any))
	}
	return out
}

func auditActions(entries []map[string]any) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i], _ = e["action"].(string)
	}
	return out
}

func TestWorkspaceAudit_RecordsWhoChangedWhatAndOnlyOwnerAndAdminsReadIt(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "audit")
	adminID, adminToken := newUser(t, tc, "audit-admin@example.test")
	memberID, memberToken := newUser(t, tc, "audit-member@example.test")
	_, outsiderToken := newUser(t, tc, "audit-outsider@example.test")
	for _, u := range []struct {
		id   uint
		role string
	}{{adminID, "Admin"}, {memberID, "Member"}} {
		addContact(t, tc, owner.token, u.id)
		if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
			map[string]any{"user_id": u.id, "role": u.role}); rec.Code != http.StatusCreated {
			t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
		}
	}
	wsPath := "/api/v1/workspaces/" + itoa(owner.workspaceID)

	if rec := request(t, tc.Router, http.MethodPatch, wsPath, adminToken,
		map[string]any{"name": "Renamed by admin", "description": "new description"}); rec.Code != http.StatusOK {
		t.Fatalf("admin rename: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPatch, wsPath, adminToken,
		map[string]any{"name": "Renamed by admin"}); rec.Code != http.StatusOK {
		t.Fatalf("repeat rename: %d", rec.Code)
	}
	if rec := uploadRequest(t, tc, workspaceAvatarPath(owner.workspaceID), adminToken, "logo.png", rgbaPNG(t, 2, 2, make([]byte, 16))); rec.Code != http.StatusOK {
		t.Fatalf("avatar: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodDelete, workspaceAvatarPath(owner.workspaceID), adminToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove avatar: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, workspaceAvatarPath(owner.workspaceID), adminToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove avatar again: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPatch, membersPath(owner.workspaceID, "/"+itoa(memberID)), adminToken,
		map[string]any{"role": "Guest"}); rec.Code != http.StatusOK {
		t.Fatalf("change role: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/"+itoa(memberID)), owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove member: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/me"), adminToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin leaves: %d: %s", rec.Code, rec.Body.String())
	}

	entries := auditEntries(t, tc, owner.token, owner.workspaceID, "")
	want := []string{
		"member.left", "member.removed", "member.role_changed", "workspace.avatar_removed",
		"workspace.avatar_changed", "workspace.updated", "member.added", "member.added",
	}
	if got := auditActions(entries); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("actions = %v, want %v", got, want)
	}

	updated := entries[5]
	changes := updated["changes"].(map[string]any)
	name := changes["name"].(map[string]any)
	if name["to"] != "Renamed by admin" || changes["description"].(map[string]any)["to"] != "new description" {
		t.Fatalf("update changes = %v", changes)
	}
	if actor := updated["actor"].(map[string]any); uint(actor["id"].(float64)) != adminID {
		t.Fatalf("actor = %v, want the admin", actor)
	}
	role := entries[2]
	if c := role["changes"].(map[string]any); c["from"] != "Member" || c["to"] != "Guest" {
		t.Fatalf("role change = %v", c)
	}
	if target := role["target"].(map[string]any); uint(target["id"].(float64)) != memberID {
		t.Fatalf("target = %v", target)
	}
	if actor := entries[1]["actor"].(map[string]any); uint(actor["id"].(float64)) != owner.userID {
		t.Fatalf("removal actor = %v, want the owner", actor)
	}
	if entries[0]["actor"].(map[string]any)["id"] != entries[0]["target"].(map[string]any)["id"] {
		t.Fatal("leaving is recorded with the same user as actor and target")
	}

	rec := request(t, tc.Router, http.MethodGet, auditPath(owner.workspaceID, ""), owner.token, nil)
	if strings.Contains(rec.Body.String(), "@") {
		t.Fatalf("the audit log leaks an e-mail address: %s", rec.Body.String())
	}

	if got := auditActions(auditEntries(t, tc, owner.token, owner.workspaceID, "?action=member.added")); len(got) != 2 {
		t.Fatalf("filter returned %v", got)
	}
	page := auditEntries(t, tc, owner.token, owner.workspaceID, "?limit=3&page=2")
	if got := auditActions(page); strings.Join(got, ",") != "workspace.avatar_removed,workspace.avatar_changed,workspace.updated" {
		t.Fatalf("page 2 = %v", got)
	}
	if rec := request(t, tc.Router, http.MethodGet, auditPath(owner.workspaceID, "?action=bogus"), owner.token, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: %d", rec.Code)
	}

	for name, token := range map[string]string{"member": memberToken, "outsider": outsiderToken} {
		if rec := request(t, tc.Router, http.MethodGet, auditPath(owner.workspaceID, ""), token, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s read the audit log: %d", name, rec.Code)
		}
	}
	if rec := request(t, tc.Router, http.MethodGet, auditPath(owner.workspaceID, ""), "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous read: %d", rec.Code)
	}
}
