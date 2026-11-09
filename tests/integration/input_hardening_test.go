package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/app"
)

type hardenEnv struct {
	tc          *TestContext
	owner       workspaceOwner
	listID      uint
	statusID    uint
	taskID      uint
	tagID       uint
	folderID    uint
	commentID   uint
	checklistID uint
	itemID      uint
	formID      uint
	questionID  uint
	fieldID     uint
	templateID  uint
}

func newHardenEnv(t *testing.T) *hardenEnv {
	t.Helper()
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "harden")
	e := &hardenEnv{tc: tc, owner: owner}
	spaceURL := "/api/v1/spaces/" + itoa(owner.spaceID)
	e.listID = mustCreate(t, tc, owner.token, spaceURL+"/lists", map[string]any{"name": "Inbox"})
	e.statusID = mustCreate(t, tc, owner.token, "/api/v1/lists/"+itoa(e.listID)+"/statuses",
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": e.listID})
	e.taskID = mustCreate(t, tc, owner.token, "/api/v1/lists/"+itoa(e.listID)+"/tasks",
		map[string]any{"title": "Task", "status_id": e.statusID})
	e.tagID = mustCreate(t, tc, owner.token, spaceURL+"/tags", map[string]any{"name": "tag", "color": "#112233"})
	e.folderID = mustCreate(t, tc, owner.token, spaceURL+"/folders", map[string]any{"name": "Folder"})
	taskURL := "/api/v1/tasks/" + itoa(e.taskID)
	e.commentID = mustCreate(t, tc, owner.token, taskURL+"/comments", map[string]any{"body": "hello"})
	e.checklistID = mustCreate(t, tc, owner.token, taskURL+"/checklists", map[string]any{"name": "Checks"})
	e.itemID = mustCreate(t, tc, owner.token, taskURL+"/checklists/"+itoa(e.checklistID)+"/items", map[string]any{"text": "item"})
	formsURL := "/api/v1/workspaces/" + itoa(owner.workspaceID) + "/forms"
	e.formID = mustCreate(t, tc, owner.token, formsURL, map[string]any{"name": "Form", "slug": "harden-form"})
	e.questionID = mustCreate(t, tc, owner.token, formsURL+"/"+itoa(e.formID)+"/questions",
		map[string]any{"type": "short_text", "title": "Q"})
	e.fieldID = mustCreate(t, tc, owner.token, "/api/v1/workspaces/"+itoa(owner.workspaceID)+"/custom-fields",
		map[string]any{"name": "Field", "type": "text"})
	e.templateID = mustCreate(t, tc, owner.token, "/api/v1/workspaces/"+itoa(owner.workspaceID)+"/task-templates",
		map[string]any{"name": "Template", "configuration": fcTemplateConfig("normal", 0, "")})
	return e
}

func (e *hardenEnv) raw(t *testing.T, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer "+e.owner.token)
	rec := httptest.NewRecorder()
	e.tc.Router.ServeHTTP(rec, req)
	return rec
}

func (e *hardenEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, e.tc.Router, method, path, e.owner.token, body)
}

func errorPayload(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	payload, ok := decodeJSON(t, rec)["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no error envelope (status %d): %s", rec.Code, rec.Body.String())
	}
	return payload
}

func hasDetailCode(payload map[string]any, field, code string) bool {
	details, _ := payload["details"].([]any)
	for _, raw := range details {
		d, _ := raw.(map[string]any)
		if d["code"] == code && (field == "" || d["field"] == field) {
			return true
		}
	}
	return false
}

func expectErrorDetail(t *testing.T, rec *httptest.ResponseRecorder, status int, code, field, detailCode string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %.300s", rec.Code, status, rec.Body.String())
	}
	payload := errorPayload(t, rec)
	if code != "" && payload["code"] != code {
		t.Fatalf("error code = %v, want %s: %.300s", payload["code"], code, rec.Body.String())
	}
	if detailCode != "" && !hasDetailCode(payload, field, detailCode) {
		t.Fatalf("no detail %s/%s: %.300s", field, detailCode, rec.Body.String())
	}
	if id, _ := payload["request_id"].(string); id == "" {
		t.Fatalf("no request_id in %.300s", rec.Body.String())
	}
}

type mutation struct {
	name   string
	method string
	path   string
	body   func(v string) map[string]any
}

