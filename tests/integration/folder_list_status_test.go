package integration

import (
	"net/http"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func createSpace(t *testing.T, tc *TestContext) uint {
	t.Helper()
	workspaceID := createWorkspace(t, tc)

	rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/workspaces/"+itoa(workspaceID)+"/spaces",
		tc.Token,
		map[string]any{"name": "Test Space"},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: %d: %s", rec.Code, rec.Body.String())
	}
	return uint(dataObject(t, rec)["ID"].(float64))
}

func createList(t *testing.T, tc *TestContext, spaceID uint) uint {
	t.Helper()

	rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/spaces/"+itoa(spaceID)+"/lists",
		tc.Token,
		map[string]any{"name": "Test List"},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list: %d: %s", rec.Code, rec.Body.String())
	}
	return uint(dataObject(t, rec)["ID"].(float64))
}

func TestFolder_CreateListInFolderAndUpdate(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)

	rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/spaces/"+itoa(spaceID)+"/folders",
		tc.Token,
		map[string]any{"name": "Projects"},
	)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("create folder: expected 200/201, got %d: %s", rec.Code, rec.Body.String())
	}
	folderID := uint(dataObject(t, rec)["ID"].(float64))

	rec = request(t, tc.Router, http.MethodPost,
		"/api/v1/folders/"+itoa(folderID)+"/lists",
		tc.Token,
		map[string]any{"name": "Folder List"},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list in folder: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	list := dataObject(t, rec)
	if uint(list["FolderID"].(float64)) != folderID {
		t.Fatalf("folder_id = %v, want %d", list["FolderID"], folderID)
	}
}

func TestStatus_PositionShiftAndDelete(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/lists/"+itoa(listID)+"/statuses",
		tc.Token,
		map[string]any{"name": "Inserted", "color": "#111111", "position": 1, "type": 0, "board_id": listID},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var statuses []domain.TaskStatus
	if err := tc.DB.Where("space_id = ?", spaceID).Order("position ASC").Find(&statuses).Error; err != nil {
		t.Fatalf("query statuses: %v", err)
	}
	if len(statuses) != 4 {
		t.Fatalf("status count = %d, want 4", len(statuses))
	}

	for i, status := range statuses {
		if status.Position != i+1 {
			t.Fatalf("status %q has position %d, want %d", status.Name, status.Position, i+1)
		}
	}

	insertedID := uint(dataObject(t, rec)["ID"].(float64))
	rec = request(t, tc.Router, http.MethodDelete,
		"/api/v1/lists/"+itoa(listID)+"/statuses/"+itoa(insertedID),
		tc.Token, nil,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
