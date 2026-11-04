package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"rabbit-hole-server/internal/config"
)

type notifyFixture struct {
	tc      *TestContext
	owner   workspaceOwner
	taskID  uint
	listID  uint
	status1 uint
	status2 uint

	memberA, memberB, guest, outsider         uint
	tokenA, tokenB, tokenGuest, tokenOutsider string
}

func notifyEmail(label string) string { return "ntf-" + label + "@example.test" }

func verifyEmail(t *testing.T, tc *TestContext, userID uint) {
	t.Helper()
	if err := tc.DB.Exec("UPDATE users SET email_verified_at = now() WHERE id = ?", userID).Error; err != nil {
		t.Fatalf("verify email: %v", err)
	}
}

func newNotifyFixture(t *testing.T, tc *TestContext) *notifyFixture {
	t.Helper()
	f := &notifyFixture{tc: tc}
	f.owner = newWorkspaceOwner(t, tc, "ntf")
	f.memberA, f.tokenA = newUser(t, tc, notifyEmail("a"))
	f.memberB, f.tokenB = newUser(t, tc, notifyEmail("b"))
	f.guest, f.tokenGuest = newUser(t, tc, notifyEmail("guest"))
	f.outsider, f.tokenOutsider = newUser(t, tc, notifyEmail("outsider"))
	for _, u := range []struct {
		id   uint
		role string
	}{{f.memberA, "Member"}, {f.memberB, "Member"}, {f.guest, "Guest"}} {
		addContact(t, tc, f.owner.token, u.id)
		rec := request(t, tc.Router, http.MethodPost, membersPath(f.owner.workspaceID, ""), f.owner.token,
			map[string]any{"user_id": u.id, "role": u.role})
		if rec.Code != http.StatusCreated {
			t.Fatalf("add member: %d: %s", rec.Code, rec.Body.String())
		}
	}
	for _, id := range []uint{f.owner.userID, f.memberA, f.memberB, f.guest, f.outsider} {
		verifyEmail(t, tc, id)
	}
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(f.owner.spaceID)+"/lists", f.owner.token, map[string]any{"name": "list"})
	f.listID = responseID(t, rec)
	status := func(name string) uint {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(f.listID)+"/statuses", f.owner.token,
			map[string]any{"name": name, "color": "#112233", "type": 1, "list_id": f.listID})
		if rec.Code != http.StatusCreated {
			t.Fatalf("status: %d: %s", rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}
	f.status1, f.status2 = status("Todo"), status("Review")
	f.taskID = f.newTask(t, "shared task", nil)
	tc.Notifications.Wait()
	tc.Mail.Reset()
	return f
}

func (f *notifyFixture) newTask(t *testing.T, title string, assignees []uint) uint {
	t.Helper()
	body := map[string]any{"title": title, "status_id": f.status1}
	if assignees != nil {
		body["assignee_ids"] = assignees
	}
	rec := request(t, f.tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(f.listID)+"/tasks", f.owner.token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func (f *notifyFixture) comment(t *testing.T, token string, taskID uint, body string, mentions ...uint) {
	t.Helper()
	payload := map[string]any{"body": body}
	if mentions != nil {
		payload["mention_user_ids"] = mentions
	}
	rec := request(t, f.tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(taskID)+"/comments", token, payload)
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment: %d: %s", rec.Code, rec.Body.String())
	}
	f.tc.Notifications.Wait()
}

func (f *notifyFixture) patch(t *testing.T, taskID uint, body map[string]any) {
	t.Helper()
	rec := request(t, f.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), f.owner.token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch task: %d: %s", rec.Code, rec.Body.String())
	}
	f.tc.Notifications.Wait()
}

func (f *notifyFixture) watch(t *testing.T, token string, method string) {
	t.Helper()
	rec := request(t, f.tc.Router, method, "/api/v1/tasks/"+itoa(f.taskID)+"/watch", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s watch: %d: %s", method, rec.Code, rec.Body.String())
	}
}

type inboxEntry struct {
	ID   uint
	Type string
	Read bool
}