func (e *hardenEnv) mutations() []mutation {
	ws := "/api/v1/workspaces/" + itoa(e.owner.workspaceID)
	space := "/api/v1/spaces/" + itoa(e.owner.spaceID)
	list := "/api/v1/lists/" + itoa(e.listID)
	task := "/api/v1/tasks/" + itoa(e.taskID)
	form := ws + "/forms/" + itoa(e.formID)
	s := func(key string, extra map[string]any) func(string) map[string]any {
		return func(v string) map[string]any {
			body := map[string]any{key: v}
			for k, val := range extra {
				body[k] = val
			}
			return body
		}
	}
	return []mutation{
		{"workspace create name", http.MethodPost, "/api/v1/workspaces", s("name", nil)},
		{"workspace create description", http.MethodPost, "/api/v1/workspaces", s("description", map[string]any{"name": "ok"})},
		{"workspace update description", http.MethodPatch, ws, s("description", nil)},
		{"space create name", http.MethodPost, ws + "/spaces", s("name", nil)},
		{"space create currency", http.MethodPost, ws + "/spaces", s("currency", map[string]any{"name": "ok"})},
		{"space update name", http.MethodPatch, space, s("name", nil)},
		{"folder create name", http.MethodPost, space + "/folders", s("name", nil)},
		{"folder update name", http.MethodPatch, "/api/v1/folders/" + itoa(e.folderID), s("name", nil)},
		{"list create name", http.MethodPost, space + "/lists", s("name", nil)},
		{"list update name", http.MethodPatch, list, s("name", nil)},
		{"tag create name", http.MethodPost, space + "/tags", s("name", map[string]any{"color": "#112233"})},
		{"tag update name", http.MethodPut, space + "/tags/" + itoa(e.tagID), s("name", nil)},
		{"status create name", http.MethodPost, list + "/statuses", s("name", map[string]any{"color": "#112233", "type": 1, "list_id": e.listID})},
		{"status update name", http.MethodPut, list + "/statuses/" + itoa(e.statusID), s("name", nil)},
		{"task create title", http.MethodPost, list + "/tasks", s("title", map[string]any{"status_id": e.statusID})},
		{"task create description", http.MethodPost, list + "/tasks", s("description", map[string]any{"title": "ok", "status_id": e.statusID})},
		{"task update title", http.MethodPatch, task, s("title", nil)},
		{"task update description", http.MethodPatch, task, s("description", nil)},
		{"comment create", http.MethodPost, task + "/comments", s("body", nil)},
		{"comment update", http.MethodPatch, task + "/comments/" + itoa(e.commentID), s("body", nil)},
		{"checklist create", http.MethodPost, task + "/checklists", s("name", nil)},
		{"checklist rename", http.MethodPatch, task + "/checklists/" + itoa(e.checklistID), s("name", nil)},
		{"checklist item create", http.MethodPost, task + "/checklists/" + itoa(e.checklistID) + "/items", s("text", nil)},
		{"checklist item update", http.MethodPatch, task + "/checklists/" + itoa(e.checklistID) + "/items/" + itoa(e.itemID), s("text", nil)},
		{"form create name", http.MethodPost, ws + "/forms", s("name", map[string]any{"slug": "nul-form"})},
		{"form create description", http.MethodPost, ws + "/forms", s("description", map[string]any{"name": "ok", "slug": "nul-form-2"})},
		{"form update name", http.MethodPatch, form, s("name", nil)},
		{"form question create", http.MethodPost, form + "/questions", s("title", map[string]any{"type": "short_text"})},
		{"form question description", http.MethodPost, form + "/questions", s("description", map[string]any{"type": "short_text", "title": "t"})},
		{"form question update", http.MethodPatch, form + "/questions/" + itoa(e.questionID), s("title", nil)},
		{"custom field create name", http.MethodPost, ws + "/custom-fields", s("name", map[string]any{"type": "text"})},
		{"custom field update name", http.MethodPatch, ws + "/custom-fields/" + itoa(e.fieldID), s("name", map[string]any{"type": "text"})},
		{"task template create name", http.MethodPost, ws + "/task-templates", s("name", map[string]any{"configuration": fcTemplateConfig("normal", 0, "")})},
		{"task template update name", http.MethodPatch, "/api/v1/task-templates/" + itoa(e.templateID), s("name", nil)},
		{"profile bio", http.MethodPatch, "/api/v1/profile", s("bio", nil)},
		{"profile first name", http.MethodPatch, "/api/v1/profile", s("first_name", nil)},
		{"task apply template title", http.MethodPost, "/api/v1/task-templates/" + itoa(e.templateID) + "/apply", s("title", map[string]any{"list_id": e.listID, "status_id": e.statusID})},
	}
}

func TestInputHardening_NulByteInAnyStringIsRejectedWith400(t *testing.T) {
	e := newHardenEnv(t)
	for _, m := range e.mutations() {
		t.Run(m.name, func(t *testing.T) {
			rec := e.do(t, m.method, m.path, m.body("bad\x00value"))
			expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "", "INVALID_VALUE")
		})
	}
	t.Run("nested and array strings", func(t *testing.T) {
		list := "/api/v1/lists/" + itoa(e.listID)
		rec := e.do(t, http.MethodPost, list+"/tasks", map[string]any{
			"title": "ok", "status_id": e.statusID, "new_tag_names": []string{"fine", "bad\x00tag"},
		})
		expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "new_tag_names[1]", "INVALID_VALUE")
		rec = e.do(t, http.MethodPost, "/api/v1/auth/register", map[string]any{"email": "nul@example.test", "password": "password-123", "username": "nul\x00user"})
		expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "username", "INVALID_VALUE")
		rec = e.do(t, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "ok", "unknown_extra": "bad\x00"})
		expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "unknown_extra", "INVALID_VALUE")
		rec = e.do(t, http.MethodPost, "/api/v1/workspaces", map[string]any{"na\x00me": "ok"})
		expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "", "INVALID_VALUE")
	})
}

