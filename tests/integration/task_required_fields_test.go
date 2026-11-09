package integration

import (
	"fmt"
	"net/http"
	"testing"
)

func rfCreateField(t *testing.T, tc *TestContext, env fcEnv, body map[string]any) uint {
	t.Helper()
	return mustCreate(t, tc, env.owner.token,
		"/api/v1/workspaces/"+itoa(env.owner.workspaceID)+"/custom-fields", body)
}

func rfRequiredText(t *testing.T, tc *TestContext, env fcEnv, name string) uint {
	t.Helper()
	return rfCreateField(t, tc, env, map[string]any{"name": name, "type": "text", "required": true})
}

func rfTasksURL(env fcEnv) string { return "/api/v1/lists/" + itoa(env.listID) + "/tasks" }

func rfExpectRequiredDetail(t *testing.T, body map[string]any, fieldID uint) {
	t.Helper()
	detail := errorDetail(body, fmt.Sprintf("custom_fields.%d", fieldID))
	if detail == nil || detail["code"] != "REQUIRED" {
		t.Fatalf("error details %v do not name required field %d", body["details"], fieldID)
	}
	params, _ := detail["params"].(map[string]any)
	if params["field_id"] != float64(fieldID) {
		t.Fatalf("detail params = %v, want field_id %d", params, fieldID)
	}
}

