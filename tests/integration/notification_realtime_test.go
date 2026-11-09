package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"rabbit-hole-server/internal/realtime"
)

const (
	notificationTicketPath = "/api/v1/notifications/events/ticket"
	notificationEventsPath = "/api/v1/notifications/events"
)

type notifyLive struct {
	env *rtEnv
	f   *notifyFixture
}

func newNotifyLive(t *testing.T) *notifyLive {
	t.Helper()
	env := newRealtimeEnv(t, realtime.Options{})
	tc := &TestContext{DB: env.tc.DB, Router: env.router, Mail: env.tc.Mail, Notifications: env.container.Notifications}
	return &notifyLive{env: env, f: newNotifyFixture(t, tc)}
}

func (l *notifyLive) ticket(t *testing.T, token string) string {
	t.Helper()
	rec := l.env.req(t, http.MethodPost, notificationTicketPath, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("notification ticket: %d: %s", rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)["ticket"].(string)
}

func (l *notifyLive) dial(ticket, origin string) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := l.env.server.URL + notificationEventsPath
	if ticket != "" {
		url += "?ticket=" + ticket
	}
	opts := &websocket.DialOptions{}
	if origin != "" {
		opts.HTTPHeader = http.Header{"Origin": []string{origin}}
	}
	return websocket.Dial(ctx, url, opts)
}

