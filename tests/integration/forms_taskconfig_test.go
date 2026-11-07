package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fcTemplateConfig(priority string, statusID uint, description string) map[string]any {
	task := map[string]any{"priority": priority}
	if statusID != 0 {
		task["status_id"] = statusID
	}
	if description != "" {
		task["description"] = description
	}
	return map[string]any{"task": task, "tags": []uint{}, "subtasks": []any{}, "custom_fields": []any{}}
}

func fcCreateTemplate(t *testing.T, tc *TestContext, env fcEnv, name string, config map[string]any) uint {
	t.Helper()
	return mustCreate(t, tc, env.owner.token, "/api/v1/workspaces/"+itoa(env.owner.workspaceID)+"/task-templates",
		map[string]any{"name": name, "configuration": config})
}

func fcCreateVersion(t *testing.T, tc *TestContext, env fcEnv, templateID uint, config map[string]any) uint {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/task-templates/"+itoa(templateID)+"/versions",
		env.owner.token, map[string]any{"configuration": config})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create template version: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func fcTemplateVersions(t *testing.T, tc *TestContext, env fcEnv, templateID uint) []uint {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/task-templates/"+itoa(templateID)+"/versions", env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list template versions: %d: %s", rec.Code, rec.Body.String())
	}
	var ids []uint
	for _, item := range responseArray(t, rec) {
		ids = append(ids, uint(item.(map[string]any)["id"].(float64)))
	}
	return ids
}

func fcTitleMapping(questionID uint) map[string]any {
	return map[string]any{"question_id": questionID, "target": map[string]any{"type": "task_field", "field": "title"}}
}

