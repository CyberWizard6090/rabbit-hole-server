package integration

import (
	"context"
	"testing"
)

func TestRetention_RemovesOnlyOldTechnicalData(t *testing.T) {
	tc := newTestContext(t)
	userID, _ := newUser(t, tc, "retention@example.test")
	_, container := newReplica(t, tc)

	exec := func(query string, args ...any) {
		t.Helper()
		if err := tc.DB.Exec(query, args...).Error; err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	exec(`DELETE FROM outbox_events`)
	exec(`INSERT INTO outbox_events (event_type, aggregate_type, aggregate_id, payload, published_at) VALUES
		('notify.entry', 'task', 1, '{}', now() - interval '10 days'),
		('notify.entry', 'task', 2, '{}', now() - interval '1 day')`)
	exec(`INSERT INTO outbox_events (event_type, aggregate_type, aggregate_id, payload, dead_at) VALUES
		('delivery.mail', 'mail', 0, '{}', now() - interval '40 days'),
		('delivery.mail', 'mail', 0, '{}', now() - interval '2 days')`)
	exec(`INSERT INTO outbox_events (event_type, aggregate_type, aggregate_id, payload, created_at) VALUES
		('notify.entry', 'task', 3, '{}', now() - interval '60 days')`)

	exec(`DELETE FROM email_tokens`)
	exec(`INSERT INTO email_tokens (user_id, kind, token_hash, expires_at) VALUES
		(?, 'verify_email', 'old-expired', now() - interval '10 days'),
		(?, 'verify_email', 'recently-expired', now() - interval '1 day'),
		(?, 'verify_email', 'still-valid', now() + interval '1 day')`, userID, userID, userID)
	exec(`INSERT INTO email_tokens (user_id, kind, token_hash, expires_at, used_at) VALUES
		(?, 'reset_password', 'old-used', now() + interval '1 day', now() - interval '9 days')`, userID)

	exec(`UPDATE user_sessions SET expires_at = now() - interval '10 days' WHERE id = (SELECT min(id) FROM user_sessions)`)
	exec(`INSERT INTO user_sessions (user_id, token_hash, expires_at) VALUES (?, 'live-session', now() + interval '1 day')`, userID)

	result, err := container.PurgeData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.OutboxDone != 1 || result.OutboxDead != 1 || result.EmailTokens != 2 || result.Sessions != 1 {
		t.Fatalf("result = %+v", result)
	}
	if n := countRows(t, tc, `SELECT count(*) FROM outbox_events`); n != 3 {
		t.Fatalf("outbox rows left = %d, want 3 (recent done, recent dead, still pending)", n)
	}
	if n := countRows(t, tc, `SELECT count(*) FROM outbox_events WHERE published_at IS NULL AND dead_at IS NULL`); n != 1 {
		t.Fatal("a pending event must never be purged, however old")
	}
	if n := countRows(t, tc, `SELECT count(*) FROM email_tokens`); n != 2 {
		t.Fatalf("e-mail tokens left = %d, want 2", n)
	}
	if n := countRows(t, tc, `SELECT count(*) FROM user_sessions WHERE token_hash = 'live-session'`); n != 1 {
		t.Fatal("a live session was removed")
	}

	again, err := container.PurgeData(context.Background())
	if err != nil || again.Total() != 0 {
		t.Fatalf("second run = %+v, %v", again, err)
	}
}
