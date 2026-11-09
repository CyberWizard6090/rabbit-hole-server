package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type actorEnv struct {
	fcEnv
	formID      uint
	formURL     string
	slug        string
	titleID     uint
	nameID      uint
	emailID     uint
	assigneeID  uint
	member      uint
	memberToken string
	guest       uint
	guestToken  string
}

func newActorEnv(t *testing.T, tc *TestContext, label string, customize func(env *actorEnv, config map[string]any)) *actorEnv {
	t.Helper()
	env := &actorEnv{fcEnv: newFCEnv(t, tc, label)}
	env.member, env.memberToken = newUser(t, tc, label+"-member@example.test")
	env.guest, env.guestToken = newUser(t, tc, label+"-guest@example.test")
	addMemberWithRole(t, tc, env.owner.workspaceID, env.member, "Member")
	addMemberWithRole(t, tc, env.owner.workspaceID, env.guest, "Guest")

	env.formID, env.formURL = env.newForm(t, tc, "Intake "+label)
	env.titleID, _ = fcAddQuestion(t, tc, env.owner.token, env.formURL, "Title", nil)
	env.nameID, _ = fcAddQuestion(t, tc, env.owner.token, env.formURL, "Your name", nil)
	env.emailID = mustCreate(t, tc, env.owner.token, env.formURL+"/questions", map[string]any{
		"type": "email", "title": "Your email",
	})
	env.assigneeID = mustCreate(t, tc, env.owner.token, env.formURL+"/questions", map[string]any{
		"type": "single_choice", "title": "Who", "options": []any{
			map[string]any{"label": "Owner", "value": itoa(env.owner.userID)},
			map[string]any{"label": "Member", "value": itoa(env.member)},
		},
	})
	config := env.taskConfig([]any{
		fcTitleMapping(env.titleID),
		map[string]any{"question_id": env.nameID, "target": map[string]any{"type": "requester", "field": "name"}},
		map[string]any{"question_id": env.emailID, "target": map[string]any{"type": "requester", "field": "email"}},
		map[string]any{"question_id": env.assigneeID, "target": map[string]any{"type": "task_field", "field": "assignee"}},
	})
	if customize != nil {
		customize(env, config)
	}
	if rec := request(t, tc.Router, http.MethodPut, env.formURL+"/task-config", env.owner.token, config); rec.Code != http.StatusOK {
		t.Fatalf("save task config: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPost, env.formURL+"/publish", env.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
	env.slug = dataObject(t, request(t, tc.Router, http.MethodGet, env.formURL, env.owner.token, nil))["slug"].(string)
	return env
}

var actorSubmitCounter int

func (e *actorEnv) submit(t *testing.T, tc *TestContext, token string, answers map[string]any) uint {
	t.Helper()
	actorSubmitCounter++
	raw, err := json.Marshal(map[string]any{"answers": answers})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/"+e.slug+"/submissions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("actor-%d-%d", e.formID, actorSubmitCounter))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = fmt.Sprintf("10.9.%d.%d:4000", actorSubmitCounter>>8&255, actorSubmitCounter&255)
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func (e *actorEnv) taskOf(t *testing.T, tc *TestContext, token string, submissionID uint) map[string]any {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/submissions/"+itoa(submissionID)+"/tasks", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("submission tasks: %d: %s", rec.Code, rec.Body.String())
	}
	tasks := responseArray(t, rec)
	if len(tasks) != 1 {
		t.Fatalf("submission produced %d tasks: %s", len(tasks), rec.Body.String())
	}
	return tasks[0].(map[string]any)
}

func (e *actorEnv) answers(title string, extra map[uint]string) map[string]any {
	out := map[string]any{itoa(e.titleID): title}
	for id, value := range extra {
		out[itoa(id)] = value
	}
	return out
}

func activityActor(t *testing.T, tc *TestContext, token string, taskID uint, action string) map[string]any {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/activity", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("activity: %d: %s", rec.Code, rec.Body.String())
	}
	for _, raw := range responseArray(t, rec) {
		item := raw.(map[string]any)
		if item["action"] == action {
			return item["actor"].(map[string]any)
		}
	}
	t.Fatalf("no %s activity: %s", action, rec.Body.String())
	return nil
}

