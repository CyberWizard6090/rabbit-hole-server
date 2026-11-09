package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type viewEnv struct {
	tc       *TestContext
	owner    workspaceOwner
	admin    string
	member   string
	guest    string
	outsider string
	listID   uint
}

func viewsPath(workspaceID uint, suffix string) string {
	return "/api/v1/workspaces/" + itoa(workspaceID) + "/views" + suffix
}

func newViewEnv(t *testing.T) *viewEnv {
	t.Helper()
	tc := newTestContext(t)
	e := &viewEnv{tc: tc, owner: newWorkspaceOwner(t, tc, "views")}
	for _, u := range []struct {
		name, role string
		token      *string
	}{{"admin", "Admin", &e.admin}, {"member", "Member", &e.member}, {"guest", "Guest", &e.guest}} {
		id, token := newUser(t, tc, "views-"+u.name+"@example.test")
		addContact(t, tc, e.owner.token, id)
		if rec := request(t, tc.Router, http.MethodPost, membersPath(e.owner.workspaceID, ""), e.owner.token,
			map[string]any{"user_id": id, "role": u.role}); rec.Code != http.StatusCreated {
			t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
		}
		*u.token = token
	}
	_, e.outsider = newUser(t, tc, "views-outsider@example.test")
	e.listID = mustCreate(t, tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/lists", map[string]any{"name": "L"})
	return e
}

func (e *viewEnv) create(t *testing.T, token string, body map[string]any, want int) map[string]any {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, viewsPath(e.owner.workspaceID, ""), token, body)
	if rec.Code != want {
		t.Fatalf("create view: %d (want %d): %s", rec.Code, want, rec.Body.String())
	}
	if want != http.StatusCreated {
		return nil
	}
	return dataObject(t, rec)
}

func (e *viewEnv) list(t *testing.T, token, query string) []map[string]any {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, viewsPath(e.owner.workspaceID, query), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list views: %d: %s", rec.Code, rec.Body.String())
	}
	raw, _ := decodeJSON(t, rec)["data"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(map[string]any))
	}
	return out
}

func viewNames(views []map[string]any) string {
	names := make([]string, len(views))
	for i, v := range views {
		names[i], _ = v["name"].(string)
	}
	return strings.Join(names, ",")
}

func TestSavedViews_PersonalViewsArePrivateAndSharedOnesAreVisibleToTheWorkspace(t *testing.T) {
	e := newViewEnv(t)
	mine := e.create(t, e.member, map[string]any{
		"name": "My open work", "list_id": e.listID,
		"filters": map[string]any{"priority": "4,5", "has_due": "true"}, "sort": "-priority,due_date",
	}, http.StatusCreated)
	if mine["shared"] != false || mine["is_owner"] != true || mine["sort"] != "-priority,due_date" ||
		mine["filters"].(map[string]any)["priority"] != "4,5" {
		t.Fatalf("view = %v", mine)
	}
	if strings.Contains(mustJSON(t, mine), "@") {
		t.Fatalf("a view leaks an e-mail address: %v", mine)
	}
	e.create(t, e.admin, map[string]any{"name": "Team board", "shared": true, "filters": map[string]any{"unassigned": "true"}}, http.StatusCreated)
	e.create(t, e.admin, map[string]any{"name": "Admin only"}, http.StatusCreated)

	if got := viewNames(e.list(t, e.member, "")); got != "Team board,My open work" {
		t.Fatalf("member sees %q", got)
	}
	if got := viewNames(e.list(t, e.admin, "")); got != "Team board,Admin only" {
		t.Fatalf("admin sees %q", got)
	}
	if got := viewNames(e.list(t, e.guest, "")); got != "Team board" {
		t.Fatalf("guest sees %q", got)
	}
	if got := viewNames(e.list(t, e.member, "?list_id="+itoa(e.listID))); got != "Team board,My open work" {
		t.Fatalf("a list filter shows %q", got)
	}
	if rec := request(t, e.tc.Router, http.MethodGet, viewsPath(e.owner.workspaceID, ""), e.outsider, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list: %d", rec.Code)
	}
}

