package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"rabbit-hole-server/internal/config"
)

type formFixtureData struct {
	owner      workspaceOwner
	member     string
	guest      string
	outsider   string
	listID     uint
	statusID   uint
	formID     uint
	questionID uint
	slug       string
	formURL    string
	templateID uint
}

func newFormFixture(t *testing.T, tc *TestContext, label string) formFixtureData {
	t.Helper()
	return buildFormFixture(t, tc, label, true)
}

func buildFormFixture(t *testing.T, tc *TestContext, label string, withUsers bool) formFixtureData {
	t.Helper()
	owner := newWorkspaceOwner(t, tc, label)
	var memberToken, guestToken, outsiderToken string
	if withUsers {
		memberToken, guestToken, outsiderToken = addFormUsers(t, tc, owner, label)
	}
	return finishFormFixture(t, tc, owner, memberToken, guestToken, outsiderToken, label)
}

func addFormUsers(t *testing.T, tc *TestContext, owner workspaceOwner, label string) (string, string, string) {
	t.Helper()
	memberID, memberToken := newUser(t, tc, label+"-member@example.test")
	guestID, guestToken := newUser(t, tc, label+"-guest@example.test")
	_, outsiderToken := newUser(t, tc, label+"-outsider@example.test")
	for _, u := range []struct {
		id   uint
		role string
	}{{memberID, "Member"}, {guestID, "Guest"}} {
		addContact(t, tc, owner.token, u.id)
		rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
			map[string]any{"user_id": u.id, "role": u.role})
		if rec.Code != http.StatusCreated {
			t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
		}
	}
	return memberToken, guestToken, outsiderToken
}

func finishFormFixture(t *testing.T, tc *TestContext, owner workspaceOwner, memberToken, guestToken, outsiderToken, label string) formFixtureData {
	t.Helper()
	listID := mustCreate(t, tc, owner.token, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", map[string]any{"name": "Inbox"})
	statusID := mustCreate(t, tc, owner.token, "/api/v1/lists/"+itoa(listID)+"/statuses",
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})

	formURL := "/api/v1/workspaces/" + itoa(owner.workspaceID) + "/forms"
	slug := fmt.Sprintf("%s-%d", label, time.Now().UnixNano())
	formID := mustCreate(t, tc, owner.token, formURL, map[string]any{"name": "Request", "slug": slug})
	questionID := mustCreate(t, tc, owner.token, formURL+"/"+itoa(formID)+"/questions", map[string]any{
		"type": "short_text", "title": "Title", "required": true, "position": 0,
	})
	templateID := mustCreate(t, tc, owner.token, "/api/v1/workspaces/"+itoa(owner.workspaceID)+"/task-templates",
		map[string]any{"name": "Base", "configuration": map[string]any{
			"task": map[string]any{"priority": "normal"}, "tags": []uint{}, "subtasks": []any{}, "custom_fields": []any{},
		}})
	rec := request(t, tc.Router, http.MethodPut, formURL+"/"+itoa(formID)+"/task-config", owner.token, map[string]any{
		"enabled":       true,
		"target":        map[string]any{"workspace_id": owner.workspaceID, "list_id": listID},
		"task_defaults": map[string]any{"priority": "normal", "status_id": statusID},
		"mappings": []any{map[string]any{
			"question_id": questionID,
			"target":      map[string]any{"type": "task_field", "field": "title"},
		}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("configure task creation: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, formURL+"/"+itoa(formID)+"/publish", owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish form: %d: %s", rec.Code, rec.Body.String())
	}
	return formFixtureData{
		owner: owner, member: memberToken, guest: guestToken, outsider: outsiderToken,
		listID: listID, statusID: statusID, formID: formID, questionID: questionID,
		slug: slug, formURL: formURL, templateID: templateID,
	}
}

func (f formFixtureData) submit(t *testing.T, tc *TestContext, key, title string) (int, uint, map[string]any) {
	t.Helper()
	rec := requestWithHeader(t, tc, http.MethodPost, "/api/v1/public/forms/"+f.slug+"/submissions", "",
		"Idempotency-Key", key, map[string]any{"answers": map[string]any{itoa(f.questionID): title}})
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		return rec.Code, 0, nil
	}
	return rec.Code, responseID(t, rec), dataObject(t, rec)
}

func TestForms_SubmissionTaskIsRecordedInActivityAfterCommit(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "formact")

	code, submissionID, body := f.submit(t, tc, "activity-1", "From the form")
	if code != http.StatusCreated || body["task_creation_status"] != "succeeded" {
		t.Fatalf("submit: %d %v", code, body)
	}
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/submissions/"+itoa(submissionID)+"/tasks", f.owner.token, nil)
	tasks := responseArray(t, rec)
	if rec.Code != http.StatusOK || len(tasks) != 1 {
		t.Fatalf("submission tasks: %d %s", rec.Code, rec.Body.String())
	}
	taskID := uint(tasks[0].(map[string]any)["id"].(float64))

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/activity", f.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("activity: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "task.created") || !strings.Contains(rec.Body.String(), "From the form") {
		t.Fatalf("form task creation is missing from the activity log: %s", rec.Body.String())
	}
}

