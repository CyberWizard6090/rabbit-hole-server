package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/mail"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

const opsPassword = "password-123"

type opsEnv struct {
	tc     *TestContext
	server *httptest.Server
}

func newOpsEnv(t *testing.T, opts realtime.Options, adjust func(*config.Config)) *opsEnv {
	t.Helper()
	base := newTestContext(t)
	cfg := *integrationConfig
	cfg.Storage.LocalDir = t.TempDir()
	if adjust != nil {
		adjust(&cfg)
	}
	outbox := mail.NewMemory()
	container, err := app.NewContainerWithMailer(integrationDB, &cfg, outbox)
	if err != nil {
		t.Fatal(err)
	}
	container.RealtimeHandler.WithOptions(opts)
	router := app.SetupRouter(container, &cfg)
	server := httptest.NewServer(router)
	t.Cleanup(func() {
		container.Realtime.Close()
		server.Close()
		container.Notifications.Close()
		service.ConfigurePasswordHashing(0, service.DefaultPasswordHashWait)
	})
	tc := &TestContext{DB: base.DB, Router: router, Mail: outbox}
	return &opsEnv{tc: tc, server: server}
}

func (e *opsEnv) dial(t *testing.T, path, ticket string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, e.server.URL+path+"?ticket="+ticket, nil)
	if err != nil {
		status := ""
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("dial %s: %v %s", path, err, status)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	if ev := readEvent(t, conn); ev["type"] != "ready" {
		t.Fatalf("first frame = %v", ev)
	}
	return conn
}

func (e *opsEnv) ticket(t *testing.T, token, path string) string {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, path, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket %s: %d: %s", path, rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)["ticket"].(string)
}

func (e *opsEnv) notifications(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	return e.dial(t, notificationEventsPath, e.ticket(t, token, notificationTicketPath))
}

func (e *opsEnv) listFeed(t *testing.T, token string, listID uint) *websocket.Conn {
	t.Helper()
	return e.dial(t, fmt.Sprintf("/api/v1/lists/%d/events", listID), e.ticket(t, token, fmt.Sprintf("/api/v1/lists/%d/events/ticket", listID)))
}

func expectClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, _, err := conn.Read(ctx)
		if err == nil {
			continue
		}
		if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
			t.Fatalf("connection ended with %v (status %d), want a policy violation close", err, status)
		}
		return
	}
}

func expectOpen(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if websocket.CloseStatus(err) != -1 {
		t.Fatalf("connection was closed: %v", err)
	}
}

func opsUserWithFeed(t *testing.T, e *opsEnv, email string) (uint, string, *websocket.Conn) {
	t.Helper()
	id, token := newUser(t, e.tc, email)
	return id, token, e.notifications(t, token)
}

func TestRealtimeRevocation_LogoutAllClosesTheNotificationFeed(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{}, nil)
	_, token, conn := opsUserWithFeed(t, e, "rev-logout@example.test")

	if rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/auth/logout-all", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("logout-all: %d: %s", rec.Code, rec.Body.String())
	}
	expectClosed(t, conn)
}

func TestRealtimeRevocation_PasswordChangeClosesEveryFeedOfTheUserOnly(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{}, nil)
	_, token, conn := opsUserWithFeed(t, e, "rev-change@example.test")
	second := e.notifications(t, token)
	_, _, bystander := opsUserWithFeed(t, e, "rev-bystander@example.test")

	rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/profile/password", token, map[string]any{
		"current_password": opsPassword, "new_password": "a-brand-new-pass-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d: %s", rec.Code, rec.Body.String())
	}
	expectClosed(t, conn)
	expectClosed(t, second)
	expectOpen(t, bystander)
}

func TestRealtimeRevocation_PasswordResetClosesTheFeed(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{}, nil)
	email := "rev-reset@example.test"
	_, _, conn := opsUserWithFeed(t, e, email)

	if rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/auth/forgot-password", "", map[string]any{"email": email}); rec.Code != http.StatusAccepted {
		t.Fatalf("forgot-password: %d: %s", rec.Code, rec.Body.String())
	}
	msgs := e.tc.Mail.To(email)
	token := linkToken(t, msgs[len(msgs)-1], "/reset-password")
	rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/auth/reset-password", "", map[string]any{"token": token, "new_password": "a-brand-new-pass-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("reset-password: %d: %s", rec.Code, rec.Body.String())
	}
	expectClosed(t, conn)
}