func inbox(t *testing.T, tc *TestContext, token, query string) []inboxEntry {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("inbox: %d: %s", rec.Code, rec.Body.String())
	}
	var out []inboxEntry
	for _, raw := range decodeJSON(t, rec)["data"].([]any) {
		item := raw.(map[string]any)
		out = append(out, inboxEntry{ID: uint(item["id"].(float64)), Type: item["type"].(string), Read: item["read_at"] != nil})
	}
	return out
}

func types(entries []inboxEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Type
	}
	return out
}

func expectTypes(t *testing.T, label string, entries []inboxEntry, want ...string) {
	t.Helper()
	got := types(entries)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s inbox = %v, want %v", label, got, want)
	}
}

func setPreference(t *testing.T, tc *TestContext, token, typ, channel string, enabled bool) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPut, "/api/v1/notifications/preferences", token,
		map[string]any{"preferences": []map[string]any{{"type": typ, "channel": channel, "enabled": enabled}}})
	if rec.Code != http.StatusOK {
		t.Fatalf("set preference: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNotifications_MentionReachesOnlyTheMentionedUserAndWatchersGetTheComment(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	f.watch(t, f.tokenB, http.MethodPut)

	f.comment(t, f.owner.token, f.taskID, "please look <i>here</i>", f.memberA)

	expectTypes(t, "mentioned", inbox(t, tc, f.tokenA, ""), "mention")
	expectTypes(t, "watcher", inbox(t, tc, f.tokenB, ""), "comment")
	expectTypes(t, "actor", inbox(t, tc, f.owner.token, ""))
	expectTypes(t, "guest", inbox(t, tc, f.tokenGuest, ""))
	expectTypes(t, "outsider", inbox(t, tc, f.tokenOutsider, ""))

	if n := len(tc.Mail.To(notifyEmail("a"))); n != 1 {
		t.Fatalf("mentioned user mails = %d, want 1", n)
	}
	msg := tc.Mail.To(notifyEmail("a"))[0]
	if !strings.Contains(msg.Text, "http://127.0.0.1:3000/tasks/"+itoa(f.taskID)) && !strings.Contains(msg.Text, "/tasks/"+itoa(f.taskID)) {
		t.Fatalf("mail has no task link: %s", msg.Text)
	}
	if strings.Contains(msg.HTML, "<i>here</i>") {
		t.Fatalf("comment text is not escaped in the HTML part: %s", msg.HTML)
	}
	for _, other := range []string{"b", "guest", "outsider"} {
		if len(tc.Mail.To(notifyEmail(other))) != 0 {
			t.Fatalf("%s got a mail", other)
		}
	}
	if len(tc.Mail.To("ntf-owner@example.test")) != 0 {
		t.Fatal("the actor got a mail")
	}

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications", f.tokenA, nil)
	first := decodeJSON(t, rec)["data"].([]any)[0].(map[string]any)
	actor := first["actor"].(map[string]any)
	if uint(actor["id"].(float64)) != f.owner.userID {
		t.Fatalf("actor = %v", actor)
	}
	if _, leaked := actor["email"]; leaked {
		t.Fatalf("actor exposes the e-mail address: %v", actor)
	}
}

func TestNotifications_AutoWatchAndExplicitUnwatchStaysMuted(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(f.taskID)+"/watchers", f.owner.token, nil)
	data := dataObject(t, rec)
	if data["watching"] != true || len(data["watchers"].([]any)) != 1 {
		t.Fatalf("creator should be watching: %v", data)
	}

	f.comment(t, f.tokenA, f.taskID, "first")
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(f.taskID)+"/watchers", f.tokenA, nil)
	if data := dataObject(t, rec); data["watching"] != true || len(data["watchers"].([]any)) != 2 {
		t.Fatalf("commenter should auto-watch: %v", data)
	}
	expectTypes(t, "creator", inbox(t, tc, f.owner.token, ""), "comment")

	f.watch(t, f.tokenA, http.MethodDelete)
	f.comment(t, f.tokenA, f.taskID, "second comment by the muted user")
	f.comment(t, f.owner.token, f.taskID, "owner replies")
	expectTypes(t, "muted user", inbox(t, tc, f.tokenA, ""))
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(f.taskID)+"/watchers", f.tokenA, nil)
	if data := dataObject(t, rec); data["watching"] != false || len(data["watchers"].([]any)) != 1 {
		t.Fatalf("muted state must survive auto-watch: %v", data)
	}

	f.watch(t, f.tokenA, http.MethodPut)
	f.comment(t, f.owner.token, f.taskID, "owner again")
	expectTypes(t, "re-watching user", inbox(t, tc, f.tokenA, ""), "comment")

	if rec := request(t, tc.Router, http.MethodPut, "/api/v1/tasks/"+itoa(f.taskID)+"/watch", f.tokenOutsider, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider watch: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(f.taskID)+"/watchers", f.tokenOutsider, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider watchers: %d", rec.Code)
	}
}

