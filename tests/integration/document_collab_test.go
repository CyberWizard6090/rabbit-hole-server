package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/reearth/ygo/crdt"

	"rabbit-hole-server/internal/collab"
	"rabbit-hole-server/internal/realtime"
)

type roomEnv struct {
	*notebookEnv
	rt    *rtEnv
	docID uint
}

func newRoomEnv(t *testing.T, opts collab.Options) *roomEnv {
	t.Helper()
	e := newNotebookEnv(t)
	rt := newRealtimeEnv(t, realtime.Options{})
	rt.container.DocumentCollab.WithOptions(opts)
	// The notebook and its document are created through the same router the sockets use.
	e.tc.Router = rt.router
	nb := e.newNotebook(t, "room")
	return &roomEnv{notebookEnv: e, rt: rt, docID: e.newDocument(t, nb, "shared")}
}

type roomClient struct {
	conn *websocket.Conn
}

func (e *roomEnv) ticket(t *testing.T, token string, docID uint) string {
	t.Helper()
	return e.rt.ticket(t, token, "documents", docID)
}

func (e *roomEnv) dial(t *testing.T, token string, since *int64) *roomClient {
	t.Helper()
	url := "ws" + strings.TrimPrefix(e.rt.server.URL, "http") + fmt.Sprintf("/api/v1/documents/%d/events?ticket=%s", e.docID, e.ticket(t, token, e.docID))
	if since != nil {
		url += fmt.Sprintf("&since=%d", *since)
	}
	// The context of Dial also owns the connection, so it lives until the test ends.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.SetReadLimit(4 << 20)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return &roomClient{conn: conn}
}

func (c *roomClient) read(t *testing.T) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, raw, err := c.conn.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("frame is not JSON: %s", raw)
	}
	return frame
}

func (c *roomClient) expect(t *testing.T, kind string) map[string]any {
	t.Helper()
	frame := c.read(t)
	if frame["type"] != kind {
		t.Fatalf("want a %q frame, got %v", kind, frame)
	}
	return frame
}

