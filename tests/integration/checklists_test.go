package integration

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

type checklistEnv struct {
	tc         *TestContext
	owner      workspaceOwner
	taskID     uint
	memberID   uint
	memberTok  string
	guestTok   string
	outsiderID uint
	outsiderTk string
}

func newChecklistEnv(t *testing.T) *checklistEnv {
	t.Helper()
	tc := newTestContext(t)
	owner, taskID, _, guestToken, outsiderToken := filesFixture(t, tc)

	var memberID uint
	if err := tc.DB.Table("users").Select("id").Where("email = ?", "files-member@example.test").Scan(&memberID).Error; err != nil || memberID == 0 {
		t.Fatalf("find member: %v", err)
	}
	var memberToken string
	{
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": "files-member@example.test", "password": "password-123",
		})
		memberToken = dataObject(t, rec)["access_token"].(string)
	}
	var outsiderID uint
	tc.DB.Table("users").Select("id").Where("email = ?", "files-outsider@example.test").Scan(&outsiderID)
	return &checklistEnv{
		tc: tc, owner: owner, taskID: taskID, memberID: memberID, memberTok: memberToken,
		guestTok: guestToken, outsiderID: outsiderID, outsiderTk: outsiderToken,
	}
}

func (e *checklistEnv) base() string { return "/api/v1/tasks/" + itoa(e.taskID) + "/checklists" }

func (e *checklistEnv) checklist(t *testing.T, name string) uint {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, e.base(), e.owner.token, map[string]any{"name": name})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create checklist %q: %d: %s", name, rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func (e *checklistEnv) item(t *testing.T, checklistID uint, text string) uint {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, e.base()+"/"+itoa(checklistID)+"/items", e.owner.token, map[string]any{"text": text})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item %q: %d: %s", text, rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func (e *checklistEnv) lists(t *testing.T, token string) []map[string]any {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, e.base(), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list checklists: %d: %s", rec.Code, rec.Body.String())
	}
	raw, _ := decodeJSON(t, rec)["data"].([]any)
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

func idsOf(items []any) []uint {
	out := make([]uint, len(items))
	for i, it := range items {
		out[i] = uint(it.(map[string]any)["id"].(float64))
	}
	return out
}