func TestNotifications_AssignmentStatusAndDueDate(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	f.watch(t, f.tokenB, http.MethodPut)

	f.patch(t, f.taskID, map[string]any{"assignee_ids": []uint{f.memberA}})
	expectTypes(t, "assignee", inbox(t, tc, f.tokenA, ""), "assigned")
	if len(tc.Mail.To(notifyEmail("a"))) != 1 {
		t.Fatal("assigned defaults to e-mail")
	}
	f.patch(t, f.taskID, map[string]any{"assignee_ids": []uint{f.memberA}})
	expectTypes(t, "unchanged assignee", inbox(t, tc, f.tokenA, ""), "assigned")

	f.patch(t, f.taskID, map[string]any{"status_id": f.status2})
	f.patch(t, f.taskID, map[string]any{"due_date": "2031-05-04T10:00:00Z"})
	expectTypes(t, "watcher B", inbox(t, tc, f.tokenB, ""), "due_date_changed", "status_changed")
	expectTypes(t, "assignee A (auto-watching)", inbox(t, tc, f.tokenA, ""), "due_date_changed", "status_changed", "assigned")
	expectTypes(t, "actor", inbox(t, tc, f.owner.token, ""))
	if len(tc.Mail.To(notifyEmail("b"))) != 0 {
		t.Fatal("status and due date changes are in-app only by default")
	}

	newTask := f.newTask(t, "with assignee", []uint{f.memberB})
	tc.Notifications.Wait()
	expectTypes(t, "assigned on create", inbox(t, tc, f.tokenB, "?type=assigned"), "assigned")
	if newTask == 0 {
		t.Fatal("task id")
	}

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(f.taskID)+"/checklists", f.owner.token, map[string]any{"name": "todo"})
	checklistID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(f.taskID)+"/checklists/"+itoa(checklistID)+"/items", f.owner.token,
		map[string]any{"text": "write docs", "assignee_id": f.guest})
	if rec.Code != http.StatusCreated {
		t.Fatalf("item: %d: %s", rec.Code, rec.Body.String())
	}
	itemID := responseID(t, rec)
	tc.Notifications.Wait()
	expectTypes(t, "checklist assignee", inbox(t, tc, f.tokenGuest, ""), "assigned")

	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(f.taskID)+"/checklists/"+itoa(checklistID)+"/items/"+itoa(itemID), f.owner.token,
		map[string]any{"assignee_id": f.memberB})
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d: %s", rec.Code, rec.Body.String())
	}
	tc.Notifications.Wait()
	if got := inbox(t, tc, f.tokenB, "?type=assigned"); len(got) != 2 {
		t.Fatalf("reassigned item should notify: %v", types(got))
	}
}

