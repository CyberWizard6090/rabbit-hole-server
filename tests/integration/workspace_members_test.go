package integration

import (
	"net/http"
	"testing"
)

type workspaceOwner struct {
	userID      uint
	token       string
	workspaceID uint
	spaceID     uint
}

func newWorkspaceOwner(t *testing.T, tc *TestContext, label string) workspaceOwner {
	t.Helper()
	userID, token := newUser(t, tc, label+"-owner@example.test")

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{"name": label + " workspace"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d: %s", rec.Code, rec.Body.String())
	}
	workspaceID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", token, map[string]any{"name": label + " space"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: %d: %s", rec.Code, rec.Body.String())
	}
	return workspaceOwner{userID: userID, token: token, workspaceID: workspaceID, spaceID: responseID(t, rec)}
}

func addContact(t *testing.T, tc *TestContext, token string, contactID uint) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/contacts", token, map[string]any{"contact_id": contactID})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request contact: %d: %s", rec.Code, rec.Body.String())
	}
	res := tc.DB.Exec("UPDATE user_contacts SET status = 'accepted' WHERE contact_id = ? AND status = 'pending'", contactID)
	if res.Error != nil || res.RowsAffected == 0 {
		t.Fatalf("accept contact %d: rows=%d err=%v", contactID, res.RowsAffected, res.Error)
	}
}

func membersPath(workspaceID uint, suffix string) string {
	return "/api/v1/workspaces/" + itoa(workspaceID) + "/members" + suffix
}

func TestMembers_OwnerInvitesContactWhoThenGainsAccess(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "owner")
	guestID, guestToken := newUser(t, tc, "guest-user@example.test")

	rec := request(t, tc.Router, http.MethodGet, membersPath(owner.workspaceID, ""), guestToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list members: expected 403, got %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": guestID, "role": "Guest"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("add non-contact: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}

	addContact(t, tc, owner.token, guestID)
	rec = request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": guestID, "role": "Guest"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": guestID, "role": "Guest"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("add twice: expected 409, got %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodGet, membersPath(owner.workspaceID, ""), guestToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("guest list members: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data, _ := decodeJSON(t, rec)["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("members = %v, want owner and guest", data)
	}
	for _, m := range data {
		if _, leaked := m.(map[string]any)["email"]; leaked {
			t.Fatalf("member list exposes email: %v", m)
		}
	}
	rec = request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), guestToken,
		map[string]any{"user_id": owner.userID, "role": "Admin"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("guest add member: expected 403, got %d", rec.Code)
	}
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(owner.spaceID)+"/dashboard", guestToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("guest reads space: expected 200, got %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodPatch, membersPath(owner.workspaceID, "/"+itoa(guestID)), owner.token,
		map[string]any{"role": "Admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("promote: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPatch, membersPath(owner.workspaceID, "/"+itoa(guestID)), guestToken,
		map[string]any{"role": "Member"})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin changes role: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMembers_OwnerIsProtectedAndRolesAreValidated(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "owner")
	adminID, adminToken := newUser(t, tc, "admin-user@example.test")
	addContact(t, tc, owner.token, adminID)
	if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": adminID, "role": "Admin"}); rec.Code != http.StatusCreated {
		t.Fatalf("add admin: %d: %s", rec.Code, rec.Body.String())
	}

	t.Run("owner role cannot be assigned", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPatch, membersPath(owner.workspaceID, "/"+itoa(adminID)), owner.token,
			map[string]any{"role": "Owner"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("admin cannot demote or remove the owner", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPatch, membersPath(owner.workspaceID, "/"+itoa(owner.userID)), adminToken,
			map[string]any{"role": "Guest"})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("demote owner: expected 403, got %d", rec.Code)
		}
		rec = request(t, tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/"+itoa(owner.userID)), adminToken, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("remove owner: expected 403, got %d", rec.Code)
		}
	})
	t.Run("owner cannot leave", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/me"), owner.token, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})
	t.Run("unknown member is 404", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/99999"), owner.token, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("another workspace's admin cannot manage this one", func(t *testing.T) {
		other := newWorkspaceOwner(t, tc, "other")
		rec := request(t, tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/"+itoa(adminID)), other.token, nil)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Fatalf("expected 403/404, got %d", rec.Code)
		}
	})
}

func TestMembers_MemberCanLeaveAndLosesAccess(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "owner")
	memberID, memberToken := newUser(t, tc, "leaver@example.test")
	addContact(t, tc, owner.token, memberID)
	if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": memberID, "role": "Member"}); rec.Code != http.StatusCreated {
		t.Fatalf("add: %d: %s", rec.Code, rec.Body.String())
	}

	rec := request(t, tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/me"), memberToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("leave: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(owner.spaceID)+"/dashboard", memberToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("after leaving: expected 403, got %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": memberID, "role": "Guest"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("re-add: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}
