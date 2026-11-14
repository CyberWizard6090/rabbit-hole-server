package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

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

	member.send(t, map[string]any{"type": "update", "update": b64("hello"), "touched": []string{"block-1"}, "ref": "r1"})
	ack := member.expect(t, "ack")
	if ack["seq"].(float64) != 1 || ack["ref"] != "r1" {
		t.Fatalf("ack: %v", ack)
	}
	update := owner.expect(t, "update")
	if update["seq"].(float64) != 1 || uint(update["user_id"].(float64)) != e.memberID || update["update"] != b64("hello") {
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

	guest.send(t, map[string]any{"type": "update", "update": b64("nope"), "ref": "g1"})
	if refused := guest.expect(t, "error"); refused["code"] != "FORBIDDEN" || refused["ref"] != "g1" {
		t.Fatalf("a viewer's update must be refused: %v", refused)
	}
	// Frames are read in order, so a stray frame would show up as the next expectation.

	member.send(t, map[string]any{"type": "update", "update": b64("by member")})
	member.expect(t, "ack")
	if got := guest.expect(t, "update"); got["update"] != b64("by member") {
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

	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/updates", e.member, map[string]any{"update": b64("rest")}, http.StatusCreated)
	got := viewer.expect(t, "update")
	if got["update"] != b64("rest") || got["seq"].(float64) != 1 || uint(got["user_id"].(float64)) != e.memberID {
		t.Fatalf("an update posted over REST must reach the room: %v", got)
	}
}

func TestDocumentRoom_CatchUpAfterConnecting(t *testing.T) {
	e := newRoomEnv(t, collab.Options{})
	post := func(text string) {
		e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/updates", e.member, map[string]any{"update": b64(text)}, http.StatusCreated)
	}
	for _, text := range []string{"a", "b", "c"} {
		post(text)
	}

	since := int64(1)
	client := e.dial(t, e.guest, &since)
	client.expect(t, "ready")
	first, second := client.expect(t, "update"), client.expect(t, "update")
	if first["seq"].(float64) != 2 || first["update"] != b64("b") || second["seq"].(float64) != 3 || second["update"] != b64("c") {
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
	e.call(t, http.MethodPost, "/api/v1/documents/"+itoa(e.docID)+"/snapshot", e.member, map[string]any{"upto_seq": 2, "state": b64("merged")}, http.StatusOK)
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
		client.send(t, map[string]any{"type": "update", "update": b64("u"), "ref": fmt.Sprint(i)})
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

func TestDocumentRoom_RevokedAccessClosesTheConnection(t *testing.T) {
	e := newRoomEnv(t, collab.Options{RecheckInterval: 100 * time.Millisecond})
	guest := e.dial(t, e.guest, nil)
	guest.expect(t, "ready")

	guestID := uint(0)
	if err := e.tc.DB.Raw("SELECT id FROM users WHERE email = ?", "nb-guest@example.test").Scan(&guestID).Error; err != nil || guestID == 0 {
		t.Fatalf("find the guest: %v", err)
	}
	e.call(t, http.MethodDelete, membersPath(e.owner.workspaceID, "/"+itoa(guestID)), e.owner.token, nil, http.StatusOK)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := guest.conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("a removed member must be disconnected with a policy violation, got %v", err)
	}
}