func TestNotifications_PreferenceMatrixChangesBehaviourPerTypeAndChannel(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications/preferences", f.tokenA, nil)
	cells := decodeJSON(t, rec)["data"].([]any)
	if len(cells) != 10 {
		t.Fatalf("matrix has %d cells, want 5 types x 2 channels", len(cells))
	}
	for _, raw := range cells {
		cell := raw.(map[string]any)
		wantDefault := cell["channel"] == "in_app" || cell["type"] == "mention" || cell["type"] == "assigned"
		if cell["default"] != wantDefault || cell["enabled"] != wantDefault || cell["usable"] != true {
			t.Fatalf("cell = %v", cell)
		}
	}

	bad := []map[string]any{{"type": "nope", "channel": "email", "enabled": true}, {"type": "comment", "channel": "fax", "enabled": true}}
	for _, p := range bad {
		if rec := request(t, tc.Router, http.MethodPut, "/api/v1/notifications/preferences", f.tokenA,
			map[string]any{"preferences": []map[string]any{p}}); rec.Code != http.StatusBadRequest {
			t.Fatalf("%v: %d: %s", p, rec.Code, rec.Body.String())
		}
	}
	if rec := request(t, tc.Router, http.MethodPut, "/api/v1/notifications/preferences", f.tokenA,
		map[string]any{"preferences": []map[string]any{{"type": "comment", "channel": "email"}}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled: %d", rec.Code)
	}

	setPreference(t, tc, f.tokenA, "comment", "email", true)
	setPreference(t, tc, f.tokenA, "mention", "in_app", false)
	setPreference(t, tc, f.tokenB, "mention", "email", false)
	f.watch(t, f.tokenA, http.MethodPut)
	f.watch(t, f.tokenB, http.MethodPut)

	f.comment(t, f.owner.token, f.taskID, "plain comment")
	if len(tc.Mail.To(notifyEmail("a"))) != 1 {
		t.Fatal("the comment e-mail was switched on for A")
	}
	if len(tc.Mail.To(notifyEmail("b"))) != 0 {
		t.Fatal("B keeps the default: no comment e-mail")
	}

	f.comment(t, f.owner.token, f.taskID, "mention both", f.memberA, f.memberB)
	expectTypes(t, "A without in-app mentions", inbox(t, tc, f.tokenA, ""), "comment")
	if len(tc.Mail.To(notifyEmail("a"))) != 2 {
		t.Fatal("A still gets the mention e-mail")
	}
	expectTypes(t, "B", inbox(t, tc, f.tokenB, ""), "mention", "comment")
	if len(tc.Mail.To(notifyEmail("b"))) != 0 {
		t.Fatal("B switched the mention e-mail off")
	}
}

func TestNotifications_LocaleSettingChangesMailLanguage(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	if rec := request(t, tc.Router, http.MethodPut, "/api/v1/notifications/settings", f.tokenA, map[string]any{"locale": "de"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported locale: %d", rec.Code)
	}
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications/settings", f.tokenA, nil)
	if dataObject(t, rec)["locale"] != "en" {
		t.Fatalf("default locale: %s", rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPut, "/api/v1/notifications/settings", f.tokenA, map[string]any{"locale": "ru"}); rec.Code != http.StatusOK {
		t.Fatalf("set locale: %d", rec.Code)
	}
	f.comment(t, f.owner.token, f.taskID, "привет", f.memberA)
	f.comment(t, f.owner.token, f.taskID, "hello", f.memberB)
	if msg := tc.Mail.To(notifyEmail("a"))[0]; !strings.Contains(msg.Subject, "упомянули") {
		t.Fatalf("subject not Russian: %q", msg.Subject)
	}
	if msg := tc.Mail.To(notifyEmail("b"))[0]; !strings.Contains(msg.Subject, "mentioned you") {
		t.Fatalf("subject not English: %q", msg.Subject)
	}
}

func TestNotifications_RemovedMemberAndHiddenTasks(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	f.watch(t, f.tokenB, http.MethodPut)
	f.comment(t, f.owner.token, f.taskID, "before removal")
	expectTypes(t, "member", inbox(t, tc, f.tokenB, ""), "comment")

	rec := request(t, tc.Router, http.MethodDelete, membersPath(f.owner.workspaceID, "/"+itoa(f.memberB)), f.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove member: %d: %s", rec.Code, rec.Body.String())
	}
	expectTypes(t, "removed member sees nothing", inbox(t, tc, f.tokenB, ""))
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/notifications/unread-count", f.tokenB, nil)
	if dataObject(t, rec)["total"].(float64) != 0 {
		t.Fatalf("unread count leaks: %s", rec.Body.String())
	}
	f.comment(t, f.owner.token, f.taskID, "after removal", f.memberA)
	var stored int64
	tc.DB.Raw("SELECT count(*) FROM notifications WHERE user_id = ?", f.memberB).Scan(&stored)
	if stored != 1 {
		t.Fatalf("removed member got a new notification: %d rows", stored)
	}
	if len(tc.Mail.To(notifyEmail("b"))) != 0 {
		t.Fatal("removed member got a mail")
	}

	f.comment(t, f.owner.token, f.taskID, "mention", f.memberA)
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(f.taskID), f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete task: %d: %s", rec.Code, rec.Body.String())
	}
	expectTypes(t, "entries of deleted tasks are hidden", inbox(t, tc, f.tokenA, ""))
}

func TestNotifications_ThrottleKeepsInboxEntries(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Notifications.EmailPerHour = 2 })
	f := newNotifyFixture(t, tc)
	for i := 0; i < 3; i++ {
		f.comment(t, f.owner.token, f.taskID, fmt.Sprintf("ping %d", i), f.memberA)
	}
	if n := len(tc.Mail.To(notifyEmail("a"))); n != 2 {
		t.Fatalf("mails = %d, want 2", n)
	}
	if n := len(inbox(t, tc, f.tokenA, "")); n != 3 {
		t.Fatalf("inbox entries = %d, want 3", n)
	}
}

