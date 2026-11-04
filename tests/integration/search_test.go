package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/repository"
)

type searchEnv struct {
	tc       *TestContext
	owner    workspaceOwner
	guest    string
	outsider string
	listID   uint
	statusID uint
}

func searchCreate(t *testing.T, tc *TestContext, token, path string, body map[string]any) uint {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, path, token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s: %d: %s", path, rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func searchList(t *testing.T, tc *TestContext, token string, spaceID uint) (listID, statusID uint) {
	t.Helper()
	listID = searchCreate(t, tc, token, "/api/v1/spaces/"+itoa(spaceID)+"/lists", map[string]any{"name": "list"})
	statusID = searchCreate(t, tc, token, "/api/v1/lists/"+itoa(listID)+"/statuses",
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	return listID, statusID
}

func newSearchEnv(t *testing.T) *searchEnv {
	t.Helper()
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "srch")
	guestID, guestToken := newUser(t, tc, "srch-guest@example.test")
	_, outsiderToken := newUser(t, tc, "srch-outsider@example.test")
	addContact(t, tc, owner.token, guestID)
	rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": guestID, "role": "Guest"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add guest: %d: %s", rec.Code, rec.Body.String())
	}
	listID, statusID := searchList(t, tc, owner.token, owner.spaceID)
	return &searchEnv{tc: tc, owner: owner, guest: guestToken, outsider: outsiderToken, listID: listID, statusID: statusID}
}

func (e *searchEnv) task(t *testing.T, title, description string) uint {
	t.Helper()
	body := map[string]any{"title": title, "status_id": e.statusID}
	if description != "" {
		body["description"] = description
	}
	return searchCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(e.listID)+"/tasks", body)
}

func (e *searchEnv) comment(t *testing.T, taskID uint, body string) uint {
	t.Helper()
	return searchCreate(t, e.tc, e.owner.token, "/api/v1/tasks/"+itoa(taskID)+"/comments", map[string]any{"body": body})
}

func (e *searchEnv) item(t *testing.T, taskID uint, text string) uint {
	t.Helper()
	checklist := searchCreate(t, e.tc, e.owner.token, "/api/v1/tasks/"+itoa(taskID)+"/checklists", map[string]any{"name": "cl"})
	return searchCreate(t, e.tc, e.owner.token, "/api/v1/tasks/"+itoa(taskID)+"/checklists/"+itoa(checklist)+"/items", map[string]any{"text": text})
}

func (e *searchEnv) search(t *testing.T, token string, workspaceID uint, query string) (int, map[string]any) {
	t.Helper()
	if !strings.Contains(query, "limit=") {
		query += "&limit=100"
	}
	rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/workspaces/"+itoa(workspaceID)+"/search?"+query, token, nil)
	return rec.Code, decodeJSON(t, rec)
}

func (e *searchEnv) results(t *testing.T, query string) []map[string]any {
	t.Helper()
	code, body := e.search(t, e.owner.token, e.owner.workspaceID, query)
	if code != http.StatusOK {
		t.Fatalf("search %s: %d: %v", query, code, body)
	}
	items, _ := body["data"].([]any)
	out := make([]map[string]any, len(items))
	for i, item := range items {
		out[i] = item.(map[string]any)
	}
	return out
}

func (e *searchEnv) setUpdatedAt(t *testing.T, expr string, ids ...uint) {
	t.Helper()
	err := e.tc.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL session_replication_role = replica").Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE tasks SET updated_at = "+expr+" WHERE id IN ?", ids).Error
	})
	if err != nil {
		t.Fatal(err)
	}
}

func q(text string) string { return "q=" + url.QueryEscape(text) }

func resultIDs(results []map[string]any, kind string) []float64 {
	var ids []float64
	for _, r := range results {
		if kind == "" || r["type"] == kind {
			ids = append(ids, r["id"].(float64))
		}
	}
	return ids
}

func hasID(results []map[string]any, kind string, id uint) bool {
	for _, got := range resultIDs(results, kind) {
		if uint(got) == id {
			return true
		}
	}
	return false
}

