package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

var fcCounter atomic.Uint32

type fcEnv struct {
	owner    workspaceOwner
	listID   uint
	statusID uint
	formsURL string
}

func newFCEnv(t *testing.T, tc *TestContext, label string) fcEnv {
	t.Helper()
	owner := newWorkspaceOwner(t, tc, label)
	listID := mustCreate(t, tc, owner.token, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", map[string]any{"name": "Inbox"})
	statusID := mustCreate(t, tc, owner.token, "/api/v1/lists/"+itoa(listID)+"/statuses",
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	return fcEnv{
		owner: owner, listID: listID, statusID: statusID,
		formsURL: "/api/v1/workspaces/" + itoa(owner.workspaceID) + "/forms",
	}
}

func (e fcEnv) newSlug(label string) string {
	label = strings.ToLower(strings.ReplaceAll(label, " ", "-"))
	return fmt.Sprintf("fc-%s-%d-%d", label, e.owner.workspaceID, fcCounter.Add(1))
}

func (e fcEnv) newForm(t *testing.T, tc *TestContext, label string) (uint, string) {
	t.Helper()
	formID := mustCreate(t, tc, e.owner.token, e.formsURL, map[string]any{"name": label, "slug": e.newSlug(label)})
	return formID, e.formsURL + "/" + itoa(formID)
}

func (e fcEnv) taskConfig(mappings []any) map[string]any {
	return map[string]any{
		"enabled":       true,
		"target":        map[string]any{"workspace_id": e.owner.workspaceID, "list_id": e.listID},
		"task_defaults": map[string]any{"priority": "normal", "status_id": e.statusID},
		"mappings":      mappings,
	}
}

func fcErrorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	body, ok := decodeJSON(t, rec)["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no error object: %s", rec.Body.String())
	}
	return body
}

func fcExpectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code, field string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	body := fcErrorBody(t, rec)
	if code != "" && body["code"] != code {
		t.Fatalf("error code = %v, want %s: %s", body["code"], code, rec.Body.String())
	}
	if field != "" {
		detail := errorDetail(body, field)
		if detail == nil || detail["code"] == "" {
			t.Fatalf("error details %v do not name %q: %s", body["details"], field, rec.Body.String())
		}
	}
}

func fcQuestions(t *testing.T, tc *TestContext, token, formURL string) []map[string]any {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, formURL, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get form: %d: %s", rec.Code, rec.Body.String())
	}
	raw, _ := dataObject(t, rec)["questions"].([]any)
	result := make([]map[string]any, len(raw))
	for i, item := range raw {
		result[i] = item.(map[string]any)
	}
	return result
}

func fcOrder(questions []map[string]any) ([]string, []int) {
	titles := make([]string, len(questions))
	positions := make([]int, len(questions))
	for i, question := range questions {
		titles[i] = question["title"].(string)
		positions[i] = int(question["position"].(float64))
	}
	return titles, positions
}

func fcAddQuestion(t *testing.T, tc *TestContext, token, formURL, title string, position *int) (uint, int) {
	t.Helper()
	body := map[string]any{"type": "short_text", "title": title}
	if position != nil {
		body["position"] = *position
	}
	rec := request(t, tc.Router, http.MethodPost, formURL+"/questions", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add question %q: %d: %s", title, rec.Code, rec.Body.String())
	}
	return responseID(t, rec), int(dataObject(t, rec)["position"].(float64))
}

func fcSubmit(t *testing.T, tc *TestContext, slug, key string, answers map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"answers": answers})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/"+slug+"/submissions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	n := fcCounter.Add(1)
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:4000", n>>16&255, n>>8&255, n&255)
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return rec
}

func fcIntPtr(value int) *int { return &value }