func TestNotifications_InboxReadCountsPaginationAndOwnership(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Notifications.MaxPerUser = 5 })
	f := newNotifyFixture(t, tc)
	f.watch(t, f.tokenB, http.MethodPut)
	for i := 0; i < 3; i++ {
		f.comment(t, f.owner.token, f.taskID, fmt.Sprintf("m%d", i), f.memberA)
	}
	f.patch(t, f.taskID, map[string]any{"status_id": f.status2})
	f.patch(t, f.taskID, map[string]any{"due_date": "2031-05-04T10:00:00Z"})

	all := inbox(t, tc, f.tokenA, "")
	if len(all) != 5 {
		t.Fatalf("A has %d entries: %v", len(all), types(all))
	}
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications?limit=2&page=2", f.tokenA, nil)
	body := decodeJSON(t, rec)
	if len(body["data"].([]any)) != 2 || body["pagination"].(map[string]any)["total"].(float64) != 5 {
		t.Fatalf("page 2: %s", rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications?type=bogus", f.tokenA, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown type filter: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications?unread=maybe", f.tokenA, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad unread: %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/notifications/unread-count", f.tokenA, nil)
	counts := dataObject(t, rec)
	by := counts["by_type"].(map[string]any)
	if counts["total"].(float64) != 5 || by["mention"].(float64) != 3 || by["status_changed"].(float64) != 1 || by["assigned"].(float64) != 0 {
		t.Fatalf("counts = %v", counts)
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/notifications/read", f.tokenA, map[string]any{"all": true, "type": "mention"})
	if dataObject(t, rec)["updated"].(float64) != 3 {
		t.Fatalf("mark type read: %s", rec.Body.String())
	}
	unread := inbox(t, tc, f.tokenA, "?unread=true")
	if len(unread) != 2 {
		t.Fatalf("unread = %v", types(unread))
	}
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/notifications/read", f.tokenA, map[string]any{"ids": []uint{unread[0].ID}}); rec.Code != http.StatusOK {
		t.Fatalf("mark ids: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/notifications/read", f.tokenA, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty read request: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/notifications/read", f.tokenB, map[string]any{"ids": []uint{unread[1].ID}}); rec.Code != http.StatusOK ||
		dataObject(t, rec)["updated"].(float64) != 0 {
		t.Fatalf("marking someone else's entry must change nothing: %s", rec.Body.String())
	}
	if len(inbox(t, tc, f.tokenA, "?unread=true")) != 1 {
		t.Fatal("one entry should still be unread")
	}

	target := unread[1].ID
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/notifications/"+itoa(target), f.tokenB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("IDOR delete: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/notifications/"+itoa(target), f.tokenA, nil); rec.Code != http.StatusOK {
		t.Fatalf("own delete: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/notifications/"+itoa(target), f.tokenA, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d", rec.Code)
	}

	f.comment(t, f.owner.token, f.taskID, "m3", f.memberA)
	f.comment(t, f.owner.token, f.taskID, "m4", f.memberA)
	f.comment(t, f.owner.token, f.taskID, "m5", f.memberA)
	var stored int64
	tc.DB.Raw("SELECT count(*) FROM notifications WHERE user_id = ?", f.memberA).Scan(&stored)
	if stored != 5 {
		t.Fatalf("retention keeps %d rows, want 5", stored)
	}
}

func channelsURL(suffix string) string { return "/api/v1/notifications/channels" + suffix }

func confirmationToken(t *testing.T, tc *TestContext, to string) string {
	t.Helper()
	msgs := tc.Mail.To(to)
	if len(msgs) == 0 {
		t.Fatalf("no mail to %s", to)
	}
	return linkToken(t, msgs[len(msgs)-1], "/confirm-channel")
}

func TestNotifications_ExtraAddressMustBeConfirmedBeforeAnythingIsSent(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	extra := "extra-a@example.test"

	rec := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "email", "destination": "  Extra-A@Example.test "})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: %d: %s", rec.Code, rec.Body.String())
	}
	channelID := responseID(t, rec)
	if dataObject(t, rec)["verified"] != false {
		t.Fatalf("new channel is verified: %s", rec.Body.String())
	}
	only(t, tc, extra)

	f.comment(t, f.owner.token, f.taskID, "ping", f.memberA)
	if len(tc.Mail.To(extra)) != 1 {
		t.Fatalf("unconfirmed address received a notification: %d mails", len(tc.Mail.To(extra)))
	}
	if len(tc.Mail.To(notifyEmail("a"))) != 1 {
		t.Fatal("the account address still receives mail")
	}

	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/resend"), f.tokenA, nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("resend within a minute: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/resend"), f.tokenB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("resend of another user's channel: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, channelsURL("/"+itoa(channelID)), f.tokenB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete of another user's channel: %d", rec.Code)
	}
	if err := tc.DB.Exec("UPDATE channel_confirmations SET created_at = now() - interval '5 minutes' WHERE channel_id = ?", channelID).Error; err != nil {
		t.Fatal(err)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/resend"), f.tokenA, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("resend: %d: %s", rec.Code, rec.Body.String())
	}
	if len(tc.Mail.To(extra)) != 2 {
		t.Fatal("resend did not send a mail")
	}
	oldToken := linkToken(t, tc.Mail.To(extra)[0], "/confirm-channel")
	token := confirmationToken(t, tc, extra)
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/confirm"), "", map[string]any{"token": oldToken}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a replaced token must stop working: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/confirm"), "", map[string]any{"token": "garbage"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("garbage token: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/confirm"), "", map[string]any{"token": token}); rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/confirm"), "", map[string]any{"token": token}); rec.Code != http.StatusBadRequest {
		t.Fatalf("token reuse: %d", rec.Code)
	}
	var stored string
	tc.DB.Raw("SELECT token_hash FROM channel_confirmations WHERE channel_id = ?", channelID).Scan(&stored)
	if stored == "" || strings.Contains(stored, token) {
		t.Fatalf("token must be stored hashed: %q", stored)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/resend"), f.tokenA, nil); rec.Code != http.StatusConflict {
		t.Fatalf("resend of a confirmed channel: %d", rec.Code)
	}

	f.comment(t, f.owner.token, f.taskID, "ping 2", f.memberA)
	if len(tc.Mail.To(extra)) != 3 || len(tc.Mail.To(notifyEmail("a"))) != 2 {
		t.Fatalf("after confirmation both addresses receive mail: extra=%d account=%d", len(tc.Mail.To(extra)), len(tc.Mail.To(notifyEmail("a"))))
	}

	rec = request(t, tc.Router, http.MethodGet, channelsURL(""), f.tokenA, nil)
	items := decodeJSON(t, rec)["data"].([]any)
	if len(items) != 3 {
		t.Fatalf("channels = %s", rec.Body.String())
	}

	if rec := request(t, tc.Router, http.MethodDelete, channelsURL("/"+itoa(channelID)), f.tokenA, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	f.comment(t, f.owner.token, f.taskID, "ping 3", f.memberA)
	if len(tc.Mail.To(extra)) != 3 {
		t.Fatal("a removed channel keeps receiving mail")
	}
}

