package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func detailParams(t *testing.T, detail map[string]any) map[string]any {
	t.Helper()
	params, _ := detail["params"].(map[string]any)
	if params == nil {
		t.Fatalf("detail %v has no params", detail)
	}
	return params
}

func TestErrorDetails_TaskReferencesNameTheOffendingField(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	tasksURL := "/api/v1/lists/" + itoa(f.listID) + "/tasks"
	otherList := mustCreate(t, tc, f.owner.token, "/api/v1/spaces/"+itoa(f.owner.spaceID)+"/lists", map[string]any{"name": "other"})
	foreignStatus := mustCreate(t, tc, f.owner.token, "/api/v1/lists/"+itoa(otherList)+"/statuses",
		map[string]any{"name": "Foreign", "color": "#112233", "type": 1, "list_id": otherList})

	rec := request(t, tc.Router, http.MethodPost, tasksURL, f.owner.token, map[string]any{"title": "x", "status_id": foreignStatus})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_REFERENCE", "status_id")
	d := expectDetail(t, fcErrorBody(t, rec), "status_id", "NOT_IN_LIST")
	if detailParams(t, d)["id"] != float64(foreignStatus) {
		t.Fatalf("detail = %v", d)
	}

	rec = request(t, tc.Router, http.MethodPost, tasksURL, f.owner.token,
		map[string]any{"title": "x", "status_id": f.status1, "assignee_ids": []uint{f.memberA, f.outsider}})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_REFERENCE", "assignee_ids")
	d = expectDetail(t, fcErrorBody(t, rec), "assignee_ids", "NOT_A_MEMBER")
	ids, _ := detailParams(t, d)["ids"].([]any)
	if len(ids) != 1 || ids[0] != float64(f.outsider) {
		t.Fatalf("detail = %v", d)
	}

	rec = request(t, tc.Router, http.MethodPost, tasksURL, f.owner.token,
		map[string]any{"title": "x", "status_id": f.status1, "tag_ids": []uint{987654}})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_REFERENCE", "tag_ids")
	expectDetail(t, fcErrorBody(t, rec), "tag_ids", "NOT_IN_SPACE")

	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(f.taskID), f.owner.token, map[string]any{"add_tag_ids": []uint{987654}})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_REFERENCE", "add_tag_ids")

	rec = request(t, tc.Router, http.MethodPost, tasksURL, f.owner.token,
		map[string]any{"title": "x", "status_id": f.status1, "parent_id": 987654})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_REFERENCE", "parent_id")

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(f.taskID)+"/move", f.owner.token,
		map[string]any{"list_id": otherList, "status_id": f.status1})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_REFERENCE", "status_id")

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(f.taskID)+"/move", f.owner.token, map[string]any{"after_id": 987654})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_REFERENCE", "after_id")
}

func TestErrorDetails_MoveWithTwoAnchorsUsesWireNames(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	other := f.newTask(t, "second", nil)
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(f.taskID)+"/move", f.owner.token,
		map[string]any{"after_id": other, "before_id": other})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("two anchors: %d: %s", rec.Code, rec.Body.String())
	}
	d := expectDetail(t, fcErrorBody(t, rec), "after_id", "CONFLICTING_FIELD")
	if detailParams(t, d)["with"] != "before_id" {
		t.Fatalf("params.with = %v, want the JSON name before_id", detailParams(t, d)["with"])
	}
}

func TestErrorDetails_InvalidMentionNamesTheUser(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(f.taskID)+"/comments", f.owner.token,
		map[string]any{"body": "hello", "mention_user_ids": []uint{f.memberA, f.outsider}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mention of an outsider: %d: %s", rec.Code, rec.Body.String())
	}
	body := fcErrorBody(t, rec)
	if body["code"] != "INVALID_MENTION" {
		t.Fatalf("code = %v", body["code"])
	}
	if params, _ := body["params"].(map[string]any); params == nil || params["user_id"] != float64(f.outsider) {
		t.Fatalf("params = %v, want the offending user id %d", body["params"], f.outsider)
	}
	d := expectDetail(t, fcErrorBody(t, rec), "mention_user_ids", "NOT_A_MEMBER")
	if detailParams(t, d)["user_id"] != float64(f.outsider) {
		t.Fatalf("detail = %v", d)
	}
}