func TestRealtimeRevocation_AccountDeletionClosesTheFeed(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{}, nil)
	_, token, conn := opsUserWithFeed(t, e, "rev-delete@example.test")

	rec := request(t, e.tc.Router, http.MethodDelete, "/api/v1/profile", token, map[string]any{"password": opsPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete account: %d: %s", rec.Code, rec.Body.String())
	}
	expectClosed(t, conn)
}

func TestRealtimeRevocation_TokenVersionBumpIsCaughtByTheRevalidation(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{RecheckInterval: 100 * time.Millisecond}, nil)
	id, _, conn := opsUserWithFeed(t, e, "rev-bump@example.test")
	_, _, other := opsUserWithFeed(t, e, "rev-bump-other@example.test")

	time.Sleep(250 * time.Millisecond)
	if err := e.tc.DB.Exec("UPDATE users SET token_version = token_version + 1 WHERE id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	expectClosed(t, conn)
	expectOpen(t, other)
}

func TestRealtimeRevocation_RemovalFromTheWorkspaceClosesResourceFeedsOnly(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{}, nil)
	owner := newWorkspaceOwner(t, e.tc, "rev-ws")
	listID := createListIn(t, e.tc, owner)
	memberID, memberToken := newUser(t, e.tc, "rev-ws-member@example.test")
	addContact(t, e.tc, owner.token, memberID)
	if rec := request(t, e.tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token, map[string]any{"user_id": memberID, "role": "Member"}); rec.Code != http.StatusCreated {
		t.Fatalf("add member: %d: %s", rec.Code, rec.Body.String())
	}
	feed := e.listFeed(t, memberToken, listID)
	notifications := e.notifications(t, memberToken)
	ownerFeed := e.listFeed(t, owner.token, listID)

	if rec := request(t, e.tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/"+itoa(memberID)), owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove member: %d: %s", rec.Code, rec.Body.String())
	}
	expectClosed(t, feed)
	expectOpen(t, notifications)
	expectOpen(t, ownerFeed)
}

func TestRealtimeRevocation_LeavingTheWorkspaceClosesResourceFeeds(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{}, nil)
	owner := newWorkspaceOwner(t, e.tc, "rev-leave")
	listID := createListIn(t, e.tc, owner)
	memberID, memberToken := newUser(t, e.tc, "rev-leave-member@example.test")
	addContact(t, e.tc, owner.token, memberID)
	if rec := request(t, e.tc.Router, http.MethodPost, membersPath(owner.workspaceID, ""), owner.token, map[string]any{"user_id": memberID, "role": "Member"}); rec.Code != http.StatusCreated {
		t.Fatalf("add member: %d: %s", rec.Code, rec.Body.String())
	}
	feed := e.listFeed(t, memberToken, listID)

	if rec := request(t, e.tc.Router, http.MethodDelete, membersPath(owner.workspaceID, "/me"), memberToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("leave: %d: %s", rec.Code, rec.Body.String())
	}
	expectClosed(t, feed)
}

func createListIn(t *testing.T, tc *TestContext, owner workspaceOwner) uint {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", owner.token, map[string]any{"name": "ops list"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func TestRealtimeRevocation_TicketIssuedBeforeARevocationIsUseless(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{}, nil)
	_, token := newUser(t, e.tc, "rev-stale@example.test")
	ticket := e.ticket(t, token, notificationTicketPath)

	if rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/auth/logout-all", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("logout-all: %d", rec.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, e.server.URL+notificationEventsPath+"?ticket="+ticket, nil)
	if err == nil {
		defer func() { _ = conn.CloseNow() }()
		expectClosed(t, conn)
		return
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stale ticket: err=%v resp=%v", err, resp)
	}
}

