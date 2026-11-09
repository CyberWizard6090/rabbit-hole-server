package integration

import (
	"context"
	"net/http"
	"testing"
)

func TestRoleSync_RestoresDefaultRolesInExistingWorkspaces(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "rolesync")
	adminID, adminToken := newUser(t, tc, "rolesync-admin@example.test")
	addContact(t, tc, owner.token, adminID)
	if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": adminID, "role": "Admin"}); rec.Code != http.StatusCreated {
		t.Fatalf("add admin: %d: %s", rec.Code, rec.Body.String())
	}
	_, container := newReplica(t, tc)

	exec := func(query string, args ...any) {
		t.Helper()
		if err := tc.DB.Exec(query, args...).Error; err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	exec(`DELETE FROM role_permissions WHERE role_id = (SELECT id FROM roles WHERE workspace_id = ? AND name = 'Admin')
		AND permission_id = (SELECT id FROM permissions WHERE code = 'workspace.update')`, owner.workspaceID)
	exec(`INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r, permissions p WHERE r.workspace_id = ? AND r.name = 'Guest' AND p.code = 'task.delete'`, owner.workspaceID)
	exec(`DELETE FROM role_permissions WHERE role_id = (SELECT id FROM roles WHERE workspace_id = ? AND name = 'Member')`, owner.workspaceID)
	exec(`DELETE FROM role_permissions WHERE permission_id = (SELECT id FROM permissions WHERE code = 'form.manage')`)
	exec(`DELETE FROM permissions WHERE code = 'form.manage'`)

	rename := map[string]any{"name": "renamed by the admin"}
	if rec := request(t, tc.Router, http.MethodPatch, "/api/v1/workspaces/"+itoa(owner.workspaceID), adminToken, rename); rec.Code != http.StatusForbidden {
		t.Fatalf("before the sync the admin renamed the workspace: %d", rec.Code)
	}

	result, err := container.SyncAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed() || result.Permissions != 1 || result.Revoked != 1 {
		t.Fatalf("result = %+v", result)
	}
	if rec := request(t, tc.Router, http.MethodPatch, "/api/v1/workspaces/"+itoa(owner.workspaceID), adminToken, rename); rec.Code != http.StatusOK {
		t.Fatalf("after the sync the admin must be able to rename: %d: %s", rec.Code, rec.Body.String())
	}
	if n := countRows(t, tc, `SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id = rp.role_id
		JOIN permissions p ON p.id = rp.permission_id WHERE r.workspace_id = ? AND r.name = 'Guest' AND p.code = 'task.delete'`, owner.workspaceID); n != 0 {
		t.Fatal("a permission that is not part of the Guest role was left in place")
	}
	if n := countRows(t, tc, `SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id = rp.role_id
		WHERE r.workspace_id = ? AND r.name = 'Member' AND rp.deleted_at IS NULL`, owner.workspaceID); n != 8 {
		t.Fatalf("Member permissions = %d, want the 8 defaults", n)
	}

	again, err := container.SyncAccess(context.Background())
	if err != nil || again.Changed() {
		t.Fatalf("a second sync must change nothing: %+v, %v", again, err)
	}
}
