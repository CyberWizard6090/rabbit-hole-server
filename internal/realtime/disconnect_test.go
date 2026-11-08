package realtime

import (
	"errors"
	"testing"
	"time"
)

func isKicked(sub *Subscriber) bool {
	select {
	case <-sub.Done():
		return true
	case <-time.After(50 * time.Millisecond):
		return false
	}
}

func TestDisconnectUserClosesEveryConnectionOfThatUserOnly(t *testing.T) {
	hub := quietHub(10)
	own, _ := hub.Subscribe(1, UserTopic(1))
	feed, _ := hub.Subscribe(1, ListTopic(5))
	other, _ := hub.Subscribe(2, UserTopic(2))

	if n := hub.DisconnectUser(1); n != 2 {
		t.Fatalf("disconnected %d connections, want 2", n)
	}
	for _, sub := range []*Subscriber{own, feed} {
		if !isKicked(sub) || sub.Reason() != ReasonAccessRevoked {
			t.Fatalf("subscriber %s was not revoked: %q", sub.Topic, sub.Reason())
		}
	}
	if isKicked(other) {
		t.Fatal("another user must stay connected")
	}
	if hub.Connections() != 1 {
		t.Fatalf("connections = %d", hub.Connections())
	}
	if n := hub.DisconnectUser(1); n != 0 {
		t.Fatalf("second call disconnected %d", n)
	}
}

func TestDisconnectUserFeedsKeepsTheNotificationStream(t *testing.T) {
	hub := quietHub(10)
	own, _ := hub.Subscribe(1, UserTopic(1))
	list, _ := hub.Subscribe(1, ListTopic(5))
	space, _ := hub.Subscribe(1, SpaceTopic(6))

	if n := hub.DisconnectUserFeeds(1); n != 2 {
		t.Fatalf("disconnected %d feeds, want 2", n)
	}
	if !isKicked(list) || !isKicked(space) || isKicked(own) {
		t.Fatal("only the list and space feeds must be closed")
	}
}

func TestDisconnectUserTopicTargetsOneTopic(t *testing.T) {
	hub := quietHub(10)
	a, _ := hub.Subscribe(1, ListTopic(5))
	b, _ := hub.Subscribe(1, ListTopic(6))

	if n := hub.DisconnectUserTopic(1, ListTopic(5)); n != 1 {
		t.Fatalf("disconnected %d", n)
	}
	if !isKicked(a) || isKicked(b) {
		t.Fatal("only the requested topic must be closed")
	}
}

func TestDisconnectFreesTheConnectionQuota(t *testing.T) {
	hub := quietHub(1)
	if _, err := hub.Subscribe(1, ListTopic(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Subscribe(1, ListTopic(2)); !errors.Is(err, ErrTooManyConnections) {
		t.Fatalf("expected the quota to be exhausted, got %v", err)
	}
	hub.DisconnectUser(1)
	if _, err := hub.Subscribe(1, ListTopic(2)); err != nil {
		t.Fatalf("quota must be freed after a disconnect: %v", err)
	}
}

func TestVersionedTicketCarriesTheTokenVersion(t *testing.T) {
	store := NewTicketStore(time.Minute)
	raw, err := store.IssueVersioned(7, UserTopic(7), 4)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := store.RedeemUserGrant(raw)
	if err != nil || grant.UserID != 7 || grant.Version != 4 {
		t.Fatalf("grant = %+v, err = %v", grant, err)
	}
	raw, _ = store.IssueVersioned(7, ListTopic(2), 9)
	if _, err := store.RedeemGrant(raw, ListTopic(3)); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("wrong topic must be refused: %v", err)
	}
}