func TestErrorDetails_CustomFieldsNameThePath(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	url := "/api/v1/workspaces/" + itoa(f.owner.workspaceID) + "/custom-fields"

	rec := request(t, tc.Router, http.MethodPost, url, f.owner.token, map[string]any{"name": "Pick", "type": "dropdown"})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_CUSTOM_FIELD", "options")
	expectDetail(t, fcErrorBody(t, rec), "options", "INVALID_COUNT")

	rec = request(t, tc.Router, http.MethodPost, url, f.owner.token, map[string]any{"name": "Pick", "type": "bogus"})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_CUSTOM_FIELD", "type")
	expectDetail(t, fcErrorBody(t, rec), "type", "INVALID_CHOICE")

	rec = request(t, tc.Router, http.MethodPost, url, f.owner.token, map[string]any{"name": "N", "type": "number", "config": map[string]any{"min": 5, "max": 1}})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_CUSTOM_FIELD", "config")

	required := mustCreate(t, tc, f.owner.token, url, map[string]any{"name": "Req", "type": "text", "required": true})
	number := mustCreate(t, tc, f.owner.token, url, map[string]any{"name": "Num", "type": "text"})
	valueURL := func(id uint) string { return "/api/v1/tasks/" + itoa(f.taskID) + "/custom-fields/" + itoa(id) }

	rec = request(t, tc.Router, http.MethodPut, valueURL(number), f.owner.token, map[string]any{"value": 12})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_CUSTOM_FIELD", "value")
	d := expectDetail(t, fcErrorBody(t, rec), "value", "INVALID_TYPE")
	if detailParams(t, d)["expected"] != "string" {
		t.Fatalf("detail = %v", d)
	}
	rec = request(t, tc.Router, http.MethodPut, valueURL(required), f.owner.token, map[string]any{"value": ""})
	fcExpectError(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "custom_fields."+itoa(required))
	expectDetail(t, fcErrorBody(t, rec), "custom_fields."+itoa(required), "REQUIRED")
}

func TestErrorDetails_TaskTemplatesNameThePath(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	url := "/api/v1/workspaces/" + itoa(f.owner.workspaceID) + "/task-templates"
	create := func(config map[string]any) *httpResult {
		rec := request(t, tc.Router, http.MethodPost, url, f.owner.token, map[string]any{"name": "T", "configuration": config})
		return &httpResult{code: rec.Code, body: rec.Body.String()}
	}
	body := func(res *httpResult) map[string]any {
		t.Helper()
		return decodeJSONString(t, res.body)["error"].(map[string]any)
	}

	res := create(map[string]any{"task": map[string]any{"priority": "bogus"}, "tags": []uint{}, "subtasks": []any{}, "custom_fields": []any{}})
	if res.code != http.StatusBadRequest || body(res)["code"] != "INVALID_TASK_TEMPLATE" {
		t.Fatalf("priority: %d %s", res.code, res.body)
	}
	expectDetail(t, body(res), "configuration.task.priority", "INVALID_CHOICE")

	res = create(map[string]any{"task": map[string]any{"priority": "normal", "status_id": 987654}, "tags": []uint{}, "subtasks": []any{}, "custom_fields": []any{}})
	expectDetail(t, body(res), "configuration.task.status_id", "NOT_FOUND")

	res = create(map[string]any{"task": map[string]any{"priority": "normal"}, "tags": []uint{987654}, "subtasks": []any{}, "custom_fields": []any{}})
	expectDetail(t, body(res), "configuration.tags[0]", "NOT_FOUND")

	res = create(map[string]any{"task": map[string]any{"priority": "normal"}, "tags": []uint{}, "subtasks": []any{},
		"custom_fields": []any{map[string]any{"field_id": 987654, "value": "x"}}})
	expectDetail(t, body(res), "configuration.custom_fields[0].field_id", "NOT_FOUND")

	field := mustCreate(t, tc, f.owner.token, "/api/v1/workspaces/"+itoa(f.owner.workspaceID)+"/custom-fields",
		map[string]any{"name": "Text", "type": "text"})
	res = create(map[string]any{"task": map[string]any{"priority": "normal"}, "tags": []uint{}, "subtasks": []any{},
		"custom_fields": []any{map[string]any{"field_id": field, "value": 7}}})
	expectDetail(t, body(res), "configuration.custom_fields[0].value", "INVALID_TYPE")

	long := strings.Repeat("n", 300)
	rec := request(t, tc.Router, http.MethodPost, url, f.owner.token, map[string]any{"name": long, "configuration": map[string]any{"task": map[string]any{}}})
	fcExpectError(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "name")

	valid := mustCreate(t, tc, f.owner.token, url, map[string]any{"name": "Valid", "configuration": map[string]any{
		"task": map[string]any{"priority": "normal"}, "tags": []uint{}, "subtasks": []any{}, "custom_fields": []any{},
	}})
	otherList := mustCreate(t, tc, f.owner.token, "/api/v1/spaces/"+itoa(f.owner.spaceID)+"/lists", map[string]any{"name": "other"})
	foreignStatus := mustCreate(t, tc, f.owner.token, "/api/v1/lists/"+itoa(otherList)+"/statuses",
		map[string]any{"name": "Foreign", "color": "#112233", "type": 1, "list_id": otherList})
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/task-templates/"+itoa(valid)+"/apply", f.owner.token,
		map[string]any{"list_id": f.listID, "status_id": foreignStatus})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_TASK_TEMPLATE_APPLICATION", "status_id")
	expectDetail(t, fcErrorBody(t, rec), "status_id", "NOT_IN_LIST")
}

