package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// tenant is a user together with a complete resource graph the user owns.
type tenant struct {
	token       string
	userID      uint
	workspaceID uint
	spaceID     uint
	folderID    uint
	listID      uint
	statusID    uint
	tagID       uint
	taskID      uint
}

func mustCreate(t *testing.T, tc *TestContext, token, path string, body map[string]any) uint {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, path, token, body)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("POST %s: %d: %s", path, rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func newTenant(t *testing.T, tc *TestContext, label string) tenant {
	t.Helper()

	userID, token := newUser(t, tc, fmt.Sprintf("%s-%d@example.test", label, time.Now().UnixNano()))
	tn := tenant{token: token, userID: userID}

	tn.workspaceID = mustCreate(t, tc, token, "/api/v1/workspaces", map[string]any{"name": label + " workspace"})
	tn.spaceID = mustCreate(t, tc, token, "/api/v1/workspaces/"+itoa(tn.workspaceID)+"/spaces", map[string]any{"name": label + " space"})
	tn.folderID = mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(tn.spaceID)+"/folders", map[string]any{"name": label + " folder"})
	tn.listID = mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(tn.spaceID)+"/lists", map[string]any{"name": label + " list"})
	tn.statusID = mustCreate(t, tc, token, "/api/v1/lists/"+itoa(tn.listID)+"/statuses", map[string]any{
		"name": "Todo", "color": "#123456", "type": 1, "list_id": tn.listID,
	})
	tn.tagID = mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(tn.spaceID)+"/tags", map[string]any{
		"name": label + "-tag", "color": "#654321",
	})
	tn.taskID = mustCreate(t, tc, token, "/api/v1/lists/"+itoa(tn.listID)+"/tasks", map[string]any{
		"title": label + " task", "status_id": tn.statusID,
	})
	return tn
}