func TestTaskCreate_RequiredCustomFieldIsEnforced(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "rfcreate")
	fieldID := rfRequiredText(t, tc, env, "Customer")
	optionalID := rfCreateField(t, tc, env, map[string]any{"name": "Note", "type": "text"})

	rec := request(t, tc.Router, http.MethodPost, rfTasksURL(env), env.owner.token,
		map[string]any{"title": "No value", "status_id": env.statusID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing required value: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	body := fcErrorBody(t, rec)
	if body["code"] != "VALIDATION_FAILED" {
		t.Fatalf("code = %v, want VALIDATION_FAILED", body["code"])
	}
	rfExpectRequiredDetail(t, body, fieldID)

	rec = request(t, tc.Router, http.MethodPost, rfTasksURL(env), env.owner.token, map[string]any{
		"title": "Blank value", "status_id": env.statusID,
		"custom_fields": map[string]any{itoa(fieldID): "   "},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank required value: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	rfExpectRequiredDetail(t, fcErrorBody(t, rec), fieldID)

	var tasks int64
	tc.DB.Table("tasks").Where("list_id = ?", env.listID).Count(&tasks)
	if tasks != 0 {
		t.Fatalf("rejected creations left %d tasks", tasks)
	}

	rec = request(t, tc.Router, http.MethodPost, rfTasksURL(env), env.owner.token, map[string]any{
		"title": "Wrong type", "status_id": env.statusID,
		"custom_fields": map[string]any{itoa(fieldID): 42},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong value type: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, rfTasksURL(env), env.owner.token, map[string]any{
		"title": "With value", "status_id": env.statusID,
		"custom_fields": map[string]any{itoa(fieldID): "ACME", itoa(optionalID): "extra"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with value: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	taskID := responseID(t, rec)
	values := dataObject(t, request(t, tc.Router, http.MethodGet,
		"/api/v1/tasks/"+itoa(taskID)+"/custom-fields", env.owner.token, nil))
	if values[itoa(fieldID)] != "ACME" || values[itoa(optionalID)] != "extra" {
		t.Fatalf("stored values = %v", values)
	}

	rec = request(t, tc.Router, http.MethodPost, rfTasksURL(env), env.owner.token, map[string]any{
		"title": "Unknown field", "status_id": env.statusID,
		"custom_fields": map[string]any{itoa(fieldID): "ACME", "999999": "x"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown custom field: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskCreate_RequiredCustomFieldAppliesToSubtasks(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "rfsub")
	fieldID := rfRequiredText(t, tc, env, "Customer")
	rec := request(t, tc.Router, http.MethodPost, rfTasksURL(env), env.owner.token, map[string]any{
		"title": "Parent", "status_id": env.statusID, "custom_fields": map[string]any{itoa(fieldID): "ACME"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create parent: %d: %s", rec.Code, rec.Body.String())
	}
	parentID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPost, rfTasksURL(env), env.owner.token,
		map[string]any{"title": "Child", "status_id": env.statusID, "parent_id": parentID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("subtask without required value: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	rfExpectRequiredDetail(t, fcErrorBody(t, rec), fieldID)
}

func TestTaskUpdate_RequiredCustomFieldValueCannotBeCleared(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "rfclear")
	fieldID := rfRequiredText(t, tc, env, "Customer")
	rec := request(t, tc.Router, http.MethodPost, rfTasksURL(env), env.owner.token, map[string]any{
		"title": "Task", "status_id": env.statusID, "custom_fields": map[string]any{itoa(fieldID): "ACME"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d: %s", rec.Code, rec.Body.String())
	}
	valueURL := "/api/v1/tasks/" + itoa(responseID(t, rec)) + "/custom-fields/" + itoa(fieldID)

	rec = request(t, tc.Router, http.MethodDelete, valueURL, env.owner.token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete required value: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	rfExpectRequiredDetail(t, fcErrorBody(t, rec), fieldID)

	for name, value := range map[string]any{"empty string": "", "null": nil} {
		rec = request(t, tc.Router, http.MethodPut, valueURL, env.owner.token, map[string]any{"value": value})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", name, rec.Code, rec.Body.String())
		}
		rfExpectRequiredDetail(t, fcErrorBody(t, rec), fieldID)
	}

	var stored int64
	tc.DB.Table("task_custom_field_values").Where("custom_field_id = ?", fieldID).Count(&stored)
	if stored != 1 {
		t.Fatalf("stored values = %d, want the original value kept", stored)
	}
	if rec := request(t, tc.Router, http.MethodPut, valueURL, env.owner.token,
		map[string]any{"value": "Other"}); rec.Code != http.StatusOK {
		t.Fatalf("replace required value: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTemplateApply_RequiredCustomField(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "rfapply")
	fieldID := rfRequiredText(t, tc, env, "Customer")
	applyURL := func(templateID uint) string { return "/api/v1/task-templates/" + itoa(templateID) + "/apply" }

	bare := fcCreateTemplate(t, tc, env, "Bare", fcTemplateConfig("normal", env.statusID, ""))
	rec := request(t, tc.Router, http.MethodPost, applyURL(bare), env.owner.token,
		map[string]any{"list_id": env.listID, "title": "From bare"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("apply without required value: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	rfExpectRequiredDetail(t, fcErrorBody(t, rec), fieldID)
	var tasks int64
	tc.DB.Table("tasks").Where("list_id = ?", env.listID).Count(&tasks)
	if tasks != 0 {
		t.Fatalf("rejected apply left %d tasks", tasks)
	}

	rec = request(t, tc.Router, http.MethodPost, applyURL(bare), env.owner.token, map[string]any{
		"list_id": env.listID, "title": "From bare", "custom_fields": map[string]any{itoa(fieldID): "Supplied"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply with supplied value: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	config := fcTemplateConfig("normal", env.statusID, "")
	config["custom_fields"] = []any{map[string]any{"field_id": fieldID, "value": "FromTemplate"}}
	full := fcCreateTemplate(t, tc, env, "Full", config)
	rec = request(t, tc.Router, http.MethodPost, applyURL(full), env.owner.token,
		map[string]any{"list_id": env.listID, "title": "From full"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply template carrying the value: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	values := dataObject(t, request(t, tc.Router, http.MethodGet,
		"/api/v1/tasks/"+itoa(responseID(t, rec))+"/custom-fields", env.owner.token, nil))
	if values[itoa(fieldID)] != "FromTemplate" {
		t.Fatalf("stored values = %v", values)
	}
}

func TestFormTask_RequiredCustomFieldFailsCreationAndIsRecorded(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "rfform")
	fieldID := rfRequiredText(t, tc, env, "Customer")
	_, formURL := env.newForm(t, tc, "Intake")
	titleID, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "Title", nil)
	customerID, _ := fcAddQuestion(t, tc, env.owner.token, formURL, "Customer", nil)

	publish := func(mappings []any) string {
		t.Helper()
		rec := request(t, tc.Router, http.MethodPut, formURL+"/task-config", env.owner.token, env.taskConfig(mappings))
		if rec.Code != http.StatusOK {
			t.Fatalf("task config: %d: %s", rec.Code, rec.Body.String())
		}
		rec = request(t, tc.Router, http.MethodPost, formURL+"/publish", env.owner.token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
		}
		return dataObject(t, request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil))["slug"].(string)
	}

	slug := publish([]any{fcTitleMapping(titleID)})
	rec := fcSubmit(t, tc, slug, "rf-missing", map[string]any{itoa(titleID): "Needs customer"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("submission must be kept: %d: %s", rec.Code, rec.Body.String())
	}
	data := dataObject(t, rec)
	if data["task_creation_status"] != "failed" {
		t.Fatalf("task_creation_status = %v, want failed: %s", data["task_creation_status"], rec.Body.String())
	}
	var recorded string
	tc.DB.Table("form_submissions").Select("task_creation_error").Where("id = ?", responseID(t, rec)).Scan(&recorded)
	if recorded != "TASK_CREATION_FAILED" {
		t.Fatalf("recorded task_creation_error = %q, want the failure code", recorded)
	}
	var tasks int64
	tc.DB.Table("tasks").Where("source_reference_id = ?", responseID(t, rec)).Count(&tasks)
	if tasks != 0 {
		t.Fatalf("failed creation left %d tasks", tasks)
	}

	slug = publish([]any{fcTitleMapping(titleID), map[string]any{
		"question_id": customerID, "target": map[string]any{"type": "custom_field", "field_id": fieldID},
	}})
	rec = fcSubmit(t, tc, slug, "rf-mapped", map[string]any{itoa(titleID): "Has customer", itoa(customerID): "ACME"})
	if rec.Code != http.StatusCreated || dataObject(t, rec)["task_creation_status"] != "succeeded" {
		t.Fatalf("mapped submission: %d: %s", rec.Code, rec.Body.String())
	}
}
