package integration

import (
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"testing"

	"rabbit-hole-server/internal/collab"
)

type notebookEnv struct {
	tc       *TestContext
	owner    workspaceOwner
	member   string
	memberID uint
	guest    string
	outsider string
}

func newNotebookEnv(t *testing.T) *notebookEnv {
	t.Helper()
	tc := newTestContext(t)
	e := &notebookEnv{tc: tc, owner: newWorkspaceOwner(t, tc, "nb")}
	for _, u := range []struct {
		name, role string
		id         *uint
		token      *string
	}{{"member", "Member", &e.memberID, &e.member}, {"guest", "Guest", new(uint), &e.guest}} {
		id, token := newUser(t, tc, "nb-"+u.name+"@example.test")
		addContact(t, tc, e.owner.token, id)
		if rec := request(t, tc.Router, http.MethodPost, membersPath(e.owner.workspaceID, ""), e.owner.token,
			map[string]any{"user_id": id, "role": u.role}); rec.Code != http.StatusCreated {
			t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
		}
		*u.id, *u.token = id, token
	}
	_, e.outsider = newUser(t, tc, "nb-outsider@example.test")
	return e
}

func (e *notebookEnv) call(t *testing.T, method, path, token string, body any, want int) map[string]any {
	t.Helper()
	rec := request(t, e.tc.Router, method, path, token, body)
	if rec.Code != want {
		t.Fatalf("%s %s: %d (want %d): %s", method, path, rec.Code, want, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	return decodeJSON(t, rec)
}

func (e *notebookEnv) notebooks() string {
	return "/api/v1/workspaces/" + itoa(e.owner.workspaceID) + "/notebooks"
}

func (e *notebookEnv) newNotebook(t *testing.T, name string) uint {
	t.Helper()
	out := e.call(t, http.MethodPost, e.notebooks(), e.owner.token, map[string]any{"name": name}, http.StatusCreated)
	return uint(out["data"].(map[string]any)["id"].(float64))
}

func (e *notebookEnv) newDocument(t *testing.T, notebookID uint, title string) uint {
	t.Helper()
	out := e.call(t, http.MethodPost, "/api/v1/notebooks/"+itoa(notebookID)+"/documents", e.member,
		map[string]any{"title": title}, http.StatusCreated)
	return uint(out["data"].(map[string]any)["id"].(float64))
}

// validState is a complete document the server accepts as a snapshot.
func validState() string { return base64.StdEncoding.EncodeToString(collab.InitialState()) }

func b64(raw string) string { return base64.StdEncoding.EncodeToString([]byte(raw)) }

func TestNotebooks_RolesAndValidation(t *testing.T) {
	e := newNotebookEnv(t)

	// Owner and member create; guest only reads; an outsider sees nothing.
	e.call(t, http.MethodPost, e.notebooks(), e.guest, map[string]any{"name": "x"}, http.StatusForbidden)
	e.call(t, http.MethodGet, e.notebooks(), e.outsider, nil, http.StatusForbidden)
	e.call(t, http.MethodGet, e.notebooks(), e.guest, nil, http.StatusOK)

	id := e.newNotebook(t, "  Lecture notes  ")
	one := e.call(t, http.MethodGet, "/api/v1/notebooks/"+itoa(id), e.guest, nil, http.StatusOK)["data"].(map[string]any)
	if one["name"] != "Lecture notes" || one["document_count"].(float64) != 0 {
		t.Fatalf("unexpected notebook: %v", one)
	}
	if created, _ := one["created_by"].(map[string]any); created == nil || uint(created["id"].(float64)) != e.owner.userID {
		t.Fatalf("created_by should be the owner: %v", one["created_by"])
	}

	for _, body := range []map[string]any{
		{"name": ""},
		{"name": "   "},
		{"name": strings.Repeat("я", 256)},
		{"name": "ok", "description": strings.Repeat("d", 2001)},
	} {
		out := e.call(t, http.MethodPost, e.notebooks(), e.owner.token, body, http.StatusBadRequest)
		if out["error"].(map[string]any)["code"] != "VALIDATION_FAILED" {
			t.Fatalf("want VALIDATION_FAILED for %v: %v", body, out)
		}
	}

	e.call(t, http.MethodPatch, "/api/v1/notebooks/"+itoa(id), e.guest, map[string]any{"name": "y"}, http.StatusForbidden)
	patched := e.call(t, http.MethodPatch, "/api/v1/notebooks/"+itoa(id), e.member,
		map[string]any{"name": "Renamed", "description": "about"}, http.StatusOK)["data"].(map[string]any)
	if patched["name"] != "Renamed" || patched["description"] != "about" {
		t.Fatalf("patch: %v", patched)
	}
	cleared := e.call(t, http.MethodPatch, "/api/v1/notebooks/"+itoa(id), e.member,
		map[string]any{"description": nil}, http.StatusOK)["data"].(map[string]any)
	if cleared["description"] != nil || cleared["name"] != "Renamed" {
		t.Fatalf("null description should clear it and keep the name: %v", cleared)
	}

	// Members may delete documents' notebook; guests may not.
	e.call(t, http.MethodDelete, "/api/v1/notebooks/"+itoa(id), e.guest, nil, http.StatusForbidden)
	e.call(t, http.MethodDelete, "/api/v1/notebooks/"+itoa(id), e.member, nil, http.StatusOK)
	e.call(t, http.MethodGet, "/api/v1/notebooks/"+itoa(id), e.owner.token, nil, http.StatusForbidden)
}

func TestNotebooks_ListIsOrderedAndCountsLiveDocuments(t *testing.T) {
	e := newNotebookEnv(t)
	first := e.newNotebook(t, "first")
	second := e.newNotebook(t, "second")
	doc := e.newDocument(t, first, "a")
	e.newDocument(t, first, "b")
	e.call(t, http.MethodDelete, "/api/v1/documents/"+itoa(doc), e.member, nil, http.StatusOK)

	list := e.call(t, http.MethodGet, e.notebooks(), e.guest, nil, http.StatusOK)["data"].([]any)
	if len(list) != 2 || uint(list[0].(map[string]any)["id"].(float64)) != first || uint(list[1].(map[string]any)["id"].(float64)) != second {
		t.Fatalf("notebooks should be listed in creation order: %v", list)
	}
	if list[0].(map[string]any)["document_count"].(float64) != 1 {
		t.Fatalf("a deleted document must not be counted: %v", list[0])
	}

	// Moving a notebook to the front is a position change.
	e.call(t, http.MethodPatch, "/api/v1/notebooks/"+itoa(second), e.owner.token, map[string]any{"position": 1}, http.StatusOK)
	list = e.call(t, http.MethodGet, e.notebooks(), e.owner.token, nil, http.StatusOK)["data"].([]any)
	if uint(list[0].(map[string]any)["id"].(float64)) != second {
		t.Fatalf("position should reorder notebooks: %v", list)
	}
}

func TestDocuments_LifecycleAndMove(t *testing.T) {
	e := newNotebookEnv(t)
	a := e.newNotebook(t, "a")
	b := e.newNotebook(t, "b")
	doc := e.newDocument(t, a, "Plan")
	path := "/api/v1/documents/" + itoa(doc)

	one := e.call(t, http.MethodGet, path, e.guest, nil, http.StatusOK)["data"].(map[string]any)
	if one["title"] != "Plan" || one["last_seq"].(float64) != 0 || one["last_edited_by"] != nil {
		t.Fatalf("new document: %v", one)
	}
	if uint(one["created_by"].(map[string]any)["id"].(float64)) != e.memberID {
		t.Fatalf("created_by should be the member: %v", one["created_by"])
	}

	e.call(t, http.MethodPost, "/api/v1/notebooks/"+itoa(a)+"/documents", e.guest, map[string]any{"title": "x"}, http.StatusForbidden)
	e.call(t, http.MethodPost, "/api/v1/notebooks/"+itoa(a)+"/documents", e.member, map[string]any{"title": " "}, http.StatusBadRequest)
	e.call(t, http.MethodPatch, path, e.guest, map[string]any{"title": "x"}, http.StatusForbidden)
	renamed := e.call(t, http.MethodPatch, path, e.member, map[string]any{"title": "Plan v2"}, http.StatusOK)["data"].(map[string]any)
	if renamed["title"] != "Plan v2" {
		t.Fatalf("rename: %v", renamed)
	}

	moved := e.call(t, http.MethodPatch, path, e.member, map[string]any{"notebook_id": b}, http.StatusOK)["data"].(map[string]any)
	if uint(moved["notebook_id"].(float64)) != b {
		t.Fatalf("move: %v", moved)
	}
	inA := e.call(t, http.MethodGet, "/api/v1/notebooks/"+itoa(a)+"/documents", e.guest, nil, http.StatusOK)
	inB := e.call(t, http.MethodGet, "/api/v1/notebooks/"+itoa(b)+"/documents", e.guest, nil, http.StatusOK)
	if len(inA["data"].([]any)) != 0 || len(inB["data"].([]any)) != 1 {
		t.Fatalf("move should change the listings: a=%v b=%v", inA["data"], inB["data"])
	}

	// A notebook of another workspace is not a valid target.
	other := newWorkspaceOwner(t, e.tc, "nb-other")
	foreign := mustCreate(t, e.tc, other.token, "/api/v1/workspaces/"+itoa(other.workspaceID)+"/notebooks", map[string]any{"name": "foreign"})
	out := e.call(t, http.MethodPatch, path, e.member, map[string]any{"notebook_id": foreign}, http.StatusNotFound)
	if out["error"].(map[string]any)["code"] != "NOTEBOOK_NOT_FOUND" {
		t.Fatalf("foreign notebook target: %v", out)
	}

	e.call(t, http.MethodDelete, path, e.guest, nil, http.StatusForbidden)
	e.call(t, http.MethodDelete, path, e.member, nil, http.StatusOK)
	e.call(t, http.MethodGet, path, e.owner.token, nil, http.StatusForbidden)
}

func TestDocuments_ListPaginates(t *testing.T) {
	e := newNotebookEnv(t)
	nb := e.newNotebook(t, "many")
	for _, title := range []string{"1", "2", "3"} {
		e.newDocument(t, nb, title)
	}
	out := e.call(t, http.MethodGet, "/api/v1/notebooks/"+itoa(nb)+"/documents?limit=2&page=2", e.guest, nil, http.StatusOK)
	items := out["data"].([]any)
	page := out["pagination"].(map[string]any)
	if len(items) != 1 || items[0].(map[string]any)["title"] != "3" || page["total"].(float64) != 3 || page["pages"].(float64) != 2 {
		t.Fatalf("page 2: %v %v", items, page)
	}
	e.call(t, http.MethodGet, "/api/v1/notebooks/"+itoa(nb)+"/documents?limit=0", e.guest, nil, http.StatusBadRequest)
}

func TestDocuments_StartWithTheInitialState(t *testing.T) {
	e := newNotebookEnv(t)
	doc := e.newDocument(t, e.newNotebook(t, "n"), "d")
	state := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/state", e.guest, nil, http.StatusOK)["data"].(map[string]any)

	raw, err := base64.StdEncoding.DecodeString(state["snapshot"].(string))
	if err != nil || len(raw) < 100 {
		t.Fatalf("a new document must start with the server-created Yjs state: %v (%d bytes)", err, len(raw))
	}
	if state["snapshot_seq"].(float64) != 0 || state["last_seq"].(float64) != 0 || len(state["updates"].([]any)) != 0 || state["format"].(float64) != 1 {
		t.Fatalf("state: %v", state)
	}
	e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/state", e.outsider, nil, http.StatusForbidden)
}

func TestDocuments_UpdatesAreOrderedAndAttributed(t *testing.T) {
	e := newNotebookEnv(t)
	doc := e.newDocument(t, e.newNotebook(t, "n"), "d")
	updates := "/api/v1/documents/" + itoa(doc) + "/updates"

	first := e.call(t, http.MethodPost, updates, e.member,
		map[string]any{"update": b64("one"), "touched": []string{"block-1", "block-2", "block-1"}}, http.StatusCreated)
	if first["data"].(map[string]any)["seq"].(float64) != 1 {
		t.Fatalf("first update: %v", first)
	}
	e.call(t, http.MethodPost, updates, e.owner.token,
		map[string]any{"update": b64("two"), "touched": []string{"block-2"}}, http.StatusCreated)

	e.call(t, http.MethodPost, updates, e.guest, map[string]any{"update": b64("x")}, http.StatusForbidden)
	e.call(t, http.MethodPost, updates, e.outsider, map[string]any{"update": b64("x")}, http.StatusForbidden)

	state := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/state", e.guest, nil, http.StatusOK)["data"].(map[string]any)
	list := state["updates"].([]any)
	if len(list) != 2 || state["last_seq"].(float64) != 2 {
		t.Fatalf("state after two updates: %v", state)
	}
	if got, _ := base64.StdEncoding.DecodeString(list[0].(map[string]any)["update"].(string)); string(got) != "one" {
		t.Fatalf("updates must keep their bytes and order: %v", list)
	}
	if uint(list[1].(map[string]any)["author"].(map[string]any)["id"].(float64)) != e.owner.userID {
		t.Fatalf("author of update 2 should be the owner: %v", list[1])
	}

	// Who created block-1 and who edited block-2 last.
	activity := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/blocks/activity", e.guest, nil, http.StatusOK)["data"].([]any)
	byBlock := map[string]map[string]any{}
	for _, item := range activity {
		byBlock[item.(map[string]any)["block_id"].(string)] = item.(map[string]any)
	}
	if len(byBlock) != 2 {
		t.Fatalf("a duplicated block id must be recorded once: %v", activity)
	}
	id := func(item map[string]any, key string) uint { return uint(item[key].(map[string]any)["id"].(float64)) }
	if id(byBlock["block-1"], "created_by") != e.memberID || id(byBlock["block-1"], "last_edited_by") != e.memberID {
		t.Fatalf("block-1 belongs to the member: %v", byBlock["block-1"])
	}
	if id(byBlock["block-2"], "created_by") != e.memberID || id(byBlock["block-2"], "last_edited_by") != e.owner.userID {
		t.Fatalf("block-2 was created by the member and last edited by the owner: %v", byBlock["block-2"])
	}
	one := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/blocks/activity?block_id=block-2", e.guest, nil, http.StatusOK)["data"].([]any)
	if len(one) != 1 {
		t.Fatalf("block_id filter: %v", one)
	}

	document := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc), e.guest, nil, http.StatusOK)["data"].(map[string]any)
	if document["last_seq"].(float64) != 2 || uint(document["last_edited_by"].(map[string]any)["id"].(float64)) != e.owner.userID {
		t.Fatalf("the document should show the last editor: %v", document)
	}
}

