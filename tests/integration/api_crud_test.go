package integration

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

func responseID(t *testing.T, rec *httptest.ResponseRecorder) uint {
	t.Helper()
	data := dataObject(t, rec)
	for _, key := range []string{"ID", "id"} {
		if value, ok := data[key].(float64); ok {
			return uint(value)
		}
	}
	t.Fatalf("response data has no ID: %v", data)
	return 0
}

func TestAPI_ProfileSearchAndContactsLifecycle(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	otherEmail := "api-contact-" + suffix + "@example.test"
	otherUsername := "api-contact-" + suffix
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    otherEmail,
		"password": "test-password-123",
		"username": otherUsername,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register contact target: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var otherUser domain.User
	if err := tc.DB.Where("email = ?", otherEmail).First(&otherUser).Error; err != nil {
		t.Fatalf("find contact target: %v", err)
	}

	firstName := "Ada"
	bio := "API integration profile"
	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/profile/", tc.Token, map[string]any{
		"first_name": firstName,
		"bio":        bio,
		"time_zone":  "UTC",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update profile: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/profile/", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get profile: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	profile := dataObject(t, rec)
	if profile["first_name"] != firstName || profile["bio"] != bio {
		t.Fatalf("profile data = %+v, want first name %q and bio %q", profile, firstName, bio)
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/users/search?q="+otherUsername, tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("search users: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	searchBody := decodeJSON(t, rec)
	searchResults, ok := searchBody["data"].([]any)
	if !ok || len(searchResults) == 0 {
		t.Fatalf("search results = %v, want target user", searchBody["data"])
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/contacts/", tc.Token, map[string]any{
		"contact_id": otherUser.ID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add contact: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/contacts/", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list contacts: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	contactsBody := decodeJSON(t, rec)
	contacts, ok := contactsBody["data"].([]any)
	if !ok || len(contacts) != 1 {
		t.Fatalf("contacts = %v, want one contact", contactsBody["data"])
	}

	var relationCount int64
	if err := tc.DB.Table("user_contacts").Where("user_id = ? AND contact_id = ?", tc.UserID, otherUser.ID).Count(&relationCount).Error; err != nil {
		t.Fatalf("count contact relation: %v", err)
	}
	if relationCount != 1 {
		t.Fatalf("contact relation count = %d, want 1", relationCount)
	}

	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/contacts/"+itoa(otherUser.ID), tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete contact: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := tc.DB.Table("user_contacts").Where("user_id = ? AND contact_id = ?", tc.UserID, otherUser.ID).Count(&relationCount).Error; err != nil {
		t.Fatalf("count deleted contact relation: %v", err)
	}
	if relationCount != 0 {
		t.Fatalf("deleted contact relation count = %d, want 0", relationCount)
	}
}

func TestAPI_RejectsWhitespaceOnlySpaceName(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", tc.Token, map[string]any{
		"name": "   ",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("whitespace-only space name: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAPI_WorkspaceSpaceFolderListStatusTagLifecycle(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", tc.Token, map[string]any{
		"name":     "API Space",
		"currency": "EUR",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	space := dataObject(t, rec)
	spaceID := uint(space["ID"].(float64))

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces?page=1&limit=10", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list spaces: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	spacesBody := decodeJSON(t, rec)
	if len(spacesBody["data"].([]any)) != 1 || spacesBody["pagination"].(map[string]any)["total"] != float64(1) {
		t.Fatalf("spaces response = %v", spacesBody)
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/dashboard", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get dashboard: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/folders", tc.Token, map[string]any{"name": "API Folder"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	folderID := uint(dataObject(t, rec)["ID"].(float64))

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/folders", tc.Token, nil)
	if rec.Code != http.StatusOK || len(decodeJSON(t, rec)["data"].([]any)) != 1 {
		t.Fatalf("list folders: %d %s", rec.Code, rec.Body.String())
	}

	updatedFolder := "Updated Folder"
	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/folders/"+itoa(folderID)+"/", tc.Token, map[string]any{"name": updatedFolder})
	if rec.Code != http.StatusOK || dataObject(t, rec)["Name"] != updatedFolder {
		t.Fatalf("update folder: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/lists", tc.Token, map[string]any{"name": "API List"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space list: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	listID := uint(dataObject(t, rec)["ID"].(float64))

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/lists", tc.Token, nil)
	if rec.Code != http.StatusOK || len(decodeJSON(t, rec)["data"].([]any)) != 2 {
		t.Fatalf("list space lists: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/folders/"+itoa(folderID)+"/lists", tc.Token, nil)
	if rec.Code != http.StatusOK || len(decodeJSON(t, rec)["data"].([]any)) != 0 {
		t.Fatalf("list folder lists: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/folders/"+itoa(folderID)+"/lists", tc.Token, map[string]any{"name": "Folder API List"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder list: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	folderListID := uint(dataObject(t, rec)["ID"].(float64))

	updatedList := "Updated List"
	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/lists/"+itoa(listID)+"/", tc.Token, map[string]any{"name": updatedList})
	if rec.Code != http.StatusOK || dataObject(t, rec)["name"] != updatedList {
		t.Fatalf("update list: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token, nil)
	if rec.Code != http.StatusOK || len(decodeJSON(t, rec)["data"].([]any)) != 0 {
		t.Fatalf("list statuses: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token, map[string]any{
		"name": "Custom", "color": "#123456", "position": 1, "type": 1, "list_id": listID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	statusID := uint(dataObject(t, rec)["ID"].(float64))

	rec = request(t, tc.Router, http.MethodPut, "/api/v1/lists/"+itoa(listID)+"/statuses/"+itoa(statusID), tc.Token, map[string]any{"name": "Renamed Status", "position": 1})
	if rec.Code != http.StatusOK || dataObject(t, rec)["name"] != "Renamed Status" {
		t.Fatalf("update status: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token, map[string]any{"name": "API Tag", "color": "#abcdef"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tag: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	tagID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token, nil)
	if rec.Code != http.StatusOK || len(decodeJSON(t, rec)["data"].([]any)) != 1 {
		t.Fatalf("list tags: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPut, "/api/v1/spaces/"+itoa(spaceID)+"/tags/"+itoa(tagID), tc.Token, map[string]any{"name": "Updated Tag", "color": "#fedcba"})
	if rec.Code != http.StatusOK || dataObject(t, rec)["name"] != "Updated Tag" {
		t.Fatalf("update tag: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/spaces/"+itoa(spaceID)+"/tags/"+itoa(tagID), tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete tag: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/lists/"+itoa(listID)+"/statuses/"+itoa(statusID), tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/folders/"+itoa(folderID)+"/", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete folder: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/lists/"+itoa(folderListID)+"/", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete folder list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var folder domain.Folder
	if err := tc.DB.First(&folder, folderID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("folder lookup after delete error = %v, want record not found", err)
	}
	var tag domain.Tag
	if err := tc.DB.First(&tag, tagID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("tag lookup after delete error = %v, want record not found", err)
	}
}

func TestAPI_TaskLifecycleAndDatabaseRelations(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token, map[string]any{
		"name": "Task Status", "color": "#112233", "position": 1, "type": 1, "list_id": listID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task status: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	statusID := uint(dataObject(t, rec)["ID"].(float64))

	var status domain.TaskStatus
	if err := tc.DB.First(&status, statusID).Error; err != nil {
		t.Fatalf("find task status: %v", err)
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token, map[string]any{"name": "Task Tag", "color": "#112233"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task tag: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	tagID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", tc.Token, map[string]any{
		"title": "Initial Task", "description": "initial description", "status_id": statusID,
		"priority": 1, "time_estimate": 30, "tag_ids": []uint{tagID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	taskID := responseID(t, rec)

	var task domain.Task
	if err := tc.DB.Preload("Tags").First(&task, taskID).Error; err != nil {
		t.Fatalf("find task: %v", err)
	}
	if task.UserID != tc.UserID || task.ListID != listID || len(task.Tags) != 1 || task.Tags[0].ID != tagID {
		t.Fatalf("stored task = %+v, tags = %+v", task, task.Tags)
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/lists/"+itoa(listID)+"/tasks?page=1&limit=10", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	tasksBody := decodeJSON(t, rec)
	if len(tasksBody["data"].([]any)) != 1 || tasksBody["pagination"].(map[string]any)["total"] != float64(1) {
		t.Fatalf("tasks response = %v", tasksBody)
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if dataObject(t, rec)["title"] != "Initial Task" {
		t.Fatalf("task response = %v", rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID)+"/", tc.Token, map[string]any{
		"title": "Updated Task", "priority": 3, "time_estimate": 45,
	})
	if rec.Code != http.StatusOK || dataObject(t, rec)["title"] != "Updated Task" {
		t.Fatalf("update task: %d %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(taskID)+"/", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete task: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := tc.DB.First(&task, taskID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("task lookup after delete error = %v, want record not found", err)
	}
}

func TestAPI_TaskRejectsCrossSpaceTagsAndAssignees(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token, map[string]any{
		"name": "Scoped Status", "color": "#112233", "position": 1, "type": 1, "list_id": listID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create scoped status: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	statusID := responseID(t, rec)

	otherSpaceID := createSpace(t, tc)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(otherSpaceID)+"/tags", tc.Token, map[string]any{
		"name": "Foreign Tag", "color": "#abcdef",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create foreign tag: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	foreignTagID := responseID(t, rec)

	_, outsiderToken := newUser(t, tc, fmt.Sprintf("foreign-assignee-%d@example.test", time.Now().UnixNano()))
	var outsider domain.User
	if err := tc.DB.Where("email LIKE ?", "foreign-assignee-%@example.test").Order("id DESC").First(&outsider).Error; err != nil {
		t.Fatalf("find foreign assignee: %v", err)
	}
	if outsiderToken == "" {
		t.Fatal("foreign assignee token is empty")
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", tc.Token, map[string]any{
		"title": "Foreign tag task", "status_id": statusID, "tag_ids": []uint{foreignTagID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign tag task: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", tc.Token, map[string]any{
		"title": "Foreign assignee task", "status_id": statusID, "assignee_ids": []uint{outsider.ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign assignee task: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