// TestAuthz_ForeignResourcesAreInaccessible walks every resource-scoped route
// with another tenant's IDs and requires a 403/404, never a success.
func TestAuthz_ForeignResourcesAreInaccessible(t *testing.T) {
	tc := newTestContext(t)
	victim := newTenant(t, tc, "victim")
	attacker := newTenant(t, tc, "attacker")

	ws, sp, fo := itoa(victim.workspaceID), itoa(victim.spaceID), itoa(victim.folderID)
	li, st, tg, tk := itoa(victim.listID), itoa(victim.statusID), itoa(victim.tagID), itoa(victim.taskID)

	cases := []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodPost, "/api/v1/workspaces/" + ws + "/spaces", map[string]any{"name": "x"}},
		{http.MethodGet, "/api/v1/workspaces/" + ws + "/spaces", nil},

		{http.MethodGet, "/api/v1/spaces/" + sp + "/dashboard", nil},
		{http.MethodGet, "/api/v1/spaces/" + sp + "/tags", nil},
		{http.MethodPost, "/api/v1/spaces/" + sp + "/tags", map[string]any{"name": "x", "color": "#111111"}},
		{http.MethodPut, "/api/v1/spaces/" + sp + "/tags/" + tg, map[string]any{"name": "x"}},
		{http.MethodDelete, "/api/v1/spaces/" + sp + "/tags/" + tg, nil},
		{http.MethodPost, "/api/v1/spaces/" + sp + "/tags/merge", map[string]any{"source_tag_id": victim.tagID, "target_tag_id": victim.tagID}},
		{http.MethodGet, "/api/v1/spaces/" + sp + "/folders", nil},
		{http.MethodPost, "/api/v1/spaces/" + sp + "/folders", map[string]any{"name": "x"}},
		{http.MethodGet, "/api/v1/spaces/" + sp + "/lists", nil},
		{http.MethodPost, "/api/v1/spaces/" + sp + "/lists", map[string]any{"name": "x"}},

		{http.MethodGet, "/api/v1/folders/" + fo + "/lists", nil},
		{http.MethodPost, "/api/v1/folders/" + fo + "/lists", map[string]any{"name": "x"}},
		{http.MethodPatch, "/api/v1/folders/" + fo, map[string]any{"name": "x"}},
		{http.MethodDelete, "/api/v1/folders/" + fo, nil},

		{http.MethodPatch, "/api/v1/lists/" + li, map[string]any{"name": "x"}},
		{http.MethodDelete, "/api/v1/lists/" + li, nil},
		{http.MethodGet, "/api/v1/lists/" + li + "/statuses", nil},
		{http.MethodPost, "/api/v1/lists/" + li + "/statuses", map[string]any{"name": "x", "color": "#111111", "type": 1, "list_id": victim.listID}},
		{http.MethodPut, "/api/v1/lists/" + li + "/statuses/" + st, map[string]any{"name": "x"}},
		{http.MethodDelete, "/api/v1/lists/" + li + "/statuses/" + st, nil},
		{http.MethodGet, "/api/v1/lists/" + li + "/tasks", nil},
		{http.MethodPost, "/api/v1/lists/" + li + "/tasks", map[string]any{"title": "x", "status_id": victim.statusID}},

		{http.MethodGet, "/api/v1/tasks/" + tk, nil},
		{http.MethodPatch, "/api/v1/tasks/" + tk, map[string]any{"title": "pwned"}},
		{http.MethodDelete, "/api/v1/tasks/" + tk, nil},
	}

	for _, tcase := range cases {
		t.Run(tcase.method+" "+tcase.path, func(t *testing.T) {
			rec := request(t, tc.Router, tcase.method, tcase.path, attacker.token, tcase.body)
			if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
				t.Fatalf("expected 403/404, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// None of the attempts above may have changed the victim's data.
	assertCount := func(table, where string, want int64) {
		t.Helper()
		var got int64
		if err := tc.DB.Table(table).Where(where).Count(&got).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s where %s: got %d, want %d", table, where, got, want)
		}
	}
	assertCount("tasks", fmt.Sprintf("id = %d AND title = 'victim task' AND deleted_at IS NULL", victim.taskID), 1)
	assertCount("tags", fmt.Sprintf("id = %d AND name = 'victim-tag'", victim.tagID), 1)
	assertCount("folders", fmt.Sprintf("id = %d AND name = 'victim folder' AND deleted_at IS NULL", victim.folderID), 1)
	assertCount("lists", fmt.Sprintf("id = %d AND deleted_at IS NULL", victim.listID), 1)
	assertCount("task_statuses", fmt.Sprintf("id = %d AND deleted_at IS NULL", victim.statusID), 1)
	assertCount("spaces", fmt.Sprintf("workspace_id = %d AND deleted_at IS NULL", victim.workspaceID), 1)
}

// TestAuthz_CrossTenantReferencesAreRejected covers requests where the caller
// is allowed to touch the route's resource, but smuggles in IDs owned by
// someone else through the path or the body.
func TestAuthz_CrossTenantReferencesAreRejected(t *testing.T) {
	tc := newTestContext(t)
	victim := newTenant(t, tc, "victim")
	attacker := newTenant(t, tc, "attacker")

	ownSpace, ownList := itoa(attacker.spaceID), itoa(attacker.listID)

	t.Run("task create with foreign status", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+ownList+"/tasks", attacker.token,
			map[string]any{"title": "x", "status_id": victim.statusID})
		requireClientError(t, rec)
	})
	t.Run("task create with foreign tag", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+ownList+"/tasks", attacker.token,
			map[string]any{"title": "x", "status_id": attacker.statusID, "tag_ids": []uint{victim.tagID}})
		requireClientError(t, rec)
	})
	t.Run("task create with foreign assignee", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+ownList+"/tasks", attacker.token,
			map[string]any{"title": "x", "status_id": attacker.statusID, "assignee_ids": []uint{victim.userID}})
		requireClientError(t, rec)
	})
	t.Run("task create with foreign parent", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+ownList+"/tasks", attacker.token,
			map[string]any{"title": "x", "status_id": attacker.statusID, "parent_id": victim.taskID})
		requireClientError(t, rec)
	})
	t.Run("task update with foreign status", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(attacker.taskID), attacker.token,
			map[string]any{"status_id": victim.statusID})
		requireClientError(t, rec)
	})
	t.Run("task update with foreign tag", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(attacker.taskID), attacker.token,
			map[string]any{"add_tag_ids": []uint{victim.tagID}})
		requireClientError(t, rec)
	})
	t.Run("foreign tag through own space path", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPut, "/api/v1/spaces/"+ownSpace+"/tags/"+itoa(victim.tagID), attacker.token,
			map[string]any{"name": "pwned"})
		requireClientError(t, rec)
		rec = request(t, tc.Router, http.MethodDelete, "/api/v1/spaces/"+ownSpace+"/tags/"+itoa(victim.tagID), attacker.token, nil)
		requireClientError(t, rec)
	})
	t.Run("merge foreign tag through own space path", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+ownSpace+"/tags/merge", attacker.token,
			map[string]any{"source_tag_id": victim.tagID, "target_tag_id": attacker.tagID})
		requireClientError(t, rec)
	})
	t.Run("foreign status through own list path", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPut, "/api/v1/lists/"+ownList+"/statuses/"+itoa(victim.statusID), attacker.token,
			map[string]any{"name": "pwned"})
		requireClientError(t, rec)
		rec = request(t, tc.Router, http.MethodDelete, "/api/v1/lists/"+ownList+"/statuses/"+itoa(victim.statusID), attacker.token, nil)
		requireClientError(t, rec)
	})
	t.Run("status create with foreign list_id in body", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+ownList+"/statuses", attacker.token,
			map[string]any{"name": "x", "color": "#111111", "type": 1, "list_id": victim.listID})
		if rec.Code == http.StatusCreated {
			var n int64
			tc.DB.Table("task_statuses").Where("list_id = ? AND name = 'x'", victim.listID).Count(&n)
			if n != 0 {
				t.Fatalf("status was created in the victim's list")
			}
		}
	})

	// Victim data must be intact.
	var tagName string
	if err := tc.DB.Table("tags").Select("name").Where("id = ?", victim.tagID).Scan(&tagName).Error; err != nil || tagName != "victim-tag" {
		t.Fatalf("victim tag changed: name=%q err=%v", tagName, err)
	}
	var statusCount int64
	tc.DB.Table("task_statuses").Where("id = ? AND deleted_at IS NULL", victim.statusID).Count(&statusCount)
	if statusCount != 1 {
		t.Fatalf("victim status was deleted")
	}
}