func TestInputHardening_InvalidUTF8AndLoneSurrogatesAreRejected(t *testing.T) {
	e := newHardenEnv(t)
	cases := map[string][]byte{
		"invalid utf8 byte":  []byte("{\"name\":\"ab\xff\xfecd\"}"),
		"truncated sequence": []byte("{\"name\":\"ab\xe2\x82\"}"),
		"lone high escape":   []byte(`{"name":"ab\ud800cd"}`),
		"lone low escape":    []byte(`{"name":"ab\udc00cd"}`),
		"high then non-low":  []byte(`{"name":"ab\ud800A"}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := e.raw(t, http.MethodPost, "/api/v1/workspaces", "application/json", body)
			expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "", "INVALID_ENCODING")
		})
	}
	t.Run("valid pair and emoji pass", func(t *testing.T) {
		rec := e.raw(t, http.MethodPost, "/api/v1/workspaces", "application/json", []byte(`{"name":"ok 😀 é \\u0000 literal"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		if got := dataObject(t, rec)["name"]; got != "ok \U0001F600 é \\u0000 literal" {
			t.Fatalf("name = %q", got)
		}
	})
}

func TestInputHardening_TagRenameToTakenNameConflicts(t *testing.T) {
	e := newHardenEnv(t)
	space := "/api/v1/spaces/" + itoa(e.owner.spaceID)
	other := mustCreate(t, e.tc, e.owner.token, space+"/tags", map[string]any{"name": "Other", "color": "#223344"})
	for _, name := range []string{"tag", "TAG"} {
		rec := e.do(t, http.MethodPut, space+"/tags/"+itoa(other), map[string]any{"name": name})
		expectErrorDetail(t, rec, http.StatusConflict, "TAG_NAME_TAKEN", "", "")
	}
	rec := e.do(t, http.MethodPut, space+"/tags/"+itoa(other), map[string]any{"name": "Renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename to free name: %d: %s", rec.Code, rec.Body.String())
	}
	rec = e.do(t, http.MethodPut, space+"/tags/999999", map[string]any{"name": "x"})
	expectErrorDetail(t, rec, http.StatusNotFound, "", "", "")
}

func TestInputHardening_ListWithUnknownParentStatusReturnsFieldError(t *testing.T) {
	e := newHardenEnv(t)
	space := "/api/v1/spaces/" + itoa(e.owner.spaceID)
	rec := e.do(t, http.MethodPost, space+"/lists", map[string]any{"name": "Child", "parent_status_id": 99999})
	expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "parent_status_id", "NOT_FOUND")

	otherOwner := newWorkspaceOwner(t, e.tc, "harden-other")
	otherList := mustCreate(t, e.tc, otherOwner.token, "/api/v1/spaces/"+itoa(otherOwner.spaceID)+"/lists", map[string]any{"name": "L"})
	foreign := mustCreate(t, e.tc, otherOwner.token, "/api/v1/lists/"+itoa(otherList)+"/statuses",
		map[string]any{"name": "Foreign", "color": "#112233", "type": 1, "list_id": otherList})
	rec = e.do(t, http.MethodPost, space+"/lists", map[string]any{"name": "Child", "parent_status_id": foreign})
	expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "parent_status_id", "NOT_FOUND")

	rec = e.do(t, http.MethodPost, space+"/lists", map[string]any{"name": "Child", "parent_status_id": e.statusID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("own status as parent: %d: %s", rec.Code, rec.Body.String())
	}
}

const overInt32 = 2147483648

