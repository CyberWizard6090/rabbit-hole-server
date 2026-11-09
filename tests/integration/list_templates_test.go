package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListTemplates_CatalogIsLocalizedByAcceptLanguage(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "ltcatalog")

	rec := requestWithHeader(t, tc, http.MethodGet, "/api/v1/list-templates", owner.token, "Accept-Language", "ru-RU,ru;q=0.9", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: %d: %s", rec.Code, rec.Body.String())
	}
	items, _ := decodeJSON(t, rec)["data"].([]any)
	keys := map[string]map[string]any{}
	for _, item := range items {
		m := item.(map[string]any)
		keys[m["key"].(string)] = m
	}
	for _, key := range []string{"simple", "kanban", "development", "bugs"} {
		if keys[key] == nil {
			t.Fatalf("template %q missing from %v", key, keys)
		}
	}
	if keys["kanban"]["name"] != "Канбан" || keys["bugs"]["statuses"].([]any)[0].(map[string]any)["name"] != "Новый" {
		t.Fatalf("the Russian catalog is not localized: %v", keys["kanban"])
	}
	en := requestWithHeader(t, tc, http.MethodGet, "/api/v1/list-templates", owner.token, "Accept-Language", "de", nil)
	if !strings.Contains(en.Body.String(), `"Kanban"`) {
		t.Fatalf("an unknown language falls back to English: %s", en.Body.String())
	}
	if anon := request(t, tc.Router, http.MethodGet, "/api/v1/list-templates", "", nil); anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous catalog: %d", anon.Code)
	}
}

func TestListTemplates_NewSpacesAndListsGetTheChosenStatusesInTheRequestLanguage(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "ltcreate")
	ru := func(method, path string, body any) *httptest.ResponseRecorder {
		return requestWithHeader(t, tc, method, path, owner.token, "Accept-Language", "ru", body)
	}

	rec := ru(http.MethodPost, "/api/v1/workspaces/"+itoa(owner.workspaceID)+"/spaces",
		map[string]any{"name": "Dev space", "template": "development"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: %d: %s", rec.Code, rec.Body.String())
	}
	spaceID := responseID(t, rec)
	var listID uint
	var listName string
	row := tc.DB.Table("lists").Select("id, name").Where("space_id = ?", spaceID).Row()
	if err := row.Scan(&listID, &listName); err != nil || listName != "Основной список" {
		t.Fatalf("default list = %d %q %v", listID, listName, err)
	}
	statuses := dsStatuses(t, tc, listID)
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = s.Name
	}
	if got := strings.Join(names, ","); got != "Бэклог,Анализ,В разработке,Код-ревью,Тестирование,Готово" {
		t.Fatalf("development statuses = %s", got)
	}

	rec = ru(http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/lists", map[string]any{"name": "Bugs", "template": "bugs"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list: %d: %s", rec.Code, rec.Body.String())
	}
	bugs := dsStatuses(t, tc, responseID(t, rec))
	if len(bugs) != 6 || bugs[0].Name != "Новый" || bugs[5].Name != "Отклонён" || bugs[5].Type != "done" {
		t.Fatalf("bug statuses = %+v", bugs)
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/lists", owner.token, map[string]any{"name": "Plain"})
	plainListID := responseID(t, rec)
	plain := dsStatuses(t, tc, plainListID)
	if len(plain) != 3 || plain[0].Name != "To do" {
		t.Fatalf("a list without a template or language gets the English simple set: %+v", plain)
	}
	if renamed := request(t, tc.Router, http.MethodPut, "/api/v1/lists/"+itoa(plainListID)+"/statuses/"+itoa(statusIDByName(t, tc, plainListID, "To do")), owner.token,
		map[string]any{"name": "Backlog"}); renamed.Code != http.StatusOK {
		t.Fatalf("a template status is ordinary data and can be renamed: %d: %s", renamed.Code, renamed.Body.String())
	}

	for _, path := range []string{"/api/v1/workspaces/" + itoa(owner.workspaceID) + "/spaces", "/api/v1/spaces/" + itoa(spaceID) + "/lists"} {
		bad := request(t, tc.Router, http.MethodPost, path, owner.token, map[string]any{"name": "x", "template": "nope"})
		if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "UNKNOWN_LIST_TEMPLATE") {
			t.Fatalf("%s with an unknown template: %d: %s", path, bad.Code, bad.Body.String())
		}
	}
}

func statusIDByName(t *testing.T, tc *TestContext, listID uint, name string) uint {
	t.Helper()
	var id uint
	if err := tc.DB.Table("task_statuses").Select("max(id)").Where("list_id = ? AND name = ? AND deleted_at IS NULL", listID, name).Scan(&id).Error; err != nil || id == 0 {
		t.Fatalf("status %q: %d %v", name, id, err)
	}
	return id
}