func TestEventActors_FormTaskIsAttributedToTheFormAndSurvivesRenameAndArchive(t *testing.T) {
	tc := newTestContext(t)
	env := newActorEnv(t, tc, "actform", nil)

	submissionID := env.submit(t, tc, "", env.answers("Broken login", nil))
	task := env.taskOf(t, tc, env.owner.token, submissionID)
	taskID := uint(task["id"].(float64))
	formName := "Intake actform"

	if task["user_id"] != float64(env.owner.userID) {
		t.Fatalf("technical owner = %v, want the form creator %d", task["user_id"], env.owner.userID)
	}
	createdBy := task["created_by"].(map[string]any)
	if createdBy["type"] != "form" || createdBy["id"] != float64(env.formID) || createdBy["name"] != formName {
		t.Fatalf("created_by = %v", createdBy)
	}
	actor := activityActor(t, tc, env.owner.token, taskID, "task.created")
	if actor["type"] != "form" || actor["id"] != float64(env.formID) || actor["name"] != formName {
		t.Fatalf("task.created actor = %v", actor)
	}

	if rec := request(t, tc.Router, http.MethodPatch, env.formURL, env.owner.token, map[string]any{"name": "Renamed intake", "slug": env.slug}); rec.Code != http.StatusOK {
		t.Fatalf("rename form: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPost, env.formURL+"/archive", env.owner.token, nil); rec.Code >= 300 {
		t.Fatalf("archive form: %d: %s", rec.Code, rec.Body.String())
	}
	actor = activityActor(t, tc, env.owner.token, taskID, "task.created")
	if actor["name"] != formName || actor["type"] != "form" {
		t.Fatalf("history must keep the label from the event time: %v", actor)
	}
	got := env.taskOf(t, tc, env.owner.token, submissionID)["created_by"].(map[string]any)
	if got["name"] != formName {
		t.Fatalf("created_by after rename = %v", got)
	}

	var foreignKeys int64
	if err := tc.DB.Raw(`SELECT count(*) FROM pg_constraint c JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
		WHERE c.contype = 'f' AND c.conrelid IN ('task_activities'::regclass, 'tasks'::regclass) AND a.attname = 'actor_id'`).
		Scan(&foreignKeys).Error; err != nil || foreignKeys != 0 {
		t.Fatalf("actor_id must not reference another table so history outlives forms: %d, %v", foreignKeys, err)
	}
}

func TestEventActors_ManualTaskCreatedByIsThePublicUserProjection(t *testing.T) {
	tc := newTestContext(t)
	env := newActorEnv(t, tc, "actuser", nil)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(env.listID)+"/tasks", env.memberToken,
		map[string]any{"title": "Manual task", "status_id": env.statusID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	created := dataObject(t, rec)
	createdBy := created["created_by"].(map[string]any)
	if createdBy["type"] != "user" || createdBy["id"] != float64(env.member) || createdBy["name"] == "" {
		t.Fatalf("created_by = %v", createdBy)
	}
	for _, path := range []string{
		"/api/v1/tasks/" + itoa(uint(created["id"].(float64))),
		"/api/v1/lists/" + itoa(env.listID) + "/tasks",
		"/api/v1/tasks/" + itoa(uint(created["id"].(float64))) + "/activity",
	} {
		body := request(t, tc.Router, http.MethodGet, path, env.guestToken, nil).Body.String()
		if strings.Contains(body, "@example.test") {
			t.Fatalf("%s leaks an e-mail address: %s", path, body)
		}
		if strings.Contains(body, `"requester"`) {
			t.Fatalf("%s exposes a requester on a manual task: %s", path, body)
		}
	}
	actor := activityActor(t, tc, env.guestToken, uint(created["id"].(float64)), "task.created")
	if actor["type"] != "user" || actor["id"] != float64(env.member) || actor["username"] == nil {
		t.Fatalf("user actor keeps the public projection: %v", actor)
	}
}

func TestEventActors_NonUserActorsDoNotSuppressNotifications(t *testing.T) {
	tc := newTestContext(t)
	env := newActorEnv(t, tc, "actnotify", func(env *actorEnv, config map[string]any) {
		config["task_defaults"] = map[string]any{"priority": "normal", "status_id": env.statusID, "assignee_id": env.owner.userID}
	})
	before := len(inbox(t, tc, env.owner.token, ""))

	env.submit(t, tc, "", env.answers("Needs triage", nil))
	tc.Notifications.Wait()

	entries := inbox(t, tc, env.owner.token, "")
	if len(entries) != before+1 || entries[0].Type != "assigned" {
		t.Fatalf("the form creator must be told about the task of their own form: %v", types(entries))
	}
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications", env.owner.token, nil)
	first := responseArray(t, rec)[0].(map[string]any)
	payload := first["payload"].(map[string]any)
	if payload["actor_type"] != "form" || payload["actor_name"] != "Intake actnotify" {
		t.Fatalf("notification payload = %v", payload)
	}
}

func TestEventActors_RequesterVisibilityByRole(t *testing.T) {
	tc := newTestContext(t)
	env := newActorEnv(t, tc, "actreq", nil)

	anonymous := env.submit(t, tc, "", env.answers("From a visitor", map[uint]string{
		env.nameID: "Pat Visitor", env.emailID: "pat@visitor.test",
	}))
	owner := env.taskOf(t, tc, env.owner.token, anonymous)
	taskID := uint(owner["id"].(float64))
	requester := owner["requester"].(map[string]any)
	if requester["name"] != "Pat Visitor" || requester["email"] != "pat@visitor.test" {
		t.Fatalf("requester = %v", requester)
	}
	member := env.taskOf(t, tc, env.memberToken, anonymous)
	if member["requester"] == nil {
		t.Fatal("members must see the requester")
	}

	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/submissions/"+itoa(anonymous)+"/tasks", env.guestToken, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("guest must not read form submissions: %d: %s", rec.Code, rec.Body.String())
	}
	guestSources := []string{
		"/api/v1/tasks/" + itoa(taskID),
		"/api/v1/lists/" + itoa(env.listID) + "/tasks",
		"/api/v1/spaces/" + itoa(env.owner.spaceID),
	}
	for _, path := range guestSources {
		rec := request(t, tc.Router, http.MethodGet, path, env.guestToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("guest %s: %d: %s", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), `"requester"`) || strings.Contains(rec.Body.String(), "visitor.test") {
			t.Fatalf("guest %s sees the requester: %s", path, rec.Body.String())
		}
	}
	for _, path := range guestSources[0:2] {
		rec := request(t, tc.Router, http.MethodGet, path, env.memberToken, nil)
		if !strings.Contains(rec.Body.String(), "pat@visitor.test") {
			t.Fatalf("member %s must see the requester: %s", path, rec.Body.String())
		}
	}
	rec := request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), env.memberToken, map[string]any{"title": "Edited by member"})
	if rec.Code != http.StatusOK || dataObject(t, rec)["requester"] == nil {
		t.Fatalf("member update response: %d: %s", rec.Code, rec.Body.String())
	}

	authenticated := env.submit(t, tc, env.memberToken, env.answers("From a colleague", map[uint]string{
		env.nameID: "Ignored Name", env.emailID: "ignored@colleague.test",
	}))
	task := env.taskOf(t, tc, env.owner.token, authenticated)
	requester = task["requester"].(map[string]any)
	user := requester["user"].(map[string]any)
	if user["id"] != float64(env.member) || requester["email"] != nil {
		t.Fatalf("authenticated requester = %v", requester)
	}
	raw, _ := json.Marshal(task)
	if strings.Contains(string(raw), "@") {
		t.Fatalf("an authenticated requester must not expose any e-mail address: %s", raw)
	}

	junk := env.submit(t, tc, "not-a-token", env.answers("Bad token is anonymous", nil))
	if env.taskOf(t, tc, env.owner.token, junk)["requester"] != nil {
		t.Fatal("an invalid token must be treated as anonymous and unmapped answers leave no requester")
	}
}

