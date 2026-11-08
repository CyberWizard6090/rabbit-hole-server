package integration

import (
	"net/http"
	"testing"
	"time"
)

func ttDueDate(t *testing.T, tc *TestContext, taskID uint) string {
	t.Helper()
	var due *string
	if err := tc.DB.Table("tasks").Select("to_char(due_date AT TIME ZONE 'UTC', 'YYYY-MM-DD')").
		Where("id = ?", taskID).Row().Scan(&due); err != nil {
		t.Fatalf("read due date: %v", err)
	}
	if due == nil {
		return ""
	}
	return *due
}

func ttConfigWithDue(env fcEnv, task map[string]any) map[string]any {
	task["priority"] = "normal"
	task["status_id"] = env.statusID
	return map[string]any{"task": task, "tags": []uint{}, "subtasks": []any{}, "custom_fields": []any{}}
}

func TestTemplateApply_HonorsAbsoluteAndRelativeDueDate(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "ttdue")
	apply := func(templateID uint) uint {
		t.Helper()
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/task-templates/"+itoa(templateID)+"/apply",
			env.owner.token, map[string]any{"list_id": env.listID, "title": "Applied"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("apply: %d: %s", rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}

	absolute := fcCreateTemplate(t, tc, env, "Absolute", ttConfigWithDue(env, map[string]any{"due_date": "2031-02-03"}))
	if got := ttDueDate(t, tc, apply(absolute)); got != "2031-02-03" {
		t.Fatalf("absolute due date = %q, want 2031-02-03", got)
	}

	relative := fcCreateTemplate(t, tc, env, "Relative", ttConfigWithDue(env, map[string]any{"due_in_days": 7}))
	before := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")
	got := ttDueDate(t, tc, apply(relative))
	after := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")
	if got != before && got != after {
		t.Fatalf("relative due date = %q, want today + 7 days (%s)", got, after)
	}

	today := fcCreateTemplate(t, tc, env, "Today", ttConfigWithDue(env, map[string]any{"due_in_days": 0}))
	if got := ttDueDate(t, tc, apply(today)); got != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("due_in_days 0 = %q, want today", got)
	}

	none := fcCreateTemplate(t, tc, env, "None", ttConfigWithDue(env, map[string]any{}))
	if got := ttDueDate(t, tc, apply(none)); got != "" {
		t.Fatalf("template without due date produced %q", got)
	}
}

func TestTemplateConfiguration_RejectsInvalidDueDate(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "ttbad")
	templatesURL := "/api/v1/workspaces/" + itoa(env.owner.workspaceID) + "/task-templates"
	for name, task := range map[string]map[string]any{
		"not a date":      {"due_date": "tomorrow"},
		"wrong layout":    {"due_date": "03.02.2031"},
		"negative days":   {"due_in_days": -1},
		"too many days":   {"due_in_days": 100000},
		"fractional days": {"due_in_days": 1.5},
		"both set":        {"due_date": "2031-02-03", "due_in_days": 3},
	} {
		rec := request(t, tc.Router, http.MethodPost, templatesURL, env.owner.token,
			map[string]any{"name": "Bad " + name, "configuration": ttConfigWithDue(env, task)})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", name, rec.Code, rec.Body.String())
			continue
		}
		fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_TEMPLATE", "")
	}
}

func TestFormTask_HonorsTemplateDueDate(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "ttform")
	for name, task := range map[string]map[string]any{
		"absolute": {"due_date": "2032-04-05"},
		"relative": {"due_in_days": 3},
	} {
		templateID := fcCreateTemplate(t, tc, env, "Form "+name, ttConfigWithDue(env, task))
		_, formURL := env.newForm(t, tc, "Due "+name)
		titleID, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "Title", nil)
		config := env.taskConfig([]any{fcTitleMapping(titleID)})
		config["template"] = map[string]any{"template_id": templateID}
		if rec := request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config); rec.Code != http.StatusOK {
			t.Fatalf("%s task config: %d: %s", name, rec.Code, rec.Body.String())
		}
		if rec := request(t, tc.Router, http.MethodPost, formURL+"/publish", env.owner.token, nil); rec.Code != http.StatusOK {
			t.Fatalf("%s publish: %d: %s", name, rec.Code, rec.Body.String())
		}
		slug := dataObject(t, request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil))["slug"].(string)
		rec := fcSubmit(t, tc, slug, "due-"+name, map[string]any{itoa(titleID): "From form"})
		if rec.Code != http.StatusCreated || dataObject(t, rec)["task_creation_status"] != "succeeded" {
			t.Fatalf("%s submit: %d: %s", name, rec.Code, rec.Body.String())
		}
		tasks := responseArray(t, request(t, tc.Router, http.MethodGet,
			"/api/v1/submissions/"+itoa(responseID(t, rec))+"/tasks", env.owner.token, nil))
		taskID := uint(tasks[0].(map[string]any)["id"].(float64))
		want := "2032-04-05"
		if name == "relative" {
			want = time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")
		}
		if got := ttDueDate(t, tc, taskID); got != want {
			t.Fatalf("%s: form task due date = %q, want %q", name, got, want)
		}
	}
}

func TestTaskTemplatePatch_IsAPartialUpdateAndBumpsUpdatedAt(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "ttpatch")
	templateID := mustCreate(t, tc, env.owner.token,
		"/api/v1/workspaces/"+itoa(env.owner.workspaceID)+"/task-templates", map[string]any{
			"name": "Original", "description": "Keep me", "configuration": fcTemplateConfig("normal", env.statusID, ""),
		})
	url := "/api/v1/task-templates/" + itoa(templateID)
	state := func() (name, description string, updated time.Time) {
		t.Helper()
		row := tc.DB.Table("task_templates").Select("name, description, updated_at").Where("id = ?", templateID).Row()
		if err := row.Scan(&name, &description, &updated); err != nil {
			t.Fatalf("read template: %v", err)
		}
		return
	}
	_, _, first := state()
	time.Sleep(20 * time.Millisecond)

	rec := request(t, tc.Router, http.MethodPatch, url, env.owner.token, map[string]any{"name": "Renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch name only: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data := dataObject(t, rec)
	if data["name"] != "Renamed" || data["description"] != "Keep me" {
		t.Fatalf("response = %v, want renamed with the description kept", data)
	}
	name, description, second := state()
	if name != "Renamed" || description != "Keep me" {
		t.Fatalf("stored name=%q description=%q", name, description)
	}
	if !second.After(first) {
		t.Fatalf("updated_at not bumped: %s -> %s", first, second)
	}
	responseUpdated, err := time.Parse(time.RFC3339Nano, data["updated_at"].(string))
	if err != nil || !responseUpdated.Equal(second) {
		t.Fatalf("response updated_at = %v (%v), want %s", data["updated_at"], err, second)
	}

	time.Sleep(20 * time.Millisecond)
	rec = request(t, tc.Router, http.MethodPatch, url, env.owner.token, map[string]any{"description": ""})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch description only: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	name, description, third := state()
	if name != "Renamed" || description != "" || !third.After(second) {
		t.Fatalf("after clearing description: name=%q description=%q updated=%s", name, description, third)
	}

	rec = request(t, tc.Router, http.MethodPatch, url, env.owner.token, map[string]any{"name": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank name: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