func TestFormTaskConfig_PriorityScaleAcceptsCriticalAndRejectsUnknownNames(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcprio")
	_, formURL := env.newForm(t, tc, "Priorities")
	titleID, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "Title", nil)
	selectID := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{
		"type": "single_choice", "title": "How bad", "options": []any{
			map[string]any{"label": "Meh", "value": "meh"},
			map[string]any{"label": "Awful", "value": "awful"},
		},
	})
	templatesURL := "/api/v1/workspaces/" + itoa(env.owner.workspaceID) + "/task-templates"

	for _, name := range []string{"low", "normal", "high", "urgent", "critical"} {
		rec := request(t, tc.Router, http.MethodPost, templatesURL, env.owner.token,
			map[string]any{"name": "Prio " + name, "configuration": fcTemplateConfig(name, 0, "")})
		if rec.Code != http.StatusCreated {
			t.Fatalf("template priority %q: %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	for _, name := range []string{"blocker", "CRITICAL", "5", "medium"} {
		rec := request(t, tc.Router, http.MethodPost, templatesURL, env.owner.token,
			map[string]any{"name": "Bad " + name, "configuration": fcTemplateConfig(name, 0, "")})
		fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_TEMPLATE", "")
	}

	put := func(defaults, transform map[string]any) *httptest.ResponseRecorder {
		mappings := []any{fcTitleMapping(titleID)}
		if transform != nil {
			mappings = append(mappings, map[string]any{
				"question_id": selectID, "target": map[string]any{"type": "task_field", "field": "priority"},
				"transform": map[string]any{"type": "enum", "values": transform},
			})
		}
		config := env.taskConfig(mappings)
		config["task_defaults"] = defaults
		return request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	}
	if rec := put(map[string]any{"priority": "critical", "status_id": env.statusID}, nil); rec.Code != http.StatusOK {
		t.Fatalf("critical default: %d: %s", rec.Code, rec.Body.String())
	}
	rec := put(map[string]any{"priority": "blocker", "status_id": env.statusID}, nil)
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "task_defaults.priority")

	defaults := map[string]any{"priority": "low", "status_id": env.statusID}
	if rec := put(defaults, map[string]any{"meh": "normal", "awful": "critical"}); rec.Code != http.StatusOK {
		t.Fatalf("critical in a select mapping: %d: %s", rec.Code, rec.Body.String())
	}
	rec = put(defaults, map[string]any{"meh": "normal", "awful": "5"})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "mappings[1].transform.values")

	rec = request(t, tc.Router, http.MethodPost, formURL+"/publish", env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
	slug := dataObject(t, request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil))["slug"].(string)
	for answer, want := range map[string]float64{"awful": 5, "meh": 2} {
		rec = fcSubmit(t, tc, slug, "prio-"+answer, map[string]any{itoa(titleID): "Prio " + answer, itoa(selectID): answer})
		if rec.Code != http.StatusCreated {
			t.Fatalf("submit %s: %d: %s", answer, rec.Code, rec.Body.String())
		}
		tasks := responseArray(t, request(t, tc.Router, http.MethodGet,
			"/api/v1/submissions/"+itoa(responseID(t, rec))+"/tasks", env.owner.token, nil))
		if got := tasks[0].(map[string]any)["priority"]; got != want {
			t.Fatalf("answer %q produced priority %v, want %v", answer, got, want)
		}
	}

	templateID := fcCreateTemplate(t, tc, env, "Applied critical", fcTemplateConfig("critical", env.statusID, ""))
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/task-templates/"+itoa(templateID)+"/apply",
		env.owner.token, map[string]any{"list_id": env.listID, "title": "Apply me"})
	if rec.Code != http.StatusCreated || dataObject(t, rec)["priority"] != float64(5) {
		t.Fatalf("apply critical template: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormTaskConfig_TemplateVersionOmittedUsesLatestAtSaveTime(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcver")
	_, formURL := env.newForm(t, tc, "Versions")
	titleID, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "Title", nil)

	templateID := fcCreateTemplate(t, tc, env, "Versioned", fcTemplateConfig("low", env.statusID, "v1"))
	versionTwo := fcCreateVersion(t, tc, env, templateID, fcTemplateConfig("urgent", env.statusID, "v2"))
	versions := fcTemplateVersions(t, tc, env, templateID)
	if len(versions) != 2 || versions[1] != versionTwo {
		t.Fatalf("template versions = %v", versions)
	}

	config := env.taskConfig([]any{fcTitleMapping(titleID)})
	config["template"] = map[string]any{"template_id": templateID}
	rec := request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	if rec.Code != http.StatusOK {
		t.Fatalf("save config without version_id: %d: %s", rec.Code, rec.Body.String())
	}
	saved := dataObject(t, rec)["template"].(map[string]any)
	if saved["version_id"] != float64(versionTwo) {
		t.Fatalf("saved version = %v, want latest %d: %s", saved["version_id"], versionTwo, rec.Body.String())
	}

	versionThree := fcCreateVersion(t, tc, env, templateID, fcTemplateConfig("high", env.statusID, "v3"))
	rec = request(t, tc.Router, http.MethodGet, formURL+"/task-config", env.owner.token, nil)
	if got := dataObject(t, rec)["template"].(map[string]any)["version_id"]; got != float64(versionTwo) {
		t.Fatalf("a later template version must not change the saved config: %v", got)
	}

	rec = request(t, tc.Router, http.MethodPost, formURL+"/publish", env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
	var snapshot string
	if err := tc.DB.Raw("SELECT configuration::text FROM form_task_configs WHERE form_id = ? ORDER BY id DESC LIMIT 1",
		dataObject(t, rec)["id"]).Scan(&snapshot).Error; err != nil {
		t.Fatalf("read published snapshot: %v", err)
	}
	var published struct {
		Template struct {
			VersionID uint `json:"version_id"`
		} `json:"template"`
	}
	if err := json.Unmarshal([]byte(snapshot), &published); err != nil || published.Template.VersionID != versionTwo {
		t.Fatalf("published snapshot = %s (%v), want template version %d", snapshot, err, versionTwo)
	}

	slug := dataObject(t, request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil))["slug"].(string)
	rec = fcSubmit(t, tc, slug, "ver-1", map[string]any{itoa(titleID): "Uses v2"})
	tasks := responseArray(t, request(t, tc.Router, http.MethodGet,
		"/api/v1/submissions/"+itoa(responseID(t, rec))+"/tasks", env.owner.token, nil))
	if tasks[0].(map[string]any)["priority"] != float64(4) || tasks[0].(map[string]any)["description"] != "v2" {
		t.Fatalf("task must use the snapshotted version 2, got %v", tasks[0])
	}

	config["template"] = map[string]any{"template_id": templateID, "version_id": versionThree}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	if rec.Code != http.StatusOK || dataObject(t, rec)["template"].(map[string]any)["version_id"] != float64(versionThree) {
		t.Fatalf("explicit version must be kept: %d: %s", rec.Code, rec.Body.String())
	}

	otherTemplate := fcCreateTemplate(t, tc, env, "Other", fcTemplateConfig("low", env.statusID, ""))
	otherVersions := fcTemplateVersions(t, tc, env, otherTemplate)
	config["template"] = map[string]any{"template_id": templateID, "version_id": otherVersions[0]}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_TASK_TEMPLATE_INVALID", "template.version_id")
	config["template"] = map[string]any{"template_id": 999999}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_TASK_TEMPLATE_INVALID", "template.template_id")

	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/task-templates/"+itoa(templateID), env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive template: %d: %s", rec.Code, rec.Body.String())
	}
	config["template"] = map[string]any{"template_id": templateID}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_TASK_TEMPLATE_INVALID", "template.template_id")
}