func TestChecklists_CreateRenameReorderDelete(t *testing.T) {
	e := newChecklistEnv(t)

	rec := request(t, e.tc.Router, http.MethodPost, e.base(), e.owner.token, map[string]any{"name": "  Release  "})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d: %s", rec.Code, rec.Body.String())
	}
	created := dataObject(t, rec)
	if created["name"] != "Release" || len(created["items"].([]any)) != 0 {
		t.Fatalf("checklist = %v", created)
	}
	first := responseID(t, rec)
	second := e.checklist(t, "QA")
	third := e.checklist(t, "Docs")

	for _, body := range []map[string]any{{"name": "   "}, {}} {
		if rec := request(t, e.tc.Router, http.MethodPost, e.base(), e.owner.token, body); rec.Code == http.StatusCreated {
			t.Fatalf("blank name accepted: %v", body)
		}
	}
	if rec := request(t, e.tc.Router, http.MethodPost, e.base(), e.guestTok, map[string]any{"name": "g"}); rec.Code != http.StatusForbidden {
		t.Fatalf("guest create: expected 403, got %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodGet, e.base(), e.outsiderTk, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list: expected 403, got %d", rec.Code)
	}
	if got := idsOfMaps(e.lists(t, e.guestTok)); !reflect.DeepEqual(got, []uint{first, second, third}) {
		t.Fatalf("guest sees %v, want creation order", got)
	}

	rec = request(t, e.tc.Router, http.MethodPatch, e.base()+"/"+itoa(second), e.owner.token, map[string]any{"name": "Quality"})
	if rec.Code != http.StatusOK || dataObject(t, rec)["name"] != "Quality" {
		t.Fatalf("rename: %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, e.tc.Router, http.MethodPut, e.base()+"/order", e.owner.token, map[string]any{"ids": []uint{third, first, second}})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder: %d: %s", rec.Code, rec.Body.String())
	}
	if got := idsOfMaps(e.lists(t, e.owner.token)); !reflect.DeepEqual(got, []uint{third, first, second}) {
		t.Fatalf("order after reorder = %v", got)
	}
	for name, ids := range map[string][]uint{
		"missing id":   {first, second},
		"unknown id":   {first, second, 999999},
		"duplicate id": {first, first, second},
		"extra id":     {first, second, third, 999999},
	} {
		if rec := request(t, e.tc.Router, http.MethodPut, e.base()+"/order", e.owner.token, map[string]any{"ids": ids}); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", name, rec.Code)
		}
	}
	if got := idsOfMaps(e.lists(t, e.owner.token)); !reflect.DeepEqual(got, []uint{third, first, second}) {
		t.Fatalf("a rejected reorder changed the order: %v", got)
	}

	other := "/api/v1/tasks/" + itoa(e.taskID+1000) + "/checklists/" + itoa(first)
	if rec := request(t, e.tc.Router, http.MethodPatch, other, e.owner.token, map[string]any{"name": "x"}); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("checklist through another task: expected 403/404, got %d", rec.Code)
	}

	if rec := request(t, e.tc.Router, http.MethodDelete, e.base()+"/"+itoa(first), e.guestTok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("guest delete: expected 403, got %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, e.base()+"/"+itoa(first), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, e.base()+"/"+itoa(first), e.owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete twice: expected 404, got %d", rec.Code)
	}
	if got := idsOfMaps(e.lists(t, e.owner.token)); !reflect.DeepEqual(got, []uint{third, second}) {
		t.Fatalf("after delete = %v", got)
	}
}

func idsOfMaps(lists []map[string]any) []uint {
	out := make([]uint, len(lists))
	for i, l := range lists {
		out[i] = uint(l["id"].(float64))
	}
	return out
}

