package integration

import (
	"net/http"
	"testing"
)

func TestComments_CreateMentionEditAndDelete(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "comments")
	memberID, memberToken := newUser(t, tc, "comments-member@example.test")
	guestID, guestToken := newUser(t, tc, "comments-guest@example.test")
	outsiderID, outsiderToken := newUser(t, tc, "comments-outsider@example.test")

	for _, u := range []struct {
		id   uint
		role string
	}{{memberID, "Member"}, {guestID, "Guest"}} {
		addContact(t, tc, owner.token, u.id)
		rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token,
			map[string]any{"user_id": u.id, "role": u.role})
		if rec.Code != http.StatusCreated {
			t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
		}
	}

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", owner.token, map[string]any{"name": "list"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list: %d: %s", rec.Code, rec.Body.String())
	}
	listID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", owner.token,
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", owner.token,
		map[string]any{"title": "discussed task", "status_id": responseID(t, rec)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	taskID := responseID(t, rec)
	commentsURL := "/api/v1/tasks/" + itoa(taskID) + "/comments"

	rec = request(t, tc.Router, http.MethodPost, commentsURL, owner.token,
		map[string]any{"body": "  looks good @member  ", "mention_user_ids": []uint{memberID, memberID}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create comment: %d: %s", rec.Code, rec.Body.String())
	}
	created := dataObject(t, rec)
	commentID := responseID(t, rec)
	if created["body"] != "looks good @member" || created["edited"] != false {
		t.Fatalf("comment = %v", created)
	}
	author := created["author"].(map[string]any)
	if uint(author["id"].(float64)) != owner.userID {
		t.Fatalf("author = %v", author)
	}
	if _, leaked := author["email"]; leaked {
		t.Fatalf("author exposes the email address: %v", author)
	}
	mentions := created["mentions"].([]any)
	if len(mentions) != 1 || uint(mentions[0].(map[string]any)["id"].(float64)) != memberID {
		t.Fatalf("mentions = %v", mentions)
	}

	if rec := request(t, tc.Router, http.MethodPost, commentsURL, owner.token,
		map[string]any{"body": "hi", "mention_user_ids": []uint{outsiderID}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("mention of a non-member: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPost, commentsURL, owner.token, map[string]any{"body": "   "}); rec.Code == http.StatusCreated {
		t.Fatalf("blank comment accepted")
	}

	if rec := request(t, tc.Router, http.MethodPost, commentsURL, guestToken, map[string]any{"body": "from a guest"}); rec.Code != http.StatusForbidden {
		t.Fatalf("guest comment: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, commentsURL, outsiderToken, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list: expected 403, got %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodPost, commentsURL, memberToken, map[string]any{"body": "second"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("member comment: %d: %s", rec.Code, rec.Body.String())
	}
	memberCommentID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodGet, commentsURL+"?limit=1&page=2", guestToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("guest list: %d: %s", rec.Code, rec.Body.String())
	}
	page := decodeJSON(t, rec)
	items := page["data"].([]any)
	if len(items) != 1 || uint(items[0].(map[string]any)["id"].(float64)) != memberCommentID {
		t.Fatalf("page 2 = %v, want the member comment (oldest first)", items)
	}
	if total := page["pagination"].(map[string]any)["total"].(float64); total != 2 {
		t.Fatalf("total = %v, want 2", total)
	}

	editURL := commentsURL + "/" + itoa(commentID)
	if rec := request(t, tc.Router, http.MethodPatch, editURL, memberToken, map[string]any{"body": "hijacked"}); rec.Code != http.StatusForbidden {
		t.Fatalf("edit someone else's comment: expected 403, got %d", rec.Code)
	}
	rec = request(t, tc.Router, http.MethodPatch, editURL, owner.token,
		map[string]any{"body": "looks great", "mention_user_ids": []uint{}})
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: %d: %s", rec.Code, rec.Body.String())
	}
	edited := dataObject(t, rec)
	if edited["body"] != "looks great" || edited["edited"] != true || len(edited["mentions"].([]any)) != 0 {
		t.Fatalf("edited comment = %v", edited)
	}

	if rec := request(t, tc.Router, http.MethodDelete, commentsURL+"/"+itoa(memberCommentID), guestToken, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("guest deletes someone else's comment: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, commentsURL+"/"+itoa(memberCommentID), owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("owner deletes a member comment: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodDelete, commentsURL+"/"+itoa(memberCommentID), owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete twice: expected 404, got %d", rec.Code)
	}

	otherTask := func() uint {
		rec := request(t, tc.Router, http.MethodGet, "/api/v1/lists/"+itoa(listID)+"/statuses", owner.token, nil)
		statuses := decodeJSON(t, rec)["data"].([]any)
		statusID := uint(statuses[0].(map[string]any)["id"].(float64))
		rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", owner.token,
			map[string]any{"title": "another task", "status_id": statusID})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create second task: %d: %s", rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}()
	crossURL := "/api/v1/tasks/" + itoa(otherTask) + "/comments/" + itoa(commentID)
	if rec := request(t, tc.Router, http.MethodPatch, crossURL, owner.token, map[string]any{"body": "x"}); rec.Code != http.StatusNotFound {
		t.Fatalf("comment addressed through another task: expected 404, got %d", rec.Code)
	}

	var remaining int64
	tc.DB.Table("task_comments").Where("task_id = ? AND deleted_at IS NULL", taskID).Count(&remaining)
	if remaining != 1 {
		t.Fatalf("live comments = %d, want 1", remaining)
	}
}