func TestSecurity_ForwardedForIsIgnoredWithoutTrustedProxies(t *testing.T) {
	tc := newTestContext(t)
	var last *httptest.ResponseRecorder
	for i := 0; i < 7; i++ {
		last = requestWithHeader(t, tc, http.MethodPost, "/api/v1/auth/forgot-password", "", "X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1),
			map[string]any{"email": "nobody@example.test"})
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed X-Forwarded-For must not give a fresh rate-limit budget, got %d", last.Code)
	}
}

func TestSecurity_ForwardedForIsHonouredOnlyFromTrustedProxies(t *testing.T) {
	trusted := newTestContextWith(t, func(cfg *config.Config) { cfg.Server.TrustedProxies = []string{"192.0.2.0/24"} })
	var last *httptest.ResponseRecorder
	for i := 0; i < 7; i++ {
		last = requestWithHeader(t, trusted, http.MethodPost, "/api/v1/auth/forgot-password", "", "X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1),
			map[string]any{"email": "nobody@example.test"})
	}
	if last.Code == http.StatusTooManyRequests {
		t.Fatalf("a trusted proxy reports distinct clients, each with its own budget, got %d", last.Code)
	}
}

func TestSecurity_IPv6ClientsShareTheirPrefixBudget(t *testing.T) {
	tc := newTestContext(t)
	var last int
	for i := 0; i < 7; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", strings.NewReader(`{"email":"nobody@example.test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = fmt.Sprintf("[2001:db8:7:7::%x]:5000", i+1)
		rec := httptest.NewRecorder()
		tc.Router.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("rotating addresses inside one /64 must not reset the budget, got %d", last)
	}
}

func TestSecurity_RepeatedWrongPasswordsLockTheAccountUniformly(t *testing.T) {
	tc := newTestContext(t)
	newUser(t, tc, "lock-victim@example.test")

	attempt := func(email, password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = fmt.Sprintf("203.0.113.%d:4000", time.Now().Nanosecond()%200+1)
		rec := httptest.NewRecorder()
		tc.Router.ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < 5; i++ {
		if rec := attempt("lock-victim@example.test", "wrong-password"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d: %d", i+1, rec.Code)
		}
		if rec := attempt("lock-ghost@example.test", "wrong-password"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("unknown address %d: %d", i+1, rec.Code)
		}
	}
	known := attempt("lock-victim@example.test", opsPassword)
	unknown := attempt("lock-ghost@example.test", opsPassword)
	if known.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("locked login: known=%d unknown=%d", known.Code, unknown.Code)
	}
	normalize := func(rec *httptest.ResponseRecorder) string {
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		delete(body, "request_id")
		if errBody, ok := body["error"].(map[string]any); ok {
			delete(errBody, "request_id")
		}
		raw, _ := json.Marshal(body)
		return string(raw)
	}
	if normalize(known) != normalize(unknown) {
		t.Fatalf("responses differ and would reveal the account:\n%s\n%s", known.Body.String(), unknown.Body.String())
	}
	if known.Header().Get("Retry-After") != unknown.Header().Get("Retry-After") {
		t.Fatal("Retry-After must not differ between known and unknown addresses")
	}
}

func TestSecurity_SaturatedPasswordHashingAnswers503WithRetryAfter(t *testing.T) {
	e := newOpsEnv(t, realtime.Options{}, func(cfg *config.Config) {
		cfg.Auth.HashConcurrency = 1
		cfg.Auth.HashWait = 0
		cfg.Auth.LoginFailureLimit = 0
	})
	newUser(t, e.tc, "busy-user@example.test")
	service.ConfigurePasswordHashing(1, 0)

	var wg sync.WaitGroup
	var mu sync.Mutex
	statuses := map[int]int{}
	retryAfter := ""
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": "busy-user@example.test", "password": opsPassword})
			mu.Lock()
			defer mu.Unlock()
			statuses[rec.Code]++
			if rec.Code == http.StatusServiceUnavailable {
				retryAfter = rec.Header().Get("Retry-After")
			}
		}()
	}
	wg.Wait()
	if statuses[http.StatusServiceUnavailable] == 0 || statuses[http.StatusOK] == 0 {
		t.Fatalf("expected both served and refused logins, got %v", statuses)
	}
	if retryAfter == "" {
		t.Fatal("503 must carry Retry-After")
	}
}