func TestErrorDetails_FormSettingsAndQuestionsNameThePath(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "formdetails")
	_, formURL := env.newForm(t, tc, "Details")

	rec := request(t, tc.Router, http.MethodPatch, formURL, env.owner.token, map[string]any{
		"name": "Details", "slug": env.newSlug("x"), "settings": map[string]any{"redirect_url": "ftp://example.test"},
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_REDIRECT_URL_INVALID", "settings.redirect_url")
	rec = request(t, tc.Router, http.MethodPatch, formURL, env.owner.token, map[string]any{
		"name": "Details", "slug": env.newSlug("x"), "settings": map[string]any{"accent_color": "red"},
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_ACCENT_COLOR_INVALID", "settings.accent_color")
	rec = request(t, tc.Router, http.MethodPatch, formURL, env.owner.token, map[string]any{
		"name": "Details", "slug": env.newSlug("x"), "settings": map[string]any{"notify_on_submit": []uint{987654}},
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_NOTIFY_RECIPIENT_INVALID", "settings.notify_on_submit[0]")
	rec = request(t, tc.Router, http.MethodPatch, formURL, env.owner.token, map[string]any{
		"name": "Details", "slug": env.newSlug("x"), "settings": map[string]any{"max_submissions": 0},
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_MAX_SUBMISSIONS_INVALID", "settings.max_submissions")

	rec = request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token, map[string]any{
		"type": "file", "title": "File", "settings": map[string]any{"max_files": 99},
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_QUESTION_SETTINGS_INVALID", "settings.max_files")

	self := mustCreate(t, tc, env.owner.token, formURL+"/questions", map[string]any{"type": "short_text", "title": "Self"})
	rec = request(t, tc.Router, http.MethodPatch, formURL+"/questions/"+itoa(self), env.owner.token, map[string]any{
		"type": "short_text", "title": "Self", "settings": visibleRule(self, "any", nil),
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_QUESTION_VISIBILITY_CYCLE", "settings.visible_if.question_id")
	rec = request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token, map[string]any{
		"type": "short_text", "title": "Bad operator", "settings": map[string]any{"visible_if": map[string]any{"question_id": self, "operator": "nope"}},
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_QUESTION_VISIBILITY_INVALID", "settings.visible_if.operator")
	rec = request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token, map[string]any{
		"type": "short_text", "title": "Bad value", "settings": visibleRule(self, "equals", []int{1}),
	})
	fcExpectError(t, rec, http.StatusBadRequest, "FORM_QUESTION_VISIBILITY_INVALID", "settings.visible_if.value")
}

func TestNotificationChannel_ConfirmationFailureUsesAChannelNeutralCode(t *testing.T) {
	tc := webhookContext(t, 10)
	f := newNotifyFixture(t, tc)
	rcv := newHookReceiver(t)
	rcv.setStatus(http.StatusBadRequest)
	rec := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "webhook", "destination": rcv.server.URL})
	fcExpectError(t, rec, http.StatusServiceUnavailable, "CONFIRMATION_DELIVERY_FAILED", "")
}

func decodeJSONString(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

func TestFormSubmissionNotification_PayloadNamesTheFormNotATask(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixture(t, tc, "ntfpayload", nil)
	f.mustSetSettings(t, tc, map[string]any{"notify_on_submit": []uint{f.owner.userID}})
	f.publish(t, tc)
	rec := f.submitWith(t, tc, "ntf-payload-1", f.plainAnswers())
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	tc.Notifications.Wait()
	list := request(t, tc.Router, http.MethodGet, "/api/v1/notifications?type=form_submission", f.owner.token, nil)
	entries := responseArray(t, list)
	if len(entries) != 1 {
		t.Fatalf("notifications: %s", list.Body.String())
	}
	payload := entries[0].(map[string]any)["payload"].(map[string]any)
	if _, hasTitle := payload["task_title"]; hasTitle {
		t.Fatalf("a form notification must not carry task_title: %v", payload)
	}
	if payload["form_id"] != float64(f.formID) || payload["form_name"] != "Request" || payload["submission_id"] != float64(submissionID) {
		t.Fatalf("payload = %v", payload)
	}
}