func TestInputHardening_IntegersAboveColumnRangeReturn400(t *testing.T) {
	e := newHardenEnv(t)
	list := "/api/v1/lists/" + itoa(e.listID)
	task := "/api/v1/tasks/" + itoa(e.taskID)
	ws := "/api/v1/workspaces/" + itoa(e.owner.workspaceID)
	form := ws + "/forms/" + itoa(e.formID)

	cases := []struct {
		name, method, path, field string
		body                      map[string]any
	}{
		{"task create time_estimate", http.MethodPost, list + "/tasks", "time_estimate", map[string]any{"title": "x", "status_id": e.statusID, "time_estimate": overInt32}},
		{"task update time_estimate", http.MethodPatch, task, "time_estimate", map[string]any{"time_estimate": overInt32}},
		{"task create priority", http.MethodPost, list + "/tasks", "priority", map[string]any{"title": "x", "status_id": e.statusID, "priority": overInt32}},
		{"task update priority", http.MethodPatch, task, "priority", map[string]any{"priority": overInt32}},
		{"status create position", http.MethodPost, list + "/statuses", "position", map[string]any{"name": "s", "color": "#112233", "type": 1, "list_id": e.listID, "position": overInt32}},
		{"status update position", http.MethodPut, list + "/statuses/" + itoa(e.statusID), "position", map[string]any{"position": overInt32}},
		{"form question position", http.MethodPost, form + "/questions", "position", map[string]any{"type": "short_text", "title": "t", "position": 99999999999}},
		{"form question update position", http.MethodPatch, form + "/questions/" + itoa(e.questionID), "position", map[string]any{"position": 99999999999}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.do(t, c.method, c.path, c.body)
			expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", c.field, "TOO_LARGE")
			detail := firstDetail(errorPayload(t, rec), c.field)
			params, _ := detail["params"].(map[string]any)
			if params["max"] == nil {
				t.Fatalf("no params.max in %v", detail)
			}
		})
	}

	t.Run("boundary values are accepted", func(t *testing.T) {
		rec := e.do(t, http.MethodPatch, task, map[string]any{"time_estimate": 2147483647})
		if rec.Code != http.StatusOK {
			t.Fatalf("max int32 estimate: %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("ids above int64 are rejected", func(t *testing.T) {
		rec := e.do(t, http.MethodPost, list+"/tasks", map[string]any{"title": "x", "status_id": uint64(18446744073709551615)})
		expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "status_id", "TOO_LARGE")
		rec = e.do(t, http.MethodPost, list+"/tasks", map[string]any{"title": "x", "status_id": e.statusID, "parent_id": uint64(9223372036854775808)})
		expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "parent_id", "TOO_LARGE")
	})

	t.Run("path ids above int64", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/tasks/18446744073709551615",
			"/api/v1/lists/18446744073709551615/tasks",
			"/api/v1/spaces/18446744073709551615",
			"/api/v1/workspaces/18446744073709551615",
			"/api/v1/tasks/9223372036854775808/comments",
			ws + "/search?q=abc&space_id=18446744073709551615",
		} {
			rec := e.do(t, http.MethodGet, path, nil)
			if rec.Code < 400 || rec.Code >= 500 {
				t.Fatalf("GET %s = %d, want 4xx: %.200s", path, rec.Code, rec.Body.String())
			}
		}
	})
}

func firstDetail(payload map[string]any, field string) map[string]any {
	details, _ := payload["details"].([]any)
	for _, raw := range details {
		d, _ := raw.(map[string]any)
		if d["field"] == field {
			return d
		}
	}
	return map[string]any{}
}

func TestInputHardening_ArraysAreCapped(t *testing.T) {
	e := newHardenEnv(t)
	list := "/api/v1/lists/" + itoa(e.listID)
	task := "/api/v1/tasks/" + itoa(e.taskID)
	ws := "/api/v1/workspaces/" + itoa(e.owner.workspaceID)
	numbers := func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	}
	names := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("tag-%d", i)
		}
		return out
	}
	cases := []struct {
		name, method, path, field string
		max                       int
		body                      map[string]any
	}{
		{"create tag_ids", http.MethodPost, list + "/tasks", "tag_ids", 100, map[string]any{"title": "x", "status_id": e.statusID, "tag_ids": numbers(101)}},
		{"create assignee_ids", http.MethodPost, list + "/tasks", "assignee_ids", 100, map[string]any{"title": "x", "status_id": e.statusID, "assignee_ids": numbers(101)}},
		{"create huge tag_ids", http.MethodPost, list + "/tasks", "tag_ids", 100, map[string]any{"title": "x", "status_id": e.statusID, "tag_ids": numbers(40000)}},
		{"create new_tag_names", http.MethodPost, list + "/tasks", "new_tag_names", 50, map[string]any{"title": "x", "status_id": e.statusID, "new_tag_names": names(51)}},
		{"update add_tag_ids", http.MethodPatch, task, "add_tag_ids", 100, map[string]any{"add_tag_ids": numbers(101)}},
		{"update add_tag_names", http.MethodPatch, task, "add_tag_names", 50, map[string]any{"add_tag_names": names(51)}},
		{"update assignee_ids", http.MethodPatch, task, "assignee_ids", 100, map[string]any{"assignee_ids": numbers(101)}},
		{"checklist order", http.MethodPut, task + "/checklists/order", "ids", 200, map[string]any{"ids": numbers(201)}},
		{"checklist items order", http.MethodPut, task + "/checklists/" + itoa(e.checklistID) + "/items/order", "ids", 200, map[string]any{"ids": numbers(201)}},
		{"form question order", http.MethodPut, ws + "/forms/" + itoa(e.formID) + "/questions/reorder", "ids", 200, map[string]any{"ids": numbers(201)}},
		{"notifications read ids", http.MethodPost, "/api/v1/notifications/read", "ids", 200, map[string]any{"ids": numbers(201)}},
		{"comment mentions", http.MethodPost, task + "/comments", "mention_user_ids", 50, map[string]any{"body": "x", "mention_user_ids": numbers(51)}},
		{"custom field options", http.MethodPost, ws + "/custom-fields", "options", 200, map[string]any{"name": "d", "type": "dropdown", "options": manyOptions(201)}},
		{"form question options", http.MethodPost, ws + "/forms/" + itoa(e.formID) + "/questions", "options", 200, map[string]any{"type": "single_choice", "title": "t", "options": manyOptions(201)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.do(t, c.method, c.path, c.body)
			expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", c.field, "TOO_MANY")
			params, _ := firstDetail(errorPayload(t, rec), c.field)["params"].(map[string]any)
			if params["max"] != float64(c.max) {
				t.Fatalf("params.max = %v, want %d", params["max"], c.max)
			}
		})
	}
}