func TestDocuments_UpdateInputIsValidated(t *testing.T) {
	e := newNotebookEnv(t)
	doc := e.newDocument(t, e.newNotebook(t, "n"), "d")
	updates := "/api/v1/documents/" + itoa(doc) + "/updates"

	e.call(t, http.MethodPost, updates, e.member, map[string]any{"touched": []string{"a"}}, http.StatusBadRequest)
	e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": "not base64!!"}, http.StatusBadRequest)
	e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": b64("x"), "touched": []string{""}}, http.StatusBadRequest)
	e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": b64("x"), "touched": []string{strings.Repeat("b", 101)}}, http.StatusBadRequest)
	tooMany := make([]string, 201)
	for i := range tooMany {
		tooMany[i] = "b" + itoa(uint(i))
	}
	e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": b64("x"), "touched": tooMany}, http.StatusBadRequest)

	big := strings.Repeat("a", 512*1024+1)
	out := e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": b64(big)}, http.StatusRequestEntityTooLarge)
	if out["error"].(map[string]any)["code"] != "DOCUMENT_UPDATE_TOO_LARGE" {
		t.Fatalf("oversized update: %v", out)
	}

	state := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/state", e.member, nil, http.StatusOK)["data"].(map[string]any)
	if len(state["updates"].([]any)) != 0 {
		t.Fatalf("rejected updates must not be stored: %v", state)
	}
}

