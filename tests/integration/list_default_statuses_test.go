package integration

import (
	"net/http"
	"testing"
)

type dsStatus struct {
	Name     string
	Color    string
	Position int
	Type     string
}

func dsStatuses(t *testing.T, tc *TestContext, listID uint) []dsStatus {
	t.Helper()
	var rows []dsStatus
	if err := tc.DB.Table("task_statuses").Select("name, color, position, type").
		Where("list_id = ? AND deleted_at IS NULL", listID).Order("position").Scan(&rows).Error; err != nil {
		t.Fatalf("read statuses: %v", err)
	}
	return rows
}

func dsExpectDefaults(t *testing.T, label string, got []dsStatus) {
	t.Helper()
	want := []dsStatus{
		{"К выполнению", "#94A3B8", 1, "todo"},
		{"В работе", "#3B82F6", 2, "in_progress"},
		{"Готово", "#22C55E", 3, "done"},
	}
	if len(got) != len(want) {
		t.Fatalf("%s: statuses = %+v, want the %d defaults", label, got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: status %d = %+v, want %+v", label, i, got[i], want[i])
		}
	}
}

func TestNewLists_AreSeededWithDefaultStatuses(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "dsseed")
	token := owner.token
	spaceLists := "/api/v1/spaces/" + itoa(owner.spaceID) + "/lists"

	var defaultListID uint
	if err := tc.DB.Table("lists").Select("id").Where("space_id = ?", owner.spaceID).Scan(&defaultListID).Error; err != nil || defaultListID == 0 {
		t.Fatalf("default list: %d %v", defaultListID, err)
	}
	dsExpectDefaults(t, "default list of a new space", dsStatuses(t, tc, defaultListID))

	rec := request(t, tc.Router, http.MethodPost, spaceLists, token, map[string]any{"name": "Plain"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list: %d: %s", rec.Code, rec.Body.String())
	}
	plainID := responseID(t, rec)
	dsExpectDefaults(t, "list in space", dsStatuses(t, tc, plainID))
	statuses, _ := dataObject(t, rec)["statuses"].([]any)
	if len(statuses) != 3 {
		t.Fatalf("create response statuses = %v, want the three defaults", statuses)
	}

	folderID := mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(owner.spaceID)+"/folders", map[string]any{"name": "Folder"})
	inFolder := mustCreate(t, tc, token, "/api/v1/folders/"+itoa(folderID)+"/lists", map[string]any{"name": "In folder"})
	dsExpectDefaults(t, "list in folder", dsStatuses(t, tc, inFolder))

	child := request(t, tc.Router, http.MethodPost, spaceLists, token, map[string]any{"name": "Child", "parent_status_id": dsFirstStatusID(t, tc, plainID)})
	if child.Code != http.StatusCreated {
		t.Fatalf("create list with parent status: %d: %s", child.Code, child.Body.String())
	}
	childID := responseID(t, child)
	dsExpectDefaults(t, "list with parent_status_id", dsStatuses(t, tc, childID))
	if dataObject(t, child)["parent_status_id"] == nil {
		t.Fatalf("parent_status_id lost: %s", child.Body.String())
	}

	taskRec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(plainID)+"/tasks", token, map[string]any{
		"title": "Usable at once", "status_id": dsFirstStatusID(t, tc, plainID),
	})
	if taskRec.Code != http.StatusCreated {
		t.Fatalf("task in a fresh list: %d: %s", taskRec.Code, taskRec.Body.String())
	}
}

func dsFirstStatusID(t *testing.T, tc *TestContext, listID uint) uint {
	t.Helper()
	var id uint
	if err := tc.DB.Table("task_statuses").Select("id").Where("list_id = ?", listID).Order("position").Limit(1).Scan(&id).Error; err != nil || id == 0 {
		t.Fatalf("first status of list %d: %d %v", listID, id, err)
	}
	return id
}

func TestNewLists_StatusCreatedByHandAppendsAfterDefaults(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "dsappend")
	listID := mustCreate(t, tc, owner.token, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", map[string]any{"name": "L"})
	srStatus(t, tc, owner.token, listID, "Extra")
	got := dsStatuses(t, tc, listID)
	if len(got) != 4 || got[3].Name != "Extra" || got[3].Position != 4 {
		t.Fatalf("statuses = %+v, want Extra appended at position 4", got)
	}
}