func TestAuthz_ContactsAreScopedToCaller(t *testing.T) {
	tc := newTestContext(t)
	a := newTenant(t, tc, "alice")
	b := newTenant(t, tc, "bob")
	c := newTenant(t, tc, "carol")

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/contacts", a.token, map[string]any{"contact_id": c.userID})
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("add contact: %d: %s", rec.Code, rec.Body.String())
	}

	// Bob cannot remove Alice's contact, and does not see it.
	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/contacts/"+itoa(c.userID), b.token, nil)
	requireClientError(t, rec)

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/contacts", b.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list contacts: %d", rec.Code)
	}
	if data, _ := decodeJSON(t, rec)["data"].([]any); len(data) != 0 {
		t.Fatalf("bob sees alice's contacts: %v", data)
	}
}

func TestAuthz_UnauthenticatedRequestsAreRejected(t *testing.T) {
	tc := newTestContext(t)
	victim := newTenant(t, tc, "victim")

	for _, path := range []string{
		"/api/v1/profile",
		"/api/v1/workspaces",
		"/api/v1/spaces/" + itoa(victim.spaceID) + "/dashboard",
		"/api/v1/lists/" + itoa(victim.listID) + "/tasks",
		"/api/v1/tasks/" + itoa(victim.taskID),
	} {
		rec := request(t, tc.Router, http.MethodGet, path, "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without token: expected 401, got %d", path, rec.Code)
		}
	}
}

func requireClientError(t *testing.T, rec interface {
	Result() *http.Response
}) {
	t.Helper()
	code := rec.Result().StatusCode
	if code < 400 || code >= 500 {
		t.Fatalf("expected 4xx, got %d", code)
	}
}