func TestFormQuestions_AppendWithoutPosition(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcappend")
	_, formURL := env.newForm(t, tc, "Append")

	wantPositions := []int{0, 1, 2}
	ids := make([]uint, 3)
	for i, title := range []string{"A", "B", "C"} {
		var position int
		ids[i], position = fcAddQuestion(t, tc, env.owner.token, formURL, title, nil)
		if position != wantPositions[i] {
			t.Fatalf("question %s position = %d, want %d", title, position, wantPositions[i])
		}
	}

	rec := request(t, tc.Router, http.MethodDelete, formURL+"/questions/"+itoa(ids[1]), env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete middle question: %d: %s", rec.Code, rec.Body.String())
	}
	if _, position := fcAddQuestion(t, tc, env.owner.token, formURL, "D", nil); position != 3 {
		t.Fatalf("appended after a delete: position = %d, want max(non-deleted)+1 = 3", position)
	}

	rec = request(t, tc.Router, http.MethodDelete, formURL+"/questions/"+itoa(ids[2]), env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete question C: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodDelete, formURL+"/questions/"+itoa(mustLastQuestionID(t, tc, env, formURL)),
		env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete question D: %d: %s", rec.Code, rec.Body.String())
	}
	if _, position := fcAddQuestion(t, tc, env.owner.token, formURL, "E", nil); position != 1 {
		t.Fatalf("append after deleting the tail: position = %d, want 1", position)
	}

	_, emptyFormURL := env.newForm(t, tc, "Empty")
	if _, position := fcAddQuestion(t, tc, env.owner.token, emptyFormURL, "First", nil); position != 0 {
		t.Fatalf("first question of a form: position = %d, want 0", position)
	}
}

func mustLastQuestionID(t *testing.T, tc *TestContext, env fcEnv, formURL string) uint {
	t.Helper()
	questions := fcQuestions(t, tc, env.owner.token, formURL)
	return uint(questions[len(questions)-1]["id"].(float64))
}

func TestFormQuestions_ExplicitPositionShiftsFollowingQuestions(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcshift")
	_, formURL := env.newForm(t, tc, "Shift")

	var ids []uint
	for _, title := range []string{"A", "B", "C"} {
		id, _ := fcAddQuestion(t, tc, env.owner.token, formURL, title, nil)
		ids = append(ids, id)
	}
	if _, position := fcAddQuestion(t, tc, env.owner.token, formURL, "D", fcIntPtr(1)); position != 1 {
		t.Fatalf("inserted question position = %d, want 1", position)
	}
	titles, positions := fcOrder(fcQuestions(t, tc, env.owner.token, formURL))
	if fmt.Sprint(titles) != "[A D B C]" || fmt.Sprint(positions) != "[0 1 2 3]" {
		t.Fatalf("after insert at 1: %v %v", titles, positions)
	}

	if _, position := fcAddQuestion(t, tc, env.owner.token, formURL, "Z", fcIntPtr(0)); position != 0 {
		t.Fatalf("insert at the head: position = %d", position)
	}
	titles, positions = fcOrder(fcQuestions(t, tc, env.owner.token, formURL))
	if fmt.Sprint(titles) != "[Z A D B C]" || fmt.Sprint(positions) != "[0 1 2 3 4]" {
		t.Fatalf("after insert at 0: %v %v", titles, positions)
	}

	if _, position := fcAddQuestion(t, tc, env.owner.token, formURL, "Far", fcIntPtr(10)); position != 10 {
		t.Fatalf("free position beyond the tail must be kept: %d", position)
	}
	if _, position := fcAddQuestion(t, tc, env.owner.token, formURL, "Next", nil); position != 11 {
		t.Fatalf("append after a gap: position = %d, want 11", position)
	}

	rec := request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token,
		map[string]any{"type": "short_text", "title": "Bad", "position": -1})
	fcExpectError(t, rec, http.StatusBadRequest, "INVALID_FORM", "position")

	ordered := []uint{ids[2], ids[0], ids[1]}
	remaining := fcQuestions(t, tc, env.owner.token, formURL)
	reorder := append([]uint(nil), ordered...)
	for _, question := range remaining {
		id := uint(question["id"].(float64))
		if id != ids[0] && id != ids[1] && id != ids[2] {
			reorder = append(reorder, id)
		}
	}
	rec = request(t, tc.Router, http.MethodPut, formURL+"/questions/reorder", env.owner.token, map[string]any{"ids": reorder})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder: %d: %s", rec.Code, rec.Body.String())
	}
	titles, positions = fcOrder(fcQuestions(t, tc, env.owner.token, formURL))
	if fmt.Sprint(titles[:3]) != "[C A B]" || positions[0] != 0 || positions[len(positions)-1] != len(positions)-1 {
		t.Fatalf("after reorder: %v %v", titles, positions)
	}

	rec = request(t, tc.Router, http.MethodPatch, formURL+"/questions/"+itoa(ids[0]), env.owner.token,
		map[string]any{"type": "short_text", "title": "A renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch without position: %d: %s", rec.Code, rec.Body.String())
	}
	if got := dataObject(t, rec)["position"]; got != float64(1) {
		t.Fatalf("patch without position must keep position 1, got %v", got)
	}
}