func manyOptions(n int) []map[string]string {
	out := make([]map[string]string, n)
	for i := range out {
		out[i] = map[string]string{"label": fmt.Sprintf("l%d", i), "value": fmt.Sprintf("v%d", i)}
	}
	return out
}

func TestInputHardening_TextFieldLengthLimitsCountRunes(t *testing.T) {
	e := newHardenEnv(t)
	ws := "/api/v1/workspaces/" + itoa(e.owner.workspaceID)
	space := "/api/v1/spaces/" + itoa(e.owner.spaceID)
	list := "/api/v1/lists/" + itoa(e.listID)
	task := "/api/v1/tasks/" + itoa(e.taskID)
	form := ws + "/forms/" + itoa(e.formID)
	cases := []struct {
		name, method, path, field string
		max                       int
		body                      func(v string) map[string]any
	}{
		{"workspace name", http.MethodPost, "/api/v1/workspaces", "name", 255, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"workspace description", http.MethodPost, "/api/v1/workspaces", "description", 5000, func(v string) map[string]any { return map[string]any{"name": "ok", "description": v} }},
		{"workspace update description", http.MethodPatch, ws, "description", 5000, func(v string) map[string]any { return map[string]any{"description": v} }},
		{"space name", http.MethodPost, ws + "/spaces", "name", 255, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"space update name", http.MethodPatch, space, "name", 255, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"folder name", http.MethodPost, space + "/folders", "name", 255, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"folder update name", http.MethodPatch, "/api/v1/folders/" + itoa(e.folderID), "name", 255, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"list name", http.MethodPost, space + "/lists", "name", 255, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"list update name", http.MethodPatch, list, "name", 255, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"tag name", http.MethodPost, space + "/tags", "name", 50, func(v string) map[string]any { return map[string]any{"name": v, "color": "#112233"} }},
		{"tag update name", http.MethodPut, space + "/tags/" + itoa(e.tagID), "name", 50, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"status name", http.MethodPost, list + "/statuses", "name", 50, func(v string) map[string]any {
			return map[string]any{"name": v, "color": "#112233", "type": 1, "list_id": e.listID}
		}},
		{"task title", http.MethodPost, list + "/tasks", "title", 500, func(v string) map[string]any { return map[string]any{"title": v, "status_id": e.statusID} }},
		{"task description", http.MethodPost, list + "/tasks", "description", 50000, func(v string) map[string]any {
			return map[string]any{"title": "ok", "description": v, "status_id": e.statusID}
		}},
		{"task update title", http.MethodPatch, task, "title", 500, func(v string) map[string]any { return map[string]any{"title": v} }},
		{"task update description", http.MethodPatch, task, "description", 50000, func(v string) map[string]any { return map[string]any{"description": v} }},
		{"new tag name", http.MethodPost, list + "/tasks", "new_tag_names[0]", 50, func(v string) map[string]any {
			return map[string]any{"title": "ok", "status_id": e.statusID, "new_tag_names": []string{v}}
		}},
		{"add tag name", http.MethodPatch, task, "add_tag_names[0]", 50, func(v string) map[string]any { return map[string]any{"add_tag_names": []string{v}} }},
		{"checklist name", http.MethodPost, task + "/checklists", "name", 255, func(v string) map[string]any { return map[string]any{"name": v} }},
		{"form name", http.MethodPost, ws + "/forms", "name", 255, func(v string) map[string]any { return map[string]any{"name": v, "slug": "len-form"} }},
		{"form description", http.MethodPost, ws + "/forms", "description", 5000, func(v string) map[string]any {
			return map[string]any{"name": "ok", "slug": "len-form-d", "description": v}
		}},
		{"form slug", http.MethodPost, ws + "/forms", "slug", 255, func(v string) map[string]any { return map[string]any{"name": "ok", "slug": v} }},
		{"form update description", http.MethodPatch, form, "description", 5000, func(v string) map[string]any { return map[string]any{"description": v} }},
		{"question title", http.MethodPost, form + "/questions", "title", 500, func(v string) map[string]any { return map[string]any{"type": "short_text", "title": v} }},
		{"question description", http.MethodPost, form + "/questions", "description", 5000, func(v string) map[string]any {
			return map[string]any{"type": "short_text", "title": "t", "description": v}
		}},
		{"question option label", http.MethodPost, form + "/questions", "options[0].label", 255, func(v string) map[string]any {
			return map[string]any{"type": "single_choice", "title": "t", "options": []any{map[string]any{"label": v, "value": "a"}, map[string]any{"label": "b", "value": "b"}}}
		}},
		{"custom field name", http.MethodPost, ws + "/custom-fields", "name", 255, func(v string) map[string]any { return map[string]any{"name": v, "type": "text"} }},
		{"custom field description", http.MethodPost, ws + "/custom-fields", "description", 5000, func(v string) map[string]any {
			return map[string]any{"name": "d", "type": "text", "description": v}
		}},
		{"template name", http.MethodPost, ws + "/task-templates", "name", 255, func(v string) map[string]any {
			return map[string]any{"name": v, "configuration": fcTemplateConfig("normal", 0, "")}
		}},
		{"template description", http.MethodPost, ws + "/task-templates", "description", 5000, func(v string) map[string]any {
			return map[string]any{"name": "ok", "description": v, "configuration": fcTemplateConfig("normal", 0, "")}
		}},
		{"template apply title", http.MethodPost, "/api/v1/task-templates/" + itoa(e.templateID) + "/apply", "title", 500, func(v string) map[string]any {
			return map[string]any{"list_id": e.listID, "status_id": e.statusID, "title": v}
		}},
		{"comment body", http.MethodPost, task + "/comments", "body", 20000, func(v string) map[string]any { return map[string]any{"body": v} }},
		{"profile first name", http.MethodPatch, "/api/v1/profile", "first_name", 100, func(v string) map[string]any { return map[string]any{"first_name": v} }},
		{"register username", http.MethodPost, "/api/v1/auth/register", "username", 30, func(v string) map[string]any {
			return map[string]any{"email": "len@example.test", "password": "password-123", "username": v}
		}},
		{"login password", http.MethodPost, "/api/v1/auth/login", "password", 128, func(v string) map[string]any { return map[string]any{"email": "len@example.test", "password": v} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.do(t, c.method, c.path, c.body(strings.Repeat("é", c.max+1)))
			expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", c.field, "TOO_LONG")
			params, _ := firstDetail(errorPayload(t, rec), c.field)["params"].(map[string]any)
			if params["max"] != float64(c.max) {
				t.Fatalf("params.max = %v, want %d", params["max"], c.max)
			}
			if c.method == http.MethodPost && !strings.Contains(c.path, "auth") && !strings.Contains(c.path, "forms") && !strings.Contains(c.path, "apply") {
				ok := e.do(t, c.method, c.path, c.body(strings.Repeat("é", c.max)))
				if ok.Code >= 400 && ok.Code != http.StatusConflict {
					t.Fatalf("exactly %d runes rejected: %d %.300s", c.max, ok.Code, ok.Body.String())
				}
			}
		})
	}

	t.Run("currency must be a 3 letter code", func(t *testing.T) {
		for _, v := range []string{"ZZZZZZZZ", "US", "12$", ""} {
			if v == "" {
				continue
			}
			rec := e.do(t, http.MethodPost, ws+"/spaces", map[string]any{"name": "cur", "currency": v})
			expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "currency", "")
		}
		rec := e.do(t, http.MethodPost, ws+"/spaces", map[string]any{"name": "cur", "currency": "EUR"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("EUR: %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("color formats", func(t *testing.T) {
		for _, v := range []string{"#12345", "123456", "#1234567", "#12345g", "red"} {
			rec := e.do(t, http.MethodPost, space+"/tags", map[string]any{"name": "col" + v, "color": v})
			expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "color", "")
		}
	})

	t.Run("big workspace description is not stored", func(t *testing.T) {
		rec := e.do(t, http.MethodPatch, ws, map[string]any{"description": strings.Repeat("a", 900000)})
		expectErrorDetail(t, rec, http.StatusBadRequest, "VALIDATION_FAILED", "description", "TOO_LONG")
	})
}

func TestInputHardening_EveryBodyLimitOverrideIsEnforced(t *testing.T) {
	e := newHardenEnv(t)
	cfg := integrationConfig
	const overhead = 1 << 20
	submissions := "/api/v1/public/forms/harden-form/submissions"
	filler := func(n int) []byte {
		return []byte(`{"answers":{},"pad":"` + strings.Repeat("a", n) + `"}`)
	}
	publish := e.do(t, http.MethodPost, "/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/forms/"+itoa(e.formID)+"/publish", nil)
	if publish.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", publish.Code, publish.Body.String())
	}

	t.Run("public submission 128 KiB", func(t *testing.T) {
		over := e.raw(t, http.MethodPost, submissions, "application/json", filler(200<<10))
		expectErrorDetail(t, over, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "", "")
		if errorPayload(t, over)["params"].(map[string]any)["max_bytes"] != float64(128<<10) {
			t.Fatalf("max_bytes = %v", errorPayload(t, over)["params"])
		}
		under := e.raw(t, http.MethodPost, submissions, "application/json", filler(100<<10))
		if under.Code == http.StatusRequestEntityTooLarge {
			t.Fatalf("100 KiB body was rejected: %s", under.Body.String())
		}
	})

	t.Run("default 1 MiB", func(t *testing.T) {
		over := e.raw(t, http.MethodPost, "/api/v1/workspaces", "application/json", filler(2<<20))
		expectErrorDetail(t, over, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "", "")
		huge := e.raw(t, http.MethodPost, "/api/v1/workspaces", "application/json", filler(5<<20))
		expectErrorDetail(t, huge, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "", "")
	})

	multipart := func(path string, size int64) *httptest.ResponseRecorder {
		boundary := "XBOUNDARYX"
		var buf bytes.Buffer
		buf.WriteString("--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.bin\"\r\nContent-Type: application/octet-stream\r\n\r\n")
		buf.Write(bytes.Repeat([]byte("a"), int(size)))
		buf.WriteString("\r\n--" + boundary + "--\r\n")
		return e.raw(t, http.MethodPost, path, "multipart/form-data; boundary="+boundary, buf.Bytes())
	}
	t.Run("public form upload", func(t *testing.T) {
		rec := multipart("/api/v1/public/forms/harden-form/uploads", cfg.Forms.UploadMaxBytes+overhead+1024)
		expectTooLarge(t, rec)
	})
	t.Run("attachment", func(t *testing.T) {
		rec := multipart("/api/v1/tasks/"+itoa(e.taskID)+"/attachments", cfg.Storage.MaxAttachmentBytes+overhead+1024)
		expectTooLarge(t, rec)
	})
	t.Run("avatar", func(t *testing.T) {
		rec := multipart("/api/v1/profile/avatar", cfg.Storage.MaxAvatarBytes+overhead+1024)
		expectTooLarge(t, rec)
	})
	t.Run("workspace avatar", func(t *testing.T) {
		rec := multipart("/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/avatar", cfg.Storage.MaxAvatarBytes+overhead+1024)
		expectTooLarge(t, rec)
	})
	t.Run("workspace image", func(t *testing.T) {
		rec := multipart("/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/images", cfg.Storage.MaxImageBytes+overhead+1024)
		expectTooLarge(t, rec)
	})
}