func (l *notifyLive) connect(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	conn, resp, err := l.dial(l.ticket(t, token), "")
	if err != nil {
		status := ""
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("connect to the notification feed: %v %s", err, status)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	ev := readEvent(t, conn)
	if ev["type"] != "ready" || ev["topic"] == nil {
		t.Fatalf("first frame = %v, want ready", ev)
	}
	return conn
}

func (l *notifyLive) mention(t *testing.T, mentioned uint, body string) {
	t.Helper()
	l.f.comment(t, l.f.owner.token, l.f.taskID, body, mentioned)
}

func (l *notifyLive) post(t *testing.T, token string, body map[string]any) {
	t.Helper()
	rec := l.env.req(t, http.MethodPost, "/api/v1/notifications/read", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("mark read: %d: %s", rec.Code, rec.Body.String())
	}
}

func expectPair(t *testing.T, conn *websocket.Conn, primary string, unread float64) map[string]any {
	t.Helper()
	ev := readEvent(t, conn)
	if ev["type"] != primary || ev["unread_count"] != unread {
		t.Fatalf("event = %v, want %s with unread_count %v", ev, primary, unread)
	}
	count := readEvent(t, conn)
	if count["type"] != "notification.unread_count" || count["unread_count"] != unread {
		t.Fatalf("count event = %v, want unread_count %v", count, unread)
	}
	if _, ok := count["by_type"].(map[string]any); !ok {
		t.Fatalf("count event has no by_type: %v", count)
	}
	return ev
}

func expectPairBetween(t *testing.T, conn *websocket.Conn, primary string, low, high float64) float64 {
	t.Helper()
	ev := readEvent(t, conn)
	got, _ := ev["unread_count"].(float64)
	if ev["type"] != primary || got < low || got > high {
		t.Fatalf("event = %v, want %s with unread_count between %v and %v", ev, primary, low, high)
	}
	count := readEvent(t, conn)
	if count["type"] != "notification.unread_count" || count["unread_count"] != ev["unread_count"] {
		t.Fatalf("count event = %v, want unread_count %v", count, ev["unread_count"])
	}
	return got
}

func TestNotificationRealtime_MentionIsPushedWithACompactPayload(t *testing.T) {
	l := newNotifyLive(t)
	conn := l.connect(t, l.f.tokenA)

	l.mention(t, l.f.memberA, "please look here")
	ev := expectPair(t, conn, "notification.created", 1)

	n, _ := ev["notification"].(map[string]any)
	entries := inbox(t, l.f.tc, l.f.tokenA, "")
	if len(entries) != 1 {
		t.Fatalf("inbox = %v", entries)
	}
	if n["id"] != float64(entries[0].ID) || n["type"] != "mention" || n["task_id"] != float64(l.f.taskID) ||
		n["title"] != "shared task" || n["snippet"] != "please look here" || n["created_at"] == nil {
		t.Fatalf("notification = %v", n)
	}
	for _, leaked := range []string{"actor", "payload", "email", "workspace_id"} {
		if _, ok := n[leaked]; ok {
			t.Fatalf("compact payload carries %q: %v", leaked, n)
		}
	}

	l.mention(t, l.f.memberA, "once more")
	expectPair(t, conn, "notification.created", 2)
}

func TestNotificationRealtime_ReadAndDeleteKeepAllTabsInSync(t *testing.T) {
	l := newNotifyLive(t)
	tabOne := l.connect(t, l.f.tokenA)
	tabTwo := l.connect(t, l.f.tokenA)

	l.mention(t, l.f.memberA, "first")
	l.mention(t, l.f.memberA, "second")
	l.mention(t, l.f.memberA, "third")
	for _, tab := range []*websocket.Conn{tabOne, tabTwo} {
		first := expectPairBetween(t, tab, "notification.created", 1, 3)
		second := expectPairBetween(t, tab, "notification.created", first, 3)
		expectPairBetween(t, tab, "notification.created", second, 3)
	}
	entries := inbox(t, l.f.tc, l.f.tokenA, "")

	l.post(t, l.f.tokenA, map[string]any{"ids": []uint{entries[0].ID}})
	for _, tab := range []*websocket.Conn{tabOne, tabTwo} {
		ev := expectPair(t, tab, "notification.read", 2)
		ids, _ := ev["ids"].([]any)
		if len(ids) != 1 || ids[0] != float64(entries[0].ID) || ev["updated"] != float64(1) {
			t.Fatalf("read event = %v", ev)
		}
	}

	rec := l.env.req(t, http.MethodDelete, "/api/v1/notifications/"+itoa(entries[1].ID), l.f.tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d: %s", rec.Code, rec.Body.String())
	}
	for _, tab := range []*websocket.Conn{tabOne, tabTwo} {
		ev := expectPair(t, tab, "notification.deleted", 1)
		if ev["notification_id"] != float64(entries[1].ID) {
			t.Fatalf("deleted event = %v", ev)
		}
	}

	l.post(t, l.f.tokenA, map[string]any{"all": true})
	for _, tab := range []*websocket.Conn{tabOne, tabTwo} {
		ev := expectPair(t, tab, "notification.read", 0)
		if ev["all"] != true || ev["updated"] != float64(1) {
			t.Fatalf("read-all event = %v", ev)
		}
		if _, listed := ev["ids"]; listed {
			t.Fatalf("read-all event lists ids: %v", ev)
		}
	}

	l.post(t, l.f.tokenA, map[string]any{"all": true})
	expectNoEvent(t, tabOne)
	expectNoEvent(t, tabTwo)

	rec = l.env.req(t, http.MethodGet, "/api/v1/notifications/unread-count", l.f.tokenA, nil)
	if dataObject(t, rec)["total"] != float64(0) {
		t.Fatalf("unread count = %s", rec.Body.String())
	}
}

func TestNotificationRealtime_UsersOnlyReceiveTheirOwnEvents(t *testing.T) {
	l := newNotifyLive(t)
	a := l.connect(t, l.f.tokenA)
	b := l.connect(t, l.f.tokenB)
	outsider := l.connect(t, l.f.tokenOutsider)
	owner := l.connect(t, l.f.owner.token)

	l.mention(t, l.f.memberA, "for A only")
	expectPair(t, a, "notification.created", 1)

	entries := inbox(t, l.f.tc, l.f.tokenA, "")
	rec := l.env.req(t, http.MethodDelete, "/api/v1/notifications/"+itoa(entries[0].ID), l.f.tokenB, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign delete: %d", rec.Code)
	}
	l.post(t, l.f.tokenB, map[string]any{"ids": []uint{entries[0].ID}})

	l.post(t, l.f.tokenA, map[string]any{"all": true})
	expectPair(t, a, "notification.read", 0)

	expectNoEvent(t, b)
	expectNoEvent(t, outsider)
	expectNoEvent(t, owner)
}

func TestNotificationRealtime_WatchersGetCommentsAndTypesAreReported(t *testing.T) {
	l := newNotifyLive(t)
	l.f.watch(t, l.f.tokenB, http.MethodPut)
	b := l.connect(t, l.f.tokenB)

	l.f.comment(t, l.f.owner.token, l.f.taskID, "to watchers")
	ev := expectPair(t, b, "notification.created", 1)
	if n := ev["notification"].(map[string]any); n["type"] != "comment" {
		t.Fatalf("notification = %v", n)
	}
}

func TestNotificationRealtime_NothingIsPushedAboutTasksTheUserCannotRead(t *testing.T) {
	l := newNotifyLive(t)
	l.f.watch(t, l.f.tokenA, http.MethodPut)
	l.f.watch(t, l.f.tokenB, http.MethodPut)
	b := l.connect(t, l.f.tokenB)
	a := l.connect(t, l.f.tokenA)

	rec := l.env.req(t, http.MethodDelete, membersPath(l.f.owner.workspaceID, "/"+itoa(l.f.memberB)), l.f.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove member: %d: %s", rec.Code, rec.Body.String())
	}
	l.f.comment(t, l.f.owner.token, l.f.taskID, "b is gone")
	expectNoEvent(t, b)
	expectPair(t, a, "notification.created", 1)

	if rec := l.env.req(t, http.MethodDelete, "/api/v1/tasks/"+itoa(l.f.taskID), l.f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete task: %d: %s", rec.Code, rec.Body.String())
	}
	l.post(t, l.f.tokenA, map[string]any{"all": true})
	expectNoEvent(t, a)
}

func TestNotificationRealtime_TicketRules(t *testing.T) {
	l := newNotifyLive(t)
	boards := l.env.newBoard(t, "rt-ntf-board@example.test")

	t.Run("a ticket needs a signed-in user", func(t *testing.T) {
		if rec := l.env.req(t, http.MethodPost, notificationTicketPath, "", nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", rec.Code)
		}
		if rec := l.env.req(t, http.MethodPost, notificationTicketPath, "not-a-token", nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", rec.Code)
		}
	})

	t.Run("missing, garbage and forged tickets are refused", func(t *testing.T) {
		for _, ticket := range []string{"", "nonsense", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "user:" + itoa(l.f.memberA)} {
			if status := handshakeStatusOf(l.dial(ticket, "")); status != http.StatusUnauthorized {
				t.Errorf("ticket %q: status %d, want 401", ticket, status)
			}
		}
	})

	t.Run("a ticket works once", func(t *testing.T) {
		ticket := l.ticket(t, l.f.tokenA)
		conn, resp, err := l.dial(ticket, "")
		if err != nil {
			t.Fatalf("first use: %v (%v)", err, resp)
		}
		_ = conn.CloseNow()
		if status := handshakeStatusOf(l.dial(ticket, "")); status != http.StatusUnauthorized {
			t.Fatalf("reused ticket: status %d, want 401", status)
		}
	})

	t.Run("tickets are not interchangeable with list or space feeds", func(t *testing.T) {
		ticket := l.ticket(t, boards.token)
		if status := handshakeStatusOf(l.env.dial("lists", boards.listID, ticket, "")); status != http.StatusUnauthorized {
			t.Errorf("notification ticket on a list: status %d, want 401", status)
		}
		ticket = l.env.ticket(t, boards.token, "lists", boards.listID)
		if status := handshakeStatusOf(l.dial(ticket, "")); status != http.StatusUnauthorized {
			t.Errorf("list ticket on the notification feed: status %d, want 401", status)
		}
		ticket = l.env.ticket(t, boards.token, "spaces", boards.spaceID)
		if status := handshakeStatusOf(l.dial(ticket, "")); status != http.StatusUnauthorized {
			t.Errorf("space ticket on the notification feed: status %d, want 401", status)
		}
	})

	t.Run("origin checks apply", func(t *testing.T) {
		ticket := l.ticket(t, l.f.tokenA)
		if status := handshakeStatusOf(l.dial(ticket, "https://evil.example")); status != http.StatusForbidden {
			t.Fatalf("foreign origin: status %d, want 403", status)
		}
		ticket = l.ticket(t, l.f.tokenA)
		conn, resp, err := l.dial(ticket, integrationConfig.AllowedOrigins[0])
		if err != nil {
			t.Fatalf("configured origin rejected: %v (%v)", err, resp)
		}
		_ = conn.CloseNow()
	})

	t.Run("a ticket belongs to the user who requested it", func(t *testing.T) {
		ticket := l.ticket(t, l.f.tokenA)
		conn, resp, err := l.dial(ticket, "")
		if err != nil {
			t.Fatalf("connect: %v (%v)", err, resp)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		if ev := readEvent(t, conn); ev["topic"] != "user:"+itoa(l.f.memberA) {
			t.Fatalf("ready = %v", ev)
		}
	})
}

func TestNotificationRealtime_ConnectionLimitPerUserIsEnforced(t *testing.T) {
	l := newNotifyLive(t)
	var conns []*websocket.Conn
	for i := 0; i < realtime.DefaultMaxConnectionsPerUser; i++ {
		conns = append(conns, l.connect(t, l.f.tokenA))
	}
	if status := handshakeStatusOf(l.dial(l.ticket(t, l.f.tokenA), "")); status != http.StatusTooManyRequests {
		t.Fatalf("over the limit: status %d, want 429", status)
	}
	if status := handshakeStatusOf(l.dial(l.ticket(t, l.f.tokenB), "")); status != 101 {
		t.Fatalf("another user is affected by the limit: status %d", status)
	}

	_ = conns[0].CloseNow()
	deadline := time.Now().Add(3 * time.Second)
	for {
		status := handshakeStatusOf(l.dial(l.ticket(t, l.f.tokenA), ""))
		if status == 101 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a closed connection never freed its slot, last status %d", status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
