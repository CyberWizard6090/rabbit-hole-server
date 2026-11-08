package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const contactsPath = "/api/v1/contacts"

func requestContact(t *testing.T, tc *TestContext, token string, contactID uint) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, tc.Router, http.MethodPost, contactsPath, token, map[string]any{"contact_id": contactID})
}

func listIDs(t *testing.T, tc *TestContext, path, token string) []float64 {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, path, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d: %s", path, rec.Code, rec.Body.String())
	}
	ids := []float64{}
	for _, item := range dataList(t, rec) {
		entry := item.(map[string]any)
		if user, ok := entry["user"].(map[string]any); ok {
			entry = user
		}
		ids = append(ids, entry["id"].(float64))
	}
	return ids
}

func TestContacts_RequestIsPendingUntilTheOtherUserAccepts(t *testing.T) {
	tc := newTestContext(t)
	aliceID, alice := newUser(t, tc, "contact-alice@example.test")
	bobID, bob := newUser(t, tc, "contact-bob@example.test")

	if rec := requestContact(t, tc, alice, bobID); rec.Code != http.StatusAccepted {
		t.Fatalf("request: %d: %s", rec.Code, rec.Body.String())
	}
	if ids := listIDs(t, tc, contactsPath, alice); len(ids) != 0 {
		t.Fatalf("a pending request must not be a contact: %v", ids)
	}
	if ids := listIDs(t, tc, contactsPath, bob); len(ids) != 0 {
		t.Fatalf("a pending request must not be a contact for the target either: %v", ids)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/outgoing", alice); len(ids) != 1 || ids[0] != float64(bobID) {
		t.Fatalf("outgoing = %v", ids)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/incoming", bob); len(ids) != 1 || ids[0] != float64(aliceID) {
		t.Fatalf("incoming = %v", ids)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/incoming", alice); len(ids) != 0 {
		t.Fatalf("the requester has an incoming request: %v", ids)
	}

	workspace := newWorkspaceOwnerFor(t, tc, alice)
	rec := request(t, tc.Router, http.MethodPost, membersPath(workspace, ""), alice, map[string]any{"user_id": bobID, "role": "Member"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a pending contact must not be addable to a workspace: %d: %s", rec.Code, rec.Body.String())
	}

	if rec := request(t, tc.Router, http.MethodPost, contactsPath+"/requests/"+itoa(bobID)+"/accept", alice, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("the requester must not accept their own request: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, contactsPath+"/requests/"+itoa(aliceID)+"/accept", bob, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept: %d: %s", rec.Code, rec.Body.String())
	}
	if ids := listIDs(t, tc, contactsPath, alice); len(ids) != 1 || ids[0] != float64(bobID) {
		t.Fatalf("alice contacts = %v", ids)
	}
	if ids := listIDs(t, tc, contactsPath, bob); len(ids) != 1 || ids[0] != float64(aliceID) {
		t.Fatalf("bob contacts = %v", ids)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/incoming", bob); len(ids) != 0 {
		t.Fatalf("accepted request still pending: %v", ids)
	}

	if rec := request(t, tc.Router, http.MethodPost, membersPath(workspace, ""), alice, map[string]any{"user_id": bobID, "role": "Member"}); rec.Code != http.StatusCreated {
		t.Fatalf("accepted contact can be added: %d: %s", rec.Code, rec.Body.String())
	}

	if rec := request(t, tc.Router, http.MethodPost, contactsPath+"/requests/"+itoa(aliceID)+"/accept", bob, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("accepting twice: %d", rec.Code)
	}

	if rec := request(t, tc.Router, http.MethodDelete, contactsPath+"/"+itoa(aliceID), bob, nil); rec.Code != http.StatusOK {
		t.Fatalf("either side can remove the contact: %d: %s", rec.Code, rec.Body.String())
	}
	if ids := listIDs(t, tc, contactsPath, alice); len(ids) != 0 {
		t.Fatalf("contact survived removal: %v", ids)
	}
	if rec := request(t, tc.Router, http.MethodDelete, contactsPath+"/"+itoa(aliceID), bob, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("removing a missing contact: %d", rec.Code)
	}
}

func newWorkspaceOwnerFor(t *testing.T, tc *TestContext, token string) uint {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{"name": "contacts workspace"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func TestContacts_DeclineHidesTheOutcomeFromTheRequester(t *testing.T) {
	tc := newTestContext(t)
	aliceID, alice := newUser(t, tc, "decline-alice@example.test")
	bobID, bob := newUser(t, tc, "decline-bob@example.test")

	requestContact(t, tc, alice, bobID)
	if rec := request(t, tc.Router, http.MethodPost, contactsPath+"/requests/"+itoa(aliceID)+"/decline", bob, nil); rec.Code != http.StatusOK {
		t.Fatalf("decline: %d: %s", rec.Code, rec.Body.String())
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/incoming", bob); len(ids) != 0 {
		t.Fatalf("declined request is still listed: %v", ids)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/outgoing", alice); len(ids) != 0 {
		t.Fatalf("the requester can tell the request was declined: %v", ids)
	}

	if rec := requestContact(t, tc, alice, bobID); rec.Code != http.StatusAccepted {
		t.Fatalf("repeat after decline must look like a success: %d", rec.Code)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/incoming", bob); len(ids) != 0 {
		t.Fatalf("a declined requester must not be able to re-request: %v", ids)
	}

	if rec := requestContact(t, tc, bob, aliceID); rec.Code != http.StatusAccepted {
		t.Fatalf("the decliner may ask back: %d", rec.Code)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/incoming", alice); len(ids) != 1 || ids[0] != float64(bobID) {
		t.Fatalf("incoming for alice = %v", ids)
	}
}

func TestContacts_MutualRequestsBecomeAContact(t *testing.T) {
	tc := newTestContext(t)
	aliceID, alice := newUser(t, tc, "mutual-alice@example.test")
	bobID, bob := newUser(t, tc, "mutual-bob@example.test")

	requestContact(t, tc, alice, bobID)
	requestContact(t, tc, bob, aliceID)
	if ids := listIDs(t, tc, contactsPath, alice); len(ids) != 1 || ids[0] != float64(bobID) {
		t.Fatalf("contacts = %v", ids)
	}
	var rows int64
	tc.DB.Table("user_contacts").Count(&rows)
	if rows != 1 {
		t.Fatalf("%d rows for one relationship", rows)
	}
}

func TestContacts_CancelOutgoingRequest(t *testing.T) {
	tc := newTestContext(t)
	_, alice := newUser(t, tc, "cancel-alice@example.test")
	bobID, bob := newUser(t, tc, "cancel-bob@example.test")

	requestContact(t, tc, alice, bobID)
	if rec := request(t, tc.Router, http.MethodDelete, contactsPath+"/requests/"+itoa(bobID), alice, nil); rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d: %s", rec.Code, rec.Body.String())
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/incoming", bob); len(ids) != 0 {
		t.Fatalf("canceled request still visible: %v", ids)
	}
	if rec := request(t, tc.Router, http.MethodDelete, contactsPath+"/requests/"+itoa(bobID), alice, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cancel twice: %d", rec.Code)
	}
}

func TestContacts_RequestAnswersAreIdenticalForEveryTarget(t *testing.T) {
	tc := newTestContext(t)
	aliceID, alice := newUser(t, tc, "oracle-alice@example.test")
	bobID, _ := newUser(t, tc, "oracle-bob@example.test")

	first := requestContact(t, tc, alice, bobID)
	repeat := requestContact(t, tc, alice, bobID)
	unknown := requestContact(t, tc, alice, 987654)
	for name, rec := range map[string]*httptest.ResponseRecorder{"first": first, "repeat": repeat, "unknown": unknown} {
		if rec.Code != http.StatusAccepted || rec.Body.String() != first.Body.String() {
			t.Fatalf("%s answer differs: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	var rows int64
	tc.DB.Table("user_contacts").Count(&rows)
	if rows != 1 {
		t.Fatalf("%d rows stored, want 1", rows)
	}

	if rec := requestContact(t, tc, alice, aliceID); rec.Code != http.StatusBadRequest {
		t.Fatalf("requesting yourself: %d", rec.Code)
	}
	if rec := requestContact(t, tc, alice, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("zero id: %d", rec.Code)
	}
}

func TestContacts_RequestsRequireAuthenticationAndAreScopedToTheCaller(t *testing.T) {
	tc := newTestContext(t)
	aliceID, alice := newUser(t, tc, "scope-alice@example.test")
	bobID, bob := newUser(t, tc, "scope-bob@example.test")
	_, carol := newUser(t, tc, "scope-carol@example.test")

	if rec := requestContact(t, tc, "", bobID); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous request: %d", rec.Code)
	}
	requestContact(t, tc, alice, bobID)

	if rec := request(t, tc.Router, http.MethodPost, contactsPath+"/requests/"+itoa(aliceID)+"/accept", carol, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a third party accepted: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, contactsPath+"/requests/"+itoa(bobID), carol, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a third party canceled: %d", rec.Code)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/incoming", carol); len(ids) != 0 {
		t.Fatalf("carol sees %v", ids)
	}
	if ids := listIDs(t, tc, contactsPath+"/requests/outgoing", bob); len(ids) != 0 {
		t.Fatalf("bob sees outgoing %v", ids)
	}
	if rec := request(t, tc.Router, http.MethodPost, contactsPath+"/requests/abc/accept", bob, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}
}

func TestContacts_PendingRequestsAreCappedPerUser(t *testing.T) {
	tc := newTestContext(t)
	_, alice := newUser(t, tc, "cap-alice@example.test")
	var aliceID uint
	tc.DB.Table("users").Select("id").Where("email = ?", "cap-alice@example.test").Scan(&aliceID)
	for i := 0; i < 100; i++ {
		var id uint
		if err := tc.DB.Raw(`INSERT INTO users (email, username, password_hash, created_at, updated_at)
			VALUES (?, ?, 'x', now(), now()) RETURNING id`, "cap-target-"+itoa(uint(i))+"@example.test", "cap-target-"+itoa(uint(i))).Scan(&id).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := tc.DB.Exec("INSERT INTO user_contacts (user_id, contact_id) VALUES (?, ?)", aliceID, id).Error; err != nil {
			t.Fatalf("seed request: %v", err)
		}
	}
	extraID, _ := newUser(t, tc, "cap-extra@example.test")
	rec := requestContact(t, tc, alice, extraID)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over the cap: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestContacts_RequestEndpointIsRateLimited(t *testing.T) {
	tc := newTestContext(t)
	_, alice := newUser(t, tc, "rate-alice@example.test")
	limited := false
	for i := 0; i < 40; i++ {
		rec := requestContact(t, tc, alice, uint(900000+i))
		if rec.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("POST /contacts was never rate limited")
	}
}

func TestMembers_OnlyAnAcceptedContactOfTheActorCanBeAdded(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "gate")
	strangerID, stranger := newUser(t, tc, "gate-stranger@example.test")
	pendingID, _ := newUser(t, tc, "gate-pending@example.test")

	if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token, map[string]any{"user_id": strangerID, "role": "Guest"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("stranger: %d", rec.Code)
	}
	requestContact(t, tc, owner.token, pendingID)
	if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token, map[string]any{"user_id": pendingID, "role": "Guest"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("pending: %d", rec.Code)
	}
	requestContact(t, tc, stranger, owner.userID)
	if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token, map[string]any{"user_id": strangerID, "role": "Guest"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a request made by the target is not consent of the actor: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, contactsPath+"/requests/"+itoa(strangerID)+"/accept", owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token, map[string]any{"user_id": strangerID, "role": "Guest"}); rec.Code != http.StatusCreated {
		t.Fatalf("accepted from either direction: %d", rec.Code)
	}
}
