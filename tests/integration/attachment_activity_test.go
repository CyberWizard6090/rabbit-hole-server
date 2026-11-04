package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestAttachments_AreRecordedInTheActivityLog(t *testing.T) {
	tc := newTestContext(t)
	owner, taskID, memberToken, guestToken, _ := filesFixture(t, tc)
	base := "/api/v1/tasks/" + itoa(taskID) + "/attachments"

	rec := uploadRequest(t, tc, base, owner.token, "report.txt", []byte("quarterly numbers"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d: %s", rec.Code, rec.Body.String())
	}
	keptID := responseID(t, rec)
	rec = uploadRequest(t, tc, base, owner.token, "draft.txt", []byte("draft"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d: %s", rec.Code, rec.Body.String())
	}
	deletedID := responseID(t, rec)

	if rec := uploadRequest(t, tc, base, owner.token, "empty.txt", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty file: expected 400, got %d", rec.Code)
	}
	if rec := uploadRequest(t, tc, base, guestToken, "guest.txt", []byte("x")); rec.Code != http.StatusForbidden {
		t.Fatalf("guest upload: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, base+"/"+itoa(deletedID), guestToken, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("guest delete: expected 403, got %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, base+"/"+itoa(deletedID), memberToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("member delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodDelete, base+"/"+itoa(deletedID), owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete twice: expected 404, got %d", rec.Code)
	}

	items, total := fetchActivity(t, tc, guestToken, taskID, "?limit=100")
	var attachments []activityItem
	for _, it := range items {
		if strings.HasPrefix(it.Action, "attachment.") {
			attachments = append(attachments, it)
		}
	}
	if len(attachments) != 3 || int(total) != len(items) {
		t.Fatalf("attachment entries = %+v (total %v), want exactly 3: failed requests must not be logged", attachments, total)
	}

	deleted, second, first := attachments[0], attachments[1], attachments[2]
	if deleted.Action != "attachment.deleted" || second.Action != "attachment.created" || first.Action != "attachment.created" {
		t.Fatalf("order (newest first) = %s, %s, %s", deleted.Action, second.Action, first.Action)
	}
	if first.Actor != owner.userID || second.Actor != owner.userID {
		t.Fatalf("upload actors = %d, %d, want the owner %d", first.Actor, second.Actor, owner.userID)
	}
	if deleted.Actor == owner.userID {
		t.Fatalf("delete actor = %d, want the member who deleted it", deleted.Actor)
	}
	if first.Old != nil || first.New == nil {
		t.Fatalf("created entry = %+v", first)
	}
	if got := first.New.(map[string]any); got["id"] != float64(keptID) || got["name"] != "report.txt" || got["size"] != float64(17) {
		t.Fatalf("created new_value = %v", got)
	}
	if deleted.New != nil {
		t.Fatalf("deleted entry carries new_value: %+v", deleted)
	}
	got := deleted.Old.(map[string]any)
	if got["id"] != float64(deletedID) || got["name"] != "draft.txt" || got["size"] != float64(5) || got["deleted_by"] != float64(deleted.Actor) {
		t.Fatalf("deleted old_value = %v", got)
	}
}
