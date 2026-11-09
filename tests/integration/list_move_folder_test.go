package integration

import (
	"net/http"
	"testing"
)

func lmListFolder(t *testing.T, tc *TestContext, listID uint) *uint {
	t.Helper()
	var folder *uint
	if err := tc.DB.Table("lists").Select("folder_id").Where("id = ?", listID).Row().Scan(&folder); err != nil {
		t.Fatal(err)
	}
	return folder
}

func TestListPatch_MovesListBetweenFoldersAndToSpaceRoot(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "lmmove")
	token := owner.token
	foldersURL := "/api/v1/spaces/" + itoa(owner.spaceID) + "/folders"
	folderA := mustCreate(t, tc, token, foldersURL, map[string]any{"name": "A"})
	folderB := mustCreate(t, tc, token, foldersURL, map[string]any{"name": "B"})
	listID := mustCreate(t, tc, token, "/api/v1/folders/"+itoa(folderA)+"/lists", map[string]any{"name": "Movable"})
	url := "/api/v1/lists/" + itoa(listID)

	rec := request(t, tc.Router, http.MethodPatch, url, token, map[string]any{"folder_id": folderB})
	if rec.Code != http.StatusOK {
		t.Fatalf("move to folder B: %d: %s", rec.Code, rec.Body.String())
	}
	if got := dataObject(t, rec)["folder_id"]; got != float64(folderB) {
		t.Fatalf("response folder_id = %v, want %d", got, folderB)
	}
	if got := lmListFolder(t, tc, listID); got == nil || *got != folderB {
		t.Fatalf("stored folder_id = %v, want %d", got, folderB)
	}
	inB := responseArray(t, request(t, tc.Router, http.MethodGet, "/api/v1/folders/"+itoa(folderB)+"/lists", token, nil))
	inA := responseArray(t, request(t, tc.Router, http.MethodGet, "/api/v1/folders/"+itoa(folderA)+"/lists", token, nil))
	if len(inB) != 1 || len(inA) != 0 {
		t.Fatalf("folder listings after move: A=%d B=%d", len(inA), len(inB))
	}

	rec = request(t, tc.Router, http.MethodPatch, url, token, map[string]any{"name": "Renamed only"})
	if rec.Code != http.StatusOK || lmListFolder(t, tc, listID) == nil {
		t.Fatalf("rename must keep the folder: %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPatch, url, token, map[string]any{"folder_id": nil})
	if rec.Code != http.StatusOK || lmListFolder(t, tc, listID) != nil {
		t.Fatalf("move to space root: %d: %s", rec.Code, rec.Body.String())
	}
	if dataObject(t, rec)["folder_id"] != nil {
		t.Fatalf("response folder_id = %v, want null", dataObject(t, rec)["folder_id"])
	}

	rec = request(t, tc.Router, http.MethodPatch, url, token, map[string]any{"folder_id": folderA, "name": "Both"})
	if rec.Code != http.StatusOK || dataObject(t, rec)["name"] != "Both" || lmListFolder(t, tc, listID) == nil {
		t.Fatalf("name and folder together: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListPatch_RejectsUnknownForeignAndDeadFolders(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "lmbad")
	other := newWorkspaceOwner(t, tc, "lmother")
	token := owner.token
	foldersURL := "/api/v1/spaces/" + itoa(owner.spaceID) + "/folders"
	home := mustCreate(t, tc, token, foldersURL, map[string]any{"name": "Home"})
	listID := mustCreate(t, tc, token, "/api/v1/folders/"+itoa(home)+"/lists", map[string]any{"name": "L"})
	url := "/api/v1/lists/" + itoa(listID)

	secondSpace := mustCreate(t, tc, token, "/api/v1/workspaces/"+itoa(owner.workspaceID)+"/spaces", map[string]any{"name": "Second"})
	sameWorkspaceOtherSpace := mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(secondSpace)+"/folders", map[string]any{"name": "Elsewhere"})
	foreign := mustCreate(t, tc, other.token, "/api/v1/spaces/"+itoa(other.spaceID)+"/folders", map[string]any{"name": "Foreign"})
	dead := mustCreate(t, tc, token, foldersURL, map[string]any{"name": "Dead"})
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/folders/"+itoa(dead), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete folder: %d: %s", rec.Code, rec.Body.String())
	}

	for name, folder := range map[string]any{
		"unknown": 99999, "other space": sameWorkspaceOtherSpace, "other workspace": foreign, "deleted": dead, "zero": 0, "text": "abc",
	} {
		rec := request(t, tc.Router, http.MethodPatch, url, token, map[string]any{"folder_id": folder})
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 400/404, got %d: %s", name, rec.Code, rec.Body.String())
		}
		if got := lmListFolder(t, tc, listID); got == nil || *got != home {
			t.Errorf("%s: list moved to %v", name, got)
		}
	}

	_, outsider := newUser(t, tc, "lm-outsider@example.test")
	rec := request(t, tc.Router, http.MethodPatch, url, outsider, map[string]any{"folder_id": nil})
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("outsider move: expected 403/404, got %d", rec.Code)
	}
	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/lists/99999", token, map[string]any{"folder_id": nil})
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("unknown list: expected 404/403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListPatch_MoveKeepsTasksAndStatuses(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "lmkeep")
	token := owner.token
	folder := mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(owner.spaceID)+"/folders", map[string]any{"name": "F"})
	listID := mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", map[string]any{"name": "L"})
	statusID := dsFirstStatusID(t, tc, listID)
	taskID := mustCreate(t, tc, token, "/api/v1/lists/"+itoa(listID)+"/tasks", map[string]any{"title": "stays", "status_id": statusID})

	if rec := request(t, tc.Router, http.MethodPatch, "/api/v1/lists/"+itoa(listID), token, map[string]any{"folder_id": folder}); rec.Code != http.StatusOK {
		t.Fatalf("move: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("task after list move: %d: %s", rec.Code, rec.Body.String())
	}
	if len(dsStatuses(t, tc, listID)) != 3 {
		t.Fatal("statuses changed by the move")
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/folders/"+itoa(folder), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete folder: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID), token, nil); rec.Code == http.StatusOK {
		t.Fatal("task of a list inside a deleted folder must be gone after the folder cascade")
	}
}