func TestSearch_FindsAllTypesWithContextAndSnippet(t *testing.T) {
	e := newSearchEnv(t)
	task := e.task(t, "Release checklist", "")
	comment := e.comment(t, task, "<p>The <b>release</b> is blocked</p>")
	item := e.item(t, task, "Tag the release")

	results := e.results(t, q("release"))
	if len(results) != 3 {
		t.Fatalf("results = %v", results)
	}
	for _, kind := range []struct {
		name string
		id   uint
	}{{"task", task}, {"comment", comment}, {"checklist_item", item}} {
		if !hasID(results, kind.name, kind.id) {
			t.Fatalf("missing %s %d in %v", kind.name, kind.id, results)
		}
	}
	for _, r := range results {
		if uint(r["task_id"].(float64)) != task || uint(r["list_id"].(float64)) != e.listID ||
			uint(r["space_id"].(float64)) != e.owner.spaceID || r["task_title"] != "Release checklist" {
			t.Fatalf("bad context: %v", r)
		}
		snippet := r["snippet"].(map[string]any)
		if strings.ContainsAny(snippet["text"].(string), "<>") {
			t.Fatalf("raw HTML leaked: %v", snippet)
		}
		matches := snippet["matches"].([]any)
		if len(matches) == 0 {
			t.Fatalf("no matches: %v", r)
		}
		m := matches[0].(map[string]any)
		runes := []rune(snippet["text"].(string))
		if got := strings.ToLower(string(runes[int(m["start"].(float64)):int(m["end"].(float64))])); got != "release" {
			t.Fatalf("offsets select %q in %v", got, snippet)
		}
		if r["score"].(float64) <= 0 {
			t.Fatalf("score = %v", r["score"])
		}
	}
}

func TestSearch_RankingOrderAndTieBreak(t *testing.T) {
	e := newSearchEnv(t)
	contains := e.task(t, "Prepare the invoice batch", "")
	prefix := e.task(t, "Invoice review", "")
	exact := e.task(t, "invoice", "")
	described := e.task(t, "Finance", "all about the invoice")
	results := e.results(t, q("invoice"))
	ids := resultIDs(results, "task")
	want := []uint{exact, prefix, contains, described}
	if len(ids) != 4 {
		t.Fatalf("results = %v", results)
	}
	for i, id := range want {
		if uint(ids[i]) != id {
			t.Fatalf("order = %v, want %v", ids, want)
		}
	}
	if results[3]["field"] != "description" || results[0]["field"] != "title" {
		t.Fatalf("fields = %v / %v", results[0]["field"], results[3]["field"])
	}

	a := e.task(t, "tie alpha", "")
	b := e.task(t, "tie bravo", "")
	e.setUpdatedAt(t, "'2020-01-01'", a, b)
	first := resultIDs(e.results(t, q("tie")), "task")
	second := resultIDs(e.results(t, q("tie")), "task")
	if len(first) != 2 || uint(first[0]) != a || uint(first[1]) != b || first[0] != second[0] {
		t.Fatalf("tie order = %v / %v", first, second)
	}
}

func TestSearch_RecencyBreaksScoreTies(t *testing.T) {
	e := newSearchEnv(t)
	older := e.task(t, "weekly sync notes", "")
	newer := e.task(t, "weekly sync agenda", "")
	e.setUpdatedAt(t, "now() - interval '2 days'", older)
	ids := resultIDs(e.results(t, q("weekly sync")), "task")
	if len(ids) != 2 || uint(ids[0]) != newer || uint(ids[1]) != older {
		t.Fatalf("ids = %v", ids)
	}
}

func TestSearch_ToleratesTypos(t *testing.T) {
	e := newSearchEnv(t)
	task := e.task(t, "Fix the deployment pipeline", "")
	e.task(t, "Buy coffee", "")
	results := e.results(t, q("deploymnt"))
	if len(results) != 1 || !hasID(results, "task", task) {
		t.Fatalf("results = %v", results)
	}
	matches := results[0]["snippet"].(map[string]any)["matches"].([]any)
	if len(matches) != 1 {
		t.Fatalf("matches = %v", matches)
	}
	if got := e.results(t, q("zzzzqqq")); len(got) != 0 {
		t.Fatalf("unrelated query matched %v", got)
	}
}