func TestFormTaskConfig_StatusMustBelongToTargetList(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcstatus")
	_, formURL := env.newForm(t, tc, "Statuses")
	titleID, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "Title", nil)
	selectID := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{
		"type": "dropdown", "title": "Stage", "options": []any{map[string]any{"label": "One", "value": "one"}},
	})
	otherListID := mustCreate(t, tc, env.owner.token, "/api/v1/spaces/"+itoa(env.owner.spaceID)+"/lists", map[string]any{"name": "Other"})
	foreignStatus := mustCreate(t, tc, env.owner.token, "/api/v1/lists/"+itoa(otherListID)+"/statuses",
		map[string]any{"name": "Elsewhere", "color": "#445566", "type": 1, "list_id": otherListID})

	const code = "FORM_TASK_STATUS_NOT_IN_LIST"
	rec := request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, func() map[string]any {
		config := env.taskConfig([]any{fcTitleMapping(titleID)})
		config["task_defaults"] = map[string]any{"status_id": foreignStatus}
		return config
	}())
	fcExpectError(t, rec, http.StatusBadRequest, code, "task_defaults.status_id")

	templateID := fcCreateTemplate(t, tc, env, "Foreign status", fcTemplateConfig("normal", foreignStatus, ""))
	config := env.taskConfig([]any{fcTitleMapping(titleID)})
	config["template"] = map[string]any{"template_id": templateID}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	fcExpectError(t, rec, http.StatusBadRequest, code, "template.template_id")

	config = env.taskConfig([]any{fcTitleMapping(titleID), map[string]any{
		"question_id": selectID, "target": map[string]any{"type": "task_field", "field": "status_id"},
		"transform": map[string]any{"type": "enum", "values": map[string]any{"one": itoa(foreignStatus)}},
	}})
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	fcExpectError(t, rec, http.StatusBadRequest, code, "mappings[1].transform.values")

	config = env.taskConfig([]any{fcTitleMapping(titleID)})
	config["target"] = map[string]any{"workspace_id": env.owner.workspaceID, "list_id": otherListID}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	fcExpectError(t, rec, http.StatusBadRequest, code, "task_defaults.status_id")

	config = env.taskConfig([]any{fcTitleMapping(titleID)})
	config["task_defaults"] = map[string]any{"priority": "normal"}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_FORM_TASK_CONFIG", "task_defaults.status_id")

	matching := mustCreate(t, tc, env.owner.token, "/api/v1/lists/"+itoa(otherListID)+"/statuses",
		map[string]any{"name": "Here", "color": "#778899", "type": 1, "list_id": otherListID})
	config = env.taskConfig([]any{fcTitleMapping(titleID)})
	config["target"] = map[string]any{"workspace_id": env.owner.workspaceID, "list_id": otherListID}
	config["task_defaults"] = map[string]any{"status_id": matching}
	config["template"] = map[string]any{"template_id": templateID}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
	if rec.Code != http.StatusOK {
		t.Fatalf("status and template status of the target list must be accepted: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFormTaskConfig_PrecedenceMappingsBeatTemplateBeatDefaults(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcprec")
	secondStatus := mustCreate(t, tc, env.owner.token, "/api/v1/lists/"+itoa(env.listID)+"/statuses",
		map[string]any{"name": "Doing", "color": "#abcdef", "type": 1, "list_id": env.listID})
	templateID := fcCreateTemplate(t, tc, env, "Precedence", fcTemplateConfig("high", secondStatus, "Template description"))

	build := func(label string, withTemplate bool, transform map[string]any) (string, uint, uint) {
		_, formURL := env.newForm(t, tc, label)
		titleID, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "Title", nil)
		descriptionID, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "Description", nil)
		selectID := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{
			"type": "single_choice", "title": "Severity", "options": []any{
				map[string]any{"label": "Minor", "value": "minor"}, map[string]any{"label": "Major", "value": "major"},
			},
		})
		mappings := []any{fcTitleMapping(titleID), map[string]any{
			"question_id": descriptionID, "target": map[string]any{"type": "task_field", "field": "description"},
		}}
		if transform != nil {
			mappings = append(mappings, map[string]any{
				"question_id": selectID, "target": map[string]any{"type": "task_field", "field": "priority"},
				"transform": map[string]any{"type": "enum", "values": transform},
			})
		}
		config := env.taskConfig(mappings)
		config["task_defaults"] = map[string]any{"priority": "urgent", "status_id": env.statusID}
		if withTemplate {
			config["template"] = map[string]any{"template_id": templateID}
		}
		rec := request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, config)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: configure: %d: %s", label, rec.Code, rec.Body.String())
		}
		rec = request(t, tc.Router, http.MethodPost, formURL+"/publish", env.owner.token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: publish: %d: %s", label, rec.Code, rec.Body.String())
		}
		slug := dataObject(t, request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil))["slug"].(string)
		return slug, titleID, selectID
	}
	submit := func(slug, key string, answers map[string]any) map[string]any {
		rec := fcSubmit(t, tc, slug, key, answers)
		if rec.Code != http.StatusCreated {
			t.Fatalf("submit %s: %d: %s", key, rec.Code, rec.Body.String())
		}
		tasks := responseArray(t, request(t, tc.Router, http.MethodGet,
			"/api/v1/submissions/"+itoa(responseID(t, rec))+"/tasks", env.owner.token, nil))
		return tasks[0].(map[string]any)
	}

	transform := map[string]any{"minor": "low", "major": "critical"}
	slug, titleID, selectID := build("with template", true, transform)
	task := submit(slug, "a", map[string]any{itoa(titleID): "Mapped wins", itoa(selectID): "major"})
	if task["priority"] != float64(5) || task["status_id"] != float64(secondStatus) ||
		task["description"] != "Template description" || task["title"] != "Mapped wins" {
		t.Fatalf("mapping > template: %v", task)
	}
	task = submit(slug, "b", map[string]any{itoa(titleID): "Unanswered mapping"})
	if task["priority"] != float64(3) || task["status_id"] != float64(secondStatus) {
		t.Fatalf("template must beat task_defaults when no answer is mapped: %v", task)
	}

	slug, titleID, selectID = build("without template", false, transform)
	task = submit(slug, "c", map[string]any{itoa(titleID): "Defaults only"})
	if task["priority"] != float64(4) || task["status_id"] != float64(env.statusID) || task["description"] != "" {
		t.Fatalf("task_defaults without a template: %v", task)
	}
	task = submit(slug, "d", map[string]any{itoa(titleID): "Minor", itoa(selectID): "minor"})
	if task["priority"] != float64(1) {
		t.Fatalf("mapping > task_defaults: %v", task)
	}
}