func TestEventActors_OwnerIDValidationAndFallback(t *testing.T) {
	tc := newTestContext(t)
	env := newActorEnv(t, tc, "actowner", func(env *actorEnv, config map[string]any) {
		config["owner_id"] = env.member
	})

	saved := dataObject(t, request(t, tc.Router, http.MethodGet, env.formURL+"/task-config", env.owner.token, nil))
	if saved["owner_id"] != float64(env.member) {
		t.Fatalf("saved owner_id = %v", saved["owner_id"])
	}
	outsider, _ := newUser(t, tc, "actowner-outsider@example.test")
	config := env.taskConfig([]any{fcTitleMapping(env.titleID)})
	config["owner_id"] = outsider
	rec := request(t, tc.Router, http.MethodPut, env.formURL+"/task-config", env.owner.token, config)
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "owner_id")

	first := env.taskOf(t, tc, env.owner.token, env.submit(t, tc, "", env.answers("Owned by member", nil)))
	if first["user_id"] != float64(env.member) {
		t.Fatalf("technical owner = %v, want the configured member %d", first["user_id"], env.member)
	}
	if first["created_by"].(map[string]any)["type"] != "form" {
		t.Fatalf("created_by = %v", first["created_by"])
	}

	if rec := request(t, tc.Router, http.MethodDelete, membersPath(env.owner.workspaceID, "/"+itoa(env.member)), env.owner.token, nil); rec.Code >= 300 {
		t.Fatalf("remove member: %d: %s", rec.Code, rec.Body.String())
	}
	second := env.taskOf(t, tc, env.owner.token, env.submit(t, tc, "", env.answers("Owner left", nil)))
	if second["user_id"] != float64(env.owner.userID) {
		t.Fatalf("technical owner = %v, want the form creator %d after the owner left", second["user_id"], env.owner.userID)
	}
}