func TestFormQuestions_ConcurrentAddsGetDistinctPositions(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcrace")
	_, formURL := env.newForm(t, tc, "Race")

	const workers = 12
	var wg sync.WaitGroup
	positions := make(chan int, workers)
	failures := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token,
				map[string]any{"type": "short_text", "title": fmt.Sprintf("Q%d", i)})
			if rec.Code != http.StatusCreated {
				failures <- fmt.Sprintf("%d: %s", rec.Code, rec.Body.String())
				return
			}
			var body struct {
				Data struct {
					Position int `json:"position"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				failures <- err.Error()
				return
			}
			positions <- body.Data.Position
		}(i)
	}
	wg.Wait()
	close(positions)
	close(failures)
	for failure := range failures {
		t.Fatalf("concurrent add failed: %s", failure)
	}
	var got []int
	for position := range positions {
		got = append(got, position)
	}
	sort.Ints(got)
	for i, position := range got {
		if position != i {
			t.Fatalf("concurrent appends produced positions %v, want 0..%d without duplicates", got, workers-1)
		}
	}

	_, sameSpotURL := env.newForm(t, tc, "Same spot")
	fcAddQuestion(t, tc, env.owner.token, sameSpotURL, "Seed", nil)
	var wg2 sync.WaitGroup
	results := make(chan int, workers)
	for i := 0; i < workers; i++ {
		wg2.Add(1)
		go func(i int) {
			defer wg2.Done()
			rec := request(t, tc.Router, http.MethodPost, sameSpotURL+"/questions", env.owner.token,
				map[string]any{"type": "short_text", "title": fmt.Sprintf("S%d", i), "position": 0})
			if rec.Code != http.StatusCreated {
				results <- -1
				return
			}
			results <- 0
		}(i)
	}
	wg2.Wait()
	close(results)
	for code := range results {
		if code < 0 {
			t.Fatalf("concurrent insert at a taken position failed")
		}
	}
	_, final := fcOrder(fcQuestions(t, tc, env.owner.token, sameSpotURL))
	if len(final) != workers+1 {
		t.Fatalf("got %d questions, want %d", len(final), workers+1)
	}
	for i, position := range final {
		if position != i {
			t.Fatalf("concurrent inserts at one position left positions %v, want contiguous 0..%d", final, workers)
		}
	}
}

func TestFormSubmissions_PaginationAndFilters(t *testing.T) {
	tc := newTestContext(t)
	f := newFormFixture(t, tc, "fcpage")
	listURL := f.formURL + "/" + itoa(f.formID) + "/submissions"

	ids := make([]uint, 5)
	for i := range ids {
		rec := fcSubmit(t, tc, f.slug, fmt.Sprintf("page-%d", i), map[string]any{itoa(f.questionID): fmt.Sprintf("Task %d", i)})
		if rec.Code != http.StatusCreated {
			t.Fatalf("submit %d: %d: %s", i, rec.Code, rec.Body.String())
		}
		ids[i] = responseID(t, rec)
	}
	for _, id := range ids[:2] {
		if err := tc.DB.Table("form_submissions").Where("id = ?", id).
			Updates(map[string]any{"task_creation_status": "failed", "task_creation_error": "boom"}).Error; err != nil {
			t.Fatalf("mark failed: %v", err)
		}
	}
	if err := tc.DB.Table("form_submissions").Where("id = ?", ids[2]).
		Update("task_creation_status", "skipped").Error; err != nil {
		t.Fatalf("mark skipped: %v", err)
	}

	list := func(query string) (*httptest.ResponseRecorder, []any, map[string]any) {
		rec := request(t, tc.Router, http.MethodGet, listURL+query, f.owner.token, nil)
		if rec.Code != http.StatusOK {
			return rec, nil, nil
		}
		body := decodeJSON(t, rec)
		items, _ := body["data"].([]any)
		meta, _ := body["pagination"].(map[string]any)
		return rec, items, meta
	}
	idOf := func(item any) uint { return uint(item.(map[string]any)["id"].(float64)) }

	rec, items, meta := list("")
	if rec.Code != http.StatusOK || len(items) != 5 || meta["total"] != float64(5) ||
		meta["page"] != float64(1) || meta["limit"] != float64(20) || meta["pages"] != float64(1) {
		t.Fatalf("default page: %d items=%d meta=%v", rec.Code, len(items), meta)
	}
	for i, item := range items {
		if idOf(item) != ids[len(ids)-1-i] {
			t.Fatalf("submissions must be newest first, got %v at %d", idOf(item), i)
		}
	}

	_, items, meta = list("?limit=2&page=2")
	if len(items) != 2 || idOf(items[0]) != ids[2] || meta["total"] != float64(5) || meta["pages"] != float64(3) {
		t.Fatalf("page 2 of 3: items=%v meta=%v", items, meta)
	}
	_, items, _ = list("?limit=2&page=3")
	if len(items) != 1 || idOf(items[0]) != ids[0] {
		t.Fatalf("last page: %v", items)
	}
	_, items, meta = list("?limit=2&page=9")
	if len(items) != 0 || meta["total"] != float64(5) {
		t.Fatalf("page beyond the end: %v %v", items, meta)
	}

	_, items, meta = list("?task_creation_status=failed")
	if len(items) != 2 || meta["total"] != float64(2) {
		t.Fatalf("failed filter: %v %v", items, meta)
	}
	_, items, meta = list("?task_creation_status=skipped")
	if len(items) != 1 || meta["total"] != float64(1) || idOf(items[0]) != ids[2] {
		t.Fatalf("skipped filter: %v %v", items, meta)
	}
	_, items, meta = list("?task_creation_status=succeeded&limit=1")
	if len(items) != 1 || meta["total"] != float64(2) || meta["pages"] != float64(2) {
		t.Fatalf("succeeded filter with limit: %v %v", items, meta)
	}
	_, items, meta = list("?task_creation_status=pending")
	if len(items) != 0 || meta["total"] != float64(0) {
		t.Fatalf("pending filter: %v %v", items, meta)
	}
	_, items, meta = list("?status=submitted&task_creation_status=failed")
	if len(items) != 2 || meta["total"] != float64(2) {
		t.Fatalf("combined filter: %v %v", items, meta)
	}
	_, items, _ = list("?status=failed")
	if len(items) != 0 {
		t.Fatalf("status=failed must match no submission: %v", items)
	}

	for _, query := range []string{
		"?page=0", "?page=abc", "?page=10001", "?limit=0", "?limit=101", "?limit=x",
		"?task_creation_status=bogus", "?status=bogus",
	} {
		rec := request(t, tc.Router, http.MethodGet, listURL+query, f.owner.token, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("GET %s: %d, want 400: %s", query, rec.Code, rec.Body.String())
		}
	}
	rec = request(t, tc.Router, http.MethodGet, listURL+"?limit=100", f.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("limit=100: %d", rec.Code)
	}
	if rec = request(t, tc.Router, http.MethodGet, listURL, f.guest, nil); rec.Code != http.StatusOK {
		t.Fatalf("guest list: %d", rec.Code)
	}
	if rec = request(t, tc.Router, http.MethodGet, listURL, f.outsider, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list: %d", rec.Code)
	}
}

func TestForms_CreateWithQuestionsAndTaskConfigIsAtomic(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcatomic")
	slug := env.newSlug("atomic")

	rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{
		"name": "Support request", "slug": slug,
		"questions": []any{
			map[string]any{"type": "short_text", "title": "Subject", "required": true},
			map[string]any{"type": "dropdown", "title": "Urgency", "required": true, "options": []any{
				map[string]any{"label": "Whenever", "value": "later"},
				map[string]any{"label": "Now", "value": "now"},
			}},
			map[string]any{"type": "rating", "title": "Mood", "settings": map[string]any{"min": 1, "max": 5}},
		},
		"task_config": env.taskConfig([]any{
			map[string]any{"question_index": 0, "target": map[string]any{"type": "task_field", "field": "title"}},
			map[string]any{
				"question_index": 1, "target": map[string]any{"type": "task_field", "field": "priority"},
				"transform": map[string]any{"type": "enum", "values": map[string]any{"later": "low", "now": "critical"}},
			},
		}),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create form with content: %d: %s", rec.Code, rec.Body.String())
	}
	form := dataObject(t, rec)
	formID := uint(form["id"].(float64))
	questions, _ := form["questions"].([]any)
	if len(questions) != 3 {
		t.Fatalf("created form has %d questions: %s", len(questions), rec.Body.String())
	}
	for i, item := range questions {
		question := item.(map[string]any)
		if question["position"] != float64(i) || question["id"] == nil {
			t.Fatalf("question %d = %v", i, question)
		}
	}
	options, _ := questions[1].(map[string]any)["options"].([]any)
	if len(options) != 2 {
		t.Fatalf("options were not created: %v", questions[1])
	}

	formURL := env.formsURL + "/" + itoa(formID)
	rec = request(t, tc.Router, http.MethodGet, formURL+"/task-config", env.owner.token, nil)
	config := dataObject(t, rec)
	mappings, _ := config["mappings"].([]any)
	if rec.Code != http.StatusOK || len(mappings) != 2 {
		t.Fatalf("task config: %d: %s", rec.Code, rec.Body.String())
	}
	firstID := questions[0].(map[string]any)["id"]
	first := mappings[0].(map[string]any)
	if first["question_id"] != firstID || first["question_index"] != nil {
		t.Fatalf("question_index was not resolved to the new question id: %v", first)
	}

	rec = request(t, tc.Router, http.MethodPost, formURL+"/publish", env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
	rec = fcSubmit(t, tc, slug, "atomic-1", map[string]any{
		itoa(uint(firstID.(float64))):                             "Printer is on fire",
		itoa(uint(questions[1].(map[string]any)["id"].(float64))): "now",
	})
	if rec.Code != http.StatusCreated || dataObject(t, rec)["task_creation_status"] != "succeeded" {
		t.Fatalf("submit: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodGet,
		"/api/v1/submissions/"+itoa(responseID(t, rec))+"/tasks", env.owner.token, nil)
	tasks := responseArray(t, rec)
	if len(tasks) != 1 || tasks[0].(map[string]any)["title"] != "Printer is on fire" ||
		tasks[0].(map[string]any)["priority"] != float64(5) {
		t.Fatalf("task created from the form: %s", rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{
		"name": "Plain", "slug": env.newSlug("plain"),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("plain create must keep working: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestForms_CreateRollsBackEverythingOnAnyError(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcrollback")
	otherListID := mustCreate(t, tc, env.owner.token, "/api/v1/spaces/"+itoa(env.owner.spaceID)+"/lists", map[string]any{"name": "Other"})
	otherStatusID := mustCreate(t, tc, env.owner.token, "/api/v1/lists/"+itoa(otherListID)+"/statuses",
		map[string]any{"name": "Elsewhere", "color": "#445566", "type": 1, "list_id": otherListID})

	valid := func(title string) map[string]any {
		return map[string]any{"type": "short_text", "title": title}
	}
	titleMapping := func(index int) map[string]any {
		return map[string]any{"question_index": index, "target": map[string]any{"type": "task_field", "field": "title"}}
	}
	tooMany := make([]any, 101)
	for i := range tooMany {
		tooMany[i] = valid(fmt.Sprintf("Q%d", i))
	}
	foreignStatus := env.taskConfig([]any{titleMapping(0)})
	foreignStatus["task_defaults"] = map[string]any{"status_id": otherStatusID}

	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
		field  string
	}{
		{"unknown type", map[string]any{"questions": []any{valid("RollbackMarker"), map[string]any{"type": "nope", "title": "x"}}},
			http.StatusBadRequest, "INVALID_FORM", "questions[1].type"},
		{"blank title", map[string]any{"questions": []any{valid("RollbackMarker"), valid("   ")}},
			http.StatusBadRequest, "INVALID_FORM", "questions[1].title"},
		{"unsupported setting", map[string]any{"questions": []any{valid("RollbackMarker"), valid("ok"),
			map[string]any{"type": "number", "title": "n", "settings": map[string]any{"color": "red"}}}},
			http.StatusBadRequest, "INVALID_FORM", "questions[2].settings.color"},
		{"options on a text question", map[string]any{"questions": []any{valid("RollbackMarker"), map[string]any{
			"type": "short_text", "title": "t", "options": []any{map[string]any{"label": "a", "value": "a"}}}}},
			http.StatusBadRequest, "INVALID_FORM", "questions[1].options"},
		{"too many questions", map[string]any{"questions": tooMany},
			http.StatusBadRequest, "INVALID_FORM", "questions"},
		{"mapping index out of range", map[string]any{"questions": []any{valid("RollbackMarker")},
			"task_config": env.taskConfig([]any{titleMapping(3)})},
			http.StatusBadRequest, "INVALID_FORM", "task_config.mappings[0].question_index"},
		{"mapping without index", map[string]any{"questions": []any{valid("RollbackMarker")},
			"task_config": env.taskConfig([]any{map[string]any{"question_id": 1,
				"target": map[string]any{"type": "task_field", "field": "title"}}})},
			http.StatusBadRequest, "INVALID_FORM", "task_config.mappings[0].question_index"},
		{"mapping to a numeric question", map[string]any{"questions": []any{
			map[string]any{"type": "number", "title": "RollbackMarker"}},
			"task_config": env.taskConfig([]any{titleMapping(0)})},
			http.StatusBadRequest, "INVALID_FORM", "task_config.mappings[0].target"},
		{"status of another list", map[string]any{"questions": []any{valid("RollbackMarker")}, "task_config": foreignStatus},
			http.StatusBadRequest, "FORM_TASK_STATUS_NOT_IN_LIST", "task_config.task_defaults.status_id"},
		{"bad slug", map[string]any{"slug": "Not A Slug!", "questions": []any{valid("RollbackMarker")}},
			http.StatusBadRequest, "INVALID_FORM", "slug"},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			body := map[string]any{"name": "Rollback " + tcase.name, "slug": env.newSlug("rb")}
			for key, value := range tcase.body {
				body[key] = value
			}
			rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, body)
			fcExpectError(t, rec, tcase.status, tcase.code, tcase.field)
			if body["slug"] != "Not A Slug!" {
				var forms int64
				if err := tc.DB.Table("forms").Where("slug = ?", body["slug"]).Count(&forms).Error; err != nil || forms != 0 {
					t.Fatalf("form leaked after a failed create: %d %v", forms, err)
				}
			}
			var questions int64
			if err := tc.DB.Table("form_questions").Where("title = ?", "RollbackMarker").Count(&questions).Error; err != nil ||
				questions != 0 {
				t.Fatalf("questions leaked after a failed create: %d %v", questions, err)
			}
		})
	}

	slug := env.newSlug("dup")
	rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{"name": "First", "slug": slug})
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create: %d", rec.Code)
	}
	rec = request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{
		"name": "Second", "slug": slug, "questions": []any{valid("RollbackMarker")},
	})
	fcExpectError(t, rec, http.StatusConflict, "FORM_SLUG_TAKEN", "")
	var leaked int64
	if err := tc.DB.Table("form_questions").Where("title = ?", "RollbackMarker").Count(&leaked).Error; err != nil || leaked != 0 {
		t.Fatalf("questions leaked after slug conflict: %d %v", leaked, err)
	}

	rec = request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, map[string]any{
		"name": "Hundred", "slug": env.newSlug("hundred"), "questions": tooMany[:100],
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("100 questions must be accepted: %d: %s", rec.Code, rec.Body.String())
	}
	if got := dataObject(t, rec)["questions"].([]any); len(got) != 100 {
		t.Fatalf("created %d questions, want 100", len(got))
	}
}

func TestFormTemplates_CatalogIsLocalizedAndCreatesForms(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fccatalog")

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/form-templates", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous catalog: %d", rec.Code)
	}

	load := func(language string) []any {
		rec := requestWithHeader(t, tc, http.MethodGet, "/api/v1/form-templates", env.owner.token,
			"Accept-Language", language, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("catalog (%s): %d: %s", language, rec.Code, rec.Body.String())
		}
		return responseArray(t, rec)
	}
	english, russian := load("en-US,en;q=0.9"), load("ru-RU,ru;q=0.9")
	if len(english) != 5 || len(russian) != 5 {
		t.Fatalf("catalog sizes: %d %d", len(english), len(russian))
	}
	keys := map[string]bool{}
	for i := range english {
		en, ru := english[i].(map[string]any), russian[i].(map[string]any)
		keys[en["key"].(string)] = true
		if en["key"] != ru["key"] || en["name"] == ru["name"] {
			t.Fatalf("preset %v is not localized: %v / %v", en["key"], en["name"], ru["name"])
		}
	}
	for _, key := range []string{"feedback", "project_request", "order", "application", "it_request"} {
		if !keys[key] {
			t.Fatalf("catalog has no %q preset", key)
		}
	}
	if english[0].(map[string]any)["name"] != "Feedback" || russian[0].(map[string]any)["name"] != "Обратная связь" {
		t.Fatalf("unexpected first preset names: %v / %v", english[0], russian[0])
	}

	for _, catalog := range [][]any{english, russian} {
		for _, item := range catalog {
			preset := item.(map[string]any)
			body := map[string]any{
				"name": preset["name"], "description": preset["description"],
				"questions": preset["questions"], "slug": env.newSlug("preset"),
			}
			rec := request(t, tc.Router, http.MethodPost, env.formsURL, env.owner.token, body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("preset %v cannot be used as a create request: %d: %s", preset["key"], rec.Code, rec.Body.String())
			}
		}
	}
}
