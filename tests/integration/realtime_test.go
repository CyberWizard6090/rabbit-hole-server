package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/realtime"
)

type rtEnv struct {
	tc        *TestContext
	container *app.Container
	router    http.Handler
	server    *httptest.Server
}

func newRealtimeEnv(t *testing.T, opts realtime.Options) *rtEnv {
	t.Helper()
	tc := newTestContext(t)
	container, err := app.NewContainer(integrationDB, integrationConfig)
	if err != nil {
		t.Fatal(err)
	}
	container.RealtimeHandler.WithOptions(opts)
	router := app.SetupRouter(container, integrationConfig)
	server := httptest.NewServer(router)
	t.Cleanup(func() {
		container.Realtime.Close()
		server.Close()
		container.Notifications.Close()
		if container.CloseMail != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := container.CloseMail(ctx); err != nil {
				t.Errorf("close integration mail sender: %v", err)
			}
		}
	})
	return &rtEnv{tc: tc, container: container, router: router, server: server}
}

func (e *rtEnv) req(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, e.router, method, path, token, body)
}

type board struct {
	token                                string
	userID, workspaceID, spaceID, listID uint
	statusID                             uint
}

func (e *rtEnv) newBoard(t *testing.T, email string) board {
	t.Helper()
	userID, token := newUser(t, e.tc, email)
	b := board{token: token, userID: userID}
	create := func(path string, body map[string]any) uint {
		rec := e.req(t, http.MethodPost, path, token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST %s: %d: %s", path, rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}
	b.workspaceID = create("/api/v1/workspaces", map[string]any{"name": "rt workspace"})
	b.spaceID = create("/api/v1/workspaces/"+itoa(b.workspaceID)+"/spaces", map[string]any{"name": "rt space"})
	b.listID = create("/api/v1/spaces/"+itoa(b.spaceID)+"/lists", map[string]any{"name": "rt list"})
	b.statusID = create("/api/v1/lists/"+itoa(b.listID)+"/statuses", map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": b.listID})
	return b
}

func (e *rtEnv) createTask(t *testing.T, b board, listID, statusID uint, title string) uint {
	t.Helper()
	rec := e.req(t, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", b.token, map[string]any{"title": title, "status_id": statusID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func (e *rtEnv) ticket(t *testing.T, token, scope string, id uint) string {
	t.Helper()
	rec := e.req(t, http.MethodPost, fmt.Sprintf("/api/v1/%s/%d/events/ticket", scope, id), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket for %s/%d: %d: %s", scope, id, rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)["ticket"].(string)
}

func (e *rtEnv) dial(scope string, id uint, ticket string, origin string) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/api/v1/%s/%d/events", e.server.URL, scope, id)
	if ticket != "" {
		url += "?ticket=" + ticket
	}
	opts := &websocket.DialOptions{}
	if origin != "" {
		opts.HTTPHeader = http.Header{"Origin": []string{origin}}
	}
	return websocket.Dial(ctx, url, opts)
}

func (e *rtEnv) connect(t *testing.T, token, scope string, id uint) *websocket.Conn {
	t.Helper()
	conn, resp, err := e.dial(scope, id, e.ticket(t, token, scope, id), "")
	if err != nil {
		body := ""
		if resp != nil {
			body = resp.Status
		}
		t.Fatalf("connect %s/%d: %v %s", scope, id, err, body)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	if ev := readEvent(t, conn); ev["type"] != "ready" {
		t.Fatalf("first frame = %v, want ready", ev)
	}
	return conn
}

func readEvent(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("event is not JSON: %v: %s", err, data)
	}
	return ev
}

func expectNoEvent(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if _, data, err := conn.Read(ctx); err == nil {
		t.Fatalf("received an event that should not have been delivered: %s", data)
	}
}

func TestRealtime_ListFollowerSeesTheTaskLifecycle(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	b := e.newBoard(t, "rt-owner@example.test")
	conn := e.connect(t, b.token, "lists", b.listID)

	taskID := e.createTask(t, b, b.listID, b.statusID, "first task")
	ev := readEvent(t, conn)
	task, _ := ev["task"].(map[string]any)
	if ev["type"] != "task.created" || ev["task_id"] != float64(taskID) || ev["list_id"] != float64(b.listID) ||
		ev["space_id"] != float64(b.spaceID) || ev["actor_id"] != float64(b.userID) || task["title"] != "first task" {
		t.Fatalf("created event = %v", ev)
	}

	rec := e.req(t, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), b.token, map[string]any{"title": "renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d: %s", rec.Code, rec.Body.String())
	}
	ev = readEvent(t, conn)
	task, _ = ev["task"].(map[string]any)
	if ev["type"] != "task.updated" || task["title"] != "renamed" || ev["task_id"] != float64(taskID) {
		t.Fatalf("updated event = %v", ev)
	}

	if rec := e.req(t, http.MethodDelete, "/api/v1/tasks/"+itoa(taskID), b.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d: %s", rec.Code, rec.Body.String())
	}
	ev = readEvent(t, conn)
	if ev["type"] != "task.deleted" || ev["task_id"] != float64(taskID) || ev["list_id"] != float64(b.listID) {
		t.Fatalf("deleted event = %v", ev)
	}
	if _, hasTask := ev["task"]; hasTask {
		t.Fatalf("deleted event must not carry the task: %v", ev)
	}
}

func TestRealtime_RejectedChangesProduceNoEvent(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	b := e.newBoard(t, "rt-rejected@example.test")
	conn := e.connect(t, b.token, "lists", b.listID)

	rec := e.req(t, http.MethodPost, "/api/v1/lists/"+itoa(b.listID)+"/tasks", b.token, map[string]any{"title": "bad ref", "status_id": 99999})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected the invalid status to be rejected, got %d", rec.Code)
	}
	expectNoEvent(t, conn)
}

func TestRealtime_SpaceFollowerSeesEveryListOfTheSpace(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	b := e.newBoard(t, "rt-space@example.test")
	rec := e.req(t, http.MethodPost, "/api/v1/spaces/"+itoa(b.spaceID)+"/lists", b.token, map[string]any{"name": "second list"})
	secondList := responseID(t, rec)
	rec = e.req(t, http.MethodPost, "/api/v1/lists/"+itoa(secondList)+"/statuses", b.token, map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": secondList})
	secondStatus := responseID(t, rec)

	space := e.connect(t, b.token, "spaces", b.spaceID)
	list := e.connect(t, b.token, "lists", b.listID)

	e.createTask(t, b, b.listID, b.statusID, "in first list")
	e.createTask(t, b, secondList, secondStatus, "in second list")

	if ev := readEvent(t, space); ev["list_id"] != float64(b.listID) {
		t.Fatalf("space follower, first event = %v", ev)
	}
	if ev := readEvent(t, space); ev["list_id"] != float64(secondList) {
		t.Fatalf("space follower, second event = %v", ev)
	}
	if ev := readEvent(t, list); ev["list_id"] != float64(b.listID) {
		t.Fatalf("list follower = %v", ev)
	}
	expectNoEvent(t, list)
}

func TestRealtime_UsersNeverSeeEachOthersEvents(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	alice := e.newBoard(t, "rt-alice@example.test")
	bob := e.newBoard(t, "rt-bob@example.test")
	aliceConn := e.connect(t, alice.token, "lists", alice.listID)
	bobConn := e.connect(t, bob.token, "lists", bob.listID)
	bobSpace := e.connect(t, bob.token, "spaces", bob.spaceID)

	e.createTask(t, alice, alice.listID, alice.statusID, "alice secret")

	if ev := readEvent(t, aliceConn); ev["type"] != "task.created" {
		t.Fatalf("alice = %v", ev)
	}
	expectNoEvent(t, bobConn)
	expectNoEvent(t, bobSpace)
}

func TestRealtime_TicketRules(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	alice := e.newBoard(t, "rt-ticket-alice@example.test")
	bob := e.newBoard(t, "rt-ticket-bob@example.test")

	t.Run("a ticket works once", func(t *testing.T) {
		ticket := e.ticket(t, alice.token, "lists", alice.listID)
		conn, resp, err := e.dial("lists", alice.listID, ticket, "")
		if err != nil {
			t.Fatalf("first use: %v (%v)", err, resp)
		}
		_ = conn.CloseNow()
		if status := handshakeStatusOf(e.dial("lists", alice.listID, ticket, "")); status != http.StatusUnauthorized {
			t.Fatalf("reused ticket: status %d, want 401", status)
		}
	})

	t.Run("no ticket or garbage", func(t *testing.T) {
		for _, ticket := range []string{"", "nonsense", strings.Repeat("A", 43)} {
			if status := handshakeStatusOf(e.dial("lists", alice.listID, ticket, "")); status != http.StatusUnauthorized {
				t.Errorf("ticket %q: status %d, want 401", ticket, status)
			}
		}
	})

	t.Run("a ticket is bound to its topic", func(t *testing.T) {
		second := responseID(t, e.req(t, http.MethodPost, "/api/v1/spaces/"+itoa(alice.spaceID)+"/lists", alice.token, map[string]any{"name": "other list"}))
		ticket := e.ticket(t, alice.token, "lists", alice.listID)
		if status := handshakeStatusOf(e.dial("lists", second, ticket, "")); status != http.StatusUnauthorized {
			t.Errorf("list ticket on another list: status %d, want 401", status)
		}
		ticket = e.ticket(t, alice.token, "lists", alice.listID)
		if status := handshakeStatusOf(e.dial("spaces", alice.spaceID, ticket, "")); status != http.StatusUnauthorized {
			t.Errorf("list ticket on the space feed: status %d, want 401", status)
		}
	})

	t.Run("a stranger cannot get a ticket for someone else's list", func(t *testing.T) {
		rec := e.req(t, http.MethodPost, fmt.Sprintf("/api/v1/lists/%d/events/ticket", alice.listID), bob.token, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
		rec = e.req(t, http.MethodPost, fmt.Sprintf("/api/v1/spaces/%d/events/ticket", alice.spaceID), bob.token, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("space ticket: expected 403, got %d", rec.Code)
		}
	})

	t.Run("a ticket needs a signed-in user", func(t *testing.T) {
		rec := e.req(t, http.MethodPost, fmt.Sprintf("/api/v1/lists/%d/events/ticket", alice.listID), "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("bob's own ticket does not open alice's list", func(t *testing.T) {
		ticket := e.ticket(t, bob.token, "lists", bob.listID)
		if status := handshakeStatusOf(e.dial("lists", alice.listID, ticket, "")); status != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", status)
		}
	})
}

func handshakeStatusOf(conn *websocket.Conn, resp *http.Response, err error) int {
	if conn != nil {
		_ = conn.CloseNow()
	}
	if err == nil {
		return 101
	}
	if resp == nil {
		return -1
	}
	return resp.StatusCode
}

func TestRealtime_AccessChangedBetweenTicketAndConnectIsHonoured(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	owner := e.newBoard(t, "rt-revoke-owner@example.test")
	guestID, guestToken := newUser(t, e.tc, "rt-revoke-guest@example.test")
	addMemberWithRoleRT(t, e.tc, owner.workspaceID, guestID, "Guest")

	ticket := e.ticket(t, guestToken, "lists", owner.listID)
	if err := e.tc.DB.Exec("UPDATE workspace_members SET deleted_at = now() WHERE workspace_id = ? AND user_id = ?", owner.workspaceID, guestID).Error; err != nil {
		t.Fatal(err)
	}
	if status := handshakeStatusOf(e.dial("lists", owner.listID, ticket, "")); status != http.StatusForbidden {
		t.Fatalf("status %d, want 403: the ticket must not outlive the membership", status)
	}
}

func addMemberWithRoleRT(t *testing.T, tc *TestContext, workspaceID, userID uint, roleName string) {
	t.Helper()
	var roleID uint
	if err := tc.DB.Table("roles").Select("id").Where("workspace_id = ? AND name = ?", workspaceID, roleName).Scan(&roleID).Error; err != nil || roleID == 0 {
		t.Fatalf("role %s: %v", roleName, err)
	}
	if err := tc.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role_id, created_at, updated_at) VALUES (?, ?, ?, now(), now())", workspaceID, userID, roleID).Error; err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func TestRealtime_ConnectedUserIsDroppedWhenAccessIsRevoked(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{RecheckInterval: 100 * time.Millisecond})
	owner := e.newBoard(t, "rt-drop-owner@example.test")
	guestID, guestToken := newUser(t, e.tc, "rt-drop-guest@example.test")
	addMemberWithRoleRT(t, e.tc, owner.workspaceID, guestID, "Guest")
	conn := e.connect(t, guestToken, "lists", owner.listID)

	if err := e.tc.DB.Exec("UPDATE workspace_members SET deleted_at = now() WHERE workspace_id = ? AND user_id = ?", owner.workspaceID, guestID).Error; err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("read after revocation: %v (status %d), want a policy-violation close", err, websocket.CloseStatus(err))
	}
}

func TestRealtime_ForeignOriginIsRejectedAndConfiguredOriginAccepted(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	b := e.newBoard(t, "rt-origin@example.test")

	ticket := e.ticket(t, b.token, "lists", b.listID)
	if status := handshakeStatusOf(e.dial("lists", b.listID, ticket, "https://evil.example")); status != http.StatusForbidden {
		t.Fatalf("foreign origin: status %d, want 403", status)
	}

	allowed := integrationConfig.AllowedOrigins[0]
	ticket = e.ticket(t, b.token, "lists", b.listID)
	conn, resp, err := e.dial("lists", b.listID, ticket, allowed)
	if err != nil {
		t.Fatalf("configured origin %s rejected: %v (%v)", allowed, err, resp)
	}
	_ = conn.CloseNow()
}

func TestRealtime_ConnectionOutlivesTheServersWriteTimeout(t *testing.T) {
	tc := newTestContext(t)
	container, err := app.NewContainer(integrationDB, integrationConfig)
	if err != nil {
		t.Fatal(err)
	}
	router := app.SetupRouter(container, integrationConfig)
	server := httptest.NewUnstartedServer(router)
	server.Config.ReadTimeout = 400 * time.Millisecond
	server.Config.WriteTimeout = 400 * time.Millisecond
	server.Start()
	t.Cleanup(func() {
		container.Realtime.Close()
		server.Close()
		container.Notifications.Close()
		if container.CloseMail != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := container.CloseMail(ctx); err != nil {
				t.Errorf("close integration mail sender: %v", err)
			}
		}
	})
	e := &rtEnv{tc: tc, container: container, router: router, server: server}

	b := e.newBoard(t, "rt-timeout@example.test")
	conn := e.connect(t, b.token, "lists", b.listID)

	time.Sleep(1200 * time.Millisecond)
	e.createTask(t, b, b.listID, b.statusID, "after the timeout")
	if ev := readEvent(t, conn); ev["type"] != "task.created" {
		t.Fatalf("event after the server's write timeout = %v", ev)
	}
}

func TestRealtime_ShutdownSendsGoingAway(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	b := e.newBoard(t, "rt-shutdown@example.test")
	conn := e.connect(t, b.token, "lists", b.listID)

	e.container.Realtime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("read after shutdown: %v (status %d), want StatusGoingAway", err, websocket.CloseStatus(err))
	}

	rec := e.req(t, http.MethodPost, fmt.Sprintf("/api/v1/lists/%d/events/ticket", b.listID), b.token, nil)
	ticket := dataObject(t, rec)["ticket"].(string)
	if status := handshakeStatusOf(e.dial("lists", b.listID, ticket, "")); status != http.StatusServiceUnavailable {
		t.Fatalf("connect during shutdown: status %d, want 503", status)
	}
}

func TestRealtime_PerUserConnectionLimit(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	b := e.newBoard(t, "rt-limit@example.test")

	for i := 0; i < realtime.DefaultMaxConnectionsPerUser; i++ {
		e.connect(t, b.token, "lists", b.listID)
	}
	ticket := e.ticket(t, b.token, "lists", b.listID)
	if status := handshakeStatusOf(e.dial("lists", b.listID, ticket, "")); status != http.StatusTooManyRequests {
		t.Fatalf("connection over the limit: status %d, want 429", status)
	}
}

func TestRealtime_ClientMessagesAreNotAccepted(t *testing.T) {
	e := newRealtimeEnv(t, realtime.Options{})
	b := e.newBoard(t, "rt-write@example.test")
	conn := e.connect(t, b.token, "lists", b.listID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"hello"}`))
	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatal("the server kept the connection open after the client sent a data frame")
	}
}