func TestSearch_WildcardsAreLiteral(t *testing.T) {
	e := newSearchEnv(t)
	percent := e.task(t, "Reach 100% coverage", "")
	e.task(t, "Reach 100 points", "")
	under := e.task(t, "rename snake_case fields", "")
	e.task(t, "rename snakeXcase fields", "")
	slash := e.task(t, `path C:\temp folder`, "")

	for query, want := range map[string]uint{"100%": percent, "snake_case": under, `C:\temp`: slash} {
		results := e.results(t, q(query))
		if len(results) == 0 || !hasID(results[:1], "task", want) || results[0]["score"].(float64) != 0.8 {
			t.Fatalf("q=%q results = %v, want %d first with a literal score", query, results, want)
		}
		for _, r := range results[1:] {
			if r["score"].(float64) >= 0.8 {
				t.Fatalf("q=%q: wildcard matched %v", query, r)
			}
		}
	}
	for _, query := range []string{"%%", "__", `\\`, "%_"} {
		if results := e.results(t, q(query)); len(results) != 0 {
			t.Fatalf("q=%q matched %v", query, results)
		}
	}
}

func TestSearch_CyrillicMatchingAndRuneOffsets(t *testing.T) {
	e := newSearchEnv(t)
	task := e.task(t, "Подготовить ОТЧЁТ за квартал", "")
	comment := e.comment(t, task, "Не забудьте отправить отчёт бухгалтерии, отчёт готов")
	e.task(t, "Купить кофе", "")

	results := e.results(t, q("отчёт"))
	if !hasID(results, "task", task) || !hasID(results, "comment", comment) || len(results) != 2 {
		t.Fatalf("results = %v", results)
	}
	for _, r := range results {
		snippet := r["snippet"].(map[string]any)
		runes := []rune(snippet["text"].(string))
		matches := snippet["matches"].([]any)
		if r["type"] == "comment" && len(matches) != 2 {
			t.Fatalf("comment matches = %v", matches)
		}
		for _, raw := range matches {
			m := raw.(map[string]any)
			got := strings.ToLower(string(runes[int(m["start"].(float64)):int(m["end"].(float64))]))
			if got != "отчёт" {
				t.Fatalf("offsets select %q in %q", got, string(runes))
			}
		}
	}
	typo := e.results(t, q("квартл"))
	if !hasID(typo, "task", task) {
		t.Fatalf("typo search = %v", typo)
	}
}

func TestSearch_TypeFilterAndSpaceFilter(t *testing.T) {
	e := newSearchEnv(t)
	task := e.task(t, "Budget review", "")
	comment := e.comment(t, task, "budget approved")
	item := e.item(t, task, "check budget")

	for types, want := range map[string]string{"tasks": "task", "comments": "comment", "checklist_items": "checklist_item"} {
		results := e.results(t, q("budget")+"&types="+types)
		if len(results) != 1 || results[0]["type"] != want {
			t.Fatalf("types=%s results = %v", types, results)
		}
	}
	both := e.results(t, q("budget")+"&types=tasks,comments,tasks")
	if len(both) != 2 || hasID(both, "checklist_item", item) || !hasID(both, "comment", comment) {
		t.Fatalf("both = %v", both)
	}

	secondSpace := searchCreate(t, e.tc, e.owner.token, "/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/spaces", map[string]any{"name": "second"})
	list, status := searchList(t, e.tc, e.owner.token, secondSpace)
	otherTask := searchCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(list)+"/tasks", map[string]any{"title": "Budget in space two", "status_id": status})

	if all := e.results(t, q("budget")+"&types=tasks"); len(all) != 2 {
		t.Fatalf("all = %v", all)
	}
	first := e.results(t, q("budget")+"&types=tasks&space_id="+itoa(e.owner.spaceID))
	second := e.results(t, q("budget")+"&space_id="+itoa(secondSpace))
	if len(first) != 1 || !hasID(first, "task", task) || len(second) != 1 || !hasID(second, "task", otherTask) {
		t.Fatalf("first = %v second = %v", first, second)
	}
}