func TestNotifications_SameMailboxCanBeConfirmedByDifferentUsers(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	shared := "shared-box@example.test"
	for _, token := range []string{f.tokenA, f.tokenB} {
		if rec := request(t, tc.Router, http.MethodPost, channelsURL(""), token, map[string]any{"kind": "email", "destination": shared}); rec.Code != http.StatusCreated {
			t.Fatalf("add shared address: %d: %s", rec.Code, rec.Body.String())
		}
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "email", "destination": shared}); rec.Code != http.StatusConflict {
		t.Fatalf("own duplicate: %d", rec.Code)
	}
	for _, msg := range tc.Mail.To(shared) {
		if rec := request(t, tc.Router, http.MethodPost, channelsURL("/confirm"), "", map[string]any{"token": linkToken(t, msg, "/confirm-channel")}); rec.Code != http.StatusOK {
			t.Fatalf("confirm: %d", rec.Code)
		}
	}
	var verified int64
	tc.DB.Raw("SELECT count(*) FROM notification_channels WHERE destination = ? AND verified_at IS NOT NULL", shared).Scan(&verified)
	if verified != 2 {
		t.Fatalf("verified rows = %d", verified)
	}
}

func TestNotifications_ChannelValidationLimitsAndLocalizedConfirmation(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	for _, body := range []map[string]any{
		{"kind": "email", "destination": "not-an-address"},
		{"kind": "email", "destination": "Name <a@b.example>"},
		{"kind": "in_app", "destination": "x@y.example"},
		{"kind": "telegram", "destination": "@someone"},
		{"kind": "email"},
	} {
		if rec := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%v: %d: %s", body, rec.Code, rec.Body.String())
		}
	}
	rec := requestWithHeader(t, tc, http.MethodPost, channelsURL(""), f.tokenA, "Accept-Language", "ru-RU",
		map[string]any{"kind": "email", "destination": "ru-extra@example.test"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add ru: %d", rec.Code)
	}
	if msg := only(t, tc, "ru-extra@example.test"); !strings.Contains(msg.Subject, "Подтвердите") {
		t.Fatalf("confirmation mail is not Russian: %q", msg.Subject)
	}
	for i := 1; i < 5; i++ {
		if rec := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA,
			map[string]any{"kind": "email", "destination": fmt.Sprintf("extra%d@example.test", i)}); rec.Code != http.StatusCreated {
			t.Fatalf("add %d: %d", i, rec.Code)
		}
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "email", "destination": "sixth@example.test"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("sixth address: %d", rec.Code)
	}
	if len(tc.Mail.To("sixth@example.test")) != 0 {
		t.Fatal("a rejected address got a mail")
	}
}