func TestInputHardening_UnknownRoutesAndMethodsUseTheEnvelope(t *testing.T) {
	e := newHardenEnv(t)
	t.Run("unknown route", func(t *testing.T) {
		for _, path := range []string{"/nope", "/api/v1/nope", "/api/v1/workspaces/1/nope", "/"} {
			rec := e.do(t, http.MethodGet, path, nil)
			expectErrorDetail(t, rec, http.StatusNotFound, "ROUTE_NOT_FOUND", "", "")
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
			}
		}
		rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/nope", "", nil)
		expectErrorDetail(t, rec, http.StatusNotFound, "ROUTE_NOT_FOUND", "", "")
	})
	t.Run("wrong method", func(t *testing.T) {
		rec := e.do(t, http.MethodDelete, "/api/v1/auth/login", nil)
		expectErrorDetail(t, rec, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "", "")
		if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "POST") {
			t.Fatalf("Allow = %q", allow)
		}
		rec = e.do(t, http.MethodPost, "/healthz", nil)
		expectErrorDetail(t, rec, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "", "")
		rec = e.do(t, http.MethodPut, "/api/v1/workspaces", nil)
		expectErrorDetail(t, rec, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "", "")
	})
	t.Run("options preflight still works", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/workspaces", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		e.tc.Router.ServeHTTP(rec, req)
		if rec.Code >= 400 && rec.Code != http.StatusForbidden {
			t.Fatalf("preflight = %d", rec.Code)
		}
	})
}