func TestSearch_IsolatedPerWorkspace(t *testing.T) {
	e := newSearchEnv(t)
	task := e.task(t, "Secret roadmap", "")
	e.comment(t, task, "roadmap comment")

	other := newWorkspaceOwner(t, e.tc, "srch-other")
	list, status := searchList(t, e.tc, other.token, other.spaceID)
	otherTask := searchCreate(t, e.tc, other.token, "/api/v1/lists/"+itoa(list)+"/tasks", map[string]any{"title": "Other roadmap", "status_id": status})

	code, body := e.search(t, other.token, other.workspaceID, q("roadmap"))
	results := body["data"].([]any)
	if code != http.StatusOK || len(results) != 1 || uint(results[0].(map[string]any)["id"].(float64)) != otherTask {
		t.Fatalf("other workspace: %d %v", code, body)
	}

	if code, _ := e.search(t, e.outsider, e.owner.workspaceID, q("roadmap")); code != http.StatusForbidden {
		t.Fatalf("outsider got %d, want 403", code)
	}
	if code, _ := e.search(t, other.token, e.owner.workspaceID, q("roadmap")); code != http.StatusForbidden {
		t.Fatalf("foreign owner got %d, want 403", code)
	}

	member := newUserInTwoWorkspaces(t, e, other)
	for _, ws := range []uint{e.owner.workspaceID, other.workspaceID} {
		code, body := e.search(t, member, ws, q("roadmap"))
		if code != http.StatusOK {
			t.Fatalf("member in ws %d: %d", ws, code)
		}
		for _, r := range body["data"].([]any) {
			spaceID := uint(r.(map[string]any)["space_id"].(float64))
			want := e.owner.spaceID
			if ws == other.workspaceID {
				want = other.spaceID
			}
			if spaceID != want {
				t.Fatalf("workspace %d leaked space %d", ws, spaceID)
			}
		}
	}

	if code, body := e.search(t, e.owner.token, e.owner.workspaceID, q("roadmap")+"&space_id="+itoa(other.spaceID)); code != http.StatusOK || len(body["data"].([]any)) != 0 {
		t.Fatalf("foreign space_id: %d %v", code, body)
	}
}