func TestEventActors_AssigneeMapping(t *testing.T) {
	tc := newTestContext(t)
	env := newActorEnv(t, tc, "actassign", nil)

	assigned := env.taskOf(t, tc, env.owner.token, env.submit(t, tc, "", env.answers("Assign to member", map[uint]string{env.assigneeID: itoa(env.member)})))
	assignees := assigned["assignees"].([]any)
	if len(assignees) != 1 || assignees[0].(map[string]any)["id"] != float64(env.member) {
		t.Fatalf("assignees = %v", assignees)
	}

	if rec := request(t, tc.Router, http.MethodDelete, membersPath(env.owner.workspaceID, "/"+itoa(env.member)), env.owner.token, nil); rec.Code >= 300 {
		t.Fatalf("remove member: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := env.submit(t, tc, "", env.answers("Member is gone", map[uint]string{env.assigneeID: itoa(env.member)}))
	unassigned := env.taskOf(t, tc, env.owner.token, submissionID)
	if len(unassigned["assignees"].([]any)) != 0 {
		t.Fatalf("a mapped user who left must not be assigned: %v", unassigned["assignees"])
	}
	var snapshot string
	if err := tc.DB.Raw("SELECT mapping_snapshot::text FROM form_submission_tasks WHERE submission_id = ?", submissionID).Scan(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(snapshot), &entries); err != nil {
		t.Fatalf("snapshot %s: %v", snapshot, err)
	}
	var failures []string
	for _, entry := range entries {
		if failure, ok := entry["failure"].(string); ok {
			failures = append(failures, failure)
		}
	}
	if len(failures) != 1 || failures[0] != "NOT_A_MEMBER" {
		t.Fatalf("mapping snapshot failures = %v (%s)", failures, snapshot)
	}
}

func TestEventActors_MappingConfigurationIsValidated(t *testing.T) {
	tc := newTestContext(t)
	env := newActorEnv(t, tc, "actcfg", nil)
	outsider, _ := newUser(t, tc, "actcfg-outsider@example.test")
	badChoice := mustCreate(t, tc, env.owner.token, env.formURL+"/questions", map[string]any{
		"type": "dropdown", "title": "Stranger", "options": []any{map[string]any{"label": "Out", "value": itoa(outsider)}},
	})
	textChoice := mustCreate(t, tc, env.owner.token, env.formURL+"/questions", map[string]any{
		"type": "dropdown", "title": "Label", "options": []any{map[string]any{"label": "A", "value": "alpha"}},
	})
	put := func(mapping map[string]any) *httptest.ResponseRecorder {
		return request(t, tc.Router, http.MethodPut, env.formURL+"/task-config", env.owner.token,
			env.taskConfig([]any{fcTitleMapping(env.titleID), mapping}))
	}
	target := func(kind, field string) map[string]any { return map[string]any{"type": kind, "field": field} }

	fcExpectError(t, put(map[string]any{"question_id": badChoice, "target": target("task_field", "assignee")}),
		http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "mappings[1].target")
	fcExpectError(t, put(map[string]any{"question_id": textChoice, "target": target("task_field", "assignee")}),
		http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "mappings[1].target")
	fcExpectError(t, put(map[string]any{"question_id": env.nameID, "target": target("task_field", "assignee")}),
		http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "mappings[1].target")
	fcExpectError(t, put(map[string]any{"question_id": env.nameID, "target": target("requester", "phone")}),
		http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "mappings[1].target.field")
	fcExpectError(t, put(map[string]any{"question_id": env.nameID, "target": target("requester", "email")}),
		http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "mappings[1].target")
	if rec := put(map[string]any{"question_id": env.emailID, "target": target("requester", "email")}); rec.Code != http.StatusOK {
		t.Fatalf("email requester mapping: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEventActors_ActivityActorConstraints(t *testing.T) {
	tc := newTestContext(t)
	env := newActorEnv(t, tc, "actdb", nil)
	task := env.taskOf(t, tc, env.owner.token, env.submit(t, tc, "", env.answers("Constraint probe", nil)))
	taskID := uint(task["id"].(float64))

	insert := func(userID any, actorType string) error {
		return tc.DB.Exec("INSERT INTO task_activities (task_id, user_id, actor_type, action) VALUES (?, ?, ?, 'probe')", taskID, userID, actorType).Error
	}
	if err := insert(nil, "system"); err != nil {
		t.Fatalf("a system actor needs no user: %v", err)
	}
	if err := insert(nil, "user"); err == nil {
		t.Fatal("a user actor must reference a user")
	}
	if err := insert(env.owner.userID, "automation"); err == nil {
		t.Fatal("a non-user actor must not carry a user id")
	}
	if err := insert(nil, "robot"); err == nil {
		t.Fatal("unknown actor types must be rejected")
	}
}