func TestDocuments_ConcurrentUpdatesGetGaplessNumbers(t *testing.T) {
	e := newNotebookEnv(t)
	doc := e.newDocument(t, e.newNotebook(t, "n"), "d")
	updates := "/api/v1/documents/" + itoa(doc) + "/updates"

	const writers = 16
	var wg sync.WaitGroup
	seqs := make(chan int, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := request(t, e.tc.Router, http.MethodPost, updates, e.member, map[string]any{"update": b64("u" + itoa(uint(i))), "touched": []string{"shared"}})
			if rec.Code != http.StatusCreated {
				t.Errorf("update %d: %d: %s", i, rec.Code, rec.Body.String())
				return
			}
			seqs <- int(decodeJSON(t, rec)["data"].(map[string]any)["seq"].(float64))
		}(i)
	}
	wg.Wait()
	close(seqs)

	seen := map[int]bool{}
	for seq := range seqs {
		seen[seq] = true
	}
	for want := 1; want <= writers; want++ {
		if !seen[want] {
			t.Fatalf("update numbers must be 1..%d without gaps or duplicates, got %v", writers, seen)
		}
	}
}

func TestDocuments_SnapshotCompactsHistory(t *testing.T) {
	e := newNotebookEnv(t)
	doc := e.newDocument(t, e.newNotebook(t, "n"), "d")
	updates := "/api/v1/documents/" + itoa(doc) + "/updates"
	snapshot := "/api/v1/documents/" + itoa(doc) + "/snapshot"

	for _, text := range []string{"a", "b", "c"} {
		e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": b64(text)}, http.StatusCreated)
	}

	e.call(t, http.MethodPost, snapshot, e.guest, map[string]any{"upto_seq": 2, "state": validState()}, http.StatusForbidden)
	// A snapshot cannot claim updates that do not exist yet, nor go back.
	conflict := e.call(t, http.MethodPost, snapshot, e.member, map[string]any{"upto_seq": 4, "state": validState()}, http.StatusConflict)
	if conflict["error"].(map[string]any)["code"] != "DOCUMENT_SNAPSHOT_CONFLICT" {
		t.Fatalf("snapshot ahead of history: %v", conflict)
	}
	e.call(t, http.MethodPost, snapshot, e.member, map[string]any{"upto_seq": 0, "state": validState()}, http.StatusBadRequest)
	e.call(t, http.MethodPost, snapshot, e.member, map[string]any{"upto_seq": 2}, http.StatusBadRequest)
	// Bytes that are not a document are refused: a snapshot replaces the whole history.
	garbage := e.call(t, http.MethodPost, snapshot, e.member, map[string]any{"upto_seq": 2, "state": b64("merged")}, http.StatusBadRequest)
	details := garbage["error"].(map[string]any)["details"].([]any)
	if first := details[0].(map[string]any); first["field"] != "state" || first["code"] != "INVALID_VALUE" {
		t.Fatalf("garbage snapshot: %v", garbage)
	}

	e.call(t, http.MethodPost, snapshot, e.member, map[string]any{"upto_seq": 2, "state": validState()}, http.StatusOK)
	e.call(t, http.MethodPost, snapshot, e.member, map[string]any{"upto_seq": 2, "state": validState()}, http.StatusConflict)

	state := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/state", e.guest, nil, http.StatusOK)["data"].(map[string]any)
	if got, _ := base64.StdEncoding.DecodeString(state["snapshot"].(string)); string(got) != string(collab.InitialState()) {
		t.Fatalf("snapshot not stored: %v", state)
	}
	list := state["updates"].([]any)
	if state["snapshot_seq"].(float64) != 2 || len(list) != 1 || list[0].(map[string]any)["seq"].(float64) != 3 || state["last_seq"].(float64) != 3 {
		t.Fatalf("only the update after the snapshot should remain: %v", state)
	}

	next := e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": b64("d")}, http.StatusCreated)
	if next["data"].(map[string]any)["seq"].(float64) != 4 {
		t.Fatalf("numbering continues after compaction: %v", next)
	}
}