func TestInputHardening_ContentTypeMustBeJSON(t *testing.T) {
	e := newHardenEnv(t)
	body := []byte(`{"name":"ct"}`)
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "application/xml", ""} {
		t.Run("content type "+ct, func(t *testing.T) {
			rec := e.raw(t, http.MethodPost, "/api/v1/workspaces", ct, body)
			expectErrorDetail(t, rec, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "", "")
			if errorPayload(t, rec)["params"].(map[string]any)["expected"] != "application/json" {
				t.Fatalf("params = %v", errorPayload(t, rec)["params"])
			}
		})
	}
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON"} {
		t.Run("accepts "+ct, func(t *testing.T) {
			rec := e.raw(t, http.MethodPost, "/api/v1/workspaces", ct, body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	t.Run("bodyless POST needs no content type", func(t *testing.T) {
		rec := e.raw(t, http.MethodPost, "/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/forms/"+itoa(e.formID)+"/publish", "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("GET is not checked", func(t *testing.T) {
		rec := e.raw(t, http.MethodGet, "/api/v1/workspaces", "text/plain", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestInputHardening_EmptyPatchKeepsUpdatedAtAndTimestampsAreConsistent(t *testing.T) {
	e := newHardenEnv(t)
	task := "/api/v1/tasks/" + itoa(e.taskID)
	before := dataObject(t, e.do(t, http.MethodGet, task, nil))
	rec := e.do(t, http.MethodPatch, task, map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("empty patch: %d: %s", rec.Code, rec.Body.String())
	}
	after := dataObject(t, rec)
	if after["updated_at"] != before["updated_at"] {
		t.Fatalf("updated_at changed: %v -> %v", before["updated_at"], after["updated_at"])
	}
	again := dataObject(t, e.do(t, http.MethodGet, task, nil))
	if again["updated_at"] != before["updated_at"] {
		t.Fatalf("updated_at changed after empty patch: %v -> %v", before["updated_at"], again["updated_at"])
	}

	list := "/api/v1/lists/" + itoa(e.listID)
	created := e.do(t, http.MethodPost, list+"/tasks", map[string]any{"title": "stamp", "status_id": e.statusID})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d", created.Code)
	}
	posted := dataObject(t, created)
	fetched := dataObject(t, e.do(t, http.MethodGet, "/api/v1/tasks/"+itoa(uint(posted["id"].(float64))), nil))
	for _, key := range []string{"created_at", "updated_at"} {
		if posted[key] != fetched[key] {
			t.Fatalf("%s differs between POST and GET: %v vs %v", key, posted[key], fetched[key])
		}
	}
	space := "/api/v1/spaces/" + itoa(e.owner.spaceID)
	for _, path := range []string{"/api/v1/workspaces", space + "/folders", space + "/lists"} {
		var name = fmt.Sprintf("stamp-%d", len(path))
		c := e.do(t, http.MethodPost, path, map[string]any{"name": name})
		if c.Code != http.StatusCreated {
			t.Fatalf("POST %s: %d %s", path, c.Code, c.Body.String())
		}
		obj := dataObject(t, c)
		item := path + "/" + itoa(uint(obj["id"].(float64)))
		if path == space+"/folders" {
			continue
		}
		if path == space+"/lists" {
			item = "/api/v1/lists/" + itoa(uint(obj["id"].(float64)))
		}
		get := e.do(t, http.MethodGet, item, nil)
		if get.Code != http.StatusOK {
			continue
		}
		got := dataObject(t, get)
		if obj["created_at"] != got["created_at"] {
			t.Fatalf("%s created_at differs: %v vs %v", path, obj["created_at"], got["created_at"])
		}
	}
}

func TestInputHardening_PaginationValidationIsUniformAcrossEndpoints(t *testing.T) {
	e := newHardenEnv(t)
	ws := "/api/v1/workspaces/" + itoa(e.owner.workspaceID)
	task := "/api/v1/tasks/" + itoa(e.taskID)
	endpoints := []string{
		"/api/v1/notifications",
		"/api/v1/contacts",
		"/api/v1/contacts/requests/incoming",
		"/api/v1/contacts/requests/outgoing",
		ws + "/spaces",
		ws + "/search?q=abc",
		ws + "/forms/" + itoa(e.formID) + "/submissions",
		task + "/comments",
		task + "/activity",
	}
	join := func(path, query string) string {
		if strings.Contains(path, "?") {
			return path + "&" + query
		}
		return path + "?" + query
	}
	for _, path := range endpoints {
		t.Run(path, func(t *testing.T) {
			for _, bad := range []string{"limit=0", "limit=-1", "limit=101", "limit=abc", "page=0", "page=-3", "page=x", "page=10001", "limit=1.5", "limit=%00"} {
				rec := e.do(t, http.MethodGet, join(path, bad), nil)
				expectErrorDetail(t, rec, http.StatusBadRequest, "INVALID_PAGINATION", "", "")
			}
			rec := e.do(t, http.MethodGet, join(path, "page=9999&limit=5"), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("page beyond range: %d: %s", rec.Code, rec.Body.String())
			}
			body := decodeJSON(t, rec)
			if data, ok := body["data"].([]any); !ok || len(data) != 0 {
				t.Fatalf("data = %v, want empty array", body["data"])
			}
			p, _ := body["pagination"].(map[string]any)
			if p["page"] != float64(9999) || p["limit"] != float64(5) {
				t.Fatalf("pagination = %v", p)
			}
			total, pages := p["total"].(float64), p["pages"].(float64)
			if want := float64((int(total) + 4) / 5); pages != want {
				t.Fatalf("pages = %v for total %v, want %v", pages, total, want)
			}
		})
	}
	t.Run("task cursor limit", func(t *testing.T) {
		for _, bad := range []string{"limit=0", "limit=101", "limit=abc"} {
			rec := e.do(t, http.MethodGet, "/api/v1/lists/"+itoa(e.listID)+"/tasks?"+bad, nil)
			expectErrorDetail(t, rec, http.StatusBadRequest, "INVALID_PAGINATION", "", "")
		}
	})
}

func expectTooLarge(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %.300s", rec.Code, rec.Body.String())
	}
	code := errorPayload(t, rec)["code"]
	if code != "PAYLOAD_TOO_LARGE" && code != "FILE_TOO_LARGE" {
		t.Fatalf("code = %v", code)
	}
}

func TestInputHardening_EveryBodyLimitKeyMatchesARegisteredRoute(t *testing.T) {
	e := newHardenEnv(t)
	engine, ok := e.tc.Router.(*gin.Engine)
	if !ok {
		t.Fatalf("router is %T", e.tc.Router)
	}
	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for key := range app.BodyLimitOverrides(integrationConfig) {
		if !registered[key] {
			t.Errorf("body limit override %q matches no route", key)
		}
	}
}