func TestChecklistItems_ResolveAssignReorderAndDelete(t *testing.T) {
	e := newChecklistEnv(t)
	list := e.checklist(t, "Release")
	a := e.item(t, list, "write changelog")
	b := e.item(t, list, "tag the build")
	c := e.item(t, list, "announce")
	items := e.base() + "/" + itoa(list) + "/items"

	var resolvedAt1 string
	rec := request(t, e.tc.Router, http.MethodPatch, items+"/"+itoa(a), e.memberTok, map[string]any{"resolved": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve: %d: %s", rec.Code, rec.Body.String())
	}
	item := dataObject(t, rec)
	if item["resolved"] != true || item["resolved_at"] == nil {
		t.Fatalf("resolved item = %v", item)
	}
	resolvedAt1 = item["resolved_at"].(string)
	by := item["resolved_by"].(map[string]any)
	if uint(by["id"].(float64)) != e.memberID {
		t.Fatalf("resolved_by = %v, want the member", by)
	}
	if _, leaked := by["email"]; leaked {
		t.Fatalf("resolved_by exposes the email address: %v", by)
	}

	rec = request(t, e.tc.Router, http.MethodPatch, items+"/"+itoa(a), e.owner.token, map[string]any{"resolved": true, "text": "write the changelog"})
	item = dataObject(t, rec)
	if item["resolved_at"] != resolvedAt1 || uint(item["resolved_by"].(map[string]any)["id"].(float64)) != e.memberID {
		t.Fatalf("resolving an already resolved item changed who and when: %v", item)
	}
	if item["text"] != "write the changelog" {
		t.Fatalf("text = %v", item["text"])
	}

	rec = request(t, e.tc.Router, http.MethodPatch, items+"/"+itoa(a), e.owner.token, map[string]any{"resolved": false})
	item = dataObject(t, rec)
	if item["resolved"] != false || item["resolved_at"] != nil || item["resolved_by"] != nil {
		t.Fatalf("reopened item = %v", item)
	}

	rec = request(t, e.tc.Router, http.MethodPatch, items+"/"+itoa(b), e.owner.token, map[string]any{"assignee_id": e.memberID})
	if rec.Code != http.StatusOK || uint(dataObject(t, rec)["assignee"].(map[string]any)["id"].(float64)) != e.memberID {
		t.Fatalf("assign: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, e.tc.Router, http.MethodPatch, items+"/"+itoa(b), e.owner.token, map[string]any{"assignee_id": e.outsiderID}); rec.Code != http.StatusBadRequest {
		t.Fatalf("assigning a non-member: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, e.tc.Router, http.MethodPost, items, e.owner.token, map[string]any{"text": "x", "assignee_id": e.outsiderID}); rec.Code != http.StatusBadRequest {
		t.Fatalf("creating with a non-member assignee: expected 400, got %d", rec.Code)
	}
	rec = request(t, e.tc.Router, http.MethodPatch, items+"/"+itoa(b), e.owner.token, map[string]any{"text": "tag the release build"})
	if dataObject(t, rec)["assignee"] == nil {
		t.Fatal("an update without assignee_id must keep the assignee")
	}
	rec = request(t, e.tc.Router, http.MethodPatch, items+"/"+itoa(b), e.owner.token, map[string]any{"assignee_id": nil})
	if dataObject(t, rec)["assignee"] != nil {
		t.Fatalf("assignee_id null must clear the assignee: %v", dataObject(t, rec))
	}

	if rec := request(t, e.tc.Router, http.MethodPatch, items+"/"+itoa(a), e.guestTok, map[string]any{"resolved": true}); rec.Code != http.StatusForbidden {
		t.Fatalf("guest resolve: expected 403, got %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodPost, items, e.owner.token, map[string]any{"text": "   "}); rec.Code == http.StatusCreated {
		t.Fatal("a blank item was accepted")
	}

	rec = request(t, e.tc.Router, http.MethodPut, items+"/order", e.owner.token, map[string]any{"ids": []uint{c, a, b}})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder items: %d: %s", rec.Code, rec.Body.String())
	}
	if got := idsOf(e.lists(t, e.owner.token)[0]["items"].([]any)); !reflect.DeepEqual(got, []uint{c, a, b}) {
		t.Fatalf("item order = %v", got)
	}
	if rec := request(t, e.tc.Router, http.MethodPut, items+"/order", e.owner.token, map[string]any{"ids": []uint{c, a}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("incomplete item order: expected 400, got %d", rec.Code)
	}

	wrongList := e.checklist(t, "Other")
	if rec := request(t, e.tc.Router, http.MethodPatch, e.base()+"/"+itoa(wrongList)+"/items/"+itoa(a), e.owner.token, map[string]any{"text": "x"}); rec.Code != http.StatusNotFound {
		t.Fatalf("item through another checklist: expected 404, got %d", rec.Code)
	}

	if rec := request(t, e.tc.Router, http.MethodDelete, items+"/"+itoa(a), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete item: %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, items+"/"+itoa(a), e.owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete item twice: expected 404, got %d", rec.Code)
	}
	if got := idsOf(e.lists(t, e.owner.token)[0]["items"].([]any)); !reflect.DeepEqual(got, []uint{c, b}) {
		t.Fatalf("items after delete = %v", got)
	}

	if rec := request(t, e.tc.Router, http.MethodDelete, e.base()+"/"+itoa(list), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete checklist: %d", rec.Code)
	}
	var leftover int64
	e.tc.DB.Table("task_checklist_items").Where("checklist_id = ?", list).Count(&leftover)
	if leftover != 0 {
		t.Fatalf("%d items survived deleting their checklist", leftover)
	}
}

func TestChecklists_Limits(t *testing.T) {
	e := newChecklistEnv(t)

	var last uint
	for i := 0; i < 20; i++ {
		last = e.checklist(t, fmt.Sprintf("list %d", i))
	}
	if rec := request(t, e.tc.Router, http.MethodPost, e.base(), e.owner.token, map[string]any{"name": "one too many"}); rec.Code != http.StatusConflict {
		t.Fatalf("21st checklist: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	if err := e.tc.DB.Exec(
		"INSERT INTO task_checklist_items (checklist_id, text, position) SELECT ?, 'item ' || g, g FROM generate_series(1, 199) g", last,
	).Error; err != nil {
		t.Fatalf("seed items: %v", err)
	}
	items := e.base() + "/" + itoa(last) + "/items"
	if rec := request(t, e.tc.Router, http.MethodPost, items, e.owner.token, map[string]any{"text": "the 200th"}); rec.Code != http.StatusCreated {
		t.Fatalf("200th item: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, e.tc.Router, http.MethodPost, items, e.owner.token, map[string]any{"text": "the 201st"}); rec.Code != http.StatusConflict {
		t.Fatalf("201st item: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChecklists_AreLoggedInTheActivityFeed(t *testing.T) {
	e := newChecklistEnv(t)
	list := e.checklist(t, "Release")
	item := e.item(t, list, "ship it")
	items := e.base() + "/" + itoa(list) + "/items/" + itoa(item)

	request(t, e.tc.Router, http.MethodPatch, items, e.owner.token, map[string]any{"resolved": true})
	request(t, e.tc.Router, http.MethodPatch, items, e.owner.token, map[string]any{"resolved": true})
	request(t, e.tc.Router, http.MethodPatch, items, e.owner.token, map[string]any{"text": "ship it today"})
	request(t, e.tc.Router, http.MethodPatch, items, e.owner.token, map[string]any{"resolved": false})
	request(t, e.tc.Router, http.MethodDelete, e.base()+"/"+itoa(list), e.owner.token, nil)

	feed, _ := fetchActivity(t, e.tc, e.owner.token, e.taskID, "?limit=100")
	count := map[string]int{}
	for _, entry := range feed {
		count[entry.Action]++
	}
	for action, want := range map[string]int{
		"checklist.created": 1, "checklist.item_resolved": 1, "checklist.item_reopened": 1, "checklist.deleted": 1,
	} {
		if count[action] != want {
			t.Errorf("%s entries = %d, want %d (all: %v)", action, count[action], want, count)
		}
	}
}

func TestChecklists_DeletingAnAccountClearsItsAssignments(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "assignee")
	memberID, memberToken := newUser(t, tc, "assignee-member@example.test")
	addContact(t, tc, owner.token, memberID)
	if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
		map[string]any{"user_id": memberID, "role": "Member"}); rec.Code != http.StatusCreated {
		t.Fatalf("add member: %d", rec.Code)
	}
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", owner.token, map[string]any{"name": "list"})
	listID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", owner.token,
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", owner.token,
		map[string]any{"title": "task one", "status_id": responseID(t, rec)})
	taskID := responseID(t, rec)

	base := "/api/v1/tasks/" + itoa(taskID) + "/checklists"
	rec = request(t, tc.Router, http.MethodPost, base, owner.token, map[string]any{"name": "list"})
	checklistID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPost, base+"/"+itoa(checklistID)+"/items", owner.token,
		map[string]any{"text": "mine", "assignee_id": memberID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item: %d: %s", rec.Code, rec.Body.String())
	}

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile", memberToken, map[string]any{"password": "password-123"}); rec.Code != http.StatusOK {
		t.Fatalf("delete account: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodGet, base, owner.token, nil)
	raw := decodeJSON(t, rec)["data"].([]any)[0].(map[string]any)["items"].([]any)
	if raw[0].(map[string]any)["assignee"] != nil {
		t.Fatalf("a deleted account is still assigned: %v", raw[0])
	}
}
