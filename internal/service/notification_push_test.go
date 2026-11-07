package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/realtime"
)

type recordedEvent struct {
	UserID uint
	Event  realtime.UserEvent
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (r *recordingPublisher) PublishUser(userID uint, event realtime.UserEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{UserID: userID, Event: event})
}

func (r *recordingPublisher) types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Event.Type
	}
	return out
}

func mentionDelivery() Delivery {
	mention, _ := domain.NotificationTypeByKey(domain.NotificationMention)
	return Delivery{RecipientID: 7, Content: NotificationContent{
		Type: mention, TaskID: 42, TaskTitle: "Fix login", ActorID: 1,
		Payload: map[string]any{"comment_excerpt": "please look here"},
	}}
}

func TestInAppChannelPublishesAfterThePersist(t *testing.T) {
	inbox := &fakeInbox{counts: map[string]int64{"mention": 2, "comment": 1}}
	pub := &recordingPublisher{}
	ch := NewInAppChannel(inbox, 10, pub)

	if err := ch.Deliver(context.Background(), mentionDelivery()); err != nil {
		t.Fatal(err)
	}
	if len(inbox.drafts) != 1 {
		t.Fatalf("drafts = %d", len(inbox.drafts))
	}
	if got := pub.types(); !slices.Equal(got, []string{"notification.created", "notification.unread_count"}) {
		t.Fatalf("events = %v", got)
	}
	created := pub.events[0]
	n := created.Event.Notification
	if created.UserID != 7 || n == nil || n.ID != 1 || n.Type != "mention" || n.TaskID != 42 || n.Title != "Fix login" ||
		n.Snippet != "please look here" || n.CreatedAt.IsZero() {
		t.Fatalf("created = %+v / %+v", created, n)
	}
	if created.Event.UnreadCount == nil || *created.Event.UnreadCount != 3 {
		t.Fatalf("unread_count = %v", created.Event.UnreadCount)
	}
	counts := pub.events[1].Event
	if counts.ByType["mention"] != 2 || counts.ByType["comment"] != 1 || *counts.UnreadCount != 3 {
		t.Fatalf("unread event = %+v", counts)
	}
}

func TestInAppChannelWithoutPublisherStillDelivers(t *testing.T) {
	inbox := &fakeInbox{marked: 1}
	ch := NewInAppChannel(inbox, 10, nil)
	if err := ch.Deliver(context.Background(), mentionDelivery()); err != nil {
		t.Fatal(err)
	}
	if len(inbox.drafts) != 1 {
		t.Fatalf("drafts = %d", len(inbox.drafts))
	}
	svc := NewNotificationService(inbox, nil, nil, NewChannelRegistry(ch))
	if _, err := svc.MarkRead(context.Background(), 7, []uint{1}, false, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), 7, 1); err != nil {
		t.Fatal(err)
	}
	svc.WithPublisher(nil)
	if err := svc.Delete(context.Background(), 7, 1); err != nil {
		t.Fatal(err)
	}
}

func TestSnippetIsTruncatedAndOptional(t *testing.T) {
	inbox := &fakeInbox{}
	pub := &recordingPublisher{}
	ch := NewInAppChannel(inbox, 10, pub)

	long := make([]rune, 500)
	for i := range long {
		long[i] = 'я'
	}
	d := mentionDelivery()
	d.Content.Payload = map[string]any{"comment_excerpt": string(long)}
	if err := ch.Deliver(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if got := []rune(pub.events[0].Event.Notification.Snippet); len(got) != notificationSnippetRunes+1 {
		t.Fatalf("snippet length = %d", len(got))
	}

	d.Content.Payload = map[string]any{"item_text": "buy milk"}
	_ = ch.Deliver(context.Background(), d)
	if got := pub.events[2].Event.Notification.Snippet; got != "buy milk" {
		t.Fatalf("checklist snippet = %q", got)
	}

	d.Content.Payload = nil
	_ = ch.Deliver(context.Background(), d)
	if got := pub.events[4].Event.Notification.Snippet; got != "" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestCreatedEventStillGoesOutWhenTheCountFails(t *testing.T) {
	inbox := &fakeInbox{countsErr: errors.New("db down")}
	pub := &recordingPublisher{}
	if err := NewInAppChannel(inbox, 10, pub).Deliver(context.Background(), mentionDelivery()); err != nil {
		t.Fatal(err)
	}
	if got := pub.types(); !slices.Equal(got, []string{"notification.created"}) {
		t.Fatalf("events = %v", got)
	}
	if pub.events[0].Event.UnreadCount != nil {
		t.Fatal("unread_count must be absent when it cannot be computed")
	}
}

func TestMarkReadAndDeletePublishToTheCaller(t *testing.T) {
	inbox := &fakeInbox{counts: map[string]int64{"mention": 1}, marked: 2}
	pub := &recordingPublisher{}
	svc := NewNotificationService(inbox, nil, nil, NewChannelRegistry()).WithPublisher(pub)

	if _, err := svc.MarkRead(context.Background(), 7, []uint{3, 4}, false, ""); err != nil {
		t.Fatal(err)
	}
	read := pub.events[0]
	if read.UserID != 7 || read.Event.Type != "notification.read" || len(read.Event.IDs) != 2 || read.Event.All ||
		*read.Event.Updated != 2 || *read.Event.UnreadCount != 1 {
		t.Fatalf("read = %+v", read)
	}

	if _, err := svc.MarkRead(context.Background(), 7, nil, true, "mention"); err != nil {
		t.Fatal(err)
	}
	all := pub.events[2]
	if !all.Event.All || all.Event.IDs != nil || all.Event.FilterType != "mention" {
		t.Fatalf("read all = %+v", all.Event)
	}

	if err := svc.Delete(context.Background(), 7, 9); err != nil {
		t.Fatal(err)
	}
	deleted := pub.events[4]
	if deleted.UserID != 7 || deleted.Event.Type != "notification.deleted" || deleted.Event.NotificationID != 9 {
		t.Fatalf("deleted = %+v", deleted)
	}
}

func TestNothingIsPublishedWhenNothingChanged(t *testing.T) {
	inbox := &fakeInbox{counts: map[string]int64{}, deleteErr: domain.ErrNotFound}
	pub := &recordingPublisher{}
	svc := NewNotificationService(inbox, nil, nil, NewChannelRegistry()).WithPublisher(pub)

	if _, err := svc.MarkRead(context.Background(), 7, []uint{3}, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkRead(context.Background(), 7, nil, true, "nonsense"); !errors.Is(err, domain.ErrUnknownNotificationType) {
		t.Fatalf("err = %v", err)
	}
	if err := svc.Delete(context.Background(), 7, 9); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(pub.events) != 0 {
		t.Fatalf("events = %v", pub.types())
	}
}