func TestSavedViews_OnlyWhoManagesTheStructureMaySharePermissionsAndEditing(t *testing.T) {
	e := newViewEnv(t)
	e.create(t, e.member, map[string]any{"name": "shared by a member", "shared": true}, http.StatusForbidden)
	personal := e.create(t, e.member, map[string]any{"name": "mine"}, http.StatusCreated)
	id := itoa(uint(personal["id"].(float64)))

	if rec := request(t, e.tc.Router, http.MethodPatch, viewsPath(e.owner.workspaceID, "/"+id), e.member, map[string]any{"shared": true}); rec.Code != http.StatusForbidden {
		t.Fatalf("a member shared a view: %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodPatch, viewsPath(e.owner.workspaceID, "/"+id), e.admin, map[string]any{"name": "taken"}); rec.Code != http.StatusNotFound {
		t.Fatalf("an admin edited someone's personal view: %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, viewsPath(e.owner.workspaceID, "/"+id), e.admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("an admin deleted someone's personal view: %d", rec.Code)
	}

	shared := e.create(t, e.admin, map[string]any{"name": "team", "shared": true}, http.StatusCreated)
	sid := itoa(uint(shared["id"].(float64)))
	if rec := request(t, e.tc.Router, http.MethodPatch, viewsPath(e.owner.workspaceID, "/"+sid), e.member, map[string]any{"name": "renamed"}); rec.Code != http.StatusForbidden {
		t.Fatalf("a member edited a shared view: %d", rec.Code)
	}
	rec := request(t, e.tc.Router, http.MethodPatch, viewsPath(e.owner.workspaceID, "/"+sid), e.owner.token, map[string]any{"name": " team (owner) ", "sort": "-created_at"})
	if rec.Code != http.StatusOK || dataObject(t, rec)["name"] != "team (owner)" || dataObject(t, rec)["is_owner"] != false {
		t.Fatalf("the owner edits a shared view: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, viewsPath(e.owner.workspaceID, "/"+sid), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete shared: %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodPatch, viewsPath(e.owner.workspaceID, "/"+id), e.member, map[string]any{"name": "mine v2", "filters": map[string]any{"q": "deploy"}}); rec.Code != http.StatusOK {
		t.Fatalf("a member edits their own view: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, viewsPath(e.owner.workspaceID, "/"+id), e.member, nil); rec.Code != http.StatusOK {
		t.Fatalf("a member deletes their own view: %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, viewsPath(e.owner.workspaceID, "/"+id), e.member, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("deleting twice: %d", rec.Code)
	}
}

func TestSavedViews_ConfigurationIsValidatedLikeTheListQuery(t *testing.T) {
	e := newViewEnv(t)
	bad := []map[string]any{
		{"name": "x", "filters": map[string]any{"limit": "5"}},
		{"name": "x", "filters": map[string]any{"priority": "9"}},
		{"name": "x", "filters": map[string]any{"due_from": "yesterday"}},
		{"name": "x", "sort": "nonsense"},
		{"name": "   "},
		{"name": strings.Repeat("a", 101)},
		{"name": "x", "list_id": 99999999},
	}
	for i, body := range bad {
		if rec := request(t, e.tc.Router, http.MethodPost, viewsPath(e.owner.workspaceID, ""), e.member, body); rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	e.create(t, e.member, map[string]any{"name": "ok", "filters": map[string]any{"due_from": "2026-01-01", "tag_id": "1,2"}, "sort": "due_date"}, http.StatusCreated)
}

func TestSavedViews_AViewOfADeletedListDisappears(t *testing.T) {
	e := newViewEnv(t)
	e.create(t, e.member, map[string]any{"name": "list view", "list_id": e.listID}, http.StatusCreated)
	e.create(t, e.member, map[string]any{"name": "everywhere"}, http.StatusCreated)
	if rec := request(t, e.tc.Router, http.MethodDelete, "/api/v1/lists/"+itoa(e.listID), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete list: %d", rec.Code)
	}
	if got := viewNames(e.list(t, e.member, "")); got != "everywhere" {
		t.Fatalf("views after the list was deleted: %q", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