func TestNotebooks_DeleteHidesDocumentsAndWorkspaceDeleteHidesNotebooks(t *testing.T) {
	e := newNotebookEnv(t)
	nb := e.newNotebook(t, "n")
	doc := e.newDocument(t, nb, "d")
	e.call(t, http.MethodDelete, "/api/v1/notebooks/"+itoa(nb), e.owner.token, nil, http.StatusOK)
	for _, path := range []string{
		"/api/v1/documents/" + itoa(doc),
		"/api/v1/documents/" + itoa(doc) + "/state",
		"/api/v1/notebooks/" + itoa(nb) + "/documents",
	} {
		e.call(t, http.MethodGet, path, e.owner.token, nil, http.StatusForbidden)
	}
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(doc)+"/updates", e.owner.token, map[string]any{"update": b64("x")}, http.StatusForbidden)

	other := e.newNotebook(t, "other")
	doc2 := e.newDocument(t, other, "d2")
	e.call(t, http.MethodDelete, "/api/v1/workspaces/"+itoa(e.owner.workspaceID), e.owner.token, nil, http.StatusOK)
	e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(doc2), e.owner.token, nil, http.StatusForbidden)
	e.call(t, http.MethodGet, "/api/v1/notebooks/"+itoa(other), e.owner.token, nil, http.StatusForbidden)
}

func TestNotebooks_RoleDefaultsGrantDocumentPermissions(t *testing.T) {
	e := newNotebookEnv(t)
	var perms map[string]bool
	rows, err := e.tc.DB.Raw(`SELECT r.name || ':' || p.code FROM roles r
		JOIN role_permissions rp ON rp.role_id = r.id AND rp.deleted_at IS NULL
		JOIN permissions p ON p.id = rp.permission_id AND p.code LIKE 'doc.%'
		WHERE r.workspace_id = ? AND r.deleted_at IS NULL`, e.owner.workspaceID).Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	perms = map[string]bool{}
	for rows.Next() {
		var item string
		if err := rows.Scan(&item); err != nil {
			t.Fatal(err)
		}
		perms[item] = true
	}
	for _, want := range []string{"Owner:doc.read", "Owner:doc.delete", "Admin:doc.update", "Member:doc.create", "Member:doc.delete", "Guest:doc.read"} {
		if !perms[want] {
			t.Errorf("missing %s in %v", want, perms)
		}
	}
	for _, deny := range []string{"Guest:doc.create", "Guest:doc.update", "Guest:doc.delete"} {
		if perms[deny] {
			t.Errorf("%s must not be granted", deny)
		}
	}
}