func newUserInTwoWorkspaces(t *testing.T, e *searchEnv, other workspaceOwner) string {
	t.Helper()
	id, token := newUser(t, e.tc, "srch-both@example.test")
	for _, owner := range []workspaceOwner{e.owner, other} {
		addContact(t, e.tc, owner.token, id)
		rec := request(t, e.tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
			map[string]any{"user_id": id, "role": "Member"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("add member: %d: %s", rec.Code, rec.Body.String())
		}
	}
	return token
}

func TestSearch_GuestMaySearchButNeverSeesEmails(t *testing.T) {
	e := newSearchEnv(t)
	task := e.task(t, "Guest visible task", "")
	e.comment(t, task, "write to boss@example.test please")

	code, body := e.search(t, e.guest, e.owner.workspaceID, q("visible"))
	if code != http.StatusOK || len(body["data"].([]any)) != 1 {
		t.Fatalf("guest search: %d %v", code, body)
	}
	for _, raw := range body["data"].([]any) {
		for key := range raw.(map[string]any) {
			if strings.Contains(key, "email") || strings.Contains(key, "author") || strings.Contains(key, "user") {
				t.Fatalf("unexpected field %q", key)
			}
		}
	}
	if results := e.results(t, q("srch-owner@example")); len(results) != 0 {
		t.Fatalf("user e-mail matched: %v", results)
	}
}

func TestSearch_ExcludesDeletedData(t *testing.T) {
	e := newSearchEnv(t)
	liveTask := e.task(t, "zebra live", "")
	e.comment(t, liveTask, "zebra comment live")
	e.item(t, liveTask, "zebra item live")

	deletedTask := e.task(t, "zebra deleted task", "")
	e.comment(t, deletedTask, "zebra comment of deleted task")
	e.item(t, deletedTask, "zebra item of deleted task")
	deletedComment := e.comment(t, liveTask, "zebra deleted comment")

	if got := e.results(t, q("zebra")); len(got) != 7 {
		t.Fatalf("before deletion: %d results", len(got))
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(deletedTask), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete task: %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(liveTask)+"/comments/"+itoa(deletedComment), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete comment: %d", rec.Code)
	}
	results := e.results(t, q("zebra"))
	if len(results) != 3 {
		t.Fatalf("after task and comment deletion: %v", results)
	}
	for _, r := range results {
		if uint(r["task_id"].(float64)) != liveTask {
			t.Fatalf("deleted data leaked: %v", r)
		}
	}

	folderList := func(label string) (folderID, listID uint) {
		folderID = searchCreate(t, e.tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/folders", map[string]any{"name": label})
		listID = searchCreate(t, e.tc, e.owner.token, "/api/v1/folders/"+itoa(folderID)+"/lists", map[string]any{"name": label})
		return folderID, listID
	}
	seedList := func(listID uint, title string) {
		status := searchCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(listID)+"/statuses",
			map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
		task := searchCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(listID)+"/tasks", map[string]any{"title": title, "status_id": status})
		e.comment(t, task, title+" comment")
		e.item(t, task, title+" item")
	}

	folderID, folderListID := folderList("doomed")
	seedList(folderListID, "zebra in folder")
	childFolder := uint(0)
	if err := e.tc.DB.Raw("INSERT INTO folders (name, space_id, parent_id) VALUES ('child', ?, ?) RETURNING id", e.owner.spaceID, folderID).Scan(&childFolder).Error; err != nil {
		t.Fatal(err)
	}
	var childList uint
	if err := e.tc.DB.Raw("INSERT INTO lists (name, space_id, folder_id) VALUES ('nested', ?, ?) RETURNING id", e.owner.spaceID, childFolder).Scan(&childList).Error; err != nil {
		t.Fatal(err)
	}
	seedList(childList, "zebra nested folder")

	deletedList := searchCreate(t, e.tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/lists", map[string]any{"name": "gone"})
	seedList(deletedList, "zebra in list")

	secondSpace := searchCreate(t, e.tc, e.owner.token, "/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/spaces", map[string]any{"name": "gone space"})
	spaceList, _ := searchList(t, e.tc, e.owner.token, secondSpace)
	seedList(spaceList, "zebra in space")

	if got := e.results(t, q("zebra")); len(got) != 3+4*3 {
		t.Fatalf("before container deletion: %d results: %v", len(got), got)
	}
	for _, path := range []string{
		"/api/v1/folders/" + itoa(folderID), "/api/v1/lists/" + itoa(deletedList), "/api/v1/spaces/" + itoa(secondSpace),
	} {
		if rec := request(t, e.tc.Router, http.MethodDelete, path, e.owner.token, nil); rec.Code != http.StatusOK {
			t.Fatalf("DELETE %s: %d: %s", path, rec.Code, rec.Body.String())
		}
	}
	results = e.results(t, q("zebra"))
	if len(results) != 3 {
		t.Fatalf("after container deletion: %v", results)
	}
	if code, body := e.search(t, e.owner.token, e.owner.workspaceID, q("zebra")); code != http.StatusOK || int(body["pagination"].(map[string]any)["total"].(float64)) != 3 {
		t.Fatalf("total after deletion: %v", body["pagination"])
	}
}

func TestSearch_PaginationTotals(t *testing.T) {
	e := newSearchEnv(t)
	for i := 0; i < 5; i++ {
		e.task(t, "pagination item "+itoa(uint(i)), "")
	}
	seen := map[float64]bool{}
	for page := 1; page <= 3; page++ {
		code, body := e.search(t, e.owner.token, e.owner.workspaceID, q("pagination")+"&limit=2&page="+itoa(uint(page)))
		if code != http.StatusOK {
			t.Fatalf("page %d: %d", page, code)
		}
		p := body["pagination"].(map[string]any)
		if p["total"].(float64) != 5 || p["pages"].(float64) != 3 || p["page"].(float64) != float64(page) || p["limit"].(float64) != 2 {
			t.Fatalf("pagination = %v", p)
		}
		items := body["data"].([]any)
		want := 2
		if page == 3 {
			want = 1
		}
		if len(items) != want {
			t.Fatalf("page %d has %d items", page, len(items))
		}
		for _, it := range items {
			id := it.(map[string]any)["id"].(float64)
			if seen[id] {
				t.Fatalf("duplicate id %v across pages", id)
			}
			seen[id] = true
		}
	}
	code, body := e.search(t, e.owner.token, e.owner.workspaceID, q("pagination")+"&limit=2&page=9")
	if code != http.StatusOK || len(body["data"].([]any)) != 0 || body["pagination"].(map[string]any)["total"].(float64) != 5 {
		t.Fatalf("beyond last page: %d %v", code, body)
	}
}

func TestSearch_ValidationErrors(t *testing.T) {
	e := newSearchEnv(t)
	cases := map[string]string{
		"":                              "QUERY_REQUIRED",
		"q=":                            "QUERY_REQUIRED",
		"q=%20%20":                      "QUERY_REQUIRED",
		"q=a":                           "QUERY_TOO_SHORT",
		"q=%20a%20":                     "QUERY_TOO_SHORT",
		"q=" + strings.Repeat("x", 101): "QUERY_TOO_LONG",
		"q=" + strings.Repeat("ж", 101): "QUERY_TOO_LONG",
		"q=abc&types=tasks,bogus":       "INVALID_SEARCH_TYPES",
		"q=abc&types=task":              "INVALID_SEARCH_TYPES",
		"q=abc&types=tasks,":            "INVALID_SEARCH_TYPES",
		"q=abc&space_id=zero":           "INVALID_SPACE_ID",
		"q=abc&space_id=0":              "INVALID_SPACE_ID",
		"q=abc&page=0":                  "INVALID_PAGINATION",
		"q=abc&limit=101":               "INVALID_PAGINATION",
		"q=ab%00cd":                     "INVALID_QUERY",
		"q=%ff%fe":                      "INVALID_QUERY",
	}
	for query, code := range cases {
		status, body := e.search(t, e.owner.token, e.owner.workspaceID, query)
		errObj, _ := body["error"].(map[string]any)
		if status != http.StatusBadRequest || errObj == nil || errObj["code"] != code {
			t.Fatalf("query %q: %d %v, want 400 %s", query, status, body, code)
		}
	}
	for _, ok := range []string{"q=ab", "q=" + strings.Repeat("ж", 100), "q=abc&types=tasks,comments,checklist_items"} {
		if status, body := e.search(t, e.owner.token, e.owner.workspaceID, ok); status != http.StatusOK {
			t.Fatalf("query %q: %d %v", ok, status, body)
		}
	}
	rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/workspaces/abc/search?q=abc", e.owner.token, nil)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("bad workspace id: %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/search?q=abc", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
}

func TestSearch_TrigramIndexesServeTheQueries(t *testing.T) {
	e := newSearchEnv(t)
	task := e.task(t, "seed", "")
	checklist := searchCreate(t, e.tc, e.owner.token, "/api/v1/tasks/"+itoa(task)+"/checklists", map[string]any{"name": "cl"})
	db := e.tc.DB
	ws, owner := itoa(e.owner.workspaceID), itoa(e.owner.userID)
	seed := []string{
		"INSERT INTO spaces (name, workspace_id, owner_id) SELECT 'sp' || g, " + ws + ", " + owner + " FROM generate_series(1, 60) g",
		"INSERT INTO lists (name, space_id) SELECT 'l' || id, id FROM spaces WHERE workspace_id = " + ws + " AND id <> " + itoa(e.owner.spaceID),
		"INSERT INTO tasks (space_id, list_id, status_id, title, description, user_id) SELECT l.space_id, l.id, " + itoa(e.statusID) + ", md5((g + l.id * 1000)::text) || ' ' || md5((g * 7 + l.id)::text), md5((g * 13 + l.id)::text) || ' filler words here', " + owner + " FROM lists l, generate_series(1, 500) g WHERE l.id <> " + itoa(e.listID),
		"INSERT INTO task_comments (task_id, user_id, body) SELECT id, " + owner + ", md5(id::text) || ' ' || md5((id * 3)::text) FROM tasks",
		"INSERT INTO task_checklist_items (checklist_id, text) SELECT " + itoa(checklist) + ", md5(id::text) || ' ' || md5((id * 5)::text) FROM tasks",
		"VACUUM ANALYZE spaces", "VACUUM ANALYZE lists", "VACUUM ANALYZE tasks", "VACUUM ANALYZE task_comments", "VACUUM ANALYZE task_checklist_items",
	}
	for _, stmt := range seed {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	querySQL, queryArgs := repository.SearchQuerySQL(domain.SearchParams{
		WorkspaceID: e.owner.workspaceID, Query: "abcdef123", Limit: 10,
	})
	sql := "EXPLAIN " + querySQL
	for _, mode := range []string{"natural", "bitmap"} {
		tx := db.Begin()
		if mode == "bitmap" {
			tx.Exec("SET LOCAL enable_seqscan = off")
		}
		var lines []string
		if err := tx.Raw(sql, queryArgs...).Scan(&lines).Error; err != nil {
			tx.Rollback()
			t.Fatalf("explain: %v", err)
		}
		tx.Rollback()
		plan := strings.Join(lines, "\n")
		for _, index := range []string{"idx_tasks_title_trgm", "idx_tasks_description_trgm", "idx_task_comments_body_trgm", "idx_task_checklist_items_text_trgm"} {
			if !strings.Contains(plan, index) {
				t.Fatalf("%s plan does not use %s:\n%s", mode, index, plan)
			}
		}
	}
}
