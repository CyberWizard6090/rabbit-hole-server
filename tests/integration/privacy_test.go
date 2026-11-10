package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"rabbit-hole-server/internal/realtime"
)

const privacyBio = "private-bio-text"

func requireNoEmail(t *testing.T, label, body string) {
	t.Helper()
	for _, leak := range []string{`"email"`, "@example.test", privacyBio, "time_zone", "email_verified_at"} {
		if strings.Contains(body, leak) {
			t.Fatalf("%s leaks %q: %s", label, leak, body)
		}
	}
}

func dataList(t *testing.T, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	list, ok := decodeJSON(t, rec)["data"].([]any)
	if !ok {
		t.Fatalf("response has no data list: %s", rec.Body.String())
	}
	return list
}

type privacyFixture struct {
	env      *rtEnv
	owner    board
	memberID uint
	member   string
	guestID  uint
	guest    string
	taskID   uint
}

func newPrivacyFixture(t *testing.T) privacyFixture {
	t.Helper()
	e := newRealtimeEnv(t, realtime.Options{})
	owner := e.newBoard(t, "privacy-owner@example.test")
	memberID, member := newUser(t, e.tc, "privacy-member@example.test")
	guestID, guest := newUser(t, e.tc, "privacy-guest@example.test")
	if err := e.tc.DB.Exec("UPDATE users SET bio = ?, time_zone = 'Europe/Berlin', first_name = 'Pat', last_name = 'Private' WHERE id IN (?, ?)",
		privacyBio, memberID, guestID).Error; err != nil {
		t.Fatalf("prepare users: %v", err)
	}
	for _, u := range []struct {
		id   uint
		role string
	}{{memberID, "Member"}, {guestID, "Guest"}} {
		addContact(t, e.tc, owner.token, u.id)
		rec := e.req(t, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token, map[string]any{"user_id": u.id, "role": u.role})
		if rec.Code != http.StatusCreated {
			t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
		}
	}
	rec := e.req(t, http.MethodPost, "/api/v1/lists/"+itoa(owner.listID)+"/tasks", owner.token, map[string]any{
		"title": "assigned", "status_id": owner.statusID, "assignee_ids": []uint{memberID, guestID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	requireNoEmail(t, "task create response", rec.Body.String())
	return privacyFixture{env: e, owner: owner, memberID: memberID, member: member, guestID: guestID, guest: guest, taskID: responseID(t, rec)}
}

func TestPrivacy_TaskAssigneesExposeOnlyThePublicProjection(t *testing.T) {
	f := newPrivacyFixture(t)
	taskPath := "/api/v1/tasks/" + itoa(f.taskID)
	listPath := "/api/v1/lists/" + itoa(f.owner.listID) + "/tasks"

	for name, token := range map[string]string{"owner": f.owner.token, "member": f.member, "guest": f.guest} {
		rec := f.env.req(t, http.MethodGet, taskPath, token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s get task: %d: %s", name, rec.Code, rec.Body.String())
		}
		requireNoEmail(t, name+" GET task", rec.Body.String())
		assignees, _ := dataObject(t, rec)["assignees"].([]any)
		if len(assignees) != 2 {
			t.Fatalf("%s sees %d assignees, want 2", name, len(assignees))
		}
		for _, a := range assignees {
			fields := a.(map[string]any)
			if len(fields) != 5 || fields["username"] == nil || fields["first_name"] != "Pat" {
				t.Fatalf("%s: assignee is not the public projection: %v", name, fields)
			}
		}

		rec = f.env.req(t, http.MethodGet, listPath, token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s list tasks: %d: %s", name, rec.Code, rec.Body.String())
		}
		requireNoEmail(t, name+" GET list tasks", rec.Body.String())
	}

	rec := f.env.req(t, http.MethodPatch, taskPath, f.owner.token, map[string]any{"title": "renamed", "assignee_ids": []uint{f.memberID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("update task: %d: %s", rec.Code, rec.Body.String())
	}
	requireNoEmail(t, "PATCH task", rec.Body.String())

	for _, path := range []string{
		"/api/v1/spaces/" + itoa(f.owner.spaceID) + "/dashboard",
		"/api/v1/workspaces/" + itoa(f.owner.workspaceID) + "/search?q=renamed",
		membersPath(f.owner.workspaceID, ""),
	} {
		rec := f.env.req(t, http.MethodGet, path, f.guest, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d: %s", path, rec.Code, rec.Body.String())
		}
		requireNoEmail(t, "guest GET "+path, rec.Body.String())
	}
}

func TestPrivacy_RealtimeTaskEventsCarryNoEmailAddresses(t *testing.T) {
	f := newPrivacyFixture(t)
	listConn := f.env.connect(t, f.guest, "lists", f.owner.listID)
	spaceConn := f.env.connect(t, f.guest, "spaces", f.owner.spaceID)

	rec := f.env.req(t, http.MethodPost, "/api/v1/lists/"+itoa(f.owner.listID)+"/tasks", f.owner.token, map[string]any{
		"title": "second", "status_id": f.owner.statusID, "assignee_ids": []uint{f.memberID, f.guestID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}

	for _, target := range []struct {
		name string
		conn *websocket.Conn
	}{{"list", listConn}, {"space", spaceConn}} {
		name, c := target.name, target.conn
		ev := readEvent(t, c)
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		if ev["type"] != "task.created" {
			t.Fatalf("%s event = %s", name, raw)
		}
		requireNoEmail(t, name+" realtime event", string(raw))
		task := ev["task"].(map[string]any)
		if assignees, _ := task["assignees"].([]any); len(assignees) != 2 {
			t.Fatalf("%s event has %d assignees, want 2", name, len(assignees))
		}
	}
}

func TestPrivacy_CommentsActivityWatchersChecklistsAndNotificationsHideEmails(t *testing.T) {
	f := newPrivacyFixture(t)
	taskPath := "/api/v1/tasks/" + itoa(f.taskID)

	rec := f.env.req(t, http.MethodPost, taskPath+"/comments", f.owner.token, map[string]any{
		"body": "hello @member", "mention_user_ids": []uint{f.memberID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment: %d: %s", rec.Code, rec.Body.String())
	}
	requireNoEmail(t, "comment create", rec.Body.String())

	rec = f.env.req(t, http.MethodPost, taskPath+"/checklists", f.owner.token, map[string]any{"name": "todo"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("checklist: %d: %s", rec.Code, rec.Body.String())
	}
	checklistID := responseID(t, rec)
	rec = f.env.req(t, http.MethodPost, taskPath+"/checklists/"+itoa(checklistID)+"/items", f.owner.token,
		map[string]any{"text": "step", "assignee_id": f.memberID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("checklist item: %d: %s", rec.Code, rec.Body.String())
	}
	requireNoEmail(t, "checklist item create", rec.Body.String())

	if rec := f.env.req(t, http.MethodPut, taskPath+"/watch", f.member, nil); rec.Code >= 300 {
		t.Fatalf("watch: %d: %s", rec.Code, rec.Body.String())
	}

	for name, token := range map[string]string{"owner": f.owner.token, "member": f.member, "guest": f.guest} {
		for _, suffix := range []string{"/comments", "/activity", "/watchers", "/checklists", "/attachments"} {
			rec := f.env.req(t, http.MethodGet, taskPath+suffix, token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s GET %s: %d: %s", name, suffix, rec.Code, rec.Body.String())
			}
			requireNoEmail(t, name+" GET "+suffix, rec.Body.String())
		}
	}

	rec = f.env.req(t, http.MethodGet, "/api/v1/notifications", f.member, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("notifications: %d: %s", rec.Code, rec.Body.String())
	}
	if len(dataList(t, rec)) == 0 {
		t.Fatalf("the member has no notification, the check proves nothing: %s", rec.Body.String())
	}
	requireNoEmail(t, "notifications", rec.Body.String())
}

func TestPrivacy_ContactsAndSearchExposeOnlyThePublicProjection(t *testing.T) {
	tc := newTestContext(t)
	aliceID, alice := newUser(t, tc, "privacy-alice@example.test")
	bobID, bob := newUser(t, tc, "privacy-bob@example.test")
	if err := tc.DB.Exec("UPDATE users SET bio = ?, time_zone = 'Asia/Tokyo', username = 'bobby-private' WHERE id = ?", privacyBio, bobID).Error; err != nil {
		t.Fatal(err)
	}
	addContact(t, tc, alice, bobID)

	for name, path := range map[string]string{
		"contacts":          "/api/v1/contacts",
		"incoming requests": "/api/v1/contacts/requests/incoming",
		"outgoing requests": "/api/v1/contacts/requests/outgoing",
		"search":            "/api/v1/users/search?q=bobby",
	} {
		for who, token := range map[string]string{"alice": alice, "bob": bob} {
			rec := request(t, tc.Router, http.MethodGet, path, token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s as %s: %d: %s", name, who, rec.Code, rec.Body.String())
			}
			requireNoEmail(t, name+" as "+who, rec.Body.String())
		}
	}

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/contacts", alice, nil)
	contacts := dataList(t, rec)
	if len(contacts) != 1 || len(contacts[0].(map[string]any)) != 5 {
		t.Fatalf("contacts = %v", contacts)
	}
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/contacts", bob, nil)
	if got := dataList(t, rec); len(got) != 1 || got[0].(map[string]any)["id"] != float64(aliceID) {
		t.Fatalf("the contact is not mutual: %v", got)
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/profile", bob, nil)
	own := dataObject(t, rec)
	if own["email"] != "privacy-bob@example.test" || own["bio"] != privacyBio {
		t.Fatalf("the own profile must keep the full data: %v", own)
	}
}

func TestPrivacy_AnonymousSubmissionNeverCarriesInternalErrorText(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "s06")
	answers := map[string]any{itoa(f.questionID): "Task title"}

	rec := fcSubmit(t, tc, f.slug, "s06-key", answers)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	if err := tc.DB.Table("form_submissions").Where("id = ?", submissionID).Updates(map[string]any{
		"task_creation_status": "failed", "task_creation_error": `ERROR: insert into "secret_table" violates fk_internal (SQLSTATE 23503)`,
	}).Error; err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	rec = fcSubmit(t, tc, f.slug, "s06-key", answers)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay: %d: %s", rec.Code, rec.Body.String())
	}
	for _, leak := range []string{"task_creation_error", "secret_table", "SQLSTATE"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("anonymous response leaks %q: %s", leak, rec.Body.String())
		}
	}

	rec = request(t, tc.Router, http.MethodGet, f.formURL+"/"+itoa(f.formID)+"/submissions", f.owner.token, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "secret_table") {
		t.Fatalf("members read the stored value: %d: %s", rec.Code, rec.Body.String())
	}
}
