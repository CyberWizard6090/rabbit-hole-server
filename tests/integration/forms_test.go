package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestForms_PublishSubmissionAndIdempotency(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := mustCreate(t, tc, tc.Token, "/api/v1/workspaces", map[string]any{"name": "Forms workspace"})
	baseURL := "/api/v1/workspaces/" + itoa(workspaceID) + "/forms"
	slug := fmt.Sprintf("form-%d", time.Now().UnixNano())

	formID := mustCreate(t, tc, tc.Token, baseURL, map[string]any{
		"name": "Bug report", "description": "Report a problem", "slug": slug,
	})
	questionID := mustCreate(t, tc, tc.Token, baseURL+"/"+itoa(formID)+"/questions", map[string]any{
		"type": "short_text", "title": "Summary", "required": true, "position": 0,
	})

	rec := request(t, tc.Router, http.MethodPost, baseURL+"/"+itoa(formID)+"/publish", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish form: %d: %s", rec.Code, rec.Body.String())
	}
	firstVersionID := uint(dataObject(t, rec)["current_version_id"].(float64))

	publicURL := "/api/v1/public/forms/" + slug
	rec = request(t, tc.Router, http.MethodGet, publicURL, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get public form: %d: %s", rec.Code, rec.Body.String())
	}
	if _, leaksWorkspace := dataObject(t, rec)["workspace_id"]; leaksWorkspace {
		t.Fatalf("public form leaked workspace metadata: %s", rec.Body.String())
	}

	submissionURL := publicURL + "/submissions"
	body := map[string]any{"answers": map[string]any{itoa(questionID): "Cannot sign in"}}
	rec = requestWithHeader(t, tc, http.MethodPost, submissionURL, "", "Idempotency-Key", "first-submit", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit form: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	submission := dataObject(t, rec)
	if uint(submission["form_version_id"].(float64)) != firstVersionID {
		t.Fatalf("submission version = %v, want %d", submission["form_version_id"], firstVersionID)
	}

	rec = request(t, tc.Router, http.MethodDelete,
		baseURL+"/"+itoa(formID)+"/questions/"+itoa(questionID), tc.Token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete submitted question: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodDelete, baseURL+"/"+itoa(formID), tc.Token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete submitted form: %d: %s", rec.Code, rec.Body.String())
	}

	rec = requestWithHeader(t, tc, http.MethodPost, submissionURL, "", "Idempotency-Key", "first-submit", body)
	if rec.Code != http.StatusOK || responseID(t, rec) != submissionID {
		t.Fatalf("idempotent replay: %d: %s", rec.Code, rec.Body.String())
	}
	rec = requestWithHeader(t, tc, http.MethodPost, submissionURL, "", "Idempotency-Key", "first-submit",
		map[string]any{"answers": map[string]any{itoa(questionID): "Different answer"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("reused idempotency key with changed request: %d: %s", rec.Code, rec.Body.String())
	}
	rec = requestWithHeader(t, tc, http.MethodPost, submissionURL, "", "Idempotency-Key", "missing-required",
		map[string]any{"answers": map[string]any{}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing required answer: %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPatch,
		baseURL+"/"+itoa(formID)+"/questions/"+itoa(questionID), tc.Token,
		map[string]any{"type": "short_text", "title": "Updated summary", "position": 0})
	if rec.Code != http.StatusOK {
		t.Fatalf("update question: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, baseURL+"/"+itoa(formID)+"/publish", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish updated form: %d: %s", rec.Code, rec.Body.String())
	}
	if secondVersionID := uint(dataObject(t, rec)["current_version_id"].(float64)); secondVersionID == firstVersionID {
		t.Fatalf("published version id did not change: %d", secondVersionID)
	}

	rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/workspaces/"+itoa(workspaceID)+"/submissions/"+itoa(submissionID), tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get historical submission: %d: %s", rec.Code, rec.Body.String())
	}
	if got := uint(dataObject(t, rec)["form_version_id"].(float64)); got != firstVersionID {
		t.Fatalf("historical submission version = %d, want %d", got, firstVersionID)
	}
}

func TestForms_SubmissionCreatesMappedTaskAndRetriesSafely(t *testing.T) {
	tc := newTestContext(t)
	_, token := newUser(t, tc, fmt.Sprintf("form-task-%d@example.test", time.Now().UnixNano()))
	workspaceID := mustCreate(t, tc, token, "/api/v1/workspaces", map[string]any{"name": "Task workspace"})
	spaceID := mustCreate(t, tc, token, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces",
		map[string]any{"name": "Operations"})
	listID := mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(spaceID)+"/lists", map[string]any{"name": "Inbox"})
	statusID := mustCreate(t, tc, token, "/api/v1/lists/"+itoa(listID)+"/statuses", map[string]any{
		"name": "Todo", "color": "#123456", "type": 1, "list_id": listID,
	})
	slug := fmt.Sprintf("task-form-%d", time.Now().UnixNano())
	formURL := "/api/v1/workspaces/" + itoa(workspaceID) + "/forms"
	formID := mustCreate(t, tc, token, formURL, map[string]any{
		"name": "Task request", "slug": slug,
	})
	questionID := mustCreate(t, tc, token, formURL+"/"+itoa(formID)+"/questions", map[string]any{
		"type": "short_text", "title": "Task title", "required": true, "position": 0,
	})
	customFieldID := mustCreate(t, tc, token, "/api/v1/workspaces/"+itoa(workspaceID)+"/custom-fields",
		map[string]any{"name": "Department", "type": "text"})
	departmentQuestionID := mustCreate(t, tc, token, formURL+"/"+itoa(formID)+"/questions", map[string]any{
		"type": "short_text", "title": "Department", "required": true, "position": 1,
	})
	templateConfig := map[string]any{
		"task": map[string]any{
			"title": nil, "description": "Template description", "priority": "high",
			"status_id": statusID, "assignee_id": nil,
		},
		"custom_fields": []any{map[string]any{"field_id": customFieldID, "value": "Template department"}},
		"tags":          []uint{}, "subtasks": []any{},
	}
	templateID := mustCreate(t, tc, token, "/api/v1/workspaces/"+itoa(workspaceID)+"/task-templates",
		map[string]any{"name": "Incoming report", "configuration": templateConfig})
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/task-templates/"+itoa(templateID)+"/versions",
		token, map[string]any{"configuration": templateConfig})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task template version: %d: %s", rec.Code, rec.Body.String())
	}
	templateVersionID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPut,
		formURL+"/"+itoa(formID)+"/task-config", token, map[string]any{
			"enabled":  true,
			"target":   map[string]any{"workspace_id": workspaceID, "list_id": listID},
			"template": map[string]any{"template_id": templateID, "version_id": templateVersionID},
			"task_defaults": map[string]any{
				"priority": "normal", "status_id": statusID,
			},
			"mappings": []any{
				map[string]any{
					"question_id": questionID,
					"target":      map[string]any{"type": "task_field", "field": "title"},
				},
				map[string]any{
					"question_id": departmentQuestionID,
					"target":      map[string]any{"type": "custom_field", "field_id": customFieldID},
				},
			},
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("configure form task creation: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, formURL+"/"+itoa(formID)+"/publish", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish task form: %d: %s", rec.Code, rec.Body.String())
	}

	body := map[string]any{"answers": map[string]any{
		itoa(questionID):           "Mapped task from submission",
		itoa(departmentQuestionID): "Submission department",
	}}
	rec = requestWithHeader(t, tc, http.MethodPost, "/api/v1/public/forms/"+slug+"/submissions", "",
		"Idempotency-Key", "create-task-once", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit task form: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	if got := dataObject(t, rec)["task_creation_status"]; got != "succeeded" {
		t.Fatalf("task creation status = %v, want succeeded; body: %s", got, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/submissions/"+itoa(submissionID)+"/tasks", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks for submission: %d: %s", rec.Code, rec.Body.String())
	}
	tasks := responseArray(t, rec)
	if len(tasks) != 1 || tasks[0].(map[string]any)["title"] != "Mapped task from submission" {
		t.Fatalf("submission tasks = %v", tasks)
	}
	taskID := uint(tasks[0].(map[string]any)["id"].(float64))
	if tasks[0].(map[string]any)["priority"] != float64(3) ||
		tasks[0].(map[string]any)["description"] != "Template description" {
		t.Fatalf("task did not merge template values with submission mapping: %v", tasks[0])
	}
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/custom-fields", token, nil)
	if rec.Code != http.StatusOK || dataObject(t, rec)[itoa(customFieldID)] != "Submission department" {
		t.Fatalf("custom field mapping = %d: %s", rec.Code, rec.Body.String())
	}
	var relationCount, outboxCount int64
	if err := tc.DB.Table("form_submission_tasks").Where("submission_id = ?", submissionID).
		Count(&relationCount).Error; err != nil {
		t.Fatalf("count submission-task relations: %v", err)
	}
	if err := tc.DB.Table("outbox_events").
		Where("aggregate_id = ? AND event_type = 'form.submission.task_created'", submissionID).
		Count(&outboxCount).Error; err != nil {
		t.Fatalf("count task outbox events: %v", err)
	}
	if relationCount != 1 || outboxCount != 1 {
		t.Fatalf("relation/outbox counts = %d/%d, want 1/1", relationCount, outboxCount)
	}

	rec = requestWithHeader(t, tc, http.MethodPost, "/api/v1/public/forms/"+slug+"/submissions", "",
		"Idempotency-Key", "create-task-once", body)
	if rec.Code != http.StatusOK || responseID(t, rec) != submissionID {
		t.Fatalf("replayed task submission: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/submissions/"+itoa(submissionID)+"/tasks", token, nil)
	tasks = responseArray(t, rec)
	if rec.Code != http.StatusOK || len(tasks) != 1 ||
		uint(tasks[0].(map[string]any)["id"].(float64)) != taskID {
		t.Fatalf("task replay created a duplicate: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/task-templates/"+itoa(templateID)+"/apply",
		token, map[string]any{"list_id": listID, "status_id": statusID, "title": "Applied task"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply task template: %d: %s", rec.Code, rec.Body.String())
	}
	applied := dataObject(t, rec)
	if applied["title"] != "Applied task" || applied["priority"] != float64(3) {
		t.Fatalf("applied task values = %v", applied)
	}
	appliedTaskID := uint(applied["id"].(float64))
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(appliedTaskID)+"/custom-fields", token, nil)
	if rec.Code != http.StatusOK || dataObject(t, rec)[itoa(customFieldID)] != "Template department" {
		t.Fatalf("applied template custom field = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestForms_TaskCreationFailureKeepsSubmissionAndRetryDoesNotDuplicate(t *testing.T) {
	tc := newTestContext(t)
	_, token := newUser(t, tc, fmt.Sprintf("form-task-failure-%d@example.test", time.Now().UnixNano()))
	workspaceID := mustCreate(t, tc, token, "/api/v1/workspaces", map[string]any{"name": "Failure workspace"})
	spaceID := mustCreate(t, tc, token, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces",
		map[string]any{"name": "Operations"})
	listID := mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(spaceID)+"/lists", map[string]any{"name": "Inbox"})
	mustCreate(t, tc, token, "/api/v1/lists/"+itoa(listID)+"/statuses", map[string]any{
		"name": "Todo", "color": "#123456", "type": 1, "list_id": listID,
	})
	targetStatusID := mustCreate(t, tc, token, "/api/v1/lists/"+itoa(listID)+"/statuses", map[string]any{
		"name": "Later", "color": "#654321", "type": 1, "list_id": listID,
	})
	formURL := "/api/v1/workspaces/" + itoa(workspaceID) + "/forms"
	slug := fmt.Sprintf("failed-task-form-%d", time.Now().UnixNano())
	formID := mustCreate(t, tc, token, formURL, map[string]any{"name": "Request", "slug": slug})
	questionID := mustCreate(t, tc, token, formURL+"/"+itoa(formID)+"/questions", map[string]any{
		"type": "short_text", "title": "Title", "required": true, "position": 0,
	})
	rec := request(t, tc.Router, http.MethodPut,
		formURL+"/"+itoa(formID)+"/task-config", token, map[string]any{
			"enabled":       true,
			"target":        map[string]any{"workspace_id": workspaceID, "list_id": listID},
			"task_defaults": map[string]any{"priority": "normal", "status_id": targetStatusID},
			"mappings": []any{map[string]any{
				"question_id": questionID,
				"target":      map[string]any{"type": "task_field", "field": "title"},
			}},
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("configure failure form: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, formURL+"/"+itoa(formID)+"/publish", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish failure form: %d: %s", rec.Code, rec.Body.String())
	}

	if err := tc.DB.Exec("UPDATE task_statuses SET deleted_at = now() WHERE id = ?", targetStatusID).Error; err != nil {
		t.Fatalf("invalidate task target: %v", err)
	}
	rec = requestWithHeader(t, tc, http.MethodPost, "/api/v1/public/forms/"+slug+"/submissions", "",
		"Idempotency-Key", "failure-is-persisted", map[string]any{
			"answers": map[string]any{itoa(questionID): "Keep my submission"},
		})
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed task must not reject submission: %d: %s", rec.Code, rec.Body.String())
	}
	submissionID := responseID(t, rec)
	if got := dataObject(t, rec)["task_creation_status"]; got != "failed" {
		t.Fatalf("task status = %v, want failed; body: %s", got, rec.Body.String())
	}
	var taskCount int64
	if err := tc.DB.Table("tasks").Where("source_reference_id = ?", submissionID).Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks after failed transaction: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("failed task transaction persisted %d tasks", taskCount)
	}

	rec = request(t, tc.Router, http.MethodPost,
		"/api/v1/submissions/"+itoa(submissionID)+"/retry-task-creation", token, nil)
	if rec.Code != http.StatusOK || dataObject(t, rec)["status"] != "failed" {
		t.Fatalf("retry failed task creation: %d: %s", rec.Code, rec.Body.String())
	}
	if err := tc.DB.Table("tasks").Where("source_reference_id = ?", submissionID).Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks after retry: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("failed retry persisted %d duplicate tasks", taskCount)
	}
}

func responseArray(t *testing.T, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	body := decodeJSON(t, rec)
	values, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("response.data is not an array: %v", body["data"])
	}
	return values
}
