package realtime

import (
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newStore() (*TicketStore, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	store := NewTicketStore(30 * time.Second)
	store.now = clock.now
	return store, clock
}

func TestTicketIsSingleUse(t *testing.T) {
	store, _ := newStore()
	raw, err := store.Issue(7, "list:1")
	if err != nil {
		t.Fatal(err)
	}
	if user, err := store.Redeem(raw, "list:1"); err != nil || user != 7 {
		t.Fatalf("first redeem = %d, %v", user, err)
	}
	if _, err := store.Redeem(raw, "list:1"); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("second redeem: err=%v, want ErrInvalidTicket", err)
	}
}

func TestTicketExpires(t *testing.T) {
	store, clock := newStore()
	raw, _ := store.Issue(7, "list:1")
	clock.advance(31 * time.Second)
	if _, err := store.Redeem(raw, "list:1"); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("expired ticket: err=%v", err)
	}

	raw, _ = store.Issue(7, "list:1")
	clock.advance(29 * time.Second)
	if _, err := store.Redeem(raw, "list:1"); err != nil {
		t.Fatalf("ticket within its TTL rejected: %v", err)
	}
}

func TestTicketIsBoundToItsTopicAndConsumedByAMismatch(t *testing.T) {
	store, _ := newStore()
	raw, _ := store.Issue(7, "list:1")
	if _, err := store.Redeem(raw, "list:2"); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("wrong topic: err=%v", err)
	}
	if _, err := store.Redeem(raw, "list:1"); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("ticket survived a mismatched redeem: err=%v", err)
	}
}

func TestTicketRejectsGarbage(t *testing.T) {
	store, _ := newStore()
	for _, raw := range []string{"", "nope", "AAAA"} {
		if _, err := store.Redeem(raw, "list:1"); !errors.Is(err, ErrInvalidTicket) {
			t.Errorf("Redeem(%q): err=%v", raw, err)
		}
	}
}

func TestTicketsAreUniqueAndStoredHashed(t *testing.T) {
	store, _ := newStore()
	a, _ := store.Issue(1, "list:1")
	b, _ := store.Issue(1, "list:1")
	if a == b || len(a) < 40 {
		t.Fatalf("tickets must be long and unique: %q %q", a, b)
	}
	for stored := range store.tickets {
		if stored == a || stored == b {
			t.Fatal("raw ticket is stored; only a hash should be kept")
		}
	}
}

func TestPerUserTicketCapAndExpiredOnesDoNotCount(t *testing.T) {
	store, clock := newStore()
	for i := 0; i < maxTicketsPerUser; i++ {
		if _, err := store.Issue(1, "list:1"); err != nil {
			t.Fatalf("ticket %d: %v", i, err)
		}
	}
	if _, err := store.Issue(1, "list:1"); !errors.Is(err, ErrTooManyTickets) {
		t.Fatalf("over the cap: err=%v", err)
	}
	if _, err := store.Issue(2, "list:1"); err != nil {
		t.Fatalf("another user is affected by the cap: %v", err)
	}

	clock.advance(time.Minute)
	if _, err := store.Issue(1, "list:1"); err != nil {
		t.Fatalf("expired tickets still count against the cap: %v", err)
	}
}
