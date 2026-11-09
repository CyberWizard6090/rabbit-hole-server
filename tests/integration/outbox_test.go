package integration

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/mail"
	"rabbit-hole-server/internal/service"
)

func countRows(t *testing.T, tc *TestContext, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := tc.DB.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func restartedWorkers(t *testing.T, tc *TestContext) ([]*service.OutboxWorker, *mail.Memory) {
	t.Helper()
	transport := mail.NewMemory()
	cfg := tc.Config
	container, err := app.NewContainerWithMailer(integrationDB, &cfg, transport)
	if err != nil {
		t.Fatalf("create a second container: %v", err)
	}
	return container.Workers, transport
}

func TestOutbox_NotificationEventIsStoredWithTheChangeAndSurvivesARestart(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)

	rec := request(t, tc.Raw, http.MethodPost, "/api/v1/lists/"+itoa(f.listID)+"/tasks", f.owner.token,
		map[string]any{"title": "assigned while the worker is down", "status_id": f.status1, "assignee_ids": []uint{f.memberA}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	taskID := responseID(t, rec)

	if n := countRows(t, tc, "SELECT count(*) FROM task_activities WHERE task_id = ? AND action = 'task.created'", taskID); n != 1 {
		t.Fatalf("activity rows = %d, want 1", n)
	}
	if n := countRows(t, tc, "SELECT count(*) FROM outbox_events WHERE event_type = 'notify.entry' AND aggregate_id = ? AND published_at IS NULL", taskID); n != 1 {
		t.Fatalf("pending outbox events = %d, want 1", n)
	}
	if n := countRows(t, tc, "SELECT count(*) FROM notifications WHERE user_id = ? AND task_id = ?", f.memberA, taskID); n != 0 {
		t.Fatalf("notifications before the worker ran = %d, want 0", n)
	}

	workers, _ := restartedWorkers(t, tc)
	if err := service.DrainOutbox(context.Background(), workers...); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, tc, "SELECT count(*) FROM notifications WHERE user_id = ? AND task_id = ?", f.memberA, taskID); n != 1 {
		t.Fatalf("notifications after the restart = %d, want 1", n)
	}
	if n := countRows(t, tc, "SELECT count(*) FROM outbox_events WHERE event_type = 'notify.entry' AND aggregate_id = ? AND published_at IS NULL", taskID); n != 0 {
		t.Fatalf("pending outbox events after processing = %d, want 0", n)
	}
}

func TestOutbox_ConcurrentWorkersProduceEachNotificationOnce(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)

	for i := 0; i < 5; i++ {
		rec := request(t, tc.Raw, http.MethodPost, "/api/v1/lists/"+itoa(f.listID)+"/tasks", f.owner.token,
			map[string]any{"title": "parallel " + itoa(uint(i)), "status_id": f.status1, "assignee_ids": []uint{f.memberB}})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
		}
	}

	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers, _ := restartedWorkers(t, tc)
		group.Add(1)
		go func() {
			defer group.Done()
			if err := service.DrainOutbox(context.Background(), workers...); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()

	if n := countRows(t, tc, "SELECT count(*) FROM notifications WHERE user_id = ? AND type = 'assigned'", f.memberB); n != 5 {
		t.Fatalf("assigned notifications = %d, want exactly 5", n)
	}
}

func TestOutbox_MailIsStoredSealedAndSentByTheWorker(t *testing.T) {
	tc := newTestContext(t)
	email := "outbox-mail@example.test"
	rec := request(t, tc.Raw, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": "password-123",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d: %s", rec.Code, rec.Body.String())
	}
	if len(tc.Mail.To(email)) != 0 {
		t.Fatal("the request must not send the mail itself")
	}
	var stored string
	if err := tc.DB.Raw("SELECT payload::text FROM outbox_events WHERE event_type = 'delivery.mail' AND published_at IS NULL").Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored == "" || strings.Contains(stored, email) || strings.Contains(stored, "verify-email") {
		t.Fatalf("the stored mail must be sealed, got %q", stored)
	}

	if err := service.DrainOutbox(context.Background(), tc.Workers...); err != nil {
		t.Fatal(err)
	}
	if len(tc.Mail.To(email)) != 1 {
		t.Fatalf("mails after the worker ran = %d, want 1", len(tc.Mail.To(email)))
	}
	var left string
	if err := tc.DB.Raw("SELECT payload::text FROM outbox_events WHERE event_type = 'delivery.mail'").Scan(&left).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(left, "sealed") {
		t.Fatalf("a sent mail must not keep its body: %q", left)
	}
}