func TestForms_FailedTaskCreationWritesNoActivity(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "formnoact")
	rec := request(t, tc.Router, http.MethodDelete,
		"/api/v1/lists/"+itoa(f.listID)+"/statuses/"+itoa(f.statusID), f.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status: %d %s", rec.Code, rec.Body.String())
	}
	code, _, body := f.submit(t, tc, "noact-1", "Will fail")
	if code != http.StatusCreated || body["task_creation_status"] != "failed" {
		t.Fatalf("submit: %d %v", code, body)
	}
	var entries int64
	if err := tc.DB.Table("task_activities").Where("new_value::text LIKE ?", "%Will fail%").Count(&entries).Error; err != nil {
		t.Fatalf("count activities: %v", err)
	}
	if entries != 0 {
		t.Fatalf("rolled back task left %d activity entries", entries)
	}
}

func TestForms_TemplateApplyIsRecordedInActivity(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "tplact")
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/task-templates/"+itoa(f.templateID)+"/apply", f.owner.token,
		map[string]any{"list_id": f.listID, "status_id": f.statusID, "title": "Applied once"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	taskID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/activity", f.owner.token, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "task.created") {
		t.Fatalf("template task has no created entry: %d %s", rec.Code, rec.Body.String())
	}
}

func TestForms_PublicSubmissionIsRateLimitedPerForm(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Forms.SubmissionsPerHour = 2 })
	f := newFormFixture(t, tc, "formlimit")
	other := buildFormFixture(t, tc, "formlimit2", false)

	for i := 1; i <= 2; i++ {
		if code, _, _ := f.submit(t, tc, fmt.Sprintf("limit-%d", i), "ok"); code != http.StatusCreated {
			t.Fatalf("submission %d: %d", i, code)
		}
	}
	if code, _, _ := f.submit(t, tc, "limit-3", "too many"); code != http.StatusTooManyRequests {
		t.Fatalf("third submission status = %d, want 429", code)
	}
	if code, _, _ := other.submit(t, tc, "limit-other", "ok"); code != http.StatusCreated {
		t.Fatalf("another form must have its own budget, got %d", code)
	}
}

