package realtime

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPublishUserReachesOnlyThatUsersConnections(t *testing.T) {
	hub := quietHub(5)
	first, _ := hub.Subscribe(1, UserTopic(1))
	second, _ := hub.Subscribe(1, UserTopic(1))
	other, _ := hub.Subscribe(2, UserTopic(2))
	listFollower, _ := hub.Subscribe(1, ListTopic(1))

	count := int64(3)
	hub.PublishUser(1, UserEvent{Type: EventNotificationUnreadCount, UnreadCount: &count})

	for name, sub := range map[string]*Subscriber{"first tab": first, "second tab": second} {
		var ev UserEvent
		if err := json.Unmarshal(<-sub.Messages(), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type != EventNotificationUnreadCount || ev.UnreadCount == nil || *ev.UnreadCount != 3 || ev.At.IsZero() {
			t.Errorf("%s got %+v", name, ev)
		}
	}
	expectSilence(t, other)
	expectSilence(t, listFollower)
}

func TestPublishUserDropsASlowConsumerWithoutBlocking(t *testing.T) {
	hub := quietHub(5)
	slow, _ := hub.Subscribe(1, UserTopic(1))
	for i := 0; i <= subscriberBuffer; i++ {
		hub.PublishUser(1, UserEvent{Type: EventNotificationDeleted, NotificationID: uint(i + 1)})
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("slow consumer was not disconnected")
	}
	if slow.Reason() != "client too slow" {
		t.Fatalf("reason = %q", slow.Reason())
	}
	if hub.Connections() != 0 {
		t.Fatalf("connections = %d, want 0", hub.Connections())
	}
}

func TestUserEventPayloadShape(t *testing.T) {
	count := int64(2)
	raw, err := json.Marshal(UserEvent{
		Type: EventNotificationCreated, UnreadCount: &count,
		Notification: &NotificationSummary{ID: 5, Type: "mention", TaskID: 9, Title: "t"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	n := got["notification"].(map[string]any)
	for _, key := range []string{"id", "type", "task_id", "title", "created_at"} {
		if _, ok := n[key]; !ok {
			t.Errorf("notification has no %q: %v", key, n)
		}
	}
	if _, ok := n["snippet"]; ok {
		t.Errorf("empty snippet should be omitted: %v", n)
	}
	if got["unread_count"] != float64(2) || got["type"] != "notification.created" {
		t.Errorf("event = %v", got)
	}
}

func TestRedeemUserOnlyAcceptsOwnUserTopic(t *testing.T) {
	store, _ := newStore()

	raw, _ := store.Issue(7, UserTopic(7))
	if user, err := store.RedeemUser(raw); err != nil || user != 7 {
		t.Fatalf("redeem = %d, %v", user, err)
	}
	if _, err := store.RedeemUser(raw); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("reuse: %v", err)
	}

	for name, topic := range map[string]string{"list ticket": ListTopic(7), "foreign user topic": UserTopic(8)} {
		raw, _ := store.Issue(7, topic)
		if _, err := store.RedeemUser(raw); !errors.Is(err, ErrInvalidTicket) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	if _, err := store.RedeemUser(""); !errors.Is(err, ErrInvalidTicket) {
		t.Errorf("empty ticket: %v", err)
	}

	raw, _ = store.Issue(7, UserTopic(7))
	if _, err := store.Redeem(raw, ListTopic(7)); !errors.Is(err, ErrInvalidTicket) {
		t.Errorf("user ticket worked on a list: %v", err)
	}
}

func TestRedeemUserExpires(t *testing.T) {
	store, clock := newStore()
	raw, _ := store.Issue(7, UserTopic(7))
	clock.advance(31 * time.Second)
	if _, err := store.RedeemUser(raw); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("expired: %v", err)
	}
}