func TestLogging_FailingQueryNeverLogsParameterValues(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, environment := range []string{"development", "staging", "production"} {
		buf.Reset()
		opts := integrationConfig.DB.Options(environment)
		opts.Logger = logger
		opts.SlowQueryThreshold = time.Nanosecond
		db, err := database.ConnectDB(integrationConfig.DB.URL, opts)
		if err != nil {
			t.Fatal(err)
		}

		const secretEmail = "plain-secret-victim@example.test"
		const secretHash = "$argon2id$v=19$SECRET-HASH-VALUE"
		insert := "INSERT INTO users (email, username, password_hash) VALUES (?, ?, ?)"
		_ = db.Exec(insert, secretEmail, "log-user-"+environment, secretHash).Error
		if err := db.Exec(insert, secretEmail, "log-user-dup-"+environment, secretHash).Error; err == nil {
			t.Fatal("the second insert should violate the unique e-mail constraint")
		}
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()

		out := buf.String()
		if out == "" {
			t.Fatalf("%s: the failing query was not logged at all", environment)
		}
		for _, secret := range []string{secretEmail, "SECRET-HASH-VALUE", "plain-secret-victim"} {
			if strings.Contains(out, secret) {
				t.Fatalf("%s: log leaks %q: %s", environment, secret, out)
			}
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("%s: gorm wrote a line that is not JSON: %q", environment, line)
			}
		}
	}
}

func TestDatabase_SessionTimeoutsAreApplied(t *testing.T) {
	opts := integrationConfig.DB.Options("test")
	opts.StatementTimeout = 150 * time.Millisecond
	opts.LockTimeout = 90 * time.Millisecond
	opts.IdleInTxnTimeout = 2 * time.Minute
	opts.Logger = slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	db, err := database.ConnectDB(integrationConfig.DB.URL, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	}()

	var statement, lock, idle string
	row := db.Raw("SELECT current_setting('statement_timeout'), current_setting('lock_timeout'), current_setting('idle_in_transaction_session_timeout')").Row()
	if err := row.Scan(&statement, &lock, &idle); err != nil {
		t.Fatal(err)
	}
	if statement != "150ms" || lock != "90ms" || idle != "2min" {
		t.Fatalf("session settings = %s %s %s", statement, lock, idle)
	}
	if err := db.Exec("SELECT pg_sleep(2)").Error; err == nil || !strings.Contains(err.Error(), "statement timeout") {
		t.Fatalf("a slow statement must be canceled, got %v", err)
	}
}

func TestDatabase_SchemaCheckAndGuardedRollback(t *testing.T) {
	ctx := context.Background()
	if err := database.CheckSchema(ctx, integrationDB); err != nil {
		t.Fatalf("the migrated test database must pass the schema check: %v", err)
	}

	var current int
	if err := integrationDB.Raw("SELECT version FROM schema_migrations").Scan(&current).Error; err != nil {
		t.Fatal(err)
	}
	restore := func() {
		_ = integrationDB.Exec("UPDATE schema_migrations SET version = ?, dirty = false", current).Error
	}
	t.Cleanup(restore)

	if err := integrationDB.Exec("UPDATE schema_migrations SET dirty = true").Error; err != nil {
		t.Fatal(err)
	}
	var schemaErr *database.SchemaError
	if err := database.CheckSchema(ctx, integrationDB); !errors.As(err, &schemaErr) || !schemaErr.Dirty || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty schema: %v", err)
	}

	if err := integrationDB.Exec("UPDATE schema_migrations SET version = 1, dirty = false").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.CheckSchema(ctx, integrationDB); !errors.As(err, &schemaErr) || schemaErr.Current != 1 || !strings.Contains(err.Error(), "migrate -action up") {
		t.Fatalf("schema behind: %v", err)
	}
	restore()

	if _, _, err := database.RunMigrations(integrationConfig.DB.URL, "down", 0, 0); !errors.Is(err, database.ErrDownNeedsSteps) {
		t.Fatalf("down without steps must be refused, got %v", err)
	}
	if err := database.CheckSchema(ctx, integrationDB); err != nil {
		t.Fatalf("the refused rollback must not touch the schema: %v", err)
	}
}