func TestForms_PublicSubmissionRejectsOversizedBodyAndBadInput(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "formbody")
	url := "/api/v1/public/forms/" + f.slug + "/submissions"

	rec := requestWithHeader(t, tc, http.MethodPost, url, "", "Idempotency-Key", "big",
		map[string]any{"answers": map[string]any{itoa(f.questionID): strings.Repeat("x", 200<<10)}})
	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, want 413/400", rec.Code)
	}
	rec = requestWithHeader(t, tc, http.MethodPost, url, "", "Idempotency-Key", "",
		map[string]any{"answers": map[string]any{itoa(f.questionID): "x"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key status = %d, want 400", rec.Code)
	}
	rec = requestWithHeader(t, tc, http.MethodPost, url, "", "Idempotency-Key", "unknown-question",
		map[string]any{"answers": map[string]any{"999999": "x"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown question status = %d, want 400", rec.Code)
	}
	rec = requestWithHeader(t, tc, http.MethodPost, url, "", "Idempotency-Key", "missing-required",
		map[string]any{"answers": map[string]any{}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing required answer status = %d, want 400", rec.Code)
	}
	rec = requestWithHeader(t, tc, http.MethodPost, "/api/v1/public/forms/no-such-form/submissions", "",
		"Idempotency-Key", "nf", map[string]any{"answers": map[string]any{}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown form status = %d, want 404", rec.Code)
	}
}

func TestForms_IdempotencyKeyReusedWithDifferentAnswersConflicts(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "formidem")
	if code, _, _ := f.submit(t, tc, "same-key", "first"); code != http.StatusCreated {
		t.Fatalf("first submit: %d", code)
	}
	if code, _, _ := f.submit(t, tc, "same-key", "different"); code != http.StatusConflict {
		t.Fatalf("different payload with same key status = %d, want 409", code)
	}
}

func TestForms_ConcurrentSubmissionsWithOneKeyCreateOneTask(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "formrace")
	const workers = 4
	results := make(chan int, workers)
	for i := 0; i < workers; i++ {
		go func() {
			code, _, _ := f.submit(t, tc, "race-key", "Race")
			results <- code
		}()
	}
	created := 0
	for i := 0; i < workers; i++ {
		switch code := <-results; code {
		case http.StatusCreated:
			created++
		case http.StatusOK, http.StatusConflict:
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if created != 1 {
		t.Fatalf("%d requests created a submission, want exactly 1", created)
	}
	var tasks int64
	if err := tc.DB.Table("tasks").Where("source = 'form' AND title = ?", "Race").Count(&tasks).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if tasks != 1 {
		t.Fatalf("race created %d tasks, want 1", tasks)
	}
}

func TestForms_AuthorizationBoundaries(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "formauth")
	other := buildFormFixture(t, tc, "formauth2", false)
	_, submissionID, _ := f.submit(t, tc, "auth-1", "Secret request")

	type probe struct {
		name, method, path string
		body               any
	}
	manage := []probe{
		{"list forms", http.MethodGet, f.formURL, nil},
		{"create form", http.MethodPost, f.formURL, map[string]any{"name": "x", "slug": "authz-x-" + f.slug}},
		{"list submissions", http.MethodGet, f.formURL + "/" + itoa(f.formID) + "/submissions", nil},
		{"get task config", http.MethodGet, f.formURL + "/" + itoa(f.formID) + "/task-config", nil},
		{"get submission", http.MethodGet, "/api/v1/workspaces/" + itoa(f.owner.workspaceID) + "/submissions/" + itoa(submissionID), nil},
		{"submission tasks", http.MethodGet, "/api/v1/submissions/" + itoa(submissionID) + "/tasks", nil},
		{"submission task status", http.MethodGet, "/api/v1/submissions/" + itoa(submissionID) + "/task-status", nil},
		{"retry task creation", http.MethodPost, "/api/v1/submissions/" + itoa(submissionID) + "/retry-task-creation", nil},
		{"get template", http.MethodGet, "/api/v1/task-templates/" + itoa(f.templateID), nil},
		{"list template versions", http.MethodGet, "/api/v1/task-templates/" + itoa(f.templateID) + "/versions", nil},
		{"apply template", http.MethodPost, "/api/v1/task-templates/" + itoa(f.templateID) + "/apply",
			map[string]any{"list_id": f.listID, "status_id": f.statusID, "title": "intruder"}},
		{"archive template", http.MethodDelete, "/api/v1/task-templates/" + itoa(f.templateID), nil},
	}
	for _, p := range manage {
		for who, token := range map[string]string{"outsider": f.outsider, "other workspace owner": other.owner.token} {
			rec := request(t, tc.Router, p.method, p.path, token, p.body)
			if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
				t.Errorf("%s as %s: status %d, want 403/404: %s", p.name, who, rec.Code, rec.Body.String())
			}
		}
	}

	writes := []probe{
		{"create form", http.MethodPost, f.formURL, map[string]any{"name": "g", "slug": "guest-" + f.slug}},
		{"publish form", http.MethodPost, f.formURL + "/" + itoa(f.formID) + "/publish", nil},
		{"apply template", http.MethodPost, "/api/v1/task-templates/" + itoa(f.templateID) + "/apply",
			map[string]any{"list_id": f.listID, "status_id": f.statusID, "title": "guest"}},
		{"retry task creation", http.MethodPost, "/api/v1/submissions/" + itoa(submissionID) + "/retry-task-creation", nil},
		{"archive template", http.MethodDelete, "/api/v1/task-templates/" + itoa(f.templateID), nil},
	}
	for _, p := range writes {
		rec := request(t, tc.Router, p.method, p.path, f.guest, p.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s as guest: status %d, want 403: %s", p.name, rec.Code, rec.Body.String())
		}
	}
}

func TestForms_UnpublishedAndArchivedFormsAreNotSubmittable(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "formstate")
	rec := request(t, tc.Router, http.MethodPost, f.formURL+"/"+itoa(f.formID)+"/archive", f.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body.String())
	}
	if code, _, _ := f.submit(t, tc, "archived", "x"); code != http.StatusNotFound && code != http.StatusBadRequest && code != http.StatusConflict {
		t.Fatalf("archived form accepted a submission: %d", code)
	}
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/public/forms/"+f.slug, "", nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("archived form is still public: %s", rec.Body.String())
	}
}

func TestTaskTemplates_ArchivedTemplateCannotBeApplied(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "tplarch")
	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/task-templates/"+itoa(f.templateID), f.owner.token, nil)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("archive template: %d %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/task-templates/"+itoa(f.templateID)+"/apply", f.owner.token,
		map[string]any{"list_id": f.listID, "status_id": f.statusID, "title": "late"})
	if rec.Code == http.StatusCreated {
		t.Fatalf("archived template was applied: %s", rec.Body.String())
	}
}
