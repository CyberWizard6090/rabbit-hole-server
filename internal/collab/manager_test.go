package collab

import (
	"encoding/json"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
)

func user(id uint, name string) domain.CommentUser {
	return domain.CommentUser{ID: id, Username: name}
}

// next returns the next queued frame as a map, failing when none is queued.
func next(t *testing.T, p *Peer) map[string]any {
	t.Helper()

	select {
	case raw := <-p.Frames():
		var frame map[string]any

		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatal(err)
		}

		return frame
	case <-time.After(time.Second):
		t.Fatal("no frame queued")
	}

	return nil
}

func none(t *testing.T, p *Peer) {
	t.Helper()

	select {
	case raw := <-p.Frames():
		t.Fatalf("unexpected frame: %s", raw)
	case <-time.After(20 * time.Millisecond):
	}
}

func join(t *testing.T, m *Manager, doc uint, u domain.CommentUser, write bool) *Peer {
	t.Helper()

	p, err := m.Join(doc, u, write)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestJoinStartsWithReadyAndListsPeopleAlreadyPresent(t *testing.T) {
	m := NewManager(Limits{})
	a := join(t, m, 1, user(1, "ann"), true)

	ready := next(t, a)
	if ready["type"] != TypeReady || ready["can_write"] != true || len(ready["peers"].([]any)) != 0 {
		t.Fatalf("first peer: %v", ready)
	}

	b := join(t, m, 1, user(2, "bob"), false)
	peers := next(t, b)["peers"].([]any)

	if len(peers) != 1 || peers[0].(map[string]any)["user"].(map[string]any)["username"] != "ann" {
		t.Fatalf("bob should see ann: %v", peers)
	}

	joined := next(t, a)
	if joined["type"] != TypePeerJoined || joined["can_write"] != false || joined["user"].(map[string]any)["username"] != "bob" {
		t.Fatalf("ann should be told about bob: %v", joined)
	}
}

func TestRoomsAreIsolated(t *testing.T) {
	m := NewManager(Limits{})
	a := join(t, m, 1, user(1, "ann"), true)
	b := join(t, m, 2, user(2, "bob"), true)
	next(t, a)
	next(t, b)

	m.PublishUpdate(1, UpdateEvent{Seq: 1, UserID: 1, Update: []byte("x")})
	none(t, b)

	if frame := next(t, a); frame["type"] != TypeUpdate || frame["seq"].(float64) != 1 {
		t.Fatalf("update: %v", frame)
	}
}

func TestPublishSkipsTheOrigin(t *testing.T) {
	m := NewManager(Limits{})
	a := join(t, m, 1, user(1, "ann"), true)
	b := join(t, m, 1, user(2, "bob"), true)

	for _, p := range []*Peer{a, b} {
		next(t, p)
	}

	next(t, a) // bob joined

	m.PublishUpdate(1, UpdateEvent{Seq: 5, UserID: 1, Update: []byte("hello"), Origin: a.ID})
	none(t, a)

	frame := next(t, b)
	if frame["type"] != TypeUpdate || frame["seq"].(float64) != 5 || frame["user_id"].(float64) != 1 {
		t.Fatalf("update: %v", frame)
	}

	if raw, _ := json.Marshal([]byte("hello")); string(raw) != `"`+frame["update"].(string)+`"` {
		t.Fatalf("update bytes should be base64: %v", frame["update"])
	}
}

func TestSlowClientIsDisconnectedWithoutBlockingOthers(t *testing.T) {
	m := NewManager(Limits{SendBuffer: 2})
	slow := join(t, m, 1, user(1, "slow"), true)
	fast := join(t, m, 1, user(2, "fast"), true)
	next(t, fast)

	for seq := int64(1); seq <= 5; seq++ {
		m.PublishUpdate(1, UpdateEvent{Seq: seq, Update: []byte("u")})
		next(t, fast)
	}

	select {
	case <-slow.Done():
		if slow.Reason() != ReasonSlowClient {
			t.Fatalf("reason = %q", slow.Reason())
		}
	default:
		t.Fatal("a client that does not read must be disconnected")
	}
}

func TestLeaveTellsOthersAndFreesTheRoom(t *testing.T) {
	m := NewManager(Limits{})
	a := join(t, m, 1, user(1, "ann"), true)
	b := join(t, m, 1, user(2, "bob"), true)
	next(t, a)
	next(t, b)
	next(t, a)

	m.Leave(b)
	m.Leave(b)

	if frame := next(t, a); frame["type"] != TypePeerLeft || uint64(frame["conn"].(float64)) != b.ID {
		t.Fatalf("peer_left: %v", frame)
	}

	none(t, a)
	m.Leave(a)

	if m.Rooms() != 0 || m.Connections() != 0 {
		t.Fatalf("rooms=%d connections=%d", m.Rooms(), m.Connections())
	}
}

func TestPresenceCountsPeopleNotTabs(t *testing.T) {
	m := NewManager(Limits{})
	join(t, m, 1, user(2, "bob"), false)
	join(t, m, 1, user(1, "ann"), false)
	second := join(t, m, 1, user(1, "ann"), true)

	presence := m.Presence(1)
	if len(presence) != 2 || presence[0].User.ID != 1 || presence[0].Connections != 2 || !presence[0].CanWrite {
		t.Fatalf("presence: %+v", presence)
	}

	if presence[1].User.ID != 2 || presence[1].Connections != 1 || presence[1].CanWrite {
		t.Fatalf("presence: %+v", presence)
	}

	second.SetCanWrite(false)

	if m.Presence(1)[0].CanWrite {
		t.Fatal("losing write access must show in presence")
	}

	if len(m.Presence(99)) != 0 {
		t.Fatal("an empty room has no presence")
	}
}

func TestAwarenessIsCoalescedAndStampedWithTheRealUser(t *testing.T) {
	m := NewManager(Limits{AwarenessEvery: 30 * time.Millisecond})
	a := join(t, m, 1, user(1, "ann"), true)
	b := join(t, m, 1, user(2, "bob"), true)
	next(t, a)
	next(t, b)
	next(t, a)

	for _, state := range []string{`{"c":1}`, `{"c":2}`, `{"c":3}`} {
		m.SetAwareness(a, json.RawMessage(state))
	}

	frame := next(t, b)
	if frame["type"] != TypeAwareness || frame["user"].(map[string]any)["username"] != "ann" || frame["state"].(map[string]any)["c"].(float64) != 3 {
		t.Fatalf("only the last state should go out: %v", frame)
	}

	none(t, b)
	none(t, a)

	// A late joiner sees the current state of everyone.
	c := join(t, m, 1, user(3, "cy"), false)
	peers := next(t, c)["peers"].([]any)

	var seen bool

	for _, item := range peers {
		entry := item.(map[string]any)
		if entry["user"].(map[string]any)["username"] == "ann" && entry["state"].(map[string]any)["c"].(float64) == 3 {
			seen = true
		}
	}

	if !seen {
		t.Fatalf("ready should carry ann's state: %v", peers)
	}

	m.SetAwareness(a, json.RawMessage("null"))

	if frame := next(t, b); frame["state"] != nil {
		t.Fatalf("null clears the state: %v", frame)
	}
}

func TestLimits(t *testing.T) {
	m := NewManager(Limits{PeersPerRoom: 3, PeersPerUser: 2})
	join(t, m, 1, user(1, "ann"), true)
	join(t, m, 1, user(1, "ann"), true)

	if _, err := m.Join(2, user(1, "ann"), true); err != ErrTooManyConnections {
		t.Fatalf("per-user limit: %v", err)
	}

	join(t, m, 1, user(2, "bob"), true)

	if _, err := m.Join(1, user(3, "cy"), true); err != ErrRoomFull {
		t.Fatalf("room limit: %v", err)
	}
}

func TestKickAndClose(t *testing.T) {
	m := NewManager(Limits{})
	a := join(t, m, 1, user(1, "ann"), true)
	b := join(t, m, 1, user(2, "bob"), true)

	if m.Kick(1, 1, ReasonAccessRevoked) != 1 {
		t.Fatal("ann has one connection")
	}

	<-a.Done()

	if a.Reason() != ReasonAccessRevoked {
		t.Fatalf("reason = %q", a.Reason())
	}

	m.Close()
	<-b.Done()

	if b.Reason() != ReasonShuttingDown {
		t.Fatalf("reason = %q", b.Reason())
	}

	if _, err := m.Join(1, user(3, "cy"), true); err != ErrManagerClosed {
		t.Fatalf("join after close: %v", err)
	}
}

func TestOriginTravelsInTheContext(t *testing.T) {
	if OriginFrom(WithOrigin(t.Context(), 7)) != 7 || OriginFrom(t.Context()) != 0 {
		t.Fatal("origin should round-trip and default to zero")
	}
}

func TestBucketLimitsBursts(t *testing.T) {
	b := newBucket(5)
	allowed := 0

	for range 100 {
		if b.allow() {
			allowed++
		}
	}

	if allowed < 10 || allowed > 12 {
		t.Fatalf("a burst of 2x the rate should pass, got %d", allowed)
	}
}