func TestNotifications_UnverifiedAccountAddressIsNeverUsed(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	if err := tc.DB.Exec("UPDATE users SET email_verified_at = NULL WHERE id = ?", f.memberA).Error; err != nil {
		t.Fatal(err)
	}
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications/preferences", f.tokenA, nil)
	for _, raw := range decodeJSON(t, rec)["data"].([]any) {
		cell := raw.(map[string]any)
		if cell["channel"] == "email" && cell["usable"] != false {
			t.Fatalf("e-mail should not be usable: %v", cell)
		}
	}
	f.comment(t, f.owner.token, f.taskID, "ping", f.memberA)
	if len(tc.Mail.To(notifyEmail("a"))) != 0 {
		t.Fatal("an unverified account address received a notification")
	}
	expectTypes(t, "in-app still works", inbox(t, tc, f.tokenA, ""), "mention")
}

func TestNotifications_AccountDeletionRemovesNotificationData(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "email", "destination": "gone@example.test"})
	setPreference(t, tc, f.tokenA, "comment", "email", true)
	request(t, tc.Router, http.MethodPut, "/api/v1/notifications/settings", f.tokenA, map[string]any{"locale": "ru"})
	f.comment(t, f.owner.token, f.taskID, "ping", f.memberA)

	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile", f.tokenA, map[string]any{"password": "password-123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete account: %d: %s", rec.Code, rec.Body.String())
	}
	for _, table := range []string{"notifications", "task_watchers", "notification_preferences", "notification_settings", "notification_channels"} {
		var n int64
		tc.DB.Raw("SELECT count(*) FROM "+table+" WHERE user_id = ?", f.memberA).Scan(&n)
		if n != 0 {
			t.Errorf("%s still has %d rows of the deleted user", table, n)
		}
	}
	var confirmations int64
	tc.DB.Raw("SELECT count(*) FROM channel_confirmations").Scan(&confirmations)
	if confirmations != 0 {
		t.Fatalf("confirmations left: %d", confirmations)
	}
}

func TestNotifications_EndpointsRequireAuthenticationAndHideForeignTasks(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous inbox: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications/preferences", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous preferences: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/notifications/abc", f.tokenA, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}
}