func (c *roomClient) send(t *testing.T, frame map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(frame)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func userOf(frame map[string]any) uint { return uint(frame["user"].(map[string]any)["id"].(float64)) }

func TestDocumentRoom_RelaysUpdatesAndPresence(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	member := e.dial(t, e.member, nil)
	ready := member.expect(t, "ready")
	if ready["can_write"] != true || len(ready["peers"].([]any)) != 0 || uint(ready["user"].(map[string]any)["id"].(float64)) != e.memberID {
		t.Fatalf("ready of the first editor: %v", ready)
	}

	owner := e.dial(t, e.owner.token, nil)
	ownerReady := owner.expect(t, "ready")
	if len(ownerReady["peers"].([]any)) != 1 || userOf(member.expect(t, "peer_joined")) != e.owner.userID {
		t.Fatalf("the owner should see the member, and the member the owner: %v", ownerReady)
	}

	member.send(t, map[string]any{"type": "update", "update": upd("hello"), "touched": []string{"block-1"}, "ref": "r1"})
	ack := member.expect(t, "ack")
	if ack["seq"].(float64) != 1 || ack["ref"] != "r1" {
		t.Fatalf("ack: %v", ack)
	}
	update := owner.expect(t, "update")
	if update["seq"].(float64) != 1 || uint(update["user_id"].(float64)) != e.memberID || update["update"] != upd("hello") {
		t.Fatalf("the owner should get the update: %v", update)
	}
	// Frames are read in order, so a stray frame would show up as the next expectation.

	// The update is stored and attributed to the member.
	state := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/state", e.guest, nil, http.StatusOK)["data"].(map[string]any)
	if state["last_seq"].(float64) != 1 {
		t.Fatalf("state after the relayed update: %v", state)
	}
	activity := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/blocks/activity", e.guest, nil, http.StatusOK)["data"].([]any)
	if len(activity) != 1 || uint(activity[0].(map[string]any)["last_edited_by"].(map[string]any)["id"].(float64)) != e.memberID {
		t.Fatalf("block activity: %v", activity)
	}

	// Presence: the user shown is the authenticated one, whatever the client puts in its state.
	owner.send(t, map[string]any{"type": "awareness", "state": map[string]any{"cursor": 4, "user": map[string]any{"id": 999}}})
	aware := member.expect(t, "awareness")
	if userOf(aware) != e.owner.userID || aware["state"].(map[string]any)["cursor"].(float64) != 4 {
		t.Fatalf("awareness: %v", aware)
	}

	presence := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/presence", e.guest, nil, http.StatusOK)["data"].([]any)
	if len(presence) != 2 {
		t.Fatalf("presence: %v", presence)
	}
	e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/presence", e.outsider, nil, http.StatusForbidden)

	_ = owner.conn.Close(websocket.StatusNormalClosure, "bye")
	if left := member.expect(t, "peer_left"); left["conn"] != aware["conn"] {
		t.Fatalf("peer_left: %v", left)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/presence", e.guest, nil, http.StatusOK)
		if len(rec["data"].([]any)) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the owner should leave the presence list")
}

func TestDocumentRoom_ReadOnlyViewerSeesEditsButCannotSend(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	member := e.dial(t, e.member, nil)
	member.expect(t, "ready")
	guest := e.dial(t, e.guest, nil)
	if ready := guest.expect(t, "ready"); ready["can_write"] != false {
		t.Fatalf("a guest cannot write: %v", ready)
	}
	member.expect(t, "peer_joined")

	guest.send(t, map[string]any{"type": "update", "update": upd("nope"), "ref": "g1"})
	if refused := guest.expect(t, "error"); refused["code"] != "FORBIDDEN" || refused["ref"] != "g1" {
		t.Fatalf("a viewer's update must be refused: %v", refused)
	}
	// Frames are read in order, so a stray frame would show up as the next expectation.

	member.send(t, map[string]any{"type": "update", "update": upd("by member")})
	member.expect(t, "ack")
	if got := guest.expect(t, "update"); got["update"] != upd("by member") {
		t.Fatalf("the viewer should still receive edits: %v", got)
	}

	guest.send(t, map[string]any{"type": "awareness", "state": map[string]any{"viewing": true}})
	if aware := member.expect(t, "awareness"); userOf(aware) == e.memberID {
		t.Fatalf("a viewer is present too: %v", aware)
	}

	state := e.call(t, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/state", e.guest, nil, http.StatusOK)["data"].(map[string]any)
	if state["last_seq"].(float64) != 1 {
		t.Fatalf("only the member's update is stored: %v", state)
	}
}

func TestDocumentRoom_RESTUpdatesReachOpenConnections(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	viewer := e.dial(t, e.guest, nil)
	viewer.expect(t, "ready")

	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/updates", e.member, map[string]any{"update": upd("rest")}, http.StatusCreated)
	got := viewer.expect(t, "update")
	if got["update"] != upd("rest") || got["seq"].(float64) != 1 || uint(got["user_id"].(float64)) != e.memberID {
		t.Fatalf("an update posted over REST must reach the room: %v", got)
	}
}

func TestDocumentRoom_CatchUpAfterConnecting(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	post := func(text string) {
		e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/updates", e.member, map[string]any{"update": upd(text)}, http.StatusCreated)
	}
	for _, text := range []string{"a", "b", "c"} {
		post(text)
	}

	since := int64(1)
	client := e.dial(t, e.guest, &since)
	client.expect(t, "ready")
	first, second := client.expect(t, "update"), client.expect(t, "update")
	if first["seq"].(float64) != 2 || first["update"] != upd("b") || second["seq"].(float64) != 3 || second["update"] != upd("c") {
		t.Fatalf("catch-up after number 1 should be updates 2 and 3: %v %v", first, second)
	}
	if synced := client.expect(t, "synced"); synced["last_seq"].(float64) != 3 {
		t.Fatalf("synced: %v", synced)
	}

	// An explicit request over the open connection.
	client.send(t, map[string]any{"type": "sync", "since": 2})
	if got := client.expect(t, "update"); got["seq"].(float64) != 3 {
		t.Fatalf("sync since 2: %v", got)
	}
	client.expect(t, "synced")

	// After compaction past the client's number it must reload the state.
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/snapshot", e.member, map[string]any{"upto_seq": 2, "state": validState()}, http.StatusOK)
	client.send(t, map[string]any{"type": "sync", "since": 1})
	if got := client.expect(t, "resync"); got["reason"] != "snapshot" {
		t.Fatalf("resync: %v", got)
	}
	late := e.dial(t, e.guest, &since)
	late.expect(t, "ready")
	late.expect(t, "resync")

	current := int64(2)
	fresh := e.dial(t, e.guest, &current)
	fresh.expect(t, "ready")
	if got := fresh.expect(t, "update"); got["seq"].(float64) != 3 {
		t.Fatalf("catch-up after the snapshot: %v", got)
	}
	fresh.expect(t, "synced")
}

func TestDocumentRoom_TicketsAreScopedAndSingleUse(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/events/ticket", e.outsider, nil, http.StatusForbidden)

	ticket := e.ticket(t, e.member, e.docID)
	base := "ws" + strings.TrimPrefix(e.rt.server.URL, "http")
	dial := func(path string) (int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		conn, resp, err := websocket.Dial(ctx, base+path, nil)
		if err == nil {
			_ = conn.CloseNow()
			return http.StatusSwitchingProtocols, nil
		}
		if resp != nil {
			return resp.StatusCode, err
		}
		return 0, err
	}

	other := e.newDocument(t, e.newNotebook(t, "other"), "other doc")
	if status, _ := dial(fmt.Sprintf("/api/v1/documents/%d/events?ticket=%s", other, e.ticket(t, e.member, e.docID))); status != http.StatusUnauthorized {
		t.Fatalf("a ticket for another document: %d", status)
	}
	if status, _ := dial(fmt.Sprintf("/api/v1/documents/%d/events?ticket=%s&since=-1", e.docID, e.ticket(t, e.member, e.docID))); status != http.StatusBadRequest {
		t.Fatalf("a negative since: %d", status)
	}
	if status, err := dial(fmt.Sprintf("/api/v1/documents/%d/events?ticket=%s", e.docID, ticket)); status != http.StatusSwitchingProtocols {
		t.Fatalf("the first use of a ticket: %d %v", status, err)
	}
	if status, _ := dial(fmt.Sprintf("/api/v1/documents/%d/events?ticket=%s", e.docID, ticket)); status != http.StatusUnauthorized {
		t.Fatalf("a ticket works once: %d", status)
	}
	if status, _ := dial(fmt.Sprintf("/api/v1/documents/%d/events", e.docID)); status != http.StatusUnauthorized {
		t.Fatalf("no ticket: %d", status)
	}
}

func TestDocumentRoom_BadFramesAreRefusedWithoutClosing(t *testing.T) {
	e := newRoomEnv(t, collab.Options{UpdatesPerSecond: 1})
	client := e.dial(t, e.member, nil)
	client.expect(t, "ready")

	raw := func(payload string) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	raw("not json")
	if got := client.expect(t, "error"); got["code"] != "INVALID_MESSAGE" {
		t.Fatalf("garbage: %v", got)
	}
	client.send(t, map[string]any{"type": "dance"})
	if got := client.expect(t, "error"); got["code"] != "INVALID_MESSAGE" {
		t.Fatalf("unknown type: %v", got)
	}
	client.send(t, map[string]any{"type": "awareness", "state": map[string]any{"blob": strings.Repeat("x", 5000)}})
	if got := client.expect(t, "error"); got["code"] != "AWARENESS_TOO_LARGE" {
		t.Fatalf("big awareness: %v", got)
	}
	client.send(t, map[string]any{"type": "update", "ref": "e"})
	if got := client.expect(t, "error"); got["code"] != "INVALID_UPDATE" || got["ref"] != "e" {
		t.Fatalf("empty update: %v", got)
	}
	client.send(t, map[string]any{"type": "update", "update": b64(strings.Repeat("a", 512*1024+1)), "ref": "big"})
	if got := client.expect(t, "error"); got["code"] != "UPDATE_TOO_LARGE" {
		t.Fatalf("oversized update: %v", got)
	}
	client.send(t, map[string]any{"type": "sync", "since": -5})
	if got := client.expect(t, "error"); got["code"] != "INVALID_MESSAGE" {
		t.Fatalf("negative sync: %v", got)
	}

	// The rate limit is per connection: a burst beyond twice the rate is refused, the connection survives.
	limited := false
	for i := 0; i < 6; i++ {
		client.send(t, map[string]any{"type": "update", "update": upd("u"), "ref": fmt.Sprint(i)})
	}
	for i := 0; i < 6; i++ {
		if client.read(t)["code"] == "RATE_LIMITED" {
			limited = true
		}
	}
	if !limited {
		t.Fatal("a burst of updates must hit the rate limit")
	}
	client.send(t, map[string]any{"type": "awareness", "state": map[string]any{"still": "open"}})
}

func TestDocumentRoom_RecheckClosesAConnectionWhoseAccessWasRevokedWithoutANotice(t *testing.T) {
	// The membership is removed straight in the database, so nothing tells the room: only the periodic recheck can.
	e := newRoomEnv(t, collab.Options{RecheckInterval: 100 * time.Millisecond})
	guest := e.dial(t, e.guest, nil)
	guest.expect(t, "ready")

	guestID := uint(0)
	if err := e.tc.DB.Raw("SELECT id FROM users WHERE email = ?", "nb-guest@example.test").Scan(&guestID).Error; err != nil || guestID == 0 {
		t.Fatalf("find the guest: %v", err)
	}
	if err := e.tc.DB.Exec("UPDATE workspace_members SET deleted_at = now() WHERE user_id = ? AND workspace_id = ?", guestID, e.owner.workspaceID).Error; err != nil {
		t.Fatal(err)
	}

	code, reason := guest.closedWith(t)
	if code != websocket.StatusPolicyViolation || reason != "access revoked" {
		t.Fatalf("got %d %q, want a policy violation with the reason that stops clients from reconnecting", code, reason)
	}
}

// closedWith waits for the server to close the connection and returns the close status and reason.
func (c *roomClient) closedWith(t *testing.T) (websocket.StatusCode, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, _, err := c.conn.Read(ctx)
		if err == nil {
			continue // frames queued before the close (presence, ...)
		}
		var closed websocket.CloseError
		if errors.As(err, &closed) {
			return closed.Code, closed.Reason
		}
		t.Fatalf("the connection should be closed by the server, got %v", err)
	}
}

func TestDocumentRoom_DeletingTheDocumentClosesTheRoomAtOnce(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	member := e.dial(t, e.member, nil)
	member.expect(t, "ready")
	owner := e.dial(t, e.owner.token, nil)
	owner.expect(t, "ready")

	e.call(t, http.MethodDelete, "/api/v1/documents/"+itoa(e.docID), e.member, nil, http.StatusOK)

	for _, client := range []*roomClient{member, owner} {
		if _, reason := client.closedWith(t); reason != "document deleted" {
			t.Fatalf("reason = %q, want the reason that tells clients not to reconnect", reason)
		}
	}
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/events/ticket", e.member, nil, http.StatusForbidden)
}

func TestDocumentRoom_DeletingTheNotebookClosesEveryDocumentOfIt(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	var notebookID uint
	if err := e.tc.DB.Raw("SELECT notebook_id FROM documents WHERE id = ?", e.docID).Scan(&notebookID).Error; err != nil || notebookID == 0 {
		t.Fatalf("find the notebook: %v", err)
	}
	first := e.dial(t, e.member, nil)
	first.expect(t, "ready")

	elsewhereNotebook := e.newNotebook(t, "elsewhere")
	elsewhere := e.newDocument(t, elsewhereNotebook, "stays")
	staying := &roomEnv{notebookEnv: e.notebookEnv, rt: e.rt, docID: elsewhere}
	stay := staying.dial(t, e.member, nil)
	stay.expect(t, "ready")

	e.call(t, http.MethodDelete, "/api/v1/notebooks/"+itoa(notebookID), e.owner.token, nil, http.StatusOK)

	if _, reason := first.closedWith(t); reason != "document deleted" {
		t.Fatalf("reason = %q", reason)
	}
	// A document of another notebook is not affected.
	stay.send(t, map[string]any{"type": "update", "update": upd("still open"), "ref": "s"})
	if ack := stay.expect(t, "ack"); ack["seq"].(float64) != 1 {
		t.Fatalf("ack: %v", ack)
	}
}

func TestDocumentRoom_RemovingAMemberDisconnectsThemAtOnce(t *testing.T) {
	// The periodic recheck is two minutes by default: closing must not wait for it.
	e := newRoomEnv(t, collab.Options{})
	member := e.dial(t, e.member, nil)
	member.expect(t, "ready")
	owner := e.dial(t, e.owner.token, nil)
	owner.expect(t, "ready")
	member.expect(t, "peer_joined")

	e.call(t, http.MethodDelete, membersPath(e.owner.workspaceID, "/"+itoa(e.memberID)), e.owner.token, nil, http.StatusOK)

	if _, reason := member.closedWith(t); reason != "access changed" {
		t.Fatalf("reason = %q: the client reconnects and is refused by the ticket", reason)
	}
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/events/ticket", e.member, nil, http.StatusForbidden)

	// Everyone else stays.
	if left := owner.expect(t, "peer_left"); left["type"] != "peer_left" {
		t.Fatalf("the owner is told that the member left: %v", left)
	}
	owner.send(t, map[string]any{"type": "update", "update": upd("owner goes on"), "ref": "o"})
	owner.expect(t, "ack")
}

func TestDocumentRoom_ChangingARoleReconnectsThemWithTheNewRights(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	member := e.dial(t, e.member, nil)
	if ready := member.expect(t, "ready"); ready["can_write"] != true {
		t.Fatalf("a member may write: %v", ready)
	}

	e.call(t, http.MethodPatch, membersPath(e.owner.workspaceID, "/"+itoa(e.memberID)), e.owner.token, map[string]any{"role": "Guest"}, http.StatusOK)

	if _, reason := member.closedWith(t); reason != "access changed" {
		t.Fatalf("reason = %q", reason)
	}
	again := e.dial(t, e.member, nil)
	if ready := again.expect(t, "ready"); ready["can_write"] != false {
		t.Fatalf("after becoming a guest the member only reads: %v", ready)
	}
}

func TestDocumentRoom_DeletingTheWorkspaceAndSigningOutEverywhereDisconnect(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	guest := e.dial(t, e.guest, nil)
	guest.expect(t, "ready")
	member := e.dial(t, e.member, nil)
	member.expect(t, "ready")

	e.call(t, http.MethodPost, "/api/v1/auth/logout-all", e.member, nil, http.StatusOK)
	if _, reason := member.closedWith(t); reason != "access changed" {
		t.Fatalf("sign-out reason = %q", reason)
	}
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/events/ticket", e.member, nil, http.StatusUnauthorized)

	e.call(t, http.MethodDelete, "/api/v1/workspaces/"+itoa(e.owner.workspaceID), e.owner.token, nil, http.StatusOK)
	if _, reason := guest.closedWith(t); reason != "access changed" {
		t.Fatalf("workspace deletion reason = %q", reason)
	}
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/events/ticket", e.guest, nil, http.StatusForbidden)
}

func TestDocumentRoom_RefusesUpdatesThatBreakTheDocument(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	member := e.dial(t, e.member, nil)
	member.expect(t, "ready")
	guest := e.dial(t, e.guest, nil)
	guest.expect(t, "ready")
	member.expect(t, "peer_joined")

	member.send(t, map[string]any{"type": "update", "update": b64("garbage"), "ref": "g"})
	if got := member.expect(t, "error"); got["code"] != "INVALID_UPDATE" || got["ref"] != "g" {
		t.Fatalf("garbage: %v", got)
	}
	member.send(t, map[string]any{"type": "update", "update": deleteRoot(t), "ref": "r"})
	if got := member.expect(t, "error"); got["code"] != "INVALID_UPDATE" || got["ref"] != "r" {
		t.Fatalf("root deletion: %v", got)
	}
	// The connection stays open, and the first frame the other client gets is the good update: the refused ones
	// were never published.
	member.send(t, map[string]any{"type": "update", "update": upd("good"), "ref": "ok"})
	if got := member.expect(t, "ack"); got["seq"].(float64) != 1 {
		t.Fatalf("ack: %v", got)
	}
	if got := guest.read(t); got["type"] != "update" || got["update"] != upd("good") {
		t.Fatalf("the other client must only see the good update, got %v", got)
	}
}

func TestDocumentRoom_UserBudgetCoversEveryConnectionAndREST(t *testing.T) {
	e := newRoomEnv(t, collab.Options{UpdatesPerSecond: 1000})
	e.rt.container.DocumentCollab.WithUpdateLimit(3, time.Minute)
	first := e.dial(t, e.member, nil)
	first.expect(t, "ready")
	second := e.dial(t, e.member, nil)
	second.expect(t, "ready")
	first.expect(t, "peer_joined")

	first.send(t, map[string]any{"type": "update", "update": upd("1"), "ref": "1"})
	first.expect(t, "ack")
	second.expect(t, "update")
	second.send(t, map[string]any{"type": "update", "update": upd("2"), "ref": "2"})
	second.expect(t, "ack")
	first.expect(t, "update")
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/updates", e.member, map[string]any{"update": upd("3")}, http.StatusCreated)
	first.expect(t, "update")
	second.expect(t, "update")

	// The budget of three is spent: the socket is told how long to wait, REST answers 429.
	second.send(t, map[string]any{"type": "update", "update": upd("4"), "ref": "4"})
	got := second.expect(t, "error")
	if got["code"] != "RATE_LIMITED" || got["ref"] != "4" {
		t.Fatalf("over the budget: %v", got)
	}
	if wait, _ := got["retry_after"].(float64); wait < 1 || wait > 60 {
		t.Fatalf("retry_after must say how long to wait: %v", got)
	}
	out := e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/updates", e.member, map[string]any{"update": upd("5")}, http.StatusTooManyRequests)
	if out["error"].(map[string]any)["code"] != "RATE_LIMIT_EXCEEDED" {
		t.Fatalf("REST over the budget: %v", out)
	}
	// Another person has their own budget.
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/updates", e.owner.token, map[string]any{"update": upd("6")}, http.StatusCreated)
}

func TestDocumentRoom_ServerCompactsALongHistory(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	e.rt.container.Documents.WithCompactAfter(4)
	updates := "/api/v1/documents/" + itoa(e.docID) + "/updates"
	state := "/api/v1/documents/" + itoa(e.docID) + "/state"
	before := e.rt.container.Documents.Compactions()

	// A client that is connected when the history is replaced is told to reload.
	watcher := e.dial(t, e.guest, nil)
	watcher.expect(t, "ready")

	for i := 0; i < 7; i++ {
		e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": upd("edit-" + itoa(uint(i)))}, http.StatusCreated)
	}
	deadline := time.Now().Add(10 * time.Second)
	for e.rt.container.Documents.Compactions() == before {
		if time.Now().After(deadline) {
			t.Fatal("the history was not compacted")
		}
		time.Sleep(20 * time.Millisecond)
	}

	got := e.call(t, http.MethodGet, state, e.guest, nil, http.StatusOK)["data"].(map[string]any)
	if got["snapshot_seq"].(float64) < 4 || got["last_seq"].(float64) != 7 {
		t.Fatalf("state after compaction: snapshot_seq=%v last_seq=%v", got["snapshot_seq"], got["last_seq"])
	}
	if remaining := len(got["updates"].([]any)); remaining >= 7 {
		t.Fatalf("%d updates still stored after compaction", remaining)
	}

	// The state a newcomer builds from the snapshot and the updates after it is the same document: every
	// edit made before the compaction is in it, and the numbering goes on.
	raw, err := base64.StdEncoding.DecodeString(got["snapshot"].(string))
	if err != nil {
		t.Fatal(err)
	}
	doc := crdt.New()
	if err := crdt.ApplyUpdateV1(doc, raw, nil); err != nil {
		t.Fatalf("the snapshot does not decode: %v", err)
	}
	for _, change := range got["updates"].([]any) {
		raw, _ := base64.StdEncoding.DecodeString(change.(map[string]any)["update"].(string))
		if err := crdt.ApplyUpdateV1(doc, raw, nil); err != nil {
			t.Fatal(err)
		}
	}
	scratch := doc.GetText("test-scratch").ToString()
	for i := 0; i < 7; i++ {
		if !strings.Contains(scratch, "edit-"+itoa(uint(i))) {
			t.Fatalf("edit-%d is missing after compaction: %q", i, scratch)
		}
	}
	next := e.call(t, http.MethodPost, updates, e.member, map[string]any{"update": upd("after")}, http.StatusCreated)
	if next["data"].(map[string]any)["seq"].(float64) != 8 {
		t.Fatalf("numbering after compaction: %v", next)
	}

	// Somebody who last saw update 1 cannot be caught up from the updates any more: the server says so.
	since := int64(1)
	late := e.dial(t, e.member, &since)
	late.expect(t, "ready")
	if frame := late.expect(t, "resync"); frame["reason"] != "snapshot" {
		t.Fatalf("resync: %v", frame)
	}
}
